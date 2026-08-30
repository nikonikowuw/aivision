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

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

func setupDepartmentAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "department")
	handler := api.NewDepartmentHandler(
		service.NewDeptService(repository.NewDepartmentRepository(db)),
	)

	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	group := engine.Group("/api/dept")
	{
		group.GET("/tree", handler.GetDeptTree)
		group.POST("", handler.CreateDept)
		group.PUT("/:id", handler.UpdateDept)
		group.DELETE("/:id", handler.DeleteDept)
	}
	return engine, db
}

func doDepartmentRequest(t *testing.T, engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	return rec
}

func TestDepartmentAPI_CRUDAndTree(t *testing.T) {
	engine, _ := setupDepartmentAPIEngine(t)

	// 创建时显式禁用必须保留 status=0，并规范化 name/leader。
	rec := doDepartmentRequest(t, engine, http.MethodPost, "/api/dept", `{"name":"  Root  ","leader":"  Alice  ","status":0}`)
	var createResp struct {
		Code int              `json:"code"`
		Data model.Department `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if rec.Code != http.StatusOK || createResp.Code != errno.CodeOK {
		t.Fatalf("create status=%d code=%d, want 200/0", rec.Code, createResp.Code)
	}
	if createResp.Data.ID == 0 || createResp.Data.Name != "Root" || createResp.Data.Leader != "Alice" || createResp.Data.Status != model.StatusDisabled {
		t.Fatalf("created department = %+v, want normalized disabled root", createResp.Data)
	}
	rootID := createResp.Data.ID

	// 创建子部门时省略 status，service 应默认启用。
	rec = doDepartmentRequest(t, engine, http.MethodPost, "/api/dept", fmt.Sprintf(`{"parentId":%d,"name":"Child","sort":1}`, rootID))
	var childResp struct {
		Code int              `json:"code"`
		Data model.Department `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &childResp); err != nil {
		t.Fatalf("unmarshal child response: %v", err)
	}
	if childResp.Code != errno.CodeOK || childResp.Data.Status != model.StatusEnabled {
		t.Fatalf("created child = %+v, want enabled", childResp.Data)
	}
	childID := childResp.Data.ID

	// 树查询返回嵌套子部门。
	rec = doDepartmentRequest(t, engine, http.MethodGet, "/api/dept/tree", "")
	var treeResp struct {
		Code int                         `json:"code"`
		Data []*model.DepartmentTreeNode `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &treeResp); err != nil {
		t.Fatalf("unmarshal tree response: %v", err)
	}
	if treeResp.Code != errno.CodeOK || len(treeResp.Data) != 1 || len(treeResp.Data[0].Children) != 1 || treeResp.Data[0].Children[0].ID != childID {
		t.Fatalf("tree response = %+v, want root with child", treeResp.Data)
	}

	// 编辑部门并保持未提供的 status 不变。
	rec = doDepartmentRequest(t, engine, http.MethodPut, fmt.Sprintf("/api/dept/%d", childID), fmt.Sprintf(`{"parentId":%d,"name":"Child Updated"}`, rootID))
	var updateResp struct {
		Code int              `json:"code"`
		Data model.Department `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("unmarshal update response: %v", err)
	}
	if updateResp.Code != errno.CodeOK || updateResp.Data.Name != "Child Updated" || updateResp.Data.Status != model.StatusEnabled {
		t.Fatalf("updated department = %+v, want renamed enabled child", updateResp.Data)
	}

	// 有子部门时父部门删除返回 1007；删除叶子后父部门可删。
	rec = doDepartmentRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/dept/%d", rootID), "")
	var blockedResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &blockedResp); err != nil {
		t.Fatalf("unmarshal blocked delete response: %v", err)
	}
	if blockedResp.Code != errno.CodeDeptHasChildren {
		t.Fatalf("blocked delete code = %d, want %d", blockedResp.Code, errno.CodeDeptHasChildren)
	}

	if rec = doDepartmentRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/dept/%d", childID), ""); rec.Code != http.StatusOK {
		t.Fatalf("delete child status = %d, want 200", rec.Code)
	}
	if rec = doDepartmentRequest(t, engine, http.MethodDelete, fmt.Sprintf("/api/dept/%d", rootID), ""); rec.Code != http.StatusOK {
		t.Fatalf("delete root status = %d, want 200", rec.Code)
	}

	rec = doDepartmentRequest(t, engine, http.MethodGet, "/api/dept/tree", "")
	var emptyResp struct {
		Code int                         `json:"code"`
		Data []*model.DepartmentTreeNode `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &emptyResp); err != nil {
		t.Fatalf("unmarshal empty tree response: %v", err)
	}
	if emptyResp.Code != errno.CodeOK || emptyResp.Data == nil || len(emptyResp.Data) != 0 {
		t.Fatalf("empty tree response = %+v, want non-nil empty data", emptyResp.Data)
	}
}
