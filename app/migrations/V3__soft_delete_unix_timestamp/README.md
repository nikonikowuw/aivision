# V3：软删除改造为毫秒时间戳与复合唯一索引

## 用途

解决软删除后无法复用同名用户名（`users.username`）与角色编码（`roles.code`）的问题：
1. 所有软删除表（`users`、`roles`、`menus`、`departments`）的 `deleted_at` 字段由 `DATETIME/TIMESTAMP NULL` 改造为 `BIGINT NOT NULL DEFAULT 0`（毫秒 Unix 时间戳，0 表示未删除）。
2. 将 `users.username` 单列唯一索引改造为 `(username, deleted_at)` 复合唯一索引（`uk_users_username`）。
3. 将 `roles.code` 单列唯一索引改造为 `(code, deleted_at)` 复合唯一索引（`uk_roles_code`）。

## 前置条件

- 已完成 V1、V2 迁移。
- 执行账号拥有变更表结构与增删索引的 DDL 权限。
- 若已有被软删除的记录，执行前需将历史已软删数据的 `deleted_at` 转换或回填为非 0 时间戳毫秒值（`UNIX_TIMESTAMP(deleted_at) * 1000` / `EXTRACT(EPOCH FROM deleted_at) * 1000`），未删除记录回填为 `0`。

## 执行方式

根据数据库驱动选择执行同目录下的脚本：
- MySQL: `soft_delete_unix_timestamp.mysql.sql`
- PostgreSQL: `soft_delete_unix_timestamp.pg.sql`

## 影响与回滚

- 修改列类型与重建索引期间可能产生短暂的表级锁。建议在低峰期或维护窗口执行。
- 回滚操作需重建原单列唯一索引并将 `deleted_at` 字段类型改回时间戳/datetime。
