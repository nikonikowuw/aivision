package middleware

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// PermCodeAuthenticated 是哨兵权限码，表示该路由仅要求已认证（无需特定角色权限）。
const PermCodeAuthenticated = "__authenticated__"

// PermMiddleware 接口权限校验中间件。
type PermMiddleware struct {
	menuRepo repository.MenuRepository
	routes   map[permissionRoute]string
}

type permissionRoute struct {
	method string
	path   string
}

// NewPermMiddleware 创建权限中间件。
func NewPermMiddleware(menuRepo repository.MenuRepository) *PermMiddleware {
	return &PermMiddleware{
		menuRepo: menuRepo,
		routes:   make(map[permissionRoute]string),
	}
}

// Register 声明路由所需的权限码。
// 未注册的 API 写路由由 Handler 默认拒绝，未注册的读路由仅要求认证。
// 传入 PermCodeAuthenticated 表示该路由仅要求已认证，无需特定角色权限。
func (m *PermMiddleware) Register(method, path, code string) {
	if method == "" || path == "" || code == "" {
		return
	}
	m.routes[permissionRoute{method: strings.ToUpper(method), path: path}] = code
}

// Handler 对 API 路由执行权限校验；已声明权限码的路由无论读写都校验，
// 已声明为 PermCodeAuthenticated 的路由仅要求认证即放行，
// 未声明权限码的写路由默认拒绝，未声明权限码的读路由仅要求认证。
func (m *PermMiddleware) Handler(c *gin.Context) {
	if !isAPIPath(c.Request.URL.Path) {
		c.Next()
		return
	}

	path := c.FullPath()
	if path == "" {
		c.Next()
		return
	}
	if isPublicAuthPath(path) {
		c.Next()
		return
	}

	method := strings.ToUpper(c.Request.Method)
	code, ok := m.routes[permissionRoute{method: method, path: path}]
	if ok && code == PermCodeAuthenticated {
		if _, hasID := IdentityFromContext(c); !hasID {
			c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
			c.Abort()
			return
		}
		c.Next()
		return
	}
	if ok && code != "" {
		m.check(c, code)
		return
	}

	if isWriteMethod(method) {
		c.Error(errno.NewError(errno.CodeForbidden)) //nolint:errcheck
		c.Abort()
		return
	}

	c.Next()
}

func (m *PermMiddleware) check(c *gin.Context, code string) {
	if code == "" {
		c.Error(errno.NewError(errno.CodeForbidden)) //nolint:errcheck
		c.Abort()
		return
	}

	identity, ok := IdentityFromContext(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeUnauthorized)) //nolint:errcheck
		c.Abort()
		return
	}

	// super 角色直接放行。
	if slices.Contains(identity.RoleCodes, model.RoleSuperCode) {
		c.Next()
		return
	}

	perms, err := m.menuRepo.GetPermissionsByRoleIDs(c.Request.Context(), identity.RoleIDs)
	if err != nil {
		c.Error(err) //nolint:errcheck
		c.Abort()
		return
	}

	if !slices.Contains(perms, code) && !slices.Contains(perms, "*") {
		c.Error(errno.NewError(errno.CodeForbidden)) //nolint:errcheck
		c.Abort()
		return
	}

	c.Next()
}
