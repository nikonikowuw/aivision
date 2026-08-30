package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"argus/app/internal/middleware"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

type MenuHandler struct {
	srv service.MenuService
}

func NewMenuHandler(srv service.MenuService) *MenuHandler {
	return &MenuHandler{srv: srv}
}

// GetMenuTree 获取全量菜单树 (GET /api/menu/tree)
// @Summary 获取全量菜单树
// @Description 查询系统所有菜单/按钮并构建为树形结构（用于菜单管理页面）
// @Tags 菜单模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} MenuTreeResponse "菜单树数据"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/menu/tree [get]
func (h *MenuHandler) GetMenuTree(c *gin.Context) {
	tree, err := h.srv.GetMenuTree(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, tree)
}

// GetUserMenuTree 获取当前用户关联的菜单树 (GET /api/menu/all)
// @Summary 获取当前用户可访问菜单树
// @Description 根据当前用户角色权限，生成用于前端动态挂载路由的菜单结构
// @Tags 菜单模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} UserMenuTreeResponse "前端动态路由树"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/menu/all [get]
func (h *MenuHandler) GetUserMenuTree(c *gin.Context) {
	identity, ok := middleware.IdentityFromContext(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	routes, err := h.srv.GetUserMenuTree(c.Request.Context(), identity.RoleCodes, identity.RoleIDs)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, routes)
}

// CreateMenu 创建菜单 (POST /api/menu)
// @Summary 创建菜单/按钮
// @Description 新增菜单、目录或权限按钮节点
// @Tags 菜单模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.SaveMenuInput true "创建菜单参数"
// @Success 200 {object} MenuResponse "创建成功的菜单信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/menu [post]
func (h *MenuHandler) CreateMenu(c *gin.Context) {
	var req service.SaveMenuInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	menu, err := h.srv.CreateMenu(c.Request.Context(), &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, menu)
}

// UpdateMenu 更新菜单 (PUT /api/menu/:id)
// @Summary 更新菜单/按钮
// @Description 更新指定 ID 的菜单节点信息
// @Tags 菜单模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "菜单ID"
// @Param request body service.SaveMenuInput true "更新菜单参数"
// @Success 200 {object} MenuResponse "更新后的菜单信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/menu/{id} [put]
func (h *MenuHandler) UpdateMenu(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var req service.SaveMenuInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	menu, err := h.srv.UpdateMenu(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, menu)
}

// DeleteMenu 删除菜单 (DELETE /api/menu/:id)
// @Summary 删除菜单/按钮
// @Description 删除指定 ID 的菜单（有子菜单时不可删除）
// @Tags 菜单模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "菜单ID"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/menu/{id} [delete]
func (h *MenuHandler) DeleteMenu(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.srv.DeleteMenu(c.Request.Context(), id); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// parseIDParam 解析并校验路径参数 :id（必须为正整数）。
func parseIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// requireIdentity 从上下文获取认证身份；未认证时自动设置 401 错误并返回 false。
func requireIdentity(c *gin.Context) (middleware.Identity, bool) {
	identity, ok := middleware.IdentityFromContext(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
	}
	return identity, ok
}
