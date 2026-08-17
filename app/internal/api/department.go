package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

type DepartmentHandler struct {
	srv service.DeptService
}

func NewDepartmentHandler(srv service.DeptService) *DepartmentHandler {
	return &DepartmentHandler{srv: srv}
}

// GetDeptTree 获取全量部门树 (GET /api/dept/tree)
func (h *DepartmentHandler) GetDeptTree(c *gin.Context) {
	tree, err := h.srv.GetDeptTree(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, tree)
}

// CreateDept 创建部门 (POST /api/dept)
func (h *DepartmentHandler) CreateDept(c *gin.Context) {
	var req service.SaveDeptInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	dept, err := h.srv.CreateDept(c.Request.Context(), &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, dept)
}

// UpdateDept 更新部门 (PUT /api/dept/:id)
func (h *DepartmentHandler) UpdateDept(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var req service.SaveDeptInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	dept, err := h.srv.UpdateDept(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, dept)
}

// DeleteDept 删除部门 (DELETE /api/dept/:id)
func (h *DepartmentHandler) DeleteDept(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.srv.DeleteDept(c.Request.Context(), id); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}
