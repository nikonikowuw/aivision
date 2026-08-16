# 技术设计：gin+gorm 后端脚手架 + vue-vben-admin 前端集成

> 本文是 `prd.md` 的技术落地设计。所有「已确认决策」以 prd.md 为准，这里只写架构、边界、数据流、契约与权衡。

## 1. 总体架构与目录

```
niko-vue-admin/
├── app/                          # Go 后端（标准 project-layout）
│   ├── cmd/api/
│   │   ├── main.go               # 入口：viper 读配置 → gorm 连库 → AutoMigrate+seed → wire 装配 → 路由 → 启动
│   │   ├── wire.go               # DI 声明（//go:build wireinject）
│   │   └── wire_gen.go           # wire 生成
│   ├── internal/
│   │   ├── model/                # gorm 模型（8 张表）+ 树构建/脱敏等纯逻辑（可单测）
│   │   ├── repository/           # BaseRepo[T] 泛型 + 各实体 repo（个性化查询、事务）
│   │   ├── service/              # 业务逻辑：登录/token/权限码/菜单树组装/CRUD 编排
│   │   ├── api/                  # handler：绑定/校验/调 service/统一响应；swaggo 注解
│   │   ├── middleware/           # auth（JWT 校验）、perm（权限码）、oplog（操作日志）、cors、recovery
│   │   ├── router/               # 路由注册：分组、权限码声明、swagger 挂载
│   │   └── pkg/                  # response（统一封装）、errno（错误码表）、jwt、config、logger
│   ├── configs/config.yaml
│   ├── Makefile                  # wire/dev/build/test 快捷入口
│   ├── Dockerfile                # 多阶段构建
│   ├── go.mod
│   └── testdata/sqlite 等测试辅助
├── ui/                           # vben 裁剪后：仅 apps/web-antd + packages + 基础设施
│   └── apps/web-antd/            # 业务视图 views/system/{user,role,menu,dept,log}
├── deploy/
│   ├── docker-compose.yml        # 数据库(mysql/postgres) + server + web(nginx)
│   └── nginx.conf
└── README.md                     # clone → 启动全流程说明
```

### 分层与依赖方向

```
router → api → service → repository → model
                ↘ middleware（auth/perm 查用户权限，用 service 或 repository 只读接口）
```

- **model**：纯 struct + gorm tag + 树构建/排序/脱敏纯函数；不 import 上层。
- **repository**：`BaseRepo[T any]` 泛型封装 `Create/Update/Delete(软删)/GetByID/Page/Count`，各实体 repo 组合 `BaseRepo[T]` 并补充个性化查询；**接口化**（`UserRepository interface`），供 service 单测 mock。
- **service**：业务编排；持有 repo 接口 + 配置（JWT 密钥等）。事务用 `db.Transaction`，由 repo 暴露 `WithTx` 或直接传 `*gorm.DB` 的场景由具体 repo 方法签名决定。
- **认证扩展点（决策 16）**：`AuthService` 内「身份验证」（密码校验，返回 `*User`）与「签发 token」（签发 access/refresh、落库、写 cookie）分离为两个职责边界，未来接入 OAuth 时新增 provider 验证逻辑即可复用签发链路，不改动现有登录代码。
- **api**：gin handler；只做绑定、校验、调用、返回；不直接接触 gorm。
- wire 装配链：`config → db → repositories → services → handlers → router → *gin.Engine`。

## 2. 数据库设计

通用列：`id`（bigint 自增）、`created_at`、`updated_at`、`deleted_at`（gorm soft delete，仅 users/roles/menus/departments 启用）。

> **方言兼容（决策 18）**：MySQL / PostgreSQL 双驱动由 `db.driver` 选择。模型 gorm tag **不写 MySQL 专有类型**（如 `type:tinyint`——Postgres 无此类型），status 等 `int8` 字段由 gorm 自动映射（MySQL→tinyint，Postgres→smallint）。`varchar(n)`/`text`/`bool` 两库通用；自增主键 gorm 按驱动自动处理。

