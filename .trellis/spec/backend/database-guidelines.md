# 数据库规范

> 本项目数据库模式与约定。

---

## 概览

- ORM：GORM（`gorm.io/gorm` v1.31），支持 mysql / postgres / sqlite 驱动。应用
  驱动由 `db.driver` 决定（默认 `mysql`，支持 `postgres` —— 决策 18）；
  测试使用 sqlite 内存库。
- Schema 管理按环境拆分：dev/test 由启动时的 GORM `AutoMigrate` 建库；
  生产环境表结构变更走 `app/migrations/` 下的版本化 SQL 脚本（见下文"迁移"）。
  MVP 尚未接入迁移工具——脚本按发版清单人工执行。
- **不建外键。** 关系纯为逻辑关系（连接表 `user_roles`、`role_menus` 使用
  复合唯一索引）。
- Seed 是幂等的：当 `admin` 已存在时 `model.Seed` 整体跳过；行通过业务唯一键
  上的 `FirstOrCreate` 插入。

---

## 查询模式

- 所有查询都通过 wire 提供的共享 `*gorm.DB` 走 GORM；目前尚未引入原生 SQL。
- 模型纯函数（如 `BuildMenuTree`）是作用于模型结构体的纯 Go 代码，无需数据库
  即可单元测试。
- 需要原子性的多写操作必须使用 `db.Transaction`；目前尚无 service 层，
  该模式待第一个 service 编写时确认。

---

## 迁移

**策略：AutoMigrate 只是 dev/test 的便利手段；生产环境的表结构变更全部走版本化
SQL 脚本**。

- **开发 / 测试**：继续使用 GORM `AutoMigrate`（`model.AutoMigrate`）快速建好 8 张表；
  `make dev` 即可就绪。
- **生产 / 发版**：必须禁用 `AutoMigrate`（配置项 `db.auto_migrate=false`，如
  `APP_DB_AUTO_MIGRATE=false`）；表结构变更通过随发版执行的版本化 SQL 脚本应用。
- **首次生产部署**：初始 8 表 schema 二选一——先跑一次 AutoMigrate 建库再关闭，
  或随 V1 基线脚本建库；在发版说明中记录所选方式。
- **每次 model 变更必须携带迁移**：结构变更需要**同时**更新 gorm 模型/标签
  （保持 dev/test 的 AutoMigrate 同步）**和**新增一个版本化 SQL 脚本（生产实际
  应用的内容）。二者不允许漂移。

### SQL 脚本约定

- **位置**：`app/migrations/`，**每次 SQL 变更一个目录**：
  ```
  app/migrations/
  └── V<序号>__<简短描述>/       # 如 V1__add_user_email
      ├── README.md             # 介绍本次 SQL 变更：用途、前提条件、执行方式
      └── <简短描述>.sql        # 本次变更的脚本（驱动拆分见下）
  ```
- **目录/命名**：目录名 `V<递增序号>__<简短描述>`；目录内脚本按 `<简短描述>.sql`
  命名，语句因驱动而异时按驱动拆分 `<desc>.mysql.sql` / `<desc>.pg.sql`；仅当语句
  在两个驱动上完全一致时才使用单文件。
- **每个目录的 README.md**：介绍**本次** SQL 变更——用途、**前提条件**（前置
  schema 版本、依赖的先前迁移、数据/权限要求、是否需备份或停机窗口）、执行方式
  与影响（顺序、锁表/耗时、失败如何重试或回滚）。发版时按该 README 的清单与前提
  条件核验后再执行。
- **版本化**：新变更 = 新的更高版本号**目录**。**已发布的迁移目录不可修改**——
  绝不编辑已应用过的内容；向前修复（新增更高版本目录）。
- **幂等性**：尽量使用可重复执行的语句（`IF NOT EXISTS`、有保护的插入），这样
  发版中途失败后可以安全重试。
- **破坏性变更**（删列/删表、改类型、重命名）：必须经评审批准，并随附回滚脚本
  （放在同目录，如 `<desc>.rollback.sql`）。
- **数据迁移**（回填、值转换）属于 SQL 脚本，**不要**放进 `seed.go`——seed 只
  保留初始幂等的演示/权限数据。
- **执行方式**：MVP 阶段无迁移工具——脚本由发版清单人工执行（先于或伴随应用
  发布）；建议在 V1 迁移目录的脚本中创建 `schema_migrations` 表记录已应用版本，便于审计。

---

## 命名约定

- 表名：显式 `TableName()` 返回单数 snake_case（`users`、`menus`、
  `user_roles`、`refresh_tokens`、`operation_logs`）。
- 列：每个字段显式声明 `gorm:"column:snake_case"` 标签；JSON 标签为 camelCase
  （`json:"deptId"`）。
