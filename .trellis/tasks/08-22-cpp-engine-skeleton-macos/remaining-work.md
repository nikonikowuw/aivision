# C++ Engine / SDK 后续未完成工作

> 对应任务：`08-22-cpp-engine-skeleton-macos`
>
> 本文记录当前 T1 骨架、SDK、macOS 适配层和算法包完成后的剩余工作。优先级按“是否阻断 T1 验收、是否影响生产安全、是否可由本地确定性测试验证”划分。
>
> 目标不是继续堆功能，而是把剩余事项拆成可以单独实现、验证和回滚的工作包。

## 1. 当前基线

### 已完成并验证

- Engine / SDK / macOS 平台 / mock 平台 / `yolov8n` 算法包已形成可构建闭环。
- Debug、mock ASan、mock TSan、macOS Debug 的 unit/contract tests 已通过。
- SDK C11、AppleClang C++20 头文件语法检查和 Engine boundary check 已通过。
- 算法包可在仓库外独立构建、运行、benchmark、打包；目录包和 ZIP 包均可通过 `package_validator`。
- 七步包校验、路径安全、原子安装、升级、回滚、卸载保护、动态库唯一导出检查已实现。
- H.264/H.265 NAL 解析、参数集收集、IDR/IRAP gate、decoder watchdog reset、断流重连和指数退避已实现并有 mock 回归测试。
- Image catalog 原子写入、孤儿清理、幂等删除、资源账本、基础 IPC desired-state 流程已实现。
- mock Engine 使用 `media_mock`，不再因构建 `PLATFORM_TARGET=mock` 强制编译 ZLMediaKit；macOS Engine 仍使用 `media_zlm`。

### 当前明确未完成

1. `engine/tests/stub_server/main.go` 目前只是 Unix socket listener，不是真正的 gRPC server。
2. Engine 没有可执行的 `make -C engine e2e` 流程，真实 `app.sock` 对账、告警 ACK、图片元数据上报尚未闭环。
3. 尚未完成真实 RTSP/ZLMediaKit + VideoToolbox 60 秒持续流、断流、track 替换和真实帧输出验证。
4. 插件在 `instance_process` 内永久阻塞时，当前 worker join 没有严格上限；不能通过 detached thread 或不安全强杀线程解决。
5. sanitizer 下的 gRPC/Protobuf IPC 覆盖仍受当前 Homebrew Protobuf/gRPC 运行时问题限制。
6. 坏包 fixture、完整 validator 错误矩阵、部署 Profile/launchd 文件和生产目录权限检查尚不完整。
7. `CapabilityStatus` C++ 枚举使用 `UNAVAILABLE=3`，proto 使用 `UNSUPPORTED=3`；当前数值相同但语义名称不同，需要显式冻结和测试。
8. manager、scheduler、telemetry、image-status、profile、配置事务和升级失败回滚的 focused tests 仍不足。

## 2. 优先级定义

| 优先级 | 含义 | 完成要求 |
| --- | --- | --- |
| P0 | 阻断 T1 功能验收或存在生产级生命周期风险 | 未完成前不能声称 T1 端到端交付 |
| P1 | 生产试运行前必须补齐的契约、安全和可观测性 | 必须有确定性测试和失败证据 |
| P2 | 不阻断当前骨架，但影响长期稳定性、性能或维护成本 | 在 T1 关闭后排期 |
| P3 | 后续平台或产品阶段工作 | 不应混入本任务的收尾提交 |

## 3. P0：必须优先完成

### P0-1：实现真实 Go gRPC Stub 与 Engine E2E 流程

**当前问题**

- `engine/tests/stub_server/main.go` 只调用 `net.Listen("unix", ...)`，没有注册 gRPC service。
- 没有生成 Go protobuf/gRPC 类型，也没有 `make -C engine e2e` target。
- 因此 AC6（告警、图片和 ACK）与 AC13（app.sock 断线重连后全量对账）无法验证。

**需要实现的功能**

