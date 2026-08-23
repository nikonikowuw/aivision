# 数据库规范

> 本项目数据库模式与约定。

---

## 概览

- ORM：GORM（`gorm.io/gorm` v1.31），仅使用 `postgres` 驱动；单元测试使用 sqlite 内存库。
- Schema 管理采用 `golang-migrate` 版本化迁移（`app/migrations/` 下嵌入式 `.sql` 文件）。
- 生产环境禁止在 API 启动时运行 GORM `AutoMigrate` 或自动 Seed；API 启动只做迁移版本就绪校验。
- `model.AutoMigrate` 与 `model.Seed` 仅保留给开发/测试本地 fixture 和 sqlite 单元测试使用。
- **不建外键。** 关系纯为逻辑关系（连接表 `user_roles`、`role_menus` 使用复合唯一索引）。
- 默认管理员不硬编码在源码中；通过 `cmd/bootstrap` 独立创建，密码通过环境变量注入。

---

## 查询模式

- 所有查询都通过 wire 提供的共享 `*gorm.DB` 走 GORM；目前尚未引入原生 SQL。
- 模型纯函数（如 `BuildMenuTree`）是作用于模型结构体的纯 Go 代码，无需数据库
  即可单元测试。
- 需要原子性的多写操作必须使用 `db.Transaction`；目前尚无 service 层，
  该模式待第一个 service 编写时确认。

---

## 迁移

## 迁移

**策略：生产环境与开发环境统一采用 `golang-migrate` 执行版本化 PostgreSQL SQL 脚本**。

- **迁移文件**：统一命名为 `app/migrations/<序号>_<名称>.up.sql` 与 `.down.sql`，由 `embed.FS` 嵌入进二进制。
- **执行命令**：通过 `go run ./cmd/migrate up`（或 `make migrate-up`）执行迁移；发布流程必须先跑迁移再启动/滚动更新 API 容器。
- **测试环境**：sqlite 内存库单测继续使用 `model.AutoMigrate`，不把 sqlite 视为生产迁移执行器。
- **结构变更契约**：新增模型或变更字段必须同时更新 GORM 模型标签与对应的新增 migration SQL，保持两者一致。
- **数据迁移**：系统角色、菜单、权限码的初始化与增量补齐属于版本化 data migration（如 `000005_seed_system_rbac.up.sql`），不放在 API 启动流程中。
- **管理员创建**：通过 `cmd/bootstrap` 或 `make bootstrap-admin` 显式创建，密码来自环境变量 `APP_BOOTSTRAP_ADMIN_PASSWORD`，绝不硬编码。

### SQL 脚本约定

- **位置**：`app/migrations/` 下标准扁平文件结构。
- **版本化**：新变更 = 新递增序号的前缀文件（如 `000006_xxx.up.sql` 与 `000006_xxx.down.sql`）。已合并发布的迁移不可修改；向前修复。
- **幂等性与兼容性**：DDL 尽量使用 `IF NOT EXISTS` / `IF EXISTS`；数据迁移优先使用明确业务键与 `ON CONFLICT DO NOTHING`。
- **破坏性变更**：必须评审批准并经过全量数据升级验证。
- **禁止项**：禁止在 API 启动中写库补表或补数据；禁止把数据回填写进 `seed.go`。

## 对时配置与增量权限迁移契约

### 1. Scope / Trigger

- **Trigger**：新增 `system_configs` JSONB 配置、NTP 系统执行器和 Ops 时间菜单时适用。
- **Scope**：数据库事实来源由 `system_configs` 保存；系统命令由 `ntp.Executor` 执行；API 不在启动时建表或 seed。
- **Why**：避免数据库状态、系统时钟状态和前端 API 契约各自漂移。

### 2. Signatures

- 数据库：`system_configs(key VARCHAR(64) UNIQUE, value JSONB NOT NULL, remark VARCHAR(255))`；`key = 'system:time'`。
- 仓储：`GetByKey(ctx, key) (*model.SystemConfig, error)`、`SetByKey(ctx, key, value, remark) error`；写入使用 `ON CONFLICT (key)` 更新配置值、备注和 `updated_at`。
- API：`GET /api/ntp/config`、`PUT /api/ntp/config`、`GET /api/ntp/status`、`POST /api/ntp/sync`、`POST /api/ntp/set-time`、`GET /api/ntp/synced`。
- 执行器：`ApplyNTP`、`DisableNTP`、`SyncNow`、`SetSystemTime`、`GetStatus`；生产平台不可用时使用返回错误的 unavailable executor，不得伪造 Mock 成功。

### 3. Contracts

- `system:time` 的 JSON 结构为 `{ "mode": "ntp"|"manual", "servers": string[] }`；NTP 模式必须有至少一个非空服务器。
- `000007_add_system_configs` 创建表和默认配置；`000008_seed_ops_time` 幂等创建 `Ops`、`Time`、`ops:time:read`、`ops:time:edit`，并绑定 `super` 角色。
- `POST /api/ntp/set-time` 成功后必须停用 NTP，并将配置模式持久化为 `manual`；调用者无需先切换模式。
- migration 必须通过 `go run ./cmd/migrate up` 执行，重复执行无变化；`down` 必须按版本逆序移除本次对象。
- `UpdateConfig` 保存的是期望配置：执行器应用失败时接口返回错误，但保留已校验的数据库配置，后续 `ReplayOnBoot` 负责重试；状态接口仍以执行器的实时状态为准。
- macOS 的 `systemsetup` 只支持一个网络时间服务器；适配器使用列表首项作为当前服务器，同时保留完整列表以便跨平台迁移，不得把首项以外的服务器当作已在 macOS 生效。
- 所有 API 错误仍由统一 `{code, data, message}` 中间件输出，handler 不拼接响应体。

### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| mode 不是 `ntp` 或 `manual` | `CodeNTPInvalidMode`（1204） |
| NTP 模式服务器列表为空 | `CodeNTPServersEmpty`（1203） |
| 服务器含控制字符或内部空白 | `CodeInvalidParam`（1009） |
| 手动模式触发 NTP 同步 | `CodeNTPSyncNotAllowedInManualMode`（1202） |
| NTP 应用或立即同步失败 | `CodeNTPSyncFailed`（1206） |
| 执行器查询/停用不可用 | `CodeNTPExecutorUnavailable`（1207） |
| 系统设时失败 | `CodeNTPSetTimeFailed`（1205） |
| 设时成功但 manual 配置持久化失败 | `CodeInternal`（1500），不得返回成功 |

### 5. Good / Base / Bad Cases

- **Good**：先在 service 层规范化并校验服务器，再通过 repository 写入 JSONB；成功设时后写入 `manual`；启动时由 `ReplayOnBoot` 重放数据库配置。
- **Base**：数据库中没有 `system:time` 时返回代码默认 NTP 配置；`servers: null` 读取为空列表，不能绕过 NTP 模式非空校验。
- **Bad**：将用户输入直接拼入配置文件、在无系统工具时退回成功的内存 Mock、或只更新数据库而把执行器失败返回为成功。

### 6. Tests Required

- SQLite repository/service 测试：断言 JSONB 字符串往返、upsert 更新、非法 mode、空服务器、控制字符/内部空白和设时持久化失败。
- Executor 测试：断言 chrony/timesyncd 状态解析、RFC3339 设时参数和 unsupported 平台返回错误；测试替身不得执行真实系统时钟修改。
- API 测试：断言六个 endpoint 的 method/path、统一响应 envelope、1202/1203/1204/1205/1206/1207 错误映射及认证/权限注册。
- PostgreSQL 验证：临时库执行 migration up、重复 up、down；断言 `system_configs` 默认行、Ops/Time 菜单、两个按钮权限和 `super` 绑定。

### 7. Wrong vs Correct

错误：

```go
if _, err := exec.LookPath("chronyc"); err != nil {
    return NewMockExecutor() // 生产环境伪造系统已成功对时
}
```

正确：

```go
return newUnavailableExecutor("no supported Linux NTP tool found")
```

错误：

```go
// API 启动时直接 AutoMigrate/Seed
model.AutoMigrate(db)
model.Seed(db)
```

正确：

```bash
go run ./cmd/migrate up
```

---

## 命名约定

- 表名：显式 `TableName()` 返回单数 snake_case（`users`、`menus`、
  `user_roles`、`refresh_tokens`、`operation_logs`）。
- 列：每个字段显式声明 `gorm:"column:snake_case"` 标签；JSON 标签为 camelCase
  （`json:"deptId"`）。
- 软删除与高并发安全（CRITICAL）：
  - 凡是需要唯一性约束（如 `users.username`、`roles.code`）且需要软删除的模型，**绝对禁止**使用 GORM 原生的 `gorm.DeletedAt`（其底层使用 `NULL` 标识活跃记录）。
  - 必须使用 `gorm.io/plugin/soft_delete` 插件，约定 `deleted_at = 0` 表示活跃，非 `0` 时间戳表示已删除。
  - 这是因为在 PostgreSQL 和 SQLite 中，唯一索引里的 `NULL` 不等于 `NULL`。如果使用原生 `DeletedAt`，当并发插入相同数据时（或高并发事务中），带有 `NULL` 的复合唯一索引（如 `(username, deleted_at)`）不会拦截重复数据，从而引发 Race Condition 插入多个相同的活跃用户名。
  - 只有使用 `deleted_at = 0`，数据库级别的唯一约束才能在高并发下稳定生效，阻挡脏数据；同时已删除记录带有不同时间戳，互不冲突。
  - 查询这些表时，必须显式或通过插件自动应用 `deleted_at = 0` 来过滤活跃记录，不得使用 `deleted_at IS NULL`。
- 共享字段：内嵌 `BaseModel` 必须使用 soft_delete 插件的 `soft_delete.DeletedAt`；不可软删除的表（`refresh_tokens`、`operation_logs`）使用
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
- **不要使用原生的 `gorm.DeletedAt`** — 带有唯一约束的模型必须使用 `gorm.io/plugin/soft_delete` 且 `deleted_at = 0` 表示活跃，否则高并发下唯一索引对 `NULL` 无效会导致重复插入的安全漏洞。
- 改动 `seed.go` 时**不要跳过** seed 幂等性保护——它绝不能覆盖用户对
  menus/roles 的修改。
- **不要修改** `seedMenuTree` 中已有的权限码——它是前端权限的唯一契约
  （"严禁增删权限码"）。
- **不要在 API 启动时自动建表或 seed** — 结构与系统权限数据必须由 `golang-migrate` 显式管理。
- **不要编辑已发布的迁移脚本** — 已发布脚本不可修改；向前修复（新增更高版本）。
- **不要在 `seed.go` 中做数据回填** — seed 只是开发/测试初始 fixture；数据迁移走 SQL 脚本。
- **不要引入除 PostgreSQL 以外的第二生产方言** — 本项目生产环境专注 PostgreSQL。

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
