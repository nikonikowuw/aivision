package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

type MenuHandler struct {
	srv service.MenuService
}

func NewMenuHandler(srv service.MenuService) *MenuHandler {
	return &MenuHandler{srv: srv}
}

// GetMenuTree 获取全量菜单树 (GET /api/menu/tree)
func (h *MenuHandler) GetMenuTree(c *gin.Context) {
	tree, err := h.srv.GetMenuTree(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, tree)
}

// GetUserMenuTree 获取当前用户关联的菜单树 (GET /api/menu/all)
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