1. 从 `engine/proto/aivision/v1/*.proto` 生成 Go protobuf 和 gRPC 代码。
2. 在独立 `engine/tests/stub_server/` 中实现：
   - `ControlPlaneService.GetDesiredState`：按 revision 返回内存中的 DesiredState；
   - `ReportService.ReportAlarm`：记录告警、事件 ID、alarm type、bbox、图片元数据和 ACK；
   - `ReportTaskState`、`ReportInstanceState`、`ReportMetrics`、`ReportOrphanImages`：记录最近状态并提供测试等待条件；
   - SIGTERM 后清理 socket，重新启动后保留由测试 harness 注入的期望状态。
3. 增加确定性测试控制接口或文件：
   - 注入 DesiredState；
   - 等待某个 `event_id`、`image_id` 或 revision；
   - 模拟服务不可用、重启和延迟 ACK。
4. 增加 `engine/Makefile` 的 `e2e` target：
   - 构建 `package_validator`、Engine、mock fixture 和 stub；
   - 启动 stub 的 `app.sock`；
   - 启动 Engine 的 `engine.sock`；
   - 通过 Engine RPC 安装包、下发 task/instance、读取结果；
   - 终止并重启 stub，确认 Engine 重新拉取 DesiredState；
   - 使用 trap 清理进程组、socket、临时目录和测试包。
5. 所有断言使用稳定 `code`/字段，不解析 `error_message` 文本。

**建议文件**

- `engine/tests/stub_server/gen/`
- `engine/tests/stub_server/server.go`
- `engine/tests/stub_server/state.go`
- `engine/tests/e2e/` 或 `engine/tests/e2e.sh`
- `engine/Makefile`

**验证标准**

- `make -C engine e2e` 在无 `app/` 产品代码的情况下通过。
- 断言告警的 `event_id`、`alarm_type_id`、算法信息、归一化 bbox、`time_synced`、图片 ID/相对路径。
- 重复结果不会产生第二张图片。
- stub 被杀掉后重新拉起，Engine 能通过 `GetDesiredState` 恢复期望状态。
- Engine 和 stub 正常退出时不留下 socket 或子进程。

**前置条件**

- 固定 Go `protoc-gen-go` / `protoc-gen-go-grpc` 版本。
- 处理 proto 的 `go_package` 与独立 `stub_server` module 的映射；不能修改 `app/` 产品代码。

### P0-2：完成真实 RTSP、ZLMediaKit 和 VideoToolbox 集成验证

**当前问题**

当前的媒体测试主要是 `DummyMediaSource` 和 mock decoder。它们能验证状态机和 NAL gate，但不能证明 ZLMediaKit 回调、真实 access unit、VideoToolbox 输出和 macOS 原生帧生命周期正确。

**需要实现的功能**

1. 提供仓库内可复现的本地媒体 fixture：
   - 固定的 H.264 1080p 回放流；
   - 至少一条 H.265 流或 H.265 access-unit fixture；
   - BT.709 VUI、无 VUI、前导 P/B、损坏 access unit；
   - 可控制断流、静默、track 替换和恢复。
2. 提供启动/停止本地 RTSP 回放源的脚本，禁止依赖公共互联网摄像头。
3. 对 `media_zlm` 验证：
   - 一个摄像头只有一个 PlayerProxy/上游连接；
   - 回调只复制编码帧并入有界队列；
   - stop、disconnect、track replacement 后 delegate、source、线程均被释放。
4. 对 VideoToolbox 验证：
   - H.264/H.265 解码输出 `CVPixelBuffer`；
   - PTS、wall clock、`time_synced` 和 color VUI 正确传入 `av_frame_desc`；
   - decoder reset 后旧 opaque/frame token 不再被使用；
   - NV12/BGRA、BT.601/BT.709/BT.2020 和 limited/full range 转换可用已知色块断言。
5. 运行多实例压力场景：
   - 同一 camera 挂两个不同 FPS 的算法实例；
   - 一个实例人为阻塞，另一个实例继续消费；
   - 记录 decoded FPS、每实例 sampled FPS、丢帧数、重连次数和 watchdog reset 次数。
6. 重复执行启动、断流、恢复、track 替换、stop、join、析构，形成固定日志和退出码。

**建议文件**

