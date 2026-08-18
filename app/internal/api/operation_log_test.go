package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

func setupOplogAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB, service.OperationLogService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "oplog")

	repo := repository.NewOperationLogRepository(db)
	srv := service.NewOperationLogService(repo)
	handler := api.NewOperationLogHandler(srv)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())

	apiGroup := engine.Group("/api/oplog")
	{
		apiGroup.GET("/page", handler.GetPage)
		apiGroup.DELETE("/batch", handler.BatchDelete)
		apiGroup.GET("/:id", handler.GetByID)
		apiGroup.DELETE("/:id", handler.Delete)
	}

	return engine, db, srv
}

func doOplogRequest(t *testing.T, engine *gin.Engine, method, path, body string) (int, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp %s %s: %v (body=%s)", method, path, err, rec.Body.String())
	}
	return rec.Code, resp.Code
}

func TestOplogAPI_GetPageAndGetByID(t *testing.T) {
	engine, _, srv := setupOplogAPIEngine(t)
	ctx := context.Background()

	// 插入种子日志
	log1 := &model.OperationLog{
		UserID:     1,
		Username:   "admin",
		Module:     "menu",
		Action:     "POST /api/menu",
		Method:     "POST",
		Path:       "/api/menu",
		StatusCode: 200,
		DurationMs: 15,
		IP:         "127.0.0.1",
		UserAgent:  "curl/7.0",
		Body:       `{"name":"test"}`,
	}
	if err := srv.Record(ctx, log1); err != nil {
		t.Fatalf("record log: %v", err)
	}

	// 1. GET /api/oplog/page
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oplog/page?page=1&pageSize=10&module=menu", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var pageResp struct {
		Code int                   `json:"code"`
		Data service.LogPageResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pageResp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if pageResp.Code != 0 || pageResp.Data.Total != 1 {
		t.Errorf("unexpected page response: %+v", pageResp)
	}
	if pageResp.Data.Items[0].Username != "admin" {
		t.Errorf("username = %s, want admin", pageResp.Data.Items[0].Username)
	}

	// 2. GET /api/oplog/:id
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/oplog/1", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get by id status = %d, want 200", rec.Code)
	}
	var singleResp struct {
		Code int                `json:"code"`
		Data model.OperationLog `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &singleResp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if singleResp.Code != 0 || singleResp.Data.ID != 1 {
		t.Errorf("unexpected single response: %+v", singleResp)
	}

	// 3. GET /api/oplog/999 (不存在)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/oplog/999", nil)
	engine.ServeHTTP(rec, req)

	var errResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal err resp: %v", err)
	}
	if errResp.Code != errno.CodeNotFound {
		t.Errorf("code = %d, want CodeNotFound (%d)", errResp.Code, errno.CodeNotFound)
	}
}

func TestOplogAPI_InvalidID(t *testing.T) {
	engine, _, _ := setupOplogAPIEngine(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oplog/abc", nil)
	engine.ServeHTTP(rec, req)

	var errResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal err resp: %v", err)
	}
	if errResp.Code != errno.CodeInvalidParam {
		t.Errorf("code = %d, want CodeInvalidParam (%d)", errResp.Code, errno.CodeInvalidParam)
	}
}

func TestOplogAPI_DeleteAndBatchDelete(t *testing.T) {
	engine, _, srv := setupOplogAPIEngine(t)
	ctx := context.Background()

	log1 := &model.OperationLog{
		UserID: 1, Username: "admin", Module: "menu", Action: "POST", Path: "/api/menu", StatusCode: 200,
	}
	log2 := &model.OperationLog{
		UserID: 1, Username: "admin", Module: "user", Action: "POST", Path: "/api/user", StatusCode: 200,
	}
	_ = srv.Record(ctx, log1)
	_ = srv.Record(ctx, log2)

	// 1. 删除单条
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/oplog/%d", log1.ID), nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete log status = %d, want 200", rec.Code)
	}

	// 2. 批量删除
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/oplog/batch", bytes.NewBufferString(fmt.Sprintf(`{"ids":[%d]}`, log2.ID)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete log status = %d, want 200", rec.Code)
	}
	if _, code := doOplogRequest(t, engine, http.MethodDelete, "/api/oplog/batch", `{"ids":[0]}`); code != errno.CodeInvalidParam {
		t.Fatalf("batch delete zero id: code = %d, want 1009", code)
	}
}
