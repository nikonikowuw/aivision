# 技术方案设计：操作日志中间件、perm 权限码中间件与日志查询

> 本文档设计操作日志全自动采集中间件、perm 权限码中间件、脱敏函数以及日志查询接口（分页与详情）。

---

## 1. 脱敏模块设计 (`internal/pkg/mask/mask.go`)

### 1.1 需求
- password/oldPassword/newPassword/token/secret/accessToken/refreshToken 等敏感字段脱敏为 `"***"`。
- 大小写不敏感匹配字段名。
- JSON 递归脱敏：对 JSON 对象、嵌套数组或结构做递归字段脱敏。
- 非 JSON 文本（或解析失败）固定记录 `"[request body omitted]"`，不得保存原文。
- URL query 中的 password/token/secret 等敏感参数脱敏；query 解析失败时记录 `"[query omitted]"`。

### 1.2 纯函数契约
- `MaskJSONBody(body []byte, maxLen int) string`
- `MaskData(data any) any`

---

## 2. 操作日志中间件设计 (`internal/middleware/oplog.go`)

### 2.1 需求
- 拦截 `POST`、`PUT`、`DELETE` 且请求 path 具有 `/api` 前缀的请求（`GET` / `OPTIONS` / `HEAD` 不产生日志）。
- 在 handler 执行前缓存 `c.Request.Body` 并用 `io.NopCloser(bytes.NewReader(bodyBytes))` 重置 `c.Request.Body`。
- 请求处理后，统计耗时（`duration_ms`）及响应状态码（`c.Writer.Status()`）。
- 从 context 读取当前用户身份（`middleware.IdentityFromContext(c)`）：
  - 若已认证：记录 `UserID` 与 `Username`。
  - 若未认证（如登录请求 `POST /api/auth/login` 或登录失败）：尝试从请求体 JSON 解析 `username`，`UserID` 设为 0。
- `module` 推断：解析 `c.FullPath()` 或 `c.Request.URL.Path`，例如：
  - `/api/auth/*` -> `auth`
  - `/api/menu/*` -> `menu`
  - `/api/dept/*` -> `dept`
  - `/api/user/*` -> `user`
  - `/api/role/*` -> `role`
  - `/api/oplog/*` -> `oplog`
- `action` 推断：记录 HTTP Method + Path（例如 `POST /api/menu`）或结合路由名称。
- 异步落库：通过 goroutine 异步落库（带 `recover()`），写失败通过 `zap.Logger.Warn` 记录，绝不影响客户端响应。

---

## 3. Perm 权限码中间件设计 (`internal/middleware/perm.go`)

### 3.1 需求
- 针对受控路由做权限校验（或通过声明权限码的中间件 `perm.Require("system:menu:add")` 或全自动全路由匹配）。
- 超级管理员（`RoleCodes` 包含 `super`）直接放行。
- 普通用户：
  - 查询用户关联的有效角色拥有的菜单/按钮权限码集合（`permission != ""`）。
  - 若路由声明了权限码，比对用户权限码集合；若不包含，则 `c.Error(errno.NewError(errno.CodeForbidden))` 并 `c.Abort()`。
  - 对于未声明权限码的写接口（POST/PUT/DELETE），默认拒绝（返回 403），防御性收紧。

---

## 4. 日志查询接口设计 (`internal/repository`, `internal/service`, `internal/api`)

### 4.1 数据层与业务层
- **Repository** (`OperationLogRepository`):
  - `Create(ctx, log *model.OperationLog) error`
  - `GetByID(ctx, id uint64) (*model.OperationLog, error)`
  - `ListPage(ctx, filter *OperationLogFilter) ([]model.OperationLog, int64, error)`
- **Service** (`OperationLogService`):
  - `Record(ctx, log *model.OperationLog) error`
  - `GetByID(ctx, id uint64) (*model.OperationLog, error)`
  - `GetPage(ctx, filter *OperationLogFilter) (*PageResult, error)`
- **Handler** (`OperationLogHandler`):
  - `GET /api/oplog/page`
  - `GET /api/oplog/:id`

---

## 5. 依赖注入与路由装配

- 在 `wire.go` 注册 `OperationLogRepository`、`OperationLogService`、`OperationLogHandler`、`OplogMiddleware`、`PermMiddleware`。
- 在 `router.New` 中挂载 `OplogMiddleware`，并在写接口挂载 `PermMiddleware`。