- `engine/tests/fixtures/media/`
- `engine/tests/media/rtsp_replay.sh`
- `engine/tests/e2e/real_media_*`
- `engine/docs/media-validation.md`

**验证标准**

- 本地 RTSP 运行至少 60 秒，无崩溃、死锁、帧池泄漏或 delegate 泄漏。
- AC4、AC5、AC12、AC22 的真实媒体部分有日志和可复查测试产物。
- 失败时能区分 `media_connect_failed`、`decoder_stall`、`track_replaced` 和 `frame_release_failed`。

**前置条件**

- Apple Silicon macOS、VideoToolbox、已固定的 ZLMediaKit commit/config。
- 可被测试脚本控制的本地 RTSP 源；不使用不稳定的公网摄像头。

### P0-3：设计并实现永久阻塞插件的有界停止

**当前问题**

当前 `AlgorithmInstance::stop()` 最终需要等待 worker 退出。若第三方插件在 `instance_process` 内永久阻塞，线程无法安全取消；detached thread 会让 ABI 动态库、实例句柄和 frame token 生命周期失控。

**必须先完成的设计决策**

1. 是否接受插件运行时进程隔离。推荐使用独立 plugin worker process，而不是依赖 C++ 线程强杀。
2. 隔离粒度：每个 instance、每个 package version，还是一个可重启 supervisor 下的多个 instance。
3. 帧传输方式：
   - macOS 原生 opaque 句柄不能直接假设跨进程有效；
   - 优先评估 shared memory + 纯值 `av_frame_desc`；
   - 对 `CVPixelBuffer`/IOSurface 句柄定义跨进程句柄协议，或在隔离边界做受控 NV12 copy。
4. 插件崩溃、超时、升级和资源回收时，哪些状态可以恢复，哪些必须标记为 `ERROR`。

**需要实现的功能**

- Engine supervisor：启动、握手、能力协商、心跳、结果回传和有界 shutdown。
- 共享内存或受控 IPC 的帧描述、frame token、引用计数和 backpressure。
- 超时后终止整个 plugin process group，等待退出，清理临时资源，不能遗留动态库句柄或子进程。
- 只重启受影响的 instance/package，不影响同 camera 的其他实例。
- 保留 `AV_ERR_RETRY = -10`、`av_algo_get_abi`、现有 gRPC 字段和 ABI 结构布局；不要为了取消调用偷偷修改已有 C ABI。
- blocking、crash、超时、重复重启和升级中的进程清理测试。

**验收标准**

- 人为阻塞的插件在确定 deadline 内停止，Engine 进程不会挂起。
- 不使用 detached thread、未定义行为的强制线程取消或持有已卸载 dylib 的回调。
- frame pool、资源账本、package/library 引用在 worker 被杀后归还。

**前置条件**

- 需要单独的架构评审；这是对当前 ABI 与“视频帧不经过 Go 进程”约束的扩展，不应直接在现有线程模型上打补丁。

## 4. P1：生产试运行前补齐

### P1-1：冻结能力枚举和 Profile 契约

**当前问题**

`engine/include/aivision/platform/platform_api.hpp` 使用：

```text
UNSPECIFIED=0, AVAILABLE=1, DEGRADED=2, UNAVAILABLE=3
```

`engine/proto/aivision/v1/engine.proto` 使用：

```text
UNSPECIFIED=0, AVAILABLE=1, DEGRADED=2, UNSUPPORTED=3
```

当前数值兼容，但语义名称不一致，后续 SDK/Go 代码容易把 unavailable、unsupported、degraded 混为一类。

**实现步骤**

1. 决定权威词：建议内部和 proto 都使用 `UNSUPPORTED`；如果保留 C++ `UNAVAILABLE`，必须在转换函数旁明确说明它对应 proto `UNSUPPORTED`。
2. 增加编译期和运行期断言，锁定 0/1/2/3 不能漂移。
3. 为 `QueryProfile` 增加 contract tests：
   - platform adapter 缺失；
   - capability available/degraded/unsupported；
   - unsupported/degraded 时 reason 非空；
   - `media_backend` 与注入的 backend 一致；
   - profile 中的 resource/capability/frame caps 与 adapter 一致。
