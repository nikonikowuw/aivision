#include "argus/algo.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct mock_face_instance_state {
    uint32_t mode;
    av_algo_result_cb on_result;
    void* result_user;
} mock_face_instance_state;

static int mock_face_library_open(const av_algo_library_args* args, av_algo_library* out) {
    (void)args;
    if (!out) return AV_ERR_INVALID_ARG;
    *out = (void*)0x5678;
    return AV_OK;
}

static int mock_face_library_query(av_algo_library lib, av_algo_library_info* out) {
    (void)lib;
    if (!out) return AV_ERR_INVALID_ARG;
    out->size = sizeof(av_algo_library_info);
    out->api_version = AV_ALGO_API_VERSION;
    strncpy(out->algorithm_id, "mock-face", sizeof(out->algorithm_id) - 1);
    strncpy(out->version, "1.0.0", sizeof(out->version) - 1);
    strncpy(out->algorithm_type, "face_recognition", sizeof(out->algorithm_type) - 1);
    return AV_OK;
}

static int mock_face_library_close(av_algo_library lib) {
    (void)lib;
    return AV_OK;
}

static int mock_face_instance_create(av_algo_library lib, const av_algo_instance_args* args,
                                     av_algo_instance* out) {
    (void)lib;
    if (!out) return AV_ERR_INVALID_ARG;
    mock_face_instance_state* state = (mock_face_instance_state*)calloc(1, sizeof(*state));
    if (!state) return AV_ERR_OUT_OF_MEMORY;
    if (args) {
        state->mode = args->mode;
        state->on_result = args->on_result;
        state->result_user = args->result_user;
    }
    *out = state;
    return AV_OK;
}

static int mock_face_instance_negotiate(av_algo_instance inst, const av_frame_caps* offered,
                                        av_frame_caps* accepted) {
    (void)inst;
    if (!offered || !accepted) return AV_ERR_INVALID_ARG;
    *accepted = *offered;
    return AV_OK;
}

static int mock_face_instance_update_config(av_algo_instance inst, const char* json, uint32_t len) {
    (void)inst;
    (void)json;
    (void)len;
    return AV_OK;
}

static int mock_face_instance_set_rules(av_algo_instance inst, const av_rule* rules, uint32_t count) {
    (void)inst;
    (void)rules;
    (void)count;
    return AV_OK;
}

