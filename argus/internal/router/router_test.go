package router

import (
	"context"
	"encoding/json"
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

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

type routerTestMenuService struct {
	panicOnTree bool
	roleCodes   []string
	roleIDs     []uint64
}

func (s *routerTestMenuService) GetMenuTree(context.Context) ([]*model.MenuTreeNode, error) {
	if s.panicOnTree {
		panic("test panic")
	}
	return []*model.MenuTreeNode{}, nil
}

func (s *routerTestMenuService) GetUserMenuTree(_ context.Context, roleCodes []string, roleIDs []uint64) ([]*service.VbenRouteRecord, error) {
	s.roleCodes = append([]string(nil), roleCodes...)
	s.roleIDs = append([]uint64(nil), roleIDs...)
	return []*service.VbenRouteRecord{}, nil
}

func (s *routerTestMenuService) CreateMenu(context.Context, *service.SaveMenuInput) (*model.Menu, error) {
	return &model.Menu{}, nil
}

func (s *routerTestMenuService) UpdateMenu(context.Context, uint64, *service.SaveMenuInput) (*model.Menu, error) {
	return &model.Menu{}, nil
}

func (s *routerTestMenuService) DeleteMenu(context.Context, uint64) error {
	return nil
}

type routerTestDeptService struct{}

func (*routerTestDeptService) GetDeptTree(context.Context) ([]*model.DepartmentTreeNode, error) {
	return []*model.DepartmentTreeNode{}, nil
}

func (*routerTestDeptService) CreateDept(context.Context, *service.SaveDeptInput) (*model.Department, error) {
	return &model.Department{}, nil
}

func (*routerTestDeptService) UpdateDept(context.Context, uint64, *service.SaveDeptInput) (*model.Department, error) {
	return &model.Department{}, nil
}

func (*routerTestDeptService) DeleteDept(context.Context, uint64) error {
	return nil
}

type routerTestRoleService struct{}

func (*routerTestRoleService) GetPage(context.Context, *service.RolePageQuery) (*service.RolePageResult, error) {
	return &service.RolePageResult{Items: []model.Role{}, Total: 0}, nil
}

func (*routerTestRoleService) CreateRole(context.Context, *service.SaveRoleInput) (*model.Role, error) {
	return &model.Role{}, nil
}

func (*routerTestRoleService) UpdateRole(context.Context, uint64, *service.SaveRoleInput) (*model.Role, error) {
	return &model.Role{}, nil
}

func (*routerTestRoleService) DeleteRole(context.Context, uint64) error {
	return nil
}

func (*routerTestRoleService) BatchDelete(context.Context, []uint64) error {
	return nil
}

func (*routerTestRoleService) GetMenuIDs(context.Context, uint64) ([]uint64, error) {
	return []uint64{}, nil
}

func (*routerTestRoleService) AssignMenus(context.Context, uint64, []uint64) error {
	return nil
}

type routerTestOperationLogService struct {
	records chan model.OperationLog
}

func (s *routerTestOperationLogService) Record(_ context.Context, log *model.OperationLog) error {
	s.records <- *log
	return nil
}

func (*routerTestOperationLogService) GetByID(context.Context, uint64) (*model.OperationLog, error) {
	return nil, nil
}

func (*routerTestOperationLogService) GetPage(context.Context, *service.LogPageQuery) (*service.LogPageResult, error) {
	return &service.LogPageResult{}, nil
}

func newRouterTestEngine(t *testing.T, panicOnTree bool) (*gin.Engine, *routerTestMenuService, string, *routerTestOperationLogService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	user := model.User{Username: "admin", Status: model.StatusEnabled}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	activeRole := model.Role{Name: "Super", Code: model.RoleSuperCode, Status: model.StatusEnabled}
	if err := db.Create(&activeRole).Error; err != nil {
		t.Fatalf("create active role: %v", err)
	}
	disabledRole := model.Role{Name: "Disabled", Code: "disabled", Status: model.StatusDisabled}
	if err := db.Create(&disabledRole).Error; err != nil {
		t.Fatalf("create disabled role: %v", err)
	}
	if err := db.Model(&model.Role{}).Where("id = ?", disabledRole.ID).Update("status", model.StatusDisabled).Error; err != nil {
		t.Fatalf("disable role: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: user.ID, RoleID: activeRole.ID}).Error; err != nil {
		t.Fatalf("create active user role: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: user.ID, RoleID: disabledRole.ID}).Error; err != nil {
		t.Fatalf("create disabled user role: %v", err)
	}

	cfg := &config.Config{
		JWT: config.JWT{Secret: "test-secret"},
		Log: config.Log{Level: "release"},
		Storage: config.Storage{
			Driver:  config.StorageDriverLocal,
			MaxSize: 10 * 1024 * 1024,
			Local:   config.Local{Root: t.TempDir(), URLPrefix: "/uploads"},
		},
	}
	auth := middleware.NewAuthMiddleware(repository.NewAuthRepository(db), cfg)
	menuRepo := repository.NewMenuRepository(db)
	perm := middleware.NewPermMiddleware(menuRepo)
	oplogSrv := &routerTestOperationLogService{records: make(chan model.OperationLog, 1)}
	oplogMid := middleware.NewOplogMiddleware(oplogSrv, zap.NewNop())
	fakeService := &routerTestMenuService{panicOnTree: panicOnTree}
	roleSrv := &routerTestRoleService{}
	deptSrv := &routerTestDeptService{}
	deptRepo := repository.NewDepartmentRepository(db)
	userRepo := repository.NewUserRepository(db)
	userSvc := service.NewUserService(userRepo, deptRepo, repository.NewRoleRepository(db))
	personSvc := service.NewPersonService(repository.NewPersonRepository(db), repository.NewPersonFaceRepository(db), storage.NopStorage(), nil)
	authSvc := service.NewAuthService(repository.NewAuthRepository(db), userRepo, menuRepo, cfg)
	engine := New(cfg, Deps{
		ErrorHandler:           middleware.ErrorHandler(),
		AuthMiddleware:         auth,
		PermMiddleware:         perm,
		OplogMiddleware:        oplogMid,
		MenuHandler:            api.NewMenuHandler(fakeService),
		RoleHandler:            api.NewRoleHandler(roleSrv),
		DepartmentHandler:      api.NewDepartmentHandler(deptSrv),
		OperationLogHandler:    api.NewOperationLogHandler(oplogSrv),
		UserHandler:            api.NewUserHandler(userSvc),
		AuthHandler:            api.NewAuthHandler(authSvc, auth, cfg),
		FileHandler:            api.NewFileHandler(service.NewFileService(storage.NopStorage(), cfg), cfg),
		PersonHandler:          api.NewPersonHandler(personSvc),
		OpenPersonIPMiddleware: middleware.NewOpenPersonIPWhitelistMiddleware(cfg),
	})
	return engine, fakeService, signRouterToken(t, cfg.JWT.Secret, user.ID), oplogSrv, db
}

func signRouterToken(t *testing.T, secret string, userID uint64) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   strconv.FormatUint(userID, 10),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func readCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Code
}

func waitForLoggedRequest(t *testing.T, records <-chan model.OperationLog, path string) model.OperationLog {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for {
		select {
		case item := <-records:
			if item.Path == path {
				return item
			}
		case <-timer.C:
			t.Fatalf("operation log %q not found", path)
			return model.OperationLog{}
		}
	}
}

func TestOperationLogDeleteRoutesAreNotRegistered(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodDelete && strings.HasPrefix(route.Path, "/api/oplog") {
			t.Fatalf("operation log delete route is still registered: %s %s", route.Method, route.Path)
		}
	}
}

func TestMenuRoutesRequireAuthenticationAndUseActiveRoles(t *testing.T) {
	engine, fakeService, token, _, _ := newRouterTestEngine(t, false)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/menu/all", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if code := readCode(t, unauthenticated); code != errno.CodeUnauthorized {
		t.Fatalf("unauthenticated code = %d, want %d", code, errno.CodeUnauthorized)
	}

	unknownUser := httptest.NewRecorder()
	unknownRequest := httptest.NewRequest(http.MethodGet, "/api/menu/all", nil)
	unknownRequest.Header.Set("Authorization", "Bearer "+signRouterToken(t, "test-secret", 999))
	engine.ServeHTTP(unknownUser, unknownRequest)
	if unknownUser.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want %d", unknownUser.Code, http.StatusUnauthorized)
	}

	authenticated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/menu/all", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(authenticated, req)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", authenticated.Code, http.StatusOK)
	}
	if len(fakeService.roleCodes) != 1 || fakeService.roleCodes[0] != model.RoleSuperCode {
		t.Fatalf("role codes = %v, want active super only", fakeService.roleCodes)
	}
}

