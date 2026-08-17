package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// UserHandler 用户 API。
type UserHandler struct {
	svc service.UserService
}

// NewUserHandler 创建 UserHandler 实例。
func NewUserHandler(svc service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// GetPage 获取用户分页列表 (GET /api/user/page)。
func (h *UserHandler) GetPage(c *gin.Context) {
	var query service.UserPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	res, err := h.svc.GetPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, res)
}

// CreateUser 创建用户 (POST /api/user)。
func (h *UserHandler) CreateUser(c *gin.Context) {
	var input service.SaveUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	u, err := h.svc.CreateUser(c.Request.Context(), &input)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, u)
}

// UpdateUser 更新用户 (PUT /api/user/:id)。
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var input service.SaveUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	u, err := h.svc.UpdateUser(c.Request.Context(), id, &input)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, u)
}

// DeleteUser 删除用户 (DELETE /api/user/:id)。
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.DeleteUser(c.Request.Context(), id); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// ResetPassword 重置用户密码 (PUT /api/user/:id/reset-password)。
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.ResetPassword(c.Request.Context(), id, service.DefaultUserPassword); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// GetRoleIDs 获取用户绑定的角色 ID 列表 (GET /api/user/:id/roles)。
func (h *UserHandler) GetRoleIDs(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	roleIDs, err := h.svc.GetRoleIDs(c.Request.Context(), id)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, roleIDs)
}

// AssignRolesInput 分配角色入参。
type AssignRolesInput struct {
	RoleIDs []uint64 `json:"roleIds"`
}

// AssignRoles 分配角色 (PUT /api/user/:id/roles)。
func (h *UserHandler) AssignRoles(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var input AssignRolesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.AssignRoles(c.Request.Context(), id, input.RoleIDs); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// UpdateStatusInput 更新状态入参。
type UpdateStatusInput struct {
	Status int8 `json:"status" binding:"oneof=0 1"`
}

// UpdateStatus 启停用用户 (PUT /api/user/:id/status)。
func (h *UserHandler) UpdateStatus(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	var input UpdateStatusInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.UpdateStatus(c.Request.Context(), id, input.Status); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}
