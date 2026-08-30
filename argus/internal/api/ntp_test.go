package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/ntp"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/repository"
	"argus/app/internal/router"
	"argus/app/internal/service"
)

// ntpAPIResp 对时接口统一响应体。
type ntpAPIResp struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

// setupNTPAPIEngine 用真实 sqlite + 真实 service 装配 NTP handler 路由（不带认证/权限中间件，
// 供业务成功/错误码测试使用）。返回可注入底层错误的 MockExecutor。
func setupNTPAPIEngine(t *testing.T) (*gin.Engine, *ntp.MockExecutor) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "ntp")
	repo := repository.NewSystemConfigRepository(db)
	mockExec := ntp.NewMockExecutor()
	srv := service.NewNTPService(repo, mockExec, zap.NewNop())
	handler := api.NewNTPHandler(srv)

	r := gin.New()
	r.Use(middleware.ErrorHandler())

	grp := r.Group("/api/ntp")
	{
		grp.GET("/config", handler.GetConfig)
		grp.PUT("/config", handler.UpdateConfig)
		grp.GET("/status", handler.GetStatus)
		grp.POST("/sync", handler.SyncNow)
		grp.POST("/set-time", handler.SetTime)
		grp.GET("/synced", handler.IsSynced)
	}

	return r, mockExec
}

// doNTPReq 发起请求并解析统一响应体 {code,data,message}。
func doNTPReq(t *testing.T, r *gin.Engine, method, path, body string) (*httptest.ResponseRecorder, ntpAPIResp) {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var resp ntpAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	return rec, resp
}

