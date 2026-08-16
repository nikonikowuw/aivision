# gin+gorm 后端脚手架 + vue-vben-admin 前端集成

## Goal

创建一个可直接克隆使用的全栈 admin 脚手架：Go 后端（gin + gorm + MySQL）提供认证、RBAC 权限、基础管理模块与操作日志；前端复用 vue-vben-admin v5.7.0 的 `web-antd` 应用，后端下发菜单驱动动态路由，登录即可见完整后台。clone 后「`go run` + `pnpm dev` 即可跑通登录 → 动态菜单 → 受控接口」全链路。

## Background（仓库现状）

- 仓库 `/Users/niko/dev/go/niko-vue-admin`：git 已初始化、零提交；根目录有空的 `app/` 目录与 `ui/` 目录。
- `ui/` 为 vue-vben-admin **v5.7.0** 完整 monorepo（未经裁剪），含 4 个前端应用（web-antd / web-ele / web-naive / web-tdesign）与 `backend-mock`（nitro 模拟后端）。
- 本机环境：Go 1.26.2、Node v24.15.0、pnpm 11.16.0（满足 vben 要求的 Node ≥20 / pnpm ≥9）。

## 已确认的设计决策（全部经用户逐条确认）

| # | 决策 | 结论 |
|---|---|---|
| 1 | MVP 功能边界 | 认证 + 用户 + 角色 + 菜单 + 部门 + **操作日志**；不做字典、文件上传、多租户 |
| 2 | 操作日志 | 只记写操作（POST/PUT/DELETE）；登录/登出并入同一张日志表；gin middleware 全自动采集（路由分组推断模块名，敏感字段脱敏） |
| 3 | 前端 UI 变体 | `apps/web-antd`（Ant Design Vue） |
| 4 | 认证方案 | JWT 双 token；Access 2h + Refresh 7d；**Refresh token 存 MySQL 表**（不引 Redis） |
| 5 | RBAC 模型 | 用户-角色多对多；菜单/按钮两级权限码；接口按权限码鉴权（自研校验器，不引 Casbin） |
| 6 | 数据库 | **MySQL 8 / PostgreSQL 可选**，gorm v2 双驱动（`gorm.io/driver/mysql` / `gorm.io/driver/postgres`），由 `db.driver` 配置项选择，默认 mysql |
| 7 | 部门 | 仅作组织架构（树形、无限层级），`users.dept_id`；**不做数据权限** |
| 8 | 路由模式 | **backend 模式**：菜单表是唯一路由源，`getAllMenusApi` 动态生成路由 |
| 9 | 仓库结构 | 单仓库 monorepo：Go 后端放现有 `app/`；**裁剪 vben**，只留 `web-antd`（删 web-ele/web-naive/web-tdesign/backend-mock 及 workspace 引用） |
| 10 | Go 分层 | 四层 `model / repository / service / api` + 泛型 `BaseRepo[T]`；`internal/` 标准布局，入口 `app/cmd/api/main.go` |
| 11 | 依赖选型 | gin v1.10+、gorm v2、viper（YAML 配置）、zap、`golang-jwt/jwt/v5`、bcrypt、**swaggo/swag**（OpenAPI 文档）、**google/wire**（编译期 DI） |
| 12 | API 契约 | `{code, data, message}` 封装、`code===0` 成功、HTTP 401 触发刷新（**以 vben 前端代码为准**，详见 §API 契约） |
| 13 | 部署 | 开发：vite proxy 反代；生产：前后端分离部署（前端 dist 由 Nginx 托管）；**完整 docker-compose**（MySQL 8 + 后端 + Nginx 前端）一键起 |
| 14 | 测试 | Go 单测覆盖核心逻辑（JWT/bcrypt/菜单树/权限码计算/分页）+ service 层 sqlite in-memory + `httptest` 冒烟（注册/登录/取菜单）；前端以 `pnpm check` 为质量门；**CI 不做** |
| 15 | 细节约定 | 业务表软删除；禁用用户拒绝登录（已发 token 在 refresh 时拦截）；username/role code 唯一索引；操作日志不自动清理；seed 账号 `admin/admin123`；refresh 轮换（换新即 revoke 旧 token）、允许多端登录 |
| 17 | 菜单标题 i18n | **中/繁/英三语**，方案 A：menus 表 `title` 列存 i18n key（如 `routes.system.user`），`/menu/all` 下发 `meta.title=key`，前端 `$t()` 渲染（tabbar/面包屑已有 `$t`，菜单渲染点补）；vben 语言类型扩展 `zh-TW`。**替换原「仅中文、不改 i18n 管道」决策** |
| 18 | 数据库驱动可选 | **MySQL / PostgreSQL 二选一**，`db.driver` 配置项（默认 mysql，环境变量 `APP_DB_DRIVER` 覆盖）；gorm 双驱动；模型 tag 避免 MySQL 专有类型（不用 `type:tinyint`，由 gorm 按 Go 类型自动映射） |
| 16 | OAuth/第三方登录 | **MVP 不做**；认证 service 按可扩展点设计（身份验证与 token 签发解耦，未来加 provider 不动现有认证代码）；前端隐藏未实现的第三方登录图标行 |