### users
`username`(varchar64, unique, not null) · `password`(varchar255, bcrypt) · `nickname`(varchar64) · `email`(varchar128) · `phone`(varchar32) · `avatar`(varchar255) · `dept_id`(bigint, index) · `status`(tinyint, 1启用/0禁用) · `remark`(varchar255)

### roles
`name`(varchar64) · `code`(varchar64, unique) · `status`(tinyint) · `sort`(int) · `remark`

### menus
`parent_id`(bigint, 0=根) · `type`(varchar16: catalog/menu/button) · `name`(varchar64, 路由标识符, 保持 ASCII) · `title`(varchar128, **i18n key**, 如 `routes.system.user`; 决策 17) · `path`(varchar128) · `component`(varchar255, 视图相对路径) · `icon`(varchar64) · `sort`(int) · `status`(tinyint) · `permission`(varchar128, 权限码, catalog 可为空) · `affix`(bool) · `keep_alive`(bool) · `home_path`(varchar128, 默认空)

### departments
`parent_id`(bigint) · `name`(varchar64) · `sort`(int) · `leader`(varchar64) · `phone`(varchar32) · `status`(tinyint)

### user_roles / role_menus
`user_id`+`role_id` 联合唯一 / `role_id`+`menu_id` 联合唯一

### refresh_tokens
`user_id`(bigint, index) · `token`(varchar512, unique) · `expires_at`(datetime, index) · `revoked`(bool) · `user_agent` · `ip` —— 启动时惰性删除过期记录（seed 阶段顺带清一次，不做定时任务）。

### operation_logs
`user_id`(bigint, nullable) · `username`(varchar64) · `module`(varchar64) · `action`(varchar64) · `method` · `path`(varchar255) · `query`(text) · `body`(text, 已脱敏 JSON) · `status_code`(int) · `duration_ms`(int) · `ip`(varchar64) · `user_agent`(varchar255) · `created_at`(datetime, 索引，用于时间范围筛选)
索引：`(created_at)`、`(username)`、`(module)`、`(status_code)`；不建全文索引。

## 3. 认证与 token 流转

- Access token：JWT HS256，claims `{sub: userID, username, exp: 2h}`，密钥来自配置 `JWT_SECRET`。
- Refresh token：**不透明随机串**（32B hex），**本身不是 JWT**——存 `refresh_tokens` 表，通过 **httpOnly cookie（名 `jwt`）** 下发/读取（对齐 vben mock 契约）。7 天过期。
- 流转：
  1. login：校验密码 → 签发 access → 生成 refresh 落库 → `Set-Cookie: jwt=<refresh>; HttpOnly; Path=/; Max-Age=604800`（dev 环境 `SameSite=Lax`，非 secure；生产 nginx 反代同域，无需跨域 cookie）
  2. 前端请求带 `Authorization: Bearer <access>`（vben 已实现）
  3. access 过期 → 后端 401 → vben 拦截器调 `/auth/refresh`（带 cookie）→ 后端校验 refresh 有效未 revoke → revoke 旧 refresh → 签发新 access + 新 refresh 并重置 cookie → 返回**裸 token 字符串** → 前端重试原请求
  4. logout：revoke 当前 refresh，清 cookie
- 禁用在 login 与 refresh 两处拦截；已签发的 access 最迟 2h 自然失效（决策 15）。

## 4. RBAC 数据流

1. login/refresh 后前端调 `/user/info`、`/auth/codes`、`/menu/all`（vben authStore 顺序已内置）。
2. 权限码集合：`users → user_roles → roles(status=1) → role_menus → menus(status=1, permission≠'')` 去重并集；角色 code 列表作为 `UserInfo.roles` 返回（vben `BasicUserInfo.roles: string[]`）。
3. `GET /menu/all`：取权限码对应的 menus，剔除 button，按 `parent_id` 递归建树、`sort` 升序，映射为 vben 路由结构：
   ```json
   { "id":1, "name":"System", "path":"/system", "component":"BasicLayout",
     "type":"catalog", "icon":"...",
     "meta":{"icon":"...","order":1,"title":"routes.system.system"},
     "children":[{ "id":101,"pid":1,"name":"User","path":"user","component":"/system/user/index",
       "type":"menu","meta":{"title":"routes.system.user","keepAlive":true} }] }
   ```
   `meta.title` 直接取 menus.title 列的 i18n key（决策 17），不做任何翻译；catalog 挂 `BasicLayout`，顶层 catalog 即一级路由。`name` 为路由标识符（ASCII，seed 中 `System`/`User`/`dashboard` 等），与展示文案解耦。
