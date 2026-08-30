package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

const (
	refreshTokenCookieName = "jwt"
	cookiePath             = "/"
)

// LoginInput 登录入参。
type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthHandler 处理用户认证、令牌刷新、登出及用户信息/权限码查询。
type AuthHandler struct {
	svc     service.AuthService
	authMid *middleware.AuthMiddleware
	cfg     *config.Config
}

// NewAuthHandler 创建 AuthHandler 实例。
func NewAuthHandler(svc service.AuthService, authMid *middleware.AuthMiddleware, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		svc:     svc,
		authMid: authMid,
		cfg:     cfg,
	}
}

func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshTokenCookieName, token, maxAge, cookiePath, "", h.cfg.JWT.SecureCookie, true)
}

func clientInfo(c *gin.Context) service.ClientInfo {
	return service.ClientInfo{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
	}
}

// Login 处理用户登录 (POST /api/auth/login)。
// @Summary 用户登录
// @Description 使用用户名和密码登录获取 AccessToken，同时在 Cookie 中写入 RefreshToken
// @Tags 认证模块
// @Accept json
// @Produce json
// @Param request body LoginInput true "登录参数"
// @Success 200 {object} LoginResponse "成功返回用户信息及 AccessToken"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "用户名或密码错误"
// @Router /api/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || input.Password == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	client := clientInfo(c)

	result, err := h.svc.Login(c.Request.Context(), input.Username, input.Password, client)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	h.setRefreshTokenCookie(c, result.RefreshToken, int(h.cfg.JWT.RefreshTTL.Seconds()))

	response.Success(c, result)
}

// RefreshToken 刷新 access token 并轮换 refresh token (POST /api/auth/refresh)。
// 响应体为裸 token 字符串，对齐 vben doRefreshToken 契约。
// @Summary 刷新令牌
// @Description 从 HttpOnly Cookie 中读取 RefreshToken，签发新的 AccessToken 并轮换 RefreshToken Cookie
// @Tags 认证模块
// @Produce plain
// @Success 200 {string} string "新的 AccessToken"
// @Failure 401 {object} response.Result "未授权或 RefreshToken 无效"
// @Router /api/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	cookieToken, err := c.Cookie(refreshTokenCookieName)
	if err != nil || cookieToken == "" {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
		return
	}

	client := clientInfo(c)

	pair, err := h.svc.RefreshToken(c.Request.Context(), cookieToken, client)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	// 轮换写入新 refresh token cookie
	h.setRefreshTokenCookie(c, pair.RefreshToken, int(h.cfg.JWT.RefreshTTL.Seconds()))

	// 裸字符串返回
	c.String(http.StatusOK, pair.AccessToken)
}

// Logout 处理用户登出 (POST /api/auth/logout)。
// @Summary 用户登出
// @Description 注销当前的 RefreshToken 并清除客户端 Cookie
// @Tags 认证模块
// @Produce json
// @Success 200 {object} NilResponse "登出成功"
// @Router /api/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	cookieToken, _ := c.Cookie(refreshTokenCookieName)

	// 1. 先通过 service 登出（内部通过 refresh token 查出操作人并吊销）
	operator, revokeErr := h.svc.Logout(c.Request.Context(), cookieToken)
	if operator != nil {
		middleware.SetIdentity(c, middleware.Identity{
			UserID:    operator.UserID,
			Username:  operator.Username,
			RoleIDs:   operator.RoleIDs,
			RoleCodes: operator.RoleCodes,
		})
	} else if h.authMid != nil {
		// 2. 备用：从 Bearer token 中提取并验证操作人身份（应对 cookie 缺失或无对应记录）
		if identity, ok := h.authMid.ExtractBearerIdentity(c); ok {
			middleware.SetIdentity(c, identity)
		}
	}

	// 清除 cookie
	h.setRefreshTokenCookie(c, "", -1)

	if revokeErr != nil {
		c.Error(revokeErr) //nolint:errcheck
		return
	}

	response.Success(c, nil)
}

// GetUserInfo 获取当前登录用户信息 (GET /api/user/info)。
// @Summary 获取当前登录用户信息
// @Description 根据请求头中的 Bearer Token 获取当前登录用户的详细信息
// @Tags 认证模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} UserInfoResponse "当前用户信息"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/user/info [get]
func (h *AuthHandler) GetUserInfo(c *gin.Context) {
	identity, ok := middleware.IdentityFromContext(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
		return
	}

	info, err := h.svc.GetUserInfo(c.Request.Context(), identity.UserID)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	response.Success(c, info)
}

// GetAccessCodes 获取当前登录用户的权限码集合 (GET /api/auth/codes)。
// @Summary 获取当前用户权限码集合
// @Description 获取当前登录用户拥有的全部按钮权限码
// @Tags 认证模块
// @Security BearerAuth
// @Produce json
// @Success 200 {object} AccessCodesResponse "权限码列表"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/auth/codes [get]
func (h *AuthHandler) GetAccessCodes(c *gin.Context) {
	identity, ok := middleware.IdentityFromContext(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
		return
	}

	codes, err := h.svc.GetAccessCodes(c.Request.Context(), identity.RoleCodes, identity.RoleIDs)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	response.Success(c, codes)
}