4. 把部署 Profile 中的能力、媒体后端版本和 QueryProfile 输出做一致性检查。

**当前已修复**

- `QueryProfile` 已不再固定返回 `ZLMediaKit`，会读取注入 backend 的名称；mock 返回 `mock`，macOS 返回 `ZLMediaKit`。

### P1-2：补齐 validator 坏包和错误矩阵

**当前问题**

正常 mock/yolov8n 包已验证，但完整坏包 fixture 矩阵仍不完整，无法证明每个拒绝路径都清理 staging 并保护旧版本。

**需要的 fixture**

- 动态库额外导出符号；
- 动态库无 `av_algo_get_abi`；
- ABI 缺函数指针或错误版本/size；
- `library_query` 与 manifest ID/version/type 不一致；
- 文件 SHA-256 错误、缺入口文件、config schema 缺失、test image 缺失；
- self-test 无回调、多回调、错误 kind、非法 JSON、超时、崩溃；
- ZIP 路径穿越、绝对路径、反斜杠、重复路径、case collision、symlink/hardlink、FIFO/device、zip bomb；
- 安装过程中 rename/fsync 失败；
- 加载新版本失败、升级重启失败、旧版本回滚失败。

**实现步骤**

1. 坏包 fixture 必须通过 CMake/脚本可再生，不能依赖开发者手工复制。
2. 每个 fixture 使用独立 test name，断言稳定 `error_stage`、`code` 和 staging 清理。
3. 在旧版本已运行、有 instance 引用、有 active marker 的场景下验证拒绝后状态不变。
4. 把目录包和 ZIP 包都走同一 validator 进程路径。

**验收标准**

- `ctest -R validator` 覆盖完整矩阵。
- 任意失败不执行 `dlopen` 后的主进程安装，不留下 `.part`、staging 或半成品 active marker。
- 旧版本和已运行实例在坏升级后仍可继续工作。

### P1-3：补齐配置、规则、资源和生命周期事务测试

**当前问题**

基础流程已能运行，但以下组合状态仍缺少 focused tests：

- `params_json` 与 rules 同时热更新时的整份提交/回滚；
- 非法规则导致参数不能先被提交；
- 规则自交、线/区域点数、line direction 等几何语义；
- FPS 没有对应 manifest tier、资源超限、最低内存不足时的回滚；
- instance 删除、camera stop、package upgrade、server stop 的资源和 worker 回收；
- `TaskScheduler`、`AlgoManager`、`TelemetryCollector` 的并发和析构顺序。

**需要实现的功能**

1. 抽出可单测的规则几何验证器，覆盖 bounds、点数、角色、方向和多边形自交。
2. 明确参数与 rules 的提交顺序及失败回滚策略；ABI 没有合并更新函数时，至少保证 Engine 侧结构验证在任何插件调用前完成，并为插件调用失败设计旧配置恢复路径。
3. 对 DesiredState reconcile、UpdateInstanceConfig、Upgrade/Rollback 使用测试夹具验证：成功、失败、重试、stale revision、部分资源分配和旧状态保留。
4. 为 resource ledger 增加 release-on-failure、overflow、memory threshold、FPS upper-tier rejection 的回归测试。
5. 为 image manager 增加 `ALREADY_ABSENT`、`FAILED`、catalog recovery、orphan cleanup、fsync/rename failure 测试。
6. 为 telemetry/profile 增加 unsupported/degraded capability 不伪造为 0 的断言。

### P1-4：恢复 sanitizer 的 gRPC/Protobuf IPC 覆盖

**当前问题**

普通 Debug IPC 测试已通过，但 sanitizer 测试暂时跳过 IPC；当前 Apple Silicon 环境的 Homebrew Protobuf/gRPC 生成代码与 sanitizer runtime 存在崩溃、TSan 符号或 interceptor 加载问题。

**实现步骤**

