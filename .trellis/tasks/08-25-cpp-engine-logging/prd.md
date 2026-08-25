# C++ Engine 结构化日志

## Goal

为 C++ Engine 建立可用于开发调试、生产故障分析和跨进程排障的统一日志能力。日志必须具备稳定的机器字段、可信的上下文、可控的异步开销和敏感信息保护；在日志管道异常时不能反向阻塞媒体解码、推理、IPC 或插件回调，也不能因为日志设施故障而终止 Engine。

## Background / Current Evidence

- 当前 Engine 自有代码主要使用 `std::cout`/`std::cerr`，没有统一的日志 API、级别过滤、上下文或敏感信息边界。
- `engine/src/validator/validator_main.cpp` 当前把 validator 成功/失败结果写成普通文本；`engine/src/core/algo/algo_sandbox.cpp` 的 `capture_command()` 将 stdout/stderr 合并捕获，并通过查找 `Error code:` 文本推断结果。这会使机器协议依赖可变日志文本。
- SDK 已存在 `av_log_fn`（`sdk/include/aivision/algo.h`），算法包会通过该回调报告模型、平台和推理诊断；但 SDK 当前没有为 `level` 定义命名常量。
- 现有 Engine 规范已经要求结构化字段、稳定错误码、URL/凭据脱敏、逐帧热路径禁止 INFO，以及指标和 validator 失败的可诊断性：
  - `.trellis/spec/engine/error-observability-guidelines.md`
  - `.trellis/spec/engine/quality-guidelines.md`
  - `.trellis/spec/engine/algo-package-spec.md`
- Engine 已依赖 `nlohmann_json`，但尚未集成统一的 C++ 日志库。

## Scope

### In Scope

日志 v1 覆盖 Engine 自有完整运行链：

- `engine_core` 及其媒体、平台适配、任务调度、IPC、图像、算法宿主代码；
- `aivision-engine` 主进程；
- 独立的 `package_validator` 进程；
- Engine 与算法包之间已有 C ABI `av_log_fn` 的宿主桥接；
- systemd unit 到 journald 的最小接收配置和部署说明；
- Logger 自身的本地统计、确定性测试和 sanitizer 覆盖。

### Out of Scope

- Engine 自己写日志文件、文件轮转、磁盘水位管理或自定义 durable log outbox；
- 直接链接 `libsystemd` 或使用 journald native protocol；
- 修改系统级 journald 全局容量、保留和轮转策略；
- 本次实现 `engine-profile.json` parser、Profile 中的 logging 配置段或运行时热更新；
- 组件级日志阈值、`SIGHUP` 热更新和 gRPC 动态日志控制面；
- Logger 内置 signal-safe crash handler、堆栈生成和崩溃恢复；
- 本次扩展 QueryMetrics/Telemetry Proto；
- 修改 ABI v1 为实例级日志回调；
- 让算法包直接依赖 Engine Logger、spdlog 或 Engine 私有库；
- 把算法包的自由文本当作稳定错误码或控制流协议。

## Confirmed Requirements

### R1. 输出与部署

1. 业务日志由 Engine 生成，统一以一行一条 JSONL 写入 `stderr`。
2. systemd/journald 负责接收、持久化、轮转和保留；Engine 不负责文件落盘和轮转。
3. v1 不直接依赖 `libsystemd`，JSON 中的业务 `level` 是跨平台权威级别；不能依赖 journald 对统一 stderr 流设置的原生优先级完成业务过滤。
4. 交付最小 systemd 集成配置和说明，至少明确：
   - `StandardError=journal`；
   - Engine 的服务标识；
   - 主进程与 validator 子进程 stderr 的继承关系；
   - 使用 `journalctl` 读取 JSON 日志的方式。
5. 不修改 journald 全局配置。正常 Engine 日志不同时写第二个文件或第二个 stderr sink。

### R2. 进程边界与 validator 协议

