package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/netconfig"
	"niko-vue-admin/app/internal/service"
)

func setupTestNetworkRouter(t *testing.T) (*gin.Engine, NetworkServiceMock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())

	tmpDir := filepath.Join(t.TempDir(), "net-api-state")
	cfg := &config.Config{
		Network: config.Network{
			StateDir:       tmpDir,
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 120 * time.Second,
			FakePlatform:   true,
		},
	}
	srv, err := service.NewNetworkService(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewNetworkService failed: %v", err)
	}
	_ = srv.Start(context.Background())

	handler := NewNetworkHandler(srv)

	group := r.Group("/api/network")
	{
		group.GET("", handler.GetOverview)
		group.GET("/transactions/:transactionId", handler.GetTransaction)
		group.PUT("/interfaces/:interfaceId", handler.ApplyInterface)
		group.POST("/transactions/:transactionId/confirm", handler.ConfirmTransaction)
		group.POST("/transactions/:transactionId/cancel", handler.CancelTransaction)
		group.POST("/interfaces/:interfaceId/factory-reset", handler.FactoryReset)
	}

	return r, srv
}

type NetworkServiceMock interface {
	GetOverview(ctx context.Context) (*netconfig.NetworkOverview, error)
}

func TestNetworkAPI_GetOverview(t *testing.T) {
	router, _ := setupTestNetworkRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/network", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/network status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Code int                       `json:"code"`
		Data netconfig.NetworkOverview `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal response failed: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("got code = %d, want 0", resp.Code)
	}
	if len(resp.Data.Interfaces) != 3 {
		t.Errorf("got %d interfaces, want 3", len(resp.Data.Interfaces))
	}
}

func TestNetworkAPI_ApplyInvalid(t *testing.T) {
	router, _ := setupTestNetworkRouter(t)

	// 非法 IP 测试
	body := []byte(`{"mode":"static","primary":false,"address":"999.999.999.999","prefix":24}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/network/interfaces/eth0", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/network/interfaces/eth0 with invalid IP status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
