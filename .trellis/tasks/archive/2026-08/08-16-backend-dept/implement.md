# 实施计划：08-16-backend-dept 部门树 CRUD

> 设计见 `design.md`；契约以父 design.md §3/§6 与 seed.go 权限码为准。
> 执行节奏：逐步实施 → 每步 `go build`/`make test` 验证 → 汇报，**暂停等用户手动 commit**。

## 步骤（每步验证点见右栏）

1. **model 树函数提炼**（`app/internal/model/tree.go` 新增 + `menu.go` 改薄包装 +
   `department.go` 增 `DepartmentTreeNode`/`BuildDeptTree`/`IsDeptDescendant`）。
   → 验证：`go build ./...` 通过；现有 `menu_tree_test.go` 全绿（锁定提炼不改 menu 行为）。
2. **`app/internal/repository/department.go`**：`DepartmentRepository` 接口 + 实现
   （Create/Update/Delete/GetByID/ListAll/CountByParentID，对齐 menu repo 风格；创建子部门与删除部门通过事务锁定父/目标行，删除前原子检查子部门）。
   → 验证：`go build ./...` 通过。
3. **`app/internal/service/department.go`**：`SaveDeptInput`/`FillModel`、`DeptService`
   接口 + 实现（name 规范化、父校验 1010/1011/1013、删除拦截 1007、错误映射复用 `mapRepoError`）。
   → 验证：`go build ./...` 通过。
4. **`app/internal/api/department.go`**：`DepartmentHandler` 四个 handler
   （GetDeptTree/CreateDept/UpdateDept/DeleteDept），复用 `parseIDParam`，错误统一 `c.Error`。
   → 验证：`go build ./...` 通过。
5. **路由注册**（`app/internal/router/router.go`）：
   - `Deps` 增 `DepartmentHandler *api.DepartmentHandler`；
   - `deptRoutePath = "/dept"`；`deptGroup` 注册 `GET /dept/tree`、`POST /dept`、
     `PUT /dept/:id`、`DELETE /dept/:id`；
   - perm 声明：POST=`system:dept:add`、PUT `:id`=`system:dept:edit`、
     DELETE `:id`=`system:dept:delete`（与 seed.go 按钮码一致）。
   → 验证：`go build ./...` 通过。
6. **wire 装配**（`app/cmd/api/wire.go`）：加 `repository.NewDepartmentRepository`、
   `service.NewDeptService`、`api.NewDepartmentHandler`；`make wire` 重新生成
   `wire_gen.go`（禁止手改）。
   → 验证：`make wire` 后 `go build ./...` 通过。
7. **测试**：
   - `app/internal/model/department_tree_test.go`：BuildDeptTree 结构/排序/空/父缺失/环断/宽树性能；
     IsDeptDescendant 直接与深层后代/非后代/父链环安全。
   - `app/internal/service/department_test.go`（sqlite 内存库，`setupTestDB` 增
     `&model.Department{}` 迁移）：CRUD 全流程 / 三层树嵌套与排序 / 删除拦截 1007 与
     软删后可删 / 二次删除 1011 / 父校验 1010·1011·1013 / name 规范化 1009 /
     status 创建显式禁用、默认启用、编辑省略不变 / 空树返回空切片。
   - `app/internal/api/department_test.go`：HTTP CRUD、树响应、错误码、软删除和状态往返。
   - `app/internal/router/router_test.go`：`routerTestDeptService` mock + Deps 装配；
     新增无 token 访问 `GET /api/dept/tree` → 401，以及普通角色访问三条写路由 → 403。
   → 验证：`gofmt -l .` 无输出；`make vet` 干净；`make test` 全绿。

## 验证清单（任务 AC 映射）

- [x] 父 AC-15 后端侧：curl 可完成部门树增删改查；有子部门的删除返回 1007
      （service 层测试覆盖 1007/1011/1010/1013；HTTP 联调需登录取 token，auth 已完成，
      可 curl 实测）。
- [x] 树形查询输出嵌套 children，任意深度层级正确（service 测试三层嵌套 + 纯函数用例）。
- [x] 无 token 访问返回 401（router 冒烟用例）。
- [x] `go test ./...` 通过（含部门树构建、软删拦截用例）。

## 回滚点

- 每步 `go build` / `make test` 通过后均可安全停在当前点。
- 唯一跨文件敏感改动是步骤 1 的 model 提炼（menu.go 改薄包装，由 menu_tree_test.go
  锁定行为）与步骤 5/6 的 `router.Deps` 扩展 + wire 再生成；出问题可 `git checkout`
  对应文件整体回退，不影响已提交代码。
- 完成后暂停，等用户手动 commit（父 implement.md 标准节奏）。

## 备注

- 部门删除在 repository 事务内锁定目标行、检查未软删子部门并执行软删除；创建子部门锁定父行，避免并发操作产生孤儿节点。
- `Department.Status` 不依赖 GORM 的零值默认处理；service 在新建且省略 status 时明确填充启用值，生产 schema 由 V2 迁移移除 status 默认值并增加 parent_id 索引。
- `app/migrations/V2__add_department_parent_index_and_fix_status_default/` 提供 MySQL/PostgreSQL 脚本。
- 不修改 seed.go（权限码与种子数据均已就位；seed 为唯一权限码源）。
- 无新增 errno 错误码（1007/1009/1010/1011/1013 全部复用）。
