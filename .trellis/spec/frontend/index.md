# 前端开发规范

> 本项目前端开发的最佳实践（vben-admin 5.7 monorepo）。

---

## 概览

`ui/` 下 Vue 3 前端的规范（pnpm + turbo monorepo，业务应用
`apps/web-antd`）。工具强制执行的规则（oxlint/eslint/stylelint/typecheck/commitlint，
全部通过 lefthook + `pnpm check` 接入）在此不重复；这些文件记录工具无法推断的
约定。项目事实见 `AGENTS.md`。

---

## 规范索引

| 规范 | 说明 | 状态 |
| [目录结构](./directory-structure.md) | monorepo + 应用 src 布局、模块组织 | 生效 |
| [组件规范](./component-guidelines.md) | vben/antd 组合、props、样式 | 生效 |
| [国际化规范](./i18n-guidelines.md) | 多语言架构、三语对齐、文本与宽度契约 | 生效 |
| [Hook 规范](./hook-guidelines.md) | Composables、数据获取 | 生效 |
| [状态管理](./state-management.md) | Pinia、vben stores、服务端状态 | 生效 |
| [质量规范](./quality-guidelines.md) | 约定、测试、评审检查清单 | 生效 |
| [类型安全](./type-safety.md) | 类型组织、API 命名空间、禁止 `any` | 生效 |

---

## 开发前检查清单

编写前端代码前阅读以下内容：

- [ ] **目录结构** — 新代码放在哪里（`api/core/`、`store/`、`views/`、
      `router/routes/modules/`）以及 `#/` + `@vben/*` 别名：
      [directory-structure.md](./directory-structure.md)
- [ ] **API 层** — 在 `api/core/<domain>.ts` 中以类型化函数添加端点，
      使用共享 `requestClient`（code=0 契约）：[directory-structure.md](./directory-structure.md)、
      [type-safety.md](./type-safety.md)
- [ ] **状态** — 复用 `@vben/stores`；新的应用状态用 setup 风格 pinia store：
      [state-management.md](./state-management.md)
- [ ] **工具链** — lefthook 在提交时运行 oxlint/oxfmt/eslint/stylelint/typecheck；
      完成前运行 `pnpm check`：[quality-guidelines.md](./quality-guidelines.md)

另请阅读共享思考指南：`../guides/index.md`。

---

## 质量检查

完成前端工作前：

- [ ] `pnpm check` 通过（circular + dep + typecheck + cspell）。
- [ ] 新增 API 调用走 `api/core/` 并使用类型化命名空间；无额外的 request
      client 实例。
- [ ] 无重复 `@vben/stores` 状态的新 store；会话变更通过 `useAuthStore`。
- [ ] 无硬编码 i18n 字符串、无深层 `packages/` 导入、无无理由的
      `@ts-ignore` / 裸 `any`。
- [ ] 提交信息遵循 conventional commits。

---

**语言**：所有文档应使用**中文**编写。
