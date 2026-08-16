# 技术设计：08-16-backend-role 角色 CRUD + 分配菜单权限

> 父任务：`../08-16-gin-vben-scaffold/`（设计契约以父 design.md §4 RBAC 数据流、§6 API 契约为准）
> 实施时先读父 prd.md/design.md 与本任务 prd.md。

## 1. 模块职责与边界

本任务实现角色管理模块的后端：

- 角色 CRUD + 分页（`app/internal/repository/role.go`、`app/internal/service/role.go`、`app/internal/api/role.go`）。
- 角色-菜单覆盖式分配（含 button 节点）与已分配菜单查询。
- 权限码并集的正确性验证（复用 `menuRepository.GetPermissionsByRoleIDs`，见 §5）。

数据模型 `model.Role` / `model.RoleMenu` 已在 `08-16-backend-skeleton` 中实现；
`menuRepository.GetPermissionsByRoleIDs`（启用角色 → 启用菜单 permission≠'' 去重并集）
已在 `08-16-backend-menu` 中实现并被 perm 中间件使用。

### 边界声明（重要）

- **`/auth/codes` 端点本身属 `08-16-backend-auth`，本任务不实现它**。本任务保证其底层计算
  `GetPermissionsByRoleIDs` 与角色菜单分配写入 `role_menus` 的一致性，并补充权限码并集测试用例。
  任务 AC-2「`/auth/codes` 返回正确权限码集合」的 HTTP 级验证在 backend-auth 完成后联调确认。
- 用户-角色分配属 `08-16-backend-user`，本任务不做。
- perm 中间件的强制执行属 `08-16-backend-oplog`（已落地），本任务仅按契约注册路由权限码。

## 2. API 契约与 DTO

路由前缀统一 `/api/role`。

### 2.1 `GET /api/role/page`

- 请求：`page` / `pageSize`（默认 1 / 20，上限 100，与 oplog 分页一致）。
- 响应：`data: { "items": [Role...], "total": n }`。
- 无筛选参数（前端角色页无筛选字段，见 frontend-pages prd；需要时再扩展）。
- 排序：`sort asc, id asc`。
- 权限：不注册权限码 → 仅要求认证（与 `GET /api/menu/tree` 一致）。

### 2.2 `POST /api/role`、`PUT /api/role/:id`

- 请求体：
  ```go
  type SaveRoleInput struct {
      Name   string `json:"name" binding:"required"`
      Code   string `json:"code" binding:"required"`
      Status *int8  `json:"status"` // 省略时新建默认启用；编辑省略表示不变
      Sort   int    `json:"sort"`
      Remark string `json:"remark"`
  }
  ```
- 业务规则：
  - 创建/编辑时 `code` 唯一（编辑排除自身），重复返回 `errno.CodeRoleCodeTaken`（1004）。
  - name/code 去除首尾空白后入库；纯空白返回 `errno.CodeInvalidParam`（1009）。
  - 软删后重建同 code、编辑改用已软删角色的 code：服务层查重过滤软删行，由唯一索引兜底
    拦截并映射为 1004（repository 层把 `gorm.ErrDuplicatedKey` 映射为 `ErrDuplicateKey`）。
  - 编辑时 `super` 角色不可停用、不可修改 code，否则返回
    `errno.CodeSuperRoleProtected`（1014，新增，见 §6）。
  - 编辑时角色不存在返回 `errno.CodeNotFound`（1011）。
- 响应：`data` 为保存后的 Role。

### 2.3 `DELETE /api/role/:id`

- 软删除；不存在返回 1011；`super` 返回 1014。
- 关联数据不级联清理：`user_roles` / `role_menus` 保留历史行，但已失效
  （auth 中间件只取启用未删除角色，权限并集只查启用角色——见 §5）。
- 响应：`data: null`。

### 2.4 `GET /api/role/:id/menu-ids`

- 响应：`data: [uint64]`（该角色当前绑定的全部 menu id，**不过滤角色状态**——
  编辑弹窗需要展示禁用角色的既有勾选；**排除已软删菜单**）。
- 角色不存在返回 1011。
- 权限：不注册权限码 → 仅要求认证。

### 2.5 `PUT /api/role/:id/menus`

- 请求体：`{ "menuIds": [uint64] }`（可含 button 节点；省略/空数组 = 清空分配）。
- 业务规则：
  - 角色不存在 → 1011；`super` 角色 → 1014（super 绕过 role_menus，分配无意义且
    会覆盖 seed 全量绑定）。
  - 去重后逐条校验菜单存在（未软删）；任一不存在 → `errno.CodeInvalidParam`（1009）。
  - 覆盖式写入：事务内先删该角色全部 `role_menus` 再插入新集（去重后），原子提交。
