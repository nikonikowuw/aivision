package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

const maxBatchDeleteForTest = 100

func newPersonServiceTestDB(t *testing.T) *gorm.DB {
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
	return db
}

func TestPersonServiceCreateValidationAndAutoID(t *testing.T) {
	db := newPersonServiceTestDB(t)
	repo := repository.NewPersonRepository(db)
	svc := service.NewPersonService(repo)
	ctx := context.Background()

	// 1. Auto generate UUID
	dto, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		Name: "Alice",
	})
	if err != nil {
		t.Fatalf("create without personId failed: %v", err)
	}
	if len(dto.PersonID) != 32 || strings.Contains(dto.PersonID, "-") {
		t.Fatalf("invalid auto generated personId: %s", dto.PersonID)
	}
	if dto.Name != "Alice" {
		t.Fatalf("name mismatch: %s", dto.Name)
	}

	// 2. Invalid personId (starts with symbol)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "_invalid",
		Name:     "Bob",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for _invalid, got %v", err)
	}

	// 3. Invalid personId (contains spaces or illegal symbols)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "valid/part",
		Name:     "Bob",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for slash in personId, got %v", err)
	}

	// 4. Invalid name (empty, too long, control chars)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob1",
		Name:     "   ",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for empty name, got %v", err)
	}

	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob2",
		Name:     "Bob\x00Name",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for control char in name, got %v", err)
	}

	longName := strings.Repeat("字", 65)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob3",
		Name:     longName,
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for >64 chars name, got %v", err)
	}

	// 5. Valid custom personId
	dto2, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie",
	})
	if err != nil {
		t.Fatalf("create custom personId failed: %v", err)
	}
	if dto2.PersonID != "Emp:001-A.b_c" {
		t.Fatalf("personId changed: %s", dto2.PersonID)
	}

	// 6. Duplicate active personId -> CodePersonIDTaken
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie 2",
	})
	if !isErrCode(err, errno.CodePersonIDTaken) {
		t.Fatalf("expected CodePersonIDTaken, got %v", err)
	}

	// 7. Duplicate deleted personId -> restore and update name
	if err := svc.DeletePerson(ctx, "Emp:001-A.b_c"); err != nil {
		t.Fatalf("delete person failed: %v", err)
	}
	restored, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie Restored",
	})
	if err != nil {
		t.Fatalf("create deleted person should restore: %v", err)
	}
	if restored.Name != "Charlie Restored" {
		t.Fatalf("restored name mismatch: %s", restored.Name)
	}
}

func TestPersonServiceSyncUpsertAndDelete(t *testing.T) {
	db := newPersonServiceTestDB(t)
	repo := repository.NewPersonRepository(db)
	svc := service.NewPersonService(repo)
	ctx := context.Background()

	// 1. SyncUpsert new
	dto, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1",
	})
	if err != nil {
		t.Fatalf("sync upsert failed: %v", err)
	}
	if dto.PersonID != "sync_01" || dto.Name != "Sync User 1" {
		t.Fatalf("unexpected dto: %+v", dto)
	}

	// 2. SyncUpsert existing
	dto2, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1 Updated",
	})
	if err != nil {
		t.Fatalf("sync update failed: %v", err)
	}
	if dto2.Name != "Sync User 1 Updated" {
		t.Fatalf("unexpected name: %s", dto2.Name)
	}

	// 3. SyncDelete idempotent
	if err := svc.SyncDeletePerson(ctx, "sync_01"); err != nil {
		t.Fatalf("sync delete failed: %v", err)
	}
	// delete again should succeed
	if err := svc.SyncDeletePerson(ctx, "sync_01"); err != nil {
		t.Fatalf("sync delete again should succeed: %v", err)
	}
	if err := svc.SyncDeletePerson(ctx, "non_existent"); err != nil {
		t.Fatalf("sync delete non_existent should succeed: %v", err)
	}

	// 4. SyncUpsert on deleted record restores it
	dto3, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1 Restored",
	})
	if err != nil {
		t.Fatalf("sync upsert after delete failed: %v", err)
	}
	if dto3.Name != "Sync User 1 Restored" {
		t.Fatalf("restored name mismatch: %s", dto3.Name)
	}
}

func TestPersonServiceBatchDelete(t *testing.T) {
	db := newPersonServiceTestDB(t)
	repo := repository.NewPersonRepository(db)
	svc := service.NewPersonService(repo)
	ctx := context.Background()

	invalidCases := []struct {
		name  string
		input *service.BatchDeletePersonInput
	}{
		{name: "nil input", input: nil},
		{name: "empty list", input: &service.BatchDeletePersonInput{PersonIDs: []string{}}},
		{name: "empty id", input: &service.BatchDeletePersonInput{PersonIDs: []string{"   "}}},
		{name: "illegal slash", input: &service.BatchDeletePersonInput{PersonIDs: []string{"bad/id"}}},
		{name: "illegal space", input: &service.BatchDeletePersonInput{PersonIDs: []string{"bad id"}}},
		{name: "invalid first character", input: &service.BatchDeletePersonInput{PersonIDs: []string{"_bad"}}},
		{name: "non ASCII", input: &service.BatchDeletePersonInput{PersonIDs: []string{"é"}}},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.BatchDeletePerson(ctx, tc.input); !isErrCode(err, errno.CodeInvalidParam) {
				t.Fatalf("expected CodeInvalidParam, got %v", err)
			}
		})
	}

	// Exactly 100 valid identifiers is within the API contract.
	validIDs := make([]string, maxBatchDeleteForTest)
	for i := range validIDs {
		validIDs[i] = fmt.Sprintf("p%d", i)
	}
	if err := svc.BatchDeletePerson(ctx, &service.BatchDeletePersonInput{PersonIDs: validIDs}); err != nil {
		t.Fatalf("100 valid IDs should be accepted: %v", err)
	}

	// More than 100 identifiers is invalid.
	tooManyIDs := make([]string, maxBatchDeleteForTest+1)
	for i := range tooManyIDs {
		tooManyIDs[i] = fmt.Sprintf("p%d", i)
	}
	if err := svc.BatchDeletePerson(ctx, &service.BatchDeletePersonInput{PersonIDs: tooManyIDs}); !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for >100 batch, got %v", err)
	}
}

func isErrCode(err error, code int) bool {
	if err == nil {
		return false
	}
	var e *errno.Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}
