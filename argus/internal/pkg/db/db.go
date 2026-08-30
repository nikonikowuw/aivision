// Package db 负责建立 PostgreSQL 数据库连接（带重试，容器场景未就绪常见）。
package db

import (
	"fmt"
	"net/url"
	"time"

	"go.uber.org/zap"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"argus/app/internal/pkg/config"
)

const (
	connectRetries = 3
	retryInterval  = 2 * time.Second
)

// New 按 PostgreSQL 配置建立数据库连接：失败重试 3 次（间隔 2s）。
// 连接池参数直接取 cfg.DB（config.Load 已注入非零默认值，见 config.defaults）。
func New(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	dsn, err := postgresDSN(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("build postgres DSN: %w", err)
	}
	dialector := gormpostgres.Open(dsn)

	var lastErr error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		// TranslateError 开启驱动错误翻译（如 23505 → gorm.ErrDuplicatedKey），
		// 供 repository 层统一映射为领域哨兵错误。
		gdb, openErr := gorm.Open(dialector, &gorm.Config{TranslateError: true})
		if openErr == nil {
			sqlDB, dbErr := gdb.DB()
			if dbErr == nil {
				if pingErr := sqlDB.Ping(); pingErr == nil {
					sqlDB.SetMaxOpenConns(cfg.DB.MaxOpen)
					sqlDB.SetMaxIdleConns(cfg.DB.MaxIdle)
					sqlDB.SetConnMaxLifetime(cfg.DB.MaxLifetime)
					return gdb, nil
				} else {
					lastErr = pingErr
				}
			} else {
				lastErr = dbErr
			}
		} else {
			lastErr = openErr
		}
		log.Warn("database connect failed, retrying",
			zap.Int("attempt", attempt), zap.String("host", cfg.DB.Host), zap.Error(lastErr))
		if attempt < connectRetries {
			time.Sleep(retryInterval)
		}
	}
	return nil, fmt.Errorf("connect postgres after %d retries: %w", connectRetries, lastErr)
}

// postgresDSN 构造 PostgreSQL DSN，并转义 user/password，避免特殊字符破坏解析。
// TimeZone 保持未编码的斜杠，是因为 GORM 会直接从原始 DSN 提取该值并调用
// time.LoadLocation；编码为 %2F 会被 PostgreSQL 当作字面量时区名。
func postgresDSN(d config.DB) (string, error) {
	if _, err := time.LoadLocation(d.TimeZone); err != nil {
		return "", fmt.Errorf("load time zone %q: %w", d.TimeZone, err)
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   "/" + d.Name,
	}
	u.RawQuery = "sslmode=disable&TimeZone=" + d.TimeZone
	return u.String(), nil
}