4. 超级管理员：roles 含 `super` 时返回全量菜单、跳过过滤，`/auth/codes` 返回 `["*"]`；perm 中间件遇 `*` 直接放行。
5. 接口权限码声明：路由分组时在 gin route 上挂元数据（如 `system:user:add`），perm 中间件从 c.FullPath 查声明码并比对集合；无声明码的写接口默认拒绝，无声明码的读接口仅要求登录（防御性收紧由 seed 菜单权限码与路由声明一致来保证）。

## 5. 操作日志中间件

- 位置：auth 之后、业务之前；仅 `POST/PUT/DELETE` 且 path 前缀 `/api`。
- 采集 body：读取 `c.Request.Body` 后重置（`io.NopCloser` 恢复）；按字段名黑名单脱敏（password/oldPassword/newPassword/token/secret）后 JSON 序列化截断（≤4KB）。
- module 推断：由 `c.FullPath` 的第一段（如 `/api/system/user` → `system:user`；`/auth/*` → `auth`）。
- 落库：goroutine 异步写（带 recover），写失败仅记 zap warn，不影响业务。
- 登录失败也记录（此时 user_id 为空、username 取请求体中的 username）。

## 6. API 契约与响应封装

```go
// pkg/response
type Body struct { Code int `json:"code"`; Data any `json:"data"`; Message string `json:"message"` }
```
- 成功 `{code:0, data, message:"ok"}`；失败 `{code:<errno>, data:null, message}`；HTTP 状态：业务失败 200，认证失败 401，权限不足 403，参数错误 400。
- 分页：请求 `page`/`pageSize`（默认 1/20，上限 100），响应 `data:{items:[], total:n}`。
- 错误码（errno 集中定义，`pkg/errno`）：`0 成功`、`401 未认证/token 失效`、`403 无权限`、`1001 用户名或密码错误`、`1002 用户不存在`、`1003 用户名已存在`、`1004 角色 code 已存在`、`1005 旧密码错误`、`1006 菜单存在子节点`、`1007 部门存在子部门`、`1008 用户被禁用` 等；参数错误码 `400x` 统一 `message` 由 validator 生成。
- `/auth/refresh` 例外：HTTP 200 + 裸 token 字符串（`Content-Type: text/plain`，或 JSON 字符串均可——vben `doRefreshToken` 直接取 `resp.data`，实现时以能稳定解出字符串为准，推荐 `data` 字段直接放字符串并跳过 `defaultResponseInterceptor` 的封装路径，即该接口不套 Body）。

## 7. 前端改动清单（vben 裁剪后）

1. `apps/web-antd/src/preferences.ts`：`accessMode: 'backend'`。
2. `vite.config.ts`：proxy `/api` → `http://localhost:8000/api`，**删除 rewrite**（后端路由自带 `/api` 前缀）。
3. `src/api/core/auth.ts`：保持 4 个函数签名不动，`refreshTokenApi` 返回 `{data: string}` 与后端裸字符串兼容；`getAccessCodesApi` 指向 `/auth/codes`。
4. `src/api/core/menu.ts`：`getAllMenusApi` 已指向 `/menu/all`，不动。
5. `src/api/core/user.ts`：`getUserInfoApi` 已指向 `/user/info`，不动。
6. 新增 `src/views/system/{user,role,menu,dept,log}/index.vue`（antd 组件 + `useVbenVxeGrid` 表格 + 弹窗表单），与菜单表 component 字段一一对应。
7. 菜单标题 i18n（决策 17，替换原「title 直接中文」）：
   - `packages/locales/src/typing.ts`：`SupportedLanguagesType` 扩展 `'zh-TW'`；`apps/web-antd/src/locales/index.ts` 补 antd/dayjs 的 zh_TW 映射。
   - `apps/web-antd/src/locales/langs/{zh-CN,zh-TW,en-US}/`：新增 `routes.json`（或复用 page.json）定义菜单标题三语 key（`routes.system.user` 等），与 seed `menus.title` 一一对应。
   - 菜单渲染点补 `$t`：`packages/@core/ui-kit/menu-ui/src/sub-menu.vue`（2 处 `{{ menu.name }}`）与 `components/normal-menu/normal-menu.vue`（1 处）；面包屑（`breadcrumb.vue:45`）与 tabbar（`use-tabbar.ts:93`）已有 `$t`，不需改。
   - 切换语言响应式生效（`i18n.global.locale.value`），无需重拉菜单。
