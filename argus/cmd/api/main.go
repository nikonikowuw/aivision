// @title argus API
// @version 1.0
// @description argus 后端 API 接口文档
// @host localhost:8000
// @BasePath /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description 在 Header 中传入 `Bearer <token>` 进行身份认证
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/migration"
	"argus/app/internal/service"
)

// App 是 wire 装配产物：main 启动所需的全部依赖。
type App struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	Engine         *gin.Engine
	NTPService     service.NTPService
	Network        service.NetworkService
	IPCRuntime     ipcRuntime
	EngineClient   *engineipc.EngineClient
	TaskService    service.TaskService
	StorageService service.StorageCleanupService
}

// ipcRuntime 是 gRPC UDS 入站 runtime 的窄接口（serverLifecycle 只依赖这三个方法，
// 便于测试注入替身触发 serve error 路径）。
type ipcRuntime interface {
	Start(socketPath string) error
	Errors() <-chan error
	Shutdown(ctx context.Context) error
}

// CleanupStartupResources 回收初始化阶段已装配的 IPC、EngineClient 与 Network 依赖。
// main 在进入 run 前如果迁移或启动准备失败，通过此方法统一释放资源。
func (a *App) CleanupStartupResources(timeout time.Duration) {
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if a.IPCRuntime != nil {
		if err := a.IPCRuntime.Shutdown(ctx); err != nil && a.Logger != nil {
			a.Logger.Error("grpc ipc startup cleanup failed", zap.Error(err))
		}
	}
	if a.EngineClient != nil {
		if err := a.EngineClient.Close(); err != nil && a.Logger != nil {
			a.Logger.Error("engine client close failed", zap.Error(err))
		}
	}
	if a.Network != nil {
		if err := a.Network.Close(ctx); err != nil && a.Logger != nil {
			a.Logger.Error("network service close failed", zap.Error(err))
		}
	}
	if a.StorageService != nil {
		a.StorageService.Stop()
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: load config: %v\n", err)
		os.Exit(1)
	}

	if err := requireRoot(os.Geteuid(), cfg.Network.FakePlatform); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: root check failed: %v\n", err)
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

	// 数据库 schema 自动迁移与种子数据就绪检查
	migRunner, err := migration.New(app.DB)
	if err != nil {
		log.Error("initialize migration runner failed", zap.Error(err))
		app.CleanupStartupResources(shutdownTimeout)
		os.Exit(1)
	}
	if err := migRunner.CheckSchemaReady(); err != nil {
		log.Error("database schema and seed check failed", zap.Error(err))
		app.CleanupStartupResources(shutdownTimeout)
		os.Exit(1)
	}
	log.Info("database schema and seed ready")

	// 开机重放对时配置（从 DB 恢复并应用到底层系统）
	if app.NTPService != nil {
		if err := app.NTPService.ReplayOnBoot(context.Background()); err != nil {
			log.Warn("failed to replay ntp config on boot", zap.Error(err))
		}
	}

	// 启动网络配置服务（首次接管基线、未决事务启动恢复）
	if app.Network != nil {
		if err := app.Network.Start(context.Background()); err != nil {
			log.Error("network service start failed", zap.Error(err))
			app.CleanupStartupResources(shutdownTimeout)
			os.Exit(1)
		}
	}

	// 启动存储自动清理与防爆盘守护任务
	if app.StorageService != nil {
		app.StorageService.Start(context.Background())
	}

	// HTTP + gRPC 联合生命周期：预绑定 HTTP TCP，再绑定 app.sock，统一等待
	// SIGINT/SIGTERM 或任一 server 的 serve error，并在同一超时窗口内优雅停止。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	lc := &serverLifecycle{cfg: cfg, app: app, quit: quit, timeout: shutdownTimeout}
	if err := lc.run(); err != nil {
		log.Fatal("server lifecycle failed", zap.Error(err))
	}
	log.Info("server exited")
}
