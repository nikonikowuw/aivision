# 错误处理、日志与可观测性规范 (Error, Logging & Observability Spec)

> 本规范定义 Engine 内部错误、C ABI 状态码、结构化日志、跨进程契约、指标与排障标准，避免各个模块发明自己的错误文本和日志格式。

---

## 1. Scope / Trigger

凡涉及以下改动时，必须严格遵守本规范：
- 新增或修改 Engine 内部错误分支、RPC 状态返回与异常捕获；
- 新增或修改 Engine 内部及各模块的日志输出（`LOG_DEBUG` / `LOG_INFO` / `LOG_WARN` / `LOG_ERROR` / `LOG_FATAL`）；
- 跨进程（如 `package_validator` 与 Engine、Engine 与 Go App）的数据通信与状态交互；
- C ABI（`sdk/include/aivision/algo.h`）状态码映射与第三方算法插件不可信输出（`last_error` / `av_log_fn`）的接入；
- 生产环境 systemd / journald 日志采集配置及指标（Metrics）打点。

---

## 2. Signatures

### 2.1 Engine 内部统一错误对象
```cpp
enum class ErrorDomain {
    Config, Media, Decode, Frame, Algorithm, Package,
    Image, Resource, Ipc, Platform, Internal
};

struct EngineError {
    ErrorDomain domain;
    std::string code;        // 稳定机器码，如 PACKAGE_CHECKSUM_MISMATCH
    std::string message;     // 可读说明（不含敏感信息）
    bool retryable;          // 是否允许上层重试
    std::map<std::string, std::string> details; // 受控上下文字段
};
```
内部业务接口统一返回 `Result<T, EngineError>` 或等价明确类型，禁止使用裸空指针或靠日志文本推断错误。

### 2.2 结构化日志核心记录与 Facade 签名
```cpp
namespace aivision::logging {

enum class Level : uint8_t {
    Debug = 0,
    Info  = 1,
    Warn  = 2,
    Error = 3,
    Fatal = 4
};

struct LogContextSnapshot {
    std::string platform_id;
    std::string device_id;
    std::string camera_id;
    std::string task_id;
    std::string instance_id;
    std::string instance_run_id;
    std::string algorithm_id;
    std::string package_version;
    int64_t frame_id{-1};
    int64_t revision{-1};
    int32_t retry_count{-1};
    double duration_ms{-1.0};
};

class Logger {
public:
    static void initialize(Level min_level = Level::Info, std::shared_ptr<LogSink> sink = nullptr) noexcept;
    static void shutdown() noexcept;
    static void set_level(Level lvl) noexcept;
    static Level get_level() noexcept;
    static LoggerStatsSnapshot stats() noexcept;

    static void log(Level lvl,
                    std::string_view component,
                    std::string_view event,
                    std::string_view message,
                    std::string_view code = "",
                    const std::map<std::string, std::string>& extra_fields = {},
                    const SourceLocation& loc = {}) noexcept;
};

} // namespace aivision::logging

// 常用宏（自动携带 __FILE__, __LINE__, __func__）
#define LOG_DEBUG(comp, evt, msg, ...) ...
#define LOG_INFO(comp, evt, msg, ...) ...
#define LOG_WARN(comp, evt, msg, ...) ...
#define LOG_ERROR(comp, evt, msg, ...) ...
#define LOG_FATAL(comp, evt, msg, ...) ...
```

---

## 3. Contracts

### 3.1 稳定全局错误码 (Error Codes)
全局统一以下稳定机器码（一经发布不得修改语义）：
```text
CONFIG_INVALID / CONFIG_SCHEMA_INVALID / STALE_REVISION
MEDIA_CONNECT_FAILED / MEDIA_INGEST_TIMEOUT
DECODER_STALLED / DECODER_RECREATE_FAILED
FRAME_CAPS_INCOMPATIBLE / FRAME_TOKEN_INVALID
ALGO_ABI_INCOMPATIBLE / ALGO_RESULT_INVALID / ALGO_PROCESS_TIMEOUT
ALGO_MODEL_LOAD_FAILED / ALGO_PROCESS_FAILED / ALGO_NOT_IMPLEMENTED
PACKAGE_MANIFEST_INVALID / PACKAGE_CHECKSUM_MISMATCH
PACKAGE_INCOMPATIBLE / PACKAGE_IN_USE / VALIDATOR_CRASHED / VALIDATOR_RESULT_INVALID
IMAGE_WRITE_FAILED / IMAGE_PATH_INVALID
RESOURCE_LIMIT_EXCEEDED / MEMORY_LIMIT_EXCEEDED
IPC_UNAVAILABLE / INTERNAL_ERROR
```

### 3.2 结构化日志行格式 (JSONL Schema)
所有业务日志统一以**单行合法 JSON** 写入 `stderr`，由宿主操作系统的 `systemd` / `journald` 负责轮转与防爆盘。

