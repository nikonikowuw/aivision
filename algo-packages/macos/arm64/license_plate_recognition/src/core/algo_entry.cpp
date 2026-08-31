/**
 * @file algo_entry.cpp
 * @brief C ABI 导出接口与车牌识别流水线
 */

#include "argus/algo.h"
#include "argus/result.h"
#include "argus/types.h"
#include "../preprocess/preprocessor.hpp"
#include "../inference/model_inference.hpp"
#include "../postprocess/postprocessor.hpp"
#include "config.hpp"

#include <memory>
#include <string>
#include <vector>
#include <mutex>
#include <cstring>
#include <iostream>

namespace lpr {

constexpr const char* kAlgorithmId = "license_plate_recognition";
constexpr const char* kVersion = "1.0.0";
constexpr const char* kAlgorithmType = "license_plate_recognition";

struct LibraryContext {
    std::string package_root;
    std::string platform_id;
    std::string detect_path = "model/plate_detect.mlpackage";
    std::string rec_path = "model/plate_rec_color.mlpackage";
    Config default_config;

    av_log_fn log = nullptr;
    void* log_user = nullptr;
    std::shared_ptr<ModelInferenceManager> model_manager;
};

struct InstanceContext {
    LibraryContext* lib = nullptr;
    uint32_t mode = AV_INSTANCE_NORMAL;
    std::string instance_id;
    std::string instance_run_id;
    Config config;

    const av_frame_ops* frame_ops = nullptr;
    const av_image_ops* image_ops = nullptr;
    av_algo_result_cb on_result = nullptr;
    void* result_user = nullptr;
    std::string last_error_msg;
    bool self_test_emitted = false;
    uint64_t last_frame_id = 0;

    std::unique_ptr<Postprocessor> postprocessor;
};

} // namespace lpr

using namespace lpr;

namespace {

thread_local std::string g_last_error;

void log_message(LibraryContext* lib, int level, const std::string& message) noexcept {
    if (!lib || !lib->log) return;
    try {
        lib->log(lib->log_user, level, message.c_str(), static_cast<uint32_t>(message.size()));
    } catch (...) {
    }
}

void set_error(InstanceContext* inst, const std::string& message) noexcept {
    try {
        g_last_error = message;
        if (inst) {
            inst->last_error_msg = message;
            log_message(inst->lib, 4, message);
        }
    } catch (...) {
    }
}

void set_error(LibraryContext* lib, const std::string& message) noexcept {
    try {
        g_last_error = message;
        log_message(lib, 4, message);
    } catch (...) {
    }
}

int fail(InstanceContext* inst, int status, const char* message) noexcept {
    set_error(inst, message ? message : "algorithm call failed");
    return status;
}

int fail(LibraryContext* lib, int status, const char* message) noexcept {
    set_error(lib, message ? message : "algorithm call failed");
    return status;
}

int algo_library_open(const av_algo_library_args* args, av_algo_library* out) {
    if (!out) return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INVALID_ARG, "null output library handle");

    auto lib = std::make_unique<LibraryContext>();
    if (args) {
        if (args->package_root) lib->package_root = args->package_root;
        if (args->platform_id) lib->platform_id = args->platform_id;
        lib->log = args->log;
        lib->log_user = args->log_user;
    }

    lib->model_manager = std::make_shared<ModelInferenceManager>();
    std::string err;
    if (!lib->model_manager->load_models(lib->package_root, lib->detect_path, lib->rec_path, err)) {
        return fail(lib.get(), AV_ERR_MODEL_LOAD_FAILED, ("failed to load models: " + err).c_str());
    }

    *out = lib.release();
    return AV_OK;
}

int algo_library_query(av_algo_library lib, av_algo_library_info* out) {
    if (!out) return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INVALID_ARG, "null output library info");
    auto* lib_ctx = static_cast<LibraryContext*>(lib);

    out->size = sizeof(av_algo_library_info);
    out->api_version = AV_ALGO_API_VERSION;
    std::strncpy(out->algorithm_id, kAlgorithmId, sizeof(out->algorithm_id) - 1);
    std::strncpy(out->version, kVersion, sizeof(out->version) - 1);
    std::strncpy(out->algorithm_type, kAlgorithmType, sizeof(out->algorithm_type) - 1);
    out->alarm_type_id[0] = '\0'; // 没有告警类型 (观测记录)

    log_message(lib_ctx, 1, "library_query called successfully");
    return AV_OK;
}

