# 高并发唯一约束安全性修复 (Soft Delete)

## 背景
在使用 GORM 默认的 `gorm.DeletedAt` (基于时间戳且为 nullable) 时，软删除通过设为当前时间实现，未删除时为 `NULL`。然而在不同的数据库中对于 `NULL` 的唯一索引有不同实现：
- Postgres 支持 `WHERE deleted_at IS NULL` 的部分索引（能够完美满足未删除情况下的唯一性）。
- MySQL/SQLite 等将 `NULL` 视为不相等，这就允许了多个活动记录在同一个唯一索引上出现重复（如果不创建特殊的计算列等复杂机制）。

如果手动在 Service 层检查 `GetByUsername` 等方法，会由于高并发的情况（如同时两个请求插入同一个用户名）导致数据中出现重复的处于活动状态的用户。

## 解决办法
恢复引入并使用 `gorm.io/plugin/soft_delete`，利用它提供的在未软删除时 `deleted_at = 0` 的特性，而删除时它等于时间戳（毫秒级）。此时所有的唯一索引都可以变成普通的复合唯一索引 `(username, deleted_at)`。
这样能够借助数据库原生的唯一约束解决高并发下的并发创建冲突（在写入时报错 `ErrDuplicatedKey`，再映射为业务错误返回）。

本 Migration 完成了：
1. 将 `deleted_at` 从时间类型 (nullable) 修改为 `bigint` (default 0)。
2. 删除由于之前取消软删除插件产生的不安全的单列/部分索引。
3. 创建新的安全复合索引。