- 共享字段：可软删除的业务表内嵌 `BaseModel`（ID + 时间戳 + 软删除
  `DeletedAt`）；不可软删除的表（`refresh_tokens`、`operation_logs`）使用
  `TimeFields`（仅时间戳）。
- 枚举：字符串常量并带 `String()` 风格的契约，例如 `menus.type` 为
  `catalog|menu|button`（`MenuTypeCatalog` 等）。不要为简单的字符串列发明新的
  枚举类型。
- 索引：对会被查询或需要唯一的列添加 `gorm:"index"`（或 `uniqueIndex`）标签；
  连接表使用复合唯一键。

---

## 常见错误

- **不要**在 gorm 标签中写 `type:tinyint`（或其他 MySQL 专属类型）——Postgres
  没有 `tinyint`（决策 18）。使用纯 Go 类型（`status` 用 `int8`），让 gorm 按
  驱动映射。
- **不要添加外键** —— 本项目刻意只使用逻辑关系。
- **不要依赖** gorm 对表/列的默认命名；始终显式声明 `TableName()` 和列标签。
- 改动 `seed.go` 时**不要跳过** seed 幂等性保护——它绝不能覆盖用户对
  menus/roles 的修改。
- **不要修改** `seedMenuTree` 中已有的权限码——它是前端权限的唯一契约
  （"严禁增删权限码"）。
- **不要依赖生产环境中的 AutoMigrate** — `db.auto_migrate=false` 时它会被静默跳过，
  只改 model 会导致生产 schema 滞后。每次 model 变更必须同步携带 SQL 脚本。
- **不要编辑已发布的迁移脚本** — 已发布脚本不可修改；向前修复（新增更高版本）。
- **不要在 `seed.go` 中做数据回填** — seed 只是初始幂等数据；数据迁移走 SQL 脚本。
- **不要只写 MySQL 语法 / 只在一侧验证** — 项目同时支持 mysql 与 postgres；
  按驱动拆分文件并保持两侧同步。

## GORM 零值与树关系写入契约

### 1. 适用范围

凡是 API 可以显式提交零值、且数据库字段曾使用 `gorm:"default:..."` 的状态字段，
以及没有数据库外键保护的 parent-child 树关系写操作，都适用本节。

### 2. Schema 与数据层签名

- 业务状态使用 `int8`；当 `0` 是合法输入时，模型字段不要声明会让 GORM 接管
  零值的 `default` 标签，service 必须负责省略值的默认填充。
- 树关系的 repository 写操作应保持 `context.Context`，并将需要共同判定的检查与
  写入放在同一 `db.Transaction` 中。

### 3. 跨层契约

- 请求省略 `status`：新建由 service 写入 `StatusEnabled`；请求显式 `status=0`：
  必须原样写入并从 API 返回。
- 删除父节点：只有不存在未软删子节点时成功；不存在返回 `CodeNotFound`，有子节点
  返回对应业务错误码。
- 创建或移动到某个父节点与删除该父节点必须使用同一锁定对象，不能只依赖先查后写。

### 4. 校验与错误矩阵

| 条件 | 结果 |
| --- | --- |
| 新建省略 status | service 写入 `StatusEnabled` |
| 新建显式 `status=0` | 持久化 `0`，不得被 ORM default 改写 |
| 删除目标不存在 | `CodeNotFound` |
| 删除目标存在未软删子节点 | `CodeDeptHasChildren` |
| parent 在并发期间被软删 | 创建/编辑映射为 `CodeNotFound` |

### 5. Good / Base / Bad

- Good：事务内锁父/目标行，检查子节点后再软删；状态默认值在 service 明确填充。
- Base：已软删子节点不计入“有子节点”检查。
- Bad：先 `Count` 再独立 `Delete`，或在带 `default:1` 的 int8 字段上直接 `Create` 零值。

### 6. 必需测试

- sqlite 测试断言创建显式 `status=0` 后重新查询仍为 `0`。
- repository/service 测试断言有子节点时删除事务回滚且返回 `CodeDeptHasChildren`。
- API 测试断言状态往返、树结构和统一错误响应；迁移同时提供 MySQL/PostgreSQL 脚本。

### 7. 错误与正确示例

错误：

```go
if count == 0 {
    db.Delete(&model.Department{}, id)
}
```

正确：

```go
return db.Transaction(func(tx *gorm.DB) error {
    // 锁定目标，检查未软删子节点，然后在同一事务内软删。
    return nil
})
```

错误：

```go
Status int8 `gorm:"default:1"`
```

正确：

```go
Status int8 `gorm:"column:status"` // service 明确填充省略值
```
