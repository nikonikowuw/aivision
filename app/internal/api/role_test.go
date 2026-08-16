package api_test

import (
	"bytes"
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

// setupRoleAPIEngine 用真实 sqlite + 真实 service 装配 role handler 路由，
// 与 operation_log_test.go 同风格。
func setupRoleAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "role")

	roleRepo := repository.NewRoleRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	handler := api.NewRoleHandler(service.NewRoleService(roleRepo, menuRepo))

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	group := engine.Group("/api/role")
	{
		group.GET("/page", handler.GetPage)
		group.POST("", handler.CreateRole)
		group.PUT("/:id", handler.UpdateRole)
		group.DELETE("/:id", handler.DeleteRole)
		group.GET("/:id/menu-ids", handler.GetMenuIDs)
		group.PUT("/:id/menus", handler.AssignMenus)
	}
	return engine, db
}

// doRoleRequest 发起请求并解析统一响应体 {code,data,message}。
func doRoleRequest(t *testing.T, engine *gin.Engine, method, path, body string) (int, int) {
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

func TestRoleAPI_CreatePageAndValidation(t *testing.T) {
	engine, _ := setupRoleAPIEngine(t)

	// 1. 空 body → 1009。
	if _, code := doRoleRequest(t, engine, http.MethodPost, "/api/role", ""); code != errno.CodeInvalidParam {
		t.Fatalf("empty body: code = %d, want 1009", code)
	}

	// 2. 非法 id 路径参数 → 1009。
	if _, code := doRoleRequest(t, engine, http.MethodPut, "/api/role/abc", `{"name":"x","code":"x"}`); code != errno.CodeInvalidParam {
		t.Fatalf("invalid id: code = %d, want 1009", code)
	}

	// 3. 创建成功 → code=0 且 status 默认启用。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/role", bytes.NewBufferString(`{"name":"编辑","code":"editor"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	var createResp struct {
		Code int        `json:"code"`
		Data model.Role `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	if rec.Code != http.StatusOK || createResp.Code != errno.CodeOK {
		t.Fatalf("create: status=%d code=%d, want 200/0", rec.Code, createResp.Code)
	}
	if createResp.Data.ID == 0 || createResp.Data.Status != model.StatusEnabled {
		t.Errorf("created role = %+v, want id>0 and status enabled", createResp.Data)
	}

	// 4. 重复 code → 1004。
	if _, code := doRoleRequest(t, engine, http.MethodPost, "/api/role", `{"name":"编辑2","code":"editor"}`); code != errno.CodeRoleCodeTaken {
		t.Fatalf("dup code: code = %d, want 1004", code)
	}

	// 5. 分页可见。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/role/page?page=1&pageSize=20", nil)
	engine.ServeHTTP(rec, req)
	var pageResp struct {
		Code int                    `json:"code"`
		Data service.RolePageResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pageResp); err != nil {
		t.Fatalf("unmarshal page resp: %v", err)
	}
	if pageResp.Code != errno.CodeOK || pageResp.Data.Total != 1 || len(pageResp.Data.Items) != 1 {
		t.Errorf("page response = %+v, want code=0 total=1 items=1", pageResp)
	}
}

func TestRoleAPI_AssignMenusAndGetMenuIDs(t *testing.T) {
	engine, db := setupRoleAPIEngine(t)

	menu := model.Menu{Type: model.MenuTypeMenu, Name: "A", Status: model.StatusEnabled}
	if err := db.Create(&menu).Error; err != nil {
		t.Fatalf("create menu: %v", err)
	}

	// 创建角色。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/role", bytes.NewBufferString(`{"name":"编辑","code":"editor"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	var createResp struct {
		Code int        `json:"code"`
		Data model.Role `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	if createResp.Code != errno.CodeOK {
		t.Fatalf("create role: code = %d, want 0", createResp.Code)
	}
	roleID := createResp.Data.ID

	// 分配菜单 → 成功。
	if _, code := doRoleRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/role/%d/menus", roleID),
		fmt.Sprintf(`{"menuIds":[%d]}`, menu.ID)); code != errno.CodeOK {
		t.Fatalf("assign menus: code = %d, want 0", code)
	}

	// 查询已分配 → 与写入一致。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/role/%d/menu-ids", roleID), nil)
	engine.ServeHTTP(rec, req)
	var idsResp struct {
		Code int      `json:"code"`
		Data []uint64 `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &idsResp); err != nil {
		t.Fatalf("unmarshal menu ids resp: %v", err)
	}
	if idsResp.Code != errno.CodeOK || len(idsResp.Data) != 1 || idsResp.Data[0] != menu.ID {
		t.Errorf("menu ids resp = %+v, want [%d]", idsResp, menu.ID)
	}

	// 菜单软删后不再返回。
	if err := db.Delete(&model.Menu{}, menu.ID).Error; err != nil {
		t.Fatalf("soft delete menu: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/role/%d/menu-ids", roleID), nil)
	engine.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &idsResp); err != nil {
		t.Fatalf("unmarshal menu ids after delete: %v", err)
	}
	if len(idsResp.Data) != 0 {
		t.Errorf("menu ids after menu soft delete = %v, want empty", idsResp.Data)
	}

	// 非法菜单 id → 1009。
	if _, code := doRoleRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/role/%d/menus", roleID),
		`{"menuIds":[99999]}`); code != errno.CodeInvalidParam {
		t.Fatalf("invalid menu id: code = %d, want 1009", code)
	}

	// 不存在的角色 → 1011。
	if _, code := doRoleRequest(t, engine, http.MethodGet, "/api/role/99999/menu-ids", ""); code != errno.CodeNotFound {
		t.Fatalf("missing role menu ids: code = %d, want 1011", code)
	}
}

func TestRoleAPI_SuperProtection(t *testing.T) {
	engine, db := setupRoleAPIEngine(t)

	super := model.Role{Name: "超级管理员", Code: model.RoleSuperCode, Status: model.StatusEnabled}
	if err := db.Create(&super).Error; err != nil {
		t.Fatalf("create super: %v", err)
	}

	// 停用 → 1014。
	if _, code := doRoleRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/role/%d", super.ID),
		`{"name":"超级管理员","code":"super","status":0}`); code != errno.CodeSuperRoleProtected {
		t.Fatalf("disable super: code = %d, want 1014", code)
	}

	// 改 code → 1014。
	if _, code := doRoleRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/role/%d", super.ID),
		`{"name":"超级管理员","code":"editor"}`); code != errno.CodeSuperRoleProtected {
		t.Fatalf("rename super: code = %d, want 1014", code)
	}

	// 删除 → 1014。
	if _, code := doRoleRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/role/%d", super.ID), ""); code != errno.CodeSuperRoleProtected {
		t.Fatalf("delete super: code = %d, want 1014", code)
	}

	// 分配菜单 → 1014。
	if _, code := doRoleRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/role/%d/menus", super.ID),
		`{"menuIds":[]}`); code != errno.CodeSuperRoleProtected {
		t.Fatalf("assign super menus: code = %d, want 1014", code)
	}
}
