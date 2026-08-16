// Package db 负责建立数据库连接（mysql / postgres 二选一，带重试，容器场景未就绪常见）。
package db

import (
	"fmt"
	"net/url"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/pkg/config"
)

const (
	connectRetries = 3
	retryInterval  = 2 * time.Second
)

// New 按 db.driver 连接 mysql/postgres：失败重试 3 次（间隔 2s），仍失败返回错误。
// 连接池参数直接取 cfg.DB（config.Load 已注入非零默认值，见 config.defaults）。
func New(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DB.Driver {
	case "postgres":
		dsn, err := postgresDSN(cfg.DB)
		if err != nil {
			return nil, fmt.Errorf("build postgres DSN: %w", err)
		}
		dialector = gormpostgres.Open(dsn)
	default:
		dsn, err := mysqlDSN(cfg.DB)
		if err != nil {
			return nil, fmt.Errorf("build mysql DSN: %w", err)
		}
		dialector = gormmysql.Open(dsn)
	}

	var err error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		// TranslateError 开启驱动错误翻译（如 1062/23505/2067 → gorm.ErrDuplicatedKey），
		// 供 repository 层统一映射为领域哨兵错误（不依赖具体驱动错误文案）。
		gdb, openErr := gorm.Open(dialector, &gorm.Config{TranslateError: true})
		if openErr == nil {
			sqlDB, dbErr := gdb.DB()
			if dbErr != nil {
				err = dbErr
			} else if pingErr := sqlDB.Ping(); pingErr != nil {
				err = pingErr
			} else {
				sqlDB.SetMaxOpenConns(cfg.DB.MaxOpen)
				sqlDB.SetMaxIdleConns(cfg.DB.MaxIdle)
				sqlDB.SetConnMaxLifetime(cfg.DB.MaxLifetime)
				return gdb, nil
			}
		} else {
			err = openErr
		}
		log.Warn("database connect failed, retrying",
			zap.String("driver", cfg.DB.Driver), zap.Int("attempt", attempt),
			zap.String("host", cfg.DB.Host), zap.Error(err))
		if attempt < connectRetries {
			time.Sleep(retryInterval)
		}
	}
	return nil, fmt.Errorf("connect %s after %d retries: %w", cfg.DB.Driver, connectRetries, err)
}

// mysqlDSN 构造 MySQL DSN。用驱动的 Config.FormatDSN 而非手拼字符串，
// 由驱动负责密码等特殊字符的转义。
func mysqlDSN(d config.DB) (string, error) {
	location, err := time.LoadLocation(d.TimeZone)
	if err != nil {
		return "", fmt.Errorf("load time zone %q: %w", d.TimeZone, err)
	}
	c := gomysql.Config{
		User:      d.User,
		Passwd:    d.Password,
		Net:       "tcp",
		Addr:      fmt.Sprintf("%s:%d", d.Host, d.Port),
		DBName:    d.Name,
		ParseTime: true,
		Loc:       location,
		Params:    map[string]string{"charset": "utf8mb4"},
	}
	return c.FormatDSN(), nil
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
