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
// @Summary 分页获取用户列表
// @Description 分页查询用户数据，支持用户名、昵称、状态、部门筛选
// @Tags 用户模块
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Param username query string false "用户名"
// @Param nickname query string false "昵称"
// @Param status query int false "状态 (0:禁用, 1:启用)"
// @Param deptId query int false "部门ID"
// @Success 200 {object} UserPageResponse "用户分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/page [get]
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
// @Summary 创建新用户
// @Description 创建系统用户账号，默认初始密码为 password123
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.SaveUserInput true "创建用户参数"
// @Success 200 {object} UserResponse "创建成功的用户信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user [post]
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
// @Summary 更新用户信息
// @Description 根据 ID 修改指定用户的信息
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param request body service.SaveUserInput true "更新用户参数"
// @Success 200 {object} UserResponse "更新后的用户信息"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/{id} [put]
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
// @Summary 删除指定用户
// @Description 软删除指定 ID 的用户
// @Tags 用户模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/{id} [delete]
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
// @Summary 重置用户密码
// @Description 将指定用户的密码重置为系统默认密码 (password123)
// @Tags 用户模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} NilResponse "重置成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/{id}/reset-password [put]
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
// @Summary 获取用户绑定的角色ID列表
// @Description 查询指定用户当前分配的角色 ID 列表
// @Tags 用户模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} RoleIDsResponse "角色ID数组"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/{id}/roles [get]
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
// @Summary 为用户分配角色
// @Description 覆盖更新指定用户的角色分配关系
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param request body AssignRolesInput true "角色分配参数"
// @Success 200 {object} NilResponse "分配成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/{id}/roles [put]
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
// @Summary 修改用户状态
// @Description 启用或禁用指定用户账号
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param request body UpdateStatusInput true "状态修改参数"
// @Success 200 {object} NilResponse "修改成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/{id}/status [put]
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

// BatchDeleteUserInput 批量删除用户入参。
type BatchDeleteUserInput struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,dive,gt=0"`
}

// BatchDeleteUser 批量删除用户 (DELETE /api/user/batch)。
// @Summary 批量删除用户
// @Description 批量软删除指定 ID 列表的用户
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body BatchDeleteUserInput true "批量删除参数"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/batch [delete]
func (h *UserHandler) BatchDeleteUser(c *gin.Context) {
	var input BatchDeleteUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.BatchDelete(c.Request.Context(), input.IDs); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// BatchUpdateUserStatusInput 批量更新用户状态入参。
type BatchUpdateUserStatusInput struct {
	IDs    []uint64 `json:"ids" binding:"required,min=1,dive,gt=0"`
	Status *int8    `json:"status" binding:"required"`
}

// BatchUpdateStatus 批量更新用户状态 (PUT /api/user/batch-status)。
// @Summary 批量更新用户状态
// @Description 批量启用或禁用指定用户
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body BatchUpdateUserStatusInput true "批量状态修改参数"
// @Success 200 {object} NilResponse "更新成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/user/batch-status [put]
func (h *UserHandler) BatchUpdateStatus(c *gin.Context) {
	var input BatchUpdateUserStatusInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}

	if err := h.svc.BatchUpdateStatus(c.Request.Context(), input.IDs, *input.Status); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// GetProfile 获取当前登录用户的个人资料 (GET /api/user/profile)。
// @Summary 获取当前登录用户个人资料
// @Description 获取当前认证用户的基本资料（包含昵称、邮箱、手机号等）
// @Tags 用户模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} service.CurrentProfileDTO "个人资料"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/profile [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	identity, ok := requireIdentity(c)
	if !ok {
		return
	}

	profile, err := h.svc.GetCurrentProfile(c.Request.Context(), identity.UserID)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, profile)
}

// UpdateProfile 更新当前登录用户的个人资料 (PUT /api/user/profile)。
// @Summary 修改当前登录用户个人资料
// @Description 修改当前认证用户的昵称、邮箱、手机号和个人简介
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.UpdateCurrentProfileInput true "修改资料参数"
// @Success 200 {object} service.CurrentProfileDTO "更新后的个人资料"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/profile [put]
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	identity, ok := requireIdentity(c)
	if !ok {
		return
	}

	var input service.UpdateCurrentProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	profile, err := h.svc.UpdateCurrentProfile(c.Request.Context(), identity.UserID, &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, profile)
}

// ChangePassword 修改当前登录用户的密码 (PUT /api/user/profile/password)。
// @Summary 修改当前登录用户密码
// @Description 校验旧密码并更新为新密码，同时吊销当前用户的所有 Refresh Token
// @Tags 用户模块
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.ChangeCurrentPasswordInput true "修改密码参数"
// @Success 200 {object} NilResponse "修改成功"
// @Failure 400 {object} response.Result "参数错误或旧密码错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/profile/password [put]
func (h *UserHandler) ChangePassword(c *gin.Context) {
	identity, ok := requireIdentity(c)
	if !ok {
		return
	}

	var input service.ChangeCurrentPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	if err := h.svc.ChangeCurrentPassword(c.Request.Context(), identity.UserID, &input); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}
