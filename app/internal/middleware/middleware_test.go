package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/mask"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

func setupMiddlewareTestApp(t *testing.T) (*gin.Engine, *gorm.DB, service.OperationLogService, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_busy_timeout=5000", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	mustCreate := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create test data: %v", err)
		}
	}

	adminUser := model.User{Username: "admin", Status: model.StatusEnabled}
	mustCreate(&adminUser)
	superRole := model.Role{Name: "Super", Code: model.RoleSuperCode, Status: model.StatusEnabled}
	mustCreate(&superRole)
	mustCreate(&model.UserRole{UserID: adminUser.ID, RoleID: superRole.ID})

	normalUser := model.User{Username: "normal", Status: model.StatusEnabled}
	mustCreate(&normalUser)
	normalRole := model.Role{Name: "Normal", Code: "normal", Status: model.StatusEnabled}
	mustCreate(&normalRole)
	mustCreate(&model.UserRole{UserID: normalUser.ID, RoleID: normalRole.ID})

	// 菜单与权限
	menuAdd := model.Menu{Name: "UserAdd", Permission: "system:user:add", Status: model.StatusEnabled}
	mustCreate(&menuAdd)
	mustCreate(&model.RoleMenu{RoleID: normalRole.ID, MenuID: menuAdd.ID})

	cfg := &config.Config{JWT: config.JWT{Secret: "test-secret"}, Log: config.Log{Level: "release"}}
	authMid := middleware.NewAuthMiddleware(repository.NewAuthRepository(db), cfg)
	menuRepo := repository.NewMenuRepository(db)
	permMid := middleware.NewPermMiddleware(menuRepo)

	oplogRepo := repository.NewOperationLogRepository(db)
	oplogSrv := service.NewOperationLogService(oplogRepo)
	oplogMid := middleware.NewOplogMiddleware(oplogSrv, zap.NewNop())

	engine := gin.New()
	engine.Use(oplogMid.Handler)
	engine.Use(middleware.ErrorHandler())

	apiGroup := engine.Group("/api")
	{
		// 模拟写接口
		userGroup := apiGroup.Group("/user")
		userGroup.Use(authMid.Handler)
		userGroup.Use(permMid.Handler)
		{
			userGroup.POST("", func(c *gin.Context) {
				response.Success(c, "user created")
			})
			userGroup.PUT("/batch-status", func(c *gin.Context) {
				response.Success(c, "user status updated")
			})
			userGroup.DELETE("/:id", func(c *gin.Context) {
				response.Success(c, "user deleted")
			})
			userGroup.POST("/unregistered", func(c *gin.Context) {
				response.Success(c, "unregistered write")
			})
			userGroup.PUT("/profile", func(c *gin.Context) {
				response.Success(c, "profile updated")
			})
			userGroup.PUT("/unregistered-action", func(c *gin.Context) {
				response.Success(c, "unregistered put")
			})
		}
		permMid.Register(http.MethodPost, "/api/user", "system:user:add")
		permMid.Register(http.MethodPut, "/api/user/batch-status", "system:user:status")
		permMid.Register(http.MethodDelete, "/api/user/:id", "system:user:delete")
		permMid.Register(http.MethodPut, "/api/user/profile", middleware.PermCodeAuthenticated)

		// 模拟需要读取权限的日志接口。
		oplogGroup := apiGroup.Group("/oplog")
		oplogGroup.Use(authMid.Handler)
		oplogGroup.Use(permMid.Handler)
		oplogGroup.GET("/page", func(c *gin.Context) {
			response.Success(c, "logs")
		})
		permMid.Register(http.MethodGet, "/api/oplog/page", "system:log")

		// 模拟登录（不走 auth 中间件）
		apiGroup.POST("/auth/login", func(c *gin.Context) {
			var body struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			_ = c.ShouldBindJSON(&body)
			if body.Username == "admin" && body.Password == "admin123" {
				response.Success(c, "login success")
			} else {
				c.Error(errno.NewError(errno.CodeBadCredential))
			}
		})

		// 模拟 GET 接口
		apiGroup.GET("/test/get", func(c *gin.Context) {
			response.Success(c, "get response")
		})
		apiGroup.POST("/test/body", func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.Error(err) //nolint:errcheck
				return
			}
			response.Success(c, len(body))
		})
	}

	adminToken := signTestToken(cfg.JWT.Secret, adminUser.ID)
	normalToken := signTestToken(cfg.JWT.Secret, normalUser.ID)

	// Camera
	cameraGroup := apiGroup.Group("/camera")
	cameraGroup.Use(authMid.Handler)
	cameraGroup.Use(permMid.Handler)
	{
		cameraGroup.POST("", func(c *gin.Context) {
			response.Success(c, "camera created")
		})
		cameraGroup.PUT("/:id", func(c *gin.Context) {
			response.Success(c, "camera updated")
		})
		cameraGroup.DELETE("/:id", func(c *gin.Context) {
			response.Success(c, "camera deleted")
		})
		cameraGroup.DELETE("/batch", func(c *gin.Context) {
			response.Success(c, "camera batch deleted")
		})
		cameraGroup.POST("/probe", func(c *gin.Context) {
			response.Success(c, "camera probed")
		})
		cameraGroup.POST("/:id/preview/start", func(c *gin.Context) {
			response.Success(c, "preview started")
		})
		cameraGroup.POST("/:id/preview/stop", func(c *gin.Context) {
			response.Success(c, "preview stopped")
		})
	}
	permMid.Register(http.MethodPost, "/api/camera", "resource:camera:add")
	permMid.Register(http.MethodPut, "/api/camera/:id", "resource:camera:edit")
	permMid.Register(http.MethodDelete, "/api/camera/:id", "resource:camera:delete")
	permMid.Register(http.MethodDelete, "/api/camera/batch", "resource:camera:delete")
	permMid.Register(http.MethodPost, "/api/camera/probe", "resource:camera:probe")
	permMid.Register(http.MethodPost, "/api/camera/:id/preview/start", "live:preview:stream")
	permMid.Register(http.MethodPost, "/api/camera/:id/preview/stop", "live:preview:stream")

	// Task：任务与实例写接口（路径与 router 装配一致，oplog 断言 action i18n key）。
	taskGroup := apiGroup.Group("/task")
	taskGroup.Use(authMid.Handler)
	taskGroup.Use(permMid.Handler)
	{
		ok := func(c *gin.Context) {
			response.Success(c, "ok")
		}
		taskGroup.POST("", ok)
		taskGroup.PUT("/:cameraId", ok)
		taskGroup.PUT("/:cameraId/enabled", ok)
		taskGroup.DELETE("/:cameraId", ok)
		taskGroup.POST("/instance", ok)
		taskGroup.PUT("/instance/:instanceId", ok)
		taskGroup.PUT("/instance/:instanceId/enabled", ok)
		taskGroup.DELETE("/instance/:instanceId", ok)
	}
	permMid.Register(http.MethodPost, "/api/task", "resource:task:add")
	permMid.Register(http.MethodPut, "/api/task/:cameraId", "resource:task:edit")
	permMid.Register(http.MethodPut, "/api/task/:cameraId/enabled", "resource:task:edit")
	permMid.Register(http.MethodDelete, "/api/task/:cameraId", "resource:task:delete")
	permMid.Register(http.MethodPost, "/api/task/instance", "resource:task:add")
	permMid.Register(http.MethodPut, "/api/task/instance/:instanceId", "resource:task:edit")
	permMid.Register(http.MethodPut, "/api/task/instance/:instanceId/enabled", "resource:task:edit")
	permMid.Register(http.MethodDelete, "/api/task/instance/:instanceId", "resource:task:delete")

	return engine, db, oplogSrv, adminToken, normalToken
}

