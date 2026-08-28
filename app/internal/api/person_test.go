package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

func setupPersonAPIEngine(t *testing.T, allowedIPs []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "person")
	repo := repository.NewPersonRepository(db)
	svc := service.NewPersonService(repo)
	handler := api.NewPersonHandler(svc)

	cfg := &config.Config{
		Open: config.Open{
			PersonSyncAllowedIPs: allowedIPs,
		},
	}
	ipMw := middleware.NewOpenPersonIPWhitelistMiddleware(cfg)

	r := gin.New()
	r.Use(middleware.ErrorHandler())

	// 模拟已认证页面路由组
	personGrp := r.Group("/api/person")
	{
		personGrp.GET("/page", handler.GetPage)
		personGrp.POST("", handler.CreatePerson)
		personGrp.DELETE("/batch", handler.BatchDeletePerson)
		personGrp.PUT("/:personId", handler.UpdatePerson)
		personGrp.DELETE("/:personId", handler.DeletePerson)
	}

	// 开放同步路由组
	openGrp := r.Group("/api/v1/open/person")
	openGrp.Use(ipMw.Handler)
	{
		openGrp.PUT("/:personId", handler.SyncUpsertPerson)
		openGrp.DELETE("/:personId", handler.SyncDeletePerson)
	}

	return r
}

func TestPersonPageAndCRUDAPI(t *testing.T) {
	r := setupPersonAPIEngine(t, []string{"192.168.1.100"})

	// 1. Create Person (POST /api/person)
	createBody := map[string]any{
		"personId": "EMP001",
		"name":     "Bob",
	}
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/person", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("create person got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			PersonID  string `json:"personId"`
			Name      string `json:"name"`
			CreatedAt string `json:"createdAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Data.PersonID != "EMP001" || resp.Data.Name != "Bob" {
		t.Fatalf("unexpected create response: %+v", resp)
	}

	// 2. Query Page (GET /api/person/page)
	req = httptest.NewRequest(http.MethodGet, "/api/person/page?page=1&pageSize=10", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query page got %d: %s", w.Code, w.Body.String())
	}

	// 3. Update Person Name (PUT /api/person/:personId)
	updateBody := map[string]any{"name": "Bob Updated"}
	body, _ = json.Marshal(updateBody)
	req = httptest.NewRequest(http.MethodPut, "/api/person/EMP001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update person got %d: %s", w.Code, w.Body.String())
	}

	// 4. Batch Delete Person (DELETE /api/person/batch)
	batchBody := map[string]any{"personIds": []string{"EMP001"}}
	body, _ = json.Marshal(batchBody)
	req = httptest.NewRequest(http.MethodDelete, "/api/person/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch delete got %d: %s", w.Code, w.Body.String())
	}

	// 5. Batch delete rejects identifiers that do not satisfy the path format.
	invalidBatchBody := map[string]any{"personIds": []string{"bad/id"}}
	body, _ = json.Marshal(invalidBatchBody)
	req = httptest.NewRequest(http.MethodDelete, "/api/person/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid batch delete got %d: %s", w.Code, w.Body.String())
	}
	var invalidBatchResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invalidBatchResp); err != nil {
		t.Fatalf("decode invalid batch response: %v", err)
	}
	if invalidBatchResp.Code != errno.CodeInvalidParam {
		t.Fatalf("invalid batch code = %d, want %d", invalidBatchResp.Code, errno.CodeInvalidParam)
	}
}

func TestOpenPersonSyncAPIRoutes(t *testing.T) {
	r := setupPersonAPIEngine(t, []string{"192.168.1.100"})

	// 1. Forbidden without whitelist IP
	body, _ := json.Marshal(map[string]any{"name": "Open Alice"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/open/person/OP001", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.200:1234"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", w.Code)
	}

	// 2. Success with whitelist IP (No JWT required)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/open/person/OP001", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.100:1234"
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open upsert got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			PersonID string `json:"personId"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data.PersonID != "OP001" {
		t.Fatalf("unexpected open upsert resp: %+v", resp)
	}

	// 3. Delete open person idempotent
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/open/person/OP001", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open delete got %d: %s", w.Code, w.Body.String())
	}

	// Repeat delete still 200
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/open/person/OP001", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("repeat open delete got %d: %s", w.Code, w.Body.String())
	}
}
