# Hook 规范

> 本项目如何使用 hooks。

---

## 概览

组合式逻辑大量复用 vben 核心包（`@vben/hooks`、`@vben/stores`、`@vben/preferences`、`@vben/common-ui`）：
例如 `useAppConfig` 读取 env/api 配置，`useAccessStore` / `useUserStore` 暴露认证状态，`useVbenModal` / `useVbenDrawer` 驱动弹窗与抽屉，`useVbenVxeGrid` 驱动表格，`useVbenForm` 驱动表单。
共享的应用级有状态逻辑放在 Pinia Store（如 `store/auth.ts`），数据访问通过类型化 API 函数（`api/core/*`）。

---

## 常用组合式 API (Vben Built-ins)

- **弹窗与抽屉**：
  - 使用 `useVbenModal` 或 `useVbenDrawer`，结合 `connectedComponent` 实现解耦式挂载。
  - 父组件通过 `modalApi.setData(...)` 传递入参并 `modalApi.open()`；子弹窗通过 `modalApi.getData()` 接收入参并在提交时 `modalApi.lock()` / `modalApi.close()`。
- **表格与表单**：
  - 表格通过 `useVbenVxeGrid` 声明配置（`gridOptions`、`formOptions`、`gridEvents`），数据代理通过 `proxyConfig.ajax.query` 直接绑定 API 函数。
  - 表单通过 `useVbenForm` 声明 `schema` 与校验规则，通过 `formApi.getValues()` / `formApi.setValues()` 读写。

---

## 自定义 Hook 模式

- 当需要共享业务逻辑时，遵循 vben 约定：文件位于 `apps/web-antd/src/hooks/` 或组件内局部复用，命名为 `useXxx.ts`，使用 `<script setup>` 兼容的 composables（返回 refs/computed 的普通函数）。
- 当状态必须在全局或跨导航存活时，优先用 Pinia Store。
- 保持 hook 单一职责且可依赖注入；不要有隐藏的全局隐式依赖。

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
