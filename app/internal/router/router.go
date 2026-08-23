// Package router 装配 gin engine 并注册 HTTP 路由。
package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "niko-vue-admin/app/docs"
	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
)

const (
	swaggerRoutePath     = "/swagger/*any"
	apiRoutePath         = "/api"
	authRoutePath        = "/auth"
	loginRoutePath       = "/login"
	refreshRoutePath     = "/refresh"
	logoutRoutePath      = "/logout"
	codesRoutePath       = "/codes"
	infoRoutePath        = "/info"
	menuRoutePath        = "/menu"
	roleRoutePath        = "/role"
	deptRoutePath        = "/dept"
	oplogRoutePath       = "/oplog"
	pageRoutePath        = "/page"
	fileRoutePath        = "/file"
	uploadRoutePath      = "/upload"
	idRoutePath          = "/:id"
	userRoutePath        = "/user"
	menuIDsRoutePath     = "/menu-ids"
	menusRoutePath       = "/menus"
	rolesRoutePath       = "/roles"
	statusRoutePath      = "/status"
	profileRoutePath     = "/profile"
	passwordRoutePath    = "/password"
	resetPasswordPath    = "/reset-password"
	batchRoutePath       = "/batch"
	batchStatusRoutePath = "/batch-status"
	ntpRoutePath         = "/ntp"
	configRoutePath      = "/config"
	syncRoutePath        = "/sync"
	setTimeRoutePath     = "/set-time"
	syncedRoutePath      = "/synced"
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
	UserHandler         *api.UserHandler
	AuthHandler         *api.AuthHandler
	FileHandler         *api.FileHandler
	NTPHandler          *api.NTPHandler
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

	// 注册 Swagger 接口文档 UI，访问路径为 /swagger/index.html。
	engine.GET(swaggerRoutePath, ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 本地文件由后端提供公开读取路径；MinIO 文件直接使用存储实现返回的公开 URL。
	if cfg.Storage.Driver == config.StorageDriverLocal && cfg.Storage.Local.Root != "" && cfg.Storage.Local.URLPrefix != "" {
		engine.StaticFS(cfg.Storage.Local.URLPrefix, http.Dir(cfg.Storage.Local.Root))
	}

	apiGroup := engine.Group(apiRoutePath)
	// 所有 API 路由默认先认证，再执行写操作权限默认拒绝；公共认证接口由中间件白名单放行。
	apiGroup.Use(deps.AuthMiddleware.Handler)
	apiGroup.Use(deps.PermMiddleware.Handler)
	{
		authGroup := apiGroup.Group(authRoutePath)
		{
			authGroup.POST(loginRoutePath, deps.AuthHandler.Login)
			authGroup.POST(refreshRoutePath, deps.AuthHandler.RefreshToken)
			authGroup.POST(logoutRoutePath, deps.AuthHandler.Logout)
			authGroup.GET(codesRoutePath, deps.AuthHandler.GetAccessCodes)
		}

		userGroup := apiGroup.Group(userRoutePath)
		{
			userGroup.GET(infoRoutePath, deps.AuthHandler.GetUserInfo)
			userGroup.GET(profileRoutePath, deps.UserHandler.GetProfile)
			userGroup.PUT(profileRoutePath, deps.UserHandler.UpdateProfile)
			userGroup.PUT(profileRoutePath+passwordRoutePath, deps.UserHandler.ChangePassword)
			userGroup.GET(pageRoutePath, deps.UserHandler.GetPage)
			userGroup.POST("", deps.UserHandler.CreateUser)
			userGroup.DELETE(batchRoutePath, deps.UserHandler.BatchDeleteUser)
			userGroup.PUT(batchStatusRoutePath, deps.UserHandler.BatchUpdateStatus)
			userGroup.PUT(idRoutePath, deps.UserHandler.UpdateUser)
			userGroup.DELETE(idRoutePath, deps.UserHandler.DeleteUser)
			userGroup.PUT(idRoutePath+resetPasswordPath, deps.UserHandler.ResetPassword)
			userGroup.GET(idRoutePath+rolesRoutePath, deps.UserHandler.GetRoleIDs)
			userGroup.PUT(idRoutePath+rolesRoutePath, deps.UserHandler.AssignRoles)
			userGroup.PUT(idRoutePath+statusRoutePath, deps.UserHandler.UpdateStatus)
		}
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+profileRoutePath, middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+profileRoutePath+passwordRoutePath, middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+userRoutePath, "system:user:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+userRoutePath+batchRoutePath, "system:user:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+batchStatusRoutePath, "system:user:status")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath, "system:user:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+userRoutePath+idRoutePath, "system:user:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+resetPasswordPath, "system:user:reset-password")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+rolesRoutePath, "system:user:assign-role")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+statusRoutePath, "system:user:status")

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
			roleGroup.DELETE(batchRoutePath, deps.RoleHandler.BatchDeleteRole)
			roleGroup.PUT(idRoutePath, deps.RoleHandler.UpdateRole)
			roleGroup.DELETE(idRoutePath, deps.RoleHandler.DeleteRole)
			roleGroup.GET(idRoutePath+menuIDsRoutePath, deps.RoleHandler.GetMenuIDs)
			roleGroup.PUT(idRoutePath+menusRoutePath, deps.RoleHandler.AssignMenus)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+roleRoutePath, "system:role:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+roleRoutePath+batchRoutePath, "system:role:delete")
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

		fileGroup := apiGroup.Group(fileRoutePath)
		{
			fileGroup.POST(uploadRoutePath, deps.FileHandler.Upload)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+fileRoutePath+uploadRoutePath, middleware.PermCodeAuthenticated)

		oplogGroup := apiGroup.Group(oplogRoutePath)
		{
			oplogGroup.GET(pageRoutePath, deps.OperationLogHandler.GetPage)
			oplogGroup.GET(idRoutePath, deps.OperationLogHandler.GetByID)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+pageRoutePath, "system:log")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+idRoutePath, "system:log")

		ntpGroup := apiGroup.Group(ntpRoutePath)
		{
			ntpGroup.GET(configRoutePath, deps.NTPHandler.GetConfig)
			ntpGroup.PUT(configRoutePath, deps.NTPHandler.UpdateConfig)
			ntpGroup.GET(statusRoutePath, deps.NTPHandler.GetStatus)
			ntpGroup.POST(syncRoutePath, deps.NTPHandler.SyncNow)
			ntpGroup.POST(setTimeRoutePath, deps.NTPHandler.SetTime)
			ntpGroup.GET(syncedRoutePath, deps.NTPHandler.IsSynced)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+configRoutePath, "ops:time:read")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+ntpRoutePath+configRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+statusRoutePath, "ops:time:read")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+ntpRoutePath+syncRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+ntpRoutePath+setTimeRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+syncedRoutePath, middleware.PermCodeAuthenticated)
	}

	return engine
}