1. 固定一组已知兼容的 Protobuf/gRPC/compiler 版本，记录到 `engine/docs/toolchain.md`。
2. 先用最小 generated-message + gRPC UDS 程序复现问题，区分依赖问题、生成代码问题和 sanitizer 启动顺序问题。
3. 若不能修复 Homebrew 组合，构建隔离的 protobuf/gRPC toolchain，不要用大范围 suppression 掩盖问题。
4. 恢复 sanitizer 下的 UdsServer/UdsClient、Go stub 对账、package RPC 和 shutdown 压力测试。

**验收标准**

- ASan/UBSan/TSan 覆盖 IPC 线程、generated message、socket reconnect 和 Engine shutdown。
- 测试不会因为 `AIVISION_SKIP_IPC_TESTS` 永久绕过关键链路。

### P1-5：交付 macOS 部署 Profile 和 launchd 运行方式

**当前问题**

Profile 规范已经定义，但以下交付文件和运行时校验尚未完成：

- `deploy/macos/engine-profile.json`；
- `deploy/macos/com.aivision.engine.plist`；
- `deploy/macos/zlm.ini`；
- `engine/docs/deployment-macos.md`；
- runtime/package/image/log 目录创建、权限和 socket 逃逸校验。

**需要实现的功能**

1. Profile JSON parser：schema version、platform ID、adapter/runtime/ZLM 版本、路径、两个 UDS、watchdog、resource limits。
2. 启动前检查：Apple Silicon、最低 macOS、路径在 runtime root 内、目录权限、`allocatable + reserved <= total`、socket 不相同。
3. launchd plist：绝对路径、独立工作目录、KeepAlive、SIGTERM 有序退出、stdout/stderr 路径；禁止写入凭据和 secret。
4. 版本升级流程：验证新二进制/Profile -> 停服 -> 原子切换 -> 启动健康检查 -> 失败回滚。
5. QueryProfile 输出与 Profile/构建报告中的媒体和运行时版本一致。

**验收标准**

- 干净 macOS 临时目录首次启动、重启、SIGTERM、遗留 socket 清理均通过。
- profile 缺字段、路径逃逸、权限过宽、版本不匹配均返回稳定错误。

### P1-6：补齐结构化日志和真实指标验收

**需要确认的功能**

- watchdog、reconnect、decoder reset、frame release failure、validator failure、package rollback 使用稳定事件名和低基数字段；
- 记录 decoded/sampled/dropped/reconnect/reset 计数；
- CPU、memory、disk、accelerator、temperature 不支持时有显式 availability/reason；
- 日志中不出现 RTSP 密码、token、模型密钥和完整本地敏感路径；
- QueryProfile、QueryMetrics 和上报到 app.sock 的字段保持一致。

**验证方式**

- 对固定场景采集 JSON log/metrics snapshot；
- 断言 unsupported 指标不是伪造的 `0`；
- 运行 60 秒媒体压力后检查低基数、计数单调性和错误关联 ID。

## 5. P2：T1 关闭后排期

### P2-1：多实例并发和性能压力

- 16 个算法实例、不同 FPS、不同 queue pressure 下验证 process 不重入。
- 一个实例阻塞时，其他实例的 decoded/sample FPS 不被拖垮。
- 输出 preprocess/inference/postprocess/e2e Avg/P50/P99 和持续 FPS。
- 增加固定压力脚本，避免测试依赖无界真实 sleep。

### P2-2：ABI 跨编译器与兼容性矩阵

- AppleClang、GCC/aarch64 交叉编译 `_Static_assert` 和关键 `offsetof`。
- 用旧 `size` 构造 `av_frame_desc`、`av_algo_result`、image request，验证新代码只读取声明范围内字段。
- 记录 SDK `VERSION`、ABI version、平台 adapter version 的组合矩阵。

### P2-3：算法包开发闭环的长期 CI

- `/tmp` 可搬运构建、`make run` 生成非空 `result.jpg`、`.env` 与环境变量覆盖、benchmark 字段完整性纳入 CI。
- 校验 vendored SDK 与上游 SHA-256，防止多个算法包出现漂移。
- 生成模型转换证据和 `.mlpackage` digest 的可重复检查。

### P2-4：真实升级和回滚压力

- 多 camera、多 instance、多个 package version 并行升级。
- 升级过程中故意触发加载失败、self-test 超时、worker crash、磁盘不足和 active marker 写入失败。
- 验证未引用包可卸载，有引用包拒绝卸载，其他算法实例不被误停。

