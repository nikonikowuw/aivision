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

### Api* 包装组件契约

这组契约适用于 `ApiCascader`、`ApiSelect` 和 `ApiTreeSelect`，因为它们都经过 `ApiComponent` 和应用组件适配器两层包装。

#### 1. 范围 / 触发

- **触发条件**：业务页面使用 `Api*` 组件承载接口返回的选项或树数据，并将组件注册到 Vben Form。
- **边界**：业务字段映射由 `ApiComponent` 负责；表单模型事件由 `apps/web-antd/src/adapter/form.ts` 负责；antd 组件只接收规范化后的 props。

#### 2. 签名

`ApiComponent` 的关键 props 契约：

```ts
interface ApiComponentProps {
  labelField?: string;
  valueField?: string;
  childrenField?: string;
  modelPropName?: string;
  options?: ApiComponentOptionsItem[];
}
```

应用适配层的表单模型契约：

```ts
setupVbenForm<ComponentType>({
  config: {
    baseModelPropName: 'value',
    modelPropNameMap: {
      ApiCascader: 'modelValue',
      ApiSelect: 'modelValue',
      ApiTreeSelect: 'modelValue',
    },
  },
});
```

外层 `Api*` 组件使用 `modelValue/onUpdate:modelValue`；内部 ant-design-vue 组件使用 `value/onUpdate:value`。

#### 3. 契约

- 原始树节点必须提供 `valueField` 指向的值、`labelField` 指向的文本，以及可选的 `childrenField` 子节点数组。例如部门节点为 `{ id: number, name: string, children?: DeptItem[] }`。
- `ApiComponent` 必须将原始节点转换为 `{ value, label, children }` 后再传给 TreeSelect/Cascader；业务页面不应将原始字段名传给底层 `fieldNames`。
- 表单提交前，表单值必须是选中节点的 `value`（部门场景为 `deptId: number | undefined`），而不是完整节点对象或显示名称。
- 本契约不新增环境变量，也不改变 `/dept/tree`、`/user` API 的请求/响应字段。

#### 4. 校验与错误矩阵

| 条件 | 预期配置/行为 | 可观察结果 |
| --- | --- | --- |
| 原始字段为 `id/name/children` | `valueField: 'id'`、`labelField: 'name'`、`childrenField: 'children'` | 节点显示名称，点击后提交对应 ID |
| `Api*` 注册到 Vben Form | `modelPropNameMap[Api*] = 'modelValue'` | `onUpdate:modelValue` 回写表单值，切换后不会恢复旧值 |
| 把原始字段写入底层 `fieldNames` | 禁止，例如 `fieldNames: { label: 'name', value: 'id' }` | 转换后的节点字段无法匹配，名称或选中值为空；通常不会抛异常 |
| 未配置 `modelPropNameMap` | 禁止使用默认的 `value/onUpdate:value` 绑定包装组件 | 点击可能暂时改变视觉状态，但表单值不变，重新渲染后恢复旧值 |
| `valueField` 对应值不存在 | API 数据或映射错误 | TreeSelect 无法回显该值；应修正数据契约或字段映射，不在页面手动拼显示文本 |

#### 5. 正确 / 基准 / 错误案例

- **正确**：页面只描述接口字段，适配器负责底层字段与模型协议。

  ```ts
  component: 'ApiTreeSelect',
  componentProps: () => ({
    childrenField: 'children',
    labelField: 'name',
    options: deptTreeOptions.value,
    valueField: 'id',
  }),
  fieldName: 'deptId',
  ```

- **基准**：直接使用已经规范化的选项时，底层组件接收 `label/value/children`，外层表单仍通过 `modelValue` 绑定。
- **错误**：页面同时设置原始 `fieldNames`，或让 `ApiTreeSelect` 继承 antd 的 `value` 表单绑定，绕过包装组件的外层模型协议。

#### 6. 必需测试

