# Design — C++ Engine 框架与 macOS 运行平台（T1）

## 1. 分层与边界

依赖方向是一个倒 V：`engine → sdk ← algo-packages`。engine 与 algo-packages 之间**没有任何边**。

```
                    sdk/  共享契约（纯 C 头 + cmake helper + 规范文档，零实现零依赖）
                   ╱                                              ╲
        engine 消费它以「加载」插件                  算法包 vendored 它以「实现」插件
                 ╱                                                  ╲
┌──────────────────────────────────────────────┐      algo-packages/<族>/<型号>/<算法>/
│ engine_app  (可执行文件 aivision-engine)      │        自包含工程，可单独 cp 走编译
├──────────────────────────────────────────────┤                    │
│ engine_core   平台无关核心层（静态库）         │                    │ 构建产出 zip
│  · media    统一媒体源 / ZLM / 有界编码帧队列  │                    ▼
│  · frame    通用帧描述符 / 引用计数句柄 / 池   │        安装校验七步 → 解压到
│  · task     摄像头任务 / 算法实例 / 采样调度   │  var/packages/<algorithm_id>/<version>/
│  · algo     算法包运行时 / 安装 / 升级 / 配置  │                    │
│  · image    裁剪缩放编码 / 原子落盘 / 幂等删除 │◄───── dlopen ──────┘
│  · resource 1000 单位资源账本 / 内存阈值       │
│  · telemetry 六项通用指标聚合                 │
│  · ipc      gRPC client+server over UDS       │
├──────────────────────────────────────────────┤
│ platform_api  适配接口（纯头文件 + 注册表）    │
├───────────────────────┬──────────────────────┤
│ platform_macos        │ platform_mock        │
│ VideoToolbox 解码     │ 内存假帧              │
│ CVPixelBuffer 帧      │ malloc 帧             │
│ Core ML 推理          │ 可编程假推理           │
│ vImage 图像处理       │ 纯 CPU memcpy         │
│ ImageIO JPEG          │ 写固定字节             │
│ sysctl/IOKit 指标     │ 可注入指标             │
└───────────────────────┴──────────────────────┘
```

**边界铁律**

1. `engine_core` 的 CMake target 只链接 `platform_api` + gRPC/protobuf + ZLM，不链接任何 `platform_*`。适配层由 `engine_app` 在链接期注入，通过注册表按 `platform_id` 激活。
2. `platform_api` 与 `sdk/` 的公共头中不得出现 `CVPixelBuffer`、`MLModel`、`rknn_*` 等私有类型；平台句柄一律走 `void* opaque` + `uint32_t opaque_kind`。
3. `sdk/` 是纯 C 头文件 + cmake 脚本 + 文档，**零实现、零第三方依赖**，可独立 `tar czf` 分发给第三方算法商，其中不含任何 engine 痕迹。
4. 算法包在**引擎构建之外**编译，vendored SDK 副本，无任何指向仓库其他位置的路径依赖（D7）。

**D2 的直接后果**：ZLM 属于 `engine_core/media`，不属于适配层。RKNN 接入时 media 层不动。

## 2. 目录布局

