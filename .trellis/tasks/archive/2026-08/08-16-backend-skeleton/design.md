# 技术设计：Go 工程骨架（backend-skeleton）

> 源需求见本任务 prd.md；8 表结构以父 design.md §2 为准，本文只写本任务范围内的落地细节。后续子任务新增代码遵循本设计确立的范式。

## 1. 目录与文件清单

```
app/
├── go.mod                      # module niko-vue-admin/app（prd 已定）
├── cmd/api/
│   ├── main.go                 # 启动流程：config → logger → gorm → AutoMigrate → seed → wire → 启动
│   ├── wire.go                 # //go:build wireinject 声明
│   └── wire_gen.go             # wire 生成（提交进仓库）
├── internal/
│   ├── model/                  # 8 表 gorm 模型 + 纯函数（树构建等）
│   ├── pkg/
│   │   ├── config/config.go    # viper：yaml + APP_* 环境变量覆盖
│   │   ├── logger/logger.go    # zap
│   │   ├── response/response.go# {code,data,message}
│   │   └── errno/errno.go      # 错误码表
│   └── router/router.go        # gin engine 装配点（本任务为空壳，后续任务注册路由）
├── configs/config.yaml
├── Makefile                    # wire / dev / build / test / vet
```

`repository/`、`service/`、`api/`、`middleware/` 目录留到对应子任务创建（YAGNI）。skeleton 引入 gin 仅作 engine 装配点，不注册业务路由。

## 2. Model 落地细节（父 design.md §2 → gorm）

- 公共字段抽 `internal/model/base.go` 的 `BaseModel`（`ID uint64` 自增、`CreatedAt`、`UpdatedAt`、`DeletedAt gorm.DeletedAt` 索引）；`operation_logs`、`refresh_tokens` 不使用软删除，单独定义时间字段。
- 表名统一显式 `TableName()`（`users`、`roles`、`menus`、`departments`、`user_roles`、`role_menus`、`refresh_tokens`、`operation_logs`），列名用 tag 显式声明，不依赖默认蛇形猜测。
- 唯一约束：`users.username`、`roles.code`、`refresh_tokens.token`；`user_roles`(user_id+role_id)、`role_menus`(role_id+menu_id) 用 gorm 复合唯一索引 tag。
- 索引：`users.dept_id`、`refresh_tokens.user_id`/`expires_at`、`operation_logs.created_at`/`username`/`module`/`status_code`；外键一律不建（gorm AutoMigrate 默认无 FK，纯逻辑关联）。
- `menus` 字段名与 `keep_alive`/`home_path` 等蛇形列名不一致处用 tag 映射（`KeepAlive bool` → `column:keep_alive`）。
- **i18n（决策 17，父 prd 新增）**：menus 表新增 `Title`（`column:title;type:varchar(128)`）存 i18n key；`Name` 保持 ASCII 路由标识符，与展示文案解耦。
- 类型：`status` 用 `int8`（go 类型），**不写 `type:tinyint`**——Postgres 无 tinyint（决策 18），由 gorm 按驱动自动映射（MySQL→tinyint，Postgres→smallint）；`menus.type` 等枚举语义用 string（`catalog|menu|button`），不引入自定义类型。

## 3. 配置（config.yaml + 环境变量覆盖）

```yaml
server:
  port: 8000
db:
  driver: mysql              # mysql | postgres（决策 18）
  host: 127.0.0.1
  port: 3306
  user: root
  password: "123456"     # 开发默认；生产用 APP_DB_PASSWORD 覆盖
  name: niko_vue_admin
jwt:
  secret: "dev-secret-change-me"
  access_ttl: 2h
  refresh_ttl: 168h
log:
  level: info
```

viper 规则：`SetEnvPrefix("APP")` + `SetEnvKeyReplacer("." → "_")` + `AutomaticEnv` → `APP_DB_DRIVER`/`APP_DB_HOST` 覆盖 `db.driver`/`db.host`、`APP_JWT_SECRET` 覆盖 `jwt.secret`，以此类推。config 包只暴露 `Load()`（返回 *Config，缺失键有默认值、非法值报错）；TTL 解析为 `time.Duration`。

**`db.driver` 校验**：仅接受 `mysql`/`postgres`，默认 `mysql`；非法值 `Load` 报错。

## 4. 启动流程（main.go）

1. config.Load() → 失败 Fatal
2. zap logger（level 来自配置）
3. gorm 连数据库（`db.driver` 选 mysql/postgres 驱动，带重试 3 次、间隔 2s，容器场景数据库未就绪常见）→ 失败 Fatal
4. AutoMigrate 全部 8 表 → 失败 Fatal
5. seed（幂等，见 §5）→ 日志打印「AutoMigrate + seed 完成」
6. wire 装配 `*gin.Engine` → 监听 `server.port`
7. 优雅退出：SIGINT/SIGTERM → `http.Server.Shutdown`（10s 超时）

## 5. seed 设计与权限码契约

## 5. seed 设计与权限码契约

