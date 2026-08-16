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

## 常见错误

- **在本地重新实现 `@vben/*` 基础设施**（认证流程、request client、访问
  控制）——复用这些包。
- **硬编码路由/菜单标题** 而不是使用 i18n key。
- **通过深层 `../../packages/...` 路径导入** —— 使用 `@vben/*` 别名。
