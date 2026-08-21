// Package migration 提供基于 golang-migrate 的 PostgreSQL 嵌入式迁移管理与版本检查。
package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"

	"niko-vue-admin/app/migrations"
)

var (
	// ErrNoChange 表示数据库已是最新版本，无需迁移。
	ErrNoChange = migrate.ErrNoChange
	// ErrNilVersion 表示数据库尚未应用任何迁移。
	ErrNilVersion = migrate.ErrNilVersion
)

// Runner 封装迁移执行与状态查询。
type Runner struct {
	db        *sql.DB
	latestVer uint
}

// New 从 GORM 连接构造迁移 Runner；使用嵌入式 migrations.FS。
func New(gdb *gorm.DB) (*Runner, error) {
	if gdb == nil {
		return nil, errors.New("gorm db is nil")
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying sql.DB: %w", err)
	}

	latest, err := calculateLatestVersion(migrations.FS)
	if err != nil {
		return nil, fmt.Errorf("calculate latest migration version: %w", err)
	}

	return &Runner{
		db:        sqlDB,
		latestVer: latest,
	}, nil
}

// LatestVersion 返回嵌入迁移文件中的最高版本号。
func (r *Runner) LatestVersion() uint {
	return r.latestVer
}

// withMigrate 创建带锁的 migrate.Migrate 实例并在操作完成后释放 source，但不关闭底层 sql.DB。
func (r *Runner) withMigrate(fn func(m *migrate.Migrate) error) error {
	srcDrv, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("init iofs migration source: %w", err)
	}
	defer func() { _ = srcDrv.Close() }()

	driver, err := migratepostgres.WithInstance(r.db, &migratepostgres.Config{
		MultiStatementEnabled: false,
	})
	if err != nil {
		return fmt.Errorf("init postgres migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", srcDrv, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}

	return fn(m)
}

// Up 执行所有未应用的迁移。如果已是最新，返回 ErrNoChange。
func (r *Runner) Up() error {
	return r.withMigrate(func(m *migrate.Migrate) error {
		return m.Up()
	})
}

// Down 回滚最近 1 个迁移版本。
func (r *Runner) Down() error {
	return r.withMigrate(func(m *migrate.Migrate) error {
		return m.Steps(-1)
	})
}

// Version 获取当前迁移版本与 dirty 状态。未初始化时返回 version=0, dirty=false, ErrNilVersion。
func (r *Runner) Version() (uint, bool, error) {
	var ver uint
	var dirty bool
	err := r.withMigrate(func(m *migrate.Migrate) error {
		v, d, vErr := m.Version()
		if vErr != nil {
			return vErr
		}
		ver = v
		dirty = d
		return nil
	})
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, ErrNilVersion
		}
		return 0, false, err
	}
	return ver, dirty, nil
}

// Force 强制设置数据库版本号，用于解决 dirty 状态或 baseline 已有数据库。
func (r *Runner) Force(version int) error {
	return r.withMigrate(func(m *migrate.Migrate) error {
		return m.Force(version)
	})
}

// CheckSchemaReady 供 API 启动时快速校验：
// 1. 数据库版本必须与嵌入的最新版本一致；
// 2. 数据库不得处于 dirty 状态。
func (r *Runner) CheckSchemaReady() error {
	v, dirty, err := r.Version()
	if err != nil {
		if errors.Is(err, ErrNilVersion) {
			return fmt.Errorf("database has no migrations applied (expected version %d); run `api migrate up` first", r.latestVer)
		}
		return fmt.Errorf("read database schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("database schema is in dirty state at version %d; resolve manually and run `api migrate force <version>`", v)
	}
	if v < r.latestVer {
		return fmt.Errorf("database schema version %d is behind expected %d; run `api migrate up` first", v, r.latestVer)
	}
	return nil
}

func closeMigrate(m *migrate.Migrate) {
	if m == nil {
		return
	}
	_, _ = m.Close()
}

func calculateLatestVersion(fsys fs.FS) (uint, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return 0, err
	}
	var maxVer uint
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		ver64, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		ver := uint(ver64)
		if ver > maxVer {
			maxVer = ver
		}
	}
	if maxVer == 0 {
		return 0, errors.New("no valid .up.sql migrations found in embedded filesystem")
	}
	return maxVer, nil
}