```
sdk/                                  ★ 共享契约，上游权威副本
├─ include/
│  └─ aivision/
│     ├─ algo.h                       # ★ 核心纯 C ABI（版本化虚表与入口）
│     ├─ types.h                      # ★ 通用帧描述符 (av_frame_desc)、错误码、枚举
│     ├─ result.h                     # ★ 结果 / 清单 Schema 常量
│     │
│     ├─ cv/                          # ★ 跨平台 CV 常用算法工具
│     │  ├─ resize.hpp                #   直接等比/拉伸缩放与坐标映射
│     │  ├─ letterbox.hpp             #   Letterbox 缩放比率计算与反向映射
│     │  └─ nms.hpp                   #   通用高效多类别 NMS / IoU 计算
│     │
│     ├─ utils/                       # ★ 通用开发与测试工具
│     │  ├─ env.hpp                   #   极简轻量 .env 键值解析
│     │  └─ json.hpp                  #   无第三方依赖的紧凑结果 JSON 序列化
│     │
│     └─ platform/                    # ★ 平台专用图像加速与调试工具
│        ├─ macos/                    #   macOS 专用图像处理加速辅助：
│        │  ├─ preprocess.hpp/mm      #     基于 vImage 的极速 NV12/BGRA Resize 与 Letterbox 前处理
│        │  ├─ frame.hpp/mm           #     JPEG 极速转 CVPixelBufferRef NV12 并包装 av_frame_desc
│        │  └─ visualizer.hpp/mm      #     CoreGraphics 高性能画 BBox/打标/保存 result.jpg
│        ├─ rknn/                     #   RKNN 专用辅助（未来扩展：RGA Resize/Letterbox/DRM 帧封装）
│        └─ ascend/                   #   Ascend 专用辅助（未来扩展：DVPP Resize/Letterbox/帧封装）
├─ cmake/AivisionAlgoSDKConfig.cmake
├─ cmake/AivisionAlgoPackage.cmake    #   打包 helper：产出规范 zip
├─ docs/{abi.md,manifest-schema.md,porting-a-package.md}
└─ VERSION                            #   api_version + 各文件 SHA-256

engine/
├─ CMakeLists.txt
├─ Makefile                           # configure/build/test/lint/asan/e2e
├─ cmake/
├─ third_party/ZLMediaKit/            # git submodule
├─ proto/aivision/v1/*.proto
├─ include/aivision/
│  ├─ core/                           # 核心层公共头
│  └─ platform/                       # 适配接口 + 能力档案 + 注册表
├─ src/
│  ├─ core/{media,frame,task,algo,image,resource,telemetry,ipc}/
│  ├─ platform/macos/                 # .mm（Objective-C++）
│  ├─ platform/mock/
│  └─ app/main.cpp
├─ tests/
│  ├─ unit/                           # gtest
│  ├─ contract/                       # 基于 mock 适配器的契约测试
│  ├─ fixtures/packages/              # mock 包 + 两个坏包（构建成 zip 走正常安装）
│  └─ stub_server/                    # 测试用 Go stub gRPC server（独立 go.mod）
└─ docs/{platform-porting.md,toolchain.md}

algo-packages/                        ★ 插件工程，与 engine 无边
├─ README.md                          # 如何单独搬运一个包到目标机器编译
├─ scripts/sync-sdk.sh                # 单向同步：sdk/ → 各包 vendor/（禁止反向）
├─ scripts/check-consistency.sh       # CI：vendor SHA-256 + 路径/清单 platform_id 一致性
└─ macos/arm64/yolov8n/               # ← 整个目录 cp 走即可编译（AC19）
   ├─ Makefile                        #   ★ 标准构建入口（build/run/package/clean，D11）
   ├─ CMakeLists.txt                  #   主 CMake：协调各子模块构建 dylib 与 run_local
   ├─ README.md                       #   单机编译与本地调试说明
   ├─ .env.example                    #   ★ 本地开发调试配置模板（D11/AC23）
   ├─ vendor/aivision-sdk/            #   vendored 副本，自上游 sdk/ 同步
   │  ├─ include/  cmake/  VERSION
   ├─ src/                            #   ★ 模块化源码目录（各模块独立 CMakeLists.txt）
   │  ├─ CMakeLists.txt               #     聚合各子模块静态库，链接输出 libyolov8n.dylib
   │  ├─ preprocess/                  #     [模块 1] 图像预处理与尺寸调整
   │  │  ├─ CMakeLists.txt
   │  │  └─ preprocessor.mm
   │  ├─ inference/                   #     [模块 2] Core ML 模型加载与推理
   │  │  ├─ CMakeLists.txt
   │  │  └─ coreml_backend.mm
   │  ├─ postprocess/                 #     [模块 3] 解码、NMS 与结果过滤
   │  │  ├─ CMakeLists.txt
   │  │  └─ postprocessor.cpp
   │  ├─ core/                        #     [模块 4] 纯 C ABI 虚表与生命周期封装
   │  │  ├─ CMakeLists.txt
   │  │  └─ algo_entry.cpp
   │  └─ runner/                      #     [模块 5] 本地单机调试运行工具 (run_local)
   │     ├─ CMakeLists.txt
   │     └─ standalone_main.cpp
   ├─ conversion/                     #   转换脚本与证据，不进分发包
   │  ├─ export.py  report.md  checksums.txt
   └─ package/                        #   分发包内容 → yolov8n-1.0.0.zip
      ├─ manifest.json                #   platform_id: macos-arm64-coreml
      ├─ lib/libyolov8n.dylib         #   构建产物拷入
      ├─ model/yolov8n.mlpackage
      ├─ config.schema.json
      └─ testimage.jpg
```