func signTestToken(secret string, userID uint64) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   strconv.FormatUint(userID, 10),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

func waitForPage(t *testing.T, srv service.OperationLogService, query *service.LogPageQuery, want int64) *service.LogPageResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := srv.GetPage(context.Background(), query)
		if err == nil && page.Total == want {
			return page
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("GetPage failed: %v", err)
			}
			t.Fatalf("log count = %d, want %d", page.Total, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOplogAndPermMiddleware(t *testing.T) {
	engine, _, oplogSrv, adminToken, normalToken := setupMiddlewareTestApp(t)

	// 1. GET 请求不产生日志
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test/get", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec.Code)
	}

	pageRes := waitForPage(t, oplogSrv, &service.LogPageQuery{}, 0)

	// 已声明权限码的读接口也必须校验；普通用户没有 system:log，超管可以访问。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/oplog/page", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("normal user log page status = %d, want 403 Forbidden", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/oplog/page", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin log page status = %d, want 200", rec.Code)
	}

	// 2. 超管访问具备全权限，并且触发日志记录与密码脱敏
	reqBody := `{"username":"alice","password":"supersecretpassword123","email":"alice@example.com"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin POST status = %d, want 200", rec.Code)
	}

	pageRes = waitForPage(t, oplogSrv, &service.LogPageQuery{}, 1)
	log0 := pageRes.Items[0]
	if log0.Username != "admin" || log0.Module != "user" || log0.Method != "POST" {
		t.Errorf("unexpected log item: %+v", log0)
	}
	var parsedBody map[string]any
	_ = json.Unmarshal([]byte(log0.Body), &parsedBody)
	if parsedBody["password"] != "***" {
		t.Errorf("password not masked in body: %s", log0.Body)
	}
	if parsedBody["username"] != "alice" {
		t.Errorf("username changed in body: %s", log0.Body)
	}

	// 3. 普通用户访问拥有权限的接口 (system:user:add) -> 成功
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user", bytes.NewBufferString(`{"username":"bob"}`))
	req.Header.Set("Authorization", "Bearer "+normalToken)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("normal user allowed POST status = %d, want 200", rec.Code)
	}

	// 4. 普通用户访问未拥有权限的接口 (system:user:delete) -> 403 Forbidden
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/user/100", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("normal user delete status = %d, want 403 Forbidden", rec.Code)
	}
	pageRes = waitForPage(t, oplogSrv, &service.LogPageQuery{}, 3)
	var deleteLog *model.OperationLog
	for i := range pageRes.Items {
		if pageRes.Items[i].Path == "/api/user/100" {
			deleteLog = &pageRes.Items[i]
			break
		}
	}
	if deleteLog == nil || deleteLog.StatusCode != http.StatusForbidden {
		t.Fatalf("delete log status = %v, want %d", deleteLog, http.StatusForbidden)
	}

	// 5. 未声明权限码的写接口默认拒绝。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user/unregistered", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unregistered write status = %d, want 403 Forbidden", rec.Code)
	}

	// 6. 登录失败也产生日志，且能从请求体提取 username，密码脱敏
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"unknown_hacker","password":"wrongpassword"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	pageRes = waitForPage(t, oplogSrv, &service.LogPageQuery{Username: "unknown_hacker"}, 1)
	loginLog := pageRes.Items[0]
	if loginLog.Username != "unknown_hacker" || loginLog.Module != "auth" {
		t.Errorf("unexpected failed login log: %+v", loginLog)
	}
	_ = json.Unmarshal([]byte(loginLog.Body), &parsedBody)
	if parsedBody["password"] != "***" {
		t.Errorf("password in failed login log not masked: %s", loginLog.Body)
	}

	// 7. 测试 Authenticated 写路由放行与未登录拦截
	// 7.1 未登录请求 Authenticated 路由 → 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user/profile", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated put profile status = %d, want 401, got %d", rec.Code, http.StatusUnauthorized)
	}

	// 7.2 普通用户请求 Authenticated 路由（无特定角色权限码） → 200 放行
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user/profile", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("normal user put profile status = %d, want 200", rec.Code)
	}

	// 7.3 普通用户请求未登记写路由 → 仍返回 403
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user/unregistered-action", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("normal user unregistered write status = %d, want 403", rec.Code)
	}
}

func TestOplogDoesNotPersistUnstructuredSecrets(t *testing.T) {
	engine, _, oplogSrv, _, _ := setupMiddlewareTestApp(t)
	body := "username=alice&password=body-secret"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/test/body?token=query-secret&scope=read", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("form body status = %d, want 200", rec.Code)
	}

	page := waitForPage(t, oplogSrv, &service.LogPageQuery{}, 1)
	item := page.Items[0]
	if item.Body != mask.OmittedBody || strings.Contains(item.Body, "body-secret") {
		t.Fatalf("unstructured body was persisted: %q", item.Body)
	}
	if strings.Contains(item.Query, "query-secret") || !strings.Contains(item.Query, "token=%2A%2A%2A") {
		t.Fatalf("sensitive query was not masked: %q", item.Query)
	}
}

func TestOplogBoundsLongFields(t *testing.T) {
	engine, _, oplogSrv, _, _ := setupMiddlewareTestApp(t)
	longPath := "/api/" + strings.Repeat("x", 300)
	engine.POST(longPath, func(c *gin.Context) {
		response.Success(c, "ok")
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, longPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("long path status = %d, want 200", rec.Code)
	}

	page := waitForPage(t, oplogSrv, &service.LogPageQuery{}, 1)
	item := page.Items[0]
	if len(item.Action) > 64 {
		t.Fatalf("action length = %d, want <= 64", len(item.Action))
	}
	if len(item.Path) > 255 {
		t.Fatalf("path length = %d, want <= 255", len(item.Path))
	}
}

func TestOplogReplaysLargeBody(t *testing.T) {
	engine, _, oplogSrv, _, _ := setupMiddlewareTestApp(t)
	body := `{"password":"must-not-leak","padding":"` + strings.Repeat("x", 2*1024*1024) + `"}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/test/body", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("large body status = %d, want 200", rec.Code)
	}

	var responseBody struct {
		Data int `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &responseBody); err != nil {
		t.Fatalf("decode large body response: %v", err)
	}
	if responseBody.Data != len(body) {
		t.Fatalf("handler received %d bytes, want %d", responseBody.Data, len(body))
	}

	page := waitForPage(t, oplogSrv, &service.LogPageQuery{}, 1)
	if strings.Contains(page.Items[0].Body, "must-not-leak") {
		t.Fatal("large request body leaked into operation log")
	}
}

func TestOplogActionInference(t *testing.T) {
	engine, _, oplogSrv, adminToken, _ := setupMiddlewareTestApp(t)

	// 注册人员路由以验证页面与开放同步 action 都能精确映射到 i18n key。
	engine.POST("/api/person", func(c *gin.Context) {
		response.Success(c, "person created")
	})
	engine.PUT("/api/person/:personId", func(c *gin.Context) {
		response.Success(c, "person updated")
	})
	engine.DELETE("/api/person/batch", func(c *gin.Context) {
		response.Success(c, "person batch deleted")
	})
	engine.DELETE("/api/person/:personId", func(c *gin.Context) {
		response.Success(c, "person deleted")
	})
	engine.PUT("/api/v1/open/person/:personId", func(c *gin.Context) {
		response.Success(c, "person sync upserted")
	})
	engine.DELETE("/api/v1/open/person/:personId", func(c *gin.Context) {
		response.Success(c, "person sync deleted")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	page := waitForPage(t, oplogSrv, &service.LogPageQuery{}, 1)
	if page.Items[0].Action != "system.user.addUser" {
		t.Fatalf("action = %q, want %q", page.Items[0].Action, "system.user.addUser")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user/batch-status", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status = %d, want 200", rec.Code)
	}

	page = waitForPage(t, oplogSrv, &service.LogPageQuery{}, 2)
	var batchStatusLog *model.OperationLog
	for i := range page.Items {
		if page.Items[i].Path == "/api/user/batch-status" {
			batchStatusLog = &page.Items[i]
			break
		}
	}
	if batchStatusLog == nil {
		t.Fatal("batch status operation log not found")
	}
	if batchStatusLog.Action != "system.user.batchStatus" {
		t.Fatalf("batch status action = %q, want %q", batchStatusLog.Action, "system.user.batchStatus")
	}

	// 个人中心路由应映射到语义化 i18n key 而非 fallback 的 "Method Path"
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user/profile", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile = %d, want 200", rec.Code)
	}

	page = waitForPage(t, oplogSrv, &service.LogPageQuery{}, 3)
	var profileLog *model.OperationLog
	for i := range page.Items {
		if page.Items[i].Path == "/api/user/profile" {
			profileLog = &page.Items[i]
			break
		}
	}
	if profileLog == nil {
		t.Fatal("profile operation log not found")
	}
	if profileLog.Action != "system.log.actionUpdateProfile" {
		t.Fatalf("profile action = %q, want %q", profileLog.Action, "system.log.actionUpdateProfile")
	}

	// 摄像头路由动作映射
	cameraCases := []struct {
		method     string
		path       string
		wantAction string
	}{
		{http.MethodPost, "/api/camera", "resource.camera.add"},
		{http.MethodPut, "/api/camera/1", "resource.camera.edit"},
		{http.MethodDelete, "/api/camera/1", "resource.camera.delete"},
		{http.MethodDelete, "/api/camera/batch", "system.common.batchDelete"},
		{http.MethodPost, "/api/camera/probe", "resource.camera.probe"},
		{http.MethodPost, "/api/camera/1/preview/start", "live.preview.start"},
		{http.MethodPost, "/api/camera/1/preview/stop", "live.preview.stop"},
	}

	for _, tc := range cameraCases {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}

	page = waitForPage(t, oplogSrv, &service.LogPageQuery{Module: "camera"}, int64(len(cameraCases)))
	for _, tc := range cameraCases {
		found := false
		for _, item := range page.Items {
			if item.Path == tc.path && item.Method == tc.method {
				found = true
				if item.Action != tc.wantAction {
					t.Errorf("%s %s action = %q, want %q", tc.method, tc.path, item.Action, tc.wantAction)
				}
				break
			}
		}
		if !found {
			t.Errorf("log item not found for %s %s", tc.method, tc.path)
		}
	}

	personCases := []struct {
		method     string
		path       string
		wantAction string
	}{
		{http.MethodPost, "/api/person", "resource.person.add"},
		{http.MethodPut, "/api/person/EMP001", "resource.person.edit"},
		{http.MethodDelete, "/api/person/EMP001", "resource.person.delete"},
		{http.MethodDelete, "/api/person/batch", "system.common.batchDelete"},
		{http.MethodPut, "/api/v1/open/person/EMP001", "resource.person.syncUpsert"},
		{http.MethodDelete, "/api/v1/open/person/EMP001", "resource.person.syncDelete"},
	}
	for _, tc := range personCases {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(tc.method, tc.path, nil)
		if strings.HasPrefix(tc.path, "/api/person") {
			req.Header.Set("Authorization", "Bearer "+adminToken)
		}
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}

	page = waitForPage(t, oplogSrv, &service.LogPageQuery{PageSize: 100}, 16)
	for _, tc := range personCases {
		found := false
		for _, item := range page.Items {
			if item.Path == tc.path && item.Method == tc.method {
				found = true
				if item.Action != tc.wantAction {
					t.Errorf("%s %s action = %q, want %q", tc.method, tc.path, item.Action, tc.wantAction)
				}
				break
			}
		}
		if !found {
			t.Errorf("log item not found for %s %s", tc.method, tc.path)
		}
	}

	// 任务路由动作映射（四位一体闭环：actionI18nMap + 前端翻译 + 此处断言）。
	taskCases := []struct {
		method     string
		path       string
		wantAction string
	}{
		{http.MethodPost, "/api/task", "resource.task.add"},
		{http.MethodPut, "/api/task/cam-1", "resource.task.edit"},
		{http.MethodPut, "/api/task/cam-1/enabled", "resource.task.toggleEnabled"},
		{http.MethodDelete, "/api/task/batch", "resource.task.delete"},
		{http.MethodDelete, "/api/task/cam-1", "resource.task.delete"},
		{http.MethodPost, "/api/task/instance", "resource.task.instanceAdd"},
		{http.MethodPut, "/api/task/instance/inst-1", "resource.task.instanceEdit"},
		{http.MethodPut, "/api/task/instance/inst-1/enabled", "resource.task.instanceToggleEnabled"},
		{http.MethodDelete, "/api/task/instance/inst-1", "resource.task.instanceDelete"},
	}
	for _, tc := range taskCases {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}
	page = waitForPage(t, oplogSrv, &service.LogPageQuery{PageSize: 100}, int64(16+len(taskCases)))
	for _, tc := range taskCases {
		found := false
		for _, item := range page.Items {
			if item.Path == tc.path && item.Method == tc.method {
				found = true
				if item.Action != tc.wantAction {
					t.Errorf("%s %s action = %q, want %q", tc.method, tc.path, item.Action, tc.wantAction)
				}
				break
			}
		}
		if !found {
			t.Errorf("log item not found for %s %s", tc.method, tc.path)
		}
	}
}
