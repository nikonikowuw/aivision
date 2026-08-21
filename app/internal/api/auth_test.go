package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/router"
	"niko-vue-admin/app/internal/service"
)

func setupAuthAPITestApp(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "auth")
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

	authSvc := service.NewAuthService(authRepo, userRepo, menuRepo, cfg)
	menuSvc := service.NewMenuService(menuRepo)
	roleSvc := service.NewRoleService(roleRepo, menuRepo)
	deptSvc := service.NewDeptService(deptRepo)
	userSvc := service.NewUserService(userRepo, deptRepo, roleRepo)
	oplogSvc := service.NewOperationLogService(oplogRepo)

	authMid := middleware.NewAuthMiddleware(authRepo, cfg)
	permMid := middleware.NewPermMiddleware(menuRepo)
	oplogMid := middleware.NewOplogMiddleware(oplogSvc, zap.NewNop())

	authHandler := api.NewAuthHandler(authSvc, authMid, cfg)
	menuHandler := api.NewMenuHandler(menuSvc)
	roleHandler := api.NewRoleHandler(roleSvc)
	deptHandler := api.NewDepartmentHandler(deptSvc)
	userHandler := api.NewUserHandler(userSvc)
	oplogHandler := api.NewOperationLogHandler(oplogSvc)

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
		FileHandler:         api.NewFileHandler(service.NewFileService(nil, cfg), cfg),
	}

	engine := router.New(cfg, deps)
	return engine, db
}

type failingLogoutAuthService struct {
	service.AuthService
	err error
}

func (s failingLogoutAuthService) Logout(context.Context, string) (*service.LogoutOperator, error) {
	return nil, s.err
}

func TestAuthAPIEndToEnd(t *testing.T) {
	engine, db := setupAuthAPITestApp(t)

	// 初始化种子用户与角色
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	admin := model.User{
		Username: "admin",
		Password: string(hash),
		Nickname: "管理员",
		Status:   model.StatusEnabled,
	}
	db.Create(&admin)
	superRole := model.Role{
		Name:   "超级管理员",
		Code:   model.RoleSuperCode,
		Status: model.StatusEnabled,
	}
	db.Create(&superRole)
	db.Create(&model.UserRole{UserID: admin.ID, RoleID: superRole.ID})

	// 1. 登录成功
	loginBody := `{"username":"admin","password":"admin123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(loginBody))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string   `json:"accessToken"`
			UserID      string   `json:"userId"`
			Username    string   `json:"username"`
			RealName    string   `json:"realName"`
			Roles       []string `json:"roles"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}

	if loginResp.Code != 0 || loginResp.Data.AccessToken == "" {
		t.Fatalf("unexpected login response: %+v", loginResp)
	}
	if strings.Contains(rec.Body.String(), `"refreshToken"`) {
		t.Fatalf("login response must not expose refreshToken: %s", rec.Body.String())
	}

	// 检查 Cookie 中是否包含 jwt
	cookieHeader := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, "jwt=") || !strings.Contains(cookieHeader, "HttpOnly") || !strings.Contains(cookieHeader, "Secure") {
		t.Fatalf("expected HttpOnly jwt cookie, got: %s", cookieHeader)
	}

	var jwtCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jwt" {
			jwtCookie = c
			break
		}
	}
	if jwtCookie == nil || jwtCookie.Value == "" || !jwtCookie.Secure {
		t.Fatalf("jwt cookie missing, empty, or not Secure: %+v", jwtCookie)
	}

	// 2. 带 token 访问受保护接口 /api/user/info
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/user/info status = %d, want 200", rec.Code)
	}

	// 3. 访问 /api/auth/codes
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/auth/codes", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/codes status = %d, want 200", rec.Code)
	}
	var codesResp response.Result
	_ = json.Unmarshal(rec.Body.Bytes(), &codesResp)
	codesSlice, ok := codesResp.Data.([]any)
	if !ok || len(codesSlice) != 1 || codesSlice[0] != "*" {
		t.Errorf("codes = %v, want ['*']", codesResp.Data)
	}

	// 4. 无 token 或伪造 token 访问 -> 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user/info", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user/info", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", rec.Code)
	}

	// 5. 刷新 token (POST /api/auth/refresh 带 cookie)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(jwtCookie)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	newAccessToken := rec.Body.String()
	if newAccessToken == "" || strings.HasPrefix(newAccessToken, "{") {
		t.Fatalf("expected raw token string, got %s", newAccessToken)
	}

	// 刷新后检查旧 refresh token 是否已被 revoke
	var oldRT model.RefreshToken
	db.Where("token = ?", jwtCookie.Value).First(&oldRT)
	if !oldRT.Revoked {
		t.Errorf("old refresh token was not revoked in db")
	}

	// 获取新的 cookie 并再次使用新 access token 访问接口
	var newJwtCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jwt" {
			newJwtCookie = c
			break
		}
	}
	if newJwtCookie == nil || newJwtCookie.Value == jwtCookie.Value {
		t.Fatalf("expected new rotated jwt cookie")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+newAccessToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("new access token failed: %d", rec.Code)
	}

	// 6. 用旧 refresh token 再次刷新 -> 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(jwtCookie)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked refresh status = %d, want 401", rec.Code)
	}

	// 7. 登出 (POST /api/auth/logout)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+newAccessToken)
	req.AddCookie(newJwtCookie)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec.Code)
	}

	// 7.1 登出日志应记录操作人（异步落库，轮询等待）
	var logoutOplog model.OperationLog
	found := false
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		if err := db.Where("path = ? AND method = ?", "/api/auth/logout", "POST").Last(&logoutOplog).Error; err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("find logout oplog: record not found after waiting")
	}
	if logoutOplog.UserID == 0 || logoutOplog.Username == "" {
		t.Fatalf("logout oplog missing operator: user_id=%d, username=%q", logoutOplog.UserID, logoutOplog.Username)
	}

	var logoutRT model.RefreshToken
	db.Where("token = ?", newJwtCookie.Value).First(&logoutRT)
	if !logoutRT.Revoked {
		t.Errorf("refresh token was not revoked on logout")
	}

	// 7.2 测试伪造 Bearer Token 登出时不会记录伪造操作人身份
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer forged.invalid.token")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated logout status = %d, want 200", rec.Code)
	}

	var forgedLogoutOplog model.OperationLog
	foundForged := false
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		if err := db.Where("path = ? AND method = ?", "/api/auth/logout", "POST").Order("id desc").First(&forgedLogoutOplog).Error; err == nil {
			if forgedLogoutOplog.ID != logoutOplog.ID {
				foundForged = true
				break
			}
		}
	}
	if !foundForged {
		t.Fatalf("find forged logout oplog: record not found after waiting")
	}
	if forgedLogoutOplog.UserID != 0 || forgedLogoutOplog.Username != "" {
		t.Fatalf("forged token should not set operator: user_id=%d, username=%q", forgedLogoutOplog.UserID, forgedLogoutOplog.Username)
	}
}