未来平台按同样形状扩展，互不影响：`algo-packages/rknn/rk3576/yolov8n/`（`platform_id: rk3576-rknn`）、`algo-packages/ascend/ascend310b/yolov8n/`（`platform_id: ascend310b-cann`）。

**目录与协议的关系（D8）**：`<族>/<型号>` 两层纯粹是给人看的组织方式，不出现在清单、proto、能力档案或任何运行时逻辑里。`platform_id` 的唯一权威是 `package/manifest.json` 的字段。`check-consistency.sh` 断言「路径推导值 == 清单声明值」，防止目录与清单错位导致包被装到错误的板子上。

`engine/tests/stub_server/` 用独立 `go.mod`，与 `app/go.mod` 完全隔离，保证 D5「不修改 Go 产品代码」。

## 3. 关键接口草案

### 3.1 通用帧描述符（`sdk/include/aivision_algo.h`）

字段布局按对齐分块排列，不依赖编译器的 padding 行为——算法包由第三方用他自己的编译器编译，布局必须完全确定。

```c
typedef struct av_frame_desc {
  /* --- 8 字节对齐块 --- */
  uint64_t frame_id;
  uint64_t modifier;             /* DRM format modifier；无此概念的平台填 0 */
  int64_t  wall_time_ns;         /* 业务时间：收帧时的系统时间（§7.1.2） */
  int64_t  pts_ns;               /* 仅诊断，不得用作业务时间（§7.1.2） */
  uint64_t offset[4];            /* 各平面字节偏移 */
  void*    opaque;               /* 平台私有句柄；算法只读，不得释放 */

  /* --- 4 字节块 --- */
  uint32_t size;                 /* = sizeof(av_frame_desc)，兼容扩展依据 */
  uint32_t api_version;
  uint32_t platform_tag;         /* 数值化 platform_id（见下方说明） */
  uint32_t opaque_kind;          /* opaque 的类型标签，决定能否 cast */
  uint32_t memory_type;          /* AV_MEM_HOST / AV_MEM_PLATFORM_SURFACE / ... */
  uint32_t pixel_format;         /* AV_PIX_NV12 / BGRA / I420 ... */
  uint32_t layout;               /* linear / tiled / compressed */
  uint32_t width, height;              /* 有效尺寸 */
  uint32_t alloc_width, alloc_height;  /* 分配尺寸 */
  int32_t  stride[4];            /* 有符号：允许负 stride（自下而上布局） */

  /* --- 小字段块 --- */
  uint16_t color_primaries;      /* BT.601 / BT.709 / BT.2020 */
  uint16_t color_transfer;
  uint16_t color_matrix;
  uint8_t  color_range;          /* limited(16-235) / full(0-255) */
  uint8_t  plane_count;          /* stride/offset 的有效项数 */
  uint8_t  time_synced;          /* §7.1.2 */
  uint8_t  _reserved[3];
} av_frame_desc;

_Static_assert(sizeof(void*) == 8, "64-bit only");
_Static_assert(sizeof(av_frame_desc) == 144, "frame desc ABI frozen");
_Static_assert(offsetof(av_frame_desc, size) == 72, "frame desc ABI frozen");
```

**几处偏离直觉的地方，都是有意的：**

- **`platform_tag` 是数值而非字符串**。帧描述符在热路径上每帧读写，塞一个 32 字节字符串既浪费又诱导算法做字符串比较。而 `platform_id` 在整个进程生命周期内是常量（D8：一台设备只激活一个），完整字符串通过一次性能力查询接口获取。仍满足 §7.4.3「帧描述符包含 platform_id」。
- **`stride` 是 `int32_t`**。负 stride 表示图像自下而上存储，用无符号类型会把 `-4096` 变成 `4294963200` 导致越界。
- **色彩四元组是必需字段，不是可选装饰**。NV12→RGB 时用 BT.601 矩阵解 BT.709 的流、或把 limited range 当 full range 解，出图肉眼「差不多」但送进模型会实打实掉精度，且极难定位（只表现为 mAP 低几个点）。摄像头 1080p 通常是 BT.709 limited，但不能假设。
- **`size` + `api_version` 是版本演进的唯一手段**（§7.5.3）：算法包用旧头编译、引擎传新结构时，算法靠 `size` 判断能安全读到哪个字段。因此新字段只能追加在末尾（吃掉 `_reserved`），永不重排、永不删除。
- **`stride[4]`/`offset[4]` 定长**：实际像素格式最多 3 平面（RGBA=1、NV12=2、I420=3），定长避免跨 ABI 传指针数组和随之而来的所有权问题。
- **`plane_count` 与 `opaque_kind` 是 PRD 没点名但 C ABI 必需的**：否则算法不知道数组读几项、不知道 `void*` 能否 cast。

