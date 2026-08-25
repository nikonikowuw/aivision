# Go gRPC 集成技术设计

状态：`draft`（待用户确认后进入实现）

## 1. 目标与非目标

本设计补齐 `app/` 产品后端的双向 gRPC 通信基础设施：

```text
同一个 Go cmd/api 进程
├── Gin HTTP server              tcp/:8000
├── ControlPlaneService server  unix/app.sock
├── ReportService server        unix/app.sock
└── EngineService client        unix/engine.sock

独立 C++ aivision-engine 进程
├── EngineService server        unix/engine.sock
└── ControlPlane/Report client  unix/app.sock
```

本任务只保证协议、连接、生命周期、错误边界和真实调用可验证，不创建摄像头、任务、算法包、告警、遥测、图片等业务表或 API。`PersonService.SyncPersons` 保持预留。

核心不变量：

1. `engine/proto/aivision/v1` 是唯一 Proto 源；Go 不维护第二套 `.proto` 或手写消息结构。
2. 只有业务 adapter 真实接受了上报，service 才能返回空 `code`；未配置 adapter 时 fail closed。
3. 活跃 UDS 绝不被 unlink；只清理由系统错误明确证明没有监听者的遗留 socket。
4. Engine 不在线不能阻止 Go HTTP/gRPC 服务启动；`app.sock` 自身无法安全绑定则 Go 启动失败。
5. 普通 Go 测试不依赖 C++ 构建；跨语言进程测试由独立 E2E target 承担。

## 2. 模块与依赖方向

### 2.1 文件布局

```text
app/
├── internal/proto/aivision/v1/       # 由 engine/proto 生成并提交的 *.pb.go
├── internal/pkg/engineipc/
│   ├── contracts.go                   # 业务 adapter、RemoteError 等窄契约
│   ├── services.go                    # ControlPlane/Report generated service adapters
│   ├── client.go                      # 可重连的 EngineService ClientConn 与薄包装
│   ├── socket.go                      # 遗留/活跃 socket 判定与 owner cleanup
│   ├── runtime.go                     # gRPC 注册、Start/Errors/Shutdown
│   └── *_test.go                      # 全 RPC、socket、生命周期测试
├── internal/pkg/config/config.go      # ipc.app_socket / ipc.engine_socket
├── cmd/api/main.go / servers.go       # HTTP + gRPC 联合生命周期
├── cmd/api/wire.go / wire_gen.go      # 注入 client/runtime/default adapters
├── tests/integration/                 # integration build tag，拉起真实 Engine
├── scripts/generate-proto.sh
├── scripts/test-grpc-engine-smoke.sh
├── configs/config.yaml
└── Makefile                           # proto/proto-check/grpc-e2e
```

`internal/pkg/engineipc` 是 Engine 专用传输基础设施，不引用 repository、GORM、Gin 或具体业务 service。未来业务适配器依赖 Proto DTO 并实现窄 adapter 端口，依赖方向保持：

```text
cmd/api -> business adapter -> service/repository
       \-> engineipc transport -> generated proto
```

### 2.2 入站业务 adapter 端口

MVP 不提前发明摄像头/告警领域 DTO，也不让业务层实现 generated gRPC server 接口。`engineipc` 直接以 Proto value 作为边界对象，只抽出实际业务动作：

```go
type DesiredStateAdapter interface {
    DesiredState(context.Context, uint64) (*aivisionv1.DesiredState, error)
}

type ReportAdapter interface {
    AcceptAlarm(context.Context, *aivisionv1.AlarmEvent) error
    AcceptTaskState(context.Context, *aivisionv1.TaskState) error
    AcceptInstanceState(context.Context, *aivisionv1.InstanceState) error
    AcceptMetrics(context.Context, *aivisionv1.DeviceTelemetry) error
    ReconcileOrphanImages(context.Context, []*aivisionv1.OrphanImageEntry) (OrphanDisposition, error)
}

type OrphanDisposition struct {
    RetainImageIDs []string
    DeleteImageIDs []string
}
```

