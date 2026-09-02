package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&model.Camera{},
		&model.AnalysisTask{},
		&model.SystemConfig{},
		&model.AlarmRecord{},
		&model.PlateObservation{},
		&model.FaceObservation{},
		&model.FaceCapture{},
		&model.CaptureRecord{},
		&model.OperationLog{},
		&model.Person{},
		&model.PersonFace{},
	)
	require.NoError(t, err)
	return db
}

type dynamicSampler struct {
	usage storage.DiskUsage
}

func (d *dynamicSampler) GetDiskUsage(path string) (storage.DiskUsage, error) {
	return d.usage, nil
}

func (d *dynamicSampler) SetUsage(usage storage.DiskUsage) {
	d.usage = usage
}

func TestStorageCleanupService_ConfigManagement(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	sampler := storage.NewMockDiskUsageSampler(storage.DiskUsage{
		TotalBytes:   1000000,
		UsedBytes:    500000,
		FreeBytes:    500000,
		UsagePercent: 50.0,
	}, nil)

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	svc := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, captureRepo, opLogRepo, fileStorage, sampler, zap.NewNop(),
	)

	ctx := context.Background()

	// 1. Default config
	c, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, c.RetentionDays)
	assert.Equal(t, 85, c.HighWatermarkPercent)
	assert.Equal(t, 70, c.LowWatermarkPercent)
	assert.True(t, c.AutoCleanupEnabled)

	// 2. Invalid config updates
	err = svc.UpdateConfig(ctx, &model.StorageRetentionConfigValue{
		RetentionDays:        0, // invalid (<1)
		HighWatermarkPercent: 85,
		LowWatermarkPercent:  70,
		CheckIntervalSeconds: 600,
	})
	require.Error(t, err)
	var appErr *errno.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, errno.CodeStorageInvalidConfig, appErr.Code)

	// Low >= High
	err = svc.UpdateConfig(ctx, &model.StorageRetentionConfigValue{
		RetentionDays:        15,
		HighWatermarkPercent: 70,
		LowWatermarkPercent:  70,
		CheckIntervalSeconds: 600,
	})
	require.Error(t, err)

	// 3. Valid update
	newCfg := &model.StorageRetentionConfigValue{
		RetentionDays:        15,
		HighWatermarkPercent: 80,
		LowWatermarkPercent:  60,
		CheckIntervalSeconds: 300,
		AutoCleanupEnabled:   true,
	}
	err = svc.UpdateConfig(ctx, newCfg)
	require.NoError(t, err)

	c2, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, 15, c2.RetentionDays)
	assert.Equal(t, 80, c2.HighWatermarkPercent)
	assert.Equal(t, 60, c2.LowWatermarkPercent)
}

