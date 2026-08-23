# 算法包规范（草案 v1）

> 规划期草案。Phase 2 落地时转为 `sdk/docs/abi.md` + `sdk/include/aivision/algo.h`，并在 Phase 2 末冻结。
> 本文只定义**引擎与算法包之间的契约**：如何加载、如何初始化、如何调用、如何销毁、出错怎么办。

## 1. 两级生命周期

PRD §7.7 要求「同一模型的并发工作单元使用独立推理上下文，并以复用只读模型资源为目标」。因此加载分两级，不能只有一层 create/destroy：

| 级别 | 数量 | 持有什么 | 典型内容 |
| --- | --- | --- | --- |
| **Library**（库级） | 每个已安装算法包版本一个 | 只读、可共享资源 | 模型权重、类别表、常量查找表 |
| **Instance**（实例级） | 每个算法实例一个（= 摄像头任务上挂的一个算法） | 可变、独占资源 | 推理上下文、跟踪器状态、事件聚合与去重表、当前配置 |

一个 Library 下可以有 N 个 Instance。Instance 之间不共享任何可变状态。

## 2. 导出符号

算法包动态库**只导出一个符号**，其余全部隐藏（`-fvisibility=hidden`）：

```c
const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version);
```

- 引擎传入自己支持的 `AV_ALGO_API_VERSION`。
- 算法包若无法满足该版本，返回 `NULL`（不是���错，是「我不兼容」）。
- 返回的 `av_algo_abi` 必须是**静态存储期**对象，生命周期覆盖整个 `dlopen` 期间，引擎不释放它。

```c
typedef struct av_algo_abi {
  uint32_t size;            /* = sizeof(av_algo_abi) */
  uint32_t api_version;     /* 算法包实际实现的版本 */

  /* --- 库级 --- */
  int (*library_open)(const av_algo_library_args* args, av_algo_library* out);
  int (*library_query)(av_algo_library lib, av_algo_library_info* out);
  int (*library_close)(av_algo_library lib);

  /* --- 实例级 --- */
  int (*instance_create)(av_algo_library lib, const av_algo_instance_args* args,
                         av_algo_instance* out);
  int (*instance_negotiate)(av_algo_instance inst, const av_frame_caps* offered,
                            av_frame_caps* accepted);
  int (*instance_update_config)(av_algo_instance inst, const char* json, uint32_t len);
  int (*instance_process)(av_algo_instance inst, const av_frame_desc* frame);
  int (*instance_flush)(av_algo_instance inst);
  int (*instance_destroy)(av_algo_instance inst);

  /* --- 通用 --- */
  int (*last_error)(av_algo_instance inst_or_null, char* buf, uint32_t cap);
} av_algo_abi;
```

`av_algo_library` 与 `av_algo_instance` 都是不透明句柄（`typedef struct av_algo_library_t* av_algo_library;`）。引擎绝不解引用。

## 3. 加载流程

引擎侧固定按此序列执行，安装校验（PRD §7.5.4 第 4–6 步）与正常启用走同一条路径：

```
1. dlopen(<install_root>/<algorithm_id>/<version>/lib/<name>.{dylib,so},
          RTLD_NOW | RTLD_LOCAL)          ← RTLD_LOCAL 防止符号污染其他算法包
2. dlsym("av_algo_get_abi")               ← 找不到 → 拒绝安装
3. abi = av_algo_get_abi(AV_ALGO_API_VERSION)
   · NULL          → 版本不兼容，拒绝，返回结构化原因
   · abi->size < 最小已知尺寸 → 拒绝
4. library_open(args)                     ← 此处加载模型，耗时操作
5. library_query(lib, &info)
   · 校验 info 与 manifest.json 声明一致（算法类型、resource_units、帧能力）
   · 不一致 → 拒绝安装（清单撒谎比不声明更危险）
6. 安装校验专用：instance_create → negotiate → process(testimage.jpg)
                → 校验结果 JSON 符合 Schema → instance_destroy
7. 卸载：所有 instance_destroy 完成后 library_close，最后 dlclose
```

**`RTLD_LOCAL` 是硬要求**：多个算法包可能各自静态链接了不同版本的同名第三方库（OpenCV、protobuf），`RTLD_GLOBAL` 会让先加载者的符号劫持后加载者，症状是随机崩溃或结果错乱，且极难归因。

## 4. 初始化

### 4.1 库级

