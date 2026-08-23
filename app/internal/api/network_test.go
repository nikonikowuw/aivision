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
	"niko-vue-admin/app/internal/pkg/errno"
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
		group.PUT("/mode", handler.SwitchMode)
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

func TestNetworkAPI_SwitchMode(t *testing.T) {
	router, _ := setupTestNetworkRouter(t)

	// 合法切换：返回 pending_confirmation
	body := []byte(`{
		"mode":"active-backup",
		"bond":{
			"slaveIds":["eth0","eth1"],
			"primarySlaveId":"eth0",
			"ipv4":{"mode":"static","primary":true,"address":"192.168.9.9","prefix":24,"gateway":"192.168.9.1","dnsServers":["192.168.9.1"]}
		}
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/network/mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/network/mode status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	if resp.Data.Status != string(netconfig.TxnStatusPendingConfirmation) {
		t.Errorf("status = %q, want pending_confirmation", resp.Data.Status)
	}
}

func TestNetworkAPI_SwitchMode_SlaveInvalid(t *testing.T) {
	router, _ := setupTestNetworkRouter(t)

	// primary 不在 slave 集合内 → 1112
	body := []byte(`{
		"mode":"active-backup",
		"bond":{
			"slaveIds":["eth0","eth1"],
			"primarySlaveId":"wlan0",
			"ipv4":{"mode":"dhcp"}
		}
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/network/mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (409) for slave invalid", w.Code, http.StatusConflict)
	}
	var resp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != errno.CodeNetworkBondSlaveInvalid {
		t.Errorf("code = %d, want %d", resp.Code, errno.CodeNetworkBondSlaveInvalid)
	}
}

func TestNetworkAPI_SwitchMode_MultiAddressWithBondRejected(t *testing.T) {
	router, _ := setupTestNetworkRouter(t)

	// multi-address 携带 bond 字段 → 400 CodeInvalidParam
	body := []byte(`{
		"mode":"multi-address",
		"bond":{
			"slaveIds":["eth0","eth1"],
			"primarySlaveId":"eth0",
			"ipv4":{"mode":"dhcp"}
		}
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/network/mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (400) for multi-address with bond", w.Code, http.StatusBadRequest)
	}
	var resp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != errno.CodeInvalidParam {
		t.Errorf("code = %d, want %d", resp.Code, errno.CodeInvalidParam)
	}
}

