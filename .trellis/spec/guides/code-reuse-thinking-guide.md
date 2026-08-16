# 代码复用思考指南

> **目的**：在创建新代码前停下来思考——它是否已存在？

---

## 问题

**重复代码是不一致类 bug 的头号来源。**

当你复制粘贴或重写已有逻辑时：
- 修复不会传播
- 行为随时间产生分歧
- 代码库变得更难理解

---

## 编写新代码之前

### 第 1 步：先搜索

```bash
# 搜索相似的函数名
grep -r "functionName" .

# 搜索相似的逻辑
grep -r "keyword" .
```

### 第 2 步：提出这些问题

| 问题 | 如果答案是"是"…… |
|----------|-----------|
| 存在相似的函数吗？ | 使用或扩展它 |
| 这个模式在别处用过吗？ | 遵循现有模式 |
| 这能成为共享工具吗？ | 在正确的位置创建它 |
| 我在从另一个文件复制代码吗？ | **停止**——提取为共享代码 |

---

## 常见的重复模式

### 模式 1：复制粘贴函数

**不好**：把校验函数复制到另一个文件

**好**：提取为共享工具，在需要处导入

### 模式 2：相似的组件

**不好**：创建一个与现有组件 80% 相似的组件

**好**：用 props/变体扩展现有组件

### 模式 3：重复的常量

**不好**：在多个文件中定义同一个常量

**好**：单一事实来源，到处导入

### 模式 4：重复的负载字段提取

**不好**：多个消费者在本地强转相同的 JSON/事件字段：

```typescript
const description = (ev as { description?: string }).description;
const context = (ev as { context?: ContextEntry[] }).context;
```

即使代码只有两行，这也是重复的契约逻辑。每个消费者现在都持有自己对
"有效负载意味着什么"的定义。

**好**：把解码器、类型守卫或投影放在数据所有者旁边：

```typescript
if (isThreadEvent(ev)) {
  renderThreadEvent(ev);
}
```

**规则**：如果同一个无类型负载字段在 2+ 处被读取，在添加第三个读取方之前，
创建一个共享的类型守卫 / 规范化器 / 投影。

---

## 何时抽象

**抽象当**：
- 相同代码出现 3+ 次
- 逻辑复杂到容易出 bug
- 多个人可能都需要它

**不抽象当**：
- 只用一次
- 微不足道的一行代码
- 抽象比重复更复杂

---

## 批量修改之后

当你对多个文件做了相似修改：

1. **复查**：你捕捉到了所有实例吗？
2. **搜索**：运行 grep 找出任何遗漏
3. **考虑**：这应该被抽象吗？

### Reducers 应使用穷举结构

当状态由类似 action 的值（`action`、`kind`、`status`、`phase`）派生时，
优先使用带单个 `switch` 的 reducer，而不是零散的 `if/else` 更新。

```typescript
// 不好 - 特定 action 的状态转换难以审计
if (action === "opened") { ... }
else if (action === "comment") { ... }
else if (action === "status") { ... }

// 好 - 一个 reducer 拥有转换表
switch (event.action) {
  case "opened":
    ...
    return;
  case "comment":
    ...
    return;
}
```

当事件日志是事实来源时这一点很重要。Reducer 是文档化的重放模型；
展示代码和命令不应重复该重放模型的片段。

---

## 提交前检查清单

- [ ] 已搜索存在的相似代码
- [ ] 没有本应共享却复制粘贴的逻辑
- [ ] 没有在共享解码器之外的重复无类型负载字段提取
- [ ] 常量定义在一处
- [ ] 相似模式遵循相同结构
- [ ] Reducer/action 转换只存在于一个 reducer 或命令分发器中

---

## 陷阱：Python if/elif/else 穷举检查

**问题**：Python 的 if/elif/else 链没有编译期的穷举检查。当你给 `Literal`
类型（例如 `Platform`）添加新值时，现有的 if/elif/else 链会静默落到
`else`，带错误默认值。

**症状**：新平台部分工作——某些方法返回 Claude 默认值而不是平台特定值。
不会抛出任何错误。

**示例**（`cli_adapter.py`）：
```python
# 不好: "gemini" 落到 else，返回 "claude"
@property
def cli_name(self) -> str:
    if self.platform == "opencode":
        return "opencode"
    else:
        return "claude"  # gemini 静默地得到 "claude"！

# 好: 为每个平台显式分支
@property
def cli_name(self) -> str:
    if self.platform == "opencode":
        return "opencode"
    elif self.platform == "gemini":
        return "gemini"
    else:
        return "claude"
```

**预防**：当给 Python `Literal` 类型添加新值时，搜索所有基于该类型的
if/elif/else 链，并为新值添加显式分支。不要依赖 `else` 对新值是正确的。

---

## 陷阱：产生相同输出的不对称机制

**问题**：当两种不同机制必须产生同一文件集时（例如 init 用递归目录复制 vs
update 用手动 `files.set()`），结构性变更（重命名、移动、添加子目录）只会
通过自动机制传播。手动机制会静默漂移。

**症状**：init 完美工作，但 update 在错误路径创建文件或完全漏掉文件。

**预防**：
- **最佳**：消除不对称——让手动路径调用自动机制（例如 `collectTemplateFiles()`
  调用 `getAllScripts()` 而不是维护自己的列表）
- **如果不对称不可避免**：添加一个比较两种机制输出的回归测试
- 迁移目录结构时，搜索所有引用旧结构的代码路径

**真实示例**：`trellis update` 有一个 11 个脚本的手动 `files.set()` 列表，而
`getAllScripts()` 已经在跟踪它们。修复：用 `for..of getAllScripts()` 循环替换
手动列表。见 v0.4.0-beta.3 中的 `update.ts` 重构。

---

## 模板文件注册（Trellis 专属）

当向 `src/templates/trellis/scripts/` 添加新文件时：

**单一注册点**：`src/templates/trellis/index.ts`

1. 添加 `export const xxxScript = readTemplate("scripts/path/file.py");`
2. 添加到 `getAllScripts()` Map

就这样。`commands/update.ts` 直接使用 `getAllScripts()`——无需手动同步。

**为什么重要**：如果不注册到 `getAllScripts()`，`trellis update` 不会把文件
同步到用户项目。bug 修复和功能不会传播。

**历史**：在 v0.4.0-beta.3 之前，`update.ts` 有自己的手工维护文件列表，经常与
`getAllScripts()` 失同步。这导致 11 个 Python 文件在 `trellis update` 时被静默
跳过。修复是消除重复列表，用 `getAllScripts()` 作为唯一事实来源。

### 新脚本快速检查清单

```bash
# 添加新的 .py 文件后，确认它在 getAllScripts() 中：
grep -l "newFileName" src/templates/trellis/index.ts  # 应该匹配
```

### 模板同步约定

`.trellis/scripts/`（dogfooding）和
`packages/cli/src/templates/trellis/scripts/`（模板）必须保持相同。编辑
`.trellis/scripts/` 后，始终同步：

```bash
rsync -av --delete --exclude='__pycache__' .trellis/scripts/ packages/cli/src/templates/trellis/scripts/
```

**陷阱**：以错误的源/目标路径运行 rsync 会创建嵌套的垃圾目录（例如
`.trellis/scripts/packages/cli/...`）。运行前务必仔细检查路径。