int algo_library_close(av_algo_library lib) {
    if (!lib) return AV_OK;
    delete static_cast<LibraryContext*>(lib);
    return AV_OK;
}

int algo_instance_create(av_algo_library lib, const av_algo_instance_args* args, av_algo_instance* out) {
    if (!lib || !out) return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INVALID_ARG, "invalid arguments for instance_create");
    auto* lib_ctx = static_cast<LibraryContext*>(lib);

    auto inst = std::make_unique<InstanceContext>();
    inst->lib = lib_ctx;
    inst->config = lib_ctx->default_config;

    if (args) {
        inst->mode = args->mode;
        if (args->instance_id) inst->instance_id = args->instance_id;
        if (args->instance_run_id) inst->instance_run_id = args->instance_run_id;
        if (args->config_json && args->config_json_len > 0) {
            inst->config = Config::from_json(args->config_json, args->config_json_len);
        }
        inst->frame_ops = args->frame_ops;
        inst->image_ops = args->image_ops;
        inst->on_result = args->on_result;
        inst->result_user = args->result_user;
    }

    inst->postprocessor = std::make_unique<Postprocessor>(inst->config);

    *out = inst.release();
    return AV_OK;
}

int algo_instance_negotiate(av_algo_instance inst, const av_frame_caps* offered, av_frame_caps* accepted) {
    if (!inst || !offered || !accepted ||
        offered->size < sizeof(av_frame_caps) || accepted->size < sizeof(av_frame_caps) ||
        offered->api_version != AV_ALGO_API_VERSION || accepted->api_version != AV_ALGO_API_VERSION) {
        return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "invalid arguments for negotiate");
    }

    bool nv12_found = false;
    for (uint32_t i = 0; i < offered->pixel_format_count; ++i) {
        if (offered->pixel_formats[i] == AV_PIX_NV12) {
            nv12_found = true;
            break;
        }
    }
    if (!nv12_found) return AV_ERR_INCOMPATIBLE_FRAME;

    accepted->pixel_format_count = 1;
    accepted->pixel_formats[0] = AV_PIX_NV12;
    accepted->memory_type_count = offered->memory_type_count;
    for (uint32_t i = 0; i < offered->memory_type_count; ++i) {
        accepted->memory_types[i] = offered->memory_types[i];
    }
    accepted->required_opaque_kind = offered->required_opaque_kind;
    accepted->min_width = 320;
    accepted->min_height = 320;
    accepted->max_width = 3840;
    accepted->max_height = 2160;

    return AV_OK;
}

int algo_instance_update_config(av_algo_instance inst, const char* json, uint32_t len) {
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in update_config");
    auto* inst_ctx = static_cast<InstanceContext*>(inst);

    if (json && len > 0) {
        inst_ctx->config = Config::from_json(json, len);
        if (inst_ctx->postprocessor) {
            inst_ctx->postprocessor->update_config(inst_ctx->config);
        }
    }
    return AV_OK;
}

int algo_instance_set_rules(av_algo_instance inst, const av_rule* rules, uint32_t count) {
    (void)rules;
    (void)count;
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in set_rules");
    return AV_OK;
}

