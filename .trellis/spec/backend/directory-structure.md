# 目录结构

> 本项目后端代码的组织方式。

---

## 概览

后端位于 `app/`（Go 模块 `argus/app`）。它是一个由 google/wire 装配的
Gin + GORM API 服务。业务分层采用标准的 `api -> service -> repository -> model`
架构，并通过 `internal/router` 和 `internal/middleware` 统一管理路由挂载与横切逻辑。

---

## 目录布局

```
app/
├── cmd/api/
│   ├── main.go          # 启动：config → logger → db → schema check → NTP → wire → HTTP+gRPC 联合生命周期
│   ├── servers.go       # HTTP + gRPC(UDS) 联合生命周期与共享关闭窗口（bind → serve → signal/serve-error → shutdown）
│   ├── wire.go          # //go:build wireinject — DI 声明
│   └── wire_gen.go      # 由 `make wire` 生成，提交到仓库
├── configs/config.yaml  # 运行时配置（开发可由 APP_* 覆盖；生产 IPC 端点由 AIVISION_ENGINE_PROFILE 提供）
├── migrations/          # 生产版本化 SQL 迁移脚本（按 V<序号>__<描述> 目录组织）
├── scripts/
│   ├── generate-proto.sh       # 从 engine/proto/aivision/v1 权威 proto 生成 Go protobuf/gRPC 代码
│   └── test-grpc-engine-smoke.sh  # 构建 mock aivision-engine 并跑跨语言 gRPC E2E（make grpc-e2e）
├── tests/integration/   # //go:build integration — 依赖真实 C++ engine 的集成测试（仅 make grpc-e2e 运行）
├── internal/
│   ├── api/             # HTTP Handler 层（参数绑定、调用 service、返回统一 response）
│   ├── middleware/      # Gin 中间件（auth、perm、oplog、error_handler 等）
│   ├── model/           # GORM 模型定义、通用结构（BaseModel/TimeFields）、迁移与初始 Seed
│   ├── proto/aivision/v1/  # 由 scripts/generate-proto.sh 生成的 pb.go / _grpc.pb.go（提交仓库，含 descriptor 冒烟测试）
│   ├── repository/      # 数据访问层（GORM 数据库查询与事务封装）
│   ├── router/          # Gin Engine 路由与中间件装配点
│   ├── service/         # 业务逻辑编排层（密码校验、权限判定、Token 生成等）
│   └── pkg/
│       ├── config/      # viper：configs/config.yaml + APP_* 环境变量覆盖
│       ├── db/          # gorm 连接（mysql/postgres，重试 3 次/2s）
│       ├── engineipc/   # 与 C++ engine 的 gRPC-over-UDS 通信：Runtime(入站 server)、EngineClient(出站)、adapter 契约
│       ├── errno/       # 业务错误码与多语言文案的唯一来源
│       ├── logger/      # zap logger（结构化日志输出）
│       ├── mask/        # 敏感信息脱敏工具
│       └── response/    # 统一响应体 {code,data,message}
├── go.mod / go.sum
└── Makefile             # wire / dev / build / test / vet / proto / proto-check / grpc-e2e
```

---

## 模块组织

- `cmd/api` — 进程入口与 wire 装配；只有 `main` 和 wire 文件放在这里。
- `internal/api` — 接口控制层，负责 HTTP 请求反序列化、入参校验、调用 service 并使用 `response.OK` / 抛出 `errno` 错误。
- `internal/middleware` — 通用与业务中间件，包括 JWT 认证（`auth.go`）、权限码校验（`perm.go`）、操作审计日志切面（`oplog.go`）与全局错误捕获（`error_handler.go`）。
- `internal/model` — 每个表模型一个文件（`user.go`、`role.go`、…），外加 `base.go`（共享结构体）、`status.go`（状态常量与枚举）、`migrate.go`（AutoMigrate）、`seed.go`（幂等 seed）。作用于模型的纯业务函数（如 `BuildMenuTree`、`BuildDepartmentTree`）也放在这里。
- `internal/pkg/*` — 横切基础设施，一个包只负责一个关注点。包只暴露很小的公共面（`config.Load`、`db.New`、`logger.New`、`response.OK/Fail`、`errno.Message`、`mask.*`）。`engineipc` 是 Go 与 C++ engine 的 gRPC-over-UDS 通信层：`Runtime` 为入站 gRPC server（监听 `ipc.appSocket`，生产可由 `AIVISION_ENGINE_PROFILE` 的 runtime_dir + 相对 socket 名解析），`EngineClient` 为出站客户端（连接 `ipc.engineSocket`），业务通过 `DesiredStateAdapter` / `ReportAdapter` 端口接入；MVP 阶段用 `engineipc.Unavailable*` 占位实现。
- `internal/proto/aivision/v1` — 由 `scripts/generate-proto.sh` 从 `engine/proto/aivision/v1` 权威源生成，`make proto-check` 保证无漂移；生成文件提交到仓库，普通 build/test 不依赖本机 protoc。
- `tests/integration` — `//go:build integration` 的集成测试（依赖真实 C++ engine），仅由 `make grpc-e2e` 运行，不并入普通 `go test ./...`。
- `internal/repository` — 数据仓储层，封装底层 GORM 增删改查、批量操作与事务。
- `internal/service` — 业务逻辑层，处理核心规则计算、安全检查、跨仓储协作。
- `internal/router` — gin 路由注册的唯一位置；通用中间件在此用 `engine.Use(...)` 统一挂载，各业务 API 路由在此分组注册。

---

## 命名约定

- Go 文件：`snake_case.go`；Go 标识符遵循标准 Go 命名（导出 `CamelCase`、
  非导出 `camelCase`）。
- 包名：小写、简短，与目录名一致（`config`、`errno`）。
- 模型：显式 `TableName()` 返回单数 snake_case（`users`、`roles`、
  `refresh_tokens`）；列标签始终显式声明（`gorm:"column:created_at"`），
  绝不依赖 gorm 默认的 snake-case 猜测。
- 表名/列名：snake_case。JSON 字段名：camelCase（`json:"createdAt"`、
  `json:"deptId"`）。

---

## 示例

- 模型 + 表声明：`app/internal/model/user.go`
- 共享基础结构体：`app/internal/model/base.go`
- 小公共面的基础设施包：`app/internal/pkg/response/response.go`
- 接口与服务分层实现：`app/internal/api/user.go`、`app/internal/service/user.go`、`app/internal/repository/user.go`
- 启动链与依赖装配：`app/cmd/api/main.go`、`app/cmd/api/wire.go`、`app/cmd/api/servers.go`（HTTP+gRPC 联合生命周期）
- 与 C++ engine 的 UDS 通信与 socket 清理：`app/internal/pkg/engineipc/socket.go`、`runtime.go`、`client.go`
- 跨语言 E2E：`app/tests/integration/grpc_engine_e2e_test.go` + `app/scripts/test-grpc-engine-smoke.sh`
