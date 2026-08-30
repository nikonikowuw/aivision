#pragma once

#include <stdint.h>
#include <stddef.h>
#include "types.h"

#ifdef __cplusplus
extern "C" {
#endif

#define AV_MAX_RESULT_JSON_BYTES (256 * 1024)

typedef enum av_result_kind {
    AV_RESULT_ALARM = 1,
    AV_RESULT_SELF_TEST = 2,
    AV_RESULT_RECOGNITION = 3
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

AV_STATIC_ASSERT(sizeof(av_algo_image_req) == 32, "image req ABI size");
AV_STATIC_ASSERT(sizeof(av_algo_result) == 48, "result ABI size");

#ifdef __cplusplus
}
#endif
