# 算法包 C ABI 接口规范

> 本规范定义算法动态库的导出符号、两级生命周期、能力协商、调用线程、安装自测和错误语义。字段布局细节以 [abi-guidelines.md](./abi-guidelines.md) 为准。

## 1. Scope / Trigger

修改插件加载器、`algo.h`、算法实例调度、结果回调、配置更新或安装自测时必须读取本规范。

推理模型加载、推理运行时 API 和实例推理上下文归算法包所有。Engine 不提供第二套 `IInferenceContext`；平台适配层只声明运行时能力，并通过 `av_image_ops` 提供帧转换机制。

## 2. 导出与虚表签名

动态库必须只导出一个 C 符号，其余符号隐藏：

```c
#define AV_ALGO_API_VERSION 1u

const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version);
```

宿主使用 `dlopen(path, RTLD_NOW | RTLD_LOCAL)`。返回的虚表具有静态存储期；不支持请求版本时返回 `NULL`。

```c
typedef struct av_algo_abi {
  uint32_t size;
  uint32_t api_version;

  int (*library_open)(const av_algo_library_args*, av_algo_library* out);
  int (*library_query)(av_algo_library, av_algo_library_info* out);
  int (*library_close)(av_algo_library);

  int (*instance_create)(av_algo_library, const av_algo_instance_args*, av_algo_instance* out);
  int (*instance_negotiate)(av_algo_instance, const av_frame_caps* offered,
                            av_frame_caps* accepted);
  int (*instance_update_config)(av_algo_instance, const char* json, uint32_t len);
  int (*instance_process)(av_algo_instance, const av_frame_desc* frame);
  int (*instance_flush)(av_algo_instance);
  int (*instance_destroy)(av_algo_instance);

  int (*last_error)(av_algo_instance inst_or_null, char* buf, uint32_t cap);
} av_algo_abi;
```

所有函数指针在 ABI v1 均为必填。调用前宿主必须同时验证 `api_version`、`size` 和非空函数指针。

## 3. 生命周期与固定参数

### 3.1 Library 级

每个已安装的 `algorithm_id + version + platform_id` 最多打开一个 Library。Library 持有模型工件和可安全共享的只读资源；是否共享运行时对象必须由算法包按目标运行时保证。

```c
typedef void (*av_log_fn)(void* user, int level,
                          const char* msg, uint32_t len);

typedef struct av_algo_library_args {
  uint32_t size;
  uint32_t api_version;
  const char* package_root;
  const char* platform_id;
  uint32_t platform_tag;
  av_log_fn log;
  void* log_user;
} av_algo_library_args;
```

算法必须以 `package_root` 为根定位 manifest 中声明的模型和依赖。禁止依赖当前工作目录、仓库路径或自行写日志文件。

`av_algo_library_info` 至少回填算法 ID、版本、类型和支持的帧能力；宿主逐项与 manifest 比对。资源 FPS 档位以 manifest 为权威，不在虚表中复制第二份可漂移数据。

### 3.2 Instance 级

每个摄像头任务挂载的算法创建一个 Instance，持有独立推理上下文、跟踪状态、事件状态与配置。

```c
typedef enum av_instance_mode {
  AV_INSTANCE_NORMAL = 1,
  AV_INSTANCE_INSTALL_SELF_TEST = 2
} av_instance_mode;

typedef struct av_algo_instance_args {
  uint32_t size;
  uint32_t api_version;
  uint32_t mode;
  uint32_t reserved0;
  const char* instance_id;       /* 稳定逻辑 ID */
  const char* instance_run_id;   /* 每次激活唯一的 UUID/ULID */
  const char* config_json;
  uint32_t config_json_len;
  uint32_t reserved1;
  const av_frame_ops* frame_ops;
  const av_image_ops* image_ops;
  av_algo_result_cb on_result;
  void* result_user;
} av_algo_instance_args;
```

