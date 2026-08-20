# 个人中心资料与密码修改实施计划

## 1. 后端契约与权限

- 在 `internal/service/user.go` 定义个人资料 DTO、资料更新入参、密码修改入参及 `UserService` 方法。
- 在 `internal/api/user.go` 新增读取本人资料、更新本人资料、修改本人密码 handler，统一从认证上下文取用户 ID。
- 在 `internal/middleware/perm.go` 增加 authenticated-write 路由登记能力，并补默认拒绝不被削弱的测试。
- 在 `internal/router/router.go` 注册 `/api/user/profile` 与 `/api/user/profile/password` 路由及 authenticated-write 声明。
- 更新 Swagger 注释并运行项目现有 Swagger 生成命令（若 Makefile 提供），确认生成文件只包含预期接口变更。

验证：普通已认证用户可访问自服务写接口，未认证为 401，任意其他未登记写接口仍为 403。

## 2. 后端业务与事务

- 实现个人资料读取和白名单更新，规范化字符串并映射仓储错误。
- 在 `UserRepository` 实现“更新密码 + 吊销该用户全部 Refresh Token”的单事务方法。
- 修改密码时先读取用户并用 bcrypt 校验旧密码，再生成新 hash 并执行事务；旧密码错误使用 `CodeWrongOldPassword`。
- 添加 service、repository、API/路由聚焦测试，覆盖越权字段、错误旧密码、正确改密、全部 Refresh Token 吊销和原子性。

验证（`app/`）：

```bash
gofmt -w internal
make vet
make test
```

## 3. 前端 API 与页面

- 在 `apps/web-antd/src/api/core/user.ts` 增加个人资料和密码类型化 API。
- 重构 `views/_core/profile/base-setting.vue`：移除模拟角色，加入只读用户名与昵称/邮箱/手机号/简介字段，接入读取、保存、成功提示和全局用户信息刷新。
- 重构 `views/_core/profile/password-setting.vue`：接入真实改密 API，保留两次密码一致校验，成功后退出到登录页。
- 更新 `views/_core/profile/index.vue`：仅保留基本设置和修改密码。
- 删除本次不再引用的静态 security/notification 页面文件（仅在确认无其他引用后）。
- 为 zh-CN、en-US、zh-TW 增加个人中心文案，页面不新增硬编码用户可见文本。
- 对异步提交增加防重 guard，并在组件能力允许时展示 loading/disabled 状态。

验证（`ui/`）：

```bash
pnpm check
pnpm test:unit
```

## 4. 集成验收

- 登录普通用户，确认个人中心可进入且只显示两个标签。
- 修改昵称、邮箱、手机号、简介，确认刷新后持久化且页头昵称立即同步。
- 用开发者工具构造包含 userId/username/role/status/deptId/avatar 的请求，确认这些字段不能修改。
- 错误旧密码返回本地化错误且数据库不变。
- 正确修改密码后回到登录页；旧密码登录失败，新密码登录成功；数据库中该用户全部 Refresh Token 已吊销。
- 分别切换 zh-CN、en-US、zh-TW，确认新增文案和表单布局正常。

## 5. 风险与回滚点

- 修改 `PermMiddleware` 后先运行其中间件回归测试；若 authenticated-write 匹配范围异常，立即回滚该登记机制，不开放路由。
- 密码事务测试未通过时不接入前端退出流程，也不将任务标记完成。
- 不修改用户表或令牌表结构，无数据库迁移回滚要求。
- 不触碰当前工作区中与本任务无关的登录页、认证布局和偏好设置改动。