**色彩信息来源**：优先从 H.264/H.265 码流的 SPS VUI 解析（`colour_primaries` / `transfer_characteristics` / `matrix_coefficients` / `video_full_range_flag`）；VUI 缺失时按 BT.709 limited 兜底，并在运行日志中记录一次「色彩信息缺失，已按 BT.709 limited 处理」。不给用户增加摄像头配置项。

### 3.2 帧句柄生命周期

`FrameHandle` 为 RAII 引用计数包装（core 侧 C++）。跨 ABI 时**描述符纯值传递、不携带函数指针**：引用计数操作通过实例创建时一次性下发的 `av_frame_ops` 函数表暴露。

```c
typedef struct av_frame_ops {
  uint32_t size; uint32_t api_version;
  void (*ref)(void* frame_token);
  void (*unref)(void* frame_token);
} av_frame_ops;
```

算法在 `process` 期间持有帧；若需异步持有，必须显式 `ref`，回调完成后 `unref`。缓冲池在引用归零后才回收 slot；调试构建下回收前用 poison 填充 + 断言检测提前访问（服务 AC16）。

### 3.3 算法包 C ABI（`sdk/include/aivision_algo.h`）

完整契约见同目录 **`algo-package-spec.md`**（加载序列、两级生命周期、初始化参数、能力协商、调用与回调、线程模型、超时、错误码、内存所有权、版本演进）。此处只记要点：

- 动态库**只导出一个符号** `av_algo_get_abi(uint32_t requested_api_version)`，返回静态存储期的 `av_algo_abi` 函数表；不兼容时返回 `NULL`。
- **两级生命周期**：Library 级（每个包版本一次，加载模型等只读共享资源）+ Instance 级（每个算法实例一份，独立推理上下文与跟踪/聚合状态）。这是 PRD §7.7「复用只读模型资源、并发工作单元用独立上下文」的直接落地。
- `dlopen` 必须用 `RTLD_NOW | RTLD_LOCAL`——多个算法包可能各自静链不同版本的同名第三方库，`RTLD_GLOBAL` 会造成符号劫持，症状是随机崩溃且极难归因。
- **`process` 是完整的一帧处理**（前处理 + 推理 + 后处理 + 跟踪聚合）。引擎不介入算法的内部阶段划分，只通过 `av_image_ops` 提供平台加速的图像原语（convert / pad / alloc / free），算法自行组合。引擎提供机制，不定义策略。
- 结果经回调返回 JSON 字节串；算法只提交归一化 ROI 的**裁图请求**，裁剪/编码/落盘/图片 ID 全由��擎 image 模块完成，算法不碰文件系统、不链接图像库。
- 线程模型：同一实例严格串行（含 `update_config` 与 `process` 互斥），实例间可并发。算法内部无需同步代码。
- 超时不强杀：引擎停止分发新帧并标记实例，已被算法持有的帧缓冲在引用归零前不回收。

```c
#define AV_ALGO_API_VERSION 1

const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version);

typedef struct av_algo_abi {
  uint32_t size; uint32_t api_version;
  /* 库级：每个包版本一次 */
  int (*library_open)(const av_algo_library_args*, av_algo_library*);
  int (*library_query)(av_algo_library, av_algo_library_info*);
  int (*library_close)(av_algo_library);
  /* 实例级：每个算法实例一份 */
  int (*instance_create)(av_algo_library, const av_algo_instance_args*, av_algo_instance*);
  int (*instance_negotiate)(av_algo_instance, const av_frame_caps*, av_frame_caps*);
  int (*instance_update_config)(av_algo_instance, const char* json, uint32_t len);
  int (*instance_process)(av_algo_instance, const av_frame_desc*);
  int (*instance_flush)(av_algo_instance);
  int (*instance_destroy)(av_algo_instance);
  /* 通用 */
  int (*last_error)(av_algo_instance /* 可为 NULL */, char* buf, uint32_t cap);
} av_algo_abi;
```

