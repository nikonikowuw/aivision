/**
 * @file mock_algo.c
 * @brief C ABI 规范 Mock 算法插件动态库实现（供自检与单测使用）
 */

#include "argus/algo.h"
#include <stdlib.h>
#include <string.h>


/* Instance state: validator self-test needs process() to emit exactly one
 * AV_RESULT_SELF_TEST callback (algo-package-spec.md §4.2). */
typedef struct mock_instance_state {
    uint32_t mode;
    av_algo_result_cb on_result;
    void* result_user;
    int emitted;
} mock_instance_state;

static int mock_library_open(const av_algo_library_args* args, av_algo_library* out) {
    if (out) *out = (void*)0x1234;
    return AV_OK;
}

static int mock_library_query(av_algo_library lib, av_algo_library_info* out) {
    if (out) {
        out->size = sizeof(av_algo_library_info);
        out->api_version = AV_ALGO_API_VERSION;
        strncpy(out->algorithm_id, "mock-detector", sizeof(out->algorithm_id) - 1);
        strncpy(out->version, "1.0.0", sizeof(out->version) - 1);
        strncpy(out->algorithm_type, "object_detection", sizeof(out->algorithm_type) - 1);
        strncpy(out->alarm_type_id, "mock_alarm", sizeof(out->alarm_type_id) - 1);
    }
    return AV_OK;
}

static int mock_library_close(av_algo_library lib) {
    return AV_OK;
}

static int mock_instance_create(av_algo_library lib, const av_algo_instance_args* args, av_algo_instance* out) {
    if (!out) return AV_ERR_INVALID_ARG;
    mock_instance_state* st = (mock_instance_state*)calloc(1, sizeof(mock_instance_state));
    if (!st) return AV_ERR_OUT_OF_MEMORY;
    st->mode = args ? args->mode : AV_INSTANCE_NORMAL;
    st->on_result = args ? args->on_result : NULL;
    st->result_user = args ? args->result_user : NULL;
    *out = st;
    return AV_OK;
}

static int mock_instance_negotiate(av_algo_instance inst, const av_frame_caps* offered, av_frame_caps* accepted) {
    if (accepted && offered) {
        *accepted = *offered;
    }
    return AV_OK;
}

static int mock_instance_update_config(av_algo_instance inst, const char* json, uint32_t len) {
    return AV_OK;
}

static int mock_instance_set_rules(av_algo_instance inst, const av_rule* rules, uint32_t count) {
    return AV_OK;
}

static int mock_instance_process(av_algo_instance inst, const av_frame_desc* frame) {
    mock_instance_state* st = (mock_instance_state*)inst;
    if (!st) return AV_ERR_INVALID_ARG;

    if (st->mode == AV_INSTANCE_INSTALL_SELF_TEST && st->on_result && !st->emitted) {
        static const char kSelfTestJson[] = "{\"status\":\"ok\",\"stages\":[\"load\",\"infer\"],\"object_count\":0}";
        av_algo_result res;
        memset(&res, 0, sizeof(res));
        res.size = sizeof(av_algo_result);
        res.api_version = AV_ALGO_API_VERSION;
        res.kind = AV_RESULT_SELF_TEST;
        res.frame_id = frame ? frame->frame_id : 0;
        res.json = kSelfTestJson;
        res.json_len = (uint32_t)(sizeof(kSelfTestJson) - 1);
        st->emitted = 1;
        st->on_result(&res, st->result_user);
    } else if (st->mode == AV_INSTANCE_NORMAL && st->on_result) {
        static const char kAlarmJson[] = "{\"object_count\":0}";
        av_algo_result res;
        memset(&res, 0, sizeof(res));
        res.size = sizeof(av_algo_result);
        res.api_version = AV_ALGO_API_VERSION;
        res.kind = AV_RESULT_ALARM;
        res.frame_id = frame ? frame->frame_id : 0;
        res.json = kAlarmJson;
        res.json_len = (uint32_t)(sizeof(kAlarmJson) - 1);
        st->on_result(&res, st->result_user);
    }
    return AV_OK;
}

static int mock_instance_flush(av_algo_instance inst) {
    return AV_OK;
}

static int mock_instance_destroy(av_algo_instance inst) {
    free(inst);
    return AV_OK;
}

static int mock_last_error(av_algo_instance inst_or_null, char* buf, uint32_t cap) {
    if (buf && cap > 0) buf[0] = '\0';
    return AV_OK;
}

static av_algo_abi g_api = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = mock_library_open,
    .library_query = mock_library_query,
    .library_close = mock_library_close,
    .instance_create = mock_instance_create,
    .instance_negotiate = mock_instance_negotiate,
    .instance_update_config = mock_instance_update_config,
    .instance_set_rules = mock_instance_set_rules,
    .instance_process = mock_instance_process,
    .instance_flush = mock_instance_flush,
    .instance_destroy = mock_instance_destroy,
    .last_error = mock_last_error
};

const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return NULL;
    return &g_api;
}