```c
typedef struct av_algo_library_args {
  uint32_t size; uint32_t api_version;
  const char* package_root;      /* 包安装根目录绝对路径，以 '/' 结尾 */
  const char* platform_id;       /* 完整字符串，如 "macos-arm64-coreml" */
  uint32_t    platform_tag;      /* 与 av_frame_desc.platform_tag 对应 */
  const void* platform_context;  /* 适配层提供的不透明上下文 */
  uint32_t    platform_context_kind;
  av_log_fn   log;               /* 日志回调，算法不得自行写文件 */
  void*       log_user;
} av_algo_library_args;
```

- **`package_root`**：算法包用它拼出模型路径（`package_root + "model/yolov8n.mlpackage"`）。算法**不得**使用相对路径或硬编码路径——包被解压到 `var/packages/<algorithm_id>/<version>/`，位置由引擎决定。
- **`platform_context`**：适配层往下传的平台句柄。macOS 上是 Core ML 的 compute unit 偏好等；RKNN 上可以是共享的 `rknn_context` 或 NPU core mask。算法按 `platform_context_kind` 判断能否 cast。这是**平台私有信息进入算法包的唯一合法通道**，核心层不解释它。
- **`log`**：`void (*av_log_fn)(void* user, int level, const char* msg, uint32_t len)`。引擎保证线程安全，算法可在任意线程调用。算法自己写文件会绕开日志轮转和脱敏，禁止。

### 4.2 实例级

```c
typedef struct av_algo_instance_args {
  uint32_t size; uint32_t api_version;
  const char* instance_id;          /* 引擎分配，用于日志关联 */
  const char* config_json;          /* 初始配置，已通过引擎侧 Schema 基础校验 */
  uint32_t    config_json_len;
  const av_frame_ops*  frame_ops;   /* ref/unref 函数表 */
  const av_image_ops*  image_ops;   /* 平台加速的图像原语，见 §4.3 */
  av_algo_result_cb    on_result;   /* 结果回调 */
  void*                result_user;
} av_algo_instance_args;
```

`instance_create` 里完成：解析配置、建立独立推理上下文、初始化跟踪器与聚合状态。**不在这里加载模型**——模型已在 `library_open` 加载完毕。`frame_ops` 与 `image_ops` 一次性下发，算法自行存进句柄，后续不再随帧传递。

### 4.3 图像加速原语

引擎**只提供机制，不定义策略**。前处理的具体逻辑（letterbox、通道顺序、归一化、tiling、多输入组装）完全由算法决定，引擎既不理解也不关心；引擎提供的是让这些操作跑在硬件上的原语：

```c
typedef struct av_image_ops {
  uint32_t size; uint32_t api_version;
  /* 从共享帧裁剪 + 缩放 + 色彩转换到算法自备的目标缓冲 */
  int (*convert)(void* ctx, const av_frame_desc* src, const av_rect* src_roi,
                 const av_image_view* dst, uint32_t filter);
  /* 用指定值填充目标缓冲的某个区域（letterbox 底色等） */
  int (*pad)(void* ctx, const av_image_view* dst, const av_rect* region,
             const uint8_t value[4]);
  /* 分配 / 释放平台友好的对齐缓冲（DMA-BUF、IOSurface 等），可直接作为推理输入 */
  int (*alloc)(void* ctx, uint32_t w, uint32_t h, uint32_t fmt, av_image_view* out);
  int (*free)(void* ctx, av_image_view* buf);
  void* ctx;
} av_image_ops;
```

- `macos-arm64-coreml` 下由 vImage / Core Image 实现，`rk3576-rknn` 下由 RGA 实现；能力不可用时适配层退回 CPU 实现并在能力档案中标注「降级」。
- 算法**可以不用**这些原语，自己写 CPU 前处理，但会失去硬件加速。
- 硬件是共享资源，排队发生在**原语实现内部**，算法无感知。这也是为什么引擎不需要理解算法的内部阶段就能调度硬件争用。

**为什么不做「声明式前处理」**：曾考虑让算法声明输入张量规格、由引擎代做前处理。放弃了——每个算法的前处理本就不一致（多输入模型、时序堆叠、两阶段裁剪、tiling 都无法描述），声明式路径永远覆盖不全、必须留 custom 兜底；而既然兜底路径必须存在，且加速原语无论如何都要暴露，那层声明式描述的增量价值只剩「同规格实例复用」这一小概率场景，不值它的复杂度。

## 5. 能力协商

`instance_negotiate` 在实例创建后、首帧分发前调用一次。引擎给出它能提供的帧能力集合，算法回填它接受的子集：

```c
typedef struct av_frame_caps {
  uint32_t size; uint32_t api_version;
  uint32_t pixel_format_count;  uint32_t pixel_formats[8];   /* 按优先级降序 */
  uint32_t memory_type_count;   uint32_t memory_types[4];
  uint32_t required_opaque_kind;   /* 0 = 不需要平台句柄 */
  uint32_t min_width, min_height;
  uint32_t max_width, max_height;
} av_frame_caps;
```