func TestStorageCleanupService_RoutineTTL(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	// Create physical dummy files
	alarmImgPath := filepath.Join(tempDir, "alarm.jpg")
	require.NoError(t, os.WriteFile(alarmImgPath, []byte("alarm-img-data"), 0o600))

	plateImgPath := filepath.Join(tempDir, "plate.jpg")
	require.NoError(t, os.WriteFile(plateImgPath, []byte("plate-img-data"), 0o600))

	faceImgPath := filepath.Join(tempDir, "face.jpg")
	require.NoError(t, os.WriteFile(faceImgPath, []byte("face-img-data"), 0o600))

	capturePanoPath := filepath.Join(tempDir, "capture_pano.jpg")
	require.NoError(t, os.WriteFile(capturePanoPath, []byte("capture-pano-data"), 0o600))

	captureSnapPath := filepath.Join(tempDir, "capture_snap.jpg")
	require.NoError(t, os.WriteFile(captureSnapPath, []byte("capture-snap-data"), 0o600))

	ctx := context.Background()
	expiredTime := time.Now().AddDate(0, 0, -40) // 40 days ago (TTL default is 30)
	freshTime := time.Now().AddDate(0, 0, -5)    // 5 days ago

	// Expired alarm record
	require.NoError(t, alarmRepo.Create(ctx, &model.AlarmRecord{
		EventID:      "ev-expired",
		InstanceID:   "inst-1",
		CameraID:     "cam-1",
		OccurredAt:   expiredTime,
		ImageRelPath: "alarm.jpg",
	}))

	// Fresh alarm record
	require.NoError(t, alarmRepo.Create(ctx, &model.AlarmRecord{
		EventID:    "ev-fresh",
		InstanceID: "inst-1",
		CameraID:   "cam-1",
		OccurredAt: freshTime,
	}))

	// Expired plate observation
	require.NoError(t, plateRepo.Create(ctx, &model.PlateObservation{
		EventID:      "plate-expired",
		ObservedAt:   expiredTime,
		ImageRelPath: "plate.jpg",
	}))

	// Expired face observation
	require.NoError(t, faceRepo.Create(ctx, &model.FaceObservation{
		EventID:      "face-expired",
		ObservedAt:   expiredTime,
		ImageRelPath: "face.jpg",
	}))

	// Expired face capture (with snapshots_json)
	snapshotsJSON, err := json.Marshal([]model.SnapshotItem{
		{
			SnapshotIndex: 1,
			ObservedAt:    expiredTime,
			ImageRelPath:  "capture_snap.jpg",
		},
	})
	require.NoError(t, err)

	require.NoError(t, captureRepo.Create(ctx, &model.FaceCapture{
		EventID:          "capture-expired",
		CameraID:         "cam-1",
		FirstObservedAt:  expiredTime,
		LastObservedAt:   expiredTime,
		BestImageRelPath: "capture_pano.jpg",
		SnapshotsJSON:    snapshotsJSON,
	}))

	// Expired operation log
	require.NoError(t, opLogRepo.Create(ctx, &model.OperationLog{
		Username:  "admin",
		Module:    "auth",
		CreatedAt: expiredTime,
	}))

	sampler := storage.NewMockDiskUsageSampler(storage.DiskUsage{
		TotalBytes:   1000000,
		UsedBytes:    500000,
		FreeBytes:    500000,
		UsagePercent: 50.0,
	}, nil)

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	svc := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, captureRepo, opLogRepo, fileStorage, sampler, zap.NewNop(),
	)

	// Trigger single cycle
	svc.Start(ctx)
	defer svc.Stop()

	// Wait for worker to finish startup cycle
	time.Sleep(150 * time.Millisecond)

	// Assert files are deleted
	_, err = os.Stat(alarmImgPath)
	assert.True(t, os.IsNotExist(err), "alarm image file should be physically removed")
	_, err = os.Stat(plateImgPath)
	assert.True(t, os.IsNotExist(err), "plate image file should be physically removed")
	_, err = os.Stat(faceImgPath)
	assert.True(t, os.IsNotExist(err), "face image file should be physically removed")
	_, err = os.Stat(capturePanoPath)
	assert.True(t, os.IsNotExist(err), "capture pano image file should be physically removed")
	_, err = os.Stat(captureSnapPath)
	assert.True(t, os.IsNotExist(err), "capture snap image file should be physically removed")

	// Assert DB records
	alarmCount, err := alarmRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), alarmCount, "only fresh alarm record should remain")

	plateCount, err := plateRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), plateCount)

	faceCount, err := faceRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), faceCount)

	captureCount, err := captureRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), captureCount)

	opLogCount, err := opLogRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), opLogCount)
}

func TestStorageCleanupService_EmergencyWatermark(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	// Insert 5 alarm records
	for i := 1; i <= 5; i++ {
		require.NoError(t, alarmRepo.Create(ctx, &model.AlarmRecord{
			EventID:    fmt.Sprintf("ev-%d", i),
			InstanceID: "inst-1",
			CameraID:   "cam-1",
			OccurredAt: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	dynSampler := &dynamicSampler{
		usage: storage.DiskUsage{
			TotalBytes:   1000000,
			UsedBytes:    900000,
			FreeBytes:    10000,
			UsagePercent: 90.0, // > 85% high watermark
		},
	}

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	svc := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, captureRepo, opLogRepo, fileStorage, dynSampler, zap.NewNop(),
	)

	// Status before
	status, err := svc.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), status.AlarmRecordCount)

	// Trigger cleanup
	svc.Start(ctx)
	defer svc.Stop()

	// Wait for cleanup
	time.Sleep(150 * time.Millisecond)

	// Records should be deleted by emergency FIFO purge
	alarmCount, err := alarmRepo.CountTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), alarmCount)
}

func TestStorageCleanupService_CircuitBreakerState(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	ctx := context.Background()

	dynSampler := &dynamicSampler{
		usage: storage.DiskUsage{
			TotalBytes:   1000000,
			UsedBytes:    960000,
			FreeBytes:    40000,
			UsagePercent: 96.0, // >= 95% triggers circuit breaker
		},
	}

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	svc := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, captureRepo, opLogRepo, fileStorage, dynSampler, zap.NewNop(),
	)

	svc.Start(ctx)
	defer svc.Stop()

	time.Sleep(150 * time.Millisecond)

	assert.True(t, svc.IsCircuitBreakerActive())

	status, err := svc.GetStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.CircuitBreakerActive)
	assert.Equal(t, "degraded", status.Status)

	// Simulate disk freed below 85%
	dynSampler.SetUsage(storage.DiskUsage{
		TotalBytes:   1000000,
		UsedBytes:    750000,
		FreeBytes:    250000,
		UsagePercent: 75.0,
	})

	require.NoError(t, svc.TriggerCleanup(ctx))
	time.Sleep(150 * time.Millisecond)

	assert.False(t, svc.IsCircuitBreakerActive())

	status2, err := svc.GetStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status2.CircuitBreakerActive)
	assert.Equal(t, "normal", status2.Status)
}