int algo_instance_process(av_algo_instance inst, const av_frame_desc* frame) {
    if (!inst || !frame) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance or frame in process");
    auto* inst_ctx = static_cast<InstanceContext*>(inst);
    auto* lib_ctx = inst_ctx->lib;

    inst_ctx->last_frame_id = frame->frame_id;

    // 1. 预处理
    PreprocessResult prep;
    std::string err;
    if (!Preprocessor::process_frame(frame, prep, err)) {
        return fail(inst_ctx, AV_ERR_INTERNAL, ("preprocessor failed: " + err).c_str());
    }

    // 2. 车牌检测推理
    PlateDetectOutput detect_out;
    if (!lib_ctx->model_manager->run_detect(prep.letterbox_rgb.data.data(), detect_out, err)) {
        return fail(inst_ctx, AV_ERR_INFERENCE_FAILED, ("detect inference failed: " + err).c_str());
    }

    // 3. NMS 过滤
    auto candidates = inst_ctx->postprocessor->filter_and_nms(
        detect_out, prep.letterbox_info, prep.original_rgb.width, prep.original_rgb.height);

    // 4. 对每个候选框进行透视变换与 CRNN 识别
    for (auto& cand : candidates) {
        ImageBuffer plate_crop;
        if (Preprocessor::warp_plate_168x48(prep.original_rgb, cand.landmarks_8, cand.is_double_layer, plate_crop, err)) {
            PlateRecOutput rec_out;
            if (lib_ctx->model_manager->run_rec(plate_crop.data.data(), rec_out, err)) {
                inst_ctx->postprocessor->decode_plate_recognition(
                    rec_out, cand.is_double_layer,
                    cand.plate_text, cand.normalized_text,
                    cand.plate_color, cand.plate_type, cand.ocr_confidence);
            }
        }
    }

    // 自检模式
    if (inst_ctx->mode == AV_INSTANCE_INSTALL_SELF_TEST) {
        if (!inst_ctx->self_test_emitted && inst_ctx->on_result) {
            av_algo_result res{};
            res.size = sizeof(av_algo_result);
            res.api_version = AV_ALGO_API_VERSION;
            res.kind = AV_RESULT_SELF_TEST;
            res.frame_id = frame->frame_id;
            res.json = "{}";
            res.json_len = 2;
            res.image_count = 0;
            res.images = nullptr;
            inst_ctx->on_result(&res, inst_ctx->result_user);
            inst_ctx->self_test_emitted = true;
        }
        return AV_OK;
    }

    // 5. 跟踪与多帧多数表决
    auto tracked_plates = inst_ctx->postprocessor->track_and_vote(candidates, frame->wall_time_ns);

    // 6. 构造结果 JSON
    std::string json_result = inst_ctx->postprocessor->build_result_json(frame->frame_id, tracked_plates);

    // 7. 若有稳定车牌触发上报，发出 AV_RESULT_RECOGNITION 回调
    bool has_reports = false;
    for (const auto& p : tracked_plates) {
        if (p.should_report) {
            has_reports = true;
            break;
        }
    }

    if (has_reports && inst_ctx->on_result) {
        av_algo_result res{};
        res.size = sizeof(av_algo_result);
        res.api_version = AV_ALGO_API_VERSION;
        res.kind = AV_RESULT_RECOGNITION;
        res.frame_id = frame->frame_id;
        res.json = json_result.c_str();
        res.json_len = static_cast<uint32_t>(json_result.size());
        res.image_count = 0;
        res.images = nullptr;

        inst_ctx->on_result(&res, inst_ctx->result_user);
    }

    return AV_OK;
}

int algo_instance_flush(av_algo_instance inst) {
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in flush");
    return AV_OK;
}

int algo_instance_destroy(av_algo_instance inst) {
    if (!inst) return AV_OK;
    delete static_cast<InstanceContext*>(inst);
    return AV_OK;
}

int algo_last_error(av_algo_instance inst_or_null, char* buf, uint32_t cap) {
    if (!buf || cap == 0) return AV_ERR_INVALID_ARG;
    std::string msg = g_last_error;
    if (inst_or_null) {
        auto* inst_ctx = static_cast<InstanceContext*>(inst_or_null);
        if (!inst_ctx->last_error_msg.empty()) {
            msg = inst_ctx->last_error_msg;
        }
    }
    std::strncpy(buf, msg.c_str(), cap - 1);
    buf[cap - 1] = '\0';
    return AV_OK;
}

const av_algo_abi g_abi = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = algo_library_open,
    .library_query = algo_library_query,
    .library_close = algo_library_close,
    .instance_create = algo_instance_create,
    .instance_negotiate = algo_instance_negotiate,
    .instance_update_config = algo_instance_update_config,
    .instance_set_rules = algo_instance_set_rules,
    .instance_process = algo_instance_process,
    .instance_flush = algo_instance_flush,
    .instance_destroy = algo_instance_destroy,
    .last_error = algo_last_error
};

} // namespace

extern "C" {

AV_EXPORT const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return nullptr;
    return &g_abi;
}

}
