# 技术设计：5 个业务页面 views/system/*

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录）。
> 对应任务：`.trellis/tasks/08-16-frontend-pages/`

## 1. 目标与范围

本设计针对 `ui/apps/web-antd` 下 5 个系统管理页面及配套 API/类型层：
- 用户管理：`src/views/system/user/index.vue`
- 角色管理：`src/views/system/role/index.vue`
- 菜单管理：`src/views/system/menu/index.vue`
- 部门管理：`src/views/system/dept/index.vue`
- 操作日志：`src/views/system/log/index.vue`
- API 接入层：`src/api/core/{user,role,menu,dept,log}.ts`
- 国际化路由多语言 key 支持

## 2. API 层与契约定义

所有 API 调用遵循前端规范 `directory-structure.md` 与 `type-safety.md`，统一使用 `requestClient`，通过 namespace 组织类型与函数。

### 2.1 用户管理 `src/api/core/user.ts`
- `getUserInfoApi()`: `GET /user/info`
- `getUserPageApi(params: UserPageQuery)`: `GET /user/page`
- `createUserApi(data: SaveUserInput)`: `POST /user`
- `updateUserApi(id: number, data: SaveUserInput)`: `PUT /user/:id`
- `deleteUserApi(id: number)`: `DELETE /user/:id`
- `resetUserPasswordApi(id: number)`: `PUT /user/:id/reset-password`
- `getUserRolesApi(id: number)`: `GET /user/:id/roles`
- `assignUserRolesApi(id: number, roleIds: number[])`: `PUT /user/:id/roles`
- `updateUserStatusApi(id: number, status: number)`: `PUT /user/:id/status`

### 2.2 角色管理 `src/api/core/role.ts`
- `getRolePageApi(params: RolePageQuery)`: `GET /role/page`
- `createRoleApi(data: SaveRoleInput)`: `POST /role`
- `updateRoleApi(id: number, data: SaveRoleInput)`: `PUT /role/:id`
- `deleteRoleApi(id: number)`: `DELETE /role/:id`
- `getRoleMenuIdsApi(id: number)`: `GET /role/:id/menu-ids`
- `assignRoleMenusApi(id: number, menuIds: number[])`: `PUT /role/:id/menus`

### 2.3 菜单管理 `src/api/core/menu.ts`
- `getAllMenusApi()`: `GET /menu/all`（用户可用动态路由树）
- `getMenuTreeApi()`: `GET /menu/tree`（全量管理树）
- `createMenuApi(data: SaveMenuInput)`: `POST /menu`
- `updateMenuApi(id: number, data: SaveMenuInput)`: `PUT /menu/:id`
- `deleteMenuApi(id: number)`: `DELETE /menu/:id`

### 2.4 部门管理 `src/api/core/dept.ts`
- `getDeptTreeApi()`: `GET /dept/tree`
- `createDeptApi(data: SaveDeptInput)`: `POST /dept`
- `updateDeptApi(id: number, data: SaveDeptInput)`: `PUT /dept/:id`
- `deleteDeptApi(id: number)`: `DELETE /dept/:id`

### 2.5 操作日志 `src/api/core/log.ts`
- `getLogPageApi(params: LogPageQuery)`: `GET /oplog/page`
- `getLogDetailApi(id: number)`: `GET /oplog/:id`

## 3. UI 架构与组件规范

### 3.1 页面技术栈
- 页面外壳：`@vben/common-ui` 中的 `Page` 组件。
- 列表表格：`#/adapter/vxe-table` 提供的 `useVbenVxeGrid` + `VbenVxeGrid`（已接入 `@vben/plugins/vxe-table`）。
  - 分页模式：开启 `proxyConfig.ajax.query` 与 `pagerConfig`。
  - 树形表格模式（菜单/部门）：配置 `treeConfig: { transform: false, rowField: 'id', parentField: 'parentId', childrenField: 'children' }`。
