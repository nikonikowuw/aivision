package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// RoleHandler 角色处理器。
type RoleHandler struct {
	srv service.RoleService
}

// NewRoleHandler 创建 RoleHandler 实例。
func NewRoleHandler(srv service.RoleService) *RoleHandler {
	return &RoleHandler{srv: srv}
}

// AssignRoleMenusRequest 角色分配菜单请求体。
type AssignRoleMenusRequest struct {
	MenuIDs []uint64 `json:"menuIds"` // 省略/空数组 = 清空分配
}

// GetPage 获取角色分页列表 (GET /api/role/page)
func (h *RoleHandler) GetPage(c *gin.Context) {
	var query service.RolePageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	res, err := h.srv.GetPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, res)
}

// CreateRole 创建角色 (POST /api/role)
func (h *RoleHandler) CreateRole(c *gin.Context) {
	var req service.SaveRoleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	role, err := h.srv.CreateRole(c.Request.Context(), &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, role)
}

// UpdateRole 更新角色 (PUT /api/role/:id)
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var req service.SaveRoleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	role, err := h.srv.UpdateRole(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, role)
}

// DeleteRole 删除角色 (DELETE /api/role/:id)
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.srv.DeleteRole(c.Request.Context(), id); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// BatchDeleteRoleRequest 批量删除角色请求体。
type BatchDeleteRoleRequest struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,dive,gt=0"`
}

// BatchDeleteRole 批量删除角色 (DELETE /api/role/batch)
func (h *RoleHandler) BatchDeleteRole(c *gin.Context) {
	var req BatchDeleteRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.srv.BatchDelete(c.Request.Context(), req.IDs); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// GetMenuIDs 获取角色已分配的菜单 id 集 (GET /api/role/:id/menu-ids)
func (h *RoleHandler) GetMenuIDs(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	menuIDs, err := h.srv.GetMenuIDs(c.Request.Context(), id)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, menuIDs)
}

// AssignMenus 覆盖式分配角色菜单 (PUT /api/role/:id/menus)
func (h *RoleHandler) AssignMenus(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var req AssignRoleMenusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.srv.AssignMenus(c.Request.Context(), id, req.MenuIDs); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}
