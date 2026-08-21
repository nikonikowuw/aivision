# 实施计划：golang-migrate PostgreSQL

## 1. 任务与验证

1. 更新配置和数据库连接层，删除 MySQL 与 AutoMigrate 配置。验证：配置单测和 `go test ./internal/pkg/config ./internal/pkg/db`。
2. 加入 `golang-migrate` 依赖、嵌入式 migration source、统一 runner 和 `cmd/migrate`。验证：runner/命令编译，source 能识别全部版本。
3. 重写 migration 目录为标准 PostgreSQL up/down SQL，覆盖初始 schema、V1-V3 结构变更、系统 RBAC 数据。验证：静态检查文件命名/版本连续，具备 PostgreSQL 集成测试入口。
4. 从 API 启动链移除 AutoMigrate/Seed，加入 schema version 检查。验证：`main.go` 无生产 schema/data 写入调用，启动检查错误可识别。
5. 新增 bootstrap-admin 命令，更新 Makefile、Dockerfile、配置和后端文档。验证：无密码时失败、已有用户不覆盖、事务写入角色关系。
6. 更新后端规范和项目 README 的迁移/发布说明。验证：文档不再描述人工 V1-V3 或 MySQL。
7. 运行 `gofmt -l .`、`make test`、`make vet`；若 PostgreSQL 服务可用，再执行真实 `migrate up/version` 和 API 启动检查。

## 2. 回滚点

- 配置/依赖层：先确保 Go 测试通过，再进入 migration SQL 改造。
- migration runner：SQL 文件嵌入和命令编译通过后，再切换 API 启动链。
- API 启动链：保留测试用 `model.AutoMigrate`，发现环境无法连接 PostgreSQL 时可回到命令层排查，不恢复启动时自动迁移。
- 不删除未提交的 `auto-sync-menu-permissions` 任务目录；该任务与本次 migration 基础设施改造保持独立。

## 3. 完成标准

- PostgreSQL-only，`golang-migrate` 可独立执行并追踪版本。
- API 不在启动时建表或写入 seed。
- 初始 schema 和系统 RBAC 数据由 SQL migration 提供，管理员由显式 bootstrap 提供。
- `make test` 和 `make vet` 通过，工作区变更范围与本任务一致。