func TestStorageCleanupService_WhitelistAssetProtection(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	ctx := context.Background()

	// Create person and person face in gallery
	person := &model.Person{
		PersonID: "whitelist-person-001",
		Name:     "VIP Employee",
	}
	require.NoError(t, db.Create(person).Error)

	face := &model.PersonFace{
		FaceID:         "face-asset-001",
		PersonID:       "whitelist-person-001",
		Embedding:      []byte{0x01, 0x02, 0x03},
		RawImageKey:    "faces/raw_001.jpg",
		AlignedFaceKey: "faces/aligned_001.jpg",
	}
	require.NoError(t, db.Create(face).Error)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	// Extreme disk usage 99%
	sampler := storage.NewMockDiskUsageSampler(storage.DiskUsage{
		TotalBytes:   1000000,
		UsedBytes:    990000,
		FreeBytes:    10000,
		UsagePercent: 99.0,
	}, nil)

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	svc := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, captureRepo, opLogRepo, fileStorage, sampler, zap.NewNop(),
	)

	svc.Start(ctx)
	defer svc.Stop()

	time.Sleep(150 * time.Millisecond)

	// Verify person and face assets are untouched
	var pCount int64
	require.NoError(t, db.Model(&model.Person{}).Count(&pCount).Error)
	assert.Equal(t, int64(1), pCount, "Person whitelist asset must NEVER be deleted")

	var fCount int64
	require.NoError(t, db.Model(&model.PersonFace{}).Count(&fCount).Error)
	assert.Equal(t, int64(1), fCount, "PersonFace whitelist asset must NEVER be deleted")
}

type sequenceSampler struct {
	usages []storage.DiskUsage
	index  int
}

func (s *sequenceSampler) GetDiskUsage(string) (storage.DiskUsage, error) {
	if s.index >= len(s.usages) {
		return s.usages[len(s.usages)-1], nil
	}
	usage := s.usages[s.index]
	s.index++
	return usage, nil
}

func TestStorageCleanupService_PrioritizesUnrecognizedCaptures(t *testing.T) {
	db := newTestDB(t)
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	faceCaptureRepo := repository.NewFaceCaptureRepository(db)
	captureRepo := repository.NewCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)
	unrecognizedPath := filepath.Join(tempDir, "unrecognized.jpg")
	recognizedPath := filepath.Join(tempDir, "recognized.jpg")
	require.NoError(t, os.WriteFile(unrecognizedPath, []byte("unrecognized"), 0o600))
	require.NoError(t, os.WriteFile(recognizedPath, []byte("recognized"), 0o600))

	unrecognized := &model.CaptureRecord{
		EventID:      "capture-unrecognized",
		TargetType:   model.CaptureTargetPerson,
		CameraID:     "cam-1",
		ImageID:      "image-unrecognized",
		ImageRelPath: "unrecognized.jpg",
		CapturedAt:   now.Add(-2 * time.Hour),
	}
	recognized := &model.CaptureRecord{
		EventID:      "capture-recognized",
		TargetType:   model.CaptureTargetPerson,
		CameraID:     "cam-1",
		ImageID:      "image-recognized",
		ImageRelPath: "recognized.jpg",
		IsRecognized: true,
		CapturedAt:   now.Add(-time.Hour),
	}
	require.NoError(t, captureRepo.Create(ctx, unrecognized))
	require.NoError(t, captureRepo.Create(ctx, recognized))

	sampler := &sequenceSampler{usages: []storage.DiskUsage{
		{TotalBytes: 1000, UsedBytes: 900, FreeBytes: 100, UsagePercent: 90},
		{TotalBytes: 1000, UsedBytes: 900, FreeBytes: 100, UsagePercent: 90},
		{TotalBytes: 1000, UsedBytes: 600, FreeBytes: 400, UsagePercent: 60},
	}}
	cfg := &config.Config{Storage: config.Storage{Local: config.Local{Root: tempDir}}}
	svc := service.NewStorageCleanupServiceWithCaptures(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, faceCaptureRepo, captureRepo, opLogRepo,
		fileStorage, sampler, zap.NewNop(),
	)

	svc.Start(ctx)
	defer svc.Stop()
	require.Eventually(t, func() bool {
		count, countErr := captureRepo.CountTotal(ctx)
		return countErr == nil && count == 1
	}, time.Second, 10*time.Millisecond)

	if _, err := os.Stat(unrecognizedPath); !os.IsNotExist(err) {
		t.Fatalf("unrecognized capture image still exists, stat error = %v", err)
	}
	if _, err := os.Stat(recognizedPath); err != nil {
		t.Fatalf("recognized capture image was removed: %v", err)
	}
	remaining, err := captureRepo.GetByEventID(ctx, recognized.EventID)
	require.NoError(t, err)
	assert.True(t, remaining.IsRecognized)
}

