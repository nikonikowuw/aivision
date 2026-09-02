package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		repository.NewTaskRepository(db), nil, nil, faceRepo, nil, nil, zap.NewNop(),
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

func TestFaceObservationService_ReadImageStream(t *testing.T) {
	db := newTestFaceDB(t)
	faceRepo := repository.NewFaceObservationRepository(db)
	camRepo := repository.NewCameraRepository(db)

	tempDir := t.TempDir()
	panoRelPath := "2026-09-02/test_pano.jpg"
	panoThumbRelPath := "2026-09-02/test_pano_thumb.jpg"
	faceRelPath := "2026-09-02/test_face.jpg"

	_ = os.MkdirAll(filepath.Join(tempDir, "2026-09-02"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, panoRelPath), []byte("fake-pano-data"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, panoThumbRelPath), []byte("fake-pano-thumb"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, faceRelPath), []byte("fake-face-data"), 0644)

	t.Setenv("AIVISION_IMAGE_DIR", tempDir)
	svc := service.NewFaceObservationService(faceRepo, camRepo, nil)
	ctx := context.Background()

	obs := &model.FaceObservation{
		EventID:          "run-1/42",
		CameraID:         "camera-1",
		ImageRelPath:     panoRelPath,
		FaceImageRelPath: faceRelPath,
		ObservedAt:       time.Now(),
	}
	if err := faceRepo.Create(ctx, obs); err != nil {
		t.Fatalf("create face observation: %v", err)
	}

	// 1. 读取原图 (isThumbnail = false)
	rc, size, mime, err := svc.ReadImageStream(ctx, obs.ID, "panorama", false)
	if err != nil {
		t.Fatalf("read pano image: %v", err)
	}
	defer rc.Close()
	if size != int64(len("fake-pano-data")) || mime != "image/jpeg" {
		t.Errorf("pano size=%d, mime=%s", size, mime)
	}

	// 2. 读取缩略图 (isThumbnail = true)
	rcThumb, sizeThumb, mimeThumb, err := svc.ReadImageStream(ctx, obs.ID, "panorama", true)
	if err != nil {
		t.Fatalf("read pano thumb: %v", err)
	}
	defer rcThumb.Close()
	if sizeThumb != int64(len("fake-pano-thumb")) || mimeThumb != "image/jpeg" {
		t.Errorf("pano thumb size=%d, mime=%s", sizeThumb, mimeThumb)
	}
}

func TestFaceObservationService_CandidatesPersistenceAndMapping(t *testing.T) {
	db := newTestFaceDB(t)
	faceRepo := repository.NewFaceObservationRepository(db)
	camRepo := repository.NewCameraRepository(db)
	adapter := service.NewReportAdapterWithAlarm(
		repository.NewTaskRepository(db), nil, nil, faceRepo, nil, nil, zap.NewNop(),
	)
	ctx := context.Background()

	obs := &argusv1.FaceObservation{
		EventId:    "run-1/cand-test",
		InstanceId: "instance-1",
		CameraId:   "camera-1",
		TrackId:    99,
		FaceId:     "face-top1",
		PersonId:   "person-top1",
		PersonName: "Top One",
		Similarity: 0.95,
		WallTimeNs: time.Now().UnixNano(),
		Candidates: []*argusv1.FaceCandidate{
			{FaceId: "face-top1", PersonId: "person-top1", PersonName: "Top One", Similarity: 0.95},
			{FaceId: "face-top2", PersonId: "person-top2", PersonName: "Top Two", Similarity: 0.81},
			{FaceId: "face-top3", PersonId: "person-top3", PersonName: "Top Three", Similarity: 0.70},
			{FaceId: "face-top4", PersonId: "person-top4", PersonName: "Top Four", Similarity: 0.65},
			{FaceId: "face-top5", PersonId: "person-top5", PersonName: "Top Five", Similarity: 0.50},
		},
	}

	if err := adapter.AcceptFaceObservation(ctx, obs); err != nil {
		t.Fatalf("accept face observation with candidates: %v", err)
	}

	svc := service.NewFaceObservationService(faceRepo, camRepo, nil)
	result, err := svc.ListPage(ctx, &service.FaceObservationQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	item := result.Items[0]
	if len(item.Candidates) != 5 {
		t.Fatalf("expected 5 candidates, got %d", len(item.Candidates))
	}
	if item.Candidates[0].PersonName != "Top One" || item.Candidates[0].Similarity != 0.95 {
		t.Errorf("candidate[0] mismatch: %+v", item.Candidates[0])
	}
	if item.Candidates[4].PersonName != "Top Five" || item.Candidates[4].Similarity != 0.50 {
		t.Errorf("candidate[4] mismatch: %+v", item.Candidates[4])
	}
}