static size_t mock_face_encode_base64(const uint8_t* data, size_t size, char* output, size_t capacity) {
    static const char alphabet[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    const size_t output_size = ((size + 2U) / 3U) * 4U;
    if (!data || !output || capacity <= output_size) return 0;

    size_t input_index = 0;
    size_t output_index = 0;
    while (input_index < size) {
        const size_t remaining = size - input_index;
        const uint32_t first = data[input_index++];
        const uint32_t second = remaining > 1U ? data[input_index++] : 0U;
        const uint32_t third = remaining > 2U ? data[input_index++] : 0U;
        const uint32_t block = (first << 16U) | (second << 8U) | third;
        output[output_index++] = alphabet[(block >> 18U) & 0x3FU];
        output[output_index++] = alphabet[(block >> 12U) & 0x3FU];
        output[output_index++] = remaining > 1U ? alphabet[(block >> 6U) & 0x3FU] : '=';
        output[output_index++] = remaining > 2U ? alphabet[block & 0x3FU] : '=';
    }
    output[output_index] = '\0';
    return output_index;
}

static void mock_face_embedding(uint64_t frame_id, uint8_t output[512U * sizeof(float)]) {
    memset(output, 0, 512U * sizeof(float));
    // 前两帧是与底库 [1, 0, ...] 的 0.8 归一化相似度，第三帧提升到 1.0。
    if (frame_id == 6U) return; // 合法 Base64，但范数为零
    if (frame_id == 10U) {
        const uint32_t second = 0x3f800000U; // 1.0f
        memcpy(output + sizeof(float), &second, sizeof(second));
    } else if (frame_id < 3U) {
        const uint32_t first = 0x3f19999aU;  // 0.6f
        const uint32_t second = 0x3f4ccccdU; // 0.8f
        memcpy(output, &first, sizeof(first));
        memcpy(output + sizeof(first), &second, sizeof(second));
    } else {
        const uint32_t first = 0x3f800000U; // 1.0f
        memcpy(output, &first, sizeof(first));
    }
}

static void mock_face_emit_result(mock_face_instance_state* state, const av_frame_desc* frame) {
    uint8_t embedding_bytes[512U * sizeof(float)];
    char embedding_base64[4096];
    const unsigned long long frame_id = frame ? (unsigned long long)frame->frame_id : 0ULL;
    const unsigned long long track_id = frame_id >= 1000ULL
        ? (frame_id == 2001ULL ? 2000ULL : frame_id)
        : 7ULL;

    if (frame_id == 4ULL) {
        static const char invalid_json[] = "{\"schema_version\":1";
        av_algo_result result;
        memset(&result, 0, sizeof(result));
        result.size = sizeof(av_algo_result);
        result.api_version = AV_ALGO_API_VERSION;
        result.kind = AV_RESULT_RECOGNITION;
        result.frame_id = frame_id;
        result.json = invalid_json;
        result.json_len = (uint32_t)(sizeof(invalid_json) - 1U);
        state->on_result(&result, state->result_user);
        return;
    }

    mock_face_embedding((uint64_t)frame_id, embedding_bytes);
    if (mock_face_encode_base64(embedding_bytes, sizeof(embedding_bytes), embedding_base64,
                                sizeof(embedding_base64)) == 0) {
        return;
    }

    const char* embedding_data = frame_id == 5ULL ? "AAAA" : embedding_base64;
    if (frame_id == 3000ULL) {
        char json[8192];
        const int written = snprintf(
            json, sizeof(json),
            "{\"schema_version\":1,\"frame_id\":%llu,"
            "\"algorithm_type\":\"face_recognition\",\"persons\":[{"
            "\"track_id\":%llu,\"bbox\":[0.1,0.1,0.4,0.6],\"confidence\":0.95,"
            "\"face\":{\"bbox\":[0.2,0.3,0.2,0.1],\"confidence\":0.93,"
            "\"landmarks\":[[0.22,0.32],[0.28,0.32],[0.25,0.35],[0.22,0.38],[0.28,0.38]],"
            "\"embedding\":{\"model\":\"mock-face\",\"dimension\":512,"
            "\"dtype\":\"float32\",\"normalized\":true,\"encoding\":\"base64\","
            "\"byte_order\":\"little_endian\",\"data\":\"%s\"}}},{"
            "\"track_id\":%llu,\"bbox\":[0.5,0.1,0.4,0.6],\"confidence\":0.95,"
            "\"face\":{\"bbox\":[0.6,0.3,0.2,0.1],\"confidence\":0.93,"
            "\"landmarks\":[[0.62,0.32],[0.68,0.32],[0.65,0.35],[0.62,0.38],[0.68,0.38]],"
            "\"embedding\":{\"model\":\"mock-face\",\"dimension\":512,"
            "\"dtype\":\"float32\",\"normalized\":true,\"encoding\":\"base64\","
            "\"byte_order\":\"little_endian\",\"data\":\"%s\"}}}]}\n",
            frame_id, 3000ULL, embedding_data, 3001ULL, embedding_data);
        if (written <= 0 || (size_t)written >= sizeof(json)) return;

        const av_algo_image_req image_requests[2] = {
            {sizeof(av_algo_image_req), AV_ALGO_API_VERSION, 0.2f, 0.3f, 0.2f, 0.1f, 0, 0},
            {sizeof(av_algo_image_req), AV_ALGO_API_VERSION, 0.6f, 0.3f, 0.2f, 0.1f, 0, 0}
        };
        av_algo_result result;
        memset(&result, 0, sizeof(result));
        result.size = sizeof(av_algo_result);
        result.api_version = AV_ALGO_API_VERSION;
        result.kind = AV_RESULT_RECOGNITION;
        result.frame_id = frame_id;
        result.json = json;
        result.json_len = (uint32_t)strlen(json);
        result.image_count = 2;
        result.images = image_requests;
        state->on_result(&result, state->result_user);
        return;
    }

    char json[8192];
    const int written = snprintf(
        json, sizeof(json),
        "{\"schema_version\":1,\"frame_id\":%llu,"
        "\"algorithm_type\":\"face_recognition\",\"persons\":[{"
        "\"track_id\":%llu,\"bbox\":[0.1,0.1,0.4,0.6],\"confidence\":0.95,"
        "\"face\":{\"bbox\":[0.2,0.3,0.2,0.1],\"confidence\":0.93,"
        "\"landmarks\":[[0.22,0.32],[0.28,0.32],[0.25,0.35],[0.22,0.38],[0.28,0.38]],"
        "\"embedding\":{\"model\":\"mock-face\",\"dimension\":512,"
        "\"dtype\":\"float32\",\"normalized\":true,\"encoding\":\"base64\","
        "\"byte_order\":\"little_endian\",\"data\":\"%s\"}}}]}\n",
        frame_id, track_id, embedding_data);
    if (written <= 0 || (size_t)written >= sizeof(json)) return;

    const av_algo_image_req image_request = {
        sizeof(av_algo_image_req), AV_ALGO_API_VERSION,
        frame_id == 9ULL ? 0.21f : 0.2f, 0.3f, 0.2f, 0.1f, 0, 0
    };
    av_algo_result result;
    memset(&result, 0, sizeof(result));
    result.size = sizeof(av_algo_result);
    result.api_version = AV_ALGO_API_VERSION;
    result.kind = AV_RESULT_RECOGNITION;
    result.frame_id = frame_id == 7ULL ? frame_id + 1ULL : frame_id;
    result.json = json;
    result.json_len = (uint32_t)strlen(json);
    result.image_count = frame_id == 8ULL ? 0U : 1U;
    result.images = frame_id == 8ULL ? NULL : &image_request;
    state->on_result(&result, state->result_user);
}

static int mock_face_instance_process(av_algo_instance inst, const av_frame_desc* frame) {
    mock_face_instance_state* state = (mock_face_instance_state*)inst;
    if (!state) return AV_ERR_INVALID_ARG;
    if (state->mode == AV_INSTANCE_NORMAL && state->on_result) {
        mock_face_emit_result(state, frame);
    }
    return AV_OK;
}

static int mock_face_instance_flush(av_algo_instance inst) {
    (void)inst;
    return AV_OK;
}

static int mock_face_instance_destroy(av_algo_instance inst) {
    free(inst);
    return AV_OK;
}

static int mock_face_last_error(av_algo_instance inst_or_null, char* buf, uint32_t cap) {
    (void)inst_or_null;
    if (buf && cap > 0) buf[0] = '\0';
    return AV_OK;
}

static av_algo_abi mock_face_api = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = mock_face_library_open,
    .library_query = mock_face_library_query,
    .library_close = mock_face_library_close,
    .instance_create = mock_face_instance_create,
    .instance_negotiate = mock_face_instance_negotiate,
    .instance_update_config = mock_face_instance_update_config,
    .instance_set_rules = mock_face_instance_set_rules,
    .instance_process = mock_face_instance_process,
    .instance_flush = mock_face_instance_flush,
    .instance_destroy = mock_face_instance_destroy,
    .last_error = mock_face_last_error,
};

const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return NULL;
    return &mock_face_api;
}
