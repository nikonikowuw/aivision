# 技术设计：08-16-backend-menu 菜单树 CRUD 与 /menu/all

> 父任务：`../08-16-gin-vben-scaffold/`（设计契约以父 design.md §4 为准）

## 1. 模块职责与边界

本任务负责菜单管理模块的后端实现：
- 菜单 CRUD 存储（`app/internal/repository/menu.go`）与业务校验（`app/internal/service/menu.go`）。
- 权限过滤与 vben 路由树转换（`VbenRouteRecord` 转换纯函数与服务层组装）。
- Gin Handler（`app/internal/api/menu.go`）与路由挂载（`app/internal/router/router.go`）。

数据模型 `model.Menu` 与基础树构建 `model.BuildMenuTree` 已在 `08-16-backend-skeleton` 中实现。

## 2. API 契约与 DTO

路由前缀统一为 `/api`（或路由分组 `/api/menu`）：

### 2.1 `GET /api/menu/tree` (全量菜单树，含 button)
- **响应**：`{ "code": 0, "message": "ok", "data": [ MenuTreeNode... ] }`

### 2.2 `POST /api/menu` & `PUT /api/menu/:id`
- **请求体** (CreateMenuReq / UpdateMenuReq)：
  ```go
  type SaveMenuReq struct {
      ParentID   uint64 `json:"parentId"`
      Type       string `json:"type" binding:"required,oneof=catalog menu button"`
      Name       string `json:"name" binding:"required"`
      Title      string `json:"title"`
      Path       string `json:"path"`
      Component  string `json:"component"`
      Icon       string `json:"icon"`
      Sort       int    `json:"sort"`
      Status     *int8  `json:"status"`
      Permission string `json:"permission"`
      Affix      bool   `json:"affix"`
      KeepAlive  bool   `json:"keepAlive"`
      HomePath   string `json:"homePath"`
  }
  ```
- **业务规则**：
  - 更新或创建时，父节点 `ParentID` 若非 0，必须存在。
  - 不能将父节点设为自己或自己的后代节点（防环）。

### 2.3 `DELETE /api/menu/:id`
- **业务规则**：
  - 查询是否存在 `parent_id = id` 且未删除的子菜单节点。
  - 若存在子节点，返回业务错误码 `errno.ErrMenuHasChildren` (1006)。

### 2.4 `GET /api/menu/all` (用户菜单树，仅 catalog + menu)
- **响应结构**：对齐 vben `RouteRecordStringComponent`
  ```go
  type VbenRouteMeta struct {
      Title     string `json:"title"`
      Icon      string `json:"icon,omitempty"`
      Order     int    `json:"order"`
      AffixTab  bool   `json:"affixTab,omitempty"`
      KeepAlive bool   `json:"keepAlive,omitempty"`
  }

  type VbenRouteRecord struct {
      ID        uint64             `json:"id"`
      PID       uint64             `json:"pid"`
      Name      string             `json:"name"`
      Path      string             `json:"path"`
      Component string             `json:"component"`
      Type      string             `json:"type"`
      Meta      VbenRouteMeta      `json:"meta"`
      Children  []*VbenRouteRecord `json:"children,omitempty"`
  }
  ```
- **过滤与转换流程**：
  1. 获取当前用户关联角色的菜单 ID 列表。如果角色含 `super`，全量查出所有状态启用的菜单；否则仅查关联的启用菜单。
  2. 过滤掉 `Type == "button"` 的节点。
  3. 调用 `model.BuildMenuTree` 构建标准 `MenuTreeNode` 树。
  4. 递归将 `MenuTreeNode` 转为 `VbenRouteRecord` 结构。

## 3. 架构分层设计

- **Repository**: `MenuRepository` (接口 `MenuRepository`)
  - `Create`, `Update`, `Delete`
  - `GetByID`, `GetByIDs`
  - `ListAll`
  - `CountByParentID`
  - `GetMenuIDsByRoleIDs` / `GetPermissionsByRoleIDs`
- **Service**: `MenuService` (接口 `MenuService`)
  - `GetMenuTree(ctx)`
  - `GetUserMenuTree(ctx, userID, roles)`
  - `GetUserPermissions(ctx, userID, roles)`
  - `CreateMenu(ctx, req)`
  - `UpdateMenu(ctx, id, req)`
  - `DeleteMenu(ctx, id)`
- **Handler**: `MenuHandler`
  - 处理 HTTP 请求与响应转换。

## 4. 依赖注入与路由

在 `app/cmd/api/wire.go` 中加入 `repository`、`service`、`api` 的 Provider Set，更新 `router.New` 接收 `*api.MenuHandler` 并注册路由。

## 5. 测试策略

- **单元测试** (`internal/service/menu_test.go`)：
  - sqlite 内存库测试：全量树获取、仅 catalog/menu 节点过滤、vben 结构映射转换、带子节点删除拦截 (errno 1006)。
