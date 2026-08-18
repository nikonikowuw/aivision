# PRD: 批量表格操作 (Batch Table Operations)

## 1. 目标与用户价值 (Goal & User Value)
为系统管理中的核心表格列表（用户管理、角色管理、操作日志）提供表格多选与【选中激活提示条 (Selection Alert Bar)】批量操作功能，提升管理员批量运维与数据管理的效率与交互体验。

## 2. 已确认事实 (Confirmed Facts)
- **前端表格体系**：系统管理页面均使用 `@vben/plugins/vxe-table` (`useVbenVxeGrid`)，原生支持多选配置（`checkboxConfig`）和插槽自定义。
- **系统管理表格现状**：
  1. 用户管理 (`/system/user`)：有分页，包含状态切换、重置密码、角色分配、编辑、删除。
  2. 角色管理 (`/system/role`)：有分页，包含编辑、权限分配、删除。
  3. 操作日志 (`/system/log`)：有分页，只读详情。
  4. 部门管理 (`/system/dept`) / 菜单管理 (`/system/menu`)：树形表格，层级关系严格，单项操作为主。
- **安全与保护机制**：
  - 超级管理员 `admin`（ID=1 或 username='admin'）受系统保护，不可删除、不可禁用。
  - 内置超级管理员角色 `super` / `admin`（ID=1）受系统保护，不可删除。

## 3. 需求范围与交互设计 (Scope & UX Design)

### 3.1 交互范式：选中激活提示条 (Selection Alert Bar)
- **初始未勾选状态**：
  - 表格第一列为复选框（Checkbox）。
  - 顶部工具栏仅保留基础操作（如“新增”），界面极简清爽。
  - 批量操作区域完全隐藏，不占用常驻空间，无无效置灰按钮。
- **勾选 1 项及以上时**：
  - 表格上方或工具栏顶部以平滑过渡展开提示条：
    - 左侧显示：`已选择 X 项` 与 `【清空】` 操作链接。
    - 右侧显示对应模块支持的批量操作按钮（带气泡二次确认 Popconfirm）。
- **受保护项控制**：
  - 用户管理中 `admin` 行的复选框禁用勾选。
  - 角色管理中 `super` / `admin` 内置角色的复选框禁用勾选。

### 3.2 模块功能范围 (In Scope)
1. **用户管理 (`/system/user`)**：
   - 表格支持多选，`admin` 账号禁用勾选。
   - 提示条提供：【批量启用】、【批量禁用】（需 `system:user:status` 权限）与【批量删除】（带 Popconfirm，需 `system:user:delete` 权限）。
   - 包含【清空】已选按钮。
2. **角色管理 (`/system/role`)**：
   - 表格支持多选，内置超级管理员角色禁用勾选。
   - 提示条提供：【批量删除】（带 Popconfirm，需 `system:role:delete` 权限）与【清空】。
3. **操作日志 (`/system/log`)**：
   - 表格支持多选。
   - 提示条提供：【批量删除】（带 Popconfirm，需 `system:log` 权限）与【清空】。
   - 表格操作列补充单条【删除】按钮（带 Popconfirm）。
4. **后端接口与事务**：
   - 用户批量删除：`DELETE /api/user/batch`，入参 `{"ids": [1, 2, 3]}`，事务内软删除用户及其角色绑定，严格拦截含 `admin` 的删除。
   - 用户批量修改状态：`PUT /api/user/batch-status`，入参 `{"ids": [1, 2, 3], "status": 1|0}`，事务内更新状态，严格拦截含 `admin` 且尝试禁用的操作。
   - 角色批量删除：`DELETE /api/role/batch`，入参 `{"ids": [1, 2, 3]}`，事务内软删除角色及其关联，严格拦截含内置超级管理员角色的删除。
   - 日志单条与批量删除：
     - `DELETE /api/oplog/:id`：删除单条日志。
     - `DELETE /api/oplog/batch`：入参 `{"ids": [1, 2, 3]}`，物理批量删除日志。
5. **国际化与体验**：
   - 补充批量操作相关 i18n 词条（zh-CN, en-US, zh-TW）：`batchDelete`, `batchEnable`, `batchDisable`, `confirmBatchDelete`, `selectedCount`, `clearSelection` 等。
   - 批量操作成功后提示并刷新当前表格数据，自动清空选中项。

### 3.3 非本期范围 (Out of Scope)
- 部门管理和菜单管理的批量操作（层级与树依赖较重，保持单项操作）。
- 操作日志的按时间段一键清理（日志清理一般为定时运维任务，本次聚焦表格选中批量删除）。

## 4. 验收标准 (Acceptance Criteria)
- [ ] **AC-1 用户管理批量操作**：勾选用户后展示提示条，支持批量删除、批量启用、批量禁用；`admin` 用户复选框禁用；点击“清空”可重置选择。
- [ ] **AC-2 角色管理批量删除**：勾选角色后展示提示条，支持批量删除；内置超级管理员角色复选框禁用；点击“清空”可重置选择。
- [ ] **AC-3 操作日志单删与批量删除**：日志列表支持单条删除；勾选后展示提示条支持批量删除；点击“清空”可重置选择。
- [ ] **AC-4 后端安全与边界防护**：后端接口对 `ids` 空数组、非法参数进行校验；若请求包含受保护的 admin 用户/角色，返回明确的业务错误码（`CodeAdminUserProtected` / `CodeSuperRoleProtected`）且事务回滚。
- [ ] **AC-5 权限控制一致性**：批量接口复用现有的权限码声明（用户删除 `system:user:delete`、用户状态 `system:user:status`、角色删除 `system:role:delete`、日志 `system:log`），无需新增权限码。
- [ ] **AC-6 质量保证**：后端 Go 测试覆盖批量 service/repo/api 及保护逻辑；前端通过 `pnpm check`（无类型错误、无循环依赖）。
