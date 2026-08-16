# 类型安全

> 本项目的类型安全模式。

---

## 概览

前端使用 TypeScript（严格模式，vben 5.7 基线），由 `vue-tsc` 做类型检查
（通过 `pnpm check:type` 强制）。共享领域类型来自 `@vben/types`
（`UserInfo`、`RouteRecordStringComponent`、`Recordable`、…）。API 模块在
`<Domain>Api` 命名空间中声明自己的请求/结果类型，使每个端点的契约与其函数
同处一地。

---

## 类型组织

- **共享领域类型**：从 `@vben/types` 导入（`UserInfo`），而不是重新声明。
- **API 类型**：`api/core/<domain>.ts` 中的按领域命名空间
  （`AuthApi.LoginParams`、`AuthApi.LoginResult`）——params 和 results 声明在
  使用它们的函数旁边。
- **视图本地类型**：小的局部接口放在组件附近；仅当跨模块共享时才提升到
  `@vben/types`。

---

## 校验

- 目前未使用运行时 schema 校验库（无 zod/yup）；信任后端 `code`/`message`
  契约和 `request.ts` 中 `code=0` 的成功拦截器。
- 传给 vben/antd 组件的表单数据按 vben 约定使用 `Recordable<any>`
  （例如 `store/auth.ts` 的 `authLogin(params: Recordable<any>)`）——这是通用
  表单负载的既定模式，不是其他地方不加检查类型的通行证。

---

## 常见模式

- 带显式结果类型的泛型请求函数：
  `requestClient.get<RouteRecordStringComponent[]>('/menu/all')`。
- 类型专用导入使用 `import type`（vben 约定）。

---

## 禁止模式

- 新业务代码中的**裸 `any`**，vben 泛型 `Recordable<any>` 表单负载除外。
- **用 `as unknown as X` / 类型断言掩盖不匹配** —— 修复类型或 API 契约本身。
- **重新声明 `@vben/types` 中已存在的类型**。
- **无文档理由的 `@ts-ignore`**（评审中会标记）。
