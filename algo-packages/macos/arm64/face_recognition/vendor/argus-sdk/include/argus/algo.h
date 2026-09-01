#pragma once

#include "types.h"
#include "result.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef void* av_algo_library;
typedef void* av_algo_instance;

/**
 * @brief SDK 日志级别常量 (兼容 av_log_fn 的 int level 传参)
 */
typedef enum av_algo_log_level {
    AV_ALGO_LOG_TRACE = 0,
    AV_ALGO_LOG_DEBUG = 1,
    AV_ALGO_LOG_INFO  = 2,
    AV_ALGO_LOG_WARN  = 3,
    AV_ALGO_LOG_ERROR = 4,
    AV_ALGO_LOG_FATAL = 5
} av_algo_log_level;

typedef void (*av_log_fn)(void* user, int level, const char* msg, uint32_t len);

typedef struct av_algo_library_args {
    uint32_t size;
    uint32_t api_version;
    const char* package_root;
    const char* platform_id;
    uint32_t platform_tag;
    av_log_fn log;
    void* log_user;
} av_algo_library_args;

AV_STATIC_ASSERT(sizeof(av_algo_library_args) == 48, "library args ABI size");

typedef struct av_algo_library_info {
    uint32_t size;
    uint32_t api_version;
    char algorithm_id[64];
    char version[32];
    char algorithm_type[32];
    char alarm_type_id[64];
} av_algo_library_info;

AV_STATIC_ASSERT(sizeof(av_algo_library_info) == 200, "library info ABI size");

typedef enum av_instance_mode {
    AV_INSTANCE_NORMAL = 1,
    AV_INSTANCE_INSTALL_SELF_TEST = 2
} av_instance_mode;

typedef void (*av_algo_result_cb)(const av_algo_result* result, void* user_data);

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
    const av_rule* rules;           /* 检测规则（ROI/Mask/分界线） */
    uint32_t rule_count;
} av_algo_instance_args;

AV_STATIC_ASSERT(sizeof(av_algo_instance_args) == 96, "instance args ABI size");

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

AV_STATIC_ASSERT(sizeof(av_frame_caps) == 84, "frame caps ABI size");

// C ABI Virtual Function Table
typedef struct av_algo_abi {
    uint32_t size;
    uint32_t api_version;

    int (*library_open)(const av_algo_library_args* args, av_algo_library* out);
    int (*library_query)(av_algo_library lib, av_algo_library_info* out);
    int (*library_close)(av_algo_library lib);

    int (*instance_create)(av_algo_library lib, const av_algo_instance_args* args, av_algo_instance* out);
    int (*instance_negotiate)(av_algo_instance inst, const av_frame_caps* offered, av_frame_caps* accepted);
    int (*instance_update_config)(av_algo_instance inst, const char* json, uint32_t len);
    int (*instance_set_rules)(av_algo_instance inst, const av_rule* rules, uint32_t count);
    int (*instance_process)(av_algo_instance inst, const av_frame_desc* frame);
    int (*instance_flush)(av_algo_instance inst);
    int (*instance_destroy)(av_algo_instance inst);

    int (*last_error)(av_algo_instance inst_or_null, char* buf, uint32_t cap);
} av_algo_abi;

AV_STATIC_ASSERT(sizeof(av_algo_abi) == 96, "algo ABI table size");

// Exported dynamic library symbol
#define AV_ALGO_GET_ABI_SYMBOL "av_algo_get_abi"
#define AV_ALGO_EXTRACT_FACE_SYMBOL "av_algo_extract_face"

/* 一次性静态图片单人脸特征提取扩展定义 */
typedef struct av_face_extract_input {
    uint32_t size;
    uint32_t api_version;
    const uint8_t* image_bytes;
    uint32_t image_bytes_len;
    float min_detection_score;
    float min_face_size;
    float min_quality_score;
    uint32_t reserved;
} av_face_extract_input;

AV_STATIC_ASSERT(sizeof(av_face_extract_input) == 40, "face extract input ABI size");

typedef struct av_face_extract_output {
    uint32_t size;
    uint32_t api_version;
    uint32_t status_code;        /* 0: OK, 1: NO_FACE, 2: MULTI_FACE, 3: QUALITY_LOW, 4: DECODE_ERR, 5: TOO_LARGE, 6: TOO_SMALL, 7: INTERNAL_ERR */
    uint32_t reserved;
    char error_message[256];
    float embedding[512];
    uint32_t embedding_dim;
    float bbox[4];               /* x, y, w, h in [0, 1] */
    float quality_score;
    float detection_score;
    uint8_t aligned_jpeg_data[65536]; /* 112x112 JPEG 字节流缓冲区 */
    uint32_t aligned_jpeg_len;
    uint32_t reserved1;
} av_face_extract_output;

AV_STATIC_ASSERT(sizeof(av_face_extract_output) == 67892, "face extract output ABI size");

typedef int (*av_algo_extract_face_fn)(av_algo_library lib, const av_face_extract_input* in, av_face_extract_output* out);

#if defined(_WIN32)
#define AV_EXPORT __declspec(dllexport)
#else
#define AV_EXPORT __attribute__((visibility("default")))
#endif

AV_EXPORT const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version);

// Function-pointer typedef for dlopen/dlsym callers (Engine loader & validator).
typedef const av_algo_abi* (*av_algo_get_abi_fn)(uint32_t requested_api_version);

#ifdef __cplusplus
}
#endif