func TestNTPHandler(t *testing.T) {
	t.Run("GET /api/ntp/config", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodGet, "/api/ntp/config", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}
		var data struct {
			Mode    string   `json:"mode"`
			Servers []string `json:"servers"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if data.Mode != "ntp" {
			t.Errorf("mode = %q, want ntp", data.Mode)
		}
		if len(data.Servers) == 0 {
			t.Error("servers empty, want non-empty default")
		}
	})

	t.Run("PUT /api/ntp/config success", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		body := `{"mode":"ntp","servers":["pool.ntp.org","ntp.aliyun.com"]}`
		rec, resp := doNTPReq(t, r, http.MethodPut, "/api/ntp/config", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}
	})

	t.Run("GET /api/ntp/status", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodGet, "/api/ntp/status", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}
		var data struct {
			Synced bool `json:"synced"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if !data.Synced {
			t.Error("synced = false, want true (mock 默认已同步)")
		}
	})

	t.Run("POST /api/ntp/sync", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/sync", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}
	})

	t.Run("POST /api/ntp/set-time success and auto-switch manual", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		body := `{"time":"2025-08-22T14:30:00Z"}`
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/set-time", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}

		// 契约：设时后配置模式自动切换并持久化为 manual
		_, cfgResp := doNTPReq(t, r, http.MethodGet, "/api/ntp/config", "")
		if cfgResp.Code != errno.CodeOK {
			t.Fatalf("GET config code = %d, want %d", cfgResp.Code, errno.CodeOK)
		}
		var cfg struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(cfgResp.Data, &cfg); err != nil {
			t.Fatalf("unmarshal config data: %v", err)
		}
		if cfg.Mode != "manual" {
			t.Errorf("mode = %q, want manual (set-time 应自动切换)", cfg.Mode)
		}
	})

	t.Run("GET /api/ntp/synced", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodGet, "/api/ntp/synced", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		if resp.Code != errno.CodeOK {
			t.Fatalf("code = %d, want %d", resp.Code, errno.CodeOK)
		}
		var data struct {
			Synced bool `json:"synced"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if !data.Synced {
			t.Error("synced = false, want true")
		}
	})
}

func TestNTPHandlerErrors(t *testing.T) {
	t.Run("1009 missing mode", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodPut, "/api/ntp/config", `{}`)
		if resp.Code != errno.CodeInvalidParam {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeInvalidParam, rec.Body.String())
		}
	})

	t.Run("1009 invalid time format", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/set-time", `{"time":"not-a-time"}`)
		if resp.Code != errno.CodeInvalidParam {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeInvalidParam, rec.Body.String())
		}
	})

	t.Run("1203 ntp empty servers", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodPut, "/api/ntp/config", `{"mode":"ntp","servers":[]}`)
		if resp.Code != errno.CodeNTPServersEmpty {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPServersEmpty, rec.Body.String())
		}
	})

	t.Run("1204 invalid mode", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		rec, resp := doNTPReq(t, r, http.MethodPut, "/api/ntp/config", `{"mode":"bogus"}`)
		if resp.Code != errno.CodeNTPInvalidMode {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPInvalidMode, rec.Body.String())
		}
	})

	t.Run("1202 sync in manual mode", func(t *testing.T) {
		r, _ := setupNTPAPIEngine(t)
		// 先切到 manual，再触发同步
		if _, resp := doNTPReq(t, r, http.MethodPut, "/api/ntp/config", `{"mode":"manual","servers":[]}`); resp.Code != errno.CodeOK {
			t.Fatalf("switch to manual failed: code = %d", resp.Code)
		}
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/sync", "")
		if resp.Code != errno.CodeNTPSyncNotAllowedInManualMode {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPSyncNotAllowedInManualMode, rec.Body.String())
		}
	})

	t.Run("1206 sync failed", func(t *testing.T) {
		r, exec := setupNTPAPIEngine(t)
		exec.SyncErr = errors.New("chronyc makestep failed")
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/sync", "")
		if resp.Code != errno.CodeNTPSyncFailed {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPSyncFailed, rec.Body.String())
		}
	})

	t.Run("1205 set time failed", func(t *testing.T) {
		r, exec := setupNTPAPIEngine(t)
		exec.SetTimeErr = errors.New("no root permission")
		rec, resp := doNTPReq(t, r, http.MethodPost, "/api/ntp/set-time", `{"time":"2025-08-22T14:30:00Z"}`)
		if resp.Code != errno.CodeNTPSetTimeFailed {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPSetTimeFailed, rec.Body.String())
		}
	})

	t.Run("1207 executor unavailable", func(t *testing.T) {
		r, exec := setupNTPAPIEngine(t)
		exec.StatusErr = errors.New("executor broken")
		rec, resp := doNTPReq(t, r, http.MethodGet, "/api/ntp/status", "")
		if resp.Code != errno.CodeNTPExecutorUnavailable {
			t.Fatalf("code = %d, want %d (body=%s)", resp.Code, errno.CodeNTPExecutorUnavailable, rec.Body.String())
		}
	})
}

// setupNTPFullApp 装配完整路由栈（认证 + 权限 + 操作日志 + NTP），用于端到端鉴权测试。
func setupNTPFullApp(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "ntp-full")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	cfg := &config.Config{
		JWT: config.JWT{
			Secret:       "api-test-jwt-secret-123456",
			AccessTTL:    time.Hour,
			RefreshTTL:   7 * 24 * time.Hour,
			SecureCookie: true,
		},
		Log:     config.Log{Level: "release"},
		Storage: config.Storage{MaxSize: 10 * 1024 * 1024},
	}

	authRepo := repository.NewAuthRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	deptRepo := repository.NewDepartmentRepository(db)
	userRepo := repository.NewUserRepository(db)
	oplogRepo := repository.NewOperationLogRepository(db)
	sysCfgRepo := repository.NewSystemConfigRepository(db)

	authSvc := service.NewAuthService(authRepo, userRepo, menuRepo, cfg)
	menuSvc := service.NewMenuService(menuRepo)
	roleSvc := service.NewRoleService(roleRepo, menuRepo)
	deptSvc := service.NewDeptService(deptRepo)
	userSvc := service.NewUserService(userRepo, deptRepo, roleRepo)
	oplogSvc := service.NewOperationLogService(oplogRepo)
	ntpSvc := service.NewNTPService(sysCfgRepo, ntp.NewMockExecutor(), zap.NewNop())

	authMid := middleware.NewAuthMiddleware(authRepo, cfg)
	permMid := middleware.NewPermMiddleware(menuRepo)
	oplogMid := middleware.NewOplogMiddleware(oplogSvc, zap.NewNop())

	authHandler := api.NewAuthHandler(authSvc, authMid, cfg)
	menuHandler := api.NewMenuHandler(menuSvc)
	roleHandler := api.NewRoleHandler(roleSvc)
	deptHandler := api.NewDepartmentHandler(deptSvc)
	userHandler := api.NewUserHandler(userSvc)
	oplogHandler := api.NewOperationLogHandler(oplogSvc)
	ntpHandler := api.NewNTPHandler(ntpSvc)

	deps := router.Deps{
		ErrorHandler:        middleware.ErrorHandler(),
		AuthMiddleware:      authMid,
		PermMiddleware:      permMid,
		OplogMiddleware:     oplogMid,
		MenuHandler:         menuHandler,
		RoleHandler:         roleHandler,
		DepartmentHandler:   deptHandler,
		OperationLogHandler: oplogHandler,
		UserHandler:         userHandler,
		AuthHandler:         authHandler,
		FileHandler:         api.NewFileHandler(service.NewFileService(storage.NopStorage(), cfg), cfg),
		NTPHandler:          ntpHandler,
	}

	return router.New(cfg, deps), db
}

func TestNTPAuthAndPermission(t *testing.T) {
	engine, db := setupNTPFullApp(t)

	// 种子用户：super 管理员 + 有角色但无菜单权限的普通用户
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	admin := model.User{Username: "admin", Password: string(hash), Nickname: "管理员", Status: model.StatusEnabled}
	db.Create(&admin)
	superRole := model.Role{Name: "超级管理员", Code: model.RoleSuperCode, Status: model.StatusEnabled}
	db.Create(&superRole)
	db.Create(&model.UserRole{UserID: admin.ID, RoleID: superRole.ID})

	normalUser := model.User{Username: "normal", Password: string(hash), Nickname: "普通用户", Status: model.StatusEnabled}
	db.Create(&normalUser)
	normalRole := model.Role{Name: "普通角色", Code: "normal", Status: model.StatusEnabled}
	db.Create(&normalRole)
	db.Create(&model.UserRole{UserID: normalUser.ID, RoleID: normalRole.ID})

	login := func(username string) string {
		t.Helper()
		body := fmt.Sprintf(`{"username":%q,"password":"admin123"}`, username)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login %s status = %d, body = %s", username, rec.Code, rec.Body.String())
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				AccessToken string `json:"accessToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal login %s: %v", username, err)
		}
		if resp.Code != errno.CodeOK || resp.Data.AccessToken == "" {
			t.Fatalf("login %s code = %d, accessToken empty", username, resp.Code)
		}
		return resp.Data.AccessToken
	}

	adminToken := login("admin")
	normalToken := login("normal")

	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// 1. 无 token → 401
	if rec := do(http.MethodGet, "/api/ntp/config", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/ntp/config without token status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/ntp/synced", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/ntp/synced without token status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}

	// 2. super 用户：读 / 写 / synced 全部放行
	if rec := do(http.MethodGet, "/api/ntp/config", adminToken, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/ntp/config (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPut, "/api/ntp/config", adminToken, `{"mode":"ntp","servers":["pool.ntp.org"]}`); rec.Code != http.StatusOK {
		t.Errorf("PUT /api/ntp/config (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/ntp/synced", adminToken, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/ntp/synced (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// 3. 普通用户（无 ops 权限）：读 / 写 → 403；synced 仅要求认证 → 200
	if rec := do(http.MethodGet, "/api/ntp/config", normalToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/ntp/config (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPut, "/api/ntp/config", normalToken, `{"mode":"manual","servers":[]}`); rec.Code != http.StatusForbidden {
		t.Errorf("PUT /api/ntp/config (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/ntp/sync", normalToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/ntp/sync (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/ntp/set-time", normalToken, `{"time":"2025-08-22T14:30:00Z"}`); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/ntp/set-time (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/ntp/status", normalToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/ntp/status (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/ntp/synced", normalToken, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/ntp/synced (normal) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}