- `ApiComponent` 单元测试：断言 `id/name/children` 被转换为 `value/label/children`，并模拟 `update:value` 后断言外层值更新。
- Vben Form 集成测试：使用名为 `ApiTreeSelect` 的包装组件，断言触发 `update:modelValue` 后 `formApi.getValues().deptId` 等于新节点 ID。
- 前端质量门禁：运行 `pnpm check`、相关 Vitest 测试、`oxlint` 和 `oxfmt`；断言不应只检查节点是否渲染，还必须检查表单模型是否改变。

#### 7. 错误与正确对照

**错误：**

```ts
// ApiComponent 已经把节点转换为 { label, value, children }
fieldNames: { label: 'name', value: 'id', children: 'children' },
```

**正确：**

```ts
// 业务字段交给 ApiComponent 转换，底层适配器保持统一字段
labelField: 'name',
valueField: 'id',
childrenField: 'children',
// 表单适配层：ApiTreeSelect -> modelValue
modelPropNameMap: { ApiTreeSelect: 'modelValue' },
```

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

### 5. 批量操作与选中激活提示条 (Selection Alert Bar)

- 表格首列必须配置 `{ type: 'checkbox', width: 50, align: 'center' }`。
- 复选框必须在 `checkboxConfig.checkMethod` 中对受保护行（如 `admin` 用户、`super/admin` 角色）进行禁用，防止非法勾选。
- 批量操作区域采用“选中激活提示条”交互模式：未选中时不展示常驻置灰按钮；勾选 1 项及以上时，在表格上方弹出提示条，展示选中数量、清空操作链接及带 Popconfirm 二次确认的批量操作按钮。
- `useVbenVxeGrid` 的 `gridEvents` 需绑定 `checkboxAll` 与 `checkboxChange` 事件（显式标注 `{ records }: { records: T[] }` 类型），操作成功或清空时需调用 `gridApi.grid?.clearCheckboxRow()` 保持状态同步。

### 6. VxeGrid 展开行（Expand Row）契约与控制器激活

#### 1. 范围 / 触发
- **触发条件**：表格需要展开子行呈现复杂下钻信息（如 1:N 实例控制台、日志详情、子参数列表）。
- **边界**：必须由 `gridOptions` 显式配置 `expandConfig` 与 `rowConfig`，仅在列定义中配置 `type: 'expand'` 无法激活 VxeTable 的展开控制器。

#### 2. 签名与配置

```ts
const gridOptions: VxeTableGridOptions<T> = {
  rowConfig: {
    keyField: 'cameraId', // 必须提供确定唯一的业务字段，供 VxeTable 跟踪行展开状态映射
  },
  expandConfig: {
    trigger: 'cell', // 'cell' 点击展开列单元格触发展开；或 'default' 仅点击箭头
    showIcon: true,
    reserve: true,   // 必须开启状态保留，配合 query 避免数据更新时展开行意外收起
  },
  columns: [
    {
      type: 'expand',
      width: 48,
      align: 'center',
      slots: { content: 'expandContent' }, // 插槽名须与 Grid 内部 template #[name] 一致
    },
    // ...
  ],
};
```

#### 3. 契约与防折叠/防重置规则
- 必须提供 `rowConfig.keyField`，否则 VxeTable 使用动态生成的 `_X_ROW_KEY`，在表格数据更新时会导致展开状态丢失或定位混乱。
- 必须声明 `expandConfig: { reserve: true }`，配合 `keyField` 在底层建立持久化展开映射表 `rowExpandedMaps`。
- **避免 `gridApi.reload()` 导致展开强制清空**：
  - VxeTable 的 Proxy 机制中，`commitProxy('reload')` 会在发起请求前同步执行 `$xeTable.clearAll()`，该方法会强行重置所有行展开（`clearRowExpand()`）。
  - 因此，数据保存、开关启停或刷新当前列表时，**必须优先调用 `gridApi.query()` 而非 `gridApi.reload()`**；`reload` 仅用于新增数据返回第一页或重置查询等需要重置全表状态的场景。
- 插槽名称通过 `slots: { content: 'slotName' }` 指定，在 `<Grid>` 内部通过 `<template #slotName="{ row }">` 承载展开容器。

