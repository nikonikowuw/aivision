package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

type storageAPIResp struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

func setupStorageAPIEngine(t *testing.T) (*gin.Engine, service.StorageCleanupService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "storage")
	sysRepo := repository.NewSystemConfigRepository(db)
	alarmRepo := repository.NewAlarmRecordRepository(db)
	plateRepo := repository.NewPlateObservationRepository(db)
	faceRepo := repository.NewFaceObservationRepository(db)
	faceCaptureRepo := repository.NewFaceCaptureRepository(db)
	opLogRepo := repository.NewOperationLogRepository(db)

	tempDir := t.TempDir()
	fileStorage, err := storage.NewLocalStorage(tempDir, "/uploads")
	require.NoError(t, err)

	sampler := storage.NewMockDiskUsageSampler(storage.DiskUsage{
		TotalBytes:   100000000,
		UsedBytes:    40000000,
		FreeBytes:    60000000,
		UsagePercent: 40.0,
	}, nil)

	cfg := &config.Config{
		Storage: config.Storage{
			Local: config.Local{Root: tempDir},
		},
	}

	srv := service.NewStorageCleanupService(
		cfg, sysRepo, alarmRepo, plateRepo, faceRepo, faceCaptureRepo, opLogRepo, fileStorage, sampler, zap.NewNop(),
	)
	handler := api.NewStorageHandler(srv, zap.NewNop())

	r := gin.New()
	r.Use(middleware.ErrorHandler())

	grp := r.Group("/api/storage")
	{
		grp.GET("/status", handler.GetStorageStatus)
		grp.GET("/config", handler.GetStorageConfig)
		grp.PUT("/config", handler.UpdateStorageConfig)
		grp.POST("/cleanup", handler.TriggerCleanup)
	}

	return r, srv
}

func doStorageReq(t *testing.T, r *gin.Engine, method, path, body string) (*httptest.ResponseRecorder, storageAPIResp) {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var resp storageAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	return rec, resp
}

func TestStorageHandler(t *testing.T) {
	r, _ := setupStorageAPIEngine(t)

	t.Run("GET /api/storage/status", func(t *testing.T) {
		rec, resp := doStorageReq(t, r, http.MethodGet, "/api/storage/status", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, errno.CodeOK, resp.Code)

		var status service.StorageStatusDTO
		require.NoError(t, json.Unmarshal(resp.Data, &status))
		assert.Equal(t, uint64(100000000), status.TotalBytes)
		assert.Equal(t, 40.0, status.UsagePercent)
		assert.Equal(t, "normal", status.Status)
		assert.False(t, status.CircuitBreakerActive)
	})

	t.Run("GET /api/storage/config", func(t *testing.T) {
		rec, resp := doStorageReq(t, r, http.MethodGet, "/api/storage/config", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, errno.CodeOK, resp.Code)

		var cfg model.StorageRetentionConfigValue
		require.NoError(t, json.Unmarshal(resp.Data, &cfg))
		assert.Equal(t, 30, cfg.RetentionDays)
		assert.Equal(t, 85, cfg.HighWatermarkPercent)
		assert.Equal(t, 70, cfg.LowWatermarkPercent)
		assert.Equal(t, 600, cfg.CheckIntervalSeconds)
		assert.True(t, cfg.AutoCleanupEnabled)
	})

	t.Run("PUT /api/storage/config - valid", func(t *testing.T) {
		body := `{
			"retentionDays": 15,
			"highWatermarkPercent": 80,
			"lowWatermarkPercent": 60,
			"checkIntervalSeconds": 300,
			"autoCleanupEnabled": true
		}`
		rec, resp := doStorageReq(t, r, http.MethodPut, "/api/storage/config", body)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, errno.CodeOK, resp.Code)

		// Verify retrieval
		_, getResp := doStorageReq(t, r, http.MethodGet, "/api/storage/config", "")
		var cfg model.StorageRetentionConfigValue
		require.NoError(t, json.Unmarshal(getResp.Data, &cfg))
		assert.Equal(t, 15, cfg.RetentionDays)
		assert.Equal(t, 80, cfg.HighWatermarkPercent)
		assert.Equal(t, 60, cfg.LowWatermarkPercent)
		assert.Equal(t, 300, cfg.CheckIntervalSeconds)
	})

	t.Run("PUT /api/storage/config - invalid range", func(t *testing.T) {
		body := `{
			"retentionDays": 0,
			"highWatermarkPercent": 80,
			"lowWatermarkPercent": 60,
			"checkIntervalSeconds": 300,
			"autoCleanupEnabled": true
		}`
		rec, resp := doStorageReq(t, r, http.MethodPut, "/api/storage/config", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, errno.CodeStorageInvalidConfig, resp.Code)
	})

	t.Run("PUT /api/storage/config - low >= high", func(t *testing.T) {
		body := `{
			"retentionDays": 15,
			"highWatermarkPercent": 70,
			"lowWatermarkPercent": 75,
			"checkIntervalSeconds": 300,
			"autoCleanupEnabled": true
		}`
		rec, resp := doStorageReq(t, r, http.MethodPut, "/api/storage/config", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, errno.CodeStorageInvalidConfig, resp.Code)
	})

	t.Run("POST /api/storage/cleanup", func(t *testing.T) {
		rec, resp := doStorageReq(t, r, http.MethodPost, "/api/storage/cleanup", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, errno.CodeOK, resp.Code)
	})
}
