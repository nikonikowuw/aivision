package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

func newCameraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Camera{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func newCameraFixture(id int, name string) *model.Camera {
	return &model.Camera{
		CameraID:        fmt.Sprintf("camera-uuid-%d", id),
		Protocol:        model.CameraProtocolRTSP,
		Name:            name,
		RtspURL:         fmt.Sprintf("rtsp://192.168.1.%d/live", id),
		TransportPolicy: model.CameraTransportAuto,
		ConfigHash:      fmt.Sprintf("hash-%d", id),
	}
}

func TestCameraRepositoryCreateGetAndUpdate(t *testing.T) {
	db := newCameraTestDB(t)
	repo := NewCameraRepository(db)
	ctx := context.Background()

	cam := newCameraFixture(1, "门口")
	if err := repo.Create(ctx, cam); err != nil {
		t.Fatalf("create: %v", err)
	}
	if cam.ID == 0 {
		t.Fatal("create: id not assigned")
	}

	got, err := repo.GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.CameraID != cam.CameraID || got.Name != "门口" || got.RtspURL != cam.RtspURL {
		t.Fatalf("get by id = %+v, want %+v", got, cam)
	}

	got.Name = "东门"
	got.RtspURL = "rtsp://192.168.1.2/live2"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := repo.GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if after.Name != "东门" || after.RtspURL != "rtsp://192.168.1.2/live2" {
		t.Fatalf("after update = %+v", after)
	}

	byUUID, err := repo.GetByCameraID(ctx, cam.CameraID)
	if err != nil {
		t.Fatalf("get by camera id: %v", err)
	}
	if byUUID.ID != cam.ID {
		t.Fatalf("get by camera id = %+v, want id %d", byUUID, cam.ID)
	}
}

func TestCameraRepositoryDeleteExcludesFromQueries(t *testing.T) {
	db := newCameraTestDB(t)
	repo := NewCameraRepository(db)
	ctx := context.Background()

	cam := newCameraFixture(2, "车库")
	if err := repo.Create(ctx, cam); err != nil {
		t.Fatalf("create: %v", err)
	}

	deleted, err := repo.Delete(ctx, cam.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete = false, want true")
	}
	// 重复删除返回 false（已软删）
	deleted, err = repo.Delete(ctx, cam.ID)
	if err != nil {
		t.Fatalf("delete again: %v", err)
	}
	if deleted {
		t.Fatal("delete again = true, want false")
	}

	if _, err := repo.GetByID(ctx, cam.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByCameraID(ctx, cam.CameraID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get by camera id after delete error = %v, want ErrNotFound", err)
	}
}

func TestCameraRepositoryBatchDelete(t *testing.T) {
	db := newCameraTestDB(t)
	repo := NewCameraRepository(db)
	ctx := context.Background()

	ids := make([]uint64, 0, 3)
	for i := 1; i <= 3; i++ {
		cam := newCameraFixture(i, fmt.Sprintf("cam-%d", i))
		if err := repo.Create(ctx, cam); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, cam.ID)
	}

	if err := repo.BatchDelete(ctx, ids[:2]); err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	for _, id := range ids[:2] {
		if _, err := repo.GetByID(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get %d after batch delete error = %v, want ErrNotFound", id, err)
		}
	}
	// 未删除的仍可查询
	if _, err := repo.GetByID(ctx, ids[2]); err != nil {
		t.Fatalf("get surviving camera: %v", err)
	}
}

func TestCameraRepositoryListPageWithNameFilter(t *testing.T) {
	db := newCameraTestDB(t)
	repo := NewCameraRepository(db)
	ctx := context.Background()

	names := []string{"门口", "东门", "车库", "走廊"}
	for i, name := range names {
		cam := newCameraFixture(i+1, name)
		if err := repo.Create(ctx, cam); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	// 全量分页（每页 2 条）
	items, total, err := repo.ListPage(ctx, &CameraFilter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 4 || len(items) != 2 {
		t.Fatalf("page1 total=%d len=%d, want 4/2", total, len(items))
	}
	// 按 id 倒序：第 1 页应为 走廊、车库
	if items[0].Name != "走廊" || items[1].Name != "车库" {
		t.Fatalf("page1 items = %+v", items)
	}

	items, total, err = repo.ListPage(ctx, &CameraFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(items) != 2 || items[0].Name != "东门" || items[1].Name != "门口" {
		t.Fatalf("page2 items = %+v", items)
	}

	// name 模糊过滤
	items, total, err = repo.ListPage(ctx, &CameraFilter{Page: 1, PageSize: 10, Name: "门"})
	if err != nil {
		t.Fatalf("list page name filter: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("name filter total=%d len=%d, want 2/2", total, len(items))
	}

	// 软删后列表不再包含
	if _, err := repo.Delete(ctx, items[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, total, err = repo.ListPage(ctx, &CameraFilter{Page: 1, PageSize: 10, Name: "门"})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("after delete total=%d len=%d, want 1/1", total, len(items))
	}
}
