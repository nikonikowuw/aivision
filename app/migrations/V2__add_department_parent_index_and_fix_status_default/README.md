# V2：为部门父节点增加索引并移除状态默认值

## 用途

为 `departments.parent_id` 增加索引，使子部门检查在生产环境保持可控的查询性能；同时移除 `departments.status` 的数据库默认值。部门 service 会在请求省略 status 时明确写入启用值，保留显式 `status=0` 的能力。

## 前置条件

- 已完成初始 8 张表 schema，且 `departments.parent_id`、`departments.status` 列存在。
- 已按驱动选择本目录中的 SQL 脚本执行权限。
- 执行前确认没有需要保留的业务自定义 `status` 默认值。

## 执行方式

根据数据库驱动选择同目录下的 SQL 文件执行一次。索引创建可安全重试；删除默认值在没有默认值时为幂等操作。

## 影响与回滚

创建索引会读取 `departments` 表并可能持有短暂的 metadata lock。删除 status 默认值不会修改已有数据，只影响后续省略该列的直接数据库插入。

回滚时：

- MySQL：恢复 `status` 默认值 `1`，再删除 `idx_departments_parent_id`。
- PostgreSQL：恢复 `status` 默认值 `1`，再执行 `DROP INDEX IF EXISTS idx_departments_parent_id`。

应用代码仍应显式写入 status，不应依赖回滚后的数据库默认值。
