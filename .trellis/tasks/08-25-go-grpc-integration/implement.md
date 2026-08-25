# Go gRPC 集成实施计划

状态：`planning`；只有用户确认 PRD、Design 与本计划后才执行 `task.py start` 并进入编码。

## M0. 实现前门禁与基线

- [ ] 用户确认 MVP 范围：双向通信基础设施、同一 Go API 进程、fail closed、真实 C++ Engine E2E；不含业务持久化/API。
- [ ] 运行 `python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-25-go-grpc-integration`，确认 `implement.jsonl`/`check.jsonl` 均有真实上下文。
- [ ] 记录当前 `make -C app test`、`make -C app vet` 基线；检查 dirty worktree，只修改本任务文件和明确产品路径，不覆盖并行的 C++ logging、face recognition 等改动。
- [ ] 进入实现前使用 `trellis-before-dev` 重新载入 manifests 中的规范与调研文件。

成功标准：规划无开放问题，Trellis 校验通过，现有失败已与本任务新增失败区分。

## M1. Proto 生成与 Go 依赖

- [ ] 先增加生成漂移测试/命令的失败基线，证明 `app/` 当前没有产品生成物且工具缺失会明确失败。
- [ ] 在 `app/go.mod/go.sum` 锁定兼容的 `google.golang.org/grpc`、`google.golang.org/protobuf` 及生成插件版本。
- [ ] 新建 `app/scripts/generate-proto.sh`：只读取 `engine/proto/aivision/v1/*.proto`，支持提交目录与临时目录输出，校验工具并打印版本要求。
- [ ] 在 `app/Makefile` 增加 `proto`、`proto-check`；`proto-check` 临时生成并逐文件比较，不修改工作树。
- [ ] 生成并提交 `app/internal/proto/aivision/v1/{common,engine,app,person}{,_grpc}.pb.go`，禁止复制或修改 `engine/tests/stub_server/gen`。
- [ ] 添加编译/描述符 smoke test，确认包名、完整 service descriptor 和 18 个本任务 RPC 与权威 Proto 一致。

验证：

```bash
make -C app proto-check
cd app && go test ./internal/proto/aivision/v1
```

回滚点：生成工具或版本无法稳定重现时，不继续手写 IPC；先解决工具链或显式调整设计。

## M2. IPC 配置与安全 socket 生命周期

- [ ] 先在 `app/internal/pkg/config/config_test.go` 增加 `ipc.app_socket` / `ipc.engine_socket` 的默认、YAML、env override、空值、相对路径和同路径失败用例。
- [ ] 修改 `config.Config`、`defaults()`、`validate()` 与 `configs/config.yaml`，开发默认匹配当前 C++ `/tmp/aivision-{app,engine}.sock`。
- [ ] 在 `app/internal/pkg/engineipc/socket_test.go` 先覆盖：路径不存在、遗留 socket、活跃 socket、symlink、普通文件、探测未知错误、identity 变化和 owner cleanup。
- [ ] 实现 `socket.go` 的 `lstat -> dial probe -> re-lstat -> conditional unlink -> ListenUnix`；关闭自动 unlink，chmod `0660` 并记录 file identity。父目录不存在时直接失败，不自动创建生产运行目录。
- [ ] 确认 Shutdown 只在路径仍匹配记录 identity 时删除 `app.sock`，且从不 remove/chmod `engine.sock`。

验证：

```bash
cd app && go test ./internal/pkg/config ./internal/pkg/engineipc -run 'Test.*Socket|Test.*IPC'
```

回滚点：任何路径判定不明确时 fail closed；不得用无条件 `os.Remove(appSocket)` 作为临时实现。

## M3. Go 入站 gRPC server

- [ ] 先写真实临时 UDS recording fakes，实现 `DesiredStateAdapter` 和 `ReportAdapter`，逐项调用 `GetDesiredState` 与 5 个 Report RPC。
- [ ] 实现 adapter 端口、`OrphanDisposition`、私有 generated-service implementations 与生产 unavailable adapters。
- [ ] 先钉死错误矩阵：成功空 `code`；typed `AdapterError` 原样返回稳定 code；未配置返回 `IPC_UNAVAILABLE`；普通 Go error 返回 `INTERNAL_ERROR` 且隐藏 cause；缺少 nested payload 为 `InvalidArgument`；panic 为 `Internal`。
- [ ] 实现 unary structured logging/recovery interceptor；只记录 method/duration/status/受控 code，不记录 payload 或诊断正文。
- [ ] 实现 `Runtime.Start/Errors/Shutdown`，覆盖重复调用、Serve error、正常 GracefulStop 与 deadline 后强制 Stop。
- [ ] 增加并发 RPC + Shutdown 的 race 测试，确保无 channel close、listener 或 goroutine 竞态。

验证：

```bash
cd app && go test ./internal/pkg/engineipc -run 'TestRuntime|TestControlPlane|TestReport'
cd app && go test -race ./internal/pkg/engineipc
```

回滚点：如果 adapter 未真实接受数据，任何路径都不得返回空 `code`。

## M4. EngineService 客户端

