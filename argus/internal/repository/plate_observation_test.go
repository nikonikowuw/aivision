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

func newPlateObservationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.PlateObservation{}); err != nil {
		t.Fatalf("migrate plate_observations: %v", err)
	}
	return db
}

func TestPlateObservationRepository_CreateAndGet(t *testing.T) {
	db := newPlateObservationTestDB(t)
	repo := repository.NewPlateObservationRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	record := &model.PlateObservation{
		EventID:         "inst-1-run-1/obs-1",
		TaskID:          "task-1",
		InstanceID:      "inst-1",
		CameraID:        "cam-1",
		CameraName:      "East Gate",
		PlateText:       "粤B12345",
		NormalizedText:  "粤B12345",
		PlateColor:      "blue",
		PlateType:       "standard",
		Confidence:      0.96,
		OcrConfidence:   0.93,
		TrackID:         42,
		BBoxJSON:        []byte(`{"x_min":0.3,"y_min":0.4,"x_max":0.5,"y_max":0.6}`),
		VehicleBBoxJSON: []byte(`{"x_min":0.1,"y_min":0.2,"x_max":0.7,"y_max":0.8}`),
		PanoramaImage:   "2026/08/31/pano-1.jpg",
		PlateImage:      "2026/08/31/plate-1.jpg",
		ObservedAt:      now,
	}

	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create plate observation: %v", err)
	}

	// 1. GetByID
	got, err := repo.GetByID(ctx, record.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.EventID != record.EventID || got.PlateText != "粤B12345" || got.PlateColor != "blue" {
		t.Errorf("got record = %+v, want match with created", got)
	}

	// 2. GetByEventID
	gotByEvent, err := repo.GetByEventID(ctx, "inst-1-run-1/obs-1")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if gotByEvent.ID != record.ID {
		t.Errorf("got id = %d, want %d", gotByEvent.ID, record.ID)
	}

	// 3. Duplicate EventID should fail with ErrDuplicateKey
	dupRecord := &model.PlateObservation{
		EventID:        "inst-1-run-1/obs-1",
		CameraID:       "cam-1",
		PlateText:      "粤B12345",
		NormalizedText: "粤B12345",
		ObservedAt:     now,
	}
	if err := repo.Create(ctx, dupRecord); err == nil {
		t.Fatalf("expected duplicate key error, got nil")
	}
}

func TestPlateObservationRepository_ListPage(t *testing.T) {
	db := newPlateObservationTestDB(t)
	repo := repository.NewPlateObservationRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		color := "blue"
		plate := fmt.Sprintf("粤B0000%d", i)
		if i == 5 {
			color = "green"
			plate = "粤BD12345"
		}
		_ = repo.Create(ctx, &model.PlateObservation{
			EventID:        fmt.Sprintf("evt-%d", i),
			CameraID:       fmt.Sprintf("cam-%d", (i-1)%2+1),
			PlateText:      plate,
			NormalizedText: plate,
			PlateColor:     color,
			PlateType:      "standard",
			Confidence:     0.90 + float32(i)*0.01,
			ObservedAt:     t0.Add(time.Duration(i) * time.Minute),
		})
	}

	// Filter by PlateText
	items, total, err := repo.ListPage(ctx, &repository.PlateObservationFilter{
		PlateText: "BD12345",
	})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].PlateText != "粤BD12345" {
		t.Errorf("expected 1 item with plate 粤BD12345, got %d (total=%d)", len(items), total)
	}

	// Filter by PlateColor
	items, total, err = repo.ListPage(ctx, &repository.PlateObservationFilter{
		PlateColor: "green",
	})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || items[0].PlateColor != "green" {
		t.Errorf("expected 1 green plate, got %d", total)
	}

	// Filter by Camera
	items, total, err = repo.ListPage(ctx, &repository.PlateObservationFilter{
		CameraID: "cam-1",
	})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 items for cam-1, got %d", total)
	}
}