1. `package_validator` 自己初始化同一套 Logger。
2. validator 的 `stdout` 只允许输出一个有限大小的机器结果 JSON 对象并以换行结束。结果至少包含：
   - `success`；
   - `error_code`；
   - `error_stage`；
   - `error_message`（仅诊断用途）；
   - 成功时的 manifest 摘要和 `package_sha256`。
3. Engine 只读取和解析 validator stdout 的稳定字段，控制流只依赖 `success` 和 `error_code`，不得解析日志文本。
4. 空 stdout、多个 JSON 对象、非 JSON、尾随非空内容、超出上限或缺少必要字段，统一映射为稳定的 `VALIDATOR_RESULT_INVALID` 诊断结果。
5. validator 的完整结构化日志写入 stderr，并继承 Engine 的 stderr；Engine 不读取、不解析、不重复转发 validator stderr。
6. validator 日志和 Engine 日志可以共享同一个稳定 `code`，例如 `PACKAGE_CHECKSUM_MISMATCH`，用于跨进程关联。

### R3. 异步写入与实时性

1. Logger 使用固定容量的异步队列和单独 writer 线程。
2. 业务线程只负责级别判断、构造记录、复制上下文和非阻塞入队；日志序列化、sink 写入和 journald 管道 I/O 不得在媒体热路径同步执行。
3. 使用两个固定容量队列：
   - 普通队列：`2048` 条，承载 `DEBUG`/`INFO`；
   - 高级别队列：`256` 条，承载 `WARN`/`ERROR`/`FATAL`。
4. 两个队列均不得因满而阻塞生产线程。高级别队列拥有独立保留空间，writer 优先处理高级别记录，但必须采用公平调度避免普通队列永久饥饿。
5. 普通队列满时允许丢弃低级别记录；高级别队列满时也允许非阻塞丢弃，不能承诺绝对不丢日志。
6. Logger 维护原子统计：普通/高级别丢弃数、队列深度、writer 写入失败数、截断数、拒绝字段数、未知算法级别数和已写记录数。
7. 丢弃和 writer 故障通过限频摘要留下可观测证据；摘要不能递归进入已经故障或已满的普通 Logger 路径。
8. Logger 关闭时停止接收新记录，优先排空高级别队列，最多等待 `2s`；超时丢弃剩余记录并完成关闭，不得无限等待。
9. stderr/journald 写入失败时 fail-open：统计失败并限频重试，不阻塞或终止 Engine；writer 不调用同一个 Logger 记录自身故障。

### R4. Logger 生命周期与依赖

1. Logger 使用显式 `initialize()`/`shutdown()`，禁止懒加载和全局构造阶段写日志。
2. 初始化前允许使用最小同步 stderr fallback，以保留启动阶段故障；初始化成功后切换到异步 sink。
3. 主进程和 validator 入口都必须显式初始化 Logger。
4. `AIVISION_LOG_LEVEL` 是 v1 唯一外部日志配置：默认 `INFO`，只在启动时读取，修改后重启生效。
5. v1 不开放队列容量、writer 参数或组件阈值环境变量；队列容量使用编译固定值。
6. 非法日志级别回退到 `INFO`，并通过早期 stderr 输出一次安全警告；不因日志配置错误阻止 Engine 启动。
7. 公开 Logger API 全部 `noexcept`。初始化失败通过初始化结果报告；运行时序列化、分配或 sink 异常只计数并丢弃，不传播到业务、C ABI、媒体回调或 shutdown 路径。
8. `fatal` 只是不可恢复级别，不自动 `abort()`、`exit()` 或抛异常；只有拥有生命周期的上层决定进程控制流。
9. 非受控崩溃的堆栈由 systemd-coredump/系统 core dump 负责，Logger v1 不实现 signal-safe crash handler。

### R5. 日志级别与过滤