私有 gRPC service 实现嵌入生成的 `Unimplemented*Server`，负责必填 nested message 校验、调用 adapter、构造响应及错误归一化。生产 Wire 显式注入 unavailable adapters；测试注入 recording fakes；后续业务任务再替换为持久化 adapters。

边界规则：

- 缺少必填 payload 使用 gRPC `codes.InvalidArgument`，不调用 adapter。
- adapter 成功后才返回空 `code`；`DesiredState(nil), nil` 视为内部实现错误。
- 默认 unavailable adapter 返回稳定 `IPC_UNAVAILABLE` 响应 code；Engine 因 code 非空不会 ACK。
- typed `AdapterError` 的稳定 code 原样进入响应；普通 Go error 记录 cause 后返回 `INTERNAL_ERROR`，只暴露受控诊断文本。
- context cancel/deadline 与显式 gRPC status 保持 transport status；panic 由 recovery interceptor 转成 `codes.Internal`。
- 业务判断不解析 `error_message`。

### 2.3 Engine 客户端

`EngineClient` 持有一个长期 `grpc.ClientConn` 和 generated client。它用共享 helper 实现 12 个同签名薄包装：transport error 原样返回，非空响应 `code` 转成可用 `errors.As` 判断的 `*RemoteError`，同时保留响应供诊断。

```go
type EngineClient struct {
    raw  aivisionv1.EngineServiceClient
    conn *grpc.ClientConn
}

func NewEngineClient(cfg *config.Config) (*EngineClient, error)
func (c *EngineClient) Close() error
// 另实现 generated EngineServiceClient 的 12 个方法。
```

约束：

- 使用 `unix://<absolute-path>` 与 `insecure.NewCredentials()`；安全边界来自本地目录、socket owner/group 和 mode，不启用 TCP fallback。
- 构造连接不使用 `WithBlock`，Engine 尚未启动时 Go 仍能启动；gRPC `ClientConn` 自行维护连接状态，后续调用可在 Engine 恢复后成功。
- deadline/cancellation 由每次调用的上层 `context.Context` 决定。包安装与查询耗时不同，传输层不硬编码统一 RPC deadline。
- `RemoteError.Code` 来自稳定响应 code；包装层不解析 `error_message`，也不把业务失败伪装成 transport status。
- 本任务不包装预留 `PersonService`；其生成类型保留，业务客户端后续单独设计。

## 3. 配置与 UDS 所有权

### 3.1 配置

在 `config.Config` 增加：

```yaml
ipc:
  app_socket: /tmp/aivision-app.sock
  engine_socket: /tmp/aivision-engine.sock
```

开发默认值与当前 C++ Engine 一致。校验要求：

- 两个路径非空、均为绝对路径；
- `filepath.Clean` 后不能相同；
- 路径中不能包含 NUL；
- 父目录由部署 Profile/systemd/launchd 或测试显式创建，App 不擅自创建安全关键运行目录。

生产部署仍应把两端配置为 `/var/run/aivision/{app,engine}.sock`。当前 C++ 仍使用 `AIVISION_*_SOCKET` 开发兼容入口；把两端统一切换到同一版本化 Deployment Profile 属于后续部署任务，本 MVP 不宣称已解决该迁移。

### 3.2 安全绑定

`app.sock` 启动前执行：

1. `lstat`：不存在则继续；symlink、普通文件及其他非 socket 对象一律拒绝启动。
2. 对已有 socket 做短超时 `net.DialTimeout("unix", path)`。
3. 连接成功表示活跃 listener，拒绝启动且绝不 unlink。
4. 仅当错误链是 `ECONNREFUSED`、`ENOENT` 或 `ENOTCONN` 时，重新 `lstat` 并确认 identity 未变化，再删除遗留 socket。
5. `net.ListenUnix` 成功后关闭自动 unlink、设置 `0660`，并记录该 socket 的 file identity。
6. Shutdown 时重新 `lstat`；只有路径仍是同一个 socket 才删除，避免误删关闭期间由其他进程创建的替代对象。

