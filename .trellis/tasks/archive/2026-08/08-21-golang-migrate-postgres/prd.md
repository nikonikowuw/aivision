# 接入 golang-migrate PostgreSQL 迁移

## 目标

将后端数据库初始化和升级从 API 启动链中拆出，改为使用 `golang-migrate` 执行版本化 PostgreSQL SQL migration。移除 MySQL 支持，生产环境不再依赖 GORM `AutoMigrate` 或启动时 `Seed`。

## 范围

- 仅支持 PostgreSQL；删除 MySQL 驱动、配置、DSN、SQL migration 和相关测试。
- 增加标准命名的嵌入式 migration SQL，并记录 `schema_migrations` 版本。
- 增加独立 `cmd/migrate` 命令，支持 `up`、`down`、`version`、`force`。
- API 启动时校验 schema 版本，但不主动改变 schema。
- 初始 8 表 schema、现有 V1-V3 的结构变更和系统 RBAC 数据纳入 migration 链。
- 增加独立初始管理员 bootstrap 命令，密码来自环境变量，不写入源码。
- 开发 Makefile 先执行 migration，再启动 API；测试继续使用 SQLite + `model.AutoMigrate`。

## 非目标

- 不在本任务中实现菜单代码声明的运行时自动同步；已有 `auto-sync-menu-permissions` 任务单独处理。
- 不迁移或删除现有业务 API、repository、service 行为。
- 不在生产 API 进程中执行 destructive migration 或 seed。

## 验收标准

- `go.mod`、配置和代码中不再包含 MySQL 驱动与 MySQL 数据库分支。
- 空 PostgreSQL 数据库可以通过 `go run ./cmd/migrate up` 建立完整 schema、系统角色、菜单和权限关系。
- 重复执行 `migrate up` 返回 no change，不重复写入系统数据。
- `migrate version` 能报告版本和 dirty 状态；`force` 可用于已有数据库 baseline。
- API 启动不调用 `model.AutoMigrate` 和 `model.Seed`；schema 缺失或版本落后时清晰失败。
- `bootstrap` 命令在迁移完成后创建管理员，密码从指定环境变量读取，重复执行不会覆盖现有用户。
- `make test`、`make vet` 和 `gofmt -l .` 通过。
- migration SQL 同时覆盖 schema 回滚和系统数据回滚；现有数据库接入方式有中文 README 说明。
