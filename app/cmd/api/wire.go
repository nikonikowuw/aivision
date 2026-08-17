//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/db"
	"niko-vue-admin/app/internal/pkg/logger"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/router"
	"niko-vue-admin/app/internal/service"
)

// InitializeApp 装配启动依赖链：config → db → logger → engine。
func InitializeApp(cfg *config.Config) (*App, error) {
	wire.Build(
		db.New,
		logger.New,
		middleware.ErrorHandler,
		repository.NewAuthRepository,
		middleware.NewAuthMiddleware,
		middleware.NewPermMiddleware,
		repository.NewOperationLogRepository,
		service.NewOperationLogService,
		middleware.NewOplogMiddleware,
		api.NewOperationLogHandler,
		repository.NewMenuRepository,
		service.NewMenuService,
		api.NewMenuHandler,
		repository.NewRoleRepository,
		service.NewRoleService,
		api.NewRoleHandler,
		repository.NewDepartmentRepository,
		service.NewDeptService,
		api.NewDepartmentHandler,
		repository.NewUserRepository,
		service.NewUserService,
		api.NewUserHandler,
		service.NewAuthService,
		api.NewAuthHandler,
		router.New,
		wire.Struct(new(router.Deps), "*"),
		wire.Struct(new(App), "*"),
	)
	return nil, nil
}