1. Engine 统一使用五级业务级别：`debug`、`info`、`warn`、`error`、`fatal`。
2. 使用单一全局阈值，不做组件级配置。默认阈值为 `info`。
3. `ERROR`/`FATAL` 日志必须携带稳定 `code`；具有明确错误语义的 `WARN` 应携带 `code`；普通 `INFO`/`DEBUG` 不强制伪造错误码。
4. 逐帧解码、抽帧、推理和媒体回调热路径禁止 `INFO`。该约束由业务模块、代码审查和固定压力测试落实，Logger 不使用运行时热路径标记增加开销。
5. 业务模块负责状态转移的语义去重和限频，Logger 不维护全局高基数 `(event, camera_id, instance_id)` 去重表。VUI 缺失等状态按 camera/source 生命周期只记录一次，后续通过指标体现。

### R6. 结构化记录格式

1. 每条记录是合法 JSON object，并且只输出一行。
2. 必填字段：
   - `ts`：记录创建时生成的 UTC RFC 3339 纳秒时间戳；
   - `level`：小写 `debug`/`info`/`warn`/`error`/`fatal`；
   - `component`：稳定组件名；
   - `event`：稳定事件名；
   - `message`：非敏感的可读摘要。
3. `event` 使用小写点号分层命名，例如 `engine.started`、`decoder.recreated`、`package.validation_failed`、`ipc.reconnect_failed`；不能包含 ID、URL、错误原文或其他动态值。
4. 可选 `code` 和上下文字段遵循 Engine 规范：
   - `platform_id`、`device_id`、`camera_id`、`task_id`；
   - `instance_id`、`instance_run_id`；
   - `algorithm_id`、`package_version`；
   - `frame_id`、`revision`、`retry_count`、`duration_ms`。
5. 所有结构化值只能是受控 JSON 标量：字符串、布尔值、有界整数和有限范围浮点数；禁止任意数组、对象、二进制和原始 JSON payload。
6. 每条记录在业务线程创建时分配当前进程内单调 `seq`。高级别记录可以抢先写出，读取方使用 `seq` 恢复创建顺序；`seq` 不替代业务 `event_id`，也不跨进程保证唯一。
7. `DEBUG`/`ERROR`/`FATAL` 记录可携带 C++20 `std::source_location` 的规范化文件名、行号和函数名；不记录完整绝对构建路径。默认 `INFO` 不增加源码位置负担。
8. 记录创建时复制 `LogContext` 快照；异步 writer 不读取生产线程的 thread-local。上下文可按作用域嵌套，worker 线程复用时必须显式设置或清理；`frame_id` 等高基数字段不能自动附着到所有日志。
9. `duration_ms` 等耗时由 monotonic clock 计算；wall clock 只用于 `ts` 和跨进程关联。

### R7. 大小、安全和字段边界

1. 单条 JSON 记录上限为 `16 KiB`；`message` 上限为 `8 KiB`；单个字符串字段上限为 `1 KiB`。
2. 超长自由文本安全截断，并保留稳定字段；记录 `message_truncated=true` 和原始字节数。`event`、`code`、上下文 ID 等稳定字段不得被自由文本截断策略覆盖。
3. 截断后仍无法形成合法记录时，按级别进入对应丢弃统计；不允许生成半截 JSON。
4. 字段名必须经过白名单校验。密码、token、gRPC metadata、完整配置 JSON、人脸/图片字节和未经处理的内部敏感路径禁止写入。
5. 未知或不安全字段被丢弃，保留其余日志记录并递增 `rejected_fields`；拒绝摘要不得回显原始值。
6. URL 只能记录去除 userinfo 和 query 后的安全 endpoint。
7. 通过 `av_log_fn` 进入的算法自由文本视为不可信输入，必须经过控制字符/编码、凭据和 URL、路径、长度等统一清洗后才进入 `message`；无法安全处理时使用固定摘要并统计，不回显原文。

### R8. C++ API 与实现边界

