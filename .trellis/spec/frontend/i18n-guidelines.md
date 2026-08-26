# 国际化规范 (i18n Guidelines)

> 本项目前端多语言与国际化约定。

---

## 概览

UI 基于 `@vben/locales`（vue-i18n 封装）+ Ant Design Vue 国际化 + Day.js 本地化。
系统默认支持三种语言：

- `zh-CN`：简体中文（默认语言）
- `en-US`：英语 (English)
- `zh-TW`：繁体中文（繁體中文）

---

## 目录分层与文案组织

1. **业务应用级文案** (`apps/web-antd/src/locales/langs/`)：
   - `zh-CN/`, `en-US/`, `zh-TW/`
   - 按业务命名空间拆分文件：
     - `system.json`：系统管理模块文案（用户、角色、菜单、部门、操作日志）
     - `ops.json`：运维管理模块文案（网络配置、时间管理/NTP、Webhook 管理、设备监控、平台信息、存储管理）
     - `routes.json`：路由与菜单标题翻译
     - `page.json` / `demos.json`：通用页面或演示文案
2. **全局/框架级文案** (`packages/locales/src/langs/`)：
   - `common.json`：基础通用词汇（如确认、取消、操作、成功、失败）
   - `preferences.json`：系统偏好设置文案
   - `ui.json`：框架通用组件文案

---

## 核心编码约定与契约

### 1. 禁止硬编码文本

- 所有用户可见的文案（包括表单 Label、Placeholder、表格表头、操作按钮、Modal 标题、Popconfirm 提示语、Message 消息等）必须使用 `$t()` 引用，禁止在 `.vue` / `.ts` 文件中硬编码中文字符串。
- 引用方式：

  ```ts
  import { $t } from '@vben/locales';

  // 示例
  $t('system.user.username')
  $t('system.common.success')
  ```

### 2. 三语言对齐契约 (Mandatory)

- 新增或修改任何 i18n key 时，**必须同步更新 `zh-CN`、`en-US`、`zh-TW` 三个语言目录下的对应 JSON 文件**。
- 保证 JSON key 命名空间与层级完全一致，杜绝因遗漏某种语言导致界面直接展示裸 key 或空白。

### 3. 路由与动态菜单 i18n 契约

- 后端数据表 `menus.title` 存入标准 i18n key（例如系统管理 `routes.system.user`，运维管理 `routes.ops.network`、`routes.ops.time`）。
- 菜单展示与树形选择器中，必须做 i18n 解析：

  ```ts
  const title = item.title?.startsWith('routes.') ? $t(item.title) : item.title;
  ```

### 4. 操作日志语义与国际化契约 (Operation Log Action i18n)

- 后端 `operation_logs.action` 存储标准 i18n key（如 `system.user.addUser`、`system.log.actionLogin`、`resource.camera.probe`）。
- **写接口新增时的国际化闭环**：
  1. `apps/web-antd/src/utils/i18n.ts` 的 `I18N_KEY_PREFIXES` 必须包含对应的命名空间（如 `system.`、`routes.`、`auth.`、`ops.`、`resource.`）。
  2. `apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/` 必须同时维护对应动作的中/英/繁三语翻译。
- 日志列表与详情页中渲染 `action` 时，必须使用 `translateI18nKey` 转换并提供优雅降级：

  ```ts
  import { translateI18nKey } from '#/utils/i18n';

  function formatAction(action?: string) {
    if (!action) return '-';
    return translateI18nKey(action);
  }
  ```

### 5. 英文/长文本 UI 宽度预留契约

- 英文文案通常是中文长度的 1.5x ~ 2.5x。
- 表格操作列（Actions）：
  - 必须显式配置 `showOverflow: false`，避免长英文按钮被截断为省略号。
  - 预留足够的列宽（用户管理 ≥360px，角色/菜单/部门管理 ≥280px，日志详情 ≥120px）。
- 表单 Label：合理配置宽度或自适应布局，避免多语言切换后 Label 换行错位。

---

## 常见错误与反模式 (Anti-patterns)

- **只加 `zh-CN` 遗漏 `en-US` 或 `zh-TW`**：切换语言后界面出现未翻译的英文 key。
- **模板中写死文本**：如 `<Button>确定</Button>` 而非 `<Button>{{ $t('common.confirm') }}</Button>`。
- **动态拼接 key 导致无法静态检索**：如 `$t('system.user.' + action)`，应使用显式 map 或完整 key。
- **操作列宽度按中文紧凑设计**：切换英文后按钮换行溢出或被遮挡。
