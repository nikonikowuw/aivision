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
│       ├── adapter/         # UI 适配器（component、vxe-table、form）
│       ├── api/             # request client + 后端 API 模块
│       │   ├── request.ts   # 带拦截器的 RequestClient（单实例）
│       │   └── core/        # 每个领域一个文件：auth.ts、user.ts、role.ts、menu.ts、dept.ts、log.ts
│       ├── constants/       # 业务常量定义（system.ts 等）
│       ├── layouts/         # 布局组件（auth.vue、basic.vue）
│       ├── locales/         # i18n 多语言目录（langs/zh-CN、en-US、zh-TW）
│       ├── router/          # 守卫 + 动态路由加载（access.ts、guard.ts、routes/）
│       ├── store/           # 应用级 pinia stores（auth.ts → useAuthStore）
│       └── views/           # 页面组件，按业务模块分组（system/、_core/、dashboard/ 等）
├── packages/                # @vben/* 可复用包（types、stores、utils、effects、locales、…）
└── internal/                # 工具链配置（lint-configs、vite-config、tsconfig、…）
```

---

## 模块组织

- **API 模块**：`api/core/` 中每个领域一个文件（`auth.ts`、`user.ts`、`role.ts`、`menu.ts`、`dept.ts`、`log.ts`），各自导出一个 `namespace <Domain>Api`，含类型化 params/result 接口以及调用 `requestClient` / `baseRequestClient` 的异步函数。通过 `api/index.ts` 重新导出（`export * from './core'`）。
- **Stores**：`store/` 中的应用级 pinia stores（`auth.ts`）；框架 stores（access/user/preferences）来自 `@vben/stores`，不重复造轮子。
- **路由与权限**：`router/access.ts` 生成动态可访问路由表，`router/guard.ts` 执行守卫鉴权；基础路由在 `router/routes/core.ts`。
- **视图**：`views/<module>/<page>/` 下的页面，如 `views/system/user/`、`views/system/role/`、`views/system/menu/`、`views/system/dept/`、`views/system/log/`，遵循 vben 5.7 页面与 VxeTable/VbenForm 规范。
- **适配器**：`adapter/` 统一封装 UI 控件行为，包括表单组件适配 `adapter/form.ts` 与表格适配 `adapter/vxe-table.ts`。

---

## 命名约定

- `.ts` 文件：camelCase（`request.ts`、`auth.ts`）；`.vue` 组件：PascalCase。
- 路径别名：`#/` → `apps/web-antd/src`（应用代码），`@vben/*` → workspace
  包。优先从 `@vben/*` 导入，而不是深层的 `../../packages/...` 路径。
- API 函数：`<verb><Domain>Api`（`getUserInfoApi`、`loginApi`、
  `getAllMenusApi`）；类型集中在 `<Domain>Api` 命名空间下。

---

## 示例

- API 模块模式：`apps/web-antd/src/api/core/auth.ts`、`apps/web-antd/src/api/core/user.ts`
- Request client + 拦截器契约：`apps/web-antd/src/api/request.ts`
- 应用 store 模式：`apps/web-antd/src/store/auth.ts`
- 系统管理视图组件：`apps/web-antd/src/views/system/user/index.vue`
- UI 适配层扩展：`apps/web-antd/src/adapter/form.ts`、`apps/web-antd/src/adapter/vxe-table.ts`
