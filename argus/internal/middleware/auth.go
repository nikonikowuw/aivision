package middleware

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
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

// VerifyToken 解析并验证 Bearer access token 签名与时效，成功返回已激活的用户身份。
func (m *AuthMiddleware) VerifyToken(c *gin.Context, rawToken string) (Identity, error) {
	if len(m.secret) == 0 {
		return Identity{}, errno.NewError(errno.CodeUnauthorized)
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
		return Identity{}, errno.NewError(errno.CodeUnauthorized)
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 {
		return Identity{}, errno.NewError(errno.CodeUnauthorized)
	}

	activeIdentity, err := m.repo.GetActiveIdentity(c.Request.Context(), userID)
	if err != nil {
		return Identity{}, err
	}
	if activeIdentity == nil || len(activeIdentity.Roles) == 0 {
		return Identity{}, errno.NewError(errno.CodeUnauthorized)
	}

	username, roleIDs, roleCodes := activeIdentity.ToIdentity(userID)
	return Identity{
		UserID:    userID,
		Username:  username,
		RoleIDs:   roleIDs,
		RoleCodes: roleCodes,
	}, nil
}

// Handler 验证 HS256 access token，并将身份放入 Gin Context。
func (m *AuthMiddleware) Handler(c *gin.Context) {
	if isPublicAuthPath(c.Request.URL.Path) {
		c.Next()
		return
	}

	rawToken, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		// 允许图片等静态媒体请求通过 URL query token (如 ?token=xxx) 传递凭证
		if qToken := c.Query("token"); qToken != "" {
			rawToken = qToken
			ok = true
		}
	}
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	identity, err := m.VerifyToken(c, rawToken)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		c.Abort()
		return
	}

	c.Set(authIdentityKey, identity)
	c.Next()
}

// ExtractBearerIdentity 尝试从请求头 Authorization 中验证并提取操作人身份（供公共接口如登出日志使用）。
func (m *AuthMiddleware) ExtractBearerIdentity(c *gin.Context) (Identity, bool) {
	rawToken, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		return Identity{}, false
	}
	identity, err := m.VerifyToken(c, rawToken)
	if err != nil {
		return Identity{}, false
	}
	return identity, true
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

// SetIdentity 将认证身份写入请求上下文，供 oplog 等下游读取。
func SetIdentity(c *gin.Context, identity Identity) {
	c.Set(authIdentityKey, identity)
}

// SetIdentityForTest 为单元测试设置请求认证身份。
// 仅供测试包使用，生产代码不应调用。
func SetIdentityForTest(c *gin.Context, identity Identity) {
	c.Set(authIdentityKey, identity)
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
