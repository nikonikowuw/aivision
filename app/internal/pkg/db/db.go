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

	// 连接池：MySQL wait_timeout（默认 8h）会掐断空闲连接，
	// ConnMaxLifetime 须小于该值，让连接定期刷新避免 stale connection。
	maxOpenConns    = 100
	maxIdleConns    = 10
	connMaxLifetime = 30 * time.Minute
)

// New 按 db.driver 连接 mysql/postgres：失败重试 3 次（间隔 2s），仍失败返回错误。
func New(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DB.Driver {
	case "postgres":
		dialector = gormpostgres.Open(postgresDSN(cfg.DB))
	default:
		dialector = gormmysql.Open(mysqlDSN(cfg.DB))
	}

	var err error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		gdb, openErr := gorm.Open(dialector, &gorm.Config{})
		if openErr == nil {
			sqlDB, dbErr := gdb.DB()
			if dbErr != nil {
				err = dbErr
			} else if pingErr := sqlDB.Ping(); pingErr != nil {
				err = pingErr
			} else {
				sqlDB.SetMaxOpenConns(maxOpenConns)
				sqlDB.SetMaxIdleConns(maxIdleConns)
				sqlDB.SetConnMaxLifetime(connMaxLifetime)
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

// mysqlDSN 构造 MySQL DSN。用驱动的 Config.FormatDSN 而非手拼字符串：
// 由驱动负责 dbname 等的转义，避免 user/password 中的特殊字符破坏 DSN 解析。
// loc 固定 Asia/Shanghai（格式化为 loc=Asia%2FShanghai），与 postgresDSN 的
// TimeZone 保持一致，避免隐式依赖服务器本地时区。
func mysqlDSN(d config.DB) string {
	c := gomysql.Config{
		User:      d.User,
		Passwd:    d.Password,
		Net:       "tcp",
		Addr:      fmt.Sprintf("%s:%d", d.Host, d.Port),
		DBName:    d.Name,
		ParseTime: true,
		Loc:       time.FixedZone("Asia/Shanghai", 8*3600),
		Params:    map[string]string{"charset": "utf8mb4"},
	}
	return c.FormatDSN()
}

// postgresDSN 构造 PostgreSQL DSN（决策 18）。用 URL 形式并转义 user/password，
// 避免特殊字符破坏 DSN 解析。
func postgresDSN(d config.DB) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   "/" + d.Name,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("TimeZone", "Asia/Shanghai")
	u.RawQuery = q.Encode()
	return u.String()
}
