# 菜单树 CRUD（含按钮级）

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现菜单管理模块：catalog/menu/button 三级类型的树形 CRUD + 按用户权限出菜单树（`/menu/all`）。本任务结束后菜单管理接口齐全，`/menu/all` 能按权限码过滤返回 vben 路由结构。

## 依赖

- `08-16-backend-skeleton`（seed 菜单树）。
- `08-16-backend-auth`（auth 中间件；权限码集合计算的底层查询在本任务实现，`/auth/codes` 与其共用）。

## Requirements

- R-3.3 菜单管理：`GET /menu/tree`（全量，含 button）、`POST /menu`、`PUT /menu/:id`、`DELETE /menu/:id`（软删，有子节点拒绝，errno 1006）。
- R-4.1 `GET /menu/all`：按当前用户权限返回菜单树（catalog+menu，**不含 button**），结构对齐 vben `RouteRecordStringComponent`（id/name/path/component/type/icon/meta{icon,order,title,affixTab,keepAlive}/children），catalog 顶层挂 BasicLayout、component 为 `views` 相对路径。
- 菜单字段：parent_id、type、name、path、component、icon、sort、status、permission、affix、keep_alive、home_path。
- super 用户返回全量树不过滤。
- 模型层菜单树构建为纯函数（可单测）。

## Acceptance Criteria

- [ ] 父 AC-5 后端侧：`/menu/all` 返回菜单树，类型为 catalog/menu 两级，无 button 节点，排序正确。
- [ ] 不同权限用户返回不同菜单集（构造低权限角色验证过滤）。
- [ ] 菜单 CRUD 全流程可用；删除有子节点的菜单返回 1006。
- [ ] `go test ./...` 通过（含树构建、权限过滤用例）。

## Out of Scope

- 角色分配菜单（backend-role）、前端菜单页面与动态路由渲染（frontend-pages / frontend-integration）
