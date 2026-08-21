# 技术设计：golang-migrate PostgreSQL 迁移

## 1. 运行时边界

生产运行链改为：

```text
cmd/migrate up -> schema_migrations -> cmd/bootstrap -> cmd/api
```

`cmd/api` 只连接数据库、检查迁移版本并启动 HTTP 服务。它不调用 `AutoMigrate`，也不调用 `Seed`。`model.AutoMigrate` 保留给 SQLite 测试辅助函数，不再作为生产能力。

API 启动检查必须区分：

- migration 表不存在：数据库尚未初始化；
- dirty=true：上一次 migration 中断，需要人工处理后 `force`；
- current version < latest：发布流程未完成，不启动服务；
- current version == latest：允许启动。

## 2. PostgreSQL-only 配置

`config.DB` 删除 `Driver` 与 `AutoMigrate` 字段。默认端口、用户、数据库全部使用 PostgreSQL 约定；`db.New` 只构造 PostgreSQL GORM dialector。删除 MySQL DSN、驱动依赖和对应测试。配置 YAML 只保留 PostgreSQL 连接池、时区和连接信息。

## 3. Migration 包与命令

新增 `app/migrations/embed.go`，使用 `embed.FS` 嵌入同目录下的标准 migration 文件。目录直接使用：

```text
app/migrations/
  000001_init_schema.up.sql
  000001_init_schema.down.sql
  000002_add_menu_parent_index.up.sql
  000002_add_menu_parent_index.down.sql
  000003_add_department_parent_index_and_fix_status_default.up.sql
  000003_add_department_parent_index_and_fix_status_default.down.sql
  000004_use_unix_millisecond_soft_delete.up.sql
  000004_use_unix_millisecond_soft_delete.down.sql
  000005_seed_system_rbac.up.sql
  000005_seed_system_rbac.down.sql
```

新增 `internal/pkg/migration`：

- 从 GORM 的 `*sql.DB` 创建 `golang-migrate` PostgreSQL database driver；
- 使用 `source/iofs` 读取嵌入式 SQL；
- `Up`、`Down`、`Version`、`Force` 操作统一封装；
- 暴露 `LatestVersion` 和 `CheckCurrent`，供命令与 API 使用；
- PostgreSQL migration 开启多语句支持，并使用 migration driver 的数据库锁。

新增 `cmd/migrate/main.go`：

- `up`：应用所有待执行 migration；
- `down`：回滚最近一个 migration；
- `version`：输出版本与 dirty 状态；
- `force VERSION`：仅用于人工确认后的 baseline 或 dirty 修复。

命令复用 `config.Load`、`logger.New`、`db.New`，不复制 DSN 逻辑。

## 4. SQL migration 链

### 000001 init schema

创建当前项目的 8 张基础表，但保留 V1-V3 之前的历史状态：

- `gorm.DeletedAt` 对应的时间戳 nullable 字段；
- users.username 和 roles.code 的单列唯一索引；
- menus/departments 尚无 parent_id 索引；
- departments.status 仍有默认值 1；
- 不创建任何外键。

这样后续 000002-000004 能真实表达已有历史迁移。SQL 使用 PostgreSQL `bigserial`、`timestamptz`、`smallint`、`boolean` 等类型，并显式创建 GORM 模型需要的索引。

### 000002 / 000003

将现有 V1、V2 改成 golang-migrate 标准文件，提供 up/down：菜单和部门 parent_id 索引，以及移除部门 status 默认值。

### 000004 soft delete

将 users、roles、menus、departments 的 `deleted_at` 从 nullable timestamp 转为毫秒 Unix 时间戳 `bigint NOT NULL DEFAULT 0`。历史非空时间转换为毫秒，NULL 转为 0。删除 users/roles 单列唯一索引，创建 `(username, deleted_at)` 与 `(code, deleted_at)` 复合唯一索引。

Down migration 反向转换非零毫秒值为 `timestamptz`，0 转 NULL，并恢复单列唯一索引。生产环境不建议使用 down，回滚以备份和向前修复为主。

### 000005 system RBAC

将当前静态系统角色、菜单、按钮和 super 角色绑定写入 SQL。使用 PostgreSQL `DO` 块和稳定业务键实现幂等：

- `roles.code = super` 作为角色键；
- 菜单以 `parent_id + name` 查找；
- 只补齐缺失记录和 super 绑定；
- 不覆盖已存在菜单字段；
- 不删除额外业务菜单；
- 不创建默认管理员，不写入默认密码。

这使系统权限数据成为可审计的版本化数据迁移，而不是 API 启动副作用。

## 5. Bootstrap 管理员

新增 `cmd/bootstrap/main.go`：

- 从 `APP_BOOTSTRAP_ADMIN_PASSWORD` 读取初始密码；没有密码时直接失败；
- 用户名、昵称、邮箱支持命令行参数，用户名默认 `admin`；
- 在一个事务中检查用户是否存在、查找 `super` 角色、创建用户和 user_roles 绑定；
- 已存在用户时返回明确错误，不覆盖密码；
- 不创建默认硬编码密码，不记录密码日志。

现有 `model.Seed` 仅保留为 SQLite 开发/测试 fixture，不能再由 API 启动调用。

## 6. 发布与兼容

Makefile 增加 `migrate-up`、`migrate-down`、`migrate-version`、`bootstrap-admin`。`make dev` 先执行 `migrate-up`，再运行 Air；Docker 镜像同时构建 api、migrate、bootstrap 三个二进制，migration SQL 已嵌入二进制，不再依赖运行时 SQL 目录。

现有空库或尚未正式发布的开发库可以删除重建后执行完整链。已有数据库不能直接执行 000001；应先备份并核对 schema，使用 `migrate force 4` 标记当前结构，再执行 000005 和 bootstrap。若数据库仍处于 V1/V2 状态，则先 force 到对应确认版本，再让 000004/000005 正常升级。README 记录这一 baseline 流程。

## 7. 验证

- Go 单元测试覆盖 PostgreSQL DSN、migration source 文件解析、命令参数和 API schema 检查分支；
- SQLite 业务测试继续使用 `model.AutoMigrate`，不把 SQLite 当作生产 migration 兼容性证明；
- 静态检查确保 MySQL 字符串、依赖、分支和 SQL 文件已移除；
- 真实 PostgreSQL 集成验证空库 up、重复 up、version 和最终表结构需在有 PostgreSQL 服务时执行。