- **幂等**：以 `users` 表存在 `admin` 为已 seed 判定；存在则整体跳过（不覆盖用户对菜单的后续修改）。菜单/角色用 `FirstOrCreate` 按业务唯一键（username/code/菜单的 `name+parent_id` 组合）插入，重复执行不产生重复行。
- **seed 数据**：super 角色（code=super，status=1）→ admin 用户（bcrypt("admin123")）+ `user_roles` 绑定 → 默认菜单树（下述）→ demo 部门（名「演示部门」，parent_id=0）。
- **菜单树**（sort 升序编号；`Title` 为 i18n key，决策 17）：

```
catalog System   /system     BasicLayout         Title: routes.system.system   ← 一级导航「系统管理」
├─ menu User     /system/user    /system/user/index    system:user   Title: routes.system.user
│   ├─ button 新增用户  system:user:add
│   ├─ button 编辑用户  system:user:edit
│   ├─ button 删除用户  system:user:delete
│   ├─ button 重置密码  system:user:reset-password
│   ├─ button 分配角色  system:user:assign-role
│   └─ button 启停用    system:user:status
├─ menu Role     /system/role    /system/role/index    system:role   Title: routes.system.role
│   ├─ button 新增角色 system:role:add
│   ├─ button 编辑角色 system:role:edit
│   ├─ button 删除角色 system:role:delete
│   └─ button 分配菜单 system:role:assign-menu
├─ menu Menu     /system/menu    /system/menu/index    system:menu   Title: routes.system.menu
│   ├─ button 新增菜单 system:menu:add
│   ├─ button 编辑菜单 system:menu:edit
│   └─ button 删除菜单 system:menu:delete
├─ menu Dept     /system/dept    /system/dept/index    system:dept   Title: routes.system.dept
│   ├─ button 新增部门 system:dept:add
│   ├─ button 编辑部门 system:dept:edit
│   └─ button 删除部门 system:dept:delete
└─ menu Log      /system/log     /system/log/index     system:log    Title: routes.system.log   （只读模块，无按钮码）
catalog Dashboard /dashboard     BasicLayout          Title: routes.dashboard.title   ← 一级导航「首页」
└─ menu dashboard /dashboard     /dashboard/index     Title: routes.dashboard.analytics （component 用 vben 自带视图）
```

> **权限码契约**：上表是**全项目唯一权限码源**。后续 backend-dept/menu/role/user/oplog 的路由声明与 frontend-pages 的 `v-access` 严格沿用，新增按钮码只在对应子任务中经评审后加。catalog/menu 的 `permission` 字段存模块码（`system:user` 等），button 存动作码。
>
> **title key 契约**：仅 catalog/menu 有 title（button 无 title，前端不展示按钮标题）。key 即上表 Title 列，`routes.<模块>.<节点>` 命名；**三语 locale 文件（zh-CN/zh-TW/en-US 的 routes 段）必须与 seed title 一一对应**（frontend-integration 落地）。

- seed 菜单 `meta`（icon/title/order/keepAlive）按父 design.md §4 结构写入：title 中文、order=sort、icon 用 vben 支持的内置图标名（如 `ant-design:user-outlined` 对应 vben 的 `iconify` 前缀）。

## 6. wire 装配链

```
config.Config → *gorm.DB → *zap.Logger → *gin.Engine
```

- `wire.go`：`//go:build wireinject` + `ProviderSet`（config.New / db.New / logger.New / router.New）；`wire_gen.go` 由 `make wire` 生成并提交。
- 本任务 ProviderSet 只有 4 个 Provider；后续任务新增 repository/service/api Provider 时各自扩展 ProviderSet，不改 wire_gen 手写。

## 7. Makefile

```makefile
wire:   # go generate 或直接 wire ./cmd/api
dev:    # go run ./cmd/api
build:  # go build -o bin/api ./cmd/api
test:   # go test ./...
vet:    # go vet ./...
```

## 8. 测试策略

- `config`：yaml 解析 + 环境变量覆盖 + 默认值（纯单测，用 t.Setenv）。
- `model`：菜单树构建纯函数（`BuildMenuTree`）单测。
- 集成冒烟：`internal/model` 用 `gorm sqlite :memory:` 跑 AutoMigrate + seed 逻辑抽出的 `seed.Seed(db)` 函数（seed 放在 `internal/model` 还是独立 `internal/seed`？——放 `internal/model/seed.go`，与表结构同包，测试可复用）。MySQL 方言差异（tinyint/DATETIME）在 sqlite 下可兼容，不用 MySQL 特有 SQL。
- AC-3（连真 MySQL 起服务）用 docker 起 mysql:8 验证，不进单测。

## 9. 风险与权衡

| 风险/权衡 | 结论 |
|---|---|
| 本机无 MySQL 客户端 | 用 `docker run mysql:8` 验证 AC-2/AC-3，验证脚本记录在汇报中 |
| seed 幂等策略 | 以 admin 存在判定整体跳过，避免重启覆盖用户对菜单的修改；开发期改 seed 需手动删库 |
| jwt secret 默认值 | dev 默认弱值，README 与 config 注释提示生产覆盖 |
| sqlite 冒烟测试与 MySQL 差异 | 测试只覆盖迁移+seed 结构层，真 MySQL 行为由 docker 手工验证 |