- [ ] 先写 fake `EngineServiceServer`，通过真实临时 `engine.sock` 覆盖全部 12 个 RPC，请求和响应逐项断言。
- [ ] 实现持有长期 `ClientConn` 的 `EngineClient`；12 个 generated-signature 薄包装共用响应 code 检查 helper，不复制连接/错误逻辑。
- [ ] 定义 typed `RemoteError`；transport error 原样返回，非空响应 code 返回响应和 `RemoteError`，调用方只判断 `Code` 而不解析 `error_message`。
- [ ] 使用 UDS + insecure credentials；禁止 TCP fallback、全局硬编码 deadline 和构造阶段阻塞连接。
- [ ] 测试 Engine 不存在时 context 内失败、同一 ClientConn 在 server 后启动/重启后恢复、Close 后调用失败。

验证：

```bash
cd app && go test ./internal/pkg/engineipc -run 'TestEngineClient'
```

回滚点：如果连接重建必须替换整个业务 client，则先修正 ClientConn 生命周期，不在调用方散落重复 Dial/Close 或 response code 判断。

## M5. Wire 与 HTTP/gRPC 联合生命周期

- [ ] 为 `cmd/api` 增加生命周期测试或可测试 helper，先覆盖 HTTP bind 失败、gRPC bind 失败、serve error、signal shutdown 和共享超时。
- [ ] 扩展 `App`，持有 `*engineipc.Runtime` 和 `*engineipc.EngineClient`；Wire 注入 unavailable adapters、client 和 runtime。
- [ ] 调整 `main.go`：schema/NTP/Network ready 后预绑定 HTTP，再绑定 `app.sock`；任何启动失败回收已获得资源。
- [ ] 用 error channels 统一处理 HTTP/gRPC Serve error 和 SIGINT/SIGTERM；移除 goroutine 内绕过清理的 `log.Fatal` 路径。
- [ ] 停机时在同一个 10 秒 context 内并发停止 HTTP/gRPC admission；gRPC 超时强制 Stop，随后关闭 EngineClient，再用剩余时间关闭 Network。
- [ ] 执行 `make -C app wire` 重新生成 `wire_gen.go`，禁止手改生成文件。
- [ ] 验证 Engine 未运行时 Go 构造和监听仍成功；`app.sock` 被活进程占用时启动失败。

验证：

```bash
make -C app wire
cd app && go test ./cmd/api
```

回滚点：任一 server 启动失败不能留下另一 listener 或 socket；停机失败必须记录但继续关闭其余资源。

## M6. 真实 C++ Engine 跨语言 E2E

- [ ] 新增 integration build-tag 测试：临时目录启动产品 `engineipc.Runtime` + 成功 recording adapters，再拉起真实 mock `aivision-engine`。
- [ ] 在 `app/Makefile` 增加独立 `grpc-e2e` target；脚本使用专用 mock build 目录配置/构建 Engine，再向测试传入绝对二进制路径，普通 `test` target 不依赖 C++。
- [ ] Go -> C++：用产品 `EngineClient.QueryProfile` 断言 transport 成功、无 `RemoteError`、profile 非空。
- [ ] C++ -> Go：等待并断言 `GetDesiredState` 和 `ReportMetrics` 至少各一次；使用 channel/condition，不用固定长 sleep。
- [ ] 对每个失败/超时/中断路径终止并 wait 子进程，验证临时 socket 与目录清理。

验证：

```bash
make -C app grpc-e2e
make -C engine test
```

回滚点：跨语言测试失败时保留日志和临时诊断路径直到根因确认；不能用 Go fake-to-Go fake 成功替代真实 C++ 证据。

## M7. 规范、质量门禁与交付检查

- [ ] 新增或更新 backend IPC 规范，记录 package 边界、两个 UDS、错误语义、socket ownership、context、停机和测试要求；在 backend index 链接。
- [ ] 核对 `.trellis/spec/engine/runtime-guidelines.md` 与实际 Go 行为；若发现既有规范冲突，先修规划/规范再改实现。
- [ ] 对本任务修改的 Go/Shell 文件格式化和 lint；确认无生成物手改、无 payload 日志、无数据库迁移和业务伪实现。
- [ ] 执行完整快速测试、race、proto 漂移检查、真实 Engine E2E 和 diff 检查。

最终命令：

```bash
make -C app proto-check
make -C app wire
make -C app vet
make -C app test
cd app && go test -race ./internal/pkg/engineipc
make -C app grpc-e2e
make -C engine test
git diff --check
python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-25-go-grpc-integration
```

完成标准：所有命令通过；若某项因外部工具或平台无法运行，任务不得标记完成，必须记录明确 blocker 和未验证风险。

## 变更后复核清单

- [ ] 权威 `.proto` 与提交的 Go 生成物无漂移。
- [ ] 6 个 Go server RPC、12 个 Engine client RPC 均有真实 UDS 调用覆盖。
- [ ] 默认 adapter fail closed，以 `IPC_UNAVAILABLE` 返回且没有 ACK 后丢弃数据的路径。
- [ ] 活跃/遗留/symlink/非 socket/identity replacement 与 owner cleanup 均有测试。
- [ ] Engine 缺席不阻止 Go 启动，`app.sock` 绑定失败阻止 Go 启动。
- [ ] HTTP、gRPC、EngineClient、Network 使用统一关闭路径，无 goroutine 内 Fatal。
- [ ] 跨语言 E2E 使用真实 `aivision-engine`，并同时证明两个方向。
- [ ] 没有新增数据库模型、迁移、REST API、前端或 PersonService 业务。
