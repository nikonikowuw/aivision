// @title niko-vue-admin API
// @version 1.0
// @description niko-vue-admin 后端 API 接口文档
// @host localhost:8000
// @BasePath /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description 在 Header 中传入 `Bearer <token>` 进行身份认证
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/migration"
	"niko-vue-admin/app/internal/service"
)

// App 是 wire 装配产物：main 启动所需的全部依赖。
type App struct {
	DB         *gorm.DB
	Logger     *zap.Logger
	Engine     *gin.Engine
	NTPService service.NTPService
}

const (
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: load config: %v\n", err)
		os.Exit(1)
	}

	app, err := InitializeApp(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: initialize app: %v\n", err)
		os.Exit(1)
	}
	log := app.Logger
	defer func() { _ = log.Sync() }()

	// 生产安全提醒：仍在用开发默认 JWT 密钥
	if cfg.UsingDefaultJWTSecret() {
		log.Warn("仍在使用默认 JWT 密钥（dev-secret-change-me），生产环境请通过 APP_JWT_SECRET 覆盖")
	}

	// 数据库 schema 与数据迁移状态检查：生产/运行期不再自动建表或 seed 数据
	migRunner, err := migration.New(app.DB)
	if err != nil {
		log.Fatal("initialize migration runner failed", zap.Error(err))
	}
	if err := migRunner.CheckSchemaReady(); err != nil {
		log.Fatal("database schema check failed; please run `make migrate-up` or `go run ./cmd/migrate up` first", zap.Error(err))
	}
	log.Info("database schema ready", zap.Uint("version", migRunner.LatestVersion()))

	// 开机重放对时配置（从 DB 恢复并应用到底层系统）
	if app.NTPService != nil {
		if err := app.NTPService.ReplayOnBoot(context.Background()); err != nil {
			log.Warn("failed to replay ntp config on boot", zap.Error(err))
		}
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           app.Engine,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server failed", zap.Error(err))
		}
	}()
	log.Info("server listening", zap.Int("port", cfg.Server.Port))

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down (10s timeout)")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", zap.Error(err))
	}
	log.Info("server exited")
}