结果回调 `on_result` 在 `instance_args` 中一次性下发，`process` 期间可触发零次、一次或多次（大多数帧无告警；聚合完成的帧可能同时吐出多个事件）。回调载荷为纯检测目标数组 JSON 字节串（包含 `alarm_type_id`、`label`、`confidence`、`bbox` 归一化坐标及可选扩展属性）+ 归一化 ROI 裁图请求，避免跨 ABI 传结构化容器。

**职责清晰切分**：

- **算法包**：只负责模型推理与视觉目标输出，不生成 `event_id`，不回填 `timestamp_ns`，不统计耗时性能；
- **Engine 宿主**：在接收回调后，统一绑定帧原生纳秒时间戳（`wall_time_ns`）、自动生成全局规范的 `event_id`、测量 P99 推理性能耗时、执行图像裁剪落盘并封装 gRPC 消息上报。

### 3.4 适配接口（`platform_api`）

```
IPlatform         : id(), profile(), create_*()
IDecoder          : open(codec, extradata) / decode(pkt) -> FrameHandle
IFrameAllocator   : 池化分配 / 引用计数回调
IInferenceContext : load(model_path) / run(inputs) / query_memory()
IImageProcessor   : crop / resize / convert
IImageEncoder     : encode_jpeg(frame, roi, quality) -> bytes
IResourceProvider : total_units() / allocatable_units() / reserved_units() / min_free_memory()
ITelemetry        : sample() -> 六项指标 + availability
```

能力档案 `PlatformProfile` 为版本化 struct + JSON 序列化，每项能力带 `Availability{available, degraded, unsupported, reason}`。

## 4. 数据流与调度

```
ZLM MediaSource ──(H264/H265 NAL 引用)──> 有界编码帧队列(每摄像头)
        │  回调内只做入队                       │
        └──> 预览会话复用同一 MediaSource        ▼
                                        Decoder(平台) ──> FrameHandle(共享)
                                                            │
                                        ┌───────────────────┴───────────────────┐
                                        ▼ 采样器A(FPS_a)                        ▼ 采样器B(FPS_b)
                                   有界帧队列A(丢旧留新)                    有界帧队列B
                                        ▼ 工作线程A（独占）                     ▼ 工作线程B（独占）
                                          algo.instance_process()
                                     （前处理 + 推理 + 后处理 + 跟踪聚合）
                                       算法可调用 av_image_ops 走硬件加速
                                                    ▼ 结果 JSON + 裁图请求
                        event_id 幂等去重 ──> ImageProcessor 裁剪/编码
                                                    ▼ tmp 写入 + 原子 rename
                                             图片 ID + 相对路径
                                                    ▼
                                          gRPC ReportAlarm ──> Go stub server
```

**调度模型：每算法实例一个独占工作线程。**

算法包的整个契约建立在「同一实例的 `process` 不会重入」之上——它因此不写任何同步代码。独占线程让这个保证是**结构性**的：一个线程天然串行，不可能出错。换成共享线程池的话，同一保证要靠调度器里「取任务时打实例占用标记」的逻辑正确，一旦有 bug 就是第三方算法包内部的数据竞争，而你没有它的源码。用��能出错的机制去保障绝不能错的契约，不划算。

每摄像头一个解码线程。所有队列有界，解码线程永不阻塞在算法队列上（`try_push` 失败即丢最旧帧）——这是 AC5 的实现依据。

**多重防死锁与静默挂起自愈设计（针对 VPU/驱动无响应）**：
1. **双层心跳看门狗**：
   - `Ingest Watchdog`：连续 5s 无任何 NAL 到达，主动切断 Socket 触发重连；
   - `Decoder Watchdog`：队列有包但连续 3s 未能输出解码帧，判定硬件解码器静默死锁，强制销毁并重建 `DecoderSession`。
2. **IDR 关键帧准入闸门**：启动与重连时严格丢弃残损 P 帧，直到抓到完整 SPS/PPS + IDR 帧才开始喂给硬件解码器，杜绝因坏帧引发底层硬件寄存器锁死。
3. **解码器易耗品模式**：解码会话生命周期与上层算法解耦，死锁时原地注销老 Session 并新建，向摄像头发送 `PLI/FIR` 关键帧请求，1 秒内完成自愈且算法实例无感。

共享硬件（RGA / vImage / 推理加速器）的争用在**原语与推理上下文的实现层**排队，引擎不需要理解算法的内部阶段划分。

