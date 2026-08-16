# 5 个业务页面 views/system/*

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现用户/角色/菜单/部门/操作日志五个管理页面，与菜单表 seed 的 component 路径一一对应，动态路由可直接加载。

## 依赖

- `08-16-frontend-integration`（登录 + 动态路由已通）。
- 对应后端模块：user/role/menu/dept/oplog 全部完成。

## Requirements

- 新增 `src/views/system/{user,role,menu,dept,log}/index.vue`：
  - user：分页表格 + 筛选（关键字/状态/部门）+ 新增/编辑弹窗（含部门选择、角色分配）+ 重置密码 + 启停用 + 删除
  - role：分页表格 + 新增/编辑 + 菜单权限树形勾选（含按钮级，Tree 带 checkbox）+ 启停用 + 删除
  - menu：树形表格（catalog/menu/button 全展示）+ 新增/编辑 + 删除
  - dept：树形列表 + 新增/编辑 + 删除
  - log：分页表格 + 筛选（时间范围/用户名/模块/状态码）+ 详情弹窗（参数脱敏展示、耗时）
- 组件栈：antd 组件 + `useVbenVxeGrid` 表格 + vben Modal/Drawer 表单模式（对齐 web-antd 现有演示页风格）。
- 页面文案中文；按钮权限用 `v-access`/`useAccess` 按权限码控制显隐。
- 菜单表 seed 的 component 路径与页面文件路径严格一致。

## Acceptance Criteria

- [ ] 父 AC-9：用户/角色/菜单/部门四模块页面全流程可用（增删改查 + 唯一性错误提示 + 树形交互）。
- [ ] 父 AC-10 前端侧：调整角色菜单后，对应用户重新登录，菜单与按钮显隐随之变化。
- [ ] 父 AC-11 前端侧：日志页可查询/筛选/看详情，脱敏字段显示 `***`。
- [ ] 父 AC-15 前端侧：部门树维护 + 用户在列表按部门筛选。
- [ ] 五个页面均可通过左侧菜单直接进入，无控制台报错；`pnpm check` 通过。

## Out of Scope

- 部署物、README（deploy 任务）
