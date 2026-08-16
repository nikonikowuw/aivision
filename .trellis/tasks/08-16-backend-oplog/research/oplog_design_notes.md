# 技术调研与设计考量：操作日志中间件、perm权限码中间件、日志查询

## 1. 操作日志中间件需求与细节
- **触发条件**：仅 `POST`、`PUT`、`DELETE` 且请求路径以 `/api` 开头。
- **请求体读取与恢复**：
  - 中间件在 handler 执行前读取 `c.Request.Body`。
  - 用 `io.NopCloser(bytes.NewReader(bodyBytes))` 重置 `c.Request.Body`，确保业务 handler 仍可读取完整请求体。
- **敏感字段脱敏**：
  - 需对 JSON 格式的 Body 递归脱敏，黑名单字段（大小写不敏感匹配）：`password`、`oldpassword`、`oldPassword`、`newpassword`、`newPassword`、`token`、`secret`、`refreshtoken`、`accessToken` 等。
  - 脱敏后替换为 `"***"`。如果 Body 不是合法 JSON 或解析失败，则固定记录 `"[request body omitted]"`，不得保存原文。
  - URL query 中的 password/token/secret 等敏感参数脱敏；query 解析失败时记录 `"[query omitted]"`。
  - 最终写入 `Body` 字段限制长度不超过 4KB（4096 字符/字节），多余截断并追加 `...`。
- **模块与动作识别**：
  - `module`：由 `c.FullPath()` 解析推断。例如 `/api/system/user` 或 `/api/user` → 模块标识，或者在路由分组元数据/路径前缀推断（如 `/api/menu` -> `menu`，`/api/auth/login` -> `auth`）。对于未匹配路由或根路径做安全兜底。
  - `action`：HTTP Method + Path（例如 `POST /api/menu`），或根据路由定义推断。
- **用户识别**：
  - 已登录请求：从 `middleware.IdentityFromContext(c)` 中获取 `UserID` 和 `Username`（需考虑 Identity 结构，当前 Identity 含 UserID/RoleIDs/RoleCodes，可扩展 Username 或在 AuthMiddleware 中存入 Username，或在 context 提取）。
  - 登录接口（`/api/auth/login`）或未携带 token 的登录失败：此时无上下文 Identity，从脱敏前的登录请求体 JSON 中尝试解析 `username`（不解析 password），将 `UserID` 留空 (0)，`Username` 记录请求体里的用户名。
- **异步落库**：
  - 业务处理 `c.Next()` 完成后，计算耗时 `duration_ms`，获取响应状态码 `c.Writer.Status()`。
  - 启动独立 goroutine 执行写入操作，使用 `recover()` 保护防止 panic，写入失败仅通过 zap logger 记录 warn 级别日志，绝不抛出错误或影响客户端 HTTP 响应。

## 2. Perm 权限码中间件需求与细节
- **权限码判断逻辑**：
  - 超级管理员判定：当前用户关联的角色包含 `super`（`model.RoleSuperCode`），直接放行。
  - 用户权限码集合获取：通过当前用户的启用角色查询关联的所有有效菜单/按钮权限码（`permission != ""`），去重并集。
  - 接口权限要求：
    - 在 Gin 路由注册或中间件配置中，每个受控写接口（或需要校验权限的接口）绑定权限码（例如 `perm.Require("system:menu:add")` 或路由集中声明 map）。
    - 若路由声明了权限码，中间件校验用户权限集合是否包含该权限码；如果不包含，则调用 `c.Error(errno.NewError(errno.CodeForbidden))` 并 `c.Abort()`。
    - 规范 R-2.4 要求：“未声明权限码的写接口默认拒绝”。
- **性能与解耦**：
  - 支持在中间件中注入权限查询接口（如 `AuthRepository` 或 `PermissionService`）。
  - 超管跳过查询，普通用户可按需查询或挂载在 context 中。

## 3. 日志查询接口 (GET /api/oplog/page, GET /api/oplog/:id)
- **分页查询参数**：
  - `page` (默认 1, min 1)
  - `pageSize` (默认 20, min 1, max 100)
  - `username` (模糊或精确匹配)
  - `module` (精确匹配)
  - `statusCode` (状态码筛选)
  - `startTime` / `endTime` (按 `created_at` 范围筛选)
- **详情查询**：
  - `GET /api/oplog/:id`：按主键查询，找不到返回 `errno.CodeNotFound` (1011)。
- **只读**：不提供删除、清空、修改接口。