## Requirements

### R-1 认证模块

- R-1.1 `POST /auth/login`：username + password 登录，成功返回用户信息 + accessToken，同时通过 httpOnly cookie 下发 refresh token。
- R-1.2 `POST /auth/refresh`：从 `jwt` cookie 读取 refresh token，校验未过期且未 revoke，轮换签发新 access token 与 refresh token（旧的立即 revoke），**响应体为原始 token 字符串**（对齐 vben `doRefreshToken` 取 `resp.data`）。
- R-1.3 `POST /auth/logout`：按 cookie 中的 refresh token revoke，清除 cookie。
- R-1.4 `GET /user/info`：返回当前用户信息（对齐 vben `UserInfo`：userId/username/realName/roles(角色码数组)/avatar/desc/homePath）。
- R-1.5 `GET /auth/codes`：返回当前用户权限码集合 `string[]`。
- R-1.6 密码 bcrypt 存储；登录失败不区分「用户不存在/密码错误」；禁用用户（status=0）拒绝登录。
- R-1.7 除 login/refresh/swagger 外的接口一律要求有效 access token，否则 HTTP 401。
- R-1.8 登录页仅展示账号密码登录；第三方登录图标行（微信/QQ/GitHub/Google/钉钉）隐藏——未实现的能力不对外展示（vben 内置组件均无后端支撑，见决策 16）。

### R-2 RBAC 权限

- R-2.1 用户-角色多对多（`user_roles`）、角色-菜单多对多（`role_menus`）。
- R-2.2 菜单分三级类型：`catalog`（目录，一级导航）/ `menu`（路由页面）/ `button`（页面内操作点）；每项有唯一权限码 `permission`。
- R-2.3 用户权限码 = 其所有角色绑定菜单权限码的去重并集；`/auth/codes` 返回该集合。
- R-2.4 写操作接口按权限码鉴权：中间件校验当前用户权限集合包含路由声明码，否则 HTTP 403（如 `system:user:add`）。
- R-2.5 超级管理员（角色 code=super）拥有全部权限，不参与菜单过滤。

### R-3 业务模块 CRUD（用户/角色/菜单/部门）

- R-3.1 **用户管理**：分页列表（可筛选：关键字/状态/部门）、新增、编辑、删除（软删）、重置密码、分配角色、启停用。创建/编辑时校验 username 唯一。
- R-3.2 **角色管理**：分页列表、新增、编辑、删除（软删）、分配菜单权限（树形勾选，含按钮级）、启停用。code 唯一。
- R-3.3 **菜单管理**：树形列表（catalog/menu/button 全部展示）、新增、编辑、删除（软删，有子节点时拒绝）。
- R-3.4 **部门管理**：树形列表、新增、编辑、删除（软删，有子部门时拒绝）。
- R-3.5 **操作日志**：分页列表（筛选：时间范围/用户名/模块/状态码）、详情（请求参数[脱敏]、响应状态、耗时、IP、UA）；只读，无删除接口。
- R-3.6 列表分页参数 `page` / `pageSize`；分页响应 `data: { items: [...], total: n }`。

### R-4 动态路由（backend 模式）

- R-4.1 `GET /menu/all`：按当前用户权限返回菜单树（catalog + menu，不含 button），结构对齐 vben `RouteRecordStringComponent`（id/name/path/component/type/icon/meta{icon,order,title,affixTab,keepAlive}/children）；`meta.title` 返回 menus.title 列的 i18n key（决策 17）。
- R-4.2 前端 `apps/web-antd/src/preferences.ts` 设 `accessMode: 'backend'`。
- R-4.3 新增页面 = 前端 `views` 下放 .vue 文件 + 菜单表加一行（component 填 `views` 相对路径），不改前端路由代码。

### R-5 操作日志采集

- R-5.1 中间件拦截 POST/PUT/DELETE（登录/登出也记录，module=认证）。
- R-5.2 记录：user_id、username、module（路由分组名）、action（method+路径）、method、path、query、body（JSON，password/token 类字段脱敏为 `***`）、status_code、ip、user_agent、耗时(ms)、created_at。
- R-5.3 日志写入不影响业务响应（异步或忽略写入错误）。

### R-6 数据库

- 8 张表：`users`、`roles`、`menus`、`departments`、`user_roles`、`role_menus`、`refresh_tokens`、`operation_logs`（结构见 design.md）。
- 启动时 gorm AutoMigrate 建表 + seed：`admin/admin123` 超级管理员、超级管理员角色（code=super）、默认菜单树（系统管理 → 用户/角色/菜单/部门/操作日志 + 首页 dashboard）。

### R-7 工程与部署

