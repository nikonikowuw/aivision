// Package main 提供初始管理员初始化工具（从环境变量或参数接收密码，不硬编码）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/db"
	"niko-vue-admin/app/internal/pkg/logger"
	"niko-vue-admin/app/internal/pkg/migration"
)

func usage() {
	fmt.Fprintf(os.Stderr, `niko-vue-admin bootstrap 工具

用途:
  为系统创建初始超级管理员，并将密码安全哈希后写入数据库。

用法:
  APP_BOOTSTRAP_ADMIN_PASSWORD="<password>" go run ./cmd/bootstrap [flags]

选项:
  -username  管理员用户名 (默认: "admin")
  -nickname  管理员昵称 (默认: "超级管理员")
  -email     管理员邮箱 (默认: "admin@example.com")
  -dept-name 初始关联部门名称 (默认: "演示部门")

说明:
  1. 初始密码必须通过环境变量 APP_BOOTSTRAP_ADMIN_PASSWORD 传入，禁止在命令行参数明文出现。
  2. 若用户已存在，命令会直接报错退出，绝不覆盖已有密码。
`)
}

func main() {
	username := flag.String("username", model.AdminUsername, "管理员用户名")
	nickname := flag.String("nickname", "超级管理员", "管理员昵称")
	email := flag.String("email", "admin@example.com", "管理员邮箱")
	deptName := flag.String("dept-name", "演示部门", "初始关联部门名称")

	flag.Usage = usage
	flag.Parse()

	password := strings.TrimSpace(os.Getenv("APP_BOOTSTRAP_ADMIN_PASSWORD"))
	if password == "" {
		fmt.Fprintln(os.Stderr, "error: APP_BOOTSTRAP_ADMIN_PASSWORD environment variable is required")
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

	// 确认数据库迁移已准备就绪
	runner, err := migration.New(gdb)
	if err != nil {
		log.Fatal("initialize migration runner failed", zap.Error(err))
	}
	if err := runner.CheckSchemaReady(); err != nil {
		log.Fatal("database schema check failed before bootstrap", zap.Error(err))
	}

	err = gdb.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Where("username = ?", *username).Count(&count).Error; err != nil {
			return fmt.Errorf("check user existence: %w", err)
		}
		if count > 0 {
			return fmt.Errorf("user %q already exists; bootstrap aborted", *username)
		}

		var superRole model.Role
		if err := tx.Where("code = ?", model.RoleSuperCode).First(&superRole).Error; err != nil {
			return fmt.Errorf("find super role (%s): %w", model.RoleSuperCode, err)
		}

		var dept model.Department
		if err := tx.Where("name = ?", *deptName).First(&dept).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find department (%s): %w", *deptName, err)
			}
			// 若没有指定部门，挂在根部门
			dept.ID = 0
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}

		adminUser := model.User{
			Username: *username,
			Password: string(hash),
			Nickname: *nickname,
			Email:    *email,
			DeptID:   dept.ID,
			Status:   model.StatusEnabled,
		}
		if err := tx.Create(&adminUser).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		userRole := model.UserRole{
			UserID: adminUser.ID,
			RoleID: superRole.ID,
		}
		if err := tx.Create(&userRole).Error; err != nil {
			return fmt.Errorf("bind user role: %w", err)
		}

		return nil
	})

	if err != nil {
		log.Fatal("bootstrap admin failed", zap.Error(err))
	}

	log.Info("bootstrap admin succeeded",
		zap.String("username", *username),
		zap.String("nickname", *nickname),
		zap.String("email", *email),
	)
}
