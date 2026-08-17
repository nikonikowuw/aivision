# 技术设计：08-16-backend-dept 部门树 CRUD

> 父任务：`../08-16-gin-vben-scaffold/`（数据模型、错误码契约以父 design.md §3/§6 为准）
> 实施时先读父 prd.md/design.md 与本任务 prd.md。

## 1. 模块职责与边界

本任务实现部门管理模块后端，是系列第一个业务模块，验证「四层 + 权限声明」范式：

- 部门树 CRUD（`app/internal/repository/department.go`、`app/internal/service/department.go`、
  `app/internal/api/department.go`）。
- 全量树查询 `GET /dept/tree`（parent_id 递归挂载，sort 升序，任意深度）。
- 软删除拦截：存在未软删子部门时拒绝（`errno.CodeDeptHasChildren` 1007）。

数据模型 `model.Department`（表 `departments`）已在 `08-16-backend-skeleton` 实现；
seed.go 已种 1 个根部门「演示部门」，且已含 `system:dept:add/edit/delete` 按钮权限码
（seed.go 为唯一权限码源，本任务不增删）。

### 边界声明（重要）

- 用户绑定部门属 `08-16-backend-user`；前端部门页面属 `08-16-frontend-pages`；
  数据权限过滤不在任何任务范围内。
- perm 中间件已在 `08-16-backend-oplog` 落地：本任务按契约声明路由权限码即生效
  （未声明的写路由默认 403、读路由仅要求认证）。

## 2. API 契约与 DTO

路由前缀统一 `/api/dept`。

### 2.1 `GET /api/dept/tree`

- 响应：`data: [DepartmentTreeNode...]`，节点内嵌 `children` 数组，任意深度；
  全量部门（含禁用），每层按 `sort asc, id asc` 排序。
- 空库时 `data` 为 `[]`（非 null）。
- 权限：不注册权限码 → 仅要求认证（与 `GET /api/menu/tree` 一致）。

### 2.2 `POST /api/dept`、`PUT /api/dept/:id`

- 请求体：
  ```go
  type SaveDeptInput struct {
      ParentID uint64 `json:"parentId"` // 0=根
      Name     string `json:"name" binding:"required"`
      Sort     int    `json:"sort"`
      Leader   string `json:"leader"`
      Phone    string `json:"phone"`
      Status   *int8  `json:"status"` // 省略时新建默认启用；编辑省略表示不变
  }
  ```
- 业务规则：
  - `name` 去首尾空白后入库；空/纯空白返回 `errno.CodeInvalidParam`（1009）。
  - 创建时 `parentId != 0`：父部门必须存在（未软删），否则 1011。
  - 编辑时 `parentId != 0`：父=自身返回 `errno.CodeParentIsSelf`（1010）；父不存在返回
    1011；父是当前节点的后代返回 `errno.CodeParentIsDescendant`（1013）。
  - 编辑时部门不存在返回 1011。
- 响应：`data` 为保存后的 Department。

### 2.3 `DELETE /api/dept/:id`

- 软删除；存在未软删子部门返回 `errno.CodeDeptHasChildren`（1007）；不存在返回 1011。
- 响应：`data: null`。

### 2.4 路由权限码声明（与 seed 按钮码一致）

| 路由 | 权限码 |
| POST `/api/dept` | `system:dept:add` |
| PUT `/api/dept/:id` | `system:dept:edit` |
| DELETE `/api/dept/:id` | `system:dept:delete` |

读路由不注册权限码（仅认证）；写路由已注册，按 perm 中间件校验。

## 3. 分层设计

### 3.0 Model 层：树纯函数复用提炼（`app/internal/model/tree.go`）

menu 模块已实现树组装（`BuildMenuTree`）与后代判定（`IsMenuDescendant`），部门需要
完全相同的算法。依据代码复用指南（禁止复制粘贴），提炼为按「下标 + 访问器」的通用函数，
menu 与 dept 各自保留薄包装，**公共 API 与 JSON 结构不变**：

```go
// tree.go
// BuildTree 按 parent 关系把 n 个元素组装为树；返回根元素下标（已按 sort,id 递归升序）。
// parentOf(i)：第 i 个元素的父下标，-1 表示父不存在（parent_id=0 或父不在列表中）。
// keyOf(i)：排序键 (sort, id)。环（脏数据）断环：环内当前元素作为根。
// childrenOf(p)：第 p 个元素的直接子下标（升序），供调用方递归翻译为节点树。
func BuildTree(n int, parentOf func(int) int, keyOf func(int) (int, uint64)) (roots []int, childrenOf func(int) []int)

// IsDescendant 判断 candidateID 是否位于 ancestorID 子树中；parentOf 查父 id。
// 父链环（脏数据）时停止遍历返回 false，避免死循环。
func IsDescendant(parentOf func(uint64) (uint64, bool), ancestorID, candidateID uint64) bool
```

- `menu.go`：`BuildMenuTree` / `IsMenuDescendant` / `sortMenuTree` 改为薄包装，
  签名与返回形状（`[]*MenuTreeNode`、空输入返回 nil）完全不变；
  现有 `menu_tree_test.go` 不改动，作为提炼重构的行为锁定。
- `department.go`：新增 `DepartmentTreeNode`（内嵌 `Department` + `Children`）、
  `BuildDeptTree([]Department) []*DepartmentTreeNode`、`IsDeptDescendant([]Department, ancestorID, candidateID) bool`，
  与 menu 对称。

### 3.1 Repository（`app/internal/repository/department.go`）

