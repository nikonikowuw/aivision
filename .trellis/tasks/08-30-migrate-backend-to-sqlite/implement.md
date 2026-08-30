# Implementation Checklist — 将 Go 后端数据库全面迁移为纯 SQLite

- [x] **Step 1: 配置与依赖更新**
  - [x] 调整 `app/configs/config.yaml`（`db.path`, `db.busy_timeout` 等）。
  - [x] 调整 `app/internal/pkg/config/config.go`，更新 `DB` 结构体字段及默认值、环境变量解析。
  - [x] 更新 `app/internal/pkg/config/config_test.go`。
- [x] **Step 2: 数据库连接层重构**
  - [x] 重构 `app/internal/pkg/db/db.go` 为纯 SQLite 驱动连接（自动创建目录、WAL 模式、忙超时、连接池）。
  - [x] 更新 `app/internal/pkg/db/db_test.go`。
- [x] **Step 3: Repository 原生 SQL 兼容改造**
  - [x] 修改 `app/internal/repository/task.go` 中的 `GetTaskStats`，将 `COUNT(*) FILTER` 改为 `SUM(CASE WHEN ...)`。
  - [x] 验证 `task_repository_test.go`。
- [x] **Step 4: 启动链与迁移改造**
  - [x] 清理/重构 `app/internal/pkg/migration/`，统一为基于 GORM `AutoMigrate` + `model.Seed` 的初始化机制。
  - [x] 调整 `app/cmd/api/main.go`、`app/cmd/bootstrap/main.go`、`app/cmd/migrate/main.go`。
- [x] **Step 5: 依赖清理与全量回归测试**
  - [x] 从 `app/go.mod` 移除 `gorm.io/driver/postgres` 及 `golang-migrate` PG 驱动依赖，执行 `go mod tidy`。
  - [x] 运行 `go test ./...` 确保所有单元测试通过。
  - [x] 运行 `make build` 确保所有命令行与服务二进制编译通过。

