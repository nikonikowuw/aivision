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
// @Summary 获取全量部门树
// @Description 查询系统所有部门并构建为树形结构
// @Tags 部门模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} DeptTreeResponse "部门树数据"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/dept/tree [get]
func (h *DepartmentHandler) GetDeptTree(c *gin.Context) {
	tree, err := h.srv.GetDeptTree(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, tree)
}

// CreateDept 创建部门 (POST /api/dept)
// @Summary 创建部门
// @Description 新增部门节点
// @Tags 部门模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.SaveDeptInput true "创建部门参数"
// @Success 200 {object} DeptResponse "创建成功的部门信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/dept [post]
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
// @Summary 更新部门
// @Description 更新指定 ID 的部门信息
// @Tags 部门模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "部门ID"
// @Param request body service.SaveDeptInput true "更新部门参数"
// @Success 200 {object} DeptResponse "更新后的部门信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/dept/{id} [put]
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
// @Summary 删除部门
// @Description 删除指定 ID 的部门（有子部门或包含用户时不可删除）
// @Tags 部门模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "部门ID"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/dept/{id} [delete]
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