```go
type DepartmentRepository interface {
    Create(ctx context.Context, dept *model.Department) error
    Update(ctx context.Context, dept *model.Department) error          // Save 全字段，与 menu repo 同风格
    Delete(ctx context.Context, id uint64) (bool, error)               // 软删；返回是否实际删除
    GetByID(ctx context.Context, id uint64) (*model.Department, error) // 未找到映射 ErrNotFound
    ListAll(ctx context.Context) ([]model.Department, error)           // sort asc, id asc
    CountByParentID(ctx context.Context, parentID uint64) (int64, error)
}
```

- 实现与 `menuRepository` 逐一对齐（GORM 软删默认过滤、`ErrNotFound` 映射等）。
- `CountByParentID` 默认 scope 只数未软删行——「子部门」即未删除子部门，与 menu 语义一致。
- `Create`/`Update`/`Delete` 在 repository 事务内按稳定顺序锁定相关部门；创建/移动子部门与删除父部门共享锁，删除时在锁内检查子部门，避免并发产生孤儿节点。

### 3.2 Service（`app/internal/service/department.go`）

```go
type SaveDeptInput struct { ... }          // 见 §2.2，含 FillModel(m *model.Department)
type DeptService interface {
    GetDeptTree(ctx context.Context) ([]*model.DepartmentTreeNode, error)
    CreateDept(ctx context.Context, input *SaveDeptInput) (*model.Department, error)
    UpdateDept(ctx context.Context, id uint64, input *SaveDeptInput) (*model.Department, error)
    DeleteDept(ctx context.Context, id uint64) error
}
```

- `GetDeptTree`：`ListAll` → `model.BuildDeptTree`；nil 归一为空切片（与 menu service 一致）。
- `FillModel` 沿用 `SaveMenuInput` 风格：Status 指针 nil 且新建时默认 `model.StatusEnabled`；`Department` 模型不声明 GORM status 默认值，避免显式 `status=0` 被 ORM 当作零值替换，生产 schema 由 V2 迁移同步。
- 入参规范化：`name = strings.TrimSpace(...)`，空 → 1009；`leader` 同步去首尾空白（非必填）。
- 父校验与删除拦截规则见 §2.2/§2.3；错误映射复用同包 `mapRepoError`
  （menu service 已定义：`repository.ErrNotFound` → 1011）。

### 3.3 Handler（`app/internal/api/department.go`）

- `DepartmentHandler` 四个方法（GetDeptTree/CreateDept/UpdateDept/DeleteDept），
  与 `MenuHandler` 同风格：绑定 → 调 service → `response.Success`；错误统一
  `c.Error(errno.NewError(...))` 交给统一错误处理中间件；路径参数复用 `parseIDParam`。

## 4. 路由装配（`app/internal/router/router.go`）

- `Deps` 增 `DepartmentHandler *api.DepartmentHandler`（wire.Struct 自动装配）。
- `deptGroup := apiGroup.Group(deptRoutePath)`（`deptRoutePath = "/dept"`）：
  `GET /tree`、`POST ""`、`PUT /:id`、`DELETE /:id`。
- 按 §2.4 用 `PermMiddleware.Register` 声明三条写路由权限码。
- `cmd/api/wire.go` 增 `repository.NewDepartmentRepository`、`service.NewDeptService`、
  `api.NewDepartmentHandler`；`make wire` 重新生成（禁止手改 `wire_gen.go`）。

## 5. 错误码

全部复用既有错误码，**无新增**：1007（有子部门）、1009（参数错误，含 name 空白）、
1010（父=自身）、1011（不存在）、1013（父为后代）。

## 6. 测试策略

### 6.1 `model/department_tree_test.go`

- 树结构：根/二级/三级嵌套正确；`sort asc, id asc` 层内排序；空输入返回 nil；宽树不会产生 O(n²) 环检测分配。
- 父节点不在列表中的节点作为根；脏数据环断环不挂死。
- `IsDeptDescendant`：直接后代/深层后代/非后代/自身传入/父链环安全。
- **`menu_tree_test.go` 不改动且必须继续通过**——锁定 `tree.go` 提炼不改变 menu 行为。

### 6.2 `service/department_test.go`（sqlite 内存库，复用 `setupTestDB`）

- `setupTestDB` 的 AutoMigrate 列表增 `&model.Department{}`（幂等，不影响 menu/role 用例）。
- CRUD 全流程：建根 → 建子 → 建孙 → `GetDeptTree` 三层嵌套与排序正确；
  编辑生效；删除叶子成功、二次删除 1011。
- 删除拦截：有子部门 → 1007；子部门软删后父可删（只数未软删行）。
- 父校验：创建父不存在 1011；编辑父=自身 1010；父为后代 1013；父不存在 1011。
- 入参：纯空白 name 1009；name/leader 首尾空白去后入库；status 创建显式禁用、
  省略新建默认启用、编辑省略不变、显式禁用生效。
- 空库 `GetDeptTree` 返回空切片（非 nil）。

### 6.3 HTTP 与路由测试

- `app/internal/api/department_test.go`：真实 handler 的 CRUD/树响应、状态往返、错误码和软删除契约。
- `router_test.go` 增量：`Deps` 增字段后补 mock；无 token 访问 `GET /api/dept/tree` → 401，普通角色访问写路由 → 403。

### 6.4 质量门禁

`gofmt -l .` 无输出 → `make vet` 干净 → `make test` 全绿 → `make wire` 重新生成 `wire_gen.go`。

## 7. 评审记录

1. **IsDeptDescendant 自身语义**：`candidate == ancestor`（自身）返回 true（“自身位于自己的子树”，
   与 IsMenuDescendant 原实现一致）；自身挂自身在 service 层由 1010（CodeParentIsSelf）单独拦截，
   该语义由 `department_tree_test.go` 与既有 `menu_tree_test.go` 共同锁定。
