package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/repository"
)

func newFaceCaptureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.FaceCapture{}); err != nil {
		t.Fatalf("migrate face_captures: %v", err)
	}
	return db
}

func TestFaceCaptureRepository_UpsertIncremental(t *testing.T) {
	db := newFaceCaptureTestDB(t)
	repo := repository.NewFaceCaptureRepository(db)
	ctx := context.Background()

	t0 := time.Now().Add(-10 * time.Second).Truncate(time.Millisecond)
	t1 := time.Now().Add(-8 * time.Second).Truncate(time.Millisecond)
	t2 := time.Now().Add(-6 * time.Second).Truncate(time.Millisecond)

	capture := &model.FaceCapture{
		EventID:          "run-1/track-100",
		InstanceID:       "inst-1",
		CameraID:         "cam-1",
		CameraName:       "Gate 1",
		AlgorithmID:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		TrackID:          100,
	}

	snap1 := &model.SnapshotItem{
		WallTimeNs:       t0.UnixNano(),
		ObservedAt:       t0,
		ImageID:          "img-1",
		ImageRelPath:     "captures/pano-1.jpg",
		FaceImageID:      "face-1",
		FaceImageRelPath: "captures/face-1.jpg",
		QualityScore:     0.75,
		Similarity:       0.60,
		PersonID:         "",
		PersonName:       "",
	}

	// 1. 首次上报
	if err := repo.UpsertIncremental(ctx, capture, snap1); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	got, err := repo.GetByEventID(ctx, "run-1/track-100")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if got.SnapshotCount != 1 {
		t.Errorf("snapshot count = %d, want 1", got.SnapshotCount)
	}
	if got.BestSimilarity != 0.60 || got.BestQualityScore != 0.75 {
		t.Errorf("best sim = %f, quality = %f", got.BestSimilarity, got.BestQualityScore)
	}

	// 2. 第二次上报：更高相似度 (0.92, 命中张三)
	snap2 := &model.SnapshotItem{
		WallTimeNs:       t1.UnixNano(),
		ObservedAt:       t1,
		ImageID:          "img-2",
		ImageRelPath:     "captures/pano-2.jpg",
		FaceImageID:      "face-2",
		FaceImageRelPath: "captures/face-2.jpg",
		QualityScore:     0.90,
		Similarity:       0.92,
		PersonID:         "p-1",
		PersonName:       "张三",
	}
	if err := repo.UpsertIncremental(ctx, capture, snap2); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	got2, err := repo.GetByEventID(ctx, "run-1/track-100")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if got2.SnapshotCount != 2 {
		t.Errorf("snapshot count = %d, want 2", got2.SnapshotCount)
	}
	if got2.BestSimilarity != 0.92 || got2.BestPersonName != "张三" {
		t.Errorf("best sim = %f, person = %s", got2.BestSimilarity, got2.BestPersonName)
	}

	// 3. 第三次上报：较低相似度 (0.80) -> 快照数增至 3，但 best 保持张三和 0.92
	snap3 := &model.SnapshotItem{
		WallTimeNs:       t2.UnixNano(),
		ObservedAt:       t2,
		ImageID:          "img-3",
		ImageRelPath:     "captures/pano-3.jpg",
		FaceImageID:      "face-3",
		FaceImageRelPath: "captures/face-3.jpg",
		QualityScore:     0.82,
		Similarity:       0.80,
		PersonID:         "p-1",
		PersonName:       "张三",
	}
	if err := repo.UpsertIncremental(ctx, capture, snap3); err != nil {
		t.Fatalf("third upsert failed: %v", err)
	}

	got3, err := repo.GetByEventID(ctx, "run-1/track-100")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if got3.SnapshotCount != 3 {
		t.Errorf("snapshot count = %d, want 3", got3.SnapshotCount)
	}
	if got3.BestSimilarity != 0.92 {
		t.Errorf("best sim = %f, want maintained at 0.92", got3.BestSimilarity)
	}

	snapshots, err := got3.ParseSnapshots()
	if err != nil {
		t.Fatalf("parse snapshots: %v", err)
	}
	if len(snapshots) != 3 {
		t.Errorf("parsed snapshots len = %d, want 3", len(snapshots))
	}

	// 4. 孤儿图片对账（必须能查出 snapshots_json 中的历史快照图片 img-1, face-1, img-2, face-2）
	existingIDs, err := repo.FindExistingImageIDs(ctx, []string{"img-1", "face-1", "img-2", "face-2", "img-3", "face-3", "img-non-existent"})
	if err != nil {
		t.Fatalf("find existing image ids: %v", err)
	}
	if len(existingIDs) < 6 {
		t.Errorf("found image ids count = %d, want >= 6 (got %v)", len(existingIDs), existingIDs)
	}
}

func TestFaceCaptureRepository_ListPage(t *testing.T) {
	db := newFaceCaptureTestDB(t)
	repo := repository.NewFaceCaptureRepository(db)
	ctx := context.Background()

	now := time.Now()
	// 记录 1: 陌生人
	if err := repo.Create(ctx, &model.FaceCapture{
		EventID:         "run-1/track-1",
		CameraID:        "cam-1",
		CameraName:      "Camera 1",
		BestSimilarity:  0.45,
		BestPersonID:    "",
		FirstObservedAt: now.Add(-5 * time.Minute),
		LastObservedAt:  now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("create record 1: %v", err)
	}
	// 记录 2: 已知人员
	if err := repo.Create(ctx, &model.FaceCapture{
		EventID:         "run-1/track-2",
		CameraID:        "cam-1",
		CameraName:      "Camera 1",
		BestSimilarity:  0.88,
		BestPersonID:    "p-1",
		BestPersonName:  "李四",
		FirstObservedAt: now.Add(-2 * time.Minute),
		LastObservedAt:  now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("create record 2: %v", err)
	}

	// 1. 查询全部
	records, total, err := repo.ListPage(ctx, &repository.FaceCaptureFilter{Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(records) != 2 {
		t.Fatalf("list all: err=%v, total=%d, len=%d", err, total, len(records))
	}
	for i, r := range records {
		t.Logf("Row %d: ID=%d, EventID=%s, BestPersonID=%q, BestPersonName=%q", i, r.ID, r.EventID, r.BestPersonID, r.BestPersonName)
	}

	// 2. 筛选陌生人
	records, total, err = repo.ListPage(ctx, &repository.FaceCaptureFilter{Status: "stranger"})
	if err != nil || total != 1 || records[0].EventID != "run-1/track-1" {
		t.Fatalf("list strangers: err=%v, total=%d", err, total)
	}

	// 3. 筛选已识别
	records, total, err = repo.ListPage(ctx, &repository.FaceCaptureFilter{Status: "recognized"})
	if err != nil || total != 1 || len(records) != 1 {
		t.Fatalf("list recognized: total=%d, len(records)=%d", total, len(records))
	}
	if records[0].BestPersonName != "李四" {
		t.Fatalf("list recognized: personName=%s, want 李四", records[0].BestPersonName)
	}

	// 4. 按 TrackID 筛选
	records, total, err = repo.ListPage(ctx, &repository.FaceCaptureFilter{TrackID: 100})
	if err != nil {
		t.Fatalf("list by track id: %v", err)
	}
}
