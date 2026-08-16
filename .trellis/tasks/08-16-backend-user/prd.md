# 用户 CRUD + 分配角色 + 重置密码

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现用户管理模块：分页 CRUD + 部门绑定 + 分配角色 + 重置密码 + 启停用。本任务结束后后台可完整管理用户及其角色、部门关系。

## 依赖

- `08-16-backend-skeleton`、`08-16-backend-auth`。
- `08-16-backend-dept`（部门树查询/筛选）。
- `08-16-backend-role`（user_roles 关联的角色校验、分配接口）。

## Requirements

- R-3.1：`GET /user/page`（分页，筛选：关键字/状态/部门）、`POST /user`、`PUT /user/:id`、`DELETE /user/:id`（软删）、`PUT /user/:id/reset-password`（重置为初始密码）、`PUT /user/:id/roles`（分配角色）、`PUT /user/:id/status`（启停用）。username 唯一（errno 1003）。
- 新建/编辑用户可绑定 dept_id；列表返回所属部门名。
- 密码 bcrypt；列表接口不回传 password 字段。
- admin 自身不可删除、不可禁用。
- 分页响应 `data:{items,total}`；分页参数 page/pageSize。

## Acceptance Criteria

- [ ] 父 AC-9 用户部分：创建 → 列表可见（含部门名）→ 编辑生效 → 删除后消失；username 重复返回 1003。
- [ ] 分配角色后 `/auth/codes` 与该用户角色一致。
- [ ] 重置密码后新密码可登录；列表响应不含 password。
- [ ] 禁用用户后该用户无法登录（errno 1008）。
- [ ] 按部门筛选返回正确结果集。
- [ ] `go test ./...` 通过。

## Out of Scope

- 前端用户页面（frontend-pages）、操作日志（backend-oplog）