1. 业务代码只依赖项目自有 `aivision::logging` 类型化 Facade，不直接依赖 spdlog、`printf` 或散落日志宏。
2. Facade 至少提供：显式初始化/关闭、五级便捷方法、受控字段构造、作用域 `LogContext`、本地 `LoggerStats` 查询和初始化时注入的 sink。
3. 初始化后 sink 不可替换；测试使用线程安全内存 sink，生产使用 stderr sink。运行期不允许全局替换 sink。
4. Logger 先构造不可变 `LogRecord`，完成过滤、字段校验、上下文复制、脱敏和大小限制后再入队。
5. 使用固定版本、仓库内 vendored 的 spdlog 作为底层依赖；业务代码不暴露 spdlog 类型。已有 `nlohmann_json` 用于 JSON 序列化和正确转义。
6. `av_algo_log_level` 在 SDK 侧冻结为兼容现有算法调用的数值：
   - `0=trace`；
   - `1=debug`；
   - `2=info`；
   - `3=warn`；
   - `4=error`；
   - `5=fatal`。
   Engine 将 `trace` 归入 `debug`；未知数值降级为 `warn`，生成 `algorithm.log_level_unknown` 诊断事件并计数。
7. Engine 和 validator 都桥接已有 `av_log_fn`，但算法包不依赖 Engine Logger。算法库级回调附加可靠的 `algorithm_id`、`package_version`、`platform_id` 等库级字段。
8. 由于 ABI v1 的 `av_log_fn` 只位于 `av_algo_library_args`，没有实例级 `log_user`，算法自由文本日志 v1 不伪造 `instance_id`/`instance_run_id`；Engine 对实例 process 失败、超时、停止等宿主事件单独记录实例上下文。
9. C ABI 状态码通过 Engine 显式映射表转换为稳定 `code`，不把 ABI 数字直接暴露给日志控制流。至少覆盖：
   - `AV_ERR_UNSUPPORTED_API` → `ALGO_ABI_INCOMPATIBLE`；
   - `AV_ERR_INVALID_ARG`/`AV_ERR_CONFIG_INVALID` → `CONFIG_INVALID`；
   - `AV_ERR_INCOMPATIBLE_FRAME` → `FRAME_CAPS_INCOMPATIBLE`；
   - `AV_ERR_MODEL_LOAD_FAILED` → `ALGO_MODEL_LOAD_FAILED`；
   - `AV_ERR_INFERENCE_FAILED` → `ALGO_PROCESS_FAILED`；
   - `AV_ERR_OUT_OF_MEMORY` → `MEMORY_LIMIT_EXCEEDED`；
   - `AV_ERR_NOT_IMPLEMENTED` → `ALGO_NOT_IMPLEMENTED`；
   - `AV_ERR_TIMEOUT` → `ALGO_PROCESS_TIMEOUT`；
   - `AV_ERR_INTERNAL` 或未知状态 → `INTERNAL_ERROR`。
10. 插件 `last_error` 和 `av_log_fn` 原始文本只用于安全处理后的诊断 message；控制流只依赖稳定状态码/`code`。

### R9. 本地 LoggerStats

v1 不修改 QueryMetrics/Telemetry Proto，但 Logger 必须提供线程安全的本地统计，至少包括：

- 普通队列和高级别队列当前深度；
- 普通/高级别入队丢弃数；
- writer 写入失败数；
- message/字段截断数；
- 被拒绝字段数；
- 未知算法日志级别数；
- 已写记录数。

统计可用于单测、自诊断和未来指标接入，不把 `frame_id`、`event_id` 等高基数字段作为 label。

## Acceptance Criteria

