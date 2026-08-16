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
			userGroup.DELETE("/:id", func(c *gin.Context) {
				response.Success(c, "user deleted")
			})
			userGroup.POST("/unregistered", func(c *gin.Context) {
				response.Success(c, "unregistered write")
			})
		}
		permMid.Register(http.MethodPost, "/api/user", "system:user:add")
		permMid.Register(http.MethodDelete, "/api/user/:id", "system:user:delete")

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