### 7. 1:N 关系的流式管道展示规范 (Pipeline Strip)

- **行内紧凑态 (Compact Pipeline)**：
  - 避免在列表中使用无信息的纯链接按钮（如单列“算法实例”文字链接）或参差不齐的单纯 Tag 堆叠。
  - 使用硬件槽位式胶囊（Pipeline Capsule）并排呈现，内置状态呼吸指示灯、算法名、实时性能（如 `当前FPS / 设定FPS`）与就地微型 Switch 启停开关，尾部提供虚线 `+` 快捷挂载入口。
- **展开沉浸态 (Instance Console)**：
  - 展开区域采用当前系统语义背景（`bg-muted/25 border-y border-border`），内部使用响应式卡片网格展示子实体详情。
  - 卡片内部直观展示进度仪表（如 `Progress` FPS 吞吐条）、防区规则数与状态。
  - 在卡片底部提供直接的就地编辑与删除操作，且在控制台头部右上角提供直接销毁当前父实体的危险动作按钮，消除深层操作路径障碍。

### 8. 操作列防溢出与最小宽度规范

- 当操作列包含 3 个及以上文字链接按钮时，宽度必须精确计算，禁止使用默认的 `120px~160px` 导致最右侧高危按钮（如“删除”）被固定列截断。
- 文本长度精简规则：使用两字动作动词（如 `编辑`、`删除`、`抽屉`），避免冗余后缀（`编辑任务`、`删除任务`）。
- 包含 3 个动作（含 Popconfirm 二次确认）的操作列宽度**至少为 `200px`**，并显式指定 `fixed: 'right'` 与 `align: 'center'`。

### 9. 瞬态状态高频轮询与静默就地 Patch 规范 (Silent Polling & In-Place Patch)

#### 1. 范围 / 触发
- **触发条件**：列表中包含异步过渡状态（如任务/实例从 `STARTING` 到 `RUNNING` 的短暂冷启动状态）或实时性能吞吐指标（如动态 FPS），需要在短时间内（如 1s 间隔）高频轮询更新状态。

#### 2. 核心原则
- **坚决禁止在高频轮询中调用 `gridApi.reload()` 或 `gridApi.query()`**：
  - VxeTable 的 Proxy 机制在执行 `query` 时，会在前后自动切换 `reactData.tableLoading = true/false` 并调用 `$xeTable.recalculate()` 重新计算容器尺寸。
  - 高频触发会导致表格出现明显的页面闪烁、Loading 遮罩微动与视觉重绘抖动。
- **采用静默请求 + 内存就地 Patch (In-Place Patch)**：
  - 通过独立的 API 函数（如 `getTaskListApi`）发起静默 HTTP 请求。
  - 通过 `gridApi.grid?.getData()` 获取当前页表格内存中的响应式对象数组。
  - 遍历匹配唯一键，直接就地更新易变字段（如 `actualStatus`、`currentFps`、`desiredEnabled`、`instances` 等）。
  - 利用 Vue3 细粒度响应式系统触发局部 DOM 补丁，只有指示灯颜色和 FPS 数值平滑跳动，实现 **零 Loading 遮罩、零布局抖动、零展开行收起**。
- **自动休眠契约 (Auto Sleep)**：
  - 轮询函数必须包含终止条件（如当前列表内所有项已脱离 `STARTING` 状态，或超出最大轮询时间如 15 秒）。
  - 一旦满足终止条件，必须立即调用 `clearInterval` 销毁定时器，避免常驻无意义的边缘网络和 CPU 资源消耗。

#### 3. 示例代码

