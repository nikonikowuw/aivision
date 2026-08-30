# PRD — 将 Go 后端数据库全面迁移为纯 SQLite

## 目标与价值

将 `aivision` 系统后端数据库由外部 PostgreSQL 彻底迁移为嵌入式纯 SQLite 架构（默认开启 WAL 模式与并发忙等待超时）。

- **业务与产品价值**：
  - 适配边缘 AI 视频分析一体机/边缘盒子的单机产品形态，免去外部 PG 数据库容器与守护进程的部署与内存开销（节省约 100~300MB 常驻内存）。
  - 实现开箱即用，设备重启秒级启动，数据库备份、故障排查与出厂重置仅需管理单个 `.db` 文件。
- **架构一致性价值**：
  - 彻底消除此前“单元测试跑 SQLite 内存库，生产环境跑 PostgreSQL”导致的行为与方言割裂。

## 现状与确认事实

1. **现有数据模型与测试**：
   - 数据模型均基于 GORM 定义（[internal/model/migrate.go](/Users/niko/dev/go/aivision/app/internal/model/migrate.go:5) 已包含 `AutoMigrate` 覆盖全部表）。
   - 绝大多数 Repository 与 Service 单元测试（如 `task_service_test.go`、`algorithm_test.go`、`user_test.go` 等）已基于 SQLite 内存库验证通过。
2. **需要清理/重构的模块**：
   - [internal/pkg/db/db.go](/Users/niko/dev/go/aivision/app/internal/pkg/db/db.go:1)：从 `postgresDSN` 重构为 SQLite 连接初始化（支持目录自动创建、WAL 模式、忙等待、`cache=shared`）。
   - [internal/pkg/config/config.go](/Users/niko/dev/go/aivision/app/internal/pkg/config/config.go:1)：配置项精简，`DBConfig` 由 host/port/user/password 改为 `Path`、`BusyTimeout`、`MaxOpen` 等字段。
   - [internal/repository/task.go](/Users/niko/dev/go/aivision/app/internal/repository/task.go:240)：`GetTaskStats` 中的 PG 方言 `COUNT(*) FILTER (...)` 统一改写为跨数据库通用的 `SUM(CASE WHEN ... THEN 1 ELSE 0 END)`。
   - [internal/pkg/migration](/Users/niko/dev/go/aivision/app/internal/pkg/migration/migration.go:1) & [cmd/migrate](/Users/niko/dev/go/aivision/app/cmd/migrate/main.go:1)：原 PG SQL 迁移驱动升级/收拢为基于 GORM `AutoMigrate` 与内置 Seed 初始化的轻量模式。
   - [cmd/api/main.go](/Users/niko/dev/go/aivision/app/cmd/api/main.go:1) & [cmd/bootstrap/main.go](/Users/niko/dev/go/aivision/app/cmd/bootstrap/main.go:1)：启动链中的 Schema 校验和初始化适配 SQLite。

## 需求范围 (Scope)

### In Scope
1. **配置层**：
   - 调整 `configs/config.yaml` 及 `config.go`，支持 `db.path`（默认 `data/argus.db`）。
2. **数据层连接**：
   - 重构 `internal/pkg/db` 为纯 SQLite 驱动，配置 WAL 日志模式（`_journal_mode=WAL`）、忙等待超时（`_busy_timeout=5000`）及连接池参数。
3. **方言兼容与修复**：
   - 修复 `task.go` 中的 `COUNT(*) FILTER` 等 PG 方言写法。
4. **迁移与初始化收拢**：
   - 统一数据库初始化入口为 GORM `AutoMigrate` + `model.SeedSystemRBAC` / `model.SeedSystemConfigs` / 业务 Seed。
   - 调整 `cmd/api`、`cmd/migrate`、`cmd/bootstrap` 启动与初始化行为。
5. **构建与测试验证**：
   - 清理不需要的 PostgreSQL 依赖，`go test ./...` 全量回归通过。
   - `make build` 编译成功。

### Out of Scope
- 保留 PostgreSQL 双驱动（明确采用纯 SQLite 单一驱动架构）。
- 修改 C++ 引擎与前端 Vue UI 业务逻辑。

## 验收标准 (Acceptance Criteria)

1. **AC-1 (配置与启动)**：后端服务在默认配置下启动时，自动在 `data/` 目录下创建并初始化 `argus.db`，表结构与初始种子数据（超级管理员角色、内置菜单、默认系统配置）自动就绪。
2. **AC-2 (并发与 WAL 模式)**：SQLite 连接启用 WAL 模式与 `_busy_timeout`，在并发读写场景下稳定不发生 `database is locked` 错误。
3. **AC-3 (SQL 与数据查询)**：`GetTaskStats`、`BumpRevisionTx` 等复杂查询与事务在 SQLite 下准确执行，返回结果正确。
4. **AC-4 (Bootstrap 工具)**：`APP_BOOTSTRAP_ADMIN_PASSWORD="xxx" go run ./cmd/bootstrap` 能够正常在 SQLite 库中创建初始管理员。
5. **AC-5 (单测与编译)**：执行 `go test ./...` 全部通过，无 PG 残留编译错误。
