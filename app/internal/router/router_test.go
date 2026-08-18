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

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
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

func (*routerTestOperationLogService) Delete(context.Context, uint64) error {
	return nil
}

func (*routerTestOperationLogService) BatchDelete(context.Context, []uint64) error {
	return nil
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

	cfg := &config.Config{JWT: config.JWT{Secret: "test-secret"}, Log: config.Log{Level: "release"}}
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
	authSvc := service.NewAuthService(repository.NewAuthRepository(db), userRepo, menuRepo, cfg)
	engine := New(cfg, Deps{
		ErrorHandler:        middleware.ErrorHandler(),
		AuthMiddleware:      auth,
		PermMiddleware:      perm,
		OplogMiddleware:     oplogMid,
		MenuHandler:         api.NewMenuHandler(fakeService),
		RoleHandler:         api.NewRoleHandler(roleSrv),
		DepartmentHandler:   api.NewDepartmentHandler(deptSrv),
		OperationLogHandler: api.NewOperationLogHandler(oplogSrv),
		UserHandler:         api.NewUserHandler(userSvc),
		AuthHandler:         api.NewAuthHandler(authSvc, cfg),
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