- [ ] `engine_core`、`aivision-engine` 和 `package_validator` 通过同一项目 Facade 输出一行一条合法 JSONL；业务代码不直接使用 spdlog/printf 日志。
- [ ] 默认 `AIVISION_LOG_LEVEL` 为 `INFO`；`DEBUG`/`INFO`/`WARN`/`ERROR`/`FATAL` 过滤行为和非法值回退行为有确定性测试。
- [ ] 每条记录具有必填 `ts`、`level`、`component`、`event`、`message`；事件名称、错误码、上下文、`seq` 和时间/耗时语义符合本 PRD。
- [ ] JSON 字符串转义、UTF-8/控制字符处理、字段白名单、URL/凭据/路径脱敏、message/record 大小上限和安全截断均有测试；测试输出不得包含密码、token、完整配置、图片/人脸字节或未经处理的敏感路径。
- [ ] `LogContext` 在异步入队前完成快照；并发线程和 worker 复用测试证明 camera/task/instance 上下文不会串线。
- [ ] 普通队列 `2048`、高级别队列 `256` 均为固定有界；高低级别入队在队列满时均不阻塞，丢弃计数正确，高级别优先且不会让普通队列永久饥饿。
- [ ] `seq` 创建顺序、两个队列可能产生的写出顺序差异、sink 写入失败、writer 异常、初始化前 fallback 和 shutdown `2s` 排空行为均有测试。
- [ ] Logger 的公开方法不向业务传播异常；`fatal` 不自动退出；崩溃路径不调用异步 Logger。
- [ ] 算法包 `av_log_fn` 在 Engine 和 validator 中可见，级别 `0..5` 映射符合契约，未知级别有安全降级和统计；算法自由文本经过安全处理。
- [ ] Engine 的 C ABI 状态码映射表有覆盖测试，稳定 `code` 与日志 message 解耦。
- [ ] validator stdout 只产生一个有限大小的机器结果 JSON；Engine 不再合并 stdout/stderr 或解析 `Error code:` 文本；异常 stdout 映射为 `VALIDATOR_RESULT_INVALID`。
- [ ] validator stderr 继承 Engine stderr，包含结构化 validator 日志；测试能单独验证结果协议和日志流，且不依赖日志文本控制安装结果。
- [ ] systemd unit/部署说明明确 `StandardError=journal`、服务标识、validator stderr 继承关系和 `journalctl` 查询方式；不修改 journald 全局配置。
- [ ] `make -C engine test`、`make -C engine asan`、`make -C engine tsan` 通过；测试覆盖异步队列、上下文、validator 通道、插件回调和 shutdown。
- [ ] 在 RK3576 或等价 systemd Linux 环境完成部署 smoke：Engine/validator 日志进入 journald，`journalctl -o json` 能读取 JSON message，Engine 不因 journald 写入故障退出。

## Risks / Deferred Decisions

1. **ABI v1 的实例上下文缺口**：算法库回调没有实例级用户指针。v1 只记录库级算法上下文，避免伪造关联；若产品要求每条算法日志都关联实例，必须单独设计 ABI v2，不在本任务中偷偷扩展固定 ABI v1。
2. **systemd 原生优先级**：当前输出选择 stderr JSONL，业务级别以 JSON `level` 为准。若未来需要使用 journald 原生字段过滤，应另行评估 `sd_journal_send` 或带优先级协议的兼容方案。
3. **日志可靠性边界**：异步内存队列和 journald 管道只提供尽力诊断，不承诺进程崩溃、断电或队列溢出时零丢失。若产品要求零丢失，需另立 durable outbox 需求。
4. **固定 spdlog 版本、CMake target、许可证文件、队列公平调度批量大小和 validator stdout 的具体字节上限**属于后续 `design.md` 的技术冻结项；PRD 只冻结行为和安全上限。
5. **Profile 集成**：本次只读 `AIVISION_LOG_LEVEL`。未来 Profile parser 完成后，可以把 Profile 的 logging 配置转换为同一个 `LoggerConfig`，但不能形成第二套业务 API。

## Out of Scope Follow-ups

- ABI v2 实例级算法日志上下文；
- 日志级别动态控制、组件级过滤和远程诊断控制面；
- Logger 指标接入 QueryMetrics/Telemetry Proto；
- crash handler/backtrace、core dump 上传和符号服务；
- journald 全局保留/轮转策略和集中式日志导出；
- Engine 日志持久化文件、日志加密、离线 durable outbox。

## Review Status

- 需求状态：已完成多轮需求澄清，等待用户审核本 PRD。
- 当前阶段：Trellis Phase 1 planning；尚未创建 `design.md`/`implement.md`，尚未执行 `task.py start`，尚未修改产品代码。
- 下一步：用户审核并明确批准或修改本 PRD 后，再进入技术设计与实现规划。
