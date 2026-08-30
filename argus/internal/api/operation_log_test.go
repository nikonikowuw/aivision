package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
	"argus/app/internal/service"
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
		apiGroup.GET("/:id", handler.GetByID)
	}

	return engine, db, srv
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