每条日志的标准 JSON 字段定义：
```json
{
  "seq": 1042,
  "ts": "2026-08-25T10:00:00.123456789Z",
  "level": "info",
  "component": "engine.algo_host",
  "event": "algo.process_failed",
  "code": "ALGO_PROCESS_FAILED",
  "message": "NPU core busy",
  "message_truncated": false,
  "raw_message_bytes": 13,
  "camera_id": "cam_east_01",
  "task_id": "task_1001",
  "instance_id": "inst_yolo_01",
  "instance_run_id": "01J6A1B2C3...",
  "algorithm_id": "yolov8n",
  "package_version": "1.0.4",
  "frame_id": 14205,
  "duration_ms": 18.250,
  "file": "engine/src/core/algo/algo_instance.cpp",
  "line": 142,
  "function": "process_frame"
}
```

- **必填字段**：`seq`、`ts`（UTC RFC 3339 纳秒）、`level`（五级小写）、`component`、`event`（小写点号分层）、`message`。
- **选填字段**：`code`、`camera_id`、`task_id`、`instance_id`、`instance_run_id`、`algorithm_id`、`package_version`、`frame_id`、`revision`、`retry_count`、`duration_ms`、`file`、`line`、`function`。
- **受控标量**：禁止在日志中直接打印嵌套的大 JSON 对象或二进制 payload。

### 3.3 异步双队列与非阻塞原则 (Queue & Reliability Contract)
1. **双有界队列隔离**：
   - **Normal Queue**：容量固定为 `2048`，承载 `DEBUG` 与 `INFO`；
   - **High Queue**：容量固定为 `256`，承载 `WARN`、`ERROR`、`FATAL`。
2. **零阻塞原则（Fail-Open）**：入队操作必须是**非阻塞的（Non-blocking Try Push）**。当队列满或下游 I/O 拥塞时，立即丢弃日志并递增 `dropped_normal` 或 `dropped_high` 原子计数器，**绝对不允许反向阻塞音视频解码、推理与 IPC 业务线程**。
3. **公平调度与防饥饿**：独立后台 Writer 线程优先消费 High Queue，但处理 Normal Queue 时每次最多批量处理 64 条即重新轮询 High Queue，避免普通日志饥饿或高级别告警积压。
4. **优雅停机**：Logger 关闭时最多等待 `2000ms` 排空在途日志，超时即强制退出，防止进程挂死。

### 3.4 字段安全白名单与脱敏清洗规则 (Security & Sanitization)
1. **大小硬限制**：单条 JSONL 上限 `16 KiB`；`message` 上限 `8 KiB`；单个标量字段上限 `1 KiB`。超长自动截断并标记 `message_truncated: true`。
2. **URL 凭据脱敏**：任何 RTSP / HTTP URL 必须剔除 `user:pass` 与 `?query` 参数（如 `rtsp://admin:123456@192.168.1.1:554/live?token=xxx` → `rtsp://192.168.1.1:554/live`）。
3. **敏感词拦截**：包含 `password`、`token`、`secret`、`authorization`、`credential` 的键值禁止写入日志。
4. **算法自由文本清洗**：通过 `av_log_fn` 和 `last_error` 传入的外部字符串视为不可信输入，强制过滤控制字符并校验 UTF-8 编码。

### 3.5 Validator 进程与跨进程机器契约 (Process Protocol)
1. **彻底分离机器输出与调试日志**：
   - `package_validator` 的 **`stdout` 严格为机器协议**：只输出单行紧凑 JSON 结果（`{success, error_code, error_stage, error_message, manifest, package_sha256}`），严禁混入任何文本日志。
   - `package_validator` 的 **`stderr` 为可观测性日志**：初始化 Logger 输出结构化 JSONL，直通宿主/journald。
2. **父进程解析原则**：
   - Engine 捕获子进程输出时，只读取并反序列化 `stdout` 的 JSON 结构体；控制流严格依据 `success` 与 `error_code`，**严禁使用字符串搜索（如 `find("Error code: ")`）来判断结果**。

### 3.6 C ABI 状态码到 Engine 稳定 `code` 映射表
| C ABI 返回状态 (`av_algo_status`) | Engine 稳定 `code` | 业务与日志行为 |
| :--- | :--- | :--- |
| `AV_OK` | `""` | 正常成功 |
| `AV_ERR_UNSUPPORTED_API` | `ALGO_ABI_INCOMPATIBLE` | ABI 版本不匹配，拒绝加载 |
| `AV_ERR_INVALID_ARG` / `AV_ERR_CONFIG_INVALID` | `CONFIG_INVALID` | 动态参数校验失败 |
| `AV_ERR_INCOMPATIBLE_FRAME` | `FRAME_CAPS_INCOMPATIBLE` | 帧格式/分辨率不匹配，拒绝启动实例 |
| `AV_ERR_MODEL_LOAD_FAILED` | `ALGO_MODEL_LOAD_FAILED` | 权重文件损坏或 NPU 运行时失败 |
| `AV_ERR_INFERENCE_FAILED` | `ALGO_PROCESS_FAILED` | 单帧推理失败，丢弃当前帧并打点 |
| `AV_ERR_OUT_OF_MEMORY` | `MEMORY_LIMIT_EXCEEDED` | 内存超限，熔断释放 |
| `AV_ERR_NOT_IMPLEMENTED` | `ALGO_NOT_IMPLEMENTED` | 接口未实现 |
| `AV_ERR_TIMEOUT` | `ALGO_PROCESS_TIMEOUT` | 算法超时，触发性能告警 |
| `AV_ERR_INTERNAL` / 其他未知值 | `INTERNAL_ERROR` | 兜底内部错误 |