type fixedBreakerChecker struct {
	active bool
}

func (f fixedBreakerChecker) IsCircuitBreakerActive() bool {
	return f.active
}

func TestReportAdapter_StorageCircuitBreakerDropsImages(t *testing.T) {
	db := newTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	captureRepo := repository.NewFaceCaptureRepository(db)

	ctx := context.Background()

	// ReportAdapter with circuit breaker ACTIVE
	adapter := service.NewReportAdapterWithAlarm(taskRepo, alarmRepo, plateRepo, faceRepo, captureRepo, nil, zap.NewNop())
	adapter.SetCircuitBreakerChecker(fixedBreakerChecker{active: true})

	// 1. Alarm event
	alarmReq := &argusv1.AlarmEvent{
		EventId:      "alarm-cb-001",
		InstanceId:   "inst-1",
		CameraId:     "cam-1",
		ImageId:      "img-cb-1",
		ImageRelPath: "2026/09/alarm.jpg",
		Objects: []*argusv1.DetectedObject{
			{Label: "person", Confidence: 0.95},
		},
	}
	require.NoError(t, adapter.AcceptAlarm(ctx, alarmReq))

	savedAlarm, err := alarmRepo.GetByEventID(ctx, "alarm-cb-001")
	require.NoError(t, err)
	assert.Equal(t, "", savedAlarm.ImageRelPath, "ImageRelPath must be dropped when circuit breaker is active")
	assert.Equal(t, "", savedAlarm.ImageID, "ImageID must be dropped when circuit breaker is active")
	assert.Equal(t, "person", savedAlarm.TargetLabel, "Metadata must still be preserved")

	// 2. Plate observation
	plateReq := &argusv1.PlateObservation{
		EventId:           "plate-cb-001",
		PlateText:         "京A88888",
		ImageId:           "img-cb-2",
		ImageRelPath:      "2026/09/plate_pano.jpg",
		PlateImageId:      "img-cb-3",
		PlateImageRelPath: "2026/09/plate_crop.jpg",
	}
	require.NoError(t, adapter.AcceptPlateObservation(ctx, plateReq))

	savedPlate, err := plateRepo.GetByEventID(ctx, "plate-cb-001")
	require.NoError(t, err)
	assert.Equal(t, "", savedPlate.ImageRelPath)
	assert.Equal(t, "", savedPlate.PlateImageRelPath)
	assert.Equal(t, "京A88888", savedPlate.PlateText)

	// 3. Face observation
	faceReq := &argusv1.FaceObservation{
		EventId:          "face-cb-001",
		PersonName:       "Alice",
		ImageId:          "img-cb-4",
		ImageRelPath:     "2026/09/face_pano.jpg",
		FaceImageId:      "img-cb-5",
		FaceImageRelPath: "2026/09/face_crop.jpg",
	}
	require.NoError(t, adapter.AcceptFaceObservation(ctx, faceReq))

	savedFace, err := faceRepo.GetByEventID(ctx, "face-cb-001")
	require.NoError(t, err)
	assert.Equal(t, "", savedFace.ImageRelPath)
	assert.Equal(t, "", savedFace.FaceImageRelPath)
	assert.Equal(t, "Alice", savedFace.PersonName)

	// 4. Face capture
	captureReq := &argusv1.FaceCapture{
		EventId:  "capture-cb-001",
		CameraId: "cam-1",
		Snapshot: &argusv1.FaceCaptureSnapshot{
			ImageId:          "img-cb-6",
			ImageRelPath:     "2026/09/cap_pano.jpg",
			FaceImageId:      "img-cb-7",
			FaceImageRelPath: "2026/09/cap_face.jpg",
			QualityScore:     0.98,
		},
	}
	require.NoError(t, adapter.AcceptFaceCapture(ctx, captureReq))

	savedCapture, err := captureRepo.GetByEventID(ctx, "capture-cb-001")
	require.NoError(t, err)
	assert.Equal(t, "", savedCapture.BestImageRelPath)
	assert.Equal(t, "", savedCapture.BestFaceRelPath)
	snapshots, err := savedCapture.ParseSnapshots()
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	assert.Equal(t, "", snapshots[0].ImageRelPath)
	assert.Equal(t, "", snapshots[0].FaceImageRelPath)
}
