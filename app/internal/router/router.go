// Package router 装配 gin engine 并注册 HTTP 路由。
package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
)

const (
	apiRoutePath     = "/api"
	menuRoutePath    = "/menu"
	roleRoutePath    = "/role"
	deptRoutePath    = "/dept"
	oplogRoutePath   = "/oplog"
	pageRoutePath    = "/page"
	idRoutePath      = "/:id"
	menuIDsRoutePath = "/menu-ids"
	menusRoutePath   = "/menus"
)

// Deps 路由依赖集合：新增业务模块时扩展结构体字段，避免 New 签名随之膨胀
// （wire.Struct 按字段自动装配，见 cmd/api/wire.go）。
type Deps struct {
	ErrorHandler        gin.HandlerFunc
	AuthMiddleware      *middleware.AuthMiddleware
	PermMiddleware      *middleware.PermMiddleware
	OplogMiddleware     *middleware.OplogMiddleware
	MenuHandler         *api.MenuHandler
	RoleHandler         *api.RoleHandler
	DepartmentHandler   *api.DepartmentHandler
	OperationLogHandler *api.OperationLogHandler
}

// New 创建 gin engine 并注册路由。
func New(cfg *config.Config, deps Deps) *gin.Engine {
	// 与 logger.New 的 zap 解析一致，级别比较大小写不敏感。
	if strings.EqualFold(cfg.Log.Level, "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	// 启用方法不匹配时触发 NoMethod（否则统一走 NoRoute，无法区分 404/405）。
	engine.HandleMethodNotAllowed = true
	// 操作日志包住 recovery 与统一错误处理，以便记录最终 HTTP 状态（包括 panic 的 500）。
	engine.Use(deps.OplogMiddleware.Handler)
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		response.WriteFail(c, http.StatusInternalServerError, errno.CodeInternal)
	}))
	// 统一错误处理：位于 recovery 之后、业务路由之前；handler 仅 c.Error，由中间件输出。
	engine.Use(deps.ErrorHandler)
	// 不信任任何代理：ClientIP 直接用 RemoteAddr，避免伪造 X-Forwarded-For。
	// 生产若置于反向代理后，需在此显式配置代理网段。
	_ = engine.SetTrustedProxies(nil)
	// NoRoute / NoMethod 输出统一 404 / 405 响应。
	engine.NoRoute(func(c *gin.Context) {
		response.WriteFail(c, http.StatusNotFound, errno.CodeNotFound)
	})
	engine.NoMethod(func(c *gin.Context) {
		response.WriteFail(c, http.StatusMethodNotAllowed, errno.CodeMethodNotAllowed)
	})

	apiGroup := engine.Group(apiRoutePath)
	// 所有 API 路由默认先认证，再执行写操作权限默认拒绝；公共认证接口由中间件白名单放行。
	apiGroup.Use(deps.AuthMiddleware.Handler)
	apiGroup.Use(deps.PermMiddleware.Handler)
	{
		menuGroup := apiGroup.Group(menuRoutePath)
		{
			menuGroup.GET("/tree", deps.MenuHandler.GetMenuTree)
			menuGroup.GET("/all", deps.MenuHandler.GetUserMenuTree)
			menuGroup.POST("", deps.MenuHandler.CreateMenu)
			menuGroup.PUT(idRoutePath, deps.MenuHandler.UpdateMenu)
			menuGroup.DELETE(idRoutePath, deps.MenuHandler.DeleteMenu)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+menuRoutePath, "system:menu:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+menuRoutePath+idRoutePath, "system:menu:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+menuRoutePath+idRoutePath, "system:menu:delete")

		roleGroup := apiGroup.Group(roleRoutePath)
		{
			roleGroup.GET(pageRoutePath, deps.RoleHandler.GetPage)
			roleGroup.POST("", deps.RoleHandler.CreateRole)
			roleGroup.PUT(idRoutePath, deps.RoleHandler.UpdateRole)
			roleGroup.DELETE(idRoutePath, deps.RoleHandler.DeleteRole)
			roleGroup.GET(idRoutePath+menuIDsRoutePath, deps.RoleHandler.GetMenuIDs)
			roleGroup.PUT(idRoutePath+menusRoutePath, deps.RoleHandler.AssignMenus)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+roleRoutePath, "system:role:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+roleRoutePath+idRoutePath, "system:role:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+roleRoutePath+idRoutePath, "system:role:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+roleRoutePath+idRoutePath+menusRoutePath, "system:role:assign-menu")

		deptGroup := apiGroup.Group(deptRoutePath)
		{
			deptGroup.GET("/tree", deps.DepartmentHandler.GetDeptTree)
			deptGroup.POST("", deps.DepartmentHandler.CreateDept)
			deptGroup.PUT(idRoutePath, deps.DepartmentHandler.UpdateDept)
			deptGroup.DELETE(idRoutePath, deps.DepartmentHandler.DeleteDept)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+deptRoutePath, "system:dept:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+deptRoutePath+idRoutePath, "system:dept:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+deptRoutePath+idRoutePath, "system:dept:delete")

		oplogGroup := apiGroup.Group(oplogRoutePath)
		{
			oplogGroup.GET(pageRoutePath, deps.OperationLogHandler.GetPage)
			oplogGroup.GET(idRoutePath, deps.OperationLogHandler.GetByID)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+pageRoutePath, "system:log")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+idRoutePath, "system:log")
	}

	return engine
}
