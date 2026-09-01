#include "argus/algo.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct mock_lpr_instance_state {
    uint32_t mode;
    av_algo_result_cb on_result;
    void* result_user;
} mock_lpr_instance_state;

static int mock_lpr_library_open(const av_algo_library_args* args, av_algo_library* out) {
    (void)args;
    if (!out) return AV_ERR_INVALID_ARG;
    *out = (void*)0x4321;
    return AV_OK;
}

static int mock_lpr_library_query(av_algo_library lib, av_algo_library_info* out) {
    (void)lib;
    if (!out) return AV_ERR_INVALID_ARG;
    out->size = sizeof(av_algo_library_info);
    out->api_version = AV_ALGO_API_VERSION;
    strncpy(out->algorithm_id, "mock-lpr", sizeof(out->algorithm_id) - 1);
    strncpy(out->version, "1.0.0", sizeof(out->version) - 1);
    strncpy(out->algorithm_type, "license_plate_recognition", sizeof(out->algorithm_type) - 1);
    return AV_OK;
}

static int mock_lpr_library_close(av_algo_library lib) {
    (void)lib;
    return AV_OK;
}

static int mock_lpr_instance_create(av_algo_library lib, const av_algo_instance_args* args,
                                    av_algo_instance* out) {
    (void)lib;
    if (!out) return AV_ERR_INVALID_ARG;
    mock_lpr_instance_state* state = (mock_lpr_instance_state*)calloc(1, sizeof(*state));
    if (!state) return AV_ERR_OUT_OF_MEMORY;
    if (args) {
        state->mode = args->mode;
        state->on_result = args->on_result;
        state->result_user = args->result_user;
    }
    *out = state;
    return AV_OK;
}

static int mock_lpr_instance_negotiate(av_algo_instance inst, const av_frame_caps* offered,
                                       av_frame_caps* accepted) {
    (void)inst;
    if (!offered || !accepted) return AV_ERR_INVALID_ARG;
    *accepted = *offered;
    return AV_OK;
}

static int mock_lpr_instance_update_config(av_algo_instance inst, const char* json, uint32_t len) {
    (void)inst;
    (void)json;
    (void)len;
    return AV_OK;
}

static int mock_lpr_instance_set_rules(av_algo_instance inst, const av_rule* rules, uint32_t count) {
    (void)inst;
    (void)rules;
    (void)count;
    return AV_OK;
}

static void mock_lpr_emit_result(mock_lpr_instance_state* state, const av_frame_desc* frame) {
    char json[1024];
    const unsigned long long frame_id = frame ? (unsigned long long)frame->frame_id : 0ULL;
    const char* plate_text = frame_id == 100 ? "123456789012345678901234567890123" : "A12345";
    snprintf(json, sizeof(json),
             "{\"schema_version\":1,\"frame_id\":%llu,"
             "\"algorithm_type\":\"license_plate_recognition\","
             "\"event_id\":\"mock-lpr-event\",\"plates\":[{"
             "\"track_id\":7,\"plate_text\":\"%s\","
             "\"normalized_text\":\"%s\",\"plate_color\":\"blue\","
             "\"plate_type\":\"standard\",\"confidence\":0.95,"
             "\"ocr_confidence\":0.93,\"bbox\":[0.2,0.3,0.2,0.1],"
             "\"vehicle_bbox\":[0.1,0.1,0.5,0.5]}]}",
             frame_id, plate_text, plate_text);

    static const av_algo_image_req image_requests[] = {
        {sizeof(av_algo_image_req), AV_ALGO_API_VERSION, 0.0f, 0.0f, 1.0f, 1.0f, 0, 0},
        {sizeof(av_algo_image_req), AV_ALGO_API_VERSION, 0.2f, 0.3f, 0.2f, 0.1f, 0, 0},
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
}

static int mock_lpr_instance_process(av_algo_instance inst, const av_frame_desc* frame) {
    mock_lpr_instance_state* state = (mock_lpr_instance_state*)inst;
    if (!state) return AV_ERR_INVALID_ARG;
    if (state->mode != AV_INSTANCE_NORMAL || !state->on_result) return AV_OK;

    if (frame && frame->frame_id == 99) {
        static const char invalid_json[] =
            "{\"schema_version\":1,\"frame_id\":99,"
            "\"algorithm_type\":\"license_plate_recognition\","
            "\"event_id\":\"invalid-image\",\"plates\":[{"
            "\"track_id\":8,\"plate_text\":\"A12345\","
            "\"normalized_text\":\"A12345\",\"plate_color\":\"blue\","
            "\"plate_type\":\"standard\",\"confidence\":0.95,"
            "\"ocr_confidence\":0.93,\"bbox\":[0.2,0.3,0.2,0.1]}]}";
        av_algo_result result;
        memset(&result, 0, sizeof(result));
        result.size = sizeof(av_algo_result);
        result.api_version = AV_ALGO_API_VERSION;
        result.kind = AV_RESULT_RECOGNITION;
        result.frame_id = frame->frame_id;
        result.json = invalid_json;
        result.json_len = (uint32_t)(sizeof(invalid_json) - 1);
        state->on_result(&result, state->result_user);
        return AV_OK;
    }

    mock_lpr_emit_result(state, frame);
    return AV_OK;
}

static int mock_lpr_instance_flush(av_algo_instance inst) {
    (void)inst;
    return AV_OK;
}

static int mock_lpr_instance_destroy(av_algo_instance inst) {
    free(inst);
    return AV_OK;
}

static int mock_lpr_last_error(av_algo_instance inst_or_null, char* buf, uint32_t cap) {
    (void)inst_or_null;
    if (buf && cap > 0) buf[0] = '\0';
    return AV_OK;
}

static av_algo_abi mock_lpr_api = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = mock_lpr_library_open,
    .library_query = mock_lpr_library_query,
    .library_close = mock_lpr_library_close,
    .instance_create = mock_lpr_instance_create,
    .instance_negotiate = mock_lpr_instance_negotiate,
    .instance_update_config = mock_lpr_instance_update_config,
    .instance_set_rules = mock_lpr_instance_set_rules,
    .instance_process = mock_lpr_instance_process,
    .instance_flush = mock_lpr_instance_flush,
    .instance_destroy = mock_lpr_instance_destroy,
    .last_error = mock_lpr_last_error,
};

const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return NULL;
    return &mock_lpr_api;
}
