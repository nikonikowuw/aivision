# 人员管理 MVP 实现计划

本计划只在用户审核最新规划摘要并执行 `task.py start` 后实施。当前阶段不得修改业务代码，也不得运行实现阶段构建。

## 1. 规划审核与基线

- [ ] 再读本任务 `prd.md`、`design.md` 和本文件，确认所有行为与范围决策一致。
- [ ] 阅读后端、前端及共享跨层规范；确认现有 camera/user 模块、动态菜单、操作日志和 wire 装配模式。
- [ ] 确认未修改预留 `engine/proto/aivision/v1/person.proto` 和任何 C++ 人员协议。

验证门：任务仍为 `planning`，业务代码没有变更，实施范围只覆盖基础人员管理。

## 2. 数据模型与数据库迁移

- [ ] 新增 `app/internal/model/person.go`，内嵌 `BaseModel`，显式声明 `persons` 表和 `person_id`、`name` 列；`person_id` 使用全局唯一索引，不能因软删除释放。
- [ ] 更新 `app/internal/model/migrate.go`，将 Person 加入 SQLite 测试 `AutoMigrate`，同步更新模型 smoke 测试期望表清单。
- [ ] 新增递增版本的 `app/migrations/000013_add_persons.up.sql` 与 `.down.sql`，创建 PostgreSQL 表、唯一约束和查询索引，不添加外键。
- [ ] 检查 migration 的 up/down、重复执行行为及 snake_case 与 GORM 标签一致性。

验证门：SQLite 模型迁移可创建表；PostgreSQL migration 可逆；软删除记录仍占用 `person_id` 唯一值。

## 3. Repository 与 Service

- [ ] 新增 `app/internal/repository/person.go`，复用 `normalizePage`、`writeError` 和 `ErrNotFound`；提供按 `person_id` 查询、分页、创建、更新、软删除、批量删除和恢复所需的窄接口。
- [ ] 为恢复/页面重复判断提供明确的含软删除查询，普通查询保持 GORM 默认只查活动记录；批量删除仅按 `person_id` 操作，不暴露内部 `id`。
- [ ] 新增 `app/internal/service/person.go`，集中实现 `personId`/姓名规范化、Unicode 长度与控制字符校验、去划线 UUIDv4 生成、页面 create/update/delete/batch 以及开放 upsert/delete。
- [ ] Service 只返回公开 DTO，避免 `model.Person` 的内部 `ID` 进入 JSON；明确处理活动重复、软删除恢复、幂等删除和最后提交覆盖规则。
- [ ] 为 `CodePersonIDTaken = 1018` 增加三语 errno 文案，并把 repository 唯一冲突映射为受控业务错误。

验证门：Service 单元测试覆盖所有标识/姓名边界、恢复、冲突、幂等、批量 0/1/100/101 边界和公开 DTO 无 `id`。

## 4. 固定白名单与配置

- [ ] 在 `app/internal/pkg/config` 增加 `Open.PersonSyncAllowedIPs []string`、yaml 默认值和 `APP_OPEN_PERSON_SYNC_ALLOWED_IPS` 覆盖解析。
- [ ] 启动配置校验全部地址为合法 IPv4/IPv6 或 CIDR；任意非法项返回配置错误并阻止启动；空数组合法且表示默认拒绝。
- [ ] 新增 `app/internal/middleware/open_person.go`（或与现有中间件一致的命名），使用真实 TCP `RemoteAddr` 提取 IP，用 `net/netip` 匹配单地址/CIDR，绝不使用 `X-Forwarded-For`。
- [ ] 白名单不匹配时返回 `CodeForbidden`，中止请求并交给统一错误处理中间件；避免在中间件内拼接响应体。

验证门：中间件测试覆盖 IPv4、IPv6、CIDR、空白配置、非法配置、IPv4-mapped 地址策略和伪造 `X-Forwarded-For`；配置测试覆盖 yaml/env 优先级。

## 5. HTTP Handler、路由、DI 与日志

