# 跨层思考指南

> **目的**：在实现前思考跨层的数据流。

---

## 问题

**大多数 bug 发生在层边界处**，而不是层内部。

常见的跨层 bug：

- API 返回格式 A，前端期望格式 B
- 数据库存 X，service 转换为 Y，但丢失了数据
- 多层以不同方式实现同一逻辑

---

## 实现跨层功能之前

### 第 1 步：绘制数据流

画出数据如何移动：

```
Source → Transform → Store → Retrieve → Transform → Display
```

对每个箭头，问：

- 数据的格式是什么？
- 可能出什么问题？
- 谁负责校验？

### 第 2 步：识别边界

| 边界              | 常见问题                     |
| --------------------- | --------------------------------- |
| API ↔ Service         | 类型不匹配、字段缺失   |
| Service ↔ Database    | 格式转换、null 处理 |
| Backend ↔ Frontend    | 序列化、日期格式       |
| Component ↔ Component | Props 形状变化               |

### 第 3 步：定义契约

对每个边界：

- 确切的输入格式是什么？
- 确切的输出格式是什么？
- 可能发生哪些错误？

---

## 常见的跨层错误

### 错误 1：隐式格式假设

**不好**：不检查就假设日期格式

**好**：在边界处显式转换格式

### 错误 2：零散的校验

**不好**：在多个层校验同一件事

**好**：在入口点校验一次

### 错误 3：泄漏的抽象

**不好**：组件知道数据库 schema

**好**：每层只知道它的邻居

### 错误 4：每个消费者都解析同一负载

**不好**：一个命令读取 JSONL 事件并内联强转字段：

```typescript
const thread = (ev as { thread?: string }).thread;
const labels = (ev as { labels?: string[] }).labels;
```

这看起来是局部的，但意味着每个消费者都持有事件契约的私有版本。下一次字段
变更只会更新一个命令而漏掉另一个。

**好**：在事件边界解码一次，然后导出类型化投影：

```typescript
if (!isThreadEvent(ev)) return false;
return ev.thread === filter.thread;
```

**规则**：对于追加式日志、JSON 流、RPC 负载或配置文件，为一个所有者负责：

- 事件 / 负载类型定义
- 从 `unknown` 的类型守卫和规范化
- UI 命令使用的元数据投影
- 从事实来源重放状态的 reducers

渲染代码可以格式化字段，但绝不能重新定义负载契约。

---

## 跨层功能检查清单

实现前：

- [ ] 绘制了完整的数据流
- [ ] 识别了所有层边界
- [ ] 定义了每个边界的格式
- [ ] 决定了校验发生在哪里

实现后：

- [ ] 用边界情况测试过（null、空、非法）
- [ ] 验证了每个边界的错误处理
- [ ] 检查了数据能经受往返
- [ ] 检查了消费者导入共享解码器 / 投影，而不是在本地强转负载字段
- [ ] 检查了派生状态指向源事件标识符（`seq`、`id`、`version`），而不是
      发明第二个游标

---

## 跨平台模板一致性

在 Trellis 中，命令模板（例如 `record-session.md`）存在于**多个平台**，内容
相同或几乎相同。这是一个跨层边界。

### 修改任何命令模板后的检查清单