- R-7.1 Go 代码在 `app/`：`cmd/api/main.go` 入口 + `internal/`（model/repository/service/api/middleware/pkg）+ `configs/config.yaml`；viper 读配置并支持环境变量覆盖（`db.driver`/DB DSN/JWT 密钥）。
- R-7.2 wire 装配依赖：`wire.go` 声明 ProviderSet，`wire_gen.go` 由 `wire` 生成；提供 Makefile（`make wire`、`make dev`、`make build`）。
- R-7.3 swagger 文档：swaggo 注解，`/swagger/index.html`。
- R-7.4 vben 裁剪：仅保留 `apps/web-antd`，删除其余 app 与 `backend-mock`，清理 `pnpm-workspace.yaml`、turbo 等引用，`pnpm install && pnpm build` 通过。
- R-7.5 vite proxy `/api` → Go 后端 `:8000`。
- R-7.6 docker-compose：MySQL 8 + 后端（多阶段构建镜像）+ 前端 Nginx（托管 dist、反代 `/api`）。

## API 契约（以 vben 前端代码为准）

- 成功：HTTP 200 + `{ "code": 0, "data": <payload>, "message": "ok" }`
- 业务失败：HTTP 200 + `{ "code": <业务码>, "data": null, "message": "<原因>" }`
- 认证失败：HTTP 401 + `{ "code": 401, "data": null, "message": "..." }`（触发 vben 自动刷新重试）
- 无权限：HTTP 403
- 前端解包约定（`apps/web-antd/src/api/request.ts`）：`codeField: 'code'`, `dataField: 'data'`, `successCode: 0`，错误展示取 `error ?? message`。
- **例外**：`/auth/refresh` 响应体为**裸 token 字符串**（不用封装），refresh token 走 **httpOnly cookie（名 `jwt`）**，请求带 `withCredentials`。
- 接口前缀统一 `/api`（后端路由组），vite dev 由 proxy 转发（`target: http://localhost:8000/api`，去除 rewrite）。
- 业务错误码集中在后端 `pkg/` 统一定义；前端不做错误码表，直接展示后端 message。

## Out of Scope（明确不做）

- 字典管理、文件上传、多租户（tenant）
- 数据权限 / data_scope（部门仅组织架构；`dept_id` 字段预留演进空间）
- **OAuth / 第三方登录（微信/QQ/GitHub/Google/钉钉扫码等）**——MVP 不做，认证服务预留 provider 扩展点
- Redis、消息队列
- 操作日志自动清理与清空接口
- 强制首次登录改密码
- CI（GitHub Actions / GitLab CI）
- 前端 e2e / Playwright 测试
- Casbin、golang-migrate（AutoMigrate 替代）

## Acceptance Criteria（验收清单）

- [ ] AC-1 `pnpm install && pnpm build`（web-antd 裁剪后）无错误；`pnpm check` 通过。
- [ ] AC-2 `go build ./...`、`go test ./...` 全部通过（sqlite 内存库，无需真数据库）。
- [ ] AC-3 配好数据库（`db.driver` 选 mysql 或 postgres）后 `make dev` 启动，日志显示 AutoMigrate + seed 完成（两驱动各验证一次）。
- [ ] AC-4 `admin/admin123` 登录成功：拿到 accessToken；响应与 `{code:0,data}` 契约一致。
- [ ] AC-5 登录后 `/menu/all` 返回菜单树，前端动态生成路由，左侧菜单可见「系统管理」全部模块并可进入。
- [ ] AC-6 访问无权限接口返回 HTTP 403；`/auth/codes` 返回正确权限码集合。
- [ ] AC-7 access token 过期后（或伪造无效 token）请求返回 401，前端自动 refresh 并重试成功；refresh token 被 revoke 后重登。
- [ ] AC-8 退出登录后 refresh token 失效，旧 access token 到期后无法续期。
- [ ] AC-9 用户/角色/菜单/部门 CRUD 全流程可用：创建 → 列表可见 → 编辑生效 → 删除后消失；username/code 重复返回明确业务错误。
- [ ] AC-10 角色分配菜单后，该角色的用户重新登录，左侧菜单与可访问接口范围随之变化。
- [ ] AC-11 任一写操作（如新增用户）后，操作日志列表出现对应记录，密码字段已脱敏；GET 请求不产生日志。
- [ ] AC-12 禁用某用户后该用户无法登录，已登录用户在 refresh 时被拒绝。
- [ ] AC-13 swagger UI 可访问，列出全部接口。
- [ ] AC-14 `docker compose up -d` 后访问 Nginx 端口可打开完整后台并完成登录。
- [ ] AC-15 部门树增删改查正常，用户可绑定部门并在列表按部门筛选。
- [ ] AC-16 登录页只显示账号密码登录，不显示任何第三方登录图标/入口。

## Open Questions

无阻塞项。（handler 目录命名 `api/` vs `handler/`、日志表索引方案等为实现细节，见 design.md 默认取值。）
