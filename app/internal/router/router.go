// Package router 装配 gin engine；本任务为空壳，后续子任务在此注册业务路由。
package router

import (
	"strings"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/config"
)

// New 创建 gin engine（空壳：仅 recovery，不注册业务路由）。
func New(cfg *config.Config) *gin.Engine {
	// 与 logger.New 的 zap 解析一致，级别比较大小写不敏感。
	if strings.EqualFold(cfg.Log.Level, "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	engine.Use(gin.Recovery())
	// 不信任任何代理：ClientIP 直接用 RemoteAddr，避免伪造 X-Forwarded-For。
	// 生产若置于反向代理后，需在此显式配置代理网段。
	_ = engine.SetTrustedProxies(nil)
	return engine
}
