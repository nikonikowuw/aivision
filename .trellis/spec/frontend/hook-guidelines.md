# Hook 规范

> 本项目如何使用 hooks。

---

## 概览

组合式逻辑来自 vben 包（`@vben/hooks`、`@vben/stores`、`@vben/preferences`）：
例如 `useAppConfig` 读取 env/api 配置，`useAccessStore` / `useUserStore` 暴露
认证状态。目前尚无项目本地自定义 hook；共享的有状态逻辑迄今都放在 pinia
store 中（见 `store/auth.ts`），数据访问通过 API 函数而非 hooks。

---

## 自定义 Hook 模式

- 当需要共享组合式逻辑时，遵循 vben 约定：文件位于 `apps/web-antd/src`，
  命名为 `useXxx.ts`，使用 `<script setup>` 兼容的 composables（返回
  refs/computed 的普通函数）。
- 当状态必须在无关组件间共享时，优先用 pinia store 而非自定义 hook
  （本项目迄今的模式）。
- 保持 hook 单一职责且可依赖注入；不要有隐藏的全局单例。

---

## 数据获取

- 服务端数据通过 `api/core/` 中的类型化 API 函数（`requestClient`）获取，
  而不是通过数据获取库（无 React Query/SWR —— 这是 Vue，且 vben 提供了
  自己的请求层）。
- 从 store 或 views 触发调用；响应契约为 `{code,data,message}`，`code=0`
  表示成功（见 `api/request.ts` 拦截器）。

---

## 命名约定

- 组合式函数以 `use` 开头（`useAppConfig`、`useAuthStore`、…）——vben 约定；
  新函数保持。
- Store hooks 命名为 `use<Store>Store`（例如 `useAuthStore`）。

---

## 常见错误

- **重新发明** `@vben/hooks` 和 `@vben/stores` 已提供的认证/请求 composables。
- **在组件中直接获取数据** 而不经过类型化的 `api/core/` 模块——这样端点
  和响应类型都在一处维护。
- **把服务端状态放在组件本地 ref 中** 当它必须跨导航存活时——使用
  pinia store。
