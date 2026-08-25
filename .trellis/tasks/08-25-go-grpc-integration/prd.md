# Go gRPC integration

## Goal

在 `app/` 产品后端中接入既有 `aivision.v1` Protobuf/gRPC 契约，使 Go 与 C++ Engine 能通过两个独立 Unix Domain Socket 建立可验证的控制与上报通道，并用自动化调用测试证明协议、生命周期和错误语义正确。

用户价值：补齐当前只有 C++ Engine 和测试 Stub、没有 Go 产品实现的跨进程通信缺口，为后续任务下发、状态同步、告警落库和设备监控提供稳定边界。

## Confirmed Facts

- Proto 权威源位于 `engine/proto/aivision/v1/`，`go_package` 已固定为 `niko-vue-admin/app/internal/proto/aivision/v1;aivisionv1`。
- `engine.sock` 由 C++ Engine 监听，Go 作为全部现有 `EngineService` RPC 的客户端；`PersonService` 仅生成契约，业务调用不属于本任务。
- `app.sock` 应由 Go 监听，C++ Engine 作为 `ControlPlaneService` 和 `ReportService` 客户端。
- `ControlPlaneService` 包含 `GetDesiredState`；`ReportService` 包含告警、任务状态、实例状态、遥测和孤儿图片上报。
- C++ Engine 已实现 `EngineService` 服务端及访问 `app.sock` 的客户端。
- `engine/tests/stub_server/` 已有独立 Go 测试模块、生成代码和可运行的 `ControlPlaneService` / `ReportService` 内存实现；它不属于 `app/` 产品代码，不能替代本任务交付。
- `app/` 当前未引入 `google.golang.org/grpc`，也没有产品侧生成代码、UDS 服务端或 Engine 客户端。
- `app/` 当前 9 张业务表只覆盖 RBAC、刷新令牌、操作日志和系统配置；尚无摄像头、任务、算法实例、告警、遥测或图片引用模型，现有 repository/service 无法直接承接 Engine 上报。
- Go 是 DesiredState 与 revision 的持久化权威；Engine 重启或 `app.sock` 重连后通过 `GetDesiredState` 拉取并全量对账。
- 传输错误使用 gRPC status；业务失败使用响应中的稳定 `code`，`error_message` 只供诊断，调用方不得解析其文本。
- 视频帧、图片字节和张量不得进入当前常规 RPC；图片仅传 `image_id` 与受限相对路径。

## Requirements

- R1：复用既有 proto 契约，不在 Go 侧复制或手写第二套消息结构。
- R2：两个 UDS 端点保持职责隔离，socket 路径可由配置提供，并支持有序启动与优雅停止。
- R3：交付产品侧完整双向 gRPC 基础设施：Go 在 `app.sock` 提供 `ControlPlaneService` / `ReportService`，并作为客户端通过 `engine.sock` 调用 `EngineService`。
- R4：本任务是 MVP 通信层，仅提供可注入的业务接口与测试替身；不新增摄像头、任务、算法包、告警、遥测或图片相关数据库模型、迁移和完整后端 API。
- R5：测试必须发起真实 gRPC 调用并验证响应，不以仅监听 socket、仅编译生成代码或直接调用 handler 代替传输测试。
- R6：业务失败断言稳定 `code`；传输失败断言 gRPC status，不解析 `error_message` 文本。
- R7：MVP 未注入业务适配器时必须以稳定 `IPC_UNAVAILABLE` fail closed，禁止对未持久化的告警、状态、遥测或孤儿图片上报返回成功 ACK；测试通过注入内存 fake 覆盖成功路径。
- R8：`app.sock` gRPC server 与现有 Gin HTTP server 运行在同一个 `cmd/api` 进程中，但使用独立 listener；两者统一接受 SIGINT/SIGTERM 并在同一超时窗口内优雅停止。

## Acceptance Criteria (Draft)

- [ ] `app/` 可生成并编译 `aivision.v1` Go protobuf/gRPC 代码，且生成结果与权威 proto 一致。
- [ ] Go `app.sock` server 的 6 个现有 RPC 与 `engine.sock` client 的 12 个现有 `EngineService` RPC 均有真实临时 UDS 合约测试。
- [ ] 合约测试覆盖成功、业务失败、传输失败、重连和优雅关闭，不以直接调用 Go handler 代替传输。
- [ ] `make -C app test` 与 `make -C app vet` 通过。
- [ ] Go 在测试环境通过真实 UDS 同时完成一次 `app.sock` 入站 RPC 和一次 `engine.sock` 出站 RPC。
- [ ] 独立跨语言 E2E 启动现有 C++ `aivision-engine`（mock platform 即可），证明 Go 调用 Engine 且 Engine 回调 Go；该测试不并入普通 `go test ./...`。

## Key Decisions

- D1：本次选择完整双向基础设施，不交付仅服务端或仅客户端的半链路。
- D2：本次定位为 MVP 通信基础设施；业务持久化、数据库迁移及管理/查询 API 在后续迭代实现。
- D3：未配置业务适配器时返回稳定 `IPC_UNAVAILABLE`；成功 ACK 仅允许由明确接受数据的实现返回。
- D4：调用验收同时包含 Go UDS 合约测试与真实 C++ Engine 跨语言 E2E；硬件、摄像头和真实算法模型不属于本通信测试前提。
- D5：gRPC 与 Gin HTTP 共用现有 `cmd/api` 进程和 Wire 依赖图，分别监听 UDS 与 TCP；不新增第二个 Go 守护进程。

## Out of Scope (Provisional)

- 修改既有 proto 字段或 C++ Engine RPC 业务实现，除非集成测试证明存在阻断性契约缺陷并重新评审。
- `PersonService.SyncPersons` 的人脸库业务实现；当前仍为预留接口。
- 前端页面、Webhook、数据库持久化，以及摄像头、任务、算法包、告警查询、监控等完整后端业务 API。
- 将测试替身或临时内存状态宣称为生产持久化实现。

## Open Questions

无阻塞项。
