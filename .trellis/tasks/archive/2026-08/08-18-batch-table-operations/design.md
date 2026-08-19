# Design: 批量表格操作 (Batch Table Operations)

## 1. 架构与边界 (Architecture & Boundaries)

遵循项目整体分层架构：
- **前端 (`ui/apps/web-antd`)**：
  - View 层：
    - 用户、角色页面的表格第一列配置 `{ type: 'checkbox', width: 50, align: 'center' }`，并配置 `checkboxConfig.checkMethod` 禁用 `admin` / `super` 等内置关键项。
    - 用户、角色页面监听 `checkboxChange` / `checkboxAll` 事件，维护响应式状态 `selectedRows = ref<Item[]>([])`，并展示选中提示条。
    - 用户、角色页面提供 `clearSelection` 方法：调用 `gridApi.grid?.clearCheckboxRow()` 并重置 `selectedRows.value = []`。
    - 操作日志页面保持只读，仅提供分页列表和详情查看，不配置复选框、选择提示条或删除按钮。
  - API 层 (`src/api/core/{user,role}.ts`)：新增用户和角色批量请求方法；`log.ts` 仅保留分页和详情查询。
  - 国际化 (`src/locales/langs/{zh-CN,en-US,zh-TW}/system.json`)：补充用户和角色批量操作需要的 `selectedCount`、`clearSelection`、`batchDelete`、`batchEnable`、`batchDisable`、`confirmBatchDelete` 等字段。
- **后端 (`app/`)**：
  - API Handler (`internal/api/{user,role}.go`)：解析 JSON 请求体，绑定/校验 `ids`（切片长度 > 0），调用 Service；操作日志 handler 仅提供查询接口。
  - Service (`internal/service/{user,role}.go`)：安全保护规则校验（判断是否包含 ID=1 或受保护 admin 用户/角色），调用 Repository 事务；操作日志 service 仅负责记录和查询。
  - Repository (`internal/repository/{user,role}.go`)：数据库事务批量操作；操作日志 repository 只提供记录和查询，不提供删除方法。
  - Router (`internal/router/router.go`)：注册用户和角色批量路由及权限码映射；操作日志只注册查询路由。

## 2. 数据契约与接口设计 (Contracts & APIs)

### 2.1 用户管理 (User)
1. **批量删除**：
   - 路由：`DELETE /api/user/batch`
   - 权限码：`system:user:delete`
   - Request Body: `{"ids": [2, 3, 4]}`
   - 行为：若包含 `admin` (ID=1 或 username="admin")，返回 `CodeAdminUserProtected` (1015)；事务内软删除 users 表记录，并清理关联的 user_roles 记录。
2. **批量更新状态**：
   - 路由：`PUT /api/user/batch-status`
   - 权限码：`system:user:status`
   - Request Body: `{"ids": [2, 3, 4], "status": 0}` (0=禁用, 1=启用)
   - 行为：若状态为 0 且包含 `admin`，返回 `CodeAdminUserProtected`；事务内批量更新 status 字段。

### 2.2 角色管理 (Role)
1. **批量删除**：
   - 路由：`DELETE /api/role/batch`
   - 权限码：`system:role:delete`
   - Request Body: `{"ids": [2, 3]}`
   - 行为：若包含超级管理员角色 (ID=1 或 code="super"|"admin")，返回 `CodeSuperRoleProtected` (1014)；事务内软删除 roles 表记录，并清理关联的 role_menus 和 user_roles 记录。

### 2.3 操作日志 (Log)
- 仅提供 `GET /api/oplog/page` 和 `GET /api/oplog/:id`。
- 权限码：`system:log`。
- 操作日志为 append-only，禁止提供单条删除、批量删除、清空或修改接口。

## 3. 安全、幂等与兼容性 (Security & Compatibility)
- **操作日志不可变性**：操作日志仅允许追加和查询，任何删除、清空或修改请求都不应注册为 API 路由。
- **权限码契约保持不变**：严格遵循 `.trellis/spec/backend/database-guidelines.md`，不改动 `seedMenuTree` 权限码，复用已有操作级权限码。
