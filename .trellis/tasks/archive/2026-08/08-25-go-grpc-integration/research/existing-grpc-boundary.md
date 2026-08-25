# 现有 gRPC 边界与 MVP 集成调研

## 1. 当前契约与职责

Proto 唯一事实来源位于 `engine/proto/aivision/v1/`：

- `engine.proto`：`EngineService` 运行在 C++ Engine 的 `engine.sock`，由 Go 调用，共 12 个 unary RPC。
- `app.proto`：`ControlPlaneService` 与 `ReportService` 运行在 Go App 的 `app.sock`，由 Engine 调用，共 6 个 unary RPC。
- `person.proto`：`PersonService.SyncPersons` 仅预留，当前 C++ 返回 `UNIMPLEMENTED`，不属于本任务业务范围。
- 四个 proto 的 `go_package` 均已固定为 `niko-vue-admin/app/internal/proto/aivision/v1;aivisionv1`。

运行时规范要求：

- transport 失败使用 gRPC status；业务失败使用响应内稳定字符串 `code`，空串表示成功。
- Go 是 DesiredState/revision 的持久化权威；Engine 是运行实例和图片物理文件的执行权威。
- Engine 仅在上报得到成功 ACK 后把图片标记为 `reported`。没有持久化适配器时 Go 必须 fail closed，不能成功 ACK 后丢弃消息。
- 活跃 socket 不得被 unlink 抢占；只允许识别并清理已失去监听者的遗留 socket。

相关规范：

- `.trellis/spec/engine/runtime-guidelines.md`
- `.trellis/spec/engine/deployment-profile.md`
- `.trellis/spec/engine/error-observability-guidelines.md`

## 2. C++ Engine 现状

实现位置：

- 服务端：`engine/src/core/ipc/uds_server.cpp`
- 客户端：`engine/src/core/ipc/uds_client.cpp`
- 进程启动：`engine/src/app/main.cpp`

开发默认路径：

- `AIVISION_ENGINE_SOCKET`，默认 `/tmp/aivision-engine.sock`
- `AIVISION_APP_SOCKET`，默认 `/tmp/aivision-app.sock`

C++ 已实现：

- `EngineService` 全部 RPC 的服务注册。
- `PersonService` 注册但业务方法返回 `UNIMPLEMENTED`。
- 每 2 秒拉取 DesiredState，并上报任务/实例状态；每 10 秒上报遥测。
- Engine -> Go 调用固定 2 秒 deadline；只有 transport 成功且响应 `code` 为空才视为成功。

C++ socket 启动逻辑会探测已有 Unix socket：连接成功说明有活跃服务并拒绝抢占；仅对 `ECONNREFUSED`、`ENOENT`、`ENOTCONN` 的遗留 socket 执行清理。Go 侧应保持同样语义。

## 3. Go 产品后端缺口

`app/` 当前：

- `go.mod` 没有直接依赖 `google.golang.org/grpc`。
- 没有 `internal/proto/aivision/v1` 生成代码。
- 没有 UDS listener、gRPC server 或 Engine client。
- `cmd/api/main.go` 只启动 Gin HTTP server；Wire 产物只持有 HTTP、DB、NTP 和 Network 生命周期依赖。
- 现有 9 张表只覆盖 RBAC、刷新令牌、操作日志和系统配置，没有摄像头、任务、实例、告警、遥测或图片引用模型。

因此 MVP 应只提供通信基础设施和可注入 adapter，不创建伪持久化层。产品默认 adapter 必须让响应返回稳定 `IPC_UNAVAILABLE`，确保 Engine 不会 ACK；测试注入内存 fake 覆盖成功和其他业务 `code` 路径。

## 4. 已有测试基础设施

`engine/tests/stub_server/` 是独立 Go module，已有：

- Go Stub `ControlPlaneService` / `ReportService` 内存实现；
- Go `EngineService` 命令行客户端；
- 独立生成的 Go protobuf 文件。

`engine/tests/e2e.sh` 会：

1. 构建 Stub 和客户端；
2. 启动 Go Stub 的 `app.sock`；
3. 启动真实 `aivision-engine` 的 `engine.sock`；
4. 调用 QueryProfile、QueryMetrics、InstallPackage、ApplyDesiredState；
5. 验证 Engine 回调任务/实例状态；
6. 验证 DesiredState 拉取与 Stub 重启重连。

该套件证明 C++ 端协议可运行，但不能替代 `app/` 产品实现。本任务应新增 app-owned 的跨语言 E2E：测试进程启动产品 gRPC 组件和内存 fake，再拉起真实 C++ Engine，至少验证 Go -> Engine `QueryProfile` 与 Engine -> Go `GetDesiredState`/`ReportMetrics`。

普通 `go test ./...` 应只运行快速 Go UDS 合约测试；真实 Engine E2E 使用独立 Make target。

## 5. 代码生成与工具现状

当前工作环境：

- Go：`go1.26.5 linux/amd64`
- 系统 PATH 中没有 `protoc`、`protoc-gen-go` 或 `protoc-gen-go-grpc`。
- `engine/tests/stub_server/gen/` 有旧生成物，生成头显示 `protoc-gen-go v1.33.0`、`protoc-gen-go-grpc v1.3.0`，但这些文件不能作为产品侧手写或复制契约的替代品。

实施必须增加显式、固定版本的生成命令，并提交 `app/internal/proto/aivision/v1/*.pb.go`，使普通 build/test 不依赖生成工具。生成门禁应能从 `engine/proto` 重建并检查工作树无漂移。工具缺失时必须报清晰错误，不得静默沿用过期生成物。

## 6. 已冻结范围

- Go 与 Gin 同一个 `cmd/api` 进程，同时监听 HTTP TCP 与 `app.sock` UDS。
- Go 同时提供 `app.sock` server 和 `engine.sock` client。
- 不新增数据库迁移、业务 REST API、前端、Webhook 或持久化实现。
- Go 合约测试覆盖全部当前 RPC；跨语言测试使用真实 C++ Engine 的 mock platform，不依赖摄像头、模型或 NPU。
