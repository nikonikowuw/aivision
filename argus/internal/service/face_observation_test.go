package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func newTestFaceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Camera{}, &model.FaceObservation{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestReportAdapter_AcceptFaceObservationMonotonic(t *testing.T) {
	db := newTestFaceDB(t)
	faceRepo := repository.NewFaceObservationRepository(db)
	adapter := service.NewReportAdapterWithAlarm(
		repository.NewTaskRepository(db), nil, nil, faceRepo, nil, zap.NewNop(),
	)
	ctx := context.Background()

	base := &argusv1.FaceObservation{
		EventId:          "run-1/42",
		InstanceId:       "instance-1",
		CameraId:         "camera-1",
		CameraName:       "West Gate",
		AlgorithmId:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		TrackId:          42,
		FaceId:           "face-1",
		PersonId:         "person-1",
		PersonName:       "Alice",
		Similarity:       0.82,
		FaceBbox:         &argusv1.BoundingBox{XMin: 0.2, YMin: 0.1, XMax: 0.4, YMax: 0.5},
		ImageId:          "img-pano-1",
		ImageRelPath:     "2026/09/02/pano.jpg",
		FaceImageId:      "img-face-1",
		FaceImageRelPath: "2026/09/02/face.jpg",
		WallTimeNs:       time.Now().UnixNano(),
		TimeSynced:       true,
	}

	if err := adapter.AcceptFaceObservation(ctx, base); err != nil {
		t.Fatalf("accept initial face observation: %v", err)
	}

	lower := protoFaceObservation(base, 0.75, "Bob", "face-2")
	if err := adapter.AcceptFaceObservation(ctx, lower); err != nil {
		t.Fatalf("accept lower face observation: %v", err)
	}
	record, err := faceRepo.GetByEventID(ctx, base.EventId)
	if err != nil {
		t.Fatalf("get face observation after lower retry: %v", err)
	}
	if record.Similarity != 0.82 || record.PersonName != "Alice" {
		t.Fatalf("lower similarity overwrote record: %+v", record)
	}

	higher := protoFaceObservation(base, 0.91, "Carol", "face-3")
	if err := adapter.AcceptFaceObservation(ctx, higher); err != nil {
		t.Fatalf("accept higher face observation: %v", err)
	}
	record, err = faceRepo.GetByEventID(ctx, base.EventId)
	if err != nil {
		t.Fatalf("get face observation after higher retry: %v", err)
	}
	if record.Similarity != 0.91 || record.PersonName != "Carol" || record.FaceID != "face-3" ||
		record.CameraName != "West Gate" {
		t.Fatalf("higher similarity did not replace record: %+v", record)
	}
}

func protoFaceObservation(base *argusv1.FaceObservation, similarity float32, personName, faceID string) *argusv1.FaceObservation {
	copy := proto.Clone(base).(*argusv1.FaceObservation)
	copy.Similarity = similarity
	copy.PersonName = personName
	copy.FaceId = faceID
	return copy
}

func TestFaceObservationServiceUsesSnapshotAndBuildsImageURLs(t *testing.T) {
	db := newTestFaceDB(t)
	faceRepo := repository.NewFaceObservationRepository(db)
	camRepo := repository.NewCameraRepository(db)
	svc := service.NewFaceObservationService(faceRepo, camRepo, nil)
	ctx := context.Background()

	if err := camRepo.Create(ctx, &model.Camera{
		CameraID: "camera-1",
		Name:     "Current Camera Name",
		RtspURL:  "rtsp://localhost/test",
	}); err != nil {
		t.Fatalf("create camera: %v", err)
	}
	if err := faceRepo.Create(ctx, &model.FaceObservation{
		EventID:          "run-1/42",
		CameraID:         "camera-1",
		CameraName:       "Name At Recognition",
		AlgorithmID:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		TrackID:          42,
		FaceID:           "face-1",
		PersonID:         "person-1",
		PersonName:       "Alice",
		Similarity:       0.91,
		BBoxJSON:         model.JSONRaw(`[0.2,0.1,0.4,0.5]`),
		ImageID:          "img-pano-1",
		ImageRelPath:     "2026/09/02/pano.jpg",
		FaceImageID:      "img-face-1",
		FaceImageRelPath: "2026/09/02/face.jpg",
		ObservedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("create face observation: %v", err)
	}

	result, err := svc.ListPage(ctx, &service.FaceObservationQuery{PersonName: "Ali"})
	if err != nil {
		t.Fatalf("list face observations: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one face observation, got total=%d items=%d", result.Total, len(result.Items))
	}
	item := result.Items[0]
	if item.CameraName != "Name At Recognition" || item.PersonName != "Alice" || item.Similarity != 0.91 {
		t.Fatalf("snapshot fields were not preserved: %+v", item)
	}
	if item.PanoramaImageURL != fmt.Sprintf("/api/record/faces/%d/panorama", item.ID) ||
		item.FaceImageURL != fmt.Sprintf("/api/record/faces/%d/face", item.ID) {
		t.Fatalf("unexpected face image URLs: %+v", item)
	}
	if len(item.BBox) != 4 || item.BBox[0] != 0.2 || item.BBox[3] != 0.5 {
		t.Fatalf("unexpected face bbox: %v", item.BBox)
	}
}
