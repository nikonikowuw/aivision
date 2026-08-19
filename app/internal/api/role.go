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
// @Summary 分页获取角色列表
// @Description 分页查询角色数据，支持角色名称、角色编码、状态筛选
// @Tags 角色模块
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Param name query string false "角色名称"
// @Param code query string false "角色编码"
// @Param status query int false "状态 (0:禁用, 1:启用)"
// @Success 200 {object} RolePageResponse "角色分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/role/page [get]
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
// @Summary 创建角色
// @Description 创建新的系统角色
// @Tags 角色模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.SaveRoleInput true "创建角色参数"
// @Success 200 {object} RoleResponse "创建成功的角色信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/role [post]
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
// @Summary 更新角色
// @Description 根据 ID 更新角色信息
// @Tags 角色模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "角色ID"
// @Param request body service.SaveRoleInput true "更新角色参数"
// @Success 200 {object} RoleResponse "更新后的角色信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/role/{id} [put]
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
// @Summary 删除角色
// @Description 删除指定 ID 的角色（内置角色禁止删除）
// @Tags 角色模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "角色ID"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/role/{id} [delete]
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
// @Summary 批量删除角色
// @Description 批量删除指定 ID 列表的角色
// @Tags 角色模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body BatchDeleteRoleRequest true "批量删除参数"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/role/batch [delete]
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
// @Summary 获取角色拥有的菜单ID列表
// @Description 查询指定角色当前关联的菜单 ID 集合
// @Tags 角色模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "角色ID"
// @Success 200 {object} MenuIDsResponse "菜单ID列表"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/role/{id}/menu-ids [get]
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
// @Summary 为角色分配菜单权限
// @Description 覆盖更新指定角色的菜单关联关系
// @Tags 角色模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "角色ID"
// @Param request body AssignRoleMenusRequest true "分配菜单参数"
// @Success 200 {object} NilResponse "分配成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/role/{id}/menus [put]
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
