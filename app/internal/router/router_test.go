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

func newRouterTestEngine(t *testing.T, panicOnTree bool) (*gin.Engine, *routerTestMenuService, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
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
	fakeService := &routerTestMenuService{panicOnTree: panicOnTree}
	engine := New(cfg, Deps{
		ErrorHandler:   middleware.ErrorHandler(),
		AuthMiddleware: auth,
		MenuHandler:    api.NewMenuHandler(fakeService),
	})
	return engine, fakeService, signRouterToken(t, cfg.JWT.Secret, user.ID)
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

func TestMenuRoutesRequireAuthenticationAndUseActiveRoles(t *testing.T) {
	engine, fakeService, token := newRouterTestEngine(t, false)

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

func TestInvalidMenuRequestUsesBadRequest(t *testing.T) {
	engine, _, token := newRouterTestEngine(t, false)
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
}

func TestRouterPanicUsesUnifiedResponse(t *testing.T) {
	engine, _, token := newRouterTestEngine(t, true)
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