**关于线程数**：16 路 × 2 算法 = 32 线程，看似与核数脱节，但这些线程绝大部分时间阻塞在帧队列或推理返回上，真正 CPU 密集的只有前后处理两小段。即使同时进入密集段，结果是单帧延迟方差变大而总��吐基本不变，而 PRD §10.3 明确「优先保证实时性，不保证处理每一个采样帧」——这是吞吐型负载，延迟方差不是要害。

**调度模型是引擎内部实现，不是 ABI 契约**。ABI 只承诺「同实例串行」，靠独占线程还是占用标记实现，算法包完全无感。因此这个决定不需要提前做：将来实测证明线程数确实是瓶颈，换成共享线程池即可，不影响 ABI、不影响算法包、无迁移成本。为此代码结构上要求「帧分发」与「执行」解耦——`process` 不得硬编码在解码回调里，届时只需替换执行器。

**同理，不做单实例内的阶段流水线**：把 `process` 拆成无状态 `detect` + 有状态 `commit`、配 reorder buffer 做三段流水，其收益只在整��跑 1~2 个实例时存在；本产品场景是 4~16 路（PRD §10.1），实例间并发已把加速器填满。代价却是 reorder buffer、跨段背压、帧生命周期延长，以及要求第三方算法作者正确切分有状态/无状态逻辑。若将来实测证明必要，可在 `av_algo_abi` 末尾追加这两个函数指针，老包因 `size` 较小自动走串行路径，零破坏。

## 5. gRPC 契约（`proto/aivision/v1/`）

- `engine.proto`：`EngineService`（对端 → C++）
  `ApplyDesiredState`（全量对账）、`UpsertTask`、`SetInstanceState`、`UpdateInstanceConfig`、`InstallPackage`、`UpgradePackage`、`RollbackPackage`、`UninstallPackage`、`DeleteImages`、`QueryProfile`、`QueryMetrics`。
- `report.proto`：`ReportService`（C++ → 对端）
  `ReportAlarm`、`ReportTaskState`、`ReportInstanceState`、`ReportMetrics`、`ReportOrphanImages`。
- `person.proto`：**预留**（D4）`PersonService` / `FeatureService` 的 service 与 message 定义，C++ 侧注册但返回 `UNIMPLEMENTED`。

传输：`unix:///var/run/aivision/engine.sock`（macOS 开发期落到 `./run/`），双向各起一个 UDS 端点。断线检测走 gRPC channel 状态；重连成功后 C++ 主动请求 `ApplyDesiredState` 做全量对账（AC13）。

**约束**：任何 message 都不含像素数据。图片只传 `image_id` + 受限相对路径。

## 6. 构建与依赖

**引擎**

- CMake ≥3.24、C++20、Objective-C++ 用于 `platform/macos/*.mm`。
- Homebrew：`grpc`、`protobuf`、`googletest`、`nlohmann-json`（结果/清单 JSON）、`cmake`、`ninja`。
- ZLMediaKit：git submodule，`add_subdirectory` 静态链接，关闭其 webrtc/ffmpeg 可选项以缩小依赖面。
- 系统框架：`CoreML`、`VideoToolbox`、`CoreVideo`、`Accelerate`、`ImageIO`、`IOKit`（仅 `platform_macos` 链接）。
- Sanitizer：`make asan`（AC16）。

**算法包（刻意保持极简，服务 AC19 的可搬运性）**

- SDK 只需 CMake ≥3.16 + 平台编译器 + 平台运行时（macOS 包只用系统框架 CoreML/CoreVideo；RKNN 包只需板上 rknn runtime）。
- 算法包工程内建标准 `Makefile`（提供 `configure`、`build`、`run`、`benchmark`、`asan`、`package`、`clean`）与 `standalone_runner`（编译产出 `run_local` 可执行文件），支持加载同目录下的 `.env` 文件或接收环境变量覆盖（如 `CONF_THRESH=0.8 make run`，`LOOPS=500 make benchmark`）。
- **开箱即用开发体验**：算法开发直接包含 SDK 提供的标准工具头（如 `<aivision/cv/resize.hpp>`、`<aivision/cv/letterbox.hpp>`、`<aivision/cv/nms.hpp>`、`<aivision/utils/env.hpp>`、`<aivision/utils/profiler.hpp>` 以及平台加速的 `<aivision/platform/macos/preprocess.hpp>` 和 `<aivision/platform/macos/visualizer.hpp>`），几行代码即可完成标准输入预处理、分段性能 Profiling 打点与结果后处理，极大降低新算法开发门槛，彻底避免重复编写样板代码。
- 不依赖 gRPC / protobuf / nlohmann-json / gtest —— 结果 JSON 用手写序列化或包内 vendored 单头库，避免把引擎的依赖链传染给第三方算法商。
- `vendor/aivision-sdk/cmake/AivisionAlgoPackage.cmake` 提供 `aivision_add_algo_package()`，负责拷贝产物进 `package/`、校验 manifest、打 zip。

