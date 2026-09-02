// Package migration 提供基于 GORM AutoMigrate 与模型 Seed 的 SQLite 嵌入式迁移与初始化管理。
package migration

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// Runner 封装表结构迁移与种子数据初始化。
type Runner struct {
	db *gorm.DB
}

// New 从 GORM 连接构造迁移 Runner。
func New(gdb *gorm.DB) (*Runner, error) {
	if gdb == nil {
		return nil, errors.New("gorm db is nil")
	}
	return &Runner{db: gdb}, nil
}

// AutoMigrate 执行所有表的自动迁移并播种初始种子数据。
func (r *Runner) AutoMigrate() error {
	if err := model.AutoMigrate(r.db); err != nil {
		return fmt.Errorf("automigrate tables: %w", err)
	}
	if _, err := model.Seed(r.db); err != nil {
		return fmt.Errorf("seed initial data: %w", err)
	}
	return nil
}

// CheckSchemaReady 供 API 启动时快速校验或初始化数据库结构与初始种子数据。
func (r *Runner) CheckSchemaReady() error {
	return r.AutoMigrate()
}
