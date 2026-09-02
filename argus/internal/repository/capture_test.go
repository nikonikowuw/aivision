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

func newCaptureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.CaptureRecord{}); err != nil {
		t.Fatalf("migrate captures: %v", err)
	}
	return db
}

func TestCaptureRepository_EventQueriesAndRetention(t *testing.T) {
	db := newCaptureTestDB(t)
	repo := repository.NewCaptureRepository(db)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)

	records := []*model.CaptureRecord{
		{
			EventID:          "capture-person-old",
			TargetType:       model.CaptureTargetPerson,
			CameraID:         "cam-1",
			CameraName:       "East Gate",
			TrackID:          10,
			Confidence:       0.81,
			QualityScore:     0.62,
			IsRecognized:     false,
			AttributesJSON:   model.JSONRaw(`{"upper_color":"black"}`),
			ImageID:          "pano-person-old",
			ImageRelPath:     "captures/person-old.jpg",
			CropImageID:      "crop-person-old",
			CropImageRelPath: "captures/person-old-crop.jpg",
			CapturedAt:       now.Add(-30 * time.Minute),
		},
		{
			EventID:        "capture-vehicle-known",
			TargetType:     model.CaptureTargetVehicle,
			CameraID:       "cam-2",
			CameraName:     "South Gate",
			TrackID:        20,
			Confidence:     0.93,
			QualityScore:   0.91,
			IsRecognized:   true,
			AttributesJSON: model.JSONRaw(`{"plate_number":"京A88888"}`),
			ImageID:        "pano-vehicle-known",
			CropImageID:    "crop-vehicle-known",
			SubCropImageID: "plate-vehicle-known",
			CapturedAt:     now.Add(-20 * time.Minute),
		},
		{
			EventID:      "capture-face-new",
			TargetType:   model.CaptureTargetFace,
			CameraID:     "cam-1",
			CameraName:   "East Gate",
			TrackID:      30,
			Confidence:   0.88,
			QualityScore: 0.77,
			IsRecognized: false,
			CapturedAt:   now.Add(-10 * time.Minute),
		},
	}
	for _, record := range records {
		if err := repo.Create(ctx, record); err != nil {
			t.Fatalf("create %s: %v", record.EventID, err)
		}
	}

	got, err := repo.GetByEventID(ctx, "capture-vehicle-known")
	if err != nil {
		t.Fatalf("get by event id: %v", err)
	}
	if got.TargetType != model.CaptureTargetVehicle || got.TrackID != 20 {
		t.Fatalf("event record = %+v", got)
	}
	if _, err := repo.GetByEventID(ctx, "missing"); err != repository.ErrNotFound {
		t.Fatalf("missing event error = %v, want ErrNotFound", err)
	}

	recognized := true
	items, total, err := repo.ListPage(ctx, &repository.CaptureFilter{
		Page: 1, PageSize: 10, TargetType: model.CaptureTargetVehicle,
		Keyword: "京A88888", IsRecognized: &recognized,
	})
	if err != nil {
		t.Fatalf("list filtered captures: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].EventID != "capture-vehicle-known" {
		t.Fatalf("filtered captures total=%d items=%+v", total, items)
	}

	expired, err := repo.FindExpired(ctx, now.Add(-25*time.Minute), 10)
	if err != nil {
		t.Fatalf("find expired captures: %v", err)
	}
	if len(expired) != 1 || expired[0].EventID != "capture-person-old" {
		t.Fatalf("expired captures = %+v, want person-old", expired)
	}

	unrecognized, err := repo.FindOldestUnrecognized(ctx, 10)
	if err != nil {
		t.Fatalf("find oldest unrecognized captures: %v", err)
	}
	if len(unrecognized) != 2 || unrecognized[0].EventID != "capture-person-old" || unrecognized[1].EventID != "capture-face-new" {
		t.Fatalf("oldest unrecognized = %+v", unrecognized)
	}

	oldest, err := repo.FindOldest(ctx, 2)
	if err != nil {
		t.Fatalf("find oldest captures: %v", err)
	}
	if len(oldest) != 2 || oldest[0].EventID != "capture-person-old" || oldest[1].EventID != "capture-vehicle-known" {
		t.Fatalf("oldest captures = %+v", oldest)
	}

	existing, err := repo.FindExistingImageIDs(ctx, []string{
		"pano-person-old", "crop-person-old", "pano-vehicle-known", "crop-vehicle-known", "plate-vehicle-known", "missing",
	})
	if err != nil {
		t.Fatalf("find existing image ids: %v", err)
	}
	found := make(map[string]bool, len(existing))
	for _, id := range existing {
		found[id] = true
	}
	for _, id := range []string{"pano-person-old", "crop-person-old", "pano-vehicle-known", "crop-vehicle-known", "plate-vehicle-known"} {
		if !found[id] {
			t.Errorf("existing image id %q missing from %v", id, existing)
		}
	}
	if found["missing"] {
		t.Errorf("non-existent image id returned: %v", existing)
	}

	if err := repo.HardDeleteBatch(ctx, []uint64{records[0].ID}); err != nil {
		t.Fatalf("hard delete capture: %v", err)
	}
	count, err := repo.CountTotal(ctx)
	if err != nil {
		t.Fatalf("count captures after delete: %v", err)
	}
	if count != 2 {
		t.Fatalf("capture count after delete = %d, want 2", count)
	}
}

func TestCaptureRepository_CreateRejectsNil(t *testing.T) {
	repo := repository.NewCaptureRepository(newCaptureTestDB(t))
	if err := repo.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) unexpectedly succeeded")
	}
}