```ts
// 轮询拉取并静默就地更新状态
async function fetchStatusSilently() {
  try {
    loadStats(); // 同步更新顶部统计
    const formValues = (await gridApi.formApi.getValues?.()) ?? {};
    const tablePage = gridApi.grid?.getProxyInfo?.()?.pager || {
      currentPage: 1,
      pageSize: 10,
    };

    const res = await getTaskListApi({
      page: tablePage.currentPage,
      pageSize: tablePage.pageSize,
      name: formValues.name || undefined,
    });

    // 检查瞬态是否全部收敛，若是则休眠
    const hasStarting = res.items?.some((t) => t.actualStatus === 1);
    if (!hasStarting) {
      stopPolling();
    }

    // 就地 Patch 避免触发 VxeTable loading 与全量重排
    const currentRows = gridApi.grid?.getData?.() ?? [];
    if (currentRows.length > 0 && res.items?.length > 0) {
      const freshMap = new Map(res.items.map((item) => [item.cameraId, item]));
      for (const row of currentRows) {
        const fresh = freshMap.get(row.cameraId);
        if (fresh) {
          row.actualStatus = fresh.actualStatus;
          row.currentFps = fresh.currentFps;
          row.instances = fresh.instances;
        }
      }
    }
  } catch {
    // 静默轮询错误静默捕获，不打扰用户
  }
}
```

---

## 智能识别记录视图与抓拍渲染契约（Record Views & Target Box Rendering Contracts）

本规范适用于 `record/face`（人脸记录）、`record/capture`（全量抓拍）、`record/alarm`（智能告警）、`record/plate`（车牌记录）等视觉智能记录模块的表格视图、搜索表单、详情弹窗与目标框（BBox）渲染。

### 1. 范围 / 触发

- **触发场景**：开发或重构视觉记录表格、全景/特写缩略图展示、目标框标注（Bounding Box）、搜索栏表单排版及 1:1 比对核验弹窗。
- **核心目标**：保障列表缩略图、详情卡片与点击全屏放大预览三层视图在几何坐标、视觉样式、线宽粗细上的 100% 绝对一致，消除由于 CSS 裁剪和 SVG 比例拉伸导致的视觉断层。

### 2. 契约与规范细节

#### A. 目标框 (BBox) 矢量渲染与预览一致性

1. **统一视觉语言**：
   - **矩形主体框**：科技蓝细边框（`#3b82f6`），半透明填充（`rgba(59, 130, 246, 0.18)`）；
   - **四角直角包角（Corner Brackets）**：必须使用翠绿色（`#10b981`），线帽为直角（`stroke-linecap="square"`）；
   - 缩略图、详情卡片和点击大图 Canvas 预览必须保持完全相同的视觉配色与四角包角高亮。
2. **矢量线宽恒定（`non-scaling-stroke`）**：
   - SVG `<rect>` 与 `<path>` 必须标记 `vector-effect="non-scaling-stroke"`，确保无论容器是 `72px` 缩略图还是 `500px+` 详情卡片，屏幕物理线宽恒定为 1.5px~2.5px，禁止出现随容器巨幅拉粗。
3. **消除裁剪导致的坐标偏移**：
   - 带有 BBox 叠加的全景监控图容器必须保持与摄像头监控流一致的 `16:9` 物理宽高比（如 `72×41`、`85×48`、`aspect-video`），并使用 `fit="contain"`，禁止使用破坏归一化坐标映射的自由 `object-fit: cover` 裁剪。
4. **性能保护**：
   - 列表静态渲染使用轻量级 SVG 覆盖层（0 JS 渲染开销）；
   - 1080P/4K 高清原图的 Canvas 目标框生成保持“仅在用户主动点击放大时按需单例生成（Lazy load on click）”，列表滚动零内存与显存浪费。

#### B. 搜索表单两端分散对齐规范（Justified Filter Labels）

1. **统一基准宽度**：
   - `useVbenVxeGrid` 的 `formOptions` 统一通过 `commonConfig: { labelWidth: 80 }` 为 3~5 字中文字符分配 `80px` 布局基准宽度；
2. **两端分散对齐 CSS**：
   - 通过 `:deep([data-slot="form-label"])` 设置 `text-align: justify; text-align-last: justify;`，使 3 字（`摄 像 头`）、4 字（`人 员 姓 名`）、5 字（`最小相似度`）中文字符在 `76px` 视口内均匀拉伸分散，冒号垂直排齐。

#### C. 表格列与详情对齐契约