---

## 4. Validation & Error Matrix

| 场景 | 机器码 / 状态 | retryable | 行为与日志规范 |
| --- | --- | --- | --- |
| RTSP 临时断开 | `MEDIA_CONNECT_FAILED` | true | 记录 WARN 日志，按退避策略重连 |
| 逐帧送算单帧失败 | `ALGO_PROCESS_FAILED` | false | 记录 ERROR 日志（含实例上下文），递增指标，丢弃当帧不卡死链路 |
| 逐帧正常推理 | `AV_OK` | - | **禁止打 INFO 日志**（避免刷屏），仅递增 FPS / 处理耗时指标 |
| 算法包校验失败 | `PACKAGE_VALIDATION_FAILED` | false | validator `stdout` 输出 JSON 错误对象，`stderr` 记录详细报错 |
| 算法自由文本级别未知 | `ALGO_LOG_LEVEL_UNKNOWN` | - | 降级为 WARN 级别记录，递增 `unknown_algo_levels` 统计 |
| 日志管道 I/O 阻塞/满 | - | - | 丢弃日志并累加 `dropped_*` 指标，不阻塞音视频主线程 |

---

## 5. Good / Base / Bad Cases

### Case 1: 算法推理报错处理
- **Good**: 
  ```cpp
  const int status = abi.instance_process(inst, &frame);
  if (status != AV_OK) {
      char last_err[512] = {0};
      abi.last_error(inst, last_err, sizeof(last_err));
      LOG_ERROR("engine.algo_host", "algo.process_failed", last_err,
                map_abi_status_to_code(status), {{"frame_id", std::to_string(frame.frame_id)}});
  }
  ```
- **Base**: 打印错误并返回错误码，但未携带 `frame_id` 或 `code`。
- **Bad**: `std::cerr << "Process failed: " << status << std::endl;`，裸流输出且未转换错误码。

### Case 2: 子进程校验结果解析
- **Good**: 
  ```cpp
  auto json_obj = nlohmann::json::parse(stdout_str);
  result.success = json_obj.value("success", false);
  result.error_code = json_obj.value("error_code", "");
  ```
- **Bad**: 
  ```cpp
  // 严禁模式匹配日志字符串
  if (stdout_and_stderr.find("Successfully validated package") != std::string::npos) { ... }
  ```

---

## 6. Tests Required

在新增或重构错误与日志模块时，必须具备以下确定性测试：
1. **格式与序列化测试**：断言每条日志为合法单行 JSON，包含必填字段（`ts`, `level`, `component`, `event`, `message`）与预期上下文。
2. **级别过滤与回退测试**：验证全局阈值过滤有效，非法环境变量字符串回退为 `INFO`。
3. **脱敏与安全测试**：验证 RTSP URL 中的密码与 token 被安全剥离，敏感 key 被拦截，超长字符串被截断。
4. **上下文隔离测试**：验证多线程并发与 `ScopedLogContext` 作用域栈不会发生数据竞争和上下文串线。
5. **非阻塞与高并发测试**：模拟多线程打满队列，验证生产线程不发生死锁/长时间卡顿，`dropped_normal` 统计正确递增。
6. **SDK 桥接与错误码映射契约测试**：验证 C ABI 状态码到稳定 `code` 的映射表完整无遗漏。

---

## 7. Wrong vs Correct

#### Wrong
```cpp
// 错误示范 1: 热路径打印 INFO 日志
void on_frame_decoded(const av_frame_desc* frame) {
    LOG_INFO("decoder", "frame.received", "Got frame id=" + std::to_string(frame->frame_id));
}

// 错误示范 2: 打印未脱敏的 URL 与敏感密码
LOG_ERROR("media", "connect.failed", "Failed to connect rtsp://admin:secret123@192.168.1.50/stream");

// 错误示范 3: 靠日志文本做流程控制
if (output.find("Error code: ") != std::string::npos) { ... }
```

#### Correct
```cpp
// 正确示范 1: 热路径不打印 INFO，仅在异常或 DEBUG 模式下记录
void on_frame_decoded(const av_frame_desc* frame) {
    LOG_DEBUG("decoder", "frame.received", "Frame decoded", "", {{"frame_id", std::to_string(frame->frame_id)}});
}

// 正确示范 2: 自动脱敏 URL
LOG_ERROR("media", "connect.failed", "Failed to connect", "MEDIA_CONNECT_FAILED",
          {{"url", LogSanitizer::sanitize_url(raw_rtsp_url)}});

// 正确示范 3: 基于机器 JSON 反序列化判断
if (!json_res.value("success", false)) {
    result.error_code = json_res.value("error_code", "INTERNAL_ERROR");
}
```