父目录不存在、不可写、路径过长、权限修改失败都属于启动失败。Go 只拥有 `app.sock`，绝不 remove 或 chmod `engine.sock`，也不得降级到 TCP 或另一个随机 socket。

## 4. Server 与进程生命周期

`engineipc.Runtime` 是 one-shot 生命周期对象：

```go
func (s *Server) Start() error
func (s *Server) Errors() <-chan error
func (s *Server) Shutdown(ctx context.Context) error
```

- `Start` 同步完成安全绑定和权限设置后才返回，然后后台执行 `grpc.Server.Serve`。
- 重复 Start、未启动 Shutdown 和重复 Shutdown 都有确定行为，不产生 goroutine/channel panic。
- `Shutdown` 先尝试 `GracefulStop`；context 到期时调用 `Stop`，等待 Serve 退出，并清理自有 socket。
- unary interceptor 只记录 method、duration 和最终 gRPC code；成功调用使用 debug，失败使用 warn/error。禁止记录完整 request/response、RTSP URL、参数 JSON 或凭据。
- recovery interceptor 捕获 panic、记录内部错误并返回 `codes.Internal`。

`cmd/api/main.go` 的启动顺序：

```text
config/root check
-> Wire/DB/schema check
-> NTP replay
-> NetworkService.Start
-> 预绑定 HTTP TCP listener
-> engineipc.Runtime.Start (绑定 app.sock)
-> 启动 HTTP Serve
-> 等待 SIGINT/SIGTERM、HTTP Serve error 或 gRPC Serve error
```

任一 listener 启动失败都关闭已获得资源并使进程启动失败。Engine client 的目标 socket 不存在不属于启动失败。

停机在现有 10 秒总窗口内：

```text
并发停止 HTTP 与 gRPC 接收新请求
-> gRPC 在 context 到期前 GracefulStop，超时则强制 Stop
-> EngineClient.Close
-> NetworkService.Close（使用同一总 deadline 的剩余时间）
-> logger sync
```

HTTP/gRPC serve error 与 OS signal 走同一关闭路径；goroutine 内不调用 `log.Fatal` 绕过清理。并发停止 admission 避免一个 server 耗尽整个 deadline，但 socket 删除仍在 gRPC Serve 完全退出后按 identity 执行。

## 5. Proto 生成与依赖

生成命令以 `engine/proto` 为输入，使用已有 `go_package` 和 module mapping 输出到 `app/internal/proto/aivision/v1`。四个 proto 一起生成，生成文件提交仓库。

`app/scripts/generate-proto.sh` 必须：

- 检查 `protoc`、`protoc-gen-go`、`protoc-gen-go-grpc`，缺失时给出明确安装/版本提示；
- 固定并记录 Go 插件版本；允许通过 `PROTOC` 指向受控工具链中的编译器；
- 支持指定临时输出根目录，供漂移检查使用；
- 不读取或复制 `engine/tests/stub_server/gen`。

Make target：

- `make -C app proto`：更新提交的生成文件；
- `make -C app proto-check`：生成到临时目录并与提交文件逐文件比较，不污染工作树；
- 普通 `build/test` 只编译已提交生成物，不要求本机安装生成工具。

新增 gRPC/Protobuf 依赖锁定在 `app/go.mod/go.sum`。不修改 Proto 字段；若生成暴露契约错误，先回到规划评审，不能在 Go 侧打补丁制造分叉。

## 6. 测试设计

### 6.1 普通 Go 测试

全部使用真实临时 Unix socket，不用 `bufconn` 代替 UDS：

