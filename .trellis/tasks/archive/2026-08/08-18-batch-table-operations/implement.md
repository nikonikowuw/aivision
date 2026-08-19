# Implementation Plan: 批量表格操作 (Batch Table Operations)

## 1. 实施检查清单 (Checklist)

### 阶段 1: 后端数据访问与业务逻辑层 (Backend Repositories & Services)
- [x] 1.1 `app/internal/repository/user.go`：新增 `BatchDelete(ctx, ids []uint64)` 与 `BatchUpdateStatus(ctx, ids []uint64, status int8)`
- [x] 1.2 `app/internal/service/user.go`：新增 `BatchDelete(ctx, ids []uint64)` 与 `BatchUpdateStatus(ctx, ids []uint64, status int8)`，并校验 admin 保护
- [x] 1.3 `app/internal/repository/role.go`：新增 `BatchDelete(ctx, ids []uint64)`
- [x] 1.4 `app/internal/service/role.go`：新增 `BatchDelete(ctx, ids []uint64)`，校验超级管理员角色保护
- [x] 1.5 操作日志 repository/service 保持 append-only，仅提供记录和查询，不提供删除方法

### 阶段 2: 后端 API 与路由层 (Backend APIs & Routes)
- [x] 2.1 `app/internal/api/user.go`：新增 `BatchDelete` 与 `BatchUpdateStatus` handler 及请求体结构
- [x] 2.2 `app/internal/api/role.go`：新增 `BatchDelete` handler 及请求体结构
- [x] 2.3 `app/internal/api/operation_log.go`：仅提供分页和详情查询 handler，不提供删除 handler
- [x] 2.4 `app/internal/router/router.go`：注册路由与权限码绑定：
  - `DELETE /api/user/batch` -> `system:user:delete`
  - `PUT /api/user/batch-status` -> `system:user:status`
  - `DELETE /api/role/batch` -> `system:role:delete`
  - 操作日志仅注册 `GET /api/oplog/page` 和 `GET /api/oplog/:id` 查询路由

### 阶段 3: 后端单元测试 (Backend Tests)
- [x] 3.1 编写/更新 `app/internal/service/*_test.go` 和 `app/internal/api/*_test.go`，覆盖正常批量与保护项拦截
- [x] 3.2 运行 `cd app && go test ./...` 确保全部通过

### 阶段 4: 前端 API、国际化与页面改造 (Frontend APIs, I18n & Views)
- [x] 4.1 国际化：更新 `ui/apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/system.json` 补充批量操作相关文案 (`selectedCount`, `clearSelection`, `batchDelete`, `batchEnable`, `batchDisable`, `confirmBatchDelete`)
- [x] 4.2 前端 API：更新 `ui/apps/web-antd/src/api/core/{user,role}.ts` 添加批量 API；操作日志 API 仅保留分页和详情查询
- [x] 4.3 用户管理页面：`ui/apps/web-antd/src/views/system/user/index.vue`
  - 列配置加入 checkbox，设置 `checkMethod` 禁用 admin
  - 勾选后展示提示条（已选 X 项、清空、批量启用、批量禁用、批量删除）
  - 绑定勾选与重置事件
- [x] 4.4 角色管理页面：`ui/apps/web-antd/src/views/system/role/index.vue`
  - 列配置加入 checkbox，设置 `checkMethod` 禁用 super 角色
  - 勾选后展示提示条（已选 X 项、清空、批量删除）
- [x] 4.5 操作日志页面：`ui/apps/web-antd/src/views/system/log/index.vue`
  - 保持只读分页列表和详情查看
  - 不提供复选框、选择提示条、单条删除或批量删除

### 阶段 5: 全面质量验证 (Verification & Quality Gates)
- [x] 5.1 后端 `cd app && make test && make vet`
- [x] 5.2 前端 `cd ui && pnpm check` (circular, dep, typecheck, cspell)
- [x] 5.3 检查 Git diff 确保无多余变更

## 2. 验证命令 (Validation Commands)
```bash
# 后端
cd app && go test -v ./internal/service/... ./internal/api/...
cd app && go vet ./...

# 前端
cd ui && pnpm check
```