`instance_create` 解析整份初始配置并创建独立运行上下文，但不得重复加载可在 Library 级复用的模型工件。输入字符串只在调用期间有效，算法需要长期使用时必须复制。

### 3.3 帧能力协商

```c
typedef struct av_frame_caps {
  uint32_t size;
  uint32_t api_version;
  uint32_t pixel_format_count;
  uint32_t pixel_formats[8];
  uint32_t memory_type_count;
  uint32_t memory_types[4];
  uint32_t required_opaque_kind;
  uint32_t min_width;
  uint32_t min_height;
  uint32_t max_width;
  uint32_t max_height;
} av_frame_caps;
```

宿主传 offered，算法按偏好回填其子集。accepted 中出现 offered 之外的值、计数越界或尺寸无交集时，宿主返回 `AV_ERR_INCOMPATIBLE_FRAME`，且不得开始分发帧。

## 4. 结果与安装自测契约

```c
typedef enum av_result_kind {
  AV_RESULT_ALARM = 1,
  AV_RESULT_SELF_TEST = 2
} av_result_kind;

typedef struct av_algo_image_req {
  uint32_t size;
  uint32_t api_version;
  float x;
  float y;
  float w;
  float h;
  uint32_t purpose;
  uint32_t reserved0;
} av_algo_image_req;

typedef struct av_algo_result {
  uint32_t size;
  uint32_t api_version;
  uint32_t kind;
  uint32_t reserved0;
  uint64_t frame_id;
  const char* json;
  uint32_t json_len;
  uint32_t image_count;
  const av_algo_image_req* images;
} av_algo_result;
```

`av_algo_image_req` 固定为 32 字节，`av_algo_result` 在 64 位目标固定为 48 字节；SDK 头必须为二者提供 `AV_STATIC_ASSERT`。

### 4.1 正常结果

- 一次 `instance_process` 可回调零次、一次或多次；每个 `AV_RESULT_ALARM` 是一条完整告警。
- 算法生成在 `instance_run_id` 内唯一的 `event_id` 和 manifest 已声明的 `alarm_type_id`。
- Engine 使用 `<instance_run_id>/<algo_event_id>` 形成全局事件 ID 并在落图前去重。
- 算法只提交归一化 ROI；裁剪、JPEG、图片 ID、落盘与 gRPC 上报归 Engine。
- 回调只能发生在 `instance_process` 或 `instance_flush` 的调用栈内。Engine 必须在回调返回前复制 JSON 和图片请求。

### 4.2 安装自测

安装 validator 使用 `AV_INSTANCE_INSTALL_SELF_TEST` 创建实例，并把 manifest 的测试图包装为协商后的真实帧格式后调用一次 `instance_process`。

自测实例必须：

1. 走真实的预处理、模型推理、后处理和序列化路径；
2. 在调用栈内恰好回调一次 `AV_RESULT_SELF_TEST`；
3. 即使测试图没有检测目标，也返回合法 self-test JSON；
4. 不产生图片请求、不写文件、不修改正常实例状态。

这将“链路可运行”与“测试图必须检出某个类别”分离。安装校验不判断精度、类别语义或检测数量。

## 5. 线程、配置与停止契约

| 接口 | 调用保证 |
| --- | --- |
| `library_open/close` | 单线程，且不与该 Library 的实例调用重叠 |
| `library_query` | 可与实例调用并发，必须只读可重入 |
| 同一 Instance 的全部函数 | 严格串行，禁止重入 |
| 不同 Instance | 可并发，不得共享无同步的可变状态 |
| `on_result` | 仅在 process/flush 调用栈内 |
| `log`、`frame_ops`、`image_ops` | Engine 侧线程安全 |

配置更新是整份替换：算法必须先解析并构造候选状态，全部验证成功后再原子交换；失败返回 `AV_ERR_CONFIG_INVALID`，旧状态保持不变。宿主保证 update 与 process/flush 互斥。

