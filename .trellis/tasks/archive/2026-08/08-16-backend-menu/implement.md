# 实施计划：08-16-backend-menu 菜单树 CRUD 与 /menu/all

## 1. 任务目标

实现 Go 后端菜单管理 CRUD 接口与 `/menu/all` 权限路由树生成，提供符合 vben 要求的 JSON 数据并防止删除带子节点的菜单。

## 2. 步骤列表

- [ ] **Step 1: 错误码与通用封装补全**
  - 在 `app/internal/pkg/errno/errno.go` 中确认/新增 `ErrMenuHasChildren` (1006) 错误码定义。

- [ ] **Step 2: Menu Repository 实现**
  - 新增 `app/internal/repository/menu.go`
  - 实现 `MenuRepository` 接口，包含全量查询、按 ID 列表查询、按 RoleID 查询 MenuID、按 ParentID 计数等。

- [ ] **Step 3: Menu Service & vben 路由树转换实现**
  - 新增 `app/internal/service/menu.go`
  - 实现全量菜单树获取 `GetMenuTree`
  - 实现用户权限菜单树 `GetUserMenuTree`（剔除 button，转换 vben 格式）
  - 实现 `CreateMenu`, `UpdateMenu`, `DeleteMenu`（包含子节点检测 1006 逻辑）

- [ ] **Step 4: Menu API Handler & Router 注册**
  - 新增 `app/internal/api/menu.go`
  - 更新 `app/internal/router/router.go` 注册 `/api/menu/tree`, `/api/menu/all`, `/api/menu` (POST/PUT/DELETE)
  - 更新 `app/cmd/api/wire.go` 并执行 `make wire` 重新生成 DI 代码

- [ ] **Step 5: 单元测试与验证**
  - 新增 `app/internal/service/menu_test.go`
  - 验证：`/menu/all` 剔除 button、删除带子节点菜单返回 1006、单元测试全部通过 (`go test ./...`)。

## 3. 验证命令

```bash
cd app
make wire
make vet
make test
```