## 6. 建议实施顺序

```text
A. 契约先收口
   CapabilityStatus + Profile mapping
   规则几何校验 + 配置事务测试
   validator 坏包 fixture

B. 确定性 E2E 基础设施
   Go gRPC stub
   scripted media_mock
   make -C engine e2e
   alarm/image/ACK/reconnect assertions

C. 真实媒体链路
   local RTSP replay
   ZLMediaKit delegate
   VideoToolbox + VUI + frame lifetime
   60s disconnect/track replacement test

D. 插件生命周期安全
   process isolation design review
   shared-memory/frame-handle protocol
   bounded stop/crash/restart tests

E. 生产化交付
   sanitizer IPC
   macOS deployment Profile/launchd
   structured logs/metrics
   upgrade/rollback stress
```

### 阶段准入关系

- B 依赖 A 的稳定 proto、错误码、结果 JSON 和资源/规则校验。
- C 依赖 B 的测试等待、日志采集和故障注入框架。
- D 可以与 B/C 并行做设计，但在设计冻结前不要改 ABI 或引入 detached worker。
- E 依赖 B/C 的真实状态和指标字段；部署 Profile 不能基于 mock 的假值完成。

## 7. 外部条件与资源

| 项目 | 必需条件 |
| --- | --- |
| Go gRPC stub | `protoc-gen-go`、`protoc-gen-go-grpc`，固定版本；不修改 `app/` |
| 真实媒体 | Apple Silicon macOS、固定 ZLMediaKit commit、可控本地 RTSP 回放源、H.264/H.265 fixture |
| VideoToolbox | 系统 VideoToolbox/CoreVideo/Accelerate/ImageIO，允许真实 `CVPixelBuffer` 输出 |
| sanitizer IPC | 与 generated protobuf/gRPC 兼容的工具链，或隔离构建的依赖版本 |
| 部署验证 | launchd、临时 runtime/package/image/log 目录和权限检查能力 |
| 插件隔离 | 进程组、shared memory/IPC、跨进程 frame handle 方案和新的资源回收测试 |

禁止依赖：公网摄像头、开发者手工操作、未固定的当前工作目录、未记录版本的 Homebrew 依赖、永久 detached 线程。

## 8. 完成定义

这份后续清单全部关闭前，T1 只能称为“可构建的 Engine/SDK/macOS 骨架 + mock/包验证闭环”，不能称为已完成真实生产端到端验收。

最终至少需要以下命令和证据：

```bash
make -C engine configure build test lint
make -C engine asan
make -C engine tsan
make -C engine e2e
make -C algo-packages/macos/arm64/yolov8n test package
bash algo-packages/scripts/check-consistency.sh
cd engine/tests/stub_server && go test ./... && go vet ./...
```

以及：

- 真实 RTSP 60 秒日志和断流/恢复/track replacement 结果；
- Go gRPC stub 收到并 ACK 的 Alarm/Image/State/Metrics 记录；
- sanitizer IPC 输出；
- 坏包错误矩阵和 staging 清理结果；
- macOS Profile/launchd 首次启动、重启、升级、回滚和 SIGTERM 结果；
- 永久阻塞插件在 deadline 内停止的证据，或明确的架构决策和延期记录。

## 9. 不应重新打开的已解决问题

除非出现新的回归，不要重复改动以下已验证内容：

- `AV_ERR_RETRY = -10` 及现有 SDK C ABI 字段布局；
- gRPC response 的 `code` / `error_message` 语义；
- `ReconcileImages`、`SyncPersons` 的既定协议决定（后者本任务保持 `UNIMPLEMENTED`）；
- H.264/H.265 参数集和 IDR/IRAP gate；
- FramePool token/reference accounting；
- ImageManager `.part`、fsync、catalog、path safety 和 delete status；
- validator 的 `posix_spawn`、进程组清理、原子安装和唯一导出检查；
- mock/macOS 分离的 ZLM 构建链路；
- 算法包 vendored SDK 一致性和仓库外可搬运构建。
