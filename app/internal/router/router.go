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

// Deps 路由依赖集合：新增业务模块时扩展结构体字段，避免 New 签名随之膨胀
// （wire.Struct 按字段自动装配，见 cmd/api/wire.go）。
type Deps struct {
	ErrorHandler   gin.HandlerFunc
	AuthMiddleware *middleware.AuthMiddleware
	MenuHandler    *api.MenuHandler
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
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		response.WriteFail(c, http.StatusInternalServerError, errno.CodeInternal)
	}))
	// 统一错误处理：恢复中间件之后、业务路由之前；handler 仅 c.Error，由中间件输出响应。
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

	apiGroup := engine.Group("/api")
	{
		menuGroup := apiGroup.Group("/menu")
		menuGroup.Use(deps.AuthMiddleware.Handler)
		{
			menuGroup.GET("/tree", deps.MenuHandler.GetMenuTree)
			menuGroup.GET("/all", deps.MenuHandler.GetUserMenuTree)
			menuGroup.POST("", deps.MenuHandler.CreateMenu)
			menuGroup.PUT("/:id", deps.MenuHandler.UpdateMenu)
			menuGroup.DELETE("/:id", deps.MenuHandler.DeleteMenu)
		}
	}

	return engine
}
