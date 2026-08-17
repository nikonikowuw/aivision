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

// setupUserAPIEngine 用真实 sqlite + 真实 service 装配 user handler 路由。
func setupUserAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "user")

	userRepo := repository.NewUserRepository(db)
	deptRepo := repository.NewDepartmentRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	handler := api.NewUserHandler(service.NewUserService(userRepo, deptRepo, roleRepo))

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	group := engine.Group("/api/user")
	{
		group.GET("/page", handler.GetPage)
		group.POST("", handler.CreateUser)
		group.PUT("/:id", handler.UpdateUser)
		group.DELETE("/:id", handler.DeleteUser)
		group.PUT("/:id/reset-password", handler.ResetPassword)
		group.GET("/:id/roles", handler.GetRoleIDs)
		group.PUT("/:id/roles", handler.AssignRoles)
		group.PUT("/:id/status", handler.UpdateStatus)
	}
	return engine, db
}

// doUserRequest 发起请求并解析统一响应体 {code,data,message}。
func doUserRequest(t *testing.T, engine *gin.Engine, method, path, body string) (int, int) {
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

func TestUserAPI_CRUDAndProtection(t *testing.T) {
	engine, db := setupUserAPIEngine(t)

	// 创建一个部门和角色
	dept := model.Department{Name: "研发部", Status: model.StatusEnabled}
	if err := db.Create(&dept).Error; err != nil {
		t.Fatalf("create dept: %v", err)
	}

	role := model.Role{Name: "管理员", Code: "admin_role", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}

	// 1. 空 body 创建 → 1009
	if _, code := doUserRequest(t, engine, http.MethodPost, "/api/user", ""); code != errno.CodeInvalidParam {
		t.Fatalf("empty body: code = %d, want 1009", code)
	}

	// 2. 创建用户成功
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user", bytes.NewBufferString(fmt.Sprintf(`{
		"username": "zhangsan",
		"nickname": "张三",
		"deptId": %d
	}`, dept.ID)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	var createResp struct {
		Code int        `json:"code"`
		Data model.User `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	if createResp.Code != errno.CodeOK || createResp.Data.ID == 0 {
		t.Fatalf("create user failed: code=%d", createResp.Code)
	}
	userID := createResp.Data.ID

	// 3. 重复用户名创建 → 1003
	if _, code := doUserRequest(t, engine, http.MethodPost, "/api/user", `{"username":"zhangsan"}`); code != errno.CodeUsernameTaken {
		t.Fatalf("duplicate username: code = %d, want 1003", code)
	}

	// 4. 分页列表查询
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user/page?username=zhangsan", nil)
	engine.ServeHTTP(rec, req)
	var pageResp struct {
		Code int                    `json:"code"`
		Data service.UserPageResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pageResp); err != nil {
		t.Fatalf("unmarshal page resp: %v", err)
	}
	if pageResp.Code != errno.CodeOK || pageResp.Data.Total != 1 || pageResp.Data.Items[0].DeptName != "研发部" {
		t.Fatalf("page response mismatch: %+v", pageResp)
	}

	// 5. 分配角色与查询角色
	if _, code := doUserRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/user/%d/roles", userID),
		fmt.Sprintf(`{"roleIds":[%d]}`, role.ID)); code != errno.CodeOK {
		t.Fatalf("assign roles: code = %d, want 0", code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/user/%d/roles", userID), nil)
	engine.ServeHTTP(rec, req)
	var rolesResp struct {
		Code int      `json:"code"`
		Data []uint64 `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rolesResp); err != nil {
		t.Fatalf("unmarshal roles resp: %v", err)
	}
	if rolesResp.Code != errno.CodeOK || len(rolesResp.Data) != 1 || rolesResp.Data[0] != role.ID {
		t.Fatalf("get role ids mismatch: %+v", rolesResp)
	}

	// 6. 重置密码
	if _, code := doUserRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/user/%d/reset-password", userID), ""); code != errno.CodeOK {
		t.Fatalf("reset password: code = %d, want 0", code)
	}

	// 7. 更新状态
	if _, code := doUserRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/user/%d/status", userID), `{"status":0}`); code != errno.CodeOK {
		t.Fatalf("update status: code = %d, want 0", code)
	}

	// 8. 非法 ID 参数
	if _, code := doUserRequest(t, engine, http.MethodPut, "/api/user/invalid/status", `{"status":0}`); code != errno.CodeInvalidParam {
		t.Fatalf("invalid id param: code = %d, want 1009", code)
	}

	// 9. 删除用户与重复删除返回 1011
	if _, code := doUserRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/user/%d", userID), ""); code != errno.CodeOK {
		t.Fatalf("delete user: code = %d, want 0", code)
	}
	if _, code := doUserRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/user/%d", userID), ""); code != errno.CodeNotFound {
		t.Fatalf("delete user second time: code = %d, want 1011", code)
	}

	// 10. admin 账号保护
	admin := model.User{Username: "admin", Status: model.StatusEnabled}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	// 禁用 admin 失败 → 1009
	if _, code := doUserRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/user/%d/status", admin.ID), `{"status":0}`); code != errno.CodeInvalidParam {
		t.Fatalf("disable admin: code = %d, want 1009", code)
	}
	// 删除 admin 失败 → 1009
	if _, code := doUserRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/user/%d", admin.ID), ""); code != errno.CodeInvalidParam {
		t.Fatalf("delete admin: code = %d, want 1009", code)
	}
}
