# 目录结构

> 本项目前端代码的组织方式。

---

## 概览

前端是基于 vben-admin 5.7.0 的 `ui/` 下的 pnpm + turbo monorepo。唯一的业务
应用是 `apps/web-antd`（其他 vben 应用正在裁剪）。基础设施位于 `packages/*`
和 `internal/*`，作为 `@vben/*` workspace 包——业务代码导入它们但不修改它们。
可自定义的面是 `apps/web-antd/src`：`api/`、`store/`、`router/`、`views/`、
`locales/`。

---

## 目录布局

```
ui/
├── apps/web-antd/
│   └── src/
│       ├── api/             # request client + 后端 API 模块
│       │   ├── request.ts   # 带拦截器的 RequestClient（单实例）
│       │   └── core/        # 每个领域一个文件：auth.ts、user.ts、menu.ts
│       ├── store/           # 应用级 pinia stores（auth.ts → useAuthStore）
│       ├── router/          # 守卫 + 路由定义（routes/modules/*.ts）
│       ├── views/           # 页面组件，按模块分组
│       ├── locales/         # i18n（菜单标题引用这里的 key）
│       ├── adapter/         # UI 适配器（ant-design-vue）
│       └── layouts/         # 布局组件
├── packages/                # @vben/* 可复用包（types、stores、utils、…）
└── internal/                # 工具链配置（lint-configs、vite-config、tsconfig、…）
```

---

## 模块组织

- **API 模块**：`api/core/` 中每个领域一个文件（`auth.ts`、`user.ts`、
  `menu.ts`），各自导出一个 `namespace <Domain>Api`，含类型化
  params/result 接口以及调用 `requestClient` / `baseRequestClient` 的异步函数。
  通过 `api/index.ts` 重新导出（`export * from './core'`）。
- **Stores**：`store/` 中的应用级 pinia stores（`auth.ts`）；框架 stores
  （access/user/preferences）来自 `@vben/stores`，不重新实现。
- **路由**：`router/routes/modules/` 中的路由模块（`vben.ts`、`dashboard.ts`、
  `demos.ts`）；守卫在 `router/guard.ts`。
- **视图**：`views/<module>/<page>/` 下的页面，遵循 vben 约定。

---

## 命名约定

- `.ts` 文件：camelCase（`request.ts`、`auth.ts`）；`.vue` 组件：PascalCase。
- 路径别名：`#/` → `apps/web-antd/src`（应用代码），`@vben/*` → workspace
  包。优先从 `@vben/*` 导入，而不是深层的 `../../packages/...` 路径。
- API 函数：`<verb><Domain>Api`（`getUserInfoApi`、`loginApi`、
  `getAllMenusApi`）；类型集中在 `<Domain>Api` 命名空间下。

---

## 示例

- API 模块模式：`apps/web-antd/src/api/core/auth.ts`
- Request client + 拦截器契约：`apps/web-antd/src/api/request.ts`
- 应用 store 模式：`apps/web-antd/src/store/auth.ts`
