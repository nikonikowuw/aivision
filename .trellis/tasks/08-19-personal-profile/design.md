# 个人中心资料与密码修改技术设计

## 1. 边界与数据流

### 资料读取

`ProfileBase` → `getCurrentProfileApi()` → `GET /api/user/profile` → AuthMiddleware → UserHandler → UserService → UserRepository → `users`

返回专用 DTO，不复用导航用 `UserInfoDTO` 或包含管理字段的 `model.User`：

```text
CurrentProfileDTO {
  username: string  // 只读展示
  nickname: string
  email: string
  phone: string
  avatar: string    // 只读展示
  remark: string
}
```

### 资料更新

`ProfileBase` → `updateCurrentProfileApi(input)` → `PUT /api/user/profile` → 从 Gin Context 读取 `identity.UserID` → UserService 仅构造 nickname/email/phone/remark 更新集 → UserRepository

请求体：

```text
UpdateCurrentProfileInput {
  nickname: string  // trim，最大 64
  email: string     // trim，可空；非空时校验 email
  phone: string     // trim，最大 32
  remark: string    // trim，最大 255
}
```

客户端不发送 userId、username、avatar、role、status、deptId。Gin 默认 JSON 绑定不会让额外字段进入 DTO；service 仍只使用白名单字段构造更新 map。

保存成功后页面调用现有 `authStore.fetchUserInfo()`，由 `@vben/stores` 更新全局用户信息，使页头昵称和个人中心摘要立即同步。

### 密码修改

`ProfilePassword` → `changeCurrentPasswordApi(input)` → `PUT /api/user/profile/password` → 从 Gin Context 读取 `identity.UserID` → UserService 校验旧密码 → UserRepository 在单事务内更新 bcrypt 密码并将该用户全部 Refresh Token 标记为 revoked。

请求体只含 `oldPassword`、`newPassword`；`confirmPassword` 只用于前端一致性校验，不发送后端。新密码沿用 6-32 字符规则。旧密码不匹配返回现有 `CodeWrongOldPassword`。

成功响应后前端调用现有 `authStore.logout()` 清理 access token、用户状态和 refresh cookie，并跳转登录页。由于 Access Token 是无状态 JWT，其他设备已持有的 Access Token 在既有 TTL 内仍可使用；其 Refresh Token 已失效，不能续期。

## 2. 后端设计

### 路由

在现有 `/api/user` 分组新增：

- `GET /api/user/profile`
- `PUT /api/user/profile`
- `PUT /api/user/profile/password`

三个 handler 都必须通过 `middleware.IdentityFromContext` 获取用户 ID。GET 遵循现有“未登记读接口仅需认证”规则；两个 PUT 通过新增的权限中间件 API 显式登记为 authenticated-write 路由。

### 权限中间件

为 `PermMiddleware` 增加独立的认证写路由集合及 `RegisterAuthenticated(method, path)`。处理顺序：

1. 已注册权限码的路由按权限码检查。
2. 已注册 authenticated-write 的路由在 AuthMiddleware 已建立身份后直接放行。
3. 其他未注册写路由继续默认拒绝。

该机制只声明“任何有效登录身份可调用”，业务对象边界仍由 handler 强制绑定 `identity.UserID`。不把个人中心路由加入公共认证白名单，也不赋予普通用户管理员权限码。

### Service 与 Repository

扩展现有 `UserService`，新增：

- `GetCurrentProfile(ctx, userID)`
- `UpdateCurrentProfile(ctx, userID, input)`
- `ChangeCurrentPassword(ctx, userID, input)`

扩展 `UserRepository`，新增原子操作 `ChangePasswordAndRevokeSessions(ctx, userID, passwordHash)`。事务内：

1. 更新 `users.password`。
2. 更新 `refresh_tokens` 中 `user_id = ? AND revoked = false` 的记录。

资料更新继续复用现有 `Update`，但 service 只传资料白名单字段。无需修改数据库模型、迁移或 wire 构造函数。

### 错误与响应

- 参数校验失败：`CodeInvalidParam`。
- 旧密码错误：现有 `CodeWrongOldPassword`。
- 认证身份缺失或用户不存在：`CodeUnauthorized`。
- 其他仓储错误沿用统一错误映射/中间件，不泄露内部细节。
- 成功均使用 `response.Success` 和 `{code,data,message}` 契约。

## 3. 前端设计

### API 类型

在 `api/core/user.ts` 的 `UserApi` 命名空间增加 `CurrentProfile`、`UpdateCurrentProfileInput`、`ChangeCurrentPasswordInput`，并导出对应三个 API 函数。继续复用唯一的 `requestClient`。

### 页面

- `profile/index.vue` 只保留基本设置、修改密码两个标签，并使用三语 i18n 文案。
- `base-setting.vue` 移除模拟角色选项；表单展示只读用户名，编辑昵称、邮箱、手机号和个人简介；头像由外层 Profile 使用现有用户信息展示，不增加 URL 输入。
- `password-setting.vue` 接入真实 API；成功后显示提示并退出登录。
- 提交函数使用本地 submitting guard 防止重复请求；表单 API 提供 loading/disabled 状态时同步到控件，否则至少保证不会发出重复请求。
- 初始化和提交错误交由现有 request client/antd 反馈链处理，不吞错；成功提示使用 i18n 文案。

### 国际化

在 zh-CN、en-US、zh-TW 的页面语言文件新增个人中心字段、标签、校验和成功提示。移除本次触达页面中的硬编码中文。

## 4. 测试策略

### 后端

- Service：读取 DTO 字段；更新仅改变白名单字段；错误旧密码不更新；正确旧密码更新 bcrypt 且吊销全部 Refresh Token。
- Repository：密码更新与令牌吊销事务行为。
- Middleware/router：普通登录用户可调用两个自服务 PUT；未认证返回 401；其他未登记写路由仍返回 403。
- API：请求体无法指定其他 userId，资料往返正确，错误码与响应结构正确。

### 前端

优先为页面提交行为添加聚焦 Vitest 测试；至少由类型检查覆盖 API 契约，并人工验证：资料回填、保存后页头同步、重复点击不重复请求、错误旧密码提示、改密成功退出。

## 5. 兼容与回滚

- 不改变 `/api/user/info` 与管理员 `/api/user/:id` 契约。
- 不修改表结构，回滚只需移除新增路由、service/repository 方法和前端调用。
- authenticated-write 注册必须有回归测试，避免扩大未注册写接口的访问范围。
