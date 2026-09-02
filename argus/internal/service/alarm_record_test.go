package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func newTestAlarmDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Camera{},
		&model.Algorithm{},
		&model.AlgorithmVersion{},
		&model.AnalysisTask{},
		&model.AlgorithmInstance{},
		&model.DesiredStateRevision{},
		&model.AlarmRecord{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestReportAdapter_AcceptAlarmAndIdempotency(t *testing.T) {
	db := newTestAlarmDB(t)
	taskRepo := repository.NewTaskRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	adapter := service.NewReportAdapterWithAlarm(taskRepo, alarmRepo, nil, nil, zap.NewNop())
	ctx := context.Background()

	event := &argusv1.AlarmEvent{
		EventId:          "run-1/evt-1",
		InstanceId:       "inst-1",
		CameraId:         "cam-1",
		AlgorithmId:      "yolov8n",
		AlgorithmVersion: "1.0.0",
		AlarmTypeId:      "person_invade",
		WallTimeNs:       time.Now().UnixNano(),
		TimeSynced:       true,
		Objects: []*argusv1.DetectedObject{
			{
				Label:      "person",
				Confidence: 0.88,
				Bbox: &argusv1.BoundingBox{
					XMin: 0.1,
					YMin: 0.1,
					XMax: 0.4,
					YMax: 0.4,
				},
				TrackId: 101,
			},
		},
		ImageId:      "img-101",
		ImageRelPath: "2026/03/30/img-101.jpg",
	}

	// 1. 首次上报落库成功
	if err := adapter.AcceptAlarm(ctx, event); err != nil {
		t.Fatalf("first accept alarm: %v", err)
	}

	record, err := alarmRepo.GetByEventID(ctx, "run-1/evt-1")
	if err != nil {
		t.Fatalf("get alarm record: %v", err)
	}
	if record.Confidence != 0.88 || record.ImageID != "img-101" || record.TargetLabel != "person" {
		t.Errorf("unexpected record data: %+v", record)
	}

	// 2. 二次重复上报相同 event_id 应当幂等成功（不报错）
	if err := adapter.AcceptAlarm(ctx, event); err != nil {
		t.Fatalf("second accept duplicate alarm: %v", err)
	}
}

func TestReportAdapter_ReconcileOrphanImages(t *testing.T) {
	db := newTestAlarmDB(t)
	taskRepo := repository.NewTaskRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	adapter := service.NewReportAdapterWithAlarm(taskRepo, alarmRepo, nil, nil, zap.NewNop())
	ctx := context.Background()

	// 插入一条已持久化的告警记录
	_ = alarmRepo.Create(ctx, &model.AlarmRecord{
		EventID: "evt-existing",
		ImageID: "img-reconciled",
	})

	now := time.Now()
	entries := []*argusv1.OrphanImageEntry{
		{
			ImageId:      "img-reconciled",
			CreatedAtNs:  now.Add(-10 * time.Minute).UnixNano(),
			RelativePath: "img-reconciled.jpg",
		},
		{
			// 超期未落库，应进入 delete 列表
			ImageId:      "img-old-orphan",
			CreatedAtNs:  now.Add(-10 * time.Minute).UnixNano(),
			RelativePath: "img-old-orphan.jpg",
		},
		{
			// 未落库但在 5 分钟保护期内，不应进入 delete
			ImageId:      "img-new-orphan",
			CreatedAtNs:  now.Add(-1 * time.Minute).UnixNano(),
			RelativePath: "img-new-orphan.jpg",
		},
	}

	disposition, err := adapter.ReconcileOrphanImages(ctx, entries)
	if err != nil {
		t.Fatalf("reconcile orphan images: %v", err)
	}

	if len(disposition.RetainImageIDs) != 1 || disposition.RetainImageIDs[0] != "img-reconciled" {
		t.Errorf("retain image IDs = %v, want ['img-reconciled']", disposition.RetainImageIDs)
	}
	if len(disposition.DeleteImageIDs) != 1 || disposition.DeleteImageIDs[0] != "img-old-orphan" {
		t.Errorf("delete image IDs = %v, want ['img-old-orphan']", disposition.DeleteImageIDs)
	}
}