- 弹窗与抽屉表单：
  - 基于 `@vben/common-ui` 的 `useVbenModal` 抽屉/模态窗。
  - 基于 `#/adapter/form` 的 `useVbenForm` 声明式表单与 Zod 校验。
- 按钮级权限控制：
  - 使用 `v-access:code="['system:user:add']"` 或 `useAccess().hasAccessByCodes` 判定操作列按钮显隐。

### 3.2 各页面核心交互设计

1. **用户管理 (`views/system/user/index.vue`)**
   - 布局：左侧可选部门树快速筛选（或表单顶层部门下拉级联）+ 右侧用户主表格。
   - 工具栏：查询表单（用户名、昵称、状态、部门）、新增用户按钮。
   - 操作列：编辑、重置密码（二次确认弹窗）、分配角色（Modal 勾选角色列表）、启停用（Switch 或 Popconfirm）、删除。
   - 新增/编辑 Modal：表单含用户名、昵称、密码（仅新增必填）、邮箱、手机号、部门（TreeSelect）、状态、备注。

2. **角色管理 (`views/system/role/index.vue`)**
   - 表格：角色名称、角色编码、状态、排序、备注、创建时间。
   - 操作列：编辑、分配菜单权限、删除。
   - 分配菜单 Modal：展示全量菜单树（`Tree` 组件，带 checkbox），调用 `getRoleMenuIdsApi` 回显勾选，提交时调用 `assignRoleMenusApi`。

3. **菜单管理 (`views/system/menu/index.vue`)**
   - 树形表格：展示全量菜单（目录/菜单/按钮），列包括：菜单名称 (title / i18n)、图标 (icon)、类型 (catalog/menu/button 标签)、路由地址 (path)、组件路径 (component)、权限标识 (permission)、排序、状态。
   - 操作列：新增子项、编辑、删除。
   - 新增/编辑 Modal：类型单选（目录/菜单/按钮）动态切换表单项；上级菜单 TreeSelect；组件路径、权限码输入等。

4. **部门管理 (`views/system/dept/index.vue`)**
   - 树形表格：部门名称、负责人、联系电话、排序、状态、创建时间。
   - 操作列：新增子部门、编辑、删除。
   - 新增/编辑 Modal：上级部门 TreeSelect、部门名称、负责人、电话、排序、状态。

5. **操作日志 (`views/system/log/index.vue`)**
   - 查询表单：操作人 (username)、模块 (module)、状态码 (statusCode)、时间范围 (RangePicker)。
   - 表格：ID、用户名、模块、操作动作、请求方式 (Tag)、请求路径、响应状态 (Tag 成功/失败)、耗时 (ms 格式化)、操作时间。
   - 操作列：查看详情。
   - 详情 Drawer / Modal：格式化展示请求 Query、请求 Body（敏感字段已被后端脱敏为 `***`）、User-Agent、客户端 IP、执行耗时等。

## 4. 路由与多语言支持

- 动态路由匹配：后端 seed 数据中菜单 component 分别为：
  - `/system/user/index` -> `views/system/user/index.vue`
  - `/system/role/index` -> `views/system/role/index.vue`
  - `/system/menu/index` -> `views/system/menu/index.vue`
  - `/system/dept/index` -> `views/system/dept/index.vue`
  - `/system/log/index` -> `views/system/log/index.vue`
- `router/access.ts` 动态 `import.meta.glob('../views/**/*.vue')` 完美解析上述组件路径。
- 在 `locales/langs/{zh-CN,en-US}/page.json` 中补齐 `routes.system.*` 的国际化翻译，确保菜单栏与面包屑正确显示标题。

## 5. 质量与兼容保证

- 严禁使用松散 `any`，全面定义入参与返回结构。
- 遵循 Vue 3 + Setup + TypeScript 标准，通过 `pnpm check`（包含 circular, dep, typecheck, cspell）。