1. **Server 全 RPC 合约**：recording adapters 覆盖 6 个入站 RPC，逐项断言请求映射、成功空 `code`、typed adapter code、默认 `IPC_UNAVAILABLE`、普通 error 的 `INTERNAL_ERROR`、InvalidArgument 和 panic status。
2. **Client 全 RPC 合约**：fake `EngineServiceServer` 覆盖 12 个 RPC；通过 `EngineClient` 逐项调用并断言请求/响应及 typed `RemoteError`。
3. **连接失败与恢复**：Engine socket 不存在时调用在 context deadline 内失败；同一 ClientConn 在 fake server 后续出现或重启后可成功调用。
4. **socket 所有权**：不存在、遗留 socket、活跃 socket、symlink、普通文件、identity replacement、两个路径相同、权限失败、关闭清理。
5. **生命周期**：重复调用、正常 GracefulStop、超时强制 Stop、Serve error、停机后新调用失败且 socket 消失。
6. **配置**：默认值、YAML、`APP_IPC_*` override、相同/相对/空路径拒绝。
7. **并发/竞态**：并发 RPC 与 Shutdown 运行 `go test -race ./internal/pkg/engineipc`。

### 6.2 真实 C++ Engine E2E

integration build-tag 测试使用专用 mock Engine 构建目录；`make -C app grpc-e2e` 调用脚本配置/构建后再运行测试：

1. 创建临时 runtime/package/image 目录和两个 socket 路径。
2. 启动带成功 recording adapters 的产品 `engineipc.Runtime`。
3. 以 `AIVISION_ENGINE_SOCKET`、`AIVISION_APP_SOCKET` 等临时环境启动真实 mock `aivision-engine`。
4. 使用产品 `EngineClient.QueryProfile` 调用 C++ `engine.sock`，断言 transport 成功、无 `RemoteError` 且 profile 非空。
5. 等待并断言 C++ 至少调用一次 Go `GetDesiredState` 和 `ReportMetrics`。
6. 终止 Engine，验证进程、连接和双方自有 socket 有序清理；测试退出路径始终回收子进程。

跨语言冒烟只验证通信兼容性，不重复现有 Engine 包安装/任务业务 E2E。普通 `go test ./...` 不触发 C++ 构建或子进程。

## 7. 可观测性、安全与错误矩阵

| 场景 | 结果 |
| --- | --- |
| `app.sock` 被活进程监听 | Go 启动失败，不删除 socket |
| `app.sock` 是确认的遗留 socket | 删除后绑定，并记录 warn |
| `app.sock` 是普通文件或未知探测错误 | Go 启动失败，不删除文件 |
| C++ Engine 启动晚或重启 | Go 保持运行；调用受 context 控制，连接恢复后可再次成功 |
| 未注入业务 adapter | gRPC OK + `code=IPC_UNAVAILABLE`，不得 ACK |
| adapter 返回稳定业务错误 | gRPC OK + 对应响应 code |
| adapter 返回普通内部错误 | gRPC OK + `code=INTERNAL_ERROR`，不暴露内部 cause |
| 非法请求或 service panic | `InvalidArgument` / `Internal` transport status |
| Engine 响应 code 非空 | Go 返回响应及 typed `RemoteError` |
| gRPC 优雅停机超过总 deadline | 强制 Stop，返回/记录 shutdown error |
| 两个配置路径相同 | 配置加载失败 |

UDS 使用 insecure transport 是既有本机部署约束，不表示无访问控制；生产必须依赖 runtime 目录 owner/group、目录最小权限和 socket `0660`。当前没有 `SO_PEERCRED` 等 peer 身份授权，UDS 文件权限是唯一认证边界。现有 Docker Compose 也没有 Engine 容器或共享 runtime volume，容器化部署接通不属于本任务。日志不得包含请求 payload、RTSP 凭据、算法参数或完整 `error_message`。

## 8. 回滚与后续迭代

回滚本任务只需停止注册 `engineipc.Runtime`、移除 `EngineClient` 注入并恢复原 HTTP 生命周期；没有数据库迁移或持久化状态需要回滚。关闭时只删除经 identity 复核仍由本进程拥有的 `app.sock`。

后续业务迭代按独立任务接入：

- DesiredState/revision 持久化及任务配置事务；
- 告警 `event_id` 幂等落库；
- 任务/实例状态、遥测和孤儿图片策略；
- HTTP API、Webhook、前端；
- 统一 Go/C++ 读取版本化 Deployment Profile；
- `PersonService` 人员/特征业务。
