# 错误处理、日志与可观测性规范

> 本规范定义 Engine 内部错误、C ABI 错误、gRPC 状态、结构化日志和运行指标之间的映射，避免每个模块发明自己的错误文本和字段名。

## 1. Scope / Trigger

新增错误分支、RPC、插件调用、媒体状态、validator 失败、重试或指标时必须读取本规范。

## 2. Signatures

Engine 内部统一错误对象：

```cpp
enum class ErrorDomain {
  Config, Media, Decode, Frame, Algorithm, Package,
  Image, Resource, Ipc, Platform, Internal
};

struct EngineError {
  ErrorDomain domain;
  std::string code;        // 稳定机器码，例如 PACKAGE_CHECKSUM_MISMATCH
  std::string message;     // 可读摘要，不含 secret
  bool retryable;
  std::map<std::string, std::string> details;
};
```

内部函数使用 `Result<T, EngineError>` 或项目统一等价类型，不以空指针、日志文本或 errno 猜测失败原因。

跨 C ABI 使用 `av_algo_status + last_error`；`last_error` 必须 NUL 结尾，容量不足时安全截断并仍返回完整错误所需长度或稳定的 truncated 状态。插件错误文本只用于诊断，控制流只依赖错误码。

gRPC 失败使用标准 status 表达 transport 类别，并在结构化 details 中携带稳定 `code`、`retryable` 和字段错误；禁止客户端解析 message 文本。

## 3. Contracts

### 3.1 稳定错误码

至少统一以下机器码：

```text
CONFIG_INVALID / CONFIG_SCHEMA_INVALID / STALE_REVISION
MEDIA_CONNECT_FAILED / MEDIA_INGEST_TIMEOUT
DECODER_STALLED / DECODER_RECREATE_FAILED
FRAME_CAPS_INCOMPATIBLE / FRAME_TOKEN_INVALID
ALGO_ABI_INCOMPATIBLE / ALGO_RESULT_INVALID / ALGO_PROCESS_TIMEOUT
PACKAGE_MANIFEST_INVALID / PACKAGE_CHECKSUM_MISMATCH
PACKAGE_INCOMPATIBLE / PACKAGE_IN_USE / VALIDATOR_CRASHED
IMAGE_WRITE_FAILED / IMAGE_PATH_INVALID
RESOURCE_LIMIT_EXCEEDED / MEMORY_LIMIT_EXCEEDED
IPC_UNAVAILABLE / INTERNAL_ERROR
```

错误码一经被 Proto、日志查询或测试引用，不得改义或复用。

### 3.2 结构化日志

每条日志固定字段：

```text
ts, level, component, event, code?, message,
platform_id?, device_id?, camera_id?, task_id?,
instance_id?, instance_run_id?, algorithm_id?, package_version?,
frame_id?, revision?, retry_count?, duration_ms?
```

- `event` 是稳定动作名，如 `decoder.recreated`；`message` 是可读说明。
- 同一状态转移只记录一次；逐帧路径禁止 INFO 日志。
- URL 只记录去除 userinfo/query 后的 endpoint；密码、token、gRPC metadata、完整配置 JSON、人脸/图片字节不得记录。
- VUI 缺失等降级日志按 camera/source 生命周期去重。

### 3.3 指标

至少暴露：队列深度、采样 FPS、处理 FPS、丢帧数、process 错误/超时、重连次数、decoder 重建次数、RPC 成败、validator 时长、图片写入失败、资源 units。指标 label 禁止使用 `event_id`、`frame_id` 等高基数字段。

## 4. Validation & Error Matrix

| 场景 | 机器码 | retryable | 行为 |
| --- | --- | --- | --- |
| RTSP 临时不可达 | `MEDIA_CONNECT_FAILED` | true | 按 Profile 退避 |
| 帧能力无交集 | `FRAME_CAPS_INCOMPATIBLE` | false | 实例不启动 |
| 算法单帧失败 | 插件错误映射 | 视错误码 | 丢当前帧、计数 |
| 算法超时 | `ALGO_PROCESS_TIMEOUT` | false | 停止分发，不强杀 |
| checksum 错误 | `PACKAGE_CHECKSUM_MISMATCH` | false | 拒绝安装 |
| 资源超限 | `RESOURCE_LIMIT_EXCEEDED` | false | 原子拒绝候选变更 |
| Go UDS 暂时断开 | `IPC_UNAVAILABLE` | true | 重连并拉全量状态 |
| 未知异常 | `INTERNAL_ERROR` | false | 边界捕获、状态 degraded |

## 5. Good / Base / Bad Cases

- Good：同一个 `PACKAGE_CHECKSUM_MISMATCH` 同时出现在 validator result、Engine 日志和 gRPC detail。
- Base：一次 VUI 缺失只记录一条 WARN，后续帧只增加指标。
- Bad：返回 `INTERNAL`，然后要求调用方解析 `"checksum wrong"` 文本决定是否重试。

## 6. Tests Required

- 每个公共错误分支断言 domain、code、retryable 和关键 details。
- C++ 异常/Objective-C error 到 ABI/EngineError/gRPC 的转换测试。
- `last_error` 空 buffer、小 buffer、完整 buffer 和 NUL 结尾测试。
- 日志字段、敏感信息脱敏、VUI 去重和逐帧无 INFO 测试。
- 指标递增与低基数 label 审计。
- gRPC 客户端只依赖 status/details，不解析 message 的契约测试。

## 7. Wrong vs Correct

```cpp
// Wrong
LOG_ERROR("install failed: " + package_path + " password=" + password);
return grpc::Status(grpc::StatusCode::INTERNAL, "checksum wrong");

// Correct
return make_error(ErrorDomain::Package,
                  "PACKAGE_CHECKSUM_MISMATCH",
                  "package file checksum mismatch",
                  false,
                  {{"relative_path", safe_relative_path}});
```
