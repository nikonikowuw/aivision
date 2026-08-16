# 角色 CRUD + 分配菜单权限

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现角色管理模块：CRUD + 树形分配菜单权限（含按钮级）。本任务结束后角色-菜单关系可维护，权限码并集计算全链路打通。

## 依赖

- `08-16-backend-skeleton`、`08-16-backend-auth`。
- `08-16-backend-menu`（菜单树查询、role_menus 关联写入的菜单校验）。

## Requirements

- R-3.2：`GET /role/page`（分页）、`POST /role`、`PUT /role/:id`、`DELETE /role/:id`（软删）、`GET /role/:id/menu-ids`（已分配菜单 id 集）、`PUT /role/:id/menus`（覆盖式分配，含 button 节点）、启停用。code 唯一（errno 1004）。
- 用户权限码 = 其所有启用角色绑定启用菜单（permission≠''）的去重并集；`/auth/codes` 使用该计算（与 backend-auth 的查询打通）。
- super 角色不可删除、不可停用。

## Acceptance Criteria

- [ ] 父 AC-9 角色部分：创建 → 列表可见 → 编辑生效 → 删除后消失；code 重复返回 1004。
- [ ] 角色分配菜单后，`/auth/codes` 对该角色的用户返回正确权限码集合（含按钮码）。
- [ ] 父 AC-10 后端侧：调整角色菜单后，该角色用户的 `/menu/all` 随之变化。
- [ ] `go test ./...` 通过（含权限码并集计算用例）。

## Out of Scope

- 用户分配角色（backend-user）、前端角色页面（frontend-pages）
- perm 中间件的权限码强制执行（backend-oplog 落地）