1. **表格序号列**：统一使用 `type: 'seq'` 序号列，禁止展示无业务语义的底层自增主键 `id`。
2. **无端截断禁止**：人员工号（`personId`）、车牌号、事件流水号等在详情卡片中必须完整展示，不添加多余的 `max-w` 截断与 Tooltip。
3. **1:1 比对核验对称布局**：
   - 顶部：现场抓拍特写（96×96） ↔ 居中相似度指示器 ↔ 底库样本照（96×96）居中对称呈现；
   - 中部：全景抓拍大图（左 50%）与结构化元数据面板（右 50%）完全等宽高对称。

### 3. 正反示例对比（Wrong vs Correct）

#### 错误做法（Wrong）

```vue
<!-- 错误：SVG 目标框未固定线宽，容器未保持 16:9，导致粗细变形与坐标偏移 -->
<div class="h-16 w-16 overflow-hidden">
  <img :src="url" class="object-cover" />
  <svg viewBox="0 0 100 100" preserveAspectRatio="none" class="absolute inset-0">
    <rect :x="x" :y="y" :width="w" :height="h" stroke="#1890ff" stroke-width="2" />
  </svg>
</div>
```

#### 正确做法（Correct）

```vue
<!-- 正确：保持 16:9 物理画面比，使用 non-scaling-stroke 和统一直角包角 -->
<div class="relative aspect-video w-[72px] h-[41px] items-center justify-center overflow-hidden">
  <img :src="thumbnailUrl" class="h-full w-full object-contain" loading="lazy" />
  <svg v-if="normalizedBbox" viewBox="0 0 100 100" preserveAspectRatio="none" class="pointer-events-none absolute inset-0 h-full w-full">
    <rect
      :x="normalizedBbox[0] * 100"
      :y="normalizedBbox[1] * 100"
      :width="(normalizedBbox[2] - normalizedBbox[0]) * 100"
      :height="(normalizedBbox[3] - normalizedBbox[1]) * 100"
      fill="rgba(59, 130, 246, 0.18)"
      stroke="#3b82f6"
      stroke-width="1.5"
      vector-effect="non-scaling-stroke"
    />
    <path
      :d="cornerBracketPath"
      fill="none"
      stroke="#10b981"
      stroke-width="2.5"
      stroke-linecap="square"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</div>
```

---

## 卡片式目录与网格布局规范 (Card Catalog & Grid Layout Guidelines)

### 1. 范围 / 触发

- **触发场景**：开发或重构 AI 算法包管理、模型资产卡片、任务实例卡片、边缘节点卡片等网格/卡片式资源目录。
- **核心目标**：确保卡片排列符合直观的从左至右（Left-to-Right）水平阅读顺序，杜绝纵向瀑布流造成的错行误解；规范整卡可点击时的微动效与内部交互事件阻断。

### 2. 契约与规范细节

#### A. 网格布局与排列顺序契约

1. **禁止使用 CSS Multi-column (`columns-*`) 作为有序卡片容器**：
   - CSS `columns` 遵循从上到下的纵向列分流（Column-first）。在元素较少（如 3~4 个）时，第 2 或第 3 项会被排在第一列的下方（第二行），严重破坏从左到右阅读习惯。
2. **强制使用 Responsive CSS Grid (`grid grid-cols-*` 或 `auto-fill`)**：
   - 优先采用自适应填满网格：`grid grid-cols-[repeat(auto-fill,minmax(290px,1fr))] gap-4`，或者分断点响应式 `grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4`。
   - `auto-fill + minmax` 能在折叠侧边栏、改变窗口大小、不同屏幕宽度下平滑动态计算最佳列数，保证每张卡片不会被无限挤压变窄或过度拉宽，保持从左到右水平排满。
3. **同行动高与对齐契约**：
   - 卡片容器根元素统一使用 `h-full flex flex-col justify-between`，使同行动高卡片的底部元信息与操作栏自然吸底对齐。

#### B. 整卡点击与事件冒泡契约 (Clickable Card & Event Bubbling)

