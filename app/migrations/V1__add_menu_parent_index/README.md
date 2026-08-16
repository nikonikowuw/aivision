# V1：为菜单父节点增加索引

## 用途

为 `menus.parent_id` 增加索引，使菜单树查询和删除前的子节点计数在生产环境保持可控的查询性能。对应 GORM 模型中的 `Menu.ParentID` `index` 标签。

## 前置条件

- 已完成初始 8 张表 schema，且 `menus.parent_id` 列存在。
- 执行账号需要在 `menus` 表上创建索引的权限。
- 执行前确认当前没有同名的 `idx_menus_parent_id` 索引。

## 执行方式

根据数据库驱动选择同目录下的 SQL 文件执行一次。MySQL 脚本会检查索引是否已存在；PostgreSQL 使用 `IF NOT EXISTS`，脚本可安全重试。

## 影响与回滚

创建普通二级索引会读取 `menus` 表并可能持有短暂的 metadata lock。回滚时可在维护窗口执行：

- MySQL：`DROP INDEX idx_menus_parent_id ON menus;`
- PostgreSQL：`DROP INDEX IF EXISTS idx_menus_parent_id;`
