package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/repository"
)

func newAlarmRecordTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AlarmRecord{}); err != nil {
		t.Fatalf("migrate alarm_records: %v", err)
	}
	return db
}

func TestAlarmRecordRepository_CreateAndGet(t *testing.T) {
	db := newAlarmRecordTestDB(t)
	repo := repository.NewAlarmRecordRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	record := &model.AlarmRecord{
		EventID:          "inst-1-run-1/algo-event-1",
		InstanceID:       "inst-1",
		CameraID:         "cam-1",
		AlgorithmID:      "yolov8n",
		AlgorithmVersion: "1.0.0",
		AlarmTypeID:      "person_invade",
		OccurredAt:       now,
		TimeSynced:       true,
		TargetLabel:      "person",
		Confidence:       0.95,
		TrackID:          1,
		BBoxJSON:         []byte(`[0.1,0.1,0.5,0.5]`),
		ImageID:          "img-12345",
		ImageRelPath:     "2026/03/30/img-12345.jpg",
	}

	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create alarm record: %v", err)
	}

	// 1. GetByID
	got, err := repo.GetByID(ctx, record.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.EventID != record.EventID || got.CameraID != "cam-1" || got.Confidence != 0.95 {
		t.Errorf("got record = %+v, want match with created", got)
	}

	// 2. GetByEventID
	gotByEvent, err := repo.GetByEventID(ctx, "inst-1-run-1/algo-event-1")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if gotByEvent.ID != record.ID {
		t.Errorf("gotByEvent ID = %d, want %d", gotByEvent.ID, record.ID)
	}

	// 3. GetByImageID
	gotByImage, err := repo.GetByImageID(ctx, "img-12345")
	if err != nil {
		t.Fatalf("get by image id: %v", err)
	}
	if gotByImage.ID != record.ID {
		t.Errorf("gotByImage ID = %d, want %d", gotByImage.ID, record.ID)
	}

	// 4. 重复 EventID 触发 ErrDuplicateKey
	dupRecord := &model.AlarmRecord{
		EventID:          "inst-1-run-1/algo-event-1",
		InstanceID:       "inst-1",
		CameraID:         "cam-1",
		AlgorithmID:      "yolov8n",
		AlgorithmVersion: "1.0.0",
		AlarmTypeID:      "person_invade",
		OccurredAt:       now,
		ImageID:          "img-99999",
	}
	err = repo.Create(ctx, dupRecord)
	if err != repository.ErrDuplicateKey {
		t.Errorf("create duplicate event_id error = %v, want ErrDuplicateKey", err)
	}
}

func TestAlarmRecordRepository_ListPageAndFilters(t *testing.T) {
	db := newAlarmRecordTestDB(t)
	repo := repository.NewAlarmRecordRepository(db)
	ctx := context.Background()

	t1 := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 30, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)

	if err := repo.Create(ctx, &model.AlarmRecord{
		EventID: "e1", InstanceID: "i1", CameraID: "cam-1", AlgorithmID: "yolo", AlarmTypeID: "invade",
		OccurredAt: t1, Confidence: 0.70, ImageID: "img-1",
	}); err != nil {
		t.Fatalf("create e1: %v", err)
	}
	if err := repo.Create(ctx, &model.AlarmRecord{
		EventID: "e2", InstanceID: "i1", CameraID: "cam-1", AlgorithmID: "yolo", AlarmTypeID: "invade",
		OccurredAt: t2, Confidence: 0.85, ImageID: "img-2",
	}); err != nil {
		t.Fatalf("create e2: %v", err)
	}
	if err := repo.Create(ctx, &model.AlarmRecord{
		EventID: "e3", InstanceID: "i2", CameraID: "cam-2", AlgorithmID: "fire", AlarmTypeID: "smoke",
		OccurredAt: t3, Confidence: 0.95, ImageID: "img-3",
	}); err != nil {
		t.Fatalf("create e3: %v", err)
	}

	// 1. 无条件查询全量
	items, total, err := repo.ListPage(ctx, nil)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("list all: err=%v, total=%d, len=%d", err, total, len(items))
	}
	// 倒序检查
	if items[0].EventID != "e3" || items[1].EventID != "e2" || items[2].EventID != "e1" {
		t.Errorf("items order mismatch: got [%s, %s, %s]", items[0].EventID, items[1].EventID, items[2].EventID)
	}

	// 2. 筛选 camera_id
	items, total, err = repo.ListPage(ctx, &repository.AlarmRecordFilter{CameraID: "cam-1"})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("filter camera: err=%v, total=%d", err, total)
	}

	// 3. 筛选置信度区间 [0.80, 0.90]
	minConf := float32(0.80)
	maxConf := float32(0.90)
	items, total, err = repo.ListPage(ctx, &repository.AlarmRecordFilter{
		MinConfidence: &minConf,
		MaxConfidence: &maxConf,
	})
	if err != nil || total != 1 || items[0].EventID != "e2" {
		t.Fatalf("filter confidence: total=%d, err=%v", total, err)
	}

	// 4. 筛选时间区间 [t1+30m, t3]
	start := t1.Add(30 * time.Minute)
	items, total, err = repo.ListPage(ctx, &repository.AlarmRecordFilter{
		StartTime: &start,
		EndTime:   &t3,
	})
	if err != nil || total != 2 {
		t.Fatalf("filter time: total=%d, err=%v", total, err)
	}

	// 5. 批量查询已存在的 image_id
	existing, err := repo.FindExistingImageIDs(ctx, []string{"img-1", "img-3", "img-unknown"})
	if err != nil {
		t.Fatalf("find existing images: %v", err)
	}
	if len(existing) != 2 {
		t.Fatalf("existing len = %d, want 2", len(existing))
	}
}