- 响应：`data: null`。

### 2.6 路由权限码声明（与 seed 按钮码一致，seed.go 为唯一权限码源）

| 路由 | 权限码 |
|---|---|
| POST `/api/role` | `system:role:add` |
| PUT `/api/role/:id` | `system:role:edit` |
| DELETE `/api/role/:id` | `system:role:delete` |
| PUT `/api/role/:id/menus` | `system:role:assign-menu` |

读路由不注册权限码（仅认证），与 menu 模块一致；写路由未注册默认被 perm 中间件拒绝。

## 3. 分层设计

### 3.1 Repository（`app/internal/repository/role.go`）

```go
type RoleRepository interface {
    Create(ctx context.Context, role *model.Role) error
    Update(ctx context.Context, role *model.Role) error        // Save 全字段，与 menu repo 同风格
    Delete(ctx context.Context, id uint64) (bool, error)       // 软删；返回是否实际删除
    GetByID(ctx context.Context, id uint64) (*model.Role, error)   // 未找到映射 repository.ErrNotFound
    GetByCode(ctx context.Context, code string) (*model.Role, error) // 未找到映射 ErrNotFound
    ListPage(ctx context.Context, page, pageSize int) ([]model.Role, int64, error)
    GetMenuIDs(ctx context.Context, roleID uint64) ([]uint64, error) // join menus 排除软删菜单，不筛角色状态
    ReplaceMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error // 事务覆盖
}
```

- `ListPage` 内做分页参数归一（page<1→1；pageSize<1→20；>100→100），排序 `sort asc, id asc`。
- `Create`/`Update` 把 `gorm.ErrDuplicatedKey` 映射为 `repository.ErrDuplicateKey`
  （跨驱动依赖 gorm `TranslateError`，db 连接已全局开启）。
- `GetMenuIDs` 不复用 `menuRepository.GetMenuIDsByRoleIDs`（后者过滤启用角色，语义不同，见 2.4）。
- `ReplaceMenus` 用 `db.Transaction`：删旧 → 插新（空集仅删）。菜单存在性校验在 service 层
  （复用 `menuRepository.GetByIDs`，其自动排除软删）。

### 3.2 Service（`app/internal/service/role.go`）

```go
type RoleService interface {
    GetPage(ctx context.Context, query *RolePageQuery) (*RolePageResult, error)
    CreateRole(ctx context.Context, input *SaveRoleInput) (*model.Role, error)
    UpdateRole(ctx context.Context, id uint64, input *SaveRoleInput) (*model.Role, error)
    DeleteRole(ctx context.Context, id uint64) error
    GetMenuIDs(ctx context.Context, id uint64) ([]uint64, error)
    AssignMenus(ctx context.Context, id uint64, menuIDs []uint64) error
}
```

- 依赖两个 repo：`RoleRepository` + `MenuRepository`（菜单存在性校验），wire 注入。
- `SaveRoleInput.FillModel(m *model.Role)` 沿用 `SaveMenuInput` 风格；Status 为指针，
  nil 且新建时默认 `model.StatusEnabled`。
- 入参先 `normalizeSaveRoleInput`：name/code 去首尾空白 + 非空校验（1009）。
- 唯一性校验走 `GetByCode` + `repository.ErrNotFound` 判断；唯一索引兜底竞态与
  软删后重建场景，`ErrDuplicateKey` 映射为 1004（`mapDuplicateKey`）。
- 错误映射复用 `mapRepoError`（menu service 已定义，同包可共用）。

### 3.3 Handler（`app/internal/api/role.go`）

- 六个 handler 与现有 `MenuHandler` 同风格：绑定 → 调 service → `response.Success`；
  错误一律 `c.Error(errno.NewError(...))` 交给统一错误处理中间件。
- 路径参数复用 `parseIDParam`。

## 4. RBAC 数据流一致性

分配菜单后，现有链路立即生效（无缓存）：

1. `PUT /role/:id/menus` → `role_menus` 覆盖写入。
2. perm 中间件：`GetPermissionsByRoleIDs([roleID...])` → 启用角色 JOIN 启用菜单，去重并集。
3. `/menu/all`：`GetMenuIDsByRoleIDs` → `GetByIDs` → 建树（backend-menu 已实现）。
4. `super` 角色：perm 中间件直接放行、`/menu/all` 返回全量菜单——super 的 role_menus 无需参与，
   但 seed 已全量绑定 24 条（契约见 `smoke_test.go`）。