处理超时后宿主停止向该实例分发新帧，但不得强杀插件线程或提前复用帧。`instance_flush`/`destroy` 必须有界返回、join 算法自建线程并释放全部 retain 的帧。

## 6. 内存所有权

| 对象 | 有效期 | 释放方 |
| --- | --- | --- |
| `library_args`、`instance_args` 及内部字符串 | 当前调用 | Engine |
| ABI 虚表 | `dlopen` 到 `dlclose` | 静态存储，不释放 |
| Library/Instance 句柄 | 对应生命周期 | 算法包 |
| `av_frame_desc` | process 调用；额外持有见 frame token | Engine |
| `frame_ops`、`image_ops` | Instance 生命周期 | Engine |
| `av_algo_result`、JSON、images | 回调期间 | 算法包 |
| `image_ops->alloc` 的缓冲 | 直到匹配 free | 算法包调用 `image_ops->free` |
| `last_error` 输出缓冲 | 当前调用 | Engine 提供 |

## 7. Validation & Error Matrix

```c
typedef enum av_algo_status {
  AV_OK = 0,
  AV_ERR_UNSUPPORTED_API = -1,
  AV_ERR_INVALID_ARG = -2,
  AV_ERR_INCOMPATIBLE_FRAME = -3,
  AV_ERR_CONFIG_INVALID = -4,
  AV_ERR_MODEL_LOAD_FAILED = -5,
  AV_ERR_INFERENCE_FAILED = -6,
  AV_ERR_OUT_OF_MEMORY = -7,
  AV_ERR_NOT_IMPLEMENTED = -8,
  AV_ERR_TIMEOUT = -9,
  AV_ERR_INTERNAL = -99
} av_algo_status;
```

| 条件 | 结果 |
| --- | --- |
| 唯一导出符号缺失或有额外导出 | 拒绝安装 |
| ABI 版本/尺寸/函数指针不完整 | `AV_ERR_UNSUPPORTED_API`，拒绝安装 |
| Library 信息与 manifest 不一致 | 拒绝安装，结构化 `PACKAGE_METADATA_MISMATCH` |
| 能力无交集 | `AV_ERR_INCOMPATIBLE_FRAME` |
| 配置非法 | `AV_ERR_CONFIG_INVALID`，旧配置继续生效 |
| self-test 无回调、多回调、kind 错误或超时 | 拒绝安装 |
| process 单帧失败 | 丢当前帧并计数；达到 Profile 阈值后停用实例 |
| C++ 异常越过 ABI | 契约测试失败；入口必须转换为 `AV_ERR_INTERNAL` |

## 8. Good / Base / Bad Cases

- Good：正常实例无告警时零回调；self-test 实例始终返回一条 self-test 结果。
- Base：同步算法在 process 返回前完成全部工作，不额外 retain 帧。
- Bad：插件自行写 `result.jpg`、从后台线程触发结果回调，或依赖进程当前目录加载模型。

## 9. Tests Required

- 单导出符号、`RTLD_LOCAL`、缺函数指针、旧 size 和版本拒绝测试。
- Library/Instance 创建销毁顺序及失败中途清理测试。
- 同实例无重入、不同实例并发、update/process 互斥和 flush/join 测试。
- 正常零/一/多回调及重复 event ID 去重测试。
- self-test 零检测仍成功、缺回调、多回调、超时和非法 JSON 测试。
- 跨 ABI 异常转换和 `last_error` 截断/NUL 结尾测试。

## 10. Wrong vs Correct

```cpp
// Wrong: 异常越过 C ABI
extern "C" int instance_process(...) {
  return backend.run();
}

// Correct
extern "C" int instance_process(...) noexcept {
  try {
    return backend.run();
  } catch (const std::exception& e) {
    set_last_error(e.what());
    return AV_ERR_INTERNAL;
  } catch (...) {
    set_last_error("unknown exception");
    return AV_ERR_INTERNAL;
  }
}
```
