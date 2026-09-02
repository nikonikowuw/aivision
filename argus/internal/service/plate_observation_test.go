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

func newTestPlateDB(t *testing.T) *gorm.DB {
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
		&model.PlateObservation{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestReportAdapter_AcceptPlateObservationAndIdempotency(t *testing.T) {
	db := newTestPlateDB(t)
	taskRepo := repository.NewTaskRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	adapter := service.NewReportAdapterWithAlarm(taskRepo, alarmRepo, plateRepo, nil, zap.NewNop())
	ctx := context.Background()

	obs := &argusv1.PlateObservation{
		EventId:          "inst-1-run-1/obs-1",
		InstanceId:       "inst-1",
		CameraId:         "cam-1",
		AlgorithmId:      "license_plate_recognition",
		AlgorithmVersion: "1.2.3",
		PlateText:        "粤B99999",
		NormalizedText:   "粤B99999",
		PlateColor:       "blue",
		PlateType:        "standard",
		Confidence:       0.95,
		OcrConfidence:    0.92,
		TrackId:          77,
		PlateBbox: &argusv1.BoundingBox{
			XMin: 0.2, YMin: 0.3, XMax: 0.4, YMax: 0.5,
		},
		VehicleBbox: &argusv1.BoundingBox{
			XMin: 0.1, YMin: 0.1, XMax: 0.8, YMax: 0.9,
		},
		ImageId:           "img-pano-1",
		ImageRelPath:      "2026/08/31/pano-1.jpg",
		PlateImageId:      "img-plate-1",
		PlateImageRelPath: "2026/08/31/plate-1.jpg",
		WallTimeNs:        time.Now().UnixNano(),
		TimeSynced:        true,
	}

	// 1. AcceptPlateObservation
	if err := adapter.AcceptPlateObservation(ctx, obs); err != nil {
		t.Fatalf("accept plate observation: %v", err)
	}

	// Verify persistence in DB
	record, err := plateRepo.GetByEventID(ctx, "inst-1-run-1/obs-1")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if record.PlateText != "粤B99999" || record.Confidence != 0.95 ||
		record.AlgorithmID != "license_plate_recognition" || record.AlgorithmVersion != "1.2.3" ||
		!record.TimeSynced || record.ImageID != "img-pano-1" ||
		record.ImageRelPath != "2026/08/31/pano-1.jpg" || record.PlateImageID != "img-plate-1" ||
		record.PlateImageRelPath != "2026/08/31/plate-1.jpg" {
		t.Errorf("unexpected record data: %+v", record)
	}

	// 2. Idempotency on second delivery with same event_id
	if err := adapter.AcceptPlateObservation(ctx, obs); err != nil {
		t.Fatalf("second accept should succeed idempotently: %v", err)
	}
}

func TestReportAdapter_ReconcileOrphanImagesIncludesPlateReferences(t *testing.T) {
	db := newTestPlateDB(t)
	taskRepo := repository.NewTaskRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	adapter := service.NewReportAdapterWithAlarm(taskRepo, nil, plateRepo, nil, zap.NewNop())
	ctx := context.Background()

	if err := plateRepo.Create(ctx, &model.PlateObservation{
		EventID:        "plate-event-1",
		ImageID:        "img-plate-pano",
		PlateImageID:   "img-plate-crop",
		PlateText:      "粤B99999",
		NormalizedText: "粤B99999",
		ObservedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("create plate observation: %v", err)
	}

	now := time.Now()
	disposition, err := adapter.ReconcileOrphanImages(ctx, []*argusv1.OrphanImageEntry{
		{ImageId: "img-plate-pano", CreatedAtNs: now.Add(-10 * time.Minute).UnixNano()},
		{ImageId: "img-plate-crop", CreatedAtNs: now.Add(-10 * time.Minute).UnixNano()},
		{ImageId: "img-unreferenced", CreatedAtNs: now.Add(-10 * time.Minute).UnixNano()},
	})
	if err != nil {
		t.Fatalf("reconcile plate orphan images: %v", err)
	}

	retained := map[string]bool{}
	for _, id := range disposition.RetainImageIDs {
		retained[id] = true
	}
	if !retained["img-plate-pano"] || !retained["img-plate-crop"] || len(retained) != 2 {
		t.Errorf("retained plate image IDs = %v", disposition.RetainImageIDs)
	}
	if len(disposition.DeleteImageIDs) != 1 || disposition.DeleteImageIDs[0] != "img-unreferenced" {
		t.Errorf("deleted plate image IDs = %v", disposition.DeleteImageIDs)
	}
}

func TestPlateObservationService_ListPageAndDetail(t *testing.T) {
	db := newTestPlateDB(t)
	plateRepo := repository.NewPlateObservationRepository(db)
	camRepo := repository.NewCameraRepository(db)
	svc := service.NewPlateObservationService(plateRepo, camRepo, nil)
	ctx := context.Background()

	// Seed Camera
	_ = camRepo.Create(ctx, &model.Camera{
		CameraID: "cam-1",
		Name:     "West Gate HD",
		RtspURL:  "rtsp://localhost/test",
	})

	// Seed Observation
	_ = plateRepo.Create(ctx, &model.PlateObservation{
		EventID:           "evt-100",
		CameraID:          "cam-1",
		AlgorithmID:       "license_plate_recognition",
		AlgorithmVersion:  "1.0.0",
		TimeSynced:        true,
		PlateText:         "京A88888",
		NormalizedText:    "京A88888",
		PlateColor:        "yellow",
		PlateType:         "double_yellow",
		Confidence:        0.98,
		OcrConfidence:     0.95,
		PanoramaImage:     "2026/08/31/pano.jpg",
		PlateImage:        "2026/08/31/plate.jpg",
		ImageID:           "img-pano-100",
		ImageRelPath:      "2026/08/31/pano.jpg",
		PlateImageID:      "img-plate-100",
		PlateImageRelPath: "2026/08/31/plate.jpg",
		ObservedAt:        time.Now(),
	})

	// List
	res, err := svc.ListPage(ctx, &service.PlateObservationQuery{
		PlateText: "A88888",
	})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", res.Total)
	}
	if res.Items[0].CameraName != "West Gate HD" {
		t.Errorf("expected CameraName 'West Gate HD', got %s", res.Items[0].CameraName)
	}
	if res.Items[0].AlgorithmID != "license_plate_recognition" ||
		res.Items[0].AlgorithmVersion != "1.0.0" || !res.Items[0].TimeSynced ||
		res.Items[0].ImageID != "img-pano-100" || res.Items[0].PlateImageID != "img-plate-100" {
		t.Errorf("unexpected provenance/image fields: %+v", res.Items[0])
	}
	if res.Items[0].PanoramaImageURL != fmt.Sprintf("/api/v1/plate-observations/%d/panorama", res.Items[0].ID) {
		t.Errorf("unexpected panorama URL: %s", res.Items[0].PanoramaImageURL)
	}

	// Detail
	detail, err := svc.GetDetail(ctx, res.Items[0].ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail.PlateText != "京A88888" || detail.PlateColor != "yellow" ||
		detail.AlgorithmVersion != "1.0.0" || !detail.TimeSynced {
		t.Errorf("unexpected detail: %+v", detail)
	}
}
