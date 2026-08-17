# 部门树 CRUD

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现部门管理模块：无限层级树形 CRUD。本任务结束后可用 curl 完成部门树的增删改查与树形查询。本任务是系列中第一个业务模块，验证「四层 + 权限声明」范式。

## 依赖

- `08-16-backend-skeleton`（model/seed）。
- `08-16-backend-auth`（auth 中间件保护接口）。

## Requirements

- `GET /dept/tree`：全量部门树（parent_id 递归挂载，sort 升序）。
- `POST /dept`：新增（parent_id、name、sort、leader、phone、status）。
- `PUT /dept/:id`：编辑。
- `DELETE /dept/:id`：软删除；存在子部门时拒绝（errno 1007）。
- 遵循四层架构：model（树构建纯函数）→ repository → service → api；wire 注册新 Provider。
- 路由声明权限码：`system:dept:add/edit/delete`（perm 中间件在 backend-oplog 落地后即生效）。

## Acceptance Criteria

- [ ] 父 AC-15 后端侧：部门树增删改查正常；有子部门的部门删除返回 1007。
- [ ] 树形查询输出为嵌套 children，任意深度层级正确。
- [ ] 无 token 访问返回 401。
- [ ] `go test ./...` 通过（含部门树构建、软删拦截用例）。

## Out of Scope

- 用户绑定部门（backend-user）、前端部门页面（frontend-pages）
- 数据权限过滤