- 算法在 `accepted` 中按**自己的偏好**降序回填（比如 NV12 优于 BGRA，因为省一次转换）。
- 交集为空 → 返回 `AV_ERR_INCOMPATIBLE_FRAME`，引擎拒绝启用该实例并把双方能力一起写进结构化错误原因（PRD §7.4.3「不兼容时拒绝启用并返回明确原因」）。
- 协商结果在实例生命周期内不变。

## 6. 调用

### 6.1 处理帧

```c
int instance_process(av_algo_instance inst, const av_frame_desc* frame);
```

`process` 是**完整的一帧处理**：前处理、推理、后处理、跟踪与事件聚合全部在其中，算法如何切分内部阶段是它自己的事，引擎不介入（见 §4.3 关于前处理归属的说明）。

- **同步调用**。返回时该帧的处理必须已完成或已被算法内部显式延后（延后必�� `frame_ops->ref`）。
- 结果通过 `on_result` 回调发出，可以**零次、一次或多次**——目标检测包大多数帧不产生告警（零次），聚合完成的那一帧可能同时吐出多个事件（多次）。这与 PRD §7.10「每次回调 = 一条完整独立告警」一致。
- 算法**不得**阻塞超过实例配置的处理超时。
- 返回非 0 视为该帧处理失败：引擎记录、丢弃该帧、继续下一帧；连续失败超阈值则停用实例并上报。

### 6.2 结果回调

```c
typedef struct av_algo_image_req {
  float x, y, w, h;         /* [0,1] 归一化 ROI，原点左上角 */
  uint32_t purpose;         /* AV_IMG_ALARM_SNAPSHOT / AV_IMG_TARGET_CROP */
} av_algo_image_req;

typedef struct av_algo_result {
  uint32_t size; uint32_t api_version;
  uint32_t kind;                    /* AV_RESULT_ALARM（本期唯一） */
  uint64_t frame_id;                /* 关联的帧 */
  const char* json; uint32_t json_len;   /* 符合统一结果 Schema */
  const av_algo_image_req* images; uint32_t image_count;
} av_algo_result;

typedef void (*av_algo_result_cb)(void* user, const av_algo_result* result);
```

**算法不碰文件系统、不做图像编码**。它只在 `images` 里说明「请用这个框裁一张图」，裁剪、缩放、JPEG 编码、原子落盘、图片 ID 分配全部由引擎的 image 模块完成（PRD §7.11「所有图片文件由 C++ 图片模块统一管理」）。这也让算法包不必链接任何图像库。

- **事件身份与告警 ID 幂等（Event ID）**：
  - 目标检测告警每次触发代表一条独立的告警记录；
  - `event_id`（告警事件 ID）由算法在结果 `json` 中生成，并保证**在当前实例生命周期内唯一**（格式约束为 `[A-Za-z0-9._/-]`，长度 ≤128 字节）；
  - **SDK 标准工具支持**：SDK 在 `aivision/utils/event_id.hpp` 提供了开箱即用的 `EventIdGenerator`（支持单调序号 `ev_1`、跟踪目标 `trk_<track_id>_<seq>` 与格式校验 `is_valid`）；
  - **Engine 全局唯一化与去重**：引擎将算法 `event_id` 与激活期全局唯一的 `instance_id` 组合为 `<instance_id>/<algo_event_id>` 对外提供全局唯一告警事件标识，并按此键执行幂等去重（AC7）。重复的 `event_id` 回调直接被引擎忽略，不重复裁图与上报；
  - **跨进程与 Webhook 幂等**：引擎将全局 `event_id` 上报给 Go 后端（作为告警记录主键之一），Go 服务与下游 Webhook 平台按 `event_id` 幂等落库和消费。

### 6.3 配置热更新

```c
int instance_update_config(av_algo_instance inst, const char* json, uint32_t len);
```

- **整份替换，不是增量 patch**。
- 语义严格二选一：全部接受并立即生效（返回 0），或全部拒绝且**内部状态完全不变**（返回 `AV_ERR_CONFIG_INVALID`）。不允许部分应用。
- 引擎保证与 `instance_process` 互斥调用，因此算法内部**不需要任何锁**。
- 只有算法返回 0，引擎才持久化新配置（PRD §7.6「仅在算法实例确认成功后持久化」）。

### 6.4 排空

```c
int instance_flush(av_algo_instance inst);
```

引擎在停用实例、升级算法包、断流进入「重连中」前调用。算法应吐出所有已聚合但未上报的事件，并释放所有仍持有的帧引用。返回后引擎认为该实例不再持有任何帧。

