# 实施计划：gin+gorm 后端脚手架 + vue-vben-admin 前端集成

> **执行模式（用户要求）**：按模块逐个实施，绝不一次性全部实现，方便 git 管理。
> 每个子任务：`task.py start` → 实施 → 验证（子任务 AC）→ **等用户手动 commit** → 下一个。
> 父任务不直接实施，只做源需求持有与最终集成验收（deploy 子任务内完成）。

## 子任务树与依赖顺序

```
08-16-backend-skeleton      （无依赖）                       ← 第一个 start
08-16-backend-auth          （依赖 skeleton）
08-16-backend-dept          （依赖 skeleton + auth）          ← 系列第一个业务模块，验证四层范式
08-16-backend-menu          （依赖 skeleton + auth）
08-16-backend-role          （依赖 skeleton + auth + menu）
08-16-backend-user          （依赖 skeleton + auth + dept + role）
08-16-backend-oplog         （依赖 auth + user/role/menu/dept）
08-16-frontend-trim         （无依赖，可与后端并行）
08-16-frontend-integration  （依赖 frontend-trim + backend-auth/menu）
08-16-frontend-pages        （依赖 frontend-integration + 全部后端模块）
08-16-deploy                （依赖全部 10 个）
```

依赖写在各子任务 prd.md「依赖」节，不靠树位置暗示。前端与后端两线可在完成各自前置后并行推进。

## 每个子任务的标准节奏

1. `python3 ./.trellis/scripts/task.py start .trellis/tasks/<dir>` 切到该子任务（实现阶段）
2. 实施（子任务 prd.md 的 Requirements）
3. 验证：跑子任务 AC 清单 + `go test ./...` / `pnpm check` 等门禁
4. 汇报结果，**暂停等待用户手动 commit**（commit 节奏由用户掌控，不代做）
5. 用户确认后进入下一个子任务

## 每阶段实施要点（与阶段 A/B/C 结构一致）

### 1. backend-skeleton
- `app/go.mod`、`cmd/api/main.go`、`configs/config.yaml`、Makefile、`internal/pkg/{config,logger,response,errno}`、`internal/model` 8 表、AutoMigrate+seed、wire
- 验证：`go build ./...`；连 MySQL 启动，8 表 + admin 就位

### 2. backend-auth
- `/auth/login|refresh|logout`、auth 中间件、`/user/info`、`/auth/codes`、认证扩展点设计（验证与签发解耦）
- 验证：curl 登录→访问→刷新→登出全链路；`go test`（登录/轮换/吊销）

### 3. backend-dept
- 四层 CRUD + `/dept/tree` 树查询；路由声明 `system:dept:*` 权限码
- 验证：curl 树形增删改查；有子部门删除返回 1007

### 4. backend-menu
- `/menu/tree`、CRUD、`/menu/all`（按权限过滤出 vben 路由结构，不含 button）
- 验证：curl `/menu/all` 结构与排序；低权限用户过滤正确

### 5. backend-role
- `/role/page`、CRUD、`menu-ids`、`menus` 覆盖式分配；权限码并集打通 `/auth/codes`
- 验证：改角色菜单后 `/auth/codes` 与 `/menu/all` 变化

### 6. backend-user
- `/user/page`（筛选）、CRUD、reset-password、roles 分配、status；username 唯一
- 验证：全流程 curl + 重置密码后可登录 + 禁用后不可登录

### 7. backend-oplog
- oplog 中间件（脱敏、异步）+ perm 中间件 + `/oplog/page`、`/oplog/:id`
- 验证：写操作产生日志、密码脱敏、无权限 403、筛选正确

### 8. frontend-trim
- 删 web-ele/web-naive/web-tdesign/backend-mock，清 workspace 引用与根脚本
- 验证：`pnpm install && pnpm build && pnpm check`
- 风险：最大回滚点，失败按 design.md §10 降级方案

### 9. frontend-integration
- preferences.ts accessMode=backend；vite proxy 去 rewrite；auth.ts 对齐契约；登录页移除 ThirdPartyLogin
- 验证：浏览器 admin 登录成功、菜单由后端驱动、过期自动刷新、无第三方入口

### 10. frontend-pages
- `views/system/{user,role,menu,dept,log}/index.vue`，antd + VxeGrid + vben 弹窗模式，`v-access` 按钮权限
- 验证：五页面全流程手工过一遍（AC-9/10/11/15 前端侧）

### 11. deploy
- `app/Dockerfile`、`deploy/docker-compose.yml`、`deploy/nginx.conf`、根 README
- 验证：`docker compose up -d` 登录成功；README 从零走通；父任务 AC-1~16 全量回归

## 回滚点

- 每个子任务 commit 后即为天然回滚点（用户手动 commit）。
- frontend-trim 前确认 git 已提交（vben 裁剪是最大风险点）。

## 风险文件清单

- `apps/web-antd/src/api/core/auth.ts`、`src/preferences.ts`、`vite.config.ts`（改动最小但契约敏感）
- `pnpm-workspace.yaml`、根 `package.json`（裁剪时易漏引用）
- `app/internal/middleware/perm.go`（权限码声明与 seed 菜单码一致性是 AC-6/10 成败点）