1. **主交互与悬浮动效**：
   - 整卡支持点击打开版本/详情抽屉（`@click="handleOpenDrawer(item)"`）；
   - 卡片容器添加 `cursor-pointer hover:-translate-y-1 hover:shadow-xl active:scale-[0.99] transition-all duration-200`，右上角可配合 `arrow-up-right` 提示可点击性。
2. **内嵌操作显式阻止冒泡 (Mandatory `.stop`)**：
   - 卡片内部的所有独立微交互（如：一键复制 ID、参数规范按钮、操作列按钮、Popover 触发器）**必须使用 `@click.stop`**，防止触发外层卡片的抽屉打开动作。

#### C. 工具栏与筛选交互极简契约

1. **避免重置与刷新图标并排混淆**：
   - 搜索框统一配置 `allow-clear` 自带清空；
   - 工具栏右侧仅保留功能明确的「刷新」按钮（保留当前筛选条件重新拉取远端最新状态），避免放置无文字提示的逆时针重置图标与顺时针刷新图标引起语义混淆；
   - 在搜索结果为空（Empty State）的中心提示卡片中提供「重置筛选条件」显式大按钮承接清空需求。

### 3. 正反示例对比（Wrong vs Correct）

#### 错误做法（Wrong）

```vue
<!-- 错误：使用 columns 导致从上到下错位排版；内嵌按钮未阻止冒泡导致点击按钮同时触发卡片点击 -->
<div class="columns-1 sm:columns-2 lg:columns-3 xl:columns-4 gap-4">
  <div v-for="item in list" :key="item.id" class="break-inside-avoid" @click="openDrawer(item)">
    <div class="card">
      <button @click="copyId(item.id)">Copy</button>
      <button @click="openSchema(item)">Schema</button>
    </div>
  </div>
</div>
```

#### 正确做法（Correct）

```vue
<!-- 正确：使用 CSS Grid auto-fill 保障平滑自适应，内部微操作全部使用 @click.stop 阻止冒泡 -->
<div class="grid grid-cols-[repeat(auto-fill,minmax(290px,1fr))] gap-4">
  <div
    v-for="item in list"
    :key="item.id"
    class="group relative flex flex-col justify-between rounded-2xl border border-border bg-card p-5 h-full cursor-pointer hover:-translate-y-1 hover:shadow-xl active:scale-[0.99] transition-all"
    @click="openDrawer(item)"
  >
    <!-- 头部信息与可点击指示 -->
    <div class="flex items-start justify-between">
      <span class="font-bold">{{ item.name }}</span>
      <button type="button" @click.stop="copyId(item.id)">
        <IconifyIcon icon="lucide:copy" class="size-3" />
      </button>
    </div>

    <!-- 底部操作栏吸底并阻止冒泡 -->
    <div class="mt-4 border-t border-border pt-3 flex justify-between items-center">
      <span class="text-xs text-muted-foreground">{{ item.createdAt }}</span>
      <button type="button" @click.stop="openSchema(item)">参数规范</button>
    </div>
  </div>
</div>
```

---

## 常见错误

- **VxeTable 配置了 `type: 'expand'` 列却遗漏 `expandConfig`**，导致展开箭头不显示或行展开控制器无法被激活。
- **操作列过窄导致删除按钮被 `fixed: right` 裁剪**，使运维人员误以为系统不支持直接删除实体。
- **在工具栏静态硬编码置灰的“批量删除”按钮**，破坏了基于复选框勾选的浮动选中激活条规范。
- **在本地重新实现 `@vben/*` 基础设施**（认证流程、request client、访问
  控制）——复用这些包。
- **表格时间列未加 `formatter: 'formatDateTime'`**，导致显示原始 ISO 时间字符串且无法响应系统时区切换。
- **自定义表单按钮未设置 `htmlType="button"`**，导致表单查询/重置触发整页提交刷新。
- **硬编码路由/菜单标题** 而不是使用 i18n key。
- **通过深层 `../../packages/...` 路径导入** —— 使用 `@vben/*` 别名。
