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
