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

func setupCaptureAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB, repository.CaptureRepository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	t.Setenv("AIVISION_IMAGE_DIR", tmpDir)

	db := newTestAPIDB(t, "capture_api")

	captureRepo := repository.NewCaptureRepository(db)

	cfg := &config.Config{}
	svc := service.NewCaptureService(captureRepo, cfg)
	handler := api.NewCaptureHandler(svc)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())

	apiGroup := engine.Group("/api/record")
	{
		apiGroup.GET("/captures", handler.ListPage)
		apiGroup.GET("/captures/:id", handler.GetDetail)
		apiGroup.GET("/captures/:id/image", handler.ReadImage)
	}

	return engine, db, captureRepo, tmpDir
}

func TestCaptureAPI_ListPageAndDetail(t *testing.T) {
	engine, _, repo, tmpDir := setupCaptureAPIEngine(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	record := &model.CaptureRecord{
		EventID:             "run-1/100-1-0",
		InstanceID:          "inst-1",
		CameraID:            "cam-1",
		AlgorithmID:         "face_recognition",
		AlgorithmVersion:    "1.0.0",
		CapturedAt:          now,
		TimeSynced:          true,
		TargetType:          model.CaptureTargetPerson,
		Confidence:          0.95,
		QualityScore:        0.88,
		TrackID:             101,
		BBoxJSON:            []byte(`[0.1,0.2,0.3,0.4]`),
		SubBBoxJSON:         []byte(`[0.12,0.22,0.1,0.1]`),
		AttributesJSON:      []byte(`{"has_face":true,"gender":"male"}`),
		IsRecognized:        true,
		ImageID:             "img_pano_100",
		ImageRelPath:        "2026-09-02/img_pano_100.jpg",
		CropImageID:         "img_crop_100",
		CropImageRelPath:    "2026-09-02/img_crop_100.jpg",
		SubCropImageID:      "img_sub_100",
		SubCropImageRelPath: "2026-09-02/img_sub_100.jpg",
	}
	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	// 1. 测试列表查询
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/record/captures?page=1&pageSize=10&targetType=person&isRecognized=true", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pageResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Total int                    `json:"total"`
			Items []*service.CaptureItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pageResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if pageResp.Code != 0 || pageResp.Data.Total != 1 {
		t.Fatalf("expected 1 item with code 0, got %v", pageResp)
	}
	if pageResp.Data.Items[0].EventID != "run-1/100-1-0" {
		t.Fatalf("unexpected event_id: %s", pageResp.Data.Items[0].EventID)
	}

	// 2. 测试根据 ID 获取详情
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/record/captures/%d", record.ID), nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var detailResp struct {
		Code int                  `json:"code"`
		Data *service.CaptureItem `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("unmarshal detail response: %v", err)
	}
	if detailResp.Code != 0 || detailResp.Data == nil || detailResp.Data.ID != record.ID {
		t.Fatalf("unexpected detail: %+v", detailResp)
	}

	// 3. 测试图片流下载
	panoPath := filepath.Join(tmpDir, "2026-09-02", "img_pano_100.jpg")
	cropPath := filepath.Join(tmpDir, "2026-09-02", "img_crop_100.jpg")
	subPath := filepath.Join(tmpDir, "2026-09-02", "img_sub_100.jpg")
	_ = os.MkdirAll(filepath.Dir(panoPath), 0755)
	_ = os.WriteFile(panoPath, []byte("pano-bytes"), 0644)
	_ = os.WriteFile(cropPath, []byte("crop-bytes"), 0644)
	_ = os.WriteFile(subPath, []byte("sub-bytes"), 0644)

	// 获取 panorama
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/record/captures/%d/image?kind=panorama", record.ID), nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "pano-bytes" {
		t.Fatalf("expected pano-bytes, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 获取 crop
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/record/captures/%d/image?kind=crop", record.ID), nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "crop-bytes" {
		t.Fatalf("expected crop-bytes, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 获取 sub_crop
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/record/captures/%d/image?kind=sub_crop", record.ID), nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "sub-bytes" {
		t.Fatalf("expected sub-bytes, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 4. 测试不存在的记录
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/record/captures/999999", nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d", w.Code)
	}
	var notFoundResp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &notFoundResp)
	if notFoundResp.Code != errno.CodeNotFound {
		t.Fatalf("expected not found code %d, got %d", errno.CodeNotFound, notFoundResp.Code)
	}
}
