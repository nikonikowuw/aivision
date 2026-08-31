# C ABI 与帧生命周期规范

> 本规范是宿主与算法包之间的二进制契约。ABI v1 冻结前，任何字段调整必须同时更新 SDK 头、静态断言、双编译器测试和 vendored SDK。

## 1. Scope / Trigger

修改 `sdk/include/argus/*.h`、帧池、跨 ABI 回调、能力枚举或结构体版本时必须读取本规范。ABI 头必须兼容 C11 与 C++20，跨边界不得传 STL、Objective-C 对象、C++ 异常或所有权不明的指针。

## 2. 基础签名与版本规则

所有跨 ABI 结构体的前 8 字节固定为：

```c
uint32_t size;
uint32_t api_version;
```

通用规则：

1. 输入结构体的 `size` 表示调用方实际提供的字节数；小于该 API 的最小尺寸返回 `AV_ERR_INVALID_ARG`。
2. 输出结构体由调用方先清零并设置 `size=sizeof(type)`；被调用方最多写入该容量，并保留调用方的 `size`。
3. 新字段只能追加到末尾；读取前使用 `size >= offsetof(type, field) + sizeof(field)`。
4. 已有字段的类型、偏移和语义不得修改。无法追加兼容的变更必须提升 `api_version`。
5. ABI 函数入口必须捕获所有 C++/Objective-C++ 异常并转换为 `av_algo_status`。

兼容 C/C++ 的断言宏：

```c
#if defined(__cplusplus)
#define AV_STATIC_ASSERT(cond, msg) static_assert((cond), msg)
#else
#define AV_STATIC_ASSERT(cond, msg) _Static_assert((cond), msg)
#endif
```

## 3. `av_frame_desc` v1 布局

帧描述符按值读取，不携带函数指针。v1 仅支持 64 位目标，固定为 152 字节：

```c
typedef struct av_frame_desc {
  uint32_t size;                 /* 0 */
  uint32_t api_version;          /* 4 */

  uint64_t frame_id;             /* 8 */
  int64_t  wall_time_ns;         /* 16 */
  int64_t  pts_ns;               /* 24 */
  uint64_t modifier;             /* 32 */
  uint64_t offset[4];            /* 40 */
  void*    opaque;               /* 72: 平台原生句柄，只读 */
  void*    frame_token;          /* 80: 只可传给 av_frame_ops */

  uint32_t platform_tag;         /* 88 */
  uint32_t opaque_kind;          /* 92 */
  uint32_t memory_type;          /* 96 */
  uint32_t pixel_format;         /* 100 */
  uint32_t layout;               /* 104 */
  uint32_t width;                /* 108 */
  uint32_t height;               /* 112 */
  uint32_t alloc_width;          /* 116 */
  uint32_t alloc_height;         /* 120 */
  int32_t  stride[4];            /* 124 */

  uint16_t color_primaries;      /* 140 */
  uint16_t color_transfer;       /* 142 */
  uint16_t color_matrix;         /* 144 */
  uint8_t  color_range;          /* 146 */
  uint8_t  plane_count;          /* 147 */
  uint8_t  time_synced;          /* 148 */
  uint8_t  reserved[3];          /* 149 */
} av_frame_desc;

AV_STATIC_ASSERT(sizeof(void*) == 8, "64-bit ABI required");
AV_STATIC_ASSERT(sizeof(av_frame_desc) == 152, "frame ABI size");
AV_STATIC_ASSERT(offsetof(av_frame_desc, frame_token) == 80, "frame token offset");
AV_STATIC_ASSERT(offsetof(av_frame_desc, stride) == 124, "stride offset");
AV_STATIC_ASSERT(offsetof(av_frame_desc, color_primaries) == 140, "color offset");
```

`frame_token` 与 `opaque` 职责不同：

- `opaque` 是 `CVPixelBufferRef`、DMA-BUF 包装等平台句柄；算法可按协商后的 `opaque_kind` 只读访问，禁止释放。
- `frame_token` 是宿主引用计数令牌；算法禁止解引用、比较其内部布局或跨实例使用。
- `frame_id` 是业务/诊断标识，不是引用计数令牌，禁止用哈希反查代替 `frame_token`，以免产生 ABA 和清理竞态。

## 4. 图像视图与帧引用签名

跨 ABI 的矩形和目标图像视图固定为：

```c
typedef struct av_rect {
  uint32_t size;
  uint32_t api_version;
  float x;
  float y;
  float width;
  float height;
} av_rect;

typedef struct av_image_view {
  uint32_t size;
  uint32_t api_version;
  uint32_t width;
  uint32_t height;
  uint32_t pixel_format;
  uint32_t memory_type;
  uint32_t plane_count;
  uint32_t opaque_kind;
  int32_t stride[4];
  uint64_t offset[4];
  void* data;                   /* host base address；平台 surface 可为 NULL */
  void* opaque;                 /* 平台句柄；只由配套 image_ops 解释 */
} av_image_view;

AV_STATIC_ASSERT(sizeof(av_rect) == 24, "rect ABI size");
AV_STATIC_ASSERT(sizeof(av_image_view) == 96, "image view ABI size");
```