## 7. 线程模型与调度

### 7.1 引擎的调度形态：每实例一个独占工作线程

```
实例A 有界帧队列 ──▶ 工作线程A（独占）──▶ instance_process()
实例B 有界帧队列 ──▶ 工作线程B（独占）──▶ instance_process()
   ...
```

算法包的整个契约建立在「同一实例的 `process` 不会重入」之上——它因此不写任何同步代码。独占线程让这个保证是**结构性**的：一个线程天然串行，不可能出错。若改用共享线程池，同一保证要靠调度器里「取任务时打实例占用标记」的逻辑正确，一旦有 bug 就是第三方算法包内部的数据竞争，症状是随机崩溃且无源码可查。

**调度模型是引擎内部实现，不是本规范的契约**。本规范只承诺下表的串行性质；引擎将来换成共享线程池不影响任何算法包。

**为什么不拆 `detect`/`commit` 两阶段流水线**：单实例内部的阶段重叠，收益只在整机跑 1~2 个实例时才存在，而本产品场景是 4~16 路（PRD §10.1），实例间并发已经把加速器填满。代价却是 reorder buffer、跨段背压、��生命周期延长，以及要求第三方算法作者正确切分有状态/无状态逻辑——大多数人会把状态误放进无状态段，写出压力下才暴露的 bug。

若将来实测证明确有必要，可在 `av_algo_abi` **末尾追加** `detect`/`commit` 函数指针：老包 `size` 小、引擎发现指针缺失即走串行路径，新包走流水线路径。零破坏、零迁移——这正是 `size` 字段存在的意义。

### 7.2 契约

| 契约 | 说明 |
| --- | --- |
| `library_open` / `library_close` | 引擎保证单线程调用，且不与任何 instance 调用重叠 |
| 同一 instance 的所有函数 | 引擎保证**严格串行**，任意时刻只有一个在执行 |
| 不同 instance 之间 | 可能并发。算法**不得**在实例间共享可变状态 |
| `library_query` | 可能与 instance 调用并发。必须只读、可���入 |
| `on_result` 回调 | 只能在 `instance_process` 或 `instance_flush` 的调用栈内发起，不得从算法自建线程发起 |
| `log` 回调 | 引擎保证线程安全，算法可在任意线程调用 |
| `image_ops` 各函数 | 引擎保证线程安全；内部对共享硬件排队，算法无感知 |
| 算法自建线程 | 允许，但必须在 `instance_destroy` 返回前全部 join。不得在实例销毁后触碰任何引擎资源 |

串行的直接收益：目标检测包按 PRD §7.5.1 必须在内部做跟踪、连续帧判断和事件聚合——这些**本来就是有状态、顺序依赖**的逻辑，单实例内并发对它没有意义，只会制造 bug。提高吞吐的正确路径是加实例（PRD §7.7「同一模型的并发工作单元使用独立推理上下文」），模型权重在 Library 级共享，只有推理上下文与跟踪状态各一份。

## 8. 超时

- 引擎为 `instance_process` 设墙钟超时（默认值待实测冻结，可配）。
- 超时后引擎**不强制中断**算法——C 代码无法安全中断，强杀会破坏推理上下文和内存。
- 引擎的动作是：停止向该实例分发新帧、标记实例「处理超时」、上报状态（PRD §7.4.4）。
- 连续超时超过阈值 → 停用实例，等待人工或上层策略介入。
- 已被算法持有的帧缓冲**不得回收**，直到算法释放引用（PRD §7.4.4「不得强制复用仍可能被算法访问的缓冲」）。
- 因此：算法**必须**保证 `process` 有界返回。无界等待会永久占用一个帧 slot。

## 9. 错误码

```c
typedef enum {
  AV_OK                     =  0,
  AV_ERR_UNSUPPORTED_API    = -1,   /* 版本不兼容 */
  AV_ERR_INVALID_ARG        = -2,
  AV_ERR_INCOMPATIBLE_FRAME = -3,   /* 能力协商失败 */
  AV_ERR_CONFIG_INVALID     = -4,   /* 配置被整份拒绝，旧配置仍有效 */
  AV_ERR_MODEL_LOAD_FAILED  = -5,
  AV_ERR_INFERENCE_FAILED   = -6,
  AV_ERR_OUT_OF_MEMORY      = -7,
  AV_ERR_NOT_IMPLEMENTED    = -8,   /* 如本期的 face_recognition 通路 */
  AV_ERR_TIMEOUT            = -9,
  AV_ERR_INTERNAL           = -99,
} av_algo_status;
```

