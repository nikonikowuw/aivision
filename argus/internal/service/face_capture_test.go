package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func newTestFaceCaptureDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Camera{}, &model.FaceCapture{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestReportAdapter_AcceptFaceCapture(t *testing.T) {
	db := newTestFaceCaptureDB(t)
	captureRepo := repository.NewFaceCaptureRepository(db)
	adapter := service.NewReportAdapterWithAlarm(
		repository.NewTaskRepository(db), nil, nil, nil, captureRepo, nil, zap.NewNop(),
	)
	ctx := context.Background()

	now := time.Now()
	// 1. 首帧抓拍上报
	req1 := &argusv1.FaceCapture{
		EventId:          "run-1/101",
		InstanceId:       "inst-1",
		CameraId:         "cam-1",
		CameraName:       "Front Gate",
		AlgorithmId:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		TrackId:          101,
		Snapshot: &argusv1.FaceCaptureSnapshot{
			SnapshotIndex:    1,
			WallTimeNs:       now.UnixNano(),
			TimeSynced:       true,
			FaceBbox:         &argusv1.BoundingBox{XMin: 0.1, YMin: 0.2, XMax: 0.3, YMax: 0.4},
			QualityScore:     0.85,
			Similarity:       0.55,
			ImageId:          "pano-1",
			ImageRelPath:     "captures/pano-1.jpg",
			FaceImageId:      "face-1",
			FaceImageRelPath: "captures/face-1.jpg",
		},
	}

	if err := adapter.AcceptFaceCapture(ctx, req1); err != nil {
		t.Fatalf("accept face capture 1: %v", err)
	}

	// 2. 第二帧追加上报（命中底库已知人员）
	req2 := &argusv1.FaceCapture{
		EventId:          "run-1/101",
		InstanceId:       "inst-1",
		CameraId:         "cam-1",
		CameraName:       "Front Gate",
		AlgorithmId:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		TrackId:          101,
		Snapshot: &argusv1.FaceCaptureSnapshot{
			SnapshotIndex:    2,
			WallTimeNs:       now.Add(time.Second).UnixNano(),
			TimeSynced:       true,
			FaceBbox:         &argusv1.BoundingBox{XMin: 0.15, YMin: 0.25, XMax: 0.35, YMax: 0.45},
			QualityScore:     0.95,
			Similarity:       0.93,
			PersonId:         "person-100",
			PersonName:       "Bob",
			ImageId:          "pano-2",
			ImageRelPath:     "captures/pano-2.jpg",
			FaceImageId:      "face-2",
			FaceImageRelPath: "captures/face-2.jpg",
		},
	}

	if err := adapter.AcceptFaceCapture(ctx, req2); err != nil {
		t.Fatalf("accept face capture 2: %v", err)
	}

	// 3. Service 查询验证
	svc := service.NewFaceCaptureService(captureRepo, repository.NewCameraRepository(db), &config.Config{})
	res, err := svc.ListPage(ctx, &service.FaceCaptureQuery{CameraID: "cam-1"})
	if err != nil || res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("list captures: total=%d, err=%v", res.Total, err)
	}

	item := res.Items[0]
	if item.SnapshotCount != 2 {
		t.Errorf("snapshot count = %d, want 2", item.SnapshotCount)
	}
	if item.BestSimilarity != 0.93 || item.BestPersonName != "Bob" {
		t.Errorf("best sim = %f, person = %s", item.BestSimilarity, item.BestPersonName)
	}
	if item.FaceImageURL != fmt.Sprintf("/api/record/captures/%d/face", item.ID) ||
		item.PanoramaImageURL != fmt.Sprintf("/api/record/captures/%d/panorama", item.ID) {
		t.Errorf("unexpected image URLs: face=%s, pano=%s", item.FaceImageURL, item.PanoramaImageURL)
	}

	// 4. Detail 查询验证（包含 Snapshots 列表）
	detail, err := svc.GetDetail(ctx, item.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.Snapshots) != 2 {
		t.Fatalf("detail snapshots len = %d, want 2", len(detail.Snapshots))
	}
	if detail.Snapshots[0].QualityScore != 0.85 || detail.Snapshots[1].Similarity != 0.93 {
		t.Errorf("snapshots detail mismatch: %+v", detail.Snapshots)
	}
	if detail.Snapshots[0].FaceImageURL != fmt.Sprintf("/api/record/captures/%d/snapshots/1/face", item.ID) {
		t.Errorf("snapshot face URL mismatch: %s", detail.Snapshots[0].FaceImageURL)
	}
}

