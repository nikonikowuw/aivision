#pragma once

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

#if defined(__cplusplus)
#define AV_STATIC_ASSERT(cond, msg) static_assert((cond), msg)
#else
#define AV_STATIC_ASSERT(cond, msg) _Static_assert((cond), msg)
#endif

// ABI API Version: v1
#define AV_ALGO_API_VERSION 1u

// Error codes / Status
// 注意：AV_ERR_RETRY 仅用于 Engine 平台层（如 IDecoder.receive_frame 的
// “暂无可用帧，稍后重试”信号），不属于算法包 ABI 的错误矩阵；算法包不得返回它。
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
    AV_ERR_RETRY = -10,
    AV_ERR_INTERNAL = -99
} av_algo_status;

// Alias for engine/sdk compatibility
typedef av_algo_status av_status;

// Pixel format enum
typedef enum av_pixel_format {
    AV_PIX_UNKNOWN = 0,
    AV_PIX_NV12 = 1,
    AV_PIX_BGRA = 2,
    AV_PIX_RGB24 = 3,
    AV_PIX_I420 = 4
} av_pixel_format;

// Memory type enum
typedef enum av_memory_type {
    AV_MEM_UNKNOWN = 0,
    AV_MEM_HOST = 1,
    AV_MEM_PLATFORM_SURFACE = 2
} av_memory_type;

// Layout enum
typedef enum av_image_layout {
    AV_LAYOUT_UNKNOWN = 0,
    AV_LAYOUT_LINEAR = 1,
    AV_LAYOUT_PLATFORM_NATIVE = 2
} av_image_layout;

// Opaque kind
typedef enum av_opaque_kind {
    AV_OPAQUE_NONE = 0,
    AV_OPAQUE_CVPIXELBUFFER = 0x1001,
    AV_OPAQUE_DMABUF = 0x2001
} av_opaque_kind;

// Color description
typedef enum av_color_primaries {
    AV_COLOR_PRIM_UNSPECIFIED = 0,
    AV_COLOR_PRIM_BT709 = 1,
    AV_COLOR_PRIM_BT2020 = 2,
} av_color_primaries;

typedef enum av_color_transfer {
    AV_COLOR_TRC_UNSPECIFIED = 0,
    AV_COLOR_TRC_BT709 = 1,
    AV_COLOR_TRC_IEC61966_2_1 = 2,
} av_color_transfer;

typedef enum av_color_matrix {
    AV_COLOR_MAT_UNSPECIFIED = 0,
    AV_COLOR_MAT_BT709 = 1,
    AV_COLOR_MAT_BT2020_NCL = 2,
} av_color_matrix;

typedef enum av_color_range {
    AV_COLOR_RANGE_UNSPECIFIED = 0,
    AV_COLOR_RANGE_LIMITED = 1,
    AV_COLOR_RANGE_FULL = 2,
} av_color_range;

// Universal frame descriptor (152 Bytes fixed layout in 64-bit ABI)
#pragma pack(push, 8)
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
#pragma pack(pop)

AV_STATIC_ASSERT(sizeof(void*) == 8, "64-bit ABI required");
AV_STATIC_ASSERT(sizeof(av_frame_desc) == 152, "frame ABI size");
AV_STATIC_ASSERT(offsetof(av_frame_desc, frame_token) == 80, "frame token offset");
AV_STATIC_ASSERT(offsetof(av_frame_desc, stride) == 124, "stride offset");
AV_STATIC_ASSERT(offsetof(av_frame_desc, color_primaries) == 140, "color offset");

// Frame lifetime management function table
typedef struct av_frame_ops {
    uint32_t size;
    uint32_t api_version;
    void* ctx;
    int (*retain)(void* ctx, void* frame_token);
    int (*release)(void* ctx, void* frame_token);
} av_frame_ops;

AV_STATIC_ASSERT(sizeof(av_frame_ops) == 32, "frame ops ABI size");

// Rect
typedef struct av_rect {
    uint32_t size;
    uint32_t api_version;
    float x;
    float y;
    float width;
    float height;
} av_rect;

AV_STATIC_ASSERT(sizeof(av_rect) == 24, "rect ABI size");

// Image view
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
    void* data;                   /* host base address */
    void* opaque;                 /* 平台句柄 */
} av_image_view;

AV_STATIC_ASSERT(sizeof(av_image_view) == 96, "image view ABI size");
AV_STATIC_ASSERT(offsetof(av_image_view, data) == 80, "image view data offset");

// Platform accelerated image processing operations
typedef struct av_image_ops {
    uint32_t size;
    uint32_t api_version;
    void* ctx;
    int (*convert)(void* ctx, const av_frame_desc* src,
                   const av_rect* src_roi, const av_image_view* dst,
                   uint32_t filter);
    int (*pad)(void* ctx, const av_image_view* dst,
               const av_rect* region, const uint8_t value[4]);
    int (*alloc)(void* ctx, uint32_t width, uint32_t height,
                 uint32_t pixel_format, av_image_view* out);
    int (*free)(void* ctx, av_image_view* image);
} av_image_ops;

AV_STATIC_ASSERT(sizeof(av_image_ops) == 48, "image ops ABI size");

// Detection Rule Definitions (ROI / Mask / Line)
typedef struct av_point {
    float x;
    float y;
} av_point;

typedef enum av_rule_role {
    AV_RULE_ROI = 1,
    AV_RULE_MASK = 2,
    AV_RULE_LINE = 3
} av_rule_role;

typedef enum av_line_dir {
    AV_LINE_DIR_BOTH = 0,
    AV_LINE_DIR_A_TO_B = 1,
    AV_LINE_DIR_B_TO_A = 2
} av_line_dir;

typedef struct av_rule {
    uint32_t size;
    uint32_t api_version;
    uint32_t role;
    uint32_t mode;
    uint32_t point_count;
    const av_point* points;
    uint32_t reserved0;
} av_rule;

AV_STATIC_ASSERT(sizeof(av_rule) == 40, "rule ABI size");

#ifdef __cplusplus
}
#endif
