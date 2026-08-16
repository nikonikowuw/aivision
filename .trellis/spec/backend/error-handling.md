# 错误处理

> 本项目如何处理错误。

---

## 概览

业务错误是**错误码而非字符串**：每个业务失败都有定义在 `internal/pkg/errno`
（唯一事实来源）中的数字错误码，API 响应体是 `internal/pkg/response` 中的统一
`{code,data,message}` 结构。`code=0` 表示成功；失败时 `data` 为 `null`，
`message` 是来自 `errno.Message` 的面向用户的中文文案。

---

## 错误类型

- **业务错误码**（`internal/pkg/errno`）：`CodeOK = 0`；类 HTTP 的 `401`
  （未授权）/ `403`（禁止）；业务错误码在 `1xxx` 段（`1001` 凭据错误、
  `1002` 用户不存在、…）。每个错误码都有对应的 `Message` 条目。
- **Go 错误**用于非业务失败：用 `fmt.Errorf("context: %w", err)` 包装并向上
  返回给调用方（例如 `config.Load`、`db.New`、`logger.New`）。

---

## 错误处理模式

- 基础设施构造函数返回 `(T, error)`；`main` 将启动错误视为致命错误
  （`log.Fatal`/`os.Exit`），并给出清晰的信息。
- 业务处理器不得将内部错误细节泄露到 API 响应中：将失败映射为 `errno` 错误码并
  交给统一错误处理中间件（见下节），由中间件输出
  `response.Fail(code, errno.Message(code))`。
- **错误码与文案只在 `internal/pkg/errno` 统一维护**：处理器/service 层不得内联
  数字错误码或用户文案，一律引用 `errno` 常量并经由 `errno.Message` 取文案；
  错误码全局唯一，禁止同一码复用为不同含义。
- 用 `%w` 包装错误以保留原因链；不要静默吞掉错误。
- 可重试的基础设施失败（DB 未就绪）以有界次数重试，在返回错误前以 `warn`
  级别记录日志（`db.New`，3 次/2s）。

---

## Gin 中间件统一错误处理

所有 HTTP 错误响应统一由中间件输出，handler 不各自拼接错误响应。

- **挂载点**：在 `internal/router/router.go` 的 engine 装配处用 `engine.Use(...)`
  挂载（recovery 之后、路由之前）；错误处理中间件放在 `internal/middleware/` 目录。
- **handler 职责**：只做业务处理；失败时把携带 `errno` 码的错误交给中间件
  （`c.AbortWithError` 或统一业务错误类型，具体机制在首个 handler/中间件实现时定），
  成功后返回 `c.JSON(http.StatusOK, response.OK(data))`。
- **错误处理中间件职责**：
  - 从 `c.Errors` 取最后一个错误并解析出 `errno` 码，统一输出
    `response.Fail(code, errno.Message(code))`。
  - HTTP 状态按错误类型决定，对齐 scaffold PRD 契约与前端 `request.ts`：业务失败
    200（`code` 为业务码）；认证失败 401（`code=401`）；无权限 403（`code=403`）。
  - panic 由 `gin.Recovery` 恢复为统一 500 响应，不向客户端泄露堆栈。
  - `NoRoute` / `NoMethod` 输出统一 404 / 405 响应。
- **禁止**：handler 内散落 `c.JSON(status, gin.H{...})` 自定义错误结构或错误文案；
  错误码与文案一律来自 `errno`。

---

## API 错误响应

```json
{ "code": 0,    "data": { ... }, "message": "ok" }        // 成功 (response.OK)
{ "code": 1001, "data": null,    "message": "用户名或密码错误" } // 失败 (response.Fail)
```

- 始终返回 `response.Result`；不要自创一次性错误结构。
- `code=0` 成功契约与前端 `defaultResponseInterceptor`
  （`codeField: 'code'`、`dataField: 'data'`、`successCode: 0`）对齐，
  见 `apps/web-antd/src/api/request.ts`。

---

## 常见错误

- **在 `errno` 之外发明/内联错误码或文案** —— 所有业务错误码与用户文案都必须在
  `internal/pkg/errno` 定义；handler/service 层不得散落数字码或字符串文案。
- **把原始 HTTP 状态码（404/500…）当作业务 `code` 返回** —— 只有 `401`/`403`
  用作业务错误码，且仅在前端对它们做特殊处理的位置使用。
- **把内部错误字符串/堆栈泄露给客户端** —— `message` 是面向用户的；
  详细信息记录在服务端日志。
- **失败时 `data` 返回非 null 值** —— `response.Fail` 会将其置为 null。
