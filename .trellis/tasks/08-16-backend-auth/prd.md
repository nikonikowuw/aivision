# 登录/刷新/登出 + JWT + auth 中间件 + user/info + auth/codes

> 父任务：`../08-16-gin-vben-scaffold/`（源需求 prd.md、design.md 在父任务目录，实施时先读）。

## Goal

实现认证全链路：登录签发双 token、refresh 轮换、登出吊销、auth 中间件保护接口、用户信息与权限码查询。本任务结束后可用 curl 完成登录 → 带 token 访问受保护接口 → 刷新 → 登出。

## 依赖

- `08-16-backend-skeleton` 已完成（model、seed、wire 骨架在位）。

## Requirements

- R-1.1 `POST /auth/login`：username+password，成功返回用户信息+accessToken，httpOnly cookie（名 `jwt`）下发 refresh token。
- R-1.2 `POST /auth/refresh`：读 cookie 中 refresh，校验未过期未 revoke，轮换新 access+refresh（旧 refresh 立即 revoke），响应体为**裸 token 字符串**。
- R-1.3 `POST /auth/logout`：revoke 当前 refresh，清 cookie。
- R-1.4 `GET /user/info`：当前用户信息（对齐 vben UserInfo：userId/username/realName/roles[角色码]/avatar/desc/homePath）。
- R-1.5 `GET /auth/codes`：当前用户权限码集合 `string[]`（super 返回 `["*"]`）。
- R-1.6 密码 bcrypt；登录失败统一「用户名或密码错误」；禁用用户拒绝登录（errno 1008）。
- R-1.7 auth 中间件：除 login/refresh/swagger 外要求有效 access token，否则 HTTP 401。
- 认证 service 按扩展点设计：身份验证与 token 签发解耦（父 design.md §1）。
- 响应契约见父 prd.md「API 契约」；错误码用父 design.md §6 表（1001/1002/1008 等）。

## Acceptance Criteria

- [ ] 父 AC-4：admin/admin123 登录成功，响应 `{code:0,data}`，拿到 accessToken + `jwt` cookie。
- [ ] 错误密码返回 code 1001；禁用用户返回 1008；不存在的用户同样 1001。
- [ ] 带有效 token 访问 `/user/info`、`/auth/codes` 返回正确数据；无 token/伪造 token 返回 HTTP 401。
- [ ] `/auth/refresh`：有效 cookie 返回新裸 token 字符串，旧 refresh 记录被 revoke；revoke 过的 refresh 再刷新返回 401。
- [ ] `/auth/logout` 后 refresh 记录 revoked，cookie 清除。
- [ ] access 过期后刷新成功，刷新后新 access 可访问接口（缩短 ttl 或伪造过期 token 验证）。
- [ ] `go test ./...` 通过（登录/轮换/吊销的 service 层用例）。

## Out of Scope

- 业务模块 CRUD、perm 中间件（权限码鉴权，后续子任务）
- 操作日志中间件（backend-oplog）