func TestAuthAPIFailures(t *testing.T) {
	engine, db := setupAuthAPITestApp(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	adminUser := model.User{
		Username: "admin",
		Password: string(hash),
		Status:   model.StatusEnabled,
	}
	db.Create(&adminUser)

	disabledUser := model.User{
		Username: "disabled_user",
		Password: string(hash),
		Status:   model.StatusDisabled,
	}
	db.Create(&disabledUser)
	db.Model(&model.User{}).Where("id = ?", disabledUser.ID).Update("status", model.StatusDisabled)

	rolelessUser := model.User{
		Username: "roleless_user",
		Password: string(hash),
		Status:   model.StatusEnabled,
	}
	db.Create(&rolelessUser)

	// 1. 错误密码
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	var resp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != errno.CodeBadCredential {
		t.Errorf("wrong password code = %d, want %d", resp.Code, errno.CodeBadCredential)
	}

	// 2. 禁用用户登录
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"disabled_user","password":"admin123"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != errno.CodeUserDisabled {
		t.Errorf("disabled user code = %d, want %d", resp.Code, errno.CodeUserDisabled)
	}

	// 3. 启用但无角色的用户不能建立可用会话，也不应产生 refresh token
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"roleless_user","password":"admin123"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("roleless user status = %d, want 401", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != errno.CodeUnauthorized {
		t.Errorf("roleless user code = %d, want %d", resp.Code, errno.CodeUnauthorized)
	}
	var tokenCount int64
	db.Model(&model.RefreshToken{}).Where("user_id = ?", rolelessUser.ID).Count(&tokenCount)
	if tokenCount != 0 {
		t.Errorf("roleless login created %d refresh tokens, want 0", tokenCount)
	}
}

func TestAuthAPILogoutPropagatesFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWT{RefreshTTL: time.Hour}}
	handler := api.NewAuthHandler(failingLogoutAuthService{err: errors.New("database unavailable")}, nil, cfg)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/logout", handler.Logout)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "refresh-token"})
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want 500, body = %s", rec.Code, rec.Body.String())
	}
	var body response.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode logout response: %v", err)
	}
	if body.Code != errno.CodeInternal {
		t.Fatalf("logout code = %d, want %d", body.Code, errno.CodeInternal)
	}
}
