# 实施计划：08-16-backend-role 角色 CRUD + 分配菜单权限

> 设计见 `design.md`；契约以父 design.md §4/§6 与 seed.go 权限码为准。
> 执行节奏：逐步实施 → `make vet` + `make test` → 汇报，**暂停等用户手动 commit**。

## 步骤（每步验证点见右栏）

1. **errno 新增错误码 1014**（`app/internal/pkg/errno/errno.go`）
   → 验证：`go build ./...` 通过。
2. **`app/internal/repository/role.go`**：`RoleRepository` 接口 + 实现
   （Create/Update/Delete/GetByID/GetByCode/ListPage/GetMenuIDs/ReplaceMenus 事务）。
   → 验证：`go build ./...` 通过。
3. **`app/internal/service/role.go`**：`SaveRoleInput`/`FillModel`、`RoleService` 接口 + 实现
   （code 唯一、super 删除/停用保护、AssignMenus 去重+菜单存在性校验、错误映射复用 `mapRepoError`）。
   → 验证：`go build ./...` 通过。
4. **`app/internal/api/role.go`**：`RoleHandler` 六个 handler（GetPage/Create/Update/Delete/GetMenuIDs/AssignMenus），
   复用 `parseIDParam`，错误统一 `c.Error`。
   → 验证：`go build ./...` 通过。
5. **路由注册**（`app/internal/router/router.go`）：
   - `Deps` 增 `RoleHandler *api.RoleHandler`；
   - `roleGroup` 注册 `GET /role/page`、`POST /role`、`PUT /role/:id`、`DELETE /role/:id`、
     `GET /role/:id/menu-ids`、`PUT /role/:id/menus`；
   - perm 声明：POST=`system:role:add`、PUT `:id`=`system:role:edit`、
     DELETE `:id`=`system:role:delete`、PUT `:id/menus`=`system:role:assign-menu`。
   → 验证：`go build ./...` 通过。
6. **wire 装配**（`app/cmd/api/wire.go`）：加 `repository.NewRoleRepository`、
   `service.NewRoleService`、`api.NewRoleHandler`；`make wire` 重新生成 `wire_gen.go`（禁止手改）。
   → 验证：`make wire` 后 `go build ./...` 通过。
7. **测试**：
   - `app/internal/service/role_test.go`（sqlite 内存库，复用 `setupTestDB`）：
     CRUD 全流程 / code 唯一（创建+编辑）/ super 保护（删除+停用）/ AssignMenus 覆盖与清空与
     非法 menuID=1009 / GetMenuIDs / 权限码并集四契约（含 button 码、去重、禁用排除、清空为空）。
   - `app/internal/router/router_test.go`：`newRouterTestEngine` 补 `routerTestRoleService` mock
     与 `RoleHandler` 装配；新增无 token 访问 `GET /api/role/page` → 401 冒烟。
   → 验证：`gofmt -l .` 无输出；`make vet` 干净；`make test` 全绿。

## 验证清单（任务 AC 映射）

- [ ] 父 AC-9 角色部分：创建 → 列表可见 → 编辑生效 → 删除后消失；code 重复返回 1004。
      （service 层测试覆盖；curl 联调需登录取 token——auth 登录接口属 backend-auth，可用现有
      测试签名 token 替代或待 backend-auth 完成后联调。）
- [ ] 角色分配菜单后，`/auth/codes` 返回正确权限码集合（含按钮码）：
      本任务以 `GetPermissionsByRoleIDs` 并集测试锁定；HTTP 联调待 backend-auth 完成。
- [ ] 父 AC-10 后端侧：调整角色菜单后 `/menu/all` 随之变化：
      由 `AssignMenus` 覆盖写入 + menu service 既有过滤逻辑保证；可加 service 级断言。
- [ ] `go test ./...` 通过（含权限码并集计算用例）。

## 回滚点

- 每步 `go build` / `make test` 通过后均可安全停在当前点。
- 唯一跨文件敏感改动是 `router.Deps` 结构体扩展（router_test 同步修改）与 wire 再生成；
  若步骤 5/6 出问题，`git checkout` 对应文件即可整体回退，不影响已提交代码。
- 完成后暂停，等用户手动 commit（父 implement.md 标准节奏）。

## 备注

- 不新建表、不改 model 结构（`roles`/`role_menus` 已在 skeleton 建好）→ 无 SQL 迁移。
- 不修改 seed.go 权限码（数据库规范：严禁增删权限码）。
