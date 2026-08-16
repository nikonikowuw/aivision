# 目录结构

> 本项目后端代码的组织方式。

---

## 概览

后端位于 `app/`（Go 模块 `niko-vue-admin/app`）。它是一个由 google/wire 装配的
Gin + GORM API 服务。业务路由尚未实现——骨架阶段已确立模型层、
config/logger/response/errno/db 各包以及 gin engine 装配点。功能子任务
（user/role/menu/dept/auth/oplog）将在首次需要时（YAGNI）创建
`internal/` 下的 `repository/`、`service/`、`api/`、`middleware/` 目录。

---

## 目录布局

```
app/
├── cmd/api/
│   ├── main.go          # 启动：config → logger → db → AutoMigrate → seed → wire → serve
│   ├── wire.go          # //go:build wireinject — DI 声明
│   └── wire_gen.go      # 由 `make wire` 生成，提交到仓库
├── configs/config.yaml  # 运行时配置（可由 APP_* 环境变量覆盖）
├── internal/
│   ├── model/           # 8 张表的 gorm 模型 + 纯函数（menu tree）+ seed
│   ├── pkg/
│   │   ├── config/      # viper：configs/config.yaml + APP_* 环境变量覆盖
│   │   ├── db/          # gorm 连接（mysql/postgres，重试 3 次/2s）
│   │   ├── errno/       # 业务错误码的唯一来源
│   │   ├── logger/      # zap logger（级别来自配置）
│   │   └── response/    # 统一响应体 {code,data,message}
│   └── router/          # gin engine 装配点（当前为空壳）
├── go.mod / go.sum
└── Makefile             # wire / dev / build / test / vet
```

---

## 模块组织

- `cmd/api` — 进程入口与 wire 装配；只有 `main` 和 wire 文件放在这里。
- `internal/model` — 每个表模型一个文件（`user.go`、`role.go`、…），外加
  `base.go`（共享结构体）、`migrate.go`（AutoMigrate）、`seed.go`（幂等 seed）。
  作用于模型的纯业务函数（如 `BuildMenuTree`）也放在这里。
- `internal/pkg/*` — 横切基础设施，一个包只负责一个关注点。包只暴露很小的
  公共面（`config.Load`、`db.New`、`logger.New`、`response.OK/Fail`、`errno.Message`）。
- `internal/router` — gin 路由注册的唯一位置；通用中间件（错误处理、恢复、
  日志等）在此用 `engine.Use(...)` 统一挂载，中间件实现放 `internal/middleware/`。
- 功能层（`repository/service/api/middleware`）由需要它们的功能子任务按需添加；
  不要预先创建空目录。

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
- 启动链：`app/cmd/api/main.go`
