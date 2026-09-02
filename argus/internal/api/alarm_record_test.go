package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func setupAlarmRecordAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB, repository.AlarmRecordRepository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	_ = os.Setenv("AIVISION_IMAGE_DIR", tmpDir)

	db := newTestAPIDB(t, "alarm_record")

	alarmRepo := repository.NewAlarmRecordRepository(db)
	camRepo := repository.NewCameraRepository(db)
	algoRepo := repository.NewAlgorithmRepository(db)
	taskRepo := repository.NewTaskRepository(db)

	cfg := &config.Config{}
	svc := service.NewAlarmRecordService(alarmRepo, camRepo, algoRepo, taskRepo, cfg)
	handler := api.NewAlarmRecordHandler(svc)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())

	apiGroup := engine.Group("/api/record")
	{
		apiGroup.GET("/alarms", handler.ListPage)
		apiGroup.GET("/alarms/:id", handler.GetDetail)
		apiGroup.GET("/images/:id", handler.ReadImageStream)
	}

	return engine, db, alarmRepo, tmpDir
}

func TestAlarmRecordAPI_ListPageAndDetail(t *testing.T) {
	engine, _, repo, _ := setupAlarmRecordAPIEngine(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	record := &model.AlarmRecord{
		EventID:          "run-1/evt-1",
		InstanceID:       "inst-1",
		CameraID:         "cam-1",
		AlgorithmID:      "yolov8n",
		AlgorithmVersion: "1.0.0",
		AlarmTypeID:      "person_invade",
		OccurredAt:       now,
		TimeSynced:       true,
		TargetLabel:      "person",
		Confidence:       0.92,
		TrackID:          1,
		BBoxJSON:         []byte(`[0.1,0.1,0.5,0.5]`),
		ImageID:          "img-1",
		ImageRelPath:     "2026/03/30/img-1.jpg",
	}
	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	// 1. GET /api/record/alarms
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/record/alarms?page=1&pageSize=10", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var listResp struct {
		Code int                           `json:"code"`
		Data service.AlarmRecordPageResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list resp: %v", err)
	}
	if listResp.Code != 0 || listResp.Data.Total != 1 || len(listResp.Data.Items) != 1 {
		t.Fatalf("unexpected list response: %+v", listResp)
	}
	if listResp.Data.Items[0].EventID != "run-1/evt-1" || listResp.Data.Items[0].ImageURL != "/api/record/images/img-1" || listResp.Data.Items[0].TargetLabel != "person" {
		t.Errorf("item data mismatch: %+v", listResp.Data.Items[0])
	}

	// 2. GET /api/record/alarms/:id
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/record/alarms/%d", record.ID), nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", rec.Code)
	}
	var detailResp struct {
		Code int                       `json:"code"`
		Data service.AlarmRecordDetail `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("unmarshal detail resp: %v", err)
	}
	if detailResp.Code != 0 || detailResp.Data.ID != record.ID || detailResp.Data.Confidence != 0.92 {
		t.Fatalf("unexpected detail response: %+v", detailResp)
	}

	// 3. GET /api/record/alarms/99999 (不存在)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/record/alarms/99999", nil)
	engine.ServeHTTP(rec, req)
	var notFoundResp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &notFoundResp)
	if notFoundResp.Code != errno.CodeNotFound {
		t.Errorf("code = %d, want CodeNotFound (%d)", notFoundResp.Code, errno.CodeNotFound)
	}
}

func TestAlarmRecordAPI_ListPageTargetLabelFilter(t *testing.T) {
	engine, _, repo, _ := setupAlarmRecordAPIEngine(t)
	ctx := context.Background()

	for _, record := range []*model.AlarmRecord{
		{
			EventID:     "run-1/evt-person",
			InstanceID:  "inst-1",
			CameraID:    "cam-1",
			AlarmTypeID: "person_invade",
			TargetLabel: "person",
			OccurredAt:  time.Now(),
			ImageID:     "img-person",
		},
		{
			EventID:     "run-1/evt-car",
			InstanceID:  "inst-1",
			CameraID:    "cam-1",
			AlarmTypeID: "vehicle_invade",
			TargetLabel: "car",
			OccurredAt:  time.Now().Add(-time.Minute),
			ImageID:     "img-car",
		},
	} {
		if err := repo.Create(ctx, record); err != nil {
			t.Fatalf("create fixture %s: %v", record.EventID, err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/record/alarms?page=1&pageSize=10&targetLabel=person", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var listResp struct {
		Code int                           `json:"code"`
		Data service.AlarmRecordPageResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list resp: %v", err)
	}
	if listResp.Code != 0 || listResp.Data.Total != 1 || len(listResp.Data.Items) != 1 {
		t.Fatalf("unexpected filtered list response: %+v", listResp)
	}
	if listResp.Data.Items[0].TargetLabel != "person" {
		t.Errorf("target label = %q, want person", listResp.Data.Items[0].TargetLabel)
	}
}

func TestAlarmRecordAPI_ReadImageStream(t *testing.T) {
	engine, _, repo, tmpDir := setupAlarmRecordAPIEngine(t)
	ctx := context.Background()

	// 写入一张真实的模拟 JPEG 文件
	imgRelPath := "2026/03/30/test-alarm.jpg"
	fullPath := filepath.Join(tmpDir, imgRelPath)
	_ = os.MkdirAll(filepath.Dir(fullPath), 0o755)
	dummyData := []byte("FAKE-JPEG-DATA-HEADER")
	if err := os.WriteFile(fullPath, dummyData, 0o644); err != nil {
		t.Fatalf("write fake image: %v", err)
	}

	record := &model.AlarmRecord{
		EventID:          "run-1/evt-img",
		InstanceID:       "inst-1",
		CameraID:         "cam-1",
		AlgorithmID:      "yolov8n",
		AlgorithmVersion: "1.0.0",
		AlarmTypeID:      "person_invade",
		OccurredAt:       time.Now(),
		ImageID:          "img-stream-1",
		ImageRelPath:     imgRelPath,
	}
	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	// 1. 成功读取原图图片流
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/record/images/img-stream-1", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("read image status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("Content-Type = %s, want image/jpeg", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != string(dummyData) {
		t.Errorf("body = %s, want %s", rec.Body.String(), string(dummyData))
	}

	// 1.1 缩略图不存在时回退读取原图
	recThumbFallback := httptest.NewRecorder()
	reqThumbFallback := httptest.NewRequest(http.MethodGet, "/api/record/images/img-stream-1?type=thumb", nil)
	engine.ServeHTTP(recThumbFallback, reqThumbFallback)
	if recThumbFallback.Code != http.StatusOK {
		t.Fatalf("read image fallback thumb status = %d, want 200", recThumbFallback.Code)
	}

	// 1.2 写入独立缩略图文件，成功命中读取缩略图
	thumbRelPath := "2026/03/30/test-alarm_thumb.jpg"
	fullThumbPath := filepath.Join(tmpDir, thumbRelPath)
	thumbData := []byte("FAKE-THUMB-DATA")
	_ = os.WriteFile(fullThumbPath, thumbData, 0o644)

	recThumb := httptest.NewRecorder()
	reqThumb := httptest.NewRequest(http.MethodGet, "/api/record/images/img-stream-1?type=thumb", nil)
	engine.ServeHTTP(recThumb, reqThumb)
	if recThumb.Code != http.StatusOK {
		t.Fatalf("read image thumb status = %d, want 200", recThumb.Code)
	}
	if recThumb.Body.String() != string(thumbData) {
		t.Errorf("thumb body = %s, want %s", recThumb.Body.String(), string(thumbData))
	}

	// 2. 读取不存在的图片
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/record/images/img-non-exist", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", rec.Code)
	}
}
