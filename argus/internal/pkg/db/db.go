// Package db 负责建立 SQLite 嵌入式数据库连接（WAL 模式与忙等待控制）。
package db

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/pkg/config"
)

// New 按 SQLite 配置建立数据库连接并配置连接池与 WAL 模式。
// 自动创建数据库父级目录（如 data/）。
func New(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	dsn, err := sqliteDSN(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("build sqlite DSN: %w", err)
	}

	// 确保数据库文件所在的目录存在
	dbDir := filepath.Dir(cfg.DB.Path)
	if dbDir != "" && dbDir != "." {
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory %s: %w", dbDir, err)
		}
	}

	dialector := sqlite.Open(dsn)
	// TranslateError 开启驱动错误翻译（如 UNIQUE 约束冲突 → gorm.ErrDuplicatedKey），
	// 供 repository 层统一映射为领域哨兵错误。
	// DisableForeignKeyConstraintWhenMigrating 遵循项目规范：逻辑关联、不建数据库物理外键约束。
	gdb, err := gorm.Open(dialector, &gorm.Config{
		TranslateError:                           true,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpen)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdle)
	sqlDB.SetConnMaxLifetime(cfg.DB.MaxLifetime)

	log.Info("sqlite database connected",
		zap.String("path", cfg.DB.Path),
		zap.Int("busy_timeout", cfg.DB.BusyTimeout),
		zap.Int("max_open", cfg.DB.MaxOpen),
	)

	return gdb, nil
}

// sqliteDSN 构造 SQLite DSN，配置 WAL 模式、忙超时、shared cache 及外键支持。
func sqliteDSN(d config.DB) (string, error) {
	busyTimeout := d.BusyTimeout
	if busyTimeout <= 0 {
		busyTimeout = 5000
	}

	q := url.Values{}
	q.Set("_journal_mode", "WAL")
	q.Set("_busy_timeout", strconv.Itoa(busyTimeout))
	q.Set("_synchronous", "NORMAL")
	q.Set("_cache", "shared")
	q.Set("_foreign_keys", "ON")

	return fmt.Sprintf("file:%s?%s", d.Path, q.Encode()), nil
}
