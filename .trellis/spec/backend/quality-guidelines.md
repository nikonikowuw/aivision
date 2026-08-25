# 质量规范

> 后端开发的代码质量标准。

---

## 概览

- 格式与静态检查：`gofmt` + `go vet ./...`（`make vet`）。当前代码树上两者均
  通过。
- 测试：`go test ./...`（`make test`）必须保持通过。
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
- 优先表驱动/小而聚焦的测试，而非笨重的 mock；目前尚未引入 mock 框架。

---

## 代码评审检查清单

- `gofmt -l .` 无输出（已格式化），`go vet` 干净，`go test ./...` 全部通过。
- 业务失败使用 `errno` 错误码 + `response.Fail`；无内部细节/密钥泄露。
- 模型遵循命名/索引/软删除约定（特别是必须使用 `gorm.io/plugin/soft_delete`，且 `deleted_at = 0` 表示活跃）；无新增外键。
- `wire_gen.go` 是最新的（重新生成，而非手工编辑）。
- 新增配置键同时在 `defaults()` 和 `validate()` 中注册。
- 无裸魔数——业务数字均使用命名常量（错误码、枚举/状态值、超时、重试次数）。