因此本任务不需要新增缓存失效或权限重算逻辑；「改分配 → 下一请求即生效」由上述查询天然保证。

## 5. 权限码并集计算（与 /auth/codes 打通）

- 计算已存在于 `menuRepository.GetPermissionsByRoleIDs`，语义即 PRD 要求：
  **启用角色（status=1 未删）× 启用菜单（status=1 未删 permission≠''）→ distinct 权限码并集**，
  含 button 码。
- 本任务不复制该计算，只补测试锁定契约（见 §7），覆盖：
  - 分配含 button 的菜单集后并集含按钮码；
  - 多角色重叠菜单去重；
  - 禁用角色 / 禁用菜单被排除；
  - 清空分配后并集为空。
- backend-auth 实现 `/auth/codes` 时直接调用该方法即可（无需感知 role_menus 写入细节）。

## 6. 错误码新增

`internal/pkg/errno` 新增一个业务码（zh-CN / en-US 双语文案）：

```go
CodeSuperRoleProtected = 1014 // 超级管理员角色不可删除、停用或修改编码
// zh-CN: "超级管理员角色不可删除、停用或修改编码"
// en-US: "Super admin role cannot be deleted, disabled, or renamed"
```

其余场景复用已有码：1004（code 重复）、1009（参数错误，含非法 menuIds）、1011（角色不存在）。

## 7. 测试策略

### 7.1 `service/role_test.go`（sqlite 内存库，复用 menu_test.go 的 `setupTestDB`）

- CRUD 全流程：创建 → 列表可见（含分页/总数）→ 编辑生效 → 删除后列表不可见、二次删除 1011。
- code 唯一：创建重复 1004；编辑改成他人 code 1004；编辑保持自身 code 成功；
  软删后重建同 code 1004；编辑改成已软删角色 code 1004。
- super 保护：删除 super 1014；编辑 super status=0 1014；编辑 super 改 code 1014；
  编辑 super 改其他字段成功。
- AssignMenus：覆盖语义（二次分配替换旧集）、空集清空、非法 menuID 1009、角色不存在 1011、
  super 角色 1014。
- GetMenuIDs：返回与分配一致（禁用角色仍可查）、菜单软删后排除、角色不存在 1011。
- 入参规范化：纯空白 name/code 1009；首尾空白去除后入库。
- 权限码并集（§5 四项契约）：
  `AssignMenus` 后用 `repository.NewMenuRepository(db).GetPermissionsByRoleIDs` 断言集合。

### 7.2 `api/role_test.go`（handler 层，真实 sqlite + 真实 service，与 operation_log_test 同风格）

- 空 body/非法 id 参数 → 1009；创建成功响应含自增 ID 与默认启用状态；重复 code → 1004。
- AssignMenus → GetMenuIDs 链路一致；菜单软删后 menu-ids 为空；非法菜单 1009；角色不存在 1011。
- super 保护：停用/改 code/删除/分配菜单均 1014。

### 7.3 `router_test.go` 增量

- `router.Deps` 新增 `RoleHandler` 字段后，`newRouterTestEngine` 补一个
  `routerTestRoleService` mock 并传入 `api.NewRoleHandler(...)`，现有用例保持通过。
- 增一条冒烟：无 token 访问 `GET /api/role/page` 返回 401（与 menu 一致即可，不铺开）。

### 7.4 质量门禁

`gofmt -l .` 无输出 → `make vet` 干净 → `make test` 全绿 → `make wire` 重新生成 `wire_gen.go`。

## 8. 评审修复记录

code review 后的行为修订（已同步实现与测试）：

1. **软删后重建同 code 不再 500**：唯一索引冲突映射为 1004——repository 新增
   `ErrDuplicateKey` 哨兵（`Create`/`Update` 映射 `gorm.ErrDuplicatedKey`），service
   映射为 `CodeRoleCodeTaken`；依赖 gorm `TranslateError`，全部 gorm 连接（含测试）
   统一开启。
2. **super 角色 code 不可修改**：`UpdateRole` 增加保护，改 code 返回 1014。
3. **`AssignMenus` 对 super 返回 1014**：super 绕过 role_menus，避免覆盖 seed 全量绑定。
4. **`GetMenuIDs` 排除已软删菜单**：join `menus` 过滤 `deleted_at IS NULL`（不筛角色状态）。
5. **name/code 入参规范化**：去首尾空白后入库，纯空白返回 1009。
6. **1014 文案扩义**：删除/停用/改 code 统一语义（zh/en 双语文案已更新）。
7. **handler 层测试补充**：新增 `api/role_test.go`；`router.go` 注册顺序重组为按模块分组。