返回非 0 后，引擎立即调用 `last_error` 取人类可读描述，写入结构化错误原因返回给用户。`last_error` 的缓冲由引擎提供，算法只填不分配。

## 10. 内存所有权

单一原则：**谁分配谁释放，跨 ABI 的指针只在调用期间有效。**

| 指针 | 有效期 | 释放方 |
| --- | --- | --- |
| `av_algo_library_args` / `instance_args` 及其内部字符串 | 仅调用期间 | 引擎。算法需自行拷贝要长期保存的内容 |
| `frame_ops` / `image_ops` 函数表 | 整个实例生命周期 | 引擎。算法可存进句柄反复使用 |
| `av_frame_desc*` | 仅 `process` 调用期间（除非 `frame_ops->ref`） | 引擎 |
| 帧底层像素缓冲 | 引用计数归零前 | 引擎的缓冲池 |
| `image_ops->alloc` 得到的缓冲 | 算法决定 | **算法**，必须用 `image_ops->free` 释放，且在 `instance_destroy` 返回前全部释放 |
| `av_algo_result` 及其 `json` / `images` | 仅 `on_result` 回调期间 | 算法。引擎在回调内完成拷贝 |
| `last_error` 的 `buf` | 仅调用期间 | 引擎 |

算法**不得**释放任何 `opaque` 平台句柄，也不得长期保存无引用的地址、fd 或私有句柄（PRD §7.4.4）。

## 11. 版本演进

- `AV_ALGO_API_VERSION` 是单调递增整数。
- 所有跨 ABI 结构体带 `size`，新字段**只能追加在末尾**，永不重排、永不删除、永不改变已有字段语义。
- 读取方按 `min(自己认识的 size, 对方给的 size)` 决定能安全访问到哪个字段。
- 破坏性变更必须提升 `api_version`；旧算法包在 `av_algo_get_abi` 里返回 `NULL`，引擎给出「需要升级算法包」的结构化提示，而不是崩溃。
- 结构体布局由 `_Static_assert` 锁定 `sizeof` 与关键 `offsetof`（AC21）。

## 12. 落地进展与仍待闭合的部分

本文定义了加载、初始化、调用、销毁的完整契约。草案写作时留白的几项，当前状态如下。

### 12.1 已落地到 `.trellis/spec/engine/`（以 spec 为准，本文不再重复定义）

| 项目 | 落地位置 |
| --- | --- |
| `manifest.json` 的完整 JSON Schema | [`manifest-schema.md`](../../spec/engine/manifest-schema.md) §1 —— 字段类型约束表、`runtime_constraints`、`resource_tier` 与完整示例 |
| 统一结果 Schema 的确切字段与嵌套形状 | [`manifest-schema.md`](../../spec/engine/manifest-schema.md) §3 —— `event_id` / `algorithm_id` / `objects[]` / `extra` |
| 参数配置受限 Schema 子集的表达方式 | [`manifest-schema.md`](../../spec/engine/manifest-schema.md) §2 —— JSON Schema 真子集白名单 + `x-ui` 扩展元数据 |
| `on_result` 调用栈约束与 `event_id` 职责切分 | [`algo-package-spec.md`](../../spec/engine/algo-package-spec.md) §4.1 |
| `process` 超时与单帧失败的处理语义 | [`algo-package-spec.md`](../../spec/engine/algo-package-spec.md) §4.2 |
| ABI 版本演进与双编译器验证规则 | [`abi-guidelines.md`](../../spec/engine/abi-guidelines.md) §5 |

### 12.2 仍待 Phase 2 冻结前闭合（阻塞 ABI 冻结）

- **`frame_token` 的取得方式** —— `av_frame_desc` 中没有承载引用计数令牌的字段，而 `opaque` 是平台原生句柄（`CVPixelBufferRef` / `dma_buf_fd`），算法被禁止对其做生命周期操作，不能复用为令牌。二选一并写回 SDK 头与 spec：
  - **(a)** 以 `frame_id` 作令牌，`ref`/`unref` 签名改为接收 `uint64_t`，引擎侧哈希反查缓冲池 slot；
  - **(b)** 在描述符末尾追加 `void* frame_token`（`_reserved[3]` 容纳不下指针），同步把 `sizeof` 断言从 144 调整为 152。

  闭合前算法侧不得实现任何异步持帧逻辑。详见 [`abi-guidelines.md`](../../spec/engine/abi-guidelines.md) §4。
- `pixel_format` / `memory_type` / `opaque_kind` 的具体枚举值分配。

### 12.3 仍待实测后冻结（不阻塞 ABI）

- `instance_process` 的墙钟超时默认值。
