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
	"argus/app/internal/pkg/response"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func setupPlateObservationAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB, repository.PlateObservationRepository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	_ = os.Setenv("AIVISION_IMAGE_DIR", tmpDir)

	db := newTestAPIDB(t, "plate_observation")

	plateRepo := repository.NewPlateObservationRepository(db)
	camRepo := repository.NewCameraRepository(db)

	cfg := &config.Config{}
	svc := service.NewPlateObservationService(plateRepo, camRepo, cfg)
	handler := api.NewPlateObservationHandler(svc)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())

	apiGroup := engine.Group("/api/record")
	{
		apiGroup.GET("/plates", handler.ListPage)
		apiGroup.GET("/plates/:id", handler.GetDetail)
		apiGroup.GET("/plates/:id/panorama", handler.ReadPanoramaImage)
		apiGroup.GET("/plates/:id/plate", handler.ReadPlateImage)
	}

	v1Group := engine.Group("/api/v1/plate-observations")
	{
		v1Group.GET("", handler.ListPage)
		v1Group.GET("/:id", handler.GetDetail)
		v1Group.GET("/:id/panorama", handler.ReadPanoramaImage)
		v1Group.GET("/:id/plate", handler.ReadPlateImage)
	}

	return engine, db, plateRepo, tmpDir
}

func TestPlateObservationAPI_ListPageAndDetail(t *testing.T) {
	engine, _, repo, _ := setupPlateObservationAPIEngine(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	record := &model.PlateObservation{
		EventID:        "evt-api-1",
		TaskID:         "task-1",
		InstanceID:     "inst-1",
		CameraID:       "cam-1",
		CameraName:     "South Gate",
		PlateText:      "粤B12345",
		NormalizedText: "粤B12345",
		PlateColor:     "blue",
		PlateType:      "standard",
		Confidence:     0.95,
		OcrConfidence:  0.92,
		TrackID:        12,
		ObservedAt:     now,
	}
	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create plate observation: %v", err)
	}

	// 1. List
	req := httptest.NewRequest(http.MethodGet, "/api/record/plates?plateText=12345", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var res response.Result
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("res.Code = %d, want 0", res.Code)
	}

	// 2. Detail
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/plate-observations/%d", record.ID), nil)
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("detail status code = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestPlateObservationAPI_ImageStreaming(t *testing.T) {
	engine, _, repo, tmpDir := setupPlateObservationAPIEngine(t)
	ctx := context.Background()

	// Write mock image file
	subDir := filepath.Join(tmpDir, "2026/08/31")
	_ = os.MkdirAll(subDir, 0755)
	imgFile := filepath.Join(subDir, "test-plate.jpg")
	_ = os.WriteFile(imgFile, []byte("fake-jpeg-content"), 0644)

	record := &model.PlateObservation{
		EventID:       "evt-img-1",
		CameraID:      "cam-1",
		PlateText:     "粤B12345",
		PanoramaImage: "2026/08/31/test-plate.jpg",
		PlateImage:    "2026/08/31/test-plate.jpg",
		ObservedAt:    time.Now(),
	}
	if err := repo.Create(ctx, record); err != nil {
		t.Fatalf("create plate record: %v", err)
	}

	// Read Panorama
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/plate-observations/%d/panorama", record.ID), nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("panorama image status code = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("content-type = %s, want image/jpeg", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != "fake-jpeg-content" {
		t.Errorf("image content = %s, want fake-jpeg-content", w.Body.String())
	}
}
