# Design: 批量表格操作 (Batch Table Operations)

## 1. 架构与边界 (Architecture & Boundaries)

遵循项目整体分层架构：
- **前端 (`ui/apps/web-antd`)**：
  - View 层 (`src/views/system/{user,role,log}/index.vue`)：
    - 表格第一列配置 `{ type: 'checkbox', width: 50, align: 'center' }`，配置 `checkboxConfig.checkMethod`（禁用 `admin` / `super` 等内置关键项）。
    - 监听 `checkboxChange` / `checkboxAll` 事件，维护响应式状态 `selectedRows = ref<Item[]>([])`。
    - 在 `<Grid>` 之上或工具栏顶部展示 Alert / 提示条：
      ```html
      <div v-if="selectedRows.length > 0" class="selection-alert-bar mb-2 flex items-center justify-between rounded-md bg-primary/10 px-4 py-2 text-sm">
        <div class="flex items-center gap-2">
          <span>{{ $t('system.common.selectedCount', { count: selectedRows.length }) }}</span>
          <Button type="link" size="small" @click="clearSelection">{{ $t('system.common.clearSelection') }}</Button>
        </div>
        <div class="flex items-center gap-2">
          <!-- 模块批量操作按钮 -->
        </div>
      </div>
      ```
    - 提供 `clearSelection` 方法：调用 `gridApi.grid?.clearCheckboxRow()` 并重置 `selectedRows.value = []`。
    - 操作完成（或分页切换、重新加载）后，同步刷新数据并重置选中状态。
  - API 层 (`src/api/core/{user,role,log}.ts`)：新增对应的批量请求方法。
  - 国际化 (`src/locales/langs/{zh-CN,en-US,zh-TW}/system.json`)：补充 `selectedCount`、`clearSelection`、`batchDelete`、`batchEnable`、`batchDisable`、`confirmBatchDelete` 等字段。
- **后端 (`app/`)**：
  - API Handler (`internal/api/{user,role,operation_log}.go`)：解析 JSON 请求体，绑定/校验 `ids`（切片长度 > 0），调用 Service。
  - Service (`internal/service/{user,role,operation_log}.go`)：安全保护规则校验（判断是否包含 ID=1 或受保护 admin 用户/角色），调用 Repository 事务。
  - Repository (`internal/repository/{user,role,operation_log}.go`)：数据库事务批量操作。
  - Router (`internal/router/router.go`)：注册批量路由及权限码映射。

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
1. **单条删除**：
   - 路由：`DELETE /api/oplog/:id`
   - 权限码：`system:log`
   - 行为：物理删除 operation_logs 记录。
2. **批量删除**：
   - 路由：`DELETE /api/oplog/batch`
   - 权限码：`system:log`
   - Request Body: `{"ids": [101, 102]}`
   - 行为：物理删除匹配 ID 的日志记录。

## 3. 安全、幂等与兼容性 (Security & Compatibility)
- **Gin 路由与 Body 支持**：Gin 的 `c.ShouldBindJSON` 支持 `DELETE` 请求携带 JSON Body。
- **权限码契约保持不变**：严格遵循 `.trellis/spec/backend/database-guidelines.md`，不改动 `seedMenuTree` 权限码，复用已有操作级权限码。