8. 登录页隐藏第三方登录入口：vben 内置的 `third-party-login.vue` 图标按钮（微信/QQ/GitHub/Google）与 `DingdingLogin` 均无后端支撑（backend-mock 无任何 oauth 端点），在 `apps/web-antd/src/views/_core/authentication/login.vue` 中移除 `ThirdPartyLogin` 引用（决策 16，验收 AC-16）。
9. 删除 `backend-mock` 后需同步清理：`pnpm-workspace.yaml` 引用、根 `package.json` 的 `build:mock`/相关脚本、`.vscode/vben-admin.code-workspace` 引用、docs 中 mock 引用（尽力而为，以 `pnpm install && pnpm build && pnpm check` 通过为验收）。

## 8. 配置与部署

- `configs/config.yaml`：`server.port=8000`、`db.{host,port,user,password,name}`、`jwt.{secret,access_ttl=2h,refresh_ttl=168h}`、`log.level`；环境变量 `APP_*` 覆盖（viper `AutomaticEnv` + 前缀）。
- Dockerfile：`golang:1.26-alpine` 构建 → `alpine` 运行（CGO_ENABLED=0）。
- docker-compose：数据库服务（`mysql:8` 或 `postgres:16`，二选一，通过 compose 变量/注释切换，初始化库 + 账号 + healthcheck）、`server`（依赖数据库 healthcheck）、`web`（nginx:alpine，挂载构建好的 dist + nginx.conf 反代 `/api` → server:8000）。

## 9. 测试策略落地

- `model`：菜单树构建、权限码并集、脱敏、分页 —— 纯单测。
- `service`：用 `gorm sqlite in-memory`（`:memory:` + 外键关闭）跑 repo+service 集成测：用户 CRUD、登录校验、refresh 轮换、菜单过滤。注意：SQL 用两库通用写法（避免 MySQL/Postgres 方言差异），模型 tag 见 §2 方言兼容。
- `api`：`httptest` 冒烟——login 200 与 1001、无 token 访问 401、`/menu/all` 结构。
- 前端：`pnpm check`（type + eslint + 依赖检查）。

## 10. 风险与权衡记录

| 风险/权衡 | 结论与理由 |
|---|---|
| 响应字段 `data` 而非 `result` | 以 vben 前端 `defaultResponseInterceptor`（`dataField:'data'`）为准，牺牲口头约定的 `result` |
| refresh 走 cookie 而非请求体 | 对齐 vben `withCredentials` 契约；代价是 dev 环境 cookie 无 secure 标记（localhost 可接受） |
| 轮换 refresh 存库 | 每次 refresh 一条 insert + update，量级极小；换来吊销能力 |
| 无 Redis | refresh 校验命中数据库索引（MySQL/Postgres 均可），脚手架阶段 QPS 足够 |
| 日志异步 goroutine | 极端情况下丢失少量日志换取业务零侵入 |
| seed 后 admin 密码固定 | 决策 15：不强制改密；README 提示生产自行修改 |
| 裁剪 vben 的风险 | 官方支持单应用形态；若裁剪引发连锁构建问题，降级方案为保留 monorepo 全量仅改 web-antd |
