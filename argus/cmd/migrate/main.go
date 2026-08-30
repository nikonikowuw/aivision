// Package main 提供数据库迁移命令行工具（golang-migrate PostgreSQL）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"go.uber.org/zap"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/db"
	"argus/app/internal/pkg/logger"
	"argus/app/internal/pkg/migration"
)

func usage() {
	fmt.Fprintf(os.Stderr, `argus migrate 工具

用法:
  go run ./cmd/migrate <command> [arguments]

支持命令:
  up                执行所有未应用的迁移
  down              回滚最近 1 个迁移版本
  version           显示当前数据库版本与 dirty 状态
  force <version>   强制将数据库版本标记为指定版本（用于 baseline 或修复 dirty 状态）

示例:
  go run ./cmd/migrate up
  go run ./cmd/migrate down
  go run ./cmd/migrate version
  go run ./cmd/migrate force 5
`)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: load config: %v\n", err)
		os.Exit(1)
	}

	log, err := logger.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = log.Sync() }()

	gdb, err := db.New(cfg, log)
	if err != nil {
		log.Fatal("database connection failed", zap.Error(err))
	}

	runner, err := migration.New(gdb)
	if err != nil {
		log.Fatal("initialize migration runner failed", zap.Error(err))
	}

	cmd := args[0]
	switch cmd {
	case "up":
		if err := runner.Up(); err != nil {
			if errors.Is(err, migration.ErrNoChange) {
				log.Info("database schema is already up to date (no change)", zap.Uint("latest", runner.LatestVersion()))
				return
			}
			log.Fatal("migration up failed", zap.Error(err))
		}
		ver, dirty, err := runner.Version()
		if err != nil {
			log.Fatal("get migration version after up failed", zap.Error(err))
		}
		log.Info("migration up finished", zap.Uint("version", ver), zap.Bool("dirty", dirty))

	case "down":
		if err := runner.Down(); err != nil {
			log.Fatal("migration down failed", zap.Error(err))
		}
		ver, dirty, err := runner.Version()
		if err != nil && !errors.Is(err, migration.ErrNilVersion) {
			log.Fatal("get migration version after down failed", zap.Error(err))
		}
		log.Info("migration down finished", zap.Uint("version", ver), zap.Bool("dirty", dirty))

	case "version":
		ver, dirty, err := runner.Version()
		if err != nil {
			if errors.Is(err, migration.ErrNilVersion) {
				log.Info("database has no migrations applied", zap.Uint("latestAvailable", runner.LatestVersion()))
				return
			}
			log.Fatal("get migration version failed", zap.Error(err))
		}
		log.Info("migration status",
			zap.Uint("current", ver),
			zap.Bool("dirty", dirty),
			zap.Uint("latestAvailable", runner.LatestVersion()),
		)

	case "force":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "error: force requires a version argument (e.g. `migrate force 5`)")
			os.Exit(1)
		}
		targetVer, err := strconv.Atoi(args[1])
		if err != nil || targetVer < 0 {
			fmt.Fprintf(os.Stderr, "error: invalid target version %q: must be non-negative integer\n", args[1])
			os.Exit(1)
		}
		if err := runner.Force(targetVer); err != nil {
			log.Fatal("migration force failed", zap.Int("version", targetVer), zap.Error(err))
		}
		log.Warn("migration version forced", zap.Int("version", targetVer))

	default:
		usage()
		os.Exit(1)
	}
}
