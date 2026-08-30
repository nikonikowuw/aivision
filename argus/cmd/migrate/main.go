// Package main 提供数据库迁移与初始化命令行工具（SQLite AutoMigrate 与 Seed）。
package main

import (
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/db"
	"argus/app/internal/pkg/logger"
	"argus/app/internal/pkg/migration"
)

func usage() {
	fmt.Fprintf(os.Stderr, `argus migrate 工具

用法:
  go run ./cmd/migrate <command>

支持命令:
  up        执行 SQLite 表结构自动迁移并播种初始种子数据
  version   检查数据库表结构与种子数据就绪状态

示例:
  go run ./cmd/migrate up
  go run ./cmd/migrate version
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
		if err := runner.AutoMigrate(); err != nil {
			log.Fatal("migration and seed up failed", zap.Error(err))
		}
		log.Info("migration and seed up finished successfully")

	case "version":
		if err := runner.CheckSchemaReady(); err != nil {
			log.Fatal("database check failed", zap.Error(err))
		}
		log.Info("database schema and seed are ready")

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}
