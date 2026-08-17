package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
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
	svc service.AuthService
	cfg *config.Config
}

// NewAuthHandler 创建 AuthHandler 实例。
func NewAuthHandler(svc service.AuthService, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		svc: svc,
		cfg: cfg,
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
func (h *AuthHandler) Logout(c *gin.Context) {
	cookieToken, _ := c.Cookie(refreshTokenCookieName)
	var revokeErr error
	if cookieToken != "" {
		revokeErr = h.svc.Logout(c.Request.Context(), cookieToken)
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
