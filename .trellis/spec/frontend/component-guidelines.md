# 组件规范

> 本项目如何构建组件。

---

## 概览

UI 基于 vben-admin 5.7 + ant-design-vue。可复用的 UI/基础设施组件已随 `@vben/*`
包和 `@vben/adapter` 组件适配器提供——新业务代码组合使用它们，而非重新实现。
目前自定义代码很薄（api/store/router）；业务页面组件将随功能任务推进落到
`views/`。

---

## 组件结构

- 业务页面位于 `apps/web-antd/src/views/<module>/<page>/`，使用
  `<script setup lang="ts">`（vben/Vue 3 约定）。
- 使用 `@vben/` 积木——布局（`@vben/layouts`）、`@vben/common-ui` 组件
  （`page`、`tree`、`ellipsis-text`、`captcha`、…）、`@vben/request`、
  `@vben/stores`——而不是自己造轮子；只有应用专属的 UI 适配器
  （`apps/web-antd/src/adapter/component`）定制组件库。

---

## Props 约定

- 用 `defineProps`/`defineEmits` + TypeScript 泛型定义 props/emits（vben
  风格）；避免松散的 `any` prop 类型。
- 自定义组件上的 v-model 优先使用 `defineModel`（Vue 3.4+），与 vben
  代码库保持一致。

---

## 样式模式

- 样式使用 vben 的 Tailwind 方案（共享 `@vben/tailwind-config`）；新样式使用
  Tailwind 工具类和 vben 设计 token。
- 页面局部布局可接受 scoped 样式 / less，与 vben 使用 `@vben/styles` 的方式
  一致。不要引入第二套样式体系。

---

## 可访问性

- 依赖 ant-design-vue 的语义和 vben 内置的 a11y 处理；在 vben 暴露 i18n 的
  地方（菜单标题已如此——决策 17）把文案放进 i18n key（`locales/`），而不是
  硬编码中文。

---

## 表格与表单规范 (VxeTable & VbenForm)

### 1. 按钮原生属性约定
- 在表单适配层自定义按钮（如 `PrimaryButton` / `DefaultButton`）时，必须显式声明 `htmlType="button"`，防止在原生 `<form>` 内被浏览器默认为 `submit` 导致点击时触发整页刷新。

### 2. 表格时间列时区格式化
- 所有表格的时间列（如 `createdAt`、`updatedAt`）必须显式配置 `formatter: 'formatDateTime'`。
- 该格式化器内置联动 `@vben/stores` 的全局时区选择器，确保用户切换时区时时间自动转换且显示为 `YYYY-MM-DD HH:mm:ss`，避免裸展示原始 ISO-8601 字符串（如 `2026-08-16T16:19:16.772648+08:00`）。

### 3. 表格 Loading 遮罩范围与平滑过渡
- `vxe-grid` 容器默认的 `.vxe-grid.is--loading::before` 会覆盖整个 Grid（包括顶部的搜索表单）。必须通过样式禁用该全局伪元素遮罩，将 loading 作用域收敛在表格内容区域（`.vxe-grid--table-wrapper` / `.vxe-table`）。
- Loading 组件配置 `minLoadingTime: 200` 并使用半透明毛玻璃过渡，避免极快接口响应时的白屏闪烁。

### 4. 操作列国际化宽度

- VXE 全局表格启用 `showOverflow: true` 时，操作列必须显式设置 `showOverflow: false`，避免英文按钮文案超出单元格后被渲染为省略号。
- 操作列使用固定宽度时，按最长语言文案预留空间；用户管理、角色/菜单/部门管理、日志详情分别至少预留 `360/280/280/120px`，并在语言切换后验证所有按钮仍可见。

---

## 常见错误

- **在本地重新实现 `@vben/*` 基础设施**（认证流程、request client、访问
  控制）——复用这些包。
- **表格时间列未加 `formatter: 'formatDateTime'`**，导致显示原始 ISO 时间字符串且无法响应系统时区切换。
- **自定义表单按钮未设置 `htmlType="button"`**，导致表单查询/重置触发整页提交刷新。
- **硬编码路由/菜单标题** 而不是使用 i18n key。
- **通过深层 `../../packages/...` 路径导入** —— 使用 `@vben/*` 别名。
