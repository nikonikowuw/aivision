# 数据库迁移说明 (PostgreSQL)

本项目使用 `golang-migrate` 管理 PostgreSQL 数据库迁移。

## 迁移文件

迁移文件统一存放在 `app/migrations/`，并通过 `embed.FS` 嵌入二进制，不再依赖运行时宿主机目录：

- `000001_init_schema`: 初始 8 表 baseline。
- `000002_add_menu_parent_index`: 菜单父节点索引。
- `000003_add_department_parent_index_and_fix_status_default`: 部门父节点索引并移除 status 默认值。
- `000004_use_unix_millisecond_soft_delete`: 软删除毫秒时间戳与高并发复合唯一索引。
- `000005_seed_system_rbac`: 系统角色、初始菜单树与超级管理员绑定（数据迁移，不创建管理员用户）。
- `000006_menu_button_name_i18n`: 按钮级菜单 `name` 由中文展示名迁移为标准 i18n key（数据迁移）。

## 常用命令

在 `app/` 目录下：

```bash
# 执行全部待迁移版本
make migrate-up
# 或：go run ./cmd/migrate up

# 回滚最近 1 个版本（谨慎使用）
make migrate-down
# 或：go run ./cmd/migrate down

# 查看当前迁移版本与 dirty 状态
make migrate-version
# 或：go run ./cmd/migrate version

# 仅在 dirty 修复或对已有数据库做 baseline 标记时使用
go run ./cmd/migrate force 5
```

## 已有数据库接入指南 (Baseline)

如果你的数据库此前已由 GORM AutoMigrate + 旧版 SQL 初始化，直接执行 `migrate up` 会因为表已存在而报错。请按以下流程完成接入：

1. 确认数据库结构已与当前代码一致（特别是 `users` / `roles` 的 `deleted_at` 复合唯一索引已建立）。
2. 执行 baseline 命令，将版本号标记为 `4`：
   ```bash
   go run ./cmd/migrate force 4
   ```
3. 执行后续数据迁移：
   ```bash
   go run ./cmd/migrate up
   ```
4. 执行完成后，使用 `go run ./cmd/migrate version` 确认版本已更新到最新版本。

## 生产发布规范

1. 生产 API 启动时**不会自动修改数据库结构**，只会检查当前数据库版本是否达到最新版本要求。
2. 发布流程必须在部署 API 容器之前先执行 `migrate up`。
3. 默认管理员请通过 `cmd/bootstrap` 或 `make bootstrap-admin` 独立创建，不要在启动流程中隐式创建弱密码账号。