- [ ] 找到所有带相同命令的平台：`find src/templates/*/commands/trellis/ -name "<command>.*"`
- [ ] 更新所有平台副本（Markdown `.md` 和 TOML `.toml`）
- [ ] 对于 Gemini TOML：调整续行（`\\` vs `\`）和三引号字符串
- [ ] 运行 `/trellis:check-cross-layer` 验证没有遗漏

**真实示例**：在 Claude 中把 `record-session.md` 更新为使用 `--mode record`，
但忘了 iFlow、Kilo、OpenCode 和 Gemini——被跨层检查发现。

---

## 生成的运行时模板升级一致性

某些生成文件既是文档又是运行时输入。在 Trellis 中，
`.trellis/workflow.md` 会被 `get_context.py`、`workflow_phase.py`、
SessionStart 过滤器和每轮 hooks 解析。模板变更必须针对全新初始化和升级两条
路径都做验证。

### 修改运行时解析模板后的检查清单

- [ ] 识别读取该模板的每个运行时解析器，而不只是安装它的文件写入器
- [ ] 检查相关语法是否存在于明显的托管区域（如 tag 块）之外
- [ ] 验证全新的 `init` 输出，以及写入旧版 `.trellis/.version` 的版本化
      `update` 场景
- [ ] 用旧的原始模板 fixture 添加升级回归，然后断言安装的文件达到当前
      打包形态
- [ ] 更新拥有运行时契约的后端 spec

---

## 版本化文档边界

版本化文档是一个跨层边界：源路径、`docs.json` 版本路由和渲染的版本选择器
必须都描述同一条发布线。

### 编辑版本化文档前的检查清单

- [ ] 识别目标发布线：stable、beta 或 RC
- [ ] 验证编辑的 MDX 路径与该线匹配：
  - stable：`docs-site/{start,advanced,...}` 和 `docs-site/zh/{start,advanced,...}`
  - beta：`docs-site/beta/**` 和 `docs-site/zh/beta/**`
  - RC：`docs-site/rc/**` 和 `docs-site/zh/rc/**`
- [ ] 验证 `docs.json` 导航把版本标签指向相同路径
- [ ] 提交前 grep 对侧目录树中发布线专属的术语
- [ ] 把出现在根发布路径下的 beta 内容视为源路径 bug，而非渲染 bug

**真实示例**：一个 beta-only 任务工作流变更在根 `start/` 和 `advanced/` 路径
下记录了 `prd.md` + `design.md` + `implement.md`、任务创建同意和 Codex
模式横幅。随后文档站点在 Release 选择器下提供了 0.6 beta 行为。修复是恢复根
发布文档、把 0.6 内容移到 `beta/` 和 `zh/beta/`，并添加针对根发布树的 beta
标记 grep 审计。

**真实示例**：Codex 内联模式把工作流平台标记从 `[Codex]` / `[Kilo, Antigravity,
Windsurf]` 改为 `[codex-sub-agent]` / `[codex-inline, Kilo, Antigravity, Windsurf]`。
全新初始化是正确的，但 `trellis update` 只合并了 `[workflow-state:*]` 块，
保留了这些块之外的过时标记。结果：升级的项目拿到新的 hook 脚本，但仍是旧的
工作流路由，因此 `get_context.py --mode phase --platform codex` 可能返回空的
Phase 2.1 详情。

---

## 模式探测探针检查清单

当 CLI 通过探测远程资源自动检测模式时（例如检查 `index.json` 是否存在以决定
marketplace 还是直接下载）：

### 实现前：

- [ ] 探针在**所有**使用其结果的代码路径中运行（interactive、`-y`、`--flag` 组合）
- [ ] 区分 404 与瞬时错误——不要把两者都当作"未找到"
- [ ] 瞬时错误**中止或重试**，绝不静默切换模式
- [ ] 当上下文变化时（例如用户切换来源），共享状态（缓存、预取数据）会
      **重置**
- [ ] **快捷路径**（例如跳过选择器的 `--template`）必须与探测路径有相同的
      错误处理质量——检查下游函数不调用 catch-all 包装器

### 实现后：

- [ ] 追踪从探针结果到模式决策分支的每条路径——没有 fallthrough
- [ ] 外部格式契约（giget URI、原始 URL）经过测试或至少以注释记录
- [ ] 元数据读取消费完整响应或使用流式解析器——绝不把固定大小的前缀解析为
      完整 JSON
- [ ] 从解析出的各部分重建复合标识符时，验证**所有**字段都在且位置**正确**
      （例如 `provider:repo/path#ref` 而非 `provider:repo#ref/path`）
- [ ] 验证快捷路径后调用的**动作函数**内部不使用旧的 catch-all fetch——当
      错误区分很重要时，它们必须使用探针质量的变体

**真实示例**：自定义注册表流程在 3 轮评审中有 8 个 bug：(1) 探针只在交互
模式下运行，(2) 瞬时错误落到错误模式，(3) giget URI 的 `#ref` 位置错误，
(4) 预取模板在来源切换间泄漏，(5) `--template` 快捷方式绕过了探针，但
`downloadTemplateById` 内部使用了 catch-all 的 `fetchTemplateIndex`，把超时
变成了"Template not found"。

**真实示例**：Agent 会话更新提示用 `response.read(4096)` 获取 npm `latest`
元数据，然后把它解析为完整 JSON。`@mindfoldhq/trellis` 包的元数据超过 4 KB，
JSON 被截断、解析静默失败，首次会话注入没有显示更新提示。修复：解析前读取
完整响应，并添加一个 `version` 后跟 8 KB 元数据尾的回归。

---

## 跨平台模板一致性

在 Trellis 中，命令模板（例如 `record-session.md`）存在于**多个平台**，内容
相同或几乎相同。这是一个跨层边界。

### 修改任何命令模板后的检查清单

- [ ] 找到所有带相同命令的平台：`find src/templates/*/commands/trellis/ -name "<command>.*"`
- [ ] 更新所有平台副本（Markdown `.md` 和 TOML `.toml`）
- [ ] 对于 Gemini TOML：调整续行（`\\` vs `\`）和三引号字符串
- [ ] 运行 `/trellis:check-cross-layer` 验证没有遗漏

**真实示例**：在 Claude 中把 `record-session.md` 更新为使用 `--mode record`，
但忘了 iFlow、Kilo、OpenCode 和 Gemini——被跨层检查发现。

---

## 生成的运行时模板升级一致性

某些生成文件既是文档又是运行时输入。在 Trellis 中，
`.trellis/workflow.md` 会被 `get_context.py`、`workflow_phase.py`、
SessionStart 过滤器和每轮 hooks 解析。模板变更必须针对全新初始化和升级两条
路径都做验证。

### 修改运行时解析模板后的检查清单

- [ ] 识别读取该模板的每个运行时解析器，而不只是安装它的文件写入器
- [ ] 检查相关语法是否存在于明显的托管区域（如 tag 块）之外
- [ ] 验证全新的 `init` 输出，以及写入旧版 `.trellis/.version` 的版本化
      `update` 场景
- [ ] 用旧的原始模板 fixture 添加升级回归，然后断言安装的文件达到当前
      打包形态
- [ ] 更新拥有运行时契约的后端 spec

**真实示例**：Codex 内联模式把工作流平台标记从 `[Codex]` / `[Kilo, Antigravity,
Windsurf]` 改为 `[codex-sub-agent]` / `[codex-inline, Kilo, Antigravity, Windsurf]`。
全新初始化是正确的，但 `trellis update` 只合并了 `[workflow-state:*]` 块，
保留了这些块之外的过时标记。结果：升级的项目拿到新的 hook 脚本，但仍是旧的
工作流路由，因此 `get_context.py --mode phase --platform codex` 可能返回空的
Phase 2.1 详情。

---

## 模式探测探针检查清单

当 CLI 通过探测远程资源自动检测模式时（例如检查 `index.json` 是否存在以决定
marketplace 还是直接下载）：

### 实现前：
- [ ] 探针在**所有**使用其结果的代码路径中运行（interactive、`-y`、`--flag` 组合）
- [ ] 区分 404 与瞬时错误——不要把两者都当作"未找到"
- [ ] 瞬时错误**中止或重试**，绝不静默切换模式
- [ ] 当上下文变化时（例如用户切换来源），共享状态（缓存、预取数据）会
      **重置**
- [ ] **快捷路径**（例如跳过选择器的 `--template`）必须与探测路径有相同的
      错误处理质量——检查下游函数不调用 catch-all 包装器

### 实现后：
- [ ] 追踪从探针结果到模式决策分支的每条路径——没有 fallthrough
- [ ] 外部格式契约（giget URI、原始 URL）经过测试或至少以注释记录
- [ ] 元数据读取消费完整响应或使用流式解析器——绝不把固定大小的前缀解析为
      完整 JSON
- [ ] 从解析出的各部分重建复合标识符时，验证**所有**字段都在且位置**正确**
      （例如 `provider:repo/path#ref` 而非 `provider:repo#ref/path`）
