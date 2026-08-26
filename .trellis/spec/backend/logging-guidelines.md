# 日志规范

> 本项目如何记录日志。

---

## 概览

- 库：`go.uber.org/zap`（`internal/pkg/logger`）。从配置构建一个单一 logger，
  通过 wire 注入；不要创建临时 logger。
- 日志级别来自配置中的 `log.level`（默认 `info`）。
- `main` 负责生命周期日志（启动里程碑、致命错误、优雅退出）；
  各包通过注入的 logger 记录运行事件。

---

## 日志级别

- `debug` — 仅在 `log.level: debug` 时启用；细粒度的流程细节。
- `info` — 生命周期里程碑：AutoMigrate 完成、seed 完成/跳过、服务器正在监听
  端口、服务器已退出。
- `warn` — 可恢复的情况：数据库连接重试尝试（`db.New` 以 `warn` 级别记录
  driver、host 和尝试次数）。
- `error` / `fatal` — 失败：启动失败、服务器监听失败、优雅退出失败。`fatal`
  仅保留给 `main` 使用，会退出进程。

---

## 结构化日志

- 使用带 `ISO8601TimeEncoder` 的生产编码器（在 `logger.New` 中设置）。
- 始终以类型化 zap 字段附加上下文（`zap.String`、`zap.Int`、`zap.Error`、
  `zap.Duration`），绝不把值字符串插值进消息。
- 示例：`log.Warn("database connect failed, retrying", zap.String("driver", cfg.DB.Driver), zap.Int("attempt", attempt), zap.Error(err))`

---

## 应该记录什么

- 启动/退出里程碑和致命失败（在 `main` 中）。
- 带尝试次数的可重试基础设施失败。
- 处理器出现后的业务级事件：映射为 errno 错误码的请求失败、认证事件——
  保持结构化且可 grep。

---

## 不应记录什么

- **密钥**：数据库密码、`APP_DB_PASSWORD` 的值、`jwt.secret`。`db.New` 记录
  driver/host，但绝不记录 DSN 或密码。
- 任何形式的**密码哈希或明文密码**。
- `info` 级别的完整内部堆栈；在 `error` 级别使用 `zap.Error`。

---

## 操作审计日志规范 (Operation Log)

### 1. 核心原则
- **不可变性（Append-Only）**：操作审计日志严禁提供任何前端可调用的单条删除或批量删除接口（`DELETE /api/oplog/*` 禁止提供）。审计日志必须保证不可抵赖性与合规性（遵循《网络安全法》与等保 2.0 规定）。
- **语义化与国际化（Human-Readable & i18n）**：
  - `action` 字段存储语义化的 i18n key（如 `system.user.addUser`、`system.role.assignMenu`、`resource.camera.probe` 等），前端配合 `$t()` 本地化渲染。
  - `path` 字段存储 HTTP 技术路径（如 `/api/user`、`/api/camera/probe`），供开发与运维排错溯源。
  - 两者不可混用或机械拼接（严禁将 `POST /api/user` 直接作为 `action`）。

### 2. 路由到 Action 的语义映射闭环（开发必须执行）
所有新增写操作接口（`POST`、`PUT`、`DELETE`），必须完成以下**四位一体闭环**，禁止遗漏：
1. **后端注册路由映射**：在 `internal/middleware/oplog.go` 的 `actionI18nMap` 中为每一个路由注册语义化 i18n key；
2. **前端前缀白名单**：在 `apps/web-antd/src/utils/i18n.ts` 的 `I18N_KEY_PREFIXES` 中确保包含了该 key 的顶级命名空间（如 `resource.`、`system.`、`ops.` 等）；
3. **前端多语言翻译**：在 `apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/` 对应 JSON 文件中补齐三语动作文案；
4. **单测覆盖**：在 `internal/middleware/middleware_test.go` 中断言写请求触发后生成的 `OperationLog.Action` 字段精确匹配期望的 i18n key，避免 fallback 到 `"Method Path"`。
