package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/repository"
)

func newPersonTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	_ = db.Create(&model.FaceGalleryRevision{ID: 1, Revision: 0}).Error
	return db
}

func TestPersonRepositoryCRUDAndSoftDelete(t *testing.T) {
	db := newPersonTestDB(t)
	repo := repository.NewPersonRepository(db)
	ctx := context.Background()

	// 1. Create
	person := &model.Person{
		PersonID: "test_person_1",
		Name:     "Alice",
	}
	if err := repo.Create(ctx, person); err != nil {
		t.Fatalf("create person failed: %v", err)
	}

	// Duplicate create should return ErrDuplicateKey
	dupPerson := &model.Person{
		PersonID: "test_person_1",
		Name:     "Alice 2",
	}
	if err := repo.Create(ctx, dupPerson); !errors.Is(err, repository.ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}

	// 2. GetByPersonID
	got, err := repo.GetByPersonID(ctx, "test_person_1")
	if err != nil {
		t.Fatalf("get person failed: %v", err)
	}
	if got.Name != "Alice" {
		t.Fatalf("name mismatch: got %s, want Alice", got.Name)
	}

	// 3. UpdateName
	updated, err := repo.UpdateName(ctx, "test_person_1", "Alice New")
	if err != nil {
		t.Fatalf("update name failed: %v", err)
	}
	if updated.Name != "Alice New" {
		t.Fatalf("expected Alice New, got %s", updated.Name)
	}

	// 4. ListPage
	list, total, err := repo.ListPage(ctx, &repository.PersonFilter{
		Page:     1,
		PageSize: 10,
		Name:     "Alice",
	})
	if err != nil {
		t.Fatalf("list page failed: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 record, total=%d, len=%d", total, len(list))
	}

	// 5. Delete (Soft delete)
	deleted, err := repo.Delete(ctx, "test_person_1")
	if err != nil || !deleted {
		t.Fatalf("delete failed: err=%v, deleted=%v", err, deleted)
	}

	// 再次删除应该返回 false, nil
	deletedAgain, err := repo.Delete(ctx, "test_person_1")
	if err != nil || deletedAgain {
		t.Fatalf("delete again expected false, got err=%v, deleted=%v", err, deletedAgain)
	}

	// 软删除后 GetByPersonID 应该返回 ErrNotFound
	if _, err := repo.GetByPersonID(ctx, "test_person_1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// 软删除记录不能通过 UpdateName 被隐式恢复。
	if _, err := repo.UpdateName(ctx, "test_person_1", "Should Stay Deleted"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when updating deleted person, got %v", err)
	}

	// 但 GetByPersonIDWithDeleted 能够查出
	deletedRecord, err := repo.GetByPersonIDWithDeleted(ctx, "test_person_1")
	if err != nil {
		t.Fatalf("get with deleted failed: %v", err)
	}
	if deletedRecord.DeletedAt == 0 {
		t.Fatalf("expected deleted_at > 0")
	}

	// 6. RestoreAndUpdate
	restored, err := repo.RestoreAndUpdate(ctx, deletedRecord.ID, "Alice Restored")
	if err != nil {
		t.Fatalf("restore and update failed: %v", err)
	}
	if restored.ID != deletedRecord.ID {
		t.Fatalf("internal ID should not change: got %d, want %d", restored.ID, deletedRecord.ID)
	}
	if restored.Name != "Alice Restored" {
		t.Fatalf("name should be Alice Restored, got %s", restored.Name)
	}
	if restored.DeletedAt != 0 {
		t.Fatalf("restored deleted_at should be 0")
	}

	// 恢复后普通查询可用
	active, err := repo.GetByPersonID(ctx, "test_person_1")
	if err != nil || active.Name != "Alice Restored" {
		t.Fatalf("get restored active failed: err=%v, active=%v", err, active)
	}
}

func TestPersonRepositoryBatchDelete(t *testing.T) {
	db := newPersonTestDB(t)
	repo := repository.NewPersonRepository(db)
	ctx := context.Background()

	_ = repo.Create(ctx, &model.Person{PersonID: "p1", Name: "User 1"})
	_ = repo.Create(ctx, &model.Person{PersonID: "p2", Name: "User 2"})
	_ = repo.Create(ctx, &model.Person{PersonID: "p3", Name: "User 3"})

	if err := repo.BatchDelete(ctx, []string{"p1", "p2", "non_existing"}); err != nil {
		t.Fatalf("batch delete failed: %v", err)
	}

	if _, err := repo.GetByPersonID(ctx, "p1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("p1 should be deleted")
	}
	if _, err := repo.GetByPersonID(ctx, "p2"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("p2 should be deleted")
	}
	p3, err := repo.GetByPersonID(ctx, "p3")
	if err != nil || p3.PersonID != "p3" {
		t.Fatalf("p3 should still exist")
	}
}

func TestPersonRepositoryUpdatePrimaryFaceID(t *testing.T) {
	db := newPersonTestDB(t)
	repo := repository.NewPersonRepository(db)
	ctx := context.Background()

	_ = repo.Create(ctx, &model.Person{PersonID: "p1", Name: "User 1"})

	updated, err := repo.UpdatePrimaryFaceID(ctx, "p1", "face_123")
	if err != nil {
		t.Fatalf("update primary face failed: %v", err)
	}
	if updated.PrimaryFaceID != "face_123" {
		t.Fatalf("expected primary_face_id to be face_123, got %s", updated.PrimaryFaceID)
	}

	// Update to empty
	updated, err = repo.UpdatePrimaryFaceID(ctx, "p1", "")
	if err != nil || updated.PrimaryFaceID != "" {
		t.Fatalf("update primary face to empty failed: %v", err)
	}

	// Update non-existing
	_, err = repo.UpdatePrimaryFaceID(ctx, "non_existing", "face_123")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existing person, got %v", err)
	}
}
