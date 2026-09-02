package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/repository"
)

func newStorageRetentionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&model.AlarmRecord{},
		&model.PlateObservation{},
		&model.FaceObservation{},
		&model.FaceCapture{},
		&model.OperationLog{},
		&model.Person{},
		&model.PersonFace{},
	)
	require.NoError(t, err)
	return db
}

func TestAlarmRecordRepository_RetentionMethods(t *testing.T) {
	db := newStorageRetentionTestDB(t)
	repo := repository.NewAlarmRecordRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	r1 := &model.AlarmRecord{
		EventID:      "ev-1",
		InstanceID:   "inst-1",
		CameraID:     "cam-1",
		OccurredAt:   now.Add(-30 * time.Minute),
		ImageID:      "img-1",
		ImageRelPath: "2026/01/img-1.jpg",
	}
	r2 := &model.AlarmRecord{
		EventID:      "ev-2",
		InstanceID:   "inst-1",
		CameraID:     "cam-1",
		OccurredAt:   now.Add(-20 * time.Minute),
		ImageID:      "img-2",
		ImageRelPath: "2026/01/img-2.jpg",
	}
	r3 := &model.AlarmRecord{
		EventID:      "ev-3",
		InstanceID:   "inst-1",
		CameraID:     "cam-1",
		OccurredAt:   now.Add(-10 * time.Minute),
		ImageID:      "img-3",
		ImageRelPath: "2026/01/img-3.jpg",
	}

	require.NoError(t, repo.Create(ctx, r1))
	require.NoError(t, repo.Create(ctx, r2))
	require.NoError(t, repo.Create(ctx, r3))

	total, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)

	// Test FindExpired
	expired, err := repo.FindExpired(ctx, now.Add(-15*time.Minute), 10)
	require.NoError(t, err)
	assert.Len(t, expired, 2)
	assert.Equal(t, "ev-1", expired[0].EventID)
	assert.Equal(t, "ev-2", expired[1].EventID)

	// Test FindOldest
	oldest, err := repo.FindOldest(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, oldest, 2)
	assert.Equal(t, "ev-1", oldest[0].EventID)
	assert.Equal(t, "ev-2", oldest[1].EventID)

	// Test HardDeleteBatch
	err = repo.HardDeleteBatch(ctx, []uint64{r1.ID, r2.ID})
	require.NoError(t, err)

	// Ensure hard deleted
	var remaining []model.AlarmRecord
	err = db.Unscoped().Find(&remaining).Error
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, r3.ID, remaining[0].ID)

	totalAfter, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalAfter)
}

func TestPlateObservationRepository_RetentionMethods(t *testing.T) {
	db := newStorageRetentionTestDB(t)
	repo := repository.NewPlateObservationRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	p1 := &model.PlateObservation{
		EventID:           "plate-1",
		PlateText:         "京A11111",
		ObservedAt:        now.Add(-30 * time.Minute),
		ImageRelPath:      "plate/img-1.jpg",
		PlateImageRelPath: "plate/crop-1.jpg",
	}
	p2 := &model.PlateObservation{
		EventID:           "plate-2",
		PlateText:         "京A22222",
		ObservedAt:        now.Add(-20 * time.Minute),
		ImageRelPath:      "plate/img-2.jpg",
		PlateImageRelPath: "plate/crop-2.jpg",
	}
	p3 := &model.PlateObservation{
		EventID:           "plate-3",
		PlateText:         "京A33333",
		ObservedAt:        now.Add(-10 * time.Minute),
		ImageRelPath:      "plate/img-3.jpg",
		PlateImageRelPath: "plate/crop-3.jpg",
	}

	require.NoError(t, repo.Create(ctx, p1))
	require.NoError(t, repo.Create(ctx, p2))
	require.NoError(t, repo.Create(ctx, p3))

	total, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)

	expired, err := repo.FindExpired(ctx, now.Add(-15*time.Minute), 10)
	require.NoError(t, err)
	assert.Len(t, expired, 2)
	assert.Equal(t, "plate-1", expired[0].EventID)
	assert.Equal(t, "plate-2", expired[1].EventID)

	oldest, err := repo.FindOldest(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, oldest, 1)
	assert.Equal(t, "plate-1", oldest[0].EventID)

	require.NoError(t, repo.HardDeleteBatch(ctx, []uint64{p1.ID}))

	var remaining []model.PlateObservation
	require.NoError(t, db.Unscoped().Find(&remaining).Error)
	assert.Len(t, remaining, 2)
}