ROI 坐标是否归一化由具体 API 规定；`av_image_ops.convert` 的 `src_roi` 使用 `[0,1]` 坐标。`av_image_view` 由 `image_ops.alloc` 填充并只能交给同一 ops/context 的 `free`，调用方不得自行释放 `data/opaque`。

帧引用函数表固定为：

```c
typedef struct av_frame_ops {
  uint32_t size;
  uint32_t api_version;
  void* ctx;
  int (*retain)(void* ctx, void* frame_token);
  int (*release)(void* ctx, void* frame_token);
} av_frame_ops;
```

生命周期契约：

1. 宿主在 `instance_process` 调用期间持有一个引用，描述符及像素数据至少有效到调用返回。
2. 算法若要在调用返回后继续持有像素，必须在返回前成功调用 `retain`；每次成功 `retain` 对应一次 `release`。
3. `instance_flush` 和 `instance_destroy` 返回前，算法必须释放全部额外引用并停止访问宿主资源。
4. `retain/release` 接收到空、过期或非本实例 token 时返回 `AV_ERR_INVALID_ARG`；调试构建同时记录结构化错误。
5. 引用归零前帧池不得复用底层 slot；归零后调试构建 poison 可写内存并增加 generation，用于检测 UAF/ABA。

## 5. 固定枚举值

v1 的数值一经发布不得复用：

```c
typedef enum av_memory_type {
  AV_MEM_UNKNOWN = 0,
  AV_MEM_HOST = 1,
  AV_MEM_PLATFORM_SURFACE = 2
} av_memory_type;

typedef enum av_pixel_format {
  AV_PIX_UNKNOWN = 0,
  AV_PIX_NV12 = 1,
  AV_PIX_BGRA = 2,
  AV_PIX_RGB24 = 3,
  AV_PIX_I420 = 4
} av_pixel_format;

typedef enum av_image_layout {
  AV_LAYOUT_UNKNOWN = 0,
  AV_LAYOUT_LINEAR = 1,
  AV_LAYOUT_PLATFORM_NATIVE = 2
} av_image_layout;

typedef enum av_opaque_kind {
  AV_OPAQUE_NONE = 0,
  AV_OPAQUE_CVPIXELBUFFER = 0x1001,
  AV_OPAQUE_DMABUF = 0x2001
} av_opaque_kind;
```

平台专用新值按平台注册段追加，不得修改公共值。未知枚举必须被能力协商拒绝，不能默认为 host/linear。

## 6. 色彩与平面契约

- H.264/H.265 优先读取 SPS VUI 的 primaries、transfer、matrix、range；缺失时使用 BT.709 limited，并且每路流只记录一次降级日志。
- NV12 必须满足 `plane_count=2`；BGRA/RGB24 为 1；I420 为 3。有效项之外的 `offset/stride` 必须清零。
- `stride` 为有符号值；访问前必须验证绝对值、分配尺寸和 plane 边界，禁止转成无符号后计算。
- 所有 YUV 到 RGB 转换必须使用描述符的矩阵和 range，禁止硬编码 BT.601/full range。

## 7. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| `size` 小于 v1 最小尺寸 | `AV_ERR_INVALID_ARG` |
| `api_version` 不受支持 | `AV_ERR_UNSUPPORTED_API` |
| `plane_count` 与像素格式不匹配 | `AV_ERR_INVALID_ARG` |
| 能力协商未接受 `opaque_kind` | `AV_ERR_INCOMPATIBLE_FRAME` |
| token 为空、过期或属于其他实例 | `AV_ERR_INVALID_ARG` |
| VUI 缺失 | 成功，使用 BT.709 limited，并记录一次降级 |
| 算法释放 `opaque` | 契约违规；ASan/契约测试必须失败 |

## 8. Good / Base / Bad Cases

- Good：算法异步持帧前调用 `retain(ctx, frame_token)`，flush 时 release。
- Base：同步算法只在 `instance_process` 栈内读取帧，不调用 frame ops。
- Bad：把 `opaque` 当作 token，或直接调用 `CVPixelBufferRelease`。

## 9. Tests Required

- C11、AppleClang C++20、aarch64 GCC 编译和全部 `sizeof/offsetof` 断言。
- 用较小旧版 `size` 解析已知前缀，证明不读取尾部字段。
- retain/release、过期 token、跨实例 token、双 release、归零后复用测试。
- ASan/LSan/UBSan 覆盖帧池；TSan 覆盖多实例 retain/release。
- BT.709 limited、有 VUI/无 VUI、负 stride、各 plane 边界像素测试。

## 10. Wrong vs Correct

```c
/* Wrong: opaque 的所有权属于宿主 */
CVPixelBufferRelease((CVPixelBufferRef)frame->opaque);

/* Correct: 需要跨调用持有时只操作 frame_token */
if (ops->retain(ops->ctx, frame->frame_token) != AV_OK) {
  return AV_ERR_INVALID_ARG;
}
```