- [ ] 验证快捷路径后调用的**动作函数**内部不使用旧的 catch-all fetch——当
      错误区分很重要时，它们必须使用探针质量的变体

**真实示例**：自定义注册表流程在 3 轮评审中有 8 个 bug：(1) 探针只在交互
模式下运行，(2) 瞬时错误落到错误模式，(3) giget URI 的 `#ref` 位置错误，
(4) 预取模板在来源切换间泄漏，(5) `--template` 快捷方式绕过了探针，但
`downloadTemplateById` 内部使用了 catch-all 的 `fetchTemplateIndex`，把超时
变成了"Template not found"。

**真实示例**：Agent 会话更新提示用 `response.read(4096)` 获取 npm `latest`
元数据，然后把它解析为完整 JSON。`@mindfoldhq/trellis` 包的元数据超过 4 KB，
JSON 被截断、解析静默失败，首次会话注入没有显示更新提示。修复：解析前读取
完整响应，并添加一个 `version` 后跟 8 KB 元数据尾的回归。

---

## 何时创建流程文档

以下情况创建详细的流程文档：

- 功能跨越 3+ 层
- 多个团队参与
- 数据格式复杂
- 该功能以前出过 bug

---

## 事件日志 / 投影边界

追加式日志是跨层契约。单个事件穿过：

```
CLI input → event writer → events.jsonl → reader → filter → reducer → display
```

### 添加新事件类型或字段后的检查清单

- [ ] 把事件类型加入中央事件分类法
- [ ] 在事件层添加类型化事件变体或类型守卫
- [ ] 为来自用户输入或 JSON 的数组/对象字段添加规范化辅助函数
- [ ] 让 `seq` / `id` 赋值只存在于事件写入器中
- [ ] 让过滤器和 reducers 消费类型化事件守卫，而不是本地强转
- [ ] 让展示代码消费 reducer 输出或类型化事件，而不是原始 JSON
- [ ] 至少添加一个证明历史重放和实时过滤使用同一过滤模型的回归

**真实示例**：线程频道添加了 `kind: "thread"`、`description`、`context`、
labels 和 `lastSeq`。第一次实现正确重放了线程状态，但几个命令仍用本地强转
重新解析事件负载字段。修复是让核心事件层拥有 `ThreadChannelEvent` 和
`isThreadEvent`，让 `reduceChannelMetadata` 成为唯一的频道元数据投影，让
`reduceThreads` 成为唯一的线程重放 reducer。