func TestFaceCaptureService_ReadImageStream(t *testing.T) {
	db := newTestFaceCaptureDB(t)
	captureRepo := repository.NewFaceCaptureRepository(db)
	camRepo := repository.NewCameraRepository(db)

	tempDir := t.TempDir()
	panoRelPath := "captures/test_pano.jpg"
	panoThumbRelPath := "captures/test_pano_thumb.jpg"
	faceRelPath := "captures/test_face.jpg"

	_ = os.MkdirAll(filepath.Join(tempDir, "captures"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, panoRelPath), []byte("fake-pano-data"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, panoThumbRelPath), []byte("fake-pano-thumb"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, faceRelPath), []byte("fake-face-data"), 0644)

	t.Setenv("AIVISION_IMAGE_DIR", tempDir)
	svc := service.NewFaceCaptureService(captureRepo, camRepo, &config.Config{})
	ctx := context.Background()

	now := time.Now()
	capture := &model.FaceCapture{
		EventID:          "run-1/200",
		CameraID:         "cam-1",
		BestImageRelPath: panoRelPath,
		BestFaceRelPath:  faceRelPath,
		FirstObservedAt:  now,
		LastObservedAt:   now,
	}
	snap := &model.SnapshotItem{
		SnapshotIndex:    1,
		ObservedAt:       now,
		ImageRelPath:     panoRelPath,
		FaceImageRelPath: faceRelPath,
	}
	if err := captureRepo.UpsertIncremental(ctx, capture, snap); err != nil {
		t.Fatalf("upsert capture: %v", err)
	}

	// 1. 读取最佳全景原图 (isThumbnail = false)
	rc, size, mime, err := svc.ReadImageStream(ctx, 1, "panorama", 0, false)
	if err != nil {
		t.Fatalf("read pano image: %v", err)
	}
	defer rc.Close()
	if size != int64(len("fake-pano-data")) || mime != "image/jpeg" {
		t.Errorf("pano size=%d, mime=%s", size, mime)
	}

	// 2. 读取最佳全景缩略图 (isThumbnail = true)
	rcThumb, sizeThumb, mimeThumb, err := svc.ReadImageStream(ctx, 1, "panorama", 0, true)
	if err != nil {
		t.Fatalf("read pano thumb image: %v", err)
	}
	defer rcThumb.Close()
	if sizeThumb != int64(len("fake-pano-thumb")) || mimeThumb != "image/jpeg" {
		t.Errorf("pano thumb size=%d, mime=%s", sizeThumb, mimeThumb)
	}

	// 3. 读取第 1 帧特写小图 (无缩略图时回退原图)
	rcFace, sizeFace, mimeFace, err := svc.ReadImageStream(ctx, 1, "face", 1, true)
	if err != nil {
		t.Fatalf("read snapshot face image: %v", err)
	}
	defer rcFace.Close()
	if sizeFace != int64(len("fake-face-data")) || mimeFace != "image/jpeg" {
		t.Errorf("face size=%d, mime=%s", sizeFace, mimeFace)
	}
}

func TestFaceCaptureService_CandidatesPersistenceAndMapping(t *testing.T) {
	db := newTestFaceCaptureDB(t)
	captureRepo := repository.NewFaceCaptureRepository(db)
	camRepo := repository.NewCameraRepository(db)
	adapter := service.NewReportAdapterWithAlarm(
		repository.NewTaskRepository(db), nil, nil, nil, captureRepo, nil, zap.NewNop(),
	)
	ctx := context.Background()

	capProto := &argusv1.FaceCapture{
		EventId:    "run-1/capture-cand",
		InstanceId: "instance-1",
		CameraId:   "camera-1",
		TrackId:    77,
		Snapshot: &argusv1.FaceCaptureSnapshot{
			SnapshotIndex: 1,
			WallTimeNs:    time.Now().UnixNano(),
			Similarity:    0.91,
			QualityScore:  0.88,
			PersonId:      "p-1",
			PersonName:    "Alice",
			Candidates: []*argusv1.FaceCandidate{
				{FaceId: "f-1", PersonId: "p-1", PersonName: "Alice", Similarity: 0.91},
				{FaceId: "f-2", PersonId: "p-2", PersonName: "Bob", Similarity: 0.75},
				{FaceId: "f-3", PersonId: "p-3", PersonName: "Charlie", Similarity: 0.60},
			},
		},
	}

	if err := adapter.AcceptFaceCapture(ctx, capProto); err != nil {
		t.Fatalf("accept face capture: %v", err)
	}

	svc := service.NewFaceCaptureService(captureRepo, camRepo, &config.Config{})
	detail, err := svc.GetDetail(ctx, 1)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.BestCandidates) != 3 {
		t.Fatalf("expected 3 best candidates, got %d", len(detail.BestCandidates))
	}
	if len(detail.Snapshots) != 1 || len(detail.Snapshots[0].Candidates) != 3 {
		t.Fatalf("expected snapshot candidates count 3, got %+v", detail.Snapshots)
	}
	if detail.BestCandidates[0].PersonName != "Alice" || detail.BestCandidates[0].Similarity != 0.91 {
		t.Errorf("candidate[0] mismatch: %+v", detail.BestCandidates[0])
	}
}
