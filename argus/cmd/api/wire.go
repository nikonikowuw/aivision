//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/db"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/logger"
	"argus/app/internal/pkg/ntp"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/repository"
	"argus/app/internal/router"
	"argus/app/internal/service"
)

// InitializeApp 装配启动依赖链：config → db → logger → engine。
func InitializeApp(cfg *config.Config) (*App, error) {
	wire.Build(
		db.New,
		logger.New,
		storage.New,
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
		service.NewFileService,
		api.NewFileHandler,
		ntp.NewExecutor,
		repository.NewSystemConfigRepository,
		service.NewNTPService,
		api.NewNTPHandler,
		service.NewNetworkService,
		api.NewNetworkHandler,
		repository.NewCameraRepository,
		service.NewCameraService,
		api.NewCameraHandler,
		repository.NewPersonRepository,
		service.NewPersonService,
		api.NewPersonHandler,
		repository.NewAlgorithmRepository,
		service.NewAlgorithmService,
		api.NewAlgorithmHandler,
		repository.NewTaskRepository,
		service.NewTaskService,
		service.NewDesiredStateAdapter,
		repository.NewAlarmRecordRepository,
		service.NewAlarmRecordService,
		api.NewAlarmRecordHandler,
		repository.NewPlateObservationRepository,
		service.NewPlateObservationService,
		api.NewPlateObservationHandler,
		service.NewReportAdapterWithAlarm,
		api.NewTaskHandler,
		middleware.NewOpenPersonIPWhitelistMiddleware,
		router.New,
		wire.Struct(new(router.Deps), "*"),
		// gRPC over UDS：Go 侧业务实现 DesiredState/Report 适配器，Engine 经 app.sock
		// 拉取期望状态、上报运行状态；配额上限经 engine.sock 的 QueryProfile 获取。
		engineipc.NewRuntime,
		engineipc.NewEngineClient,
		wire.Bind(new(ipcRuntime), new(*engineipc.Runtime)),
		wire.Bind(new(service.CameraProbeClient), new(*engineipc.EngineClient)),
		wire.Bind(new(service.ProfileClient), new(*engineipc.EngineClient)),
		wire.Bind(new(engineipc.ReportAdapter), new(*service.ReportAdapter)),
		wire.Struct(new(App), "*"),
	)
	return nil, nil
}
