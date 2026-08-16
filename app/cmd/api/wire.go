//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"

	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/db"
	"niko-vue-admin/app/internal/pkg/logger"
	"niko-vue-admin/app/internal/router"
)

// InitializeApp 装配启动依赖链：config → db → logger → engine。
func InitializeApp(cfg *config.Config) (*App, error) {
	wire.Build(
		db.New,
		logger.New,
		router.New,
		wire.Struct(new(App), "*"),
	)
	return nil, nil
}
