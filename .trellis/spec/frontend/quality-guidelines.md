# 质量规范

> 前端开发的代码质量标准。

---

## 概览

Lint/格式化/类型检查**已由 lefthook 在 pre-commit 自动强制执行**
（oxlint `--fix --type-aware`、oxfmt、eslint `--fix`、stylelint `--fix`、
`pnpm check:type`），commit-msg 上的 commitlint 以及 `pnpm check` 流水线
（circular deps、dep check、typecheck、cspell）也是如此。这些文件不重复
那些工具规则；它们记录工具无法推断的约定。

---

## 禁止模式

- **在本地重新实现 `@vben/*` 基础设施**（request client、认证 store、访问
  控制、通用 UI）。
- **深层导入 `packages/...`** —— 使用 `#/` 和 `@vben/*` 别名。
- **存在 i18n key 时硬编码面向用户的字符串**（菜单标题、页面文案）。
- **在共享 `request.ts` client 之外做一次性请求/错误处理**。
- **前端维护自己的错误码表 / 按 `code` 自行翻译文案** —— 业务错误码统一由后端
  `errno` 管理；前端直接展示后端 `message`，不重复建错误码映射。
- **裸魔数** —— 有业务语义的数字（状态值、超时、分页大小、宽高等）直接以字面量
  散落在业务代码中，而不是使用命名常量。

---

## 必用模式

- **必须包含完备的代码注释**：组件、Store、API 函数及复杂交互逻辑（如异步数据流、动态路由权限、响应式状态派生）必须包含清晰的中文注释，解释设计意图与边界约束。
- 后端调用走 `api/core/` 中的类型化 API 模块（每个领域单一端点 +
  类型来源），通过 `#/api` 导入。
- 应用状态遵循 store 模式（`store/auth.ts`）：setup 风格 pinia store，
  编排逻辑在 store 内部，token 通过 `@vben/stores`。
- 新页面代码位于 `views/<module>/<page>/`，使用 `<script setup lang="ts">`。
- 面向用户的可见文本把 i18n key 加到 `locales/`，而非内联字面量。
- **有业务语义的数字使用命名常量**（如状态值、超时、分页大小）；与后端契约对齐的
  值（`code=0` 成功、401/403）复用 `request.ts` / `@vben/constants` 中的既有常量，
  不要裸写数字。
- **错误提示直接使用后端 `message`**（`request.ts` 的 `errorMessageResponseInterceptor`
  已统一处理），不要在前端按错误码另行翻译或硬编码错误文案。

---

## 测试要求

- 单元测试使用 vitest 运行：`pnpm test:unit`（`vitest run --dom`）。
- 目前尚无项目专属前端测试；添加时优先测试纯逻辑（stores/工具函数）并保持
  快速（`--dom` happy-dom）。
- 完成前端工作前 `pnpm check` 流水线必须通过。

### 并行测试隔离

- 测试写入外部化单例或共享状态（例如 `globalShareState`）后，必须在同一同步区间内恢复默认值；不能依赖测试文件串行执行。
- 使用 Vue Test Utils 挂载组件的测试必须在 `afterEach` 卸载 wrapper；卸载后仍有异步 watcher、校验或 debounce 时，需等待其完成，避免任务泄漏到下一个测试。
- 异步校验、提交回调或 DOM 更新不能只依赖一次 `flushPromises` 的时序假设；最终断言使用 `vi.waitFor` 等待可观察结果。
- 不得通过全局关闭文件并行（如 `fileParallelism: false`）掩盖共享状态或异步清理问题；应在产生污染的测试边界恢复状态。

---

## 代码评审检查清单

- API 新增遵循 `api/core/<domain>.ts` 命名空间模式并复用 `requestClient`；
  无新的 request client 实例。
- 无重复 `@vben/stores` 状态的新 store；会话变更通过 `useAuthStore`。
- 无硬编码 i18n 字符串；无深层 `packages/` 导入；无无理由的 `@ts-ignore`。
- 提交遵循 conventional commits（由 commitlint 强制）。
- 无裸魔数——业务数字均使用命名常量，与后端契约对齐的值复用既有常量。
- 无前端错误码表 / 按 `code` 翻译文案——错误提示直接展示后端 `message`。
