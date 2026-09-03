# 实施计划: 人员管理页面 UI 与交互体验优化

## 实施阶段与步骤

### 阶段 1：基础组件增强与新增人员一体化弹窗 (AddPersonModal)
- [ ] 步骤 1.1：增强 `FaceThumbnail.vue`，支持 `isPrimary`（金标角标）、`emptyWarning`（无样本虚线警示槽）与轻拟态微边框。
- [ ] 步骤 1.2：新建 `components/AddPersonModal.vue`，实现左侧微凹卡槽即时预览（带首选主图金标）+ 右侧人员 ID/姓名表单的一体化界面与提交串联逻辑。
- [ ] 步骤 1.3：补充相关 i18n 国际化键值（`resource.json`），保证规范对齐。

### 阶段 2：人脸管理抽屉全面升级 (PersonFaceDrawer)
- [ ] 步骤 2.1：将单列卡片结构重构为 2 列响应式 Card Grid，优化抽屉内空间利用率。
- [ ] 步骤 2.2：实现主特征图永久自动排序在首位（#1），支持点击任意卡片「设为主图」平滑飞升置顶与动画；卡片内集成「112×112 对齐图 / 原始图」原位分段切换。
- [ ] 步骤 2.3：彻底移除内部的二级 Modal 预览弹窗，接入 Ant Design `<Image.PreviewGroup>` 全屏灯箱预览。
- [ ] 步骤 2.4：升级上传组件为批量多图选择（`multiple: true`），加入前端配额校验与排队上传反馈。

### 阶段 3：主表格视觉优化与抓拍业务联动 (Table & Integration)
- [ ] 步骤 3.1：在 `index.vue` 中挂载 `AddPersonModal` 替换原基础 Modal。
- [ ] 步骤 3.2：表格头像列应用增强的 `FaceThumbnail`（未录人脸显示警告与补录引导）。
- [ ] 步骤 3.3：表格样本数列优化为带色彩梯度的质量/数量状态条。
- [ ] 步骤 3.4：表格操作列增加「识别记录」按钮，使用 `router.push({ path: '/record/face', query: { personId } })`。
- [ ] 步骤 3.5：检查并优化 `views/record/face/index.vue`，支持在进入时自动拾取 `route.query.personId` 进行过滤。

### 阶段 4：质量检查与端到端验证
- [ ] 步骤 4.1：执行 `pnpm check`（类型安全、无裸 any、依赖与无循环引用）。
- [ ] 步骤 4.2：启动前端 `pnpm dev` 验证各分辨率下 UI 视觉与交互动画体验。

## 验证命令

```bash
# 进入前端工程目录
cd ui

# 静态检查 (类型、ESLint、拼写等)
pnpm check

# 单元测试 (若有)
pnpm test:unit
```
