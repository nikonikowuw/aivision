# 质量规范

> 后端开发的代码质量标准。

---

## 概览

- 格式与静态检查：`gofmt` + `go vet ./...`（`make vet`）。当前代码树上两者均
  通过。
- 测试：`go test ./...`（`make test`）必须保持通过；`make proto-check` 必须无漂移；
  `make grpc-e2e`（跨语言 Go<->C++ engine 冒烟）在改动 engineipc/proto 后应运行。
- 项目遵循骨架设计（`.trellis/tasks/08-16-backend-skeleton/design.md`）确立的
  约定及其引用的决策（决策 17 i18n key、决策 18 mysql/postgres）。

---

## 禁止模式

- **标签中的驱动特定 gorm 类型** —— `type:tinyint` 之类会破坏 Postgres
  （决策 18）；使用纯 Go 类型。
- **外键** —— 关系仅限逻辑关系。
- **在 `internal/pkg/errno` 之外新增或内联业务错误码/文案**，以及在
  `internal/pkg/response` 之外自创一次性响应结构。
- **依赖 gorm 默认命名** —— 始终显式声明 `TableName()` 和列标签。
- **手工编辑 `cmd/api/wire_gen.go`** —— 修改 `wire.go` 或 wire 装配的构造函数
  签名后，用 `make wire` 重新生成。
- **裸魔数** —— 有业务语义的数字（错误码、枚举/状态值、超时、重试次数等）直接
  以字面量散落在业务代码中，而不是使用命名常量。

---

## 必用模式

- **修改完代码后必须进行格式整理**：运行 `gofmt -w .` 自动格式化，并确认
  `gofmt -l .` 无输出（未格式化的文件会被列出）。这是提交前不可跳过的步骤，
  先格式化，再进入 `make vet` / `make test` 质量关卡。
- **必须包含完备的代码注释**：新增/修改代码严禁裸写无注释逻辑。每个包和非平凡函数都必须带文档注释（`// Package …`、`// New …` 等），业务代码内部的关键流程（事务流转、软删除查询、鉴权逻辑、复杂数据组装）必须包含清晰的中文行内注释。
- 构造函数返回 `(T, error)`，并用 `%w` 包装失败。
- 模型按数据库规范内嵌 `BaseModel` / `TimeFields` 并声明 `TableName()`。
- wire DI 是依赖注入路径——不要用 `sync.Once` 或全局变量为应用依赖
  （config、logger、db）构建单例。
- **有业务语义的数字必须使用命名常量**：错误码在 `errno`（`CodeXXX`）、枚举值如
  菜单类型（`MenuTypeCatalog` 等）、状态值（启用/禁用）、重试/超时次数（如
  `db.New` 的 `connectRetries`）。gorm 标签里的 `default:1` 这类驱动层字面量除外——
  在业务代码中比较/判断这些值时用常量，不要裸写 `1`/`0`。
- **HTTP 错误响应统一由中间件输出**：handler 只做业务处理并交错误（携带 `errno`
  码），不直接拼接错误响应；统一出口在 `internal/router` 挂载的错误处理中间件
  （见 error-handling.md）。

---

## 测试要求

- 完成任务前 `make test` 必须通过，`make vet` 必须干净。
- 模型/纯函数逻辑使用 **sqlite 内存库** 做单元测试（`smoke_test.go` 中的
  `newSmokeDB` —— 通过 `t.Name()` 每个测试一个内存库），无需外部服务器。
- 响应契约由 JSON 测试钉死（`response_test.go` 断言精确的 `{code,data,message}`
  JSON）——契约变更时保持同步。
- **proto 生成代码只由 `scripts/generate-proto.sh` 产出**：修改
  `engine/proto/aivision/v1/*.proto` 后运行 `make proto` 重新生成并提交；
  `make proto-check` 通过 `diff` 校验提交目录与新鲜生成一致（排除 `*_test.go`）。
  生成文件包含 descriptor 冒烟测试（`descriptor_smoke_test.go`）钉住 RPC 面。
- **依赖真实 C++ engine 的测试**放 `tests/integration`（`//go:build integration`），
  只由 `make grpc-e2e` 运行；普通 `go test ./...` 不依赖 C++ 构建。
- **UDS socket 生命周期**（`engineipc/socket.go`）：绑定前探测并清理
  `ECONNREFUSED` 的陈旧 socket；关闭时仅删除经 identity 复核仍属于本进程的
  socket 文件；engine 缺席时 Go 侧仍可独立监听。
- 优先表驱动/小而聚焦的测试，而非笨重的 mock；目前尚未引入 mock 框架。

---

## gRPC over UDS（engineipc）契约

`internal/pkg/engineipc` 是 Go 与 C++ engine 的 gRPC-over-UDS 通信层，遵循以下
稳定契约（MVP 错误矩阵钉死在 `server_test.go` / `client_test.go`）：

- **成功 = 响应 `code` 为空串**；只有业务 adapter 真实接受了数据才返回空 `code`。
  未注入 adapter 时 fail closed，返回稳定 `IPC_UNAVAILABLE`，禁止对未持久化的
  告警/状态/遥测/孤儿图片上报 ACK。
- **业务失败 ≠ 传输失败**：业务失败返回 gRPC OK + 非空响应 `code`（稳定码，如
  `IPC_UNAVAILABLE` / `INTERNAL_ERROR`）；传输失败（连接、超时）才用 gRPC status。
  普通 Go error 统一归一化为 `INTERNAL_ERROR`，只暴露受控诊断文本，不泄露内部 cause。
- **调用方只判断稳定 `Code`**（`*RemoteError` / `AdapterError`），绝不解析
  `error_message` 文本；context cancel/deadline 与显式 gRPC status 保持 transport
  status，不降级为响应内业务码。
- **adapter 成功但返回 nil DesiredState 视为内部错误**（归一化 `INTERNAL_ERROR`），
  绝不伪装成功。
- **停机**：`Runtime.Shutdown` 先 `GracefulStop`，超时强制 `Stop`，等待 Serve 退出
  后按 identity 删除自有 `app.sock`；HTTP 与 gRPC 在同一超时窗口内并发停止，
  EngineClient 关闭后 Network 再关。

---

## 代码评审检查清单

- `gofmt -l .` 无输出（已格式化），`go vet` 干净，`go test ./...` 全部通过。
- 业务失败使用 `errno` 错误码 + `response.Fail`；无内部细节/密钥泄露。
- 模型遵循命名/索引/软删除约定（特别是必须使用 `gorm.io/plugin/soft_delete`，且 `deleted_at = 0` 表示活跃）；无新增外键。
- `wire_gen.go` 是最新的（重新生成，而非手工编辑）。
- `internal/proto/aivision/v1` 生成代码与 proto 权威源无漂移（`make proto-check`）；
  `wire_gen.go` 已用 `make wire` 重新生成。
- engineipc 改动后 `make grpc-e2e` 通过（Go<->真实 C++ engine 双向通信）。
- engineipc 错误契约：adapter 未接受数据不返回空 `code`；业务失败用稳定响应
  `code`（`IPC_UNAVAILABLE`/`INTERNAL_ERROR`），传输失败才用 gRPC status；
  调用方不解析 `error_message` 文本。
- 新增配置键同时在 `defaults()` 和 `validate()` 中注册。
- 无裸魔数——业务数字均使用命名常量（错误码、枚举/状态值、超时、重试次数）。