func TestPersonRoutesRequireAuthenticationAndOpenIPWhitelist(t *testing.T) {
	engine, _, token, oplogSrv, db := newRouterTestEngine(t, false)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/person/page", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated person page status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if code := readCode(t, unauthenticated); code != errno.CodeUnauthorized {
		t.Fatalf("unauthenticated person page code = %d, want %d", code, errno.CodeUnauthorized)
	}

	authenticated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/person/page", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(authenticated, req)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated person page status = %d, want %d", authenticated.Code, http.StatusOK)
	}

	normalUser := model.User{Username: "person-reader", Status: model.StatusEnabled}
	if err := db.Create(&normalUser).Error; err != nil {
		t.Fatalf("create normal user: %v", err)
	}
	normalRole := model.Role{Name: "Person reader", Code: "person-reader", Status: model.StatusEnabled}
	if err := db.Create(&normalRole).Error; err != nil {
		t.Fatalf("create normal role: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: normalUser.ID, RoleID: normalRole.ID}).Error; err != nil {
		t.Fatalf("create normal user role: %v", err)
	}

	forbidden := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/person/page", nil)
	req.Header.Set("Authorization", "Bearer "+signRouterToken(t, "test-secret", normalUser.ID))
	engine.ServeHTTP(forbidden, req)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("person page without permission status = %d, want %d", forbidden.Code, http.StatusForbidden)
	}
	if code := readCode(t, forbidden); code != errno.CodeForbidden {
		t.Fatalf("person page without permission code = %d, want %d", code, errno.CodeForbidden)
	}

	openRequest := httptest.NewRequest(http.MethodPut, "/api/v1/open/person/EMP001", strings.NewReader(`{"name":"Alice"}`))
	openRequest.RemoteAddr = "192.0.2.10:1234"
	openRequest.Header.Set("Content-Type", "application/json")
	openResponse := httptest.NewRecorder()
	engine.ServeHTTP(openResponse, openRequest)
	if openResponse.Code != http.StatusForbidden {
		t.Fatalf("open person without whitelist status = %d, want %d", openResponse.Code, http.StatusForbidden)
	}
	if code := readCode(t, openResponse); code != errno.CodeForbidden {
		t.Fatalf("open person without whitelist code = %d, want %d", code, errno.CodeForbidden)
	}
	waitForLoggedRequest(t, oplogSrv.records, "/api/v1/open/person/EMP001")
}