**一致性检查（AC2 / AC20）**

- AC2：对 `engine_core` 静态库跑 `nm -u` 断言不含 `_CVPixelBuffer*` / `_MLModel*` 符号；对 `engine/include/aivision/{core,platform}` 与 `sdk/include` 跑 grep 断言不含私有类型名。
- AC20-a：逐包比对 `vendor/aivision-sdk/` 与上游 `sdk/` 的 SHA-256（D10 单向同步）。
- AC20-b：逐包比对「路径 `<族>/<型号>` 推导出的 platform_id」与 `package/manifest.json` 声明值（D8）。
- AC19：CI 中 `cp -r algo-packages/macos/arm64/yolov8n /tmp/portable-check && cd /tmp/portable-check && cmake -B build && cmake --build build`，随后 `otool -L` 断言无 engine 库。

## 7. 重要权衡

| 决策 | 取舍 |
| --- | --- |
| 算法结果用 JSON 而非二进制结构体跨 ABI | 牺牲少量序列化开销，换取版本兼容与 Schema 统一（PRD §7.5.3 禁止跨 ABI 传容器）；检测框数量级小，开销可忽略 |
| ZLM 放核心层而非适配层 | 减少两个平台的重复实现与预览协议分歧；代价是核心层多一个第三方依赖，若未来某平台无法用 ZLM 需再抽象一层 |
| 每实例一个独占工作线程 | 串行契约由线程独占结构性保证，不依赖调度逻辑正确性——算法包不写同步代码正是建立在这个保证上；代价是线程数随实例数增长，但这些线程绝大部分时间阻塞，且调度模型不是 ABI 契约，将来可无损换成共享线程池 |
| 不拆 `detect`/`commit` 阶段流水线 | 单实例阶段重叠的收益在 4~16 路场景下接近零，而 reorder buffer、跨段背压、第三方作者切分状态的出错风险都是实打实的；`size` 字段保证将来可无损追加 |
| 前处理留在算法内，引擎只给加速原语 | 每个算法前处理本就不一致（多输入、时序堆叠、两阶段裁剪、tiling），声明式描述覆盖不全必须留 custom 兜底；既然兜底路径必存，声明式层的增量价值不抵复杂度 |
| Core ML 而非 ONNX Runtime | ANE 加速、macOS 原生；代价是多一条模型转换证据链（D3 已接受） |
| Go stub server 独立 go.mod | 严守 D5 不污染 `app/`；代价是 proto 生成产物要生成两份（C++ + stub 的 Go） |
| SDK vendored 进每个算法包 | 换来单目录可搬运（D7/AC19）——这是给开发板场景的刚需；代价是 N 份副本会漂移，用 D10 单向同步 + CI SHA 校验兜住 |
| 型号间不共享算法代码 | 型号差异真实存在，且共享层会立刻破坏可搬运性；代价是同一算法跨型号有重复代码，属插件生态正常成本 |
| 目录层级不进协议 | 目录可随时重组而不动协议；代价是多一条「路径 == 清单」的 CI 断言 |

## 8. 回滚与风险控制

- ZLM spike 是第一个风险闸门（`implement.md` M0 步骤 2）。若 macOS 构建受阻，退路是自研最小 RTSP/RTP 客户端仅供开发期使用，`media` 层接口不变，不影响其余步骤。
- 每个里程碑独立可验证。`sdk/`、`engine/`、`algo-packages/` 均为全新顶层目录，任何时刻回滚都不影响 `app/` 与 `ui/`。
- 坏包场景（AC10）通过 `engine/tests/fixtures/packages/` 里专门构造的 fixture 实现，同样走「构建 zip → 正常安装流程」，不污染示例包、也不走特权加载路径。
- `sdk/` 的 ABI 在 M2 末冻结并评审；冻结前 `algo-packages/` 内的包靠 sync 脚本被动跟随，不承担兼容责任。
