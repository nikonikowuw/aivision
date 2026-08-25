# C++ Engine 结构化日志 技术设计 (Design)

## 1. 架构总览与模块划分 (Architecture Overview)

为满足高吞吐实时音视频处理场景对低延迟、非阻塞、异常安全和故障诊断的要求，C++ Engine 日志系统采用“业务轻量入队 + 后台专用 I/O 线程序列化”的异步解耦架构。

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Engine / Validator 业务线程                      │
│                                                                        │
│  LOG_INFO(...) / LOG_ERROR(...) / SDK av_log_fn                        │
│       │                                                                │
│       ▼                                                                │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ 1. 级别快速过滤 (Atomic Level Check)                              │  │
│  │ 2. 捕获上下文快照 (LogContext Snapshot)                            │  │
│  │ 3. 构造 LogRecord (白名单校验 / 脱敏 / 截断 / 提取 source_location)│  │
│  │ 4. 非阻塞入队 (Non-blocking Try Push)                            │  │
│  └──────────────────┬─────────────────────────────┬─────────────────┘  │
└─────────────────────┼─────────────────────────────┼────────────────────┘
                      │                             │
                      ▼ (DEBUG / INFO)              ▼ (WARN / ERROR / FATAL)
         ┌─────────────────────────┐   ┌────────────────────────┐
         │ Normal Queue (容量 2048) │   │  High Queue (容量 256) │
         └────────────┬────────────┘   └───────────┬────────────┘
                      │                            │
                      └─────────────┬──────────────┘
                                    │
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        Logger 后台 Writer 线程                          │
│                                                                        │
│  1. 优先级公平调度出队 (High Queue 优先，单轮最大批量避免饥饿)            │
│  2. JSONL 单行序列化 (nlohmann_json / fast buffer)                     │
│  3. 写入目标 Sink (默认 stderr -> systemd/journald)                     │
│  4. 维护本地 LoggerStats 原子计数器                                    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 核心数据结构与契约 (Data Structures & Contracts)

### 2.1 结构化记录 `LogRecord`
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

struct SourceLocation {
    const char* file{nullptr};
    int line{0};
    const char* function{nullptr};
};

struct LogRecord {
    uint64_t seq{0};                         // 进程内单调递增序号
    std::chrono::system_clock::time_point ts;// 记录生成 UTC 时间戳
    Level level{Level::Info};
    std::string component;                   // 组件名 (如 "engine.algo_host")
    std::string event;                       // 事件名 (小写点号分层，如 "algo.process_failed")
    std::string code;                        // 稳定错误码 (如 "ALGO_PROCESS_FAILED")
    std::string message;                     // 清洗、脱敏与截断后的说明
    bool message_truncated{false};
    size_t raw_message_bytes{0};
    LogContextSnapshot context;
    SourceLocation loc;
    std::map<std::string, std::string> extra_fields; // 受控白名单标量字段
};

} // namespace aivision::logging
```

### 2.2 上下文管理器 `LogContext`
- 采用 `thread_local` 维护当前线程的作用域栈。
- 支持 RAII 作用域结构 `ScopedLogContext`：
  ```cpp
  {
      ScopedLogContext ctx({
          {"camera_id", "cam_01"},
          {"instance_id", "inst_face_01"}
      });
      // 当前代码块及下游同步调用均自动附带该上下文快照
  }
  ```
- 异步入队时对当前栈顶做全量深拷贝快照（`LogContextSnapshot`），彻底消除多线程并发或 Worker 复用时的上下文串线风险。

---

## 3. 双队列与无锁调度设计 (Queue & Dispatch Mechanism)

### 3.1 容量与淘汰策略
- **Normal Queue**：容量固定为 `2048`，承载 `Debug` 与 `Info`。
  - 队列满时：立即递增 `dropped_normal` 原子计数，非阻塞丢弃，绝不阻塞生产线程。
- **High Queue**：容量固定为 `256`，承载 `Warn`、`Error`、`Fatal`。
  - 队列满时：立即递增 `dropped_high` 原子计数，非阻塞丢弃，保证极端雪崩情况下服务不卡死。

### 3.2 Writer 线程调度与防饥饿策略
- Writer 线程等待条件变量通知（或固定 10ms 超时轮询）。
- **调度规则**：
  1. 优先消费 High Queue 中的所有就绪记录。
  2. 消费 Normal Queue，但单次连续处理不超过 `BATCH_SIZE = 64` 条记录，随后重新检查 High Queue，防止突发低级别日志导致高级别告警延迟，同时避免 Normal Queue 完全饥饿。
- **优雅停机 (Shutdown)**：
  - 设置 `running_ = false`，唤醒 Writer 线程。
  - 最多等待 `2000ms`（2秒）排空双队列中的剩余日志。
  - 超时后丢弃剩余队列数据并安全退出。

---

## 4. 字段安全、脱敏与清洗机制 (Sanitization & Safety)

### 4.1 字段白名单与大小限制
- 单条 JSONL 记录上限：`16 KiB`。
- `message` 上限：`8 KiB`（超长截断并标记 `message_truncated: true`）。
- 单个标量字段上限：`1 KiB`。
- 字段白名单机制：只允许标准上下文字段及受控标量字段，未知字段丢弃并递增 `rejected_fields`。

### 4.2 敏感信息脱敏规则 (Redaction)
- **URL 脱敏**：通过正则或专用解析器剥离 `rtsp://user:pass@host:port/path?query` 中的 `user:pass` 与 `?query`，保留安全端点。
- **敏感关键字黑名单**：禁止记录包含 `password`, `token`, `secret`, `authorization`, `gRPC metadata` 的键值。
- **二进制安全**：禁止输出原始图片/人脸特征 Byte 数组及未格式化的二进制块。