func TestRoleRoutesRequireAuthentication(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/role/page", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if code := readCode(t, unauthenticated); code != errno.CodeUnauthorized {
		t.Fatalf("unauthenticated code = %d, want %d", code, errno.CodeUnauthorized)
	}
}

func TestFileUploadRouteRequiresAuthentication(t *testing.T) {
	engine, _, token, _, _ := newRouterTestEngine(t, false)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/file/upload", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if code := readCode(t, unauthenticated); code != errno.CodeUnauthorized {
		t.Fatalf("unauthenticated code = %d, want %d", code, errno.CodeUnauthorized)
	}

	authenticated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/file/upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(authenticated, req)
	if authenticated.Code != http.StatusBadRequest {
		t.Fatalf("authenticated request without file status = %d, want %d", authenticated.Code, http.StatusBadRequest)
	}
	if code := readCode(t, authenticated); code != errno.CodeInvalidParam {
		t.Fatalf("authenticated request without file code = %d, want %d", code, errno.CodeInvalidParam)
	}
}

func TestLocalFileRouteIsRegistered(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/uploads/*filepath" {
			return
		}
	}
	t.Fatal("local file GET route is not registered")
}

func TestDeptRoutesRequireAuthentication(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/dept/tree", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if code := readCode(t, unauthenticated); code != errno.CodeUnauthorized {
		t.Fatalf("unauthenticated code = %d, want %d", code, errno.CodeUnauthorized)
	}
}

func TestDeptWriteRoutesRequirePermissions(t *testing.T) {
	engine, _, _, _, db := newRouterTestEngine(t, false)

	user := model.User{Username: "dept-normal", Status: model.StatusEnabled}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create normal user: %v", err)
	}
	role := model.Role{Name: "Dept reader", Code: "dept-reader", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create normal role: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	token := signRouterToken(t, "test-secret", user.ID)

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/dept", body: `{"name":"new"}`},
		{method: http.MethodPut, path: "/api/dept/1", body: `{"name":"updated"}`},
		{method: http.MethodDelete, path: "/api/dept/1"},
	}
	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want %d", route.method, route.path, rec.Code, http.StatusForbidden)
		}
		if code := readCode(t, rec); code != errno.CodeForbidden {
			t.Errorf("%s %s code = %d, want %d", route.method, route.path, code, errno.CodeForbidden)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dept/tree", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/dept/tree status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestInvalidMenuRequestUsesBadRequest(t *testing.T) {
	engine, _, token, oplogSrv, _ := newRouterTestEngine(t, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/menu", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if code := readCode(t, rec); code != errno.CodeInvalidParam {
		t.Fatalf("invalid request code = %d, want %d", code, errno.CodeInvalidParam)
	}
	logItem := waitForLoggedRequest(t, oplogSrv.records, "/api/menu")
	if logItem.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid request log status = %d, want %d", logItem.StatusCode, http.StatusBadRequest)
	}
}

func TestRouterPanicUsesUnifiedResponse(t *testing.T) {
	engine, _, token, _, _ := newRouterTestEngine(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/menu/tree", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if code := readCode(t, rec); code != errno.CodeInternal {
		t.Fatalf("panic code = %d, want %d", code, errno.CodeInternal)
	}
}

func TestRouterPanicIsLogged(t *testing.T) {
	engine, _, _, oplogSrv, _ := newRouterTestEngine(t, false)
	engine.POST("/api/test/panic", func(c *gin.Context) {
		panic("test panic")
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/test/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	logItem := waitForLoggedRequest(t, oplogSrv.records, "/api/test/panic")
	if logItem.StatusCode != http.StatusInternalServerError {
		t.Fatalf("panic log status = %d, want %d", logItem.StatusCode, http.StatusInternalServerError)
	}
}

func TestSwaggerEndpointAccessible(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	// 测试 /swagger/index.html 可以正常访问，状态码 200
	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /swagger/index.html status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "swagger") {
		t.Fatalf("GET /swagger/index.html body does not contain 'swagger'")
	}

	// 测试 /swagger/doc.json 可以获取 OpenAPI 文档
	docReq := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	docRec := httptest.NewRecorder()
	engine.ServeHTTP(docRec, docReq)

	if docRec.Code != http.StatusOK {
		t.Fatalf("GET /swagger/doc.json status = %d, want %d", docRec.Code, http.StatusOK)
	}
	if !strings.Contains(docRec.Body.String(), "argus API") {
		t.Fatalf("GET /swagger/doc.json body does not contain 'argus API'")
	}
}

func TestSPARoutingAnd404Isolation(t *testing.T) {
	engine, _, _, _, _ := newRouterTestEngine(t, false)

	// 1. 根路径 GET / 返回 index.html (200)
	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "<html") {
			t.Fatalf("GET / body does not contain html")
		}
		if rec.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
			t.Fatalf("GET / cache-control = %s, want no-cache, no-store, must-revalidate", rec.Header().Get("Cache-Control"))
		}
	}

	// 2. 前端 SPA 页面路径 GET /system/user 返回 index.html (200)
	{
		req := httptest.NewRequest(http.MethodGet, "/system/user", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /system/user status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "<html") {
			t.Fatalf("GET /system/user body does not contain html")
		}
	}

	// 3. 未定义的 API GET /api/nonexistent 严格返回 JSON 404 (不是 html)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /api/nonexistent status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if code := readCode(t, rec); code != errno.CodeNotFound {
			t.Fatalf("GET /api/nonexistent code = %d, want %d", code, errno.CodeNotFound)
		}
	}

	// 4. 未定义的 API POST /api/nonexistent 严格返回 JSON 404
	{
		req := httptest.NewRequest(http.MethodPost, "/api/nonexistent", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST /api/nonexistent status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if code := readCode(t, rec); code != errno.CodeNotFound {
			t.Fatalf("POST /api/nonexistent code = %d, want %d", code, errno.CodeNotFound)
		}
	}

	// 5. 非 GET/HEAD 的未定义路由 POST /nonexistent 返回 JSON 404
	{
		req := httptest.NewRequest(http.MethodPost, "/nonexistent", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST /nonexistent status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if code := readCode(t, rec); code != errno.CodeNotFound {
			t.Fatalf("POST /nonexistent code = %d, want %d", code, errno.CodeNotFound)
		}
	}

	// 6. 不存在的上传文件 GET /uploads/nonexistent.jpg 返回 JSON 404
	{
		req := httptest.NewRequest(http.MethodGet, "/uploads/nonexistent.jpg", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /uploads/nonexistent.jpg status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	}
}
