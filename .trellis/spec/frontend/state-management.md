# 状态管理

> 本项目如何管理状态。

---

## 概览

状态管理使用 **Pinia**。框架状态（access token、用户信息、应用偏好、locale）
由 vben 的 `@vben/stores`（`useAccessStore`、`useUserStore`）和 `@vben/preferences`
提供。应用专属状态放在 `apps/web-antd/src/store/` 下的 setup 风格 pinia
stores 中（当前为 `auth.ts` → `useAuthStore`）。没有专门的服务器状态缓存库；
API 响应在使用处直接消费。

---

## 状态分类

- **框架/全局**：access token + 用户信息 + 权限码通过 `@vben/stores`；偏好
  通过 `@vben/preferences`。不要重复这些。
- **应用级**：会话流程（登录 loading、登出、刷新）在 `useAuthStore`
  （`store/auth.ts`）中。
- **本地**：瞬时 UI 状态用组件本地 ref；仅在跨组件共享或需要跨导航存活时
  才提升为 store。

---

## 何时使用全局状态

- 状态被多个无关组件读写，或需要跨路由导航存活（会话、用户、偏好）。
- 否则保持状态在组件本地（`ref`/`computed`）；避免过早创建 store。

---

## 服务端状态

- 通过类型化的 `api/core/` 函数获取，并在使用处保存结果（会话/用户数据放
  store，页面数据放 views）。
- Token 生命周期集中在 `api/request.ts` 拦截器 + `useAuthStore` 中（自动刷新、
  401 重新认证）——不要在每页重新实现。

---

## 常见错误

- **在第二个地方存储 access token / 用户信息** —— 始终用 `@vben/stores`
  （通过 `useAccessStore` / `useUserStore`）。
- **为单组件状态创建新的 pinia store**。
- **手动硬重置认证状态** —— 像 `store/auth.ts` 那样使用提供的重置辅助函数
  （`resetAllStores`）。