### 4.3 算法包不可信自由文本清洗 (`av_log_fn` & `last_error`)
- 算法包传入的 `const char* msg` 视为不可信输入：
  1. 剔除 ASCII 控制字符（保留常用换行/制表符并做转义处理）。
  2. 强制做 UTF-8 校验，非法字节序列转义或替换为 `?`。
  3. 执行 URL/敏感词脱敏和 1KB 长度截断。

---

## 5. Validator 进程协议重构 (Validator Process Protocol)

### 5.1 彻底分离 stdout 与 stderr
- **`stdout` (机器契约)**：只输出单行合法 JSON 对象，严禁混入任何调试日志。
  ```json
  {
    "success": true,
    "error_code": "",
    "error_stage": "",
    "error_message": "",
    "manifest": {
      "algorithm_id": "yolov8n",
      "version": "1.0.0"
    },
    "package_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  }
  ```
- **`stderr` (可观测性日志)**：`package_validator` 初始化与 Engine 相同的 Logger，所有运行日志输出到 `stderr`。
- **Engine 侧接收**：
  - 重构 `algo_sandbox.cpp` 的 `capture_command`：只捕获子进程的 `stdout` 作为机器响应进行 JSON 反序列化；子进程的 `stderr` 透传到当前进程的 `stderr`（直通 journald）。
  - 控制流仅依据解析出的 `success` 与 `error_code` 进行判断，杜绝字符串模式匹配。

---

## 6. 异常与故障自愈 (Error Handling & Fail-Open)

1. **公开 API `noexcept` 保证**：所有 `LOG_*` 宏与函数均包装在 `noexcept` 块中，内部捕获 `std::exception` 并递增错误计数器。
2. **Sink 写入失败处理**：
   - 当 `stderr` 或管道发生 `EPIPE` / `EIO` 时，递增 `sink_write_failures`，采用限频（如每 5 秒最多 1 次）方式尝试输出警告或直接丢弃。
   - 绝不调用自身 Logger 递归记录错误，防止死锁或栈溢出。
3. **`Fatal` 级别语义**：
   - 记录 `level: "fatal"`，但 Logger 自身绝不调用 `std::abort()` 或 `exit()`。由上层业务逻辑根据错误域决定退出或熔断。

---

## 7. 统计指标 `LoggerStats`
提供线程安全读取的本地统计结构体：
```cpp
struct LoggerStats {
    uint64_t records_written{0};
    uint64_t dropped_normal{0};
    uint64_t dropped_high{0};
    uint64_t sink_write_failures{0};
    uint64_t message_truncations{0};
    uint64_t rejected_fields{0};
    uint64_t unknown_algo_levels{0};
    size_t current_normal_queue_depth{0};
    size_t current_high_queue_depth{0};
};
```
