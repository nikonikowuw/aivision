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

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
)

// App 是 wire 装配产物：main 启动所需的全部依赖。
type App struct {
	DB     *gorm.DB
	Logger *zap.Logger
	Engine *gin.Engine
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

	// AutoMigrate：8 张表（仅 dev/test；生产 db.auto_migrate=false 时结构变更走 app/migrations SQL 脚本）
	if cfg.DB.AutoMigrate {
		if err := model.AutoMigrate(app.DB); err != nil {
			log.Fatal("AutoMigrate failed", zap.Error(err))
		}
		log.Info("AutoMigrate 完成")
	} else {
		log.Info("AutoMigrate 已禁用（db.auto_migrate=false），表结构变更走 app/migrations 版本化 SQL 脚本")
	}

	// seed：幂等（admin 存在则跳过）
	seeded, err := model.Seed(app.DB)
	if err != nil {
		log.Fatal("seed failed", zap.Error(err))
	}
	if seeded {
		log.Info("seed 完成")
		log.Warn("已创建默认管理员 admin（默认密码 admin123），生产环境请立即修改密码")
	} else {
		log.Info("seed 跳过（admin 已存在，不覆盖）")
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