func TestFaceObservationRepository_RetentionMethods(t *testing.T) {
	db := newStorageRetentionTestDB(t)
	repo := repository.NewFaceObservationRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	f1 := &model.FaceObservation{
		EventID:          "face-1",
		PersonName:       "Alice",
		ObservedAt:       now.Add(-30 * time.Minute),
		ImageRelPath:     "face/pano-1.jpg",
		FaceImageRelPath: "face/crop-1.jpg",
	}
	f2 := &model.FaceObservation{
		EventID:          "face-2",
		PersonName:       "Bob",
		ObservedAt:       now.Add(-20 * time.Minute),
		ImageRelPath:     "face/pano-2.jpg",
		FaceImageRelPath: "face/crop-2.jpg",
	}

	require.NoError(t, repo.Create(ctx, f1))
	require.NoError(t, repo.Create(ctx, f2))

	total, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	expired, err := repo.FindExpired(ctx, now.Add(-25*time.Minute), 10)
	require.NoError(t, err)
	assert.Len(t, expired, 1)
	assert.Equal(t, "face-1", expired[0].EventID)

	require.NoError(t, repo.HardDeleteBatch(ctx, []uint64{f1.ID, f2.ID}))

	totalAfter, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), totalAfter)
}

func TestOperationLogRepository_RetentionMethods(t *testing.T) {
	db := newStorageRetentionTestDB(t)
	repo := repository.NewOperationLogRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	l1 := &model.OperationLog{
		Username:   "admin",
		Module:     "auth",
		Action:     "login",
		StatusCode: 200,
		CreatedAt:  now.Add(-30 * time.Minute),
	}
	l2 := &model.OperationLog{
		Username:   "admin",
		Module:     "user",
		Action:     "create",
		StatusCode: 200,
		CreatedAt:  now.Add(-20 * time.Minute),
	}
	l3 := &model.OperationLog{
		Username:   "admin",
		Module:     "system",
		Action:     "update",
		StatusCode: 200,
		CreatedAt:  now.Add(-10 * time.Minute),
	}

	require.NoError(t, repo.Create(ctx, l1))
	require.NoError(t, repo.Create(ctx, l2))
	require.NoError(t, repo.Create(ctx, l3))

	total, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)

	deleted, err := repo.DeleteExpired(ctx, now.Add(-15*time.Minute), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	totalAfter, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalAfter)
}

func TestFaceCaptureRepository_RetentionMethods(t *testing.T) {
	db := newStorageRetentionTestDB(t)
	repo := repository.NewFaceCaptureRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	c1 := &model.FaceCapture{
		EventID:          "run-1/track-1",
		CameraID:         "cam-1",
		FirstObservedAt:  now.Add(-30 * time.Minute),
		LastObservedAt:   now.Add(-30 * time.Minute),
		BestImageRelPath: "captures/pano-1.jpg",
		BestFaceRelPath:  "captures/face-1.jpg",
	}
	c2 := &model.FaceCapture{
		EventID:          "run-1/track-2",
		CameraID:         "cam-1",
		FirstObservedAt:  now.Add(-20 * time.Minute),
		LastObservedAt:   now.Add(-20 * time.Minute),
		BestImageRelPath: "captures/pano-2.jpg",
		BestFaceRelPath:  "captures/face-2.jpg",
	}

	require.NoError(t, repo.Create(ctx, c1))
	require.NoError(t, repo.Create(ctx, c2))

	total, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	expired, err := repo.FindExpired(ctx, now.Add(-25*time.Minute), 10)
	require.NoError(t, err)
	assert.Len(t, expired, 1)
	assert.Equal(t, "run-1/track-1", expired[0].EventID)

	oldest, err := repo.FindOldest(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, oldest, 1)
	assert.Equal(t, "run-1/track-1", oldest[0].EventID)

	require.NoError(t, repo.HardDeleteBatch(ctx, []uint64{c1.ID}))

	totalAfter, err := repo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalAfter)
}
