package middleware

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

const authIdentityKey = "auth.identity"

// Identity 是认证中间件解析出的当前用户身份和有效角色。
type Identity struct {
	UserID    uint64
	Username  string
	RoleIDs   []uint64
	RoleCodes []string
}

// AuthMiddleware 校验 Bearer access token，并从数据库加载用户的启用角色。
type AuthMiddleware struct {
	repo   repository.AuthRepository
	secret []byte
}

// NewAuthMiddleware 创建 JWT 认证中间件。
func NewAuthMiddleware(repo repository.AuthRepository, cfg *config.Config) *AuthMiddleware {
	return &AuthMiddleware{repo: repo, secret: []byte(cfg.JWT.Secret)}
}

// Handler 验证 HS256 access token，并将身份放入 Gin Context。
func (m *AuthMiddleware) Handler(c *gin.Context) {
	if isPublicAuthPath(c.Request.URL.Path) {
		c.Next()
		return
	}

	rawToken, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok || len(m.secret) == 0 {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(
		rawToken,
		&claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				algorithm := "<nil>"
				if token.Method != nil {
					algorithm = token.Method.Alg()
				}
				return nil, fmt.Errorf("unexpected signing method %q", algorithm)
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil || !token.Valid || claims.ExpiresAt == nil {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	activeIdentity, err := m.repo.GetActiveIdentity(c.Request.Context(), userID)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}
	if activeIdentity == nil || len(activeIdentity.Roles) == 0 {
		// 用户不存在、被禁用或无任何启用角色，均视为未认证。
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	identity := Identity{
		UserID:    userID,
		Username:  activeIdentity.Username,
		RoleIDs:   make([]uint64, 0, len(activeIdentity.Roles)),
		RoleCodes: make([]string, 0, len(activeIdentity.Roles)),
	}
	for _, role := range activeIdentity.Roles {
		identity.RoleIDs = append(identity.RoleIDs, role.ID)
		identity.RoleCodes = append(identity.RoleCodes, role.Code)
	}
	c.Set(authIdentityKey, identity)
	c.Next()
}

// IdentityFromContext 读取当前请求的认证身份。
func IdentityFromContext(c *gin.Context) (Identity, bool) {
	value, exists := c.Get(authIdentityKey)
	if !exists {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}

func isPublicAuthPath(path string) bool {
	switch path {
	case "/api/auth/login", "/api/auth/refresh", "/api/auth/logout":
		return true
	default:
		return false
	}
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