- [ ] 新增 `app/internal/api/person.go`，只负责绑定请求、调用 Service、返回 `response.Success` 或交由统一错误处理中间件；定义页面批量请求 DTO 和公开响应 DTO。
- [ ] 在 `app/internal/router/router.go` 注册认证页面组 `/api/person` 及 `resource:person`/操作权限；开放 `/api/v1/open/person` 放在 JWT/Perm 路由组之外，只挂白名单中间件。
- [ ] 更新 `router.Deps`、wire provider/set 和生成的 `wire_gen.go`，使用 `make wire` 生成，不手工编辑生成文件。
- [ ] 在操作日志 action 映射中加入人员页面写操作的 i18n key；确认开放写操作继续被现有 `/api` 操作日志中间件采集，内部 `id` 不进入日志业务载荷。
- [ ] 新增/更新 API 与 router 测试，钉住 method/path、认证、权限、403 白名单、统一响应和错误码。

验证门：页面路由必须认证和按权限访问；开放路由不接受无白名单请求，也不被 JWT/页面权限错误地拦截。

## 6. 菜单迁移与前端页面

- [ ] 新增递增版本的资源菜单 data migration（当前应为 `000014_seed_resource_person_menu`），幂等创建/补齐 `ResourcePerson` 页面菜单和 `resource:person`、`resource:person:add/edit/delete` 按钮，并绑定 `super` 角色；不修改既有权限码。
- [ ] 新增 `ui/apps/web-antd/src/api/core/person.ts`，在 `PersonApi` namespace 中定义类型化 DTO 和 page/create/update/delete/batch 函数，统一使用共享 `requestClient`。
- [ ] 在 `api/index.ts`/core 导出人员 API，确认前端只使用 `personId`/`personIds`，不读取内部 `id`。
- [ ] 新增 `ui/apps/web-antd/src/views/resource/person/index.vue`，复用 Vben Form/Vxe Grid 和 camera 页面已有模式：筛选、分页、时间 formatter、多选提示条、批量确认、弹窗新增/编辑、单个删除。
- [ ] 编辑时 `personId` 只读且只提交 `name`；新增时允许空 `personId`；批量选择只保留当前页，最多 100 项。
- [ ] 补齐 `zh-CN`、`en-US`、`zh-TW` 的 `routes.json`、`resource.json` 和操作日志相关文案；操作列设置足够宽度并禁用 overflow 截断。
- [ ] 确认动态菜单返回的页面权限、keepAlive 和重新激活刷新行为符合 PRD；不新增详情页、回收站或静态重复路由。

验证门：页面可完成查询、新增、编辑、单个删除和当前页批量删除；三语切换无裸 key，内部 `id` 不在前端类型或请求中。

## 7. 质量验证

- [ ] `cd app && gofmt -w` 作用于改动 Go 文件，确认 `gofmt -l` 无输出。
- [ ] `cd app && make wire`、`make test`、`make vet`。
- [ ] 若数据库环境可用，执行 `cd app && make migrate-up`、重复迁移并检查 migration version；不可用时记录跳过原因并完成 SQLite 覆盖。
- [ ] `cd ui && pnpm check`，必要时运行 `pnpm test:unit`。
- [ ] 检查 `git diff`，确认没有修改人脸 gRPC、无外键、无内部 ID 泄露、无硬编码用户文案和无第二 request client。
- [ ] 运行 `python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-26-person-management-mvp`，修复任务上下文或格式问题。

## 风险文件与回滚点

- `app/internal/model/migrate.go` 与 `app/migrations/000013_*`：schema/软删除唯一性风险；上线前必须验证 PostgreSQL up/down 和旧数据兼容。
- `app/internal/pkg/config/*`、`app/internal/middleware/*`：白名单默认拒绝与环境变量解析风险；配置错误必须 fail closed。
- `app/internal/router/router.go`、wire 文件：认证组与开放组边界、生成代码漂移风险；通过路由测试和 `make wire` 回归。
- `app/internal/pkg/errno/errno.go`：全局错误码/三语文案同步风险；保持 1018 未与现有码冲突。
- `ui/apps/web-antd/src/views/resource/person/index.vue`：分页选择、内部 ID 泄露和三语操作列宽度风险；按 camera 页面模式检查并运行 `pnpm check`。

## 实施前检查

- [ ] 用户已审核最新规划摘要并明确批准进入实现阶段。
- [ ] `prd.md`、`design.md` 和本文件无阻塞性未决产品问题。
- [ ] 只有后续用户明确批准后，才执行：
  `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-26-person-management-mvp`
