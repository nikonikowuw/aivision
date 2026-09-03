# PRD: 人员管理页面 UI 与交互体验优化

## Goal

针对 Argus 边缘 AI 系统的核心底库——「人员管理」页面（`views/resource/person/`），进行全流程 UI/UX 体验重构。采用**现代 SaaS 后台 + 轻量 boil 拟态风格**（微漫反射阴影、柔白内高光、微凹卡槽、立体微触感按钮与果冻进度槽），解决“录入信息与人脸上传流程割裂”、“样本抽屉单列排版冗长”、“原图对比嵌套弹窗繁琐”、“主特征图缺乏直观置顶”等核心痛点，将页面打造为符合工业级视觉 AI 标准的高质感底库工作台。

## Confirmed Facts & Decisions

1. **改造范围与视觉风格决议**：
   - **全流程体验闭环**：包含 R1（人脸样本抽屉工作台）、R2（新增人员一体化首图录入）、R3（表格特征感知与识别记录业务联动）。
   - **视觉设计语言**：现代 SaaS 后台 + 轻量 boil 拟态风格，已在 `prototype.html` 中通过原型验证并定稿。
2. **技术栈与代码现状**：
   - 视图路径：`ui/apps/web-antd/src/views/resource/person/`
   - 核心文件：`index.vue`、`components/FaceThumbnail.vue`、`components/PersonFaceDrawer.vue`、`components/AddPersonModal.vue`（组件化抽离）
   - API 层：`ui/apps/web-antd/src/api/core/person.ts`（支持完整人员 CRUD、样本注册、设为主图、对齐/原图图片流）
3. **数据契约与架构事实**：
   - 单机边缘系统（嵌入式 SQLite + 本地 C++ 推理引擎，无多节点同步机制）。
   - 每位人员由 `personId` 与 `name` 构成，最多允许关联 10 张样本；每张样本含 512 维特征向量、`qualityScore`、`detectionScore`、`alignedFace`、`rawImage`。
   - 人员对外包含 `primaryFaceId` 字段。

## Requirements

### R1. 人脸样本抽屉升级为「样本工作室」（PersonFaceDrawer）
1. **双列相片墙画廊（Card Grid）**：
   - 由原单列改为 2 列响应式 Grid，一屏尽览 4~6 张样本，大幅减少纵向滚动。
2. **主特征图自动置顶与平滑飞升（Primary-First Ordering）**：
   - **永久置顶**：无论何时打开抽屉，主特征图始终自动排序在第 1 位（#1），佩戴金色徽标 `👑 算法主特征图 (置顶首位)` 与 Primary 高亮外框；
   - **交互置顶**：在任意非主图卡片点击「设为主图并置顶」，该卡片伴随平滑过渡动画飞升至首位，其余样本顺延；
   - **容错顺延**：删除主图时自动将剩余的第一张样本提升为主图并置顶。
3. **原位对比与灯箱预览（彻底剔除二级 Modal）**：
   - 卡片内提供「112×112 对齐特征图」与「原始图像」原位分段单选快速切换；
   - 点击图片直接唤起 Ant Design `<Image.PreviewGroup>` 全屏灯箱查看原图，支持滚轮缩放、平移与左右前后翻页。
4. **多选批量上传**：
   - 上传组件支持一次性多选拖拽/选择多张图片（`multiple: true`，受剩余配额约束），自动队列式注册并排在主图之后。

### R2. 新增人员「一步到位」流程闭环（AddPersonModal）
1. 重构新增人员弹窗（抽离为独立组件 `components/AddPersonModal.vue`），支持在输入 `personId` 和 `name` 的同时，**直接拖拽/选择首张人脸照片**。
2. 选图后前端即时呈现轻拟态圆角预览与「首选特征主图」金标，支持随时清除或重选。
3. 提交时串联：先创建人员实体，紧接着调用注册人脸接口，建档与主特征录入一次点击完成；若不选照片也允许创建基础档案。

### R3. 主表格感知强化与「识别记录」业务联动（Table Presentation）
1. **行内查询表单**：严格遵循 Vben 5 标准栅格水平行内布局（Label 在左、输入框在右），融入轻拟态输入凹槽与触感按钮。
2. **主图头像与未录底库告警**：
   - 拥有主图时展示微凸卡槽与金色立体微角标（★），悬浮提示可更换主图；
   - 未录人脸（`faceCount === 0`）时呈现橙色虚线警示框与感叹号，操作列高亮为「+ 补录人脸」，点击直达抽屉。
3. **样本健康度胶囊条**：
   - 将原纯文本优化为微凹槽体 + 发光渐变胶囊指示柱（0/10 待录入、1~7 进行色、8~10 饱和色）。
4. **一键直达「识别记录」**：
   - 操作列增加「识别记录」入口，一键带入 `personId` 路由跳转至 `/record/face?personId=xxx` 并自动过滤。

## Acceptance Criteria

- [x] 需求范围明确为全流程闭环（R1 + R2 + R3）。
- [x] 完成原型开发与样式定稿（`prototype.html`：现代 SaaS + 轻量 boil 拟态）。
- [x] 技术设计与交互规范定稿（`design.md`，包含主图自动置顶与平滑飞升交互）。
- [ ] 实现 `AddPersonModal.vue`：支持一体化输入与首图上传注册。
- [ ] 重构 `PersonFaceDrawer.vue`：双列相册画廊，主图自动排在第 1 位，点击任意图设为主图即刻平滑置顶；卡片内原位切换对齐/原图；集成 AntD `<Image.PreviewGroup>` 原生灯箱。
- [ ] 升级 `FaceThumbnail.vue`：支持金标与空底库虚线警告样式。
- [ ] 优化 `index.vue` 表格：表单对齐 Vben 5 标准，增加识别记录联动跳转与状态条。
- [ ] 优化 `views/record/face/index.vue`：挂载时自动提取 `route.query.personId` 预填并过滤。
- [ ] 前端运行 `pnpm check`（类型、ESLint、依赖、拼写）全部通过。

## Out of Scope

- 不修改后端数据库表结构及特征提取 C ABI 推理流程。
- 不引入外部臃肿第三方库，纯基于 Tailwind CSS + Ant Design Vue 原生体系实现轻拟态与触感交互。
