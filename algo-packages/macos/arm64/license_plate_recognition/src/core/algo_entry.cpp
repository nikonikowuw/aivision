/**
 * @file algo_entry.cpp
 * @brief C ABI 导出接口与通用多语言车牌识别流水线实现
 */

#include "argus/algo.h"
#include "argus/result.h"
#include "argus/types.h"
#include "../preprocess/preprocessor.hpp"
#include "../inference/model_inference.hpp"
#include "../postprocess/postprocessor.hpp"
#include "config.hpp"
#include "rules.hpp"

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstring>
#include <iostream>
#include <limits>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

namespace lpr {

constexpr const char* kAlgorithmId = "license_plate_recognition";
constexpr const char* kVersion = "1.0.0";
constexpr const char* kAlgorithmType = "license_plate_recognition";

struct LibraryContext {
    std::string package_root;
    std::string platform_id;
    std::string detect_path = "model/plate_detect.mlpackage";
    std::string rec_path = "model/plate_rec_ppocr.mlpackage";
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
    std::vector<RuleState> rules;
    std::unordered_map<int64_t, av_point> previous_rule_points;
    std::unordered_map<int64_t, uint32_t> missed_rule_frames;
    std::vector<av_algo_image_req> result_images;

    std::unique_ptr<Postprocessor> postprocessor;
};

} // namespace lpr

using namespace lpr;

namespace {

thread_local std::string g_last_error;

template <typename T>
bool has_abi_member(const T* value, size_t offset, size_t member_size) noexcept {
    return value && value->size >= offset + member_size;
}

bool valid_frame(const av_frame_desc& frame) noexcept {
    return frame.size >= sizeof(av_frame_desc) && frame.api_version == AV_ALGO_API_VERSION &&
           frame.frame_token != nullptr && frame.opaque != nullptr && frame.pixel_format == AV_PIX_NV12 &&
           (frame.memory_type == AV_MEM_HOST || frame.memory_type == AV_MEM_PLATFORM_SURFACE) &&
           frame.width >= 320 && frame.height >= 320 && frame.width <= 3840 && frame.height <= 2160 &&
           frame.stride[0] > 0 && frame.stride[1] > 0;
}

void set_error(InstanceContext* inst, const std::string& message) noexcept;

bool invoke_result_callback(InstanceContext* inst, const av_algo_result& result) noexcept {
    if (!inst || !inst->on_result) return true;
    try {
        inst->on_result(&result, inst->result_user);
        return true;
    } catch (...) {
        set_error(inst, "result callback raised an exception");
        return false;
    }
}

std::string make_self_test_json(uint32_t object_count) {
    return "{\"status\":\"ok\",\"stages\":[\"model_load\",\"preprocess\",\"detect\",\"recognition\",\"postprocess\"],\"object_count\":" +
           std::to_string(object_count) + "}";
}

void prepare_result_images(InstanceContext* inst, const std::vector<PlateObject>& plates) {
    inst->result_images.clear();
    av_algo_image_req panorama{};
    panorama.size = sizeof(av_algo_image_req);
    panorama.api_version = AV_ALGO_API_VERSION;
    panorama.w = 1.0f;
    panorama.h = 1.0f;
    panorama.purpose = 0;
    inst->result_images.push_back(panorama);

    if (!inst->config.save_plate_crop) return;
    for (const auto& plate : plates) {
        if (!plate.should_report) continue;
        av_algo_image_req crop{};
        crop.size = sizeof(av_algo_image_req);
        crop.api_version = AV_ALGO_API_VERSION;
        crop.x = plate.x_min;
        crop.y = plate.y_min;
        crop.w = plate.x_max - plate.x_min;
        crop.h = plate.y_max - plate.y_min;
        crop.purpose = 0;
        inst->result_images.push_back(crop);
    }
}

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
    if (args && (!has_abi_member(args, offsetof(av_algo_library_args, api_version), sizeof(args->api_version)) ||
                 args->api_version != AV_ALGO_API_VERSION)) {
        return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_UNSUPPORTED_API,
                    "unsupported or truncated library arguments");
    }

    try {
        auto lib = std::make_unique<LibraryContext>();
        if (args) {
            if (has_abi_member(args, offsetof(av_algo_library_args, package_root), sizeof(args->package_root)) &&
                args->package_root) {
                lib->package_root = args->package_root;
            }
            if (has_abi_member(args, offsetof(av_algo_library_args, platform_id), sizeof(args->platform_id)) &&
                args->platform_id) {
                lib->platform_id = args->platform_id;
            }
            if (has_abi_member(args, offsetof(av_algo_library_args, log), sizeof(args->log))) {
                lib->log = args->log;
                lib->log_user = args->log_user;
            }
        }

        lib->model_manager = std::make_shared<ModelInferenceManager>();
        std::string err;
        if (!lib->model_manager->load_models(lib->package_root, lib->detect_path, lib->rec_path, err)) {
            return fail(lib.get(), AV_ERR_MODEL_LOAD_FAILED, ("failed to load models: " + err).c_str());
        }

        *out = lib.release();
        return AV_OK;
    } catch (const std::bad_alloc&) {
        return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_OUT_OF_MEMORY, "out of memory in library_open");
    } catch (...) {
        return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INTERNAL, "exception in library_open");
    }
}

int algo_library_query(av_algo_library lib, av_algo_library_info* out) {
    if (!lib || !out) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_INVALID_ARG,
                    "invalid arguments for library_query");
    }
    if (!has_abi_member(out, offsetof(av_algo_library_info, api_version), sizeof(out->api_version)) ||
        out->api_version != AV_ALGO_API_VERSION || out->size < sizeof(av_algo_library_info)) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_INVALID_ARG,
                    "library_query output buffer is truncated or incompatible");
    }

    try {
        auto* lib_ctx = static_cast<LibraryContext*>(lib);
        out->size = sizeof(av_algo_library_info);
        out->api_version = AV_ALGO_API_VERSION;
        std::memset(out->algorithm_id, 0, sizeof(out->algorithm_id));
        std::memset(out->version, 0, sizeof(out->version));
        std::memset(out->algorithm_type, 0, sizeof(out->algorithm_type));
        std::memset(out->alarm_type_id, 0, sizeof(out->alarm_type_id));
        std::strncpy(out->algorithm_id, kAlgorithmId, sizeof(out->algorithm_id) - 1);
        std::strncpy(out->version, kVersion, sizeof(out->version) - 1);
        std::strncpy(out->algorithm_type, kAlgorithmType, sizeof(out->algorithm_type) - 1);

        log_message(lib_ctx, 1, "library_query called successfully");
        return AV_OK;
    } catch (...) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_INTERNAL, "exception in library_query");
    }
}

int algo_library_close(av_algo_library lib) {
    if (!lib) return AV_OK;
    try {
        delete static_cast<LibraryContext*>(lib);
        return AV_OK;
    } catch (...) {
        return AV_ERR_INTERNAL;
    }
}

int algo_instance_create(av_algo_library lib, const av_algo_instance_args* args, av_algo_instance* out) {
    if (!lib || !out) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_INVALID_ARG,
                    "invalid arguments for instance_create");
    }
    if (args && (!has_abi_member(args, offsetof(av_algo_instance_args, api_version), sizeof(args->api_version)) ||
                 args->api_version != AV_ALGO_API_VERSION)) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_UNSUPPORTED_API,
                    "unsupported or truncated instance arguments");
    }

    try {
        auto* lib_ctx = static_cast<LibraryContext*>(lib);
        auto inst = std::make_unique<InstanceContext>();
        inst->lib = lib_ctx;
        inst->config = lib_ctx->default_config;

        if (args) {
            if (has_abi_member(args, offsetof(av_algo_instance_args, mode), sizeof(args->mode))) {
                inst->mode = args->mode;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, instance_id), sizeof(args->instance_id)) &&
                args->instance_id) {
                inst->instance_id = args->instance_id;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, instance_run_id), sizeof(args->instance_run_id)) &&
                args->instance_run_id) {
                inst->instance_run_id = args->instance_run_id;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, frame_ops), sizeof(args->frame_ops))) {
                inst->frame_ops = args->frame_ops;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, image_ops), sizeof(args->image_ops))) {
                inst->image_ops = args->image_ops;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, on_result), sizeof(args->on_result))) {
                inst->on_result = args->on_result;
            }
            if (has_abi_member(args, offsetof(av_algo_instance_args, result_user), sizeof(args->result_user))) {
                inst->result_user = args->result_user;
            }
            if (args->config_json && args->config_json_len > 0) {
                std::string error;
                (void)Config::parse_json(args->config_json, args->config_json_len, inst->config, error);
            }
            if (args->rules && args->rule_count > 0) {
                std::string error;
                validate_and_copy_rules(args->rules, args->rule_count, inst->rules, error);
            }
        }

        inst->postprocessor = std::make_unique<Postprocessor>(inst->config);
        *out = inst.release();
        return AV_OK;
    } catch (const std::bad_alloc&) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_OUT_OF_MEMORY, "out of memory in instance_create");
    } catch (...) {
        return fail(static_cast<LibraryContext*>(lib), AV_ERR_INTERNAL, "exception in instance_create");
    }
}

int algo_instance_destroy(av_algo_instance inst) {
    if (!inst) return AV_OK;
    try {
        delete static_cast<InstanceContext*>(inst);
        return AV_OK;
    } catch (...) {
        return AV_ERR_INTERNAL;
    }
}

int algo_instance_negotiate(av_algo_instance inst,
                            const av_frame_caps* offered,
                            av_frame_caps* accepted) {
    if (!inst || !offered || !accepted) {
        return fail(static_cast<InstanceContext*>(inst), AV_ERR_INVALID_ARG, "null args in negotiate");
    }
    if (!has_abi_member(offered, offsetof(av_frame_caps, api_version), sizeof(offered->api_version)) ||
        offered->api_version != AV_ALGO_API_VERSION ||
        !has_abi_member(accepted, offsetof(av_frame_caps, api_version), sizeof(accepted->api_version)) ||
        accepted->api_version != AV_ALGO_API_VERSION) {
        return fail(static_cast<InstanceContext*>(inst), AV_ERR_UNSUPPORTED_API, "media profile ABI mismatch");
    }

    try {
        accepted->size = sizeof(av_frame_caps);
        accepted->api_version = AV_ALGO_API_VERSION;
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
    } catch (...) {
        return fail(static_cast<InstanceContext*>(inst), AV_ERR_INTERNAL, "exception in negotiate");
    }
}

int algo_instance_update_config(av_algo_instance inst, const char* json, uint32_t len) {
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in update_config");
    auto* inst_ctx = static_cast<InstanceContext*>(inst);
    if (!json || len == 0) return fail(inst_ctx, AV_ERR_CONFIG_INVALID, "config JSON is empty");

    try {
        Config parsed;
        std::string error;
        if (!Config::parse_json(json, len, parsed, error)) {
            return fail(inst_ctx, AV_ERR_CONFIG_INVALID, error.c_str());
        }
        inst_ctx->config = std::move(parsed);
        if (inst_ctx->postprocessor) inst_ctx->postprocessor->update_config(inst_ctx->config);
        return AV_OK;
    } catch (const std::bad_alloc&) {
        return fail(inst_ctx, AV_ERR_OUT_OF_MEMORY, "out of memory in update_config");
    } catch (...) {
        return fail(inst_ctx, AV_ERR_INTERNAL, "exception in update_config");
    }
}

int algo_instance_set_rules(av_algo_instance inst, const av_rule* rules, uint32_t count) {
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in set_rules");
    auto* inst_ctx = static_cast<InstanceContext*>(inst);
    try {
        std::vector<RuleState> copied_rules;
        std::string error;
        if (!validate_and_copy_rules(rules, count, copied_rules, error)) {
            return fail(inst_ctx, AV_ERR_INVALID_ARG, error.c_str());
        }
        inst_ctx->rules = std::move(copied_rules);
        inst_ctx->previous_rule_points.clear();
        inst_ctx->missed_rule_frames.clear();
        return AV_OK;
    } catch (const std::bad_alloc&) {
        return fail(inst_ctx, AV_ERR_OUT_OF_MEMORY, "out of memory in set_rules");
    } catch (...) {
        return fail(inst_ctx, AV_ERR_INTERNAL, "exception in set_rules");
    }
}

int algo_instance_process(av_algo_instance inst, const av_frame_desc* frame) {
    if (!inst || !frame) {
        return fail(static_cast<InstanceContext*>(inst), AV_ERR_INVALID_ARG,
                    "null instance or frame in process");
    }
    auto* inst_ctx = static_cast<InstanceContext*>(inst);
    if (!valid_frame(*frame)) {
        return fail(inst_ctx, AV_ERR_INVALID_ARG, "frame descriptor is invalid or unsupported");
    }

    try {
        auto* lib_ctx = inst_ctx->lib;
        if (!lib_ctx || !lib_ctx->model_manager || !inst_ctx->postprocessor) {
            return fail(inst_ctx, AV_ERR_INTERNAL, "algorithm instance is not initialized");
        }
        inst_ctx->last_frame_id = frame->frame_id;

        // 1. 预处理 (NV12 -> 640x384 Letterbox RGB)
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

        // 3. NMS 过滤并应用区域规则
        auto candidates = inst_ctx->postprocessor->filter_and_nms(
            detect_out, prep.letterbox_info, prep.original_rgb.width, prep.original_rgb.height);
        filter_region_rules(candidates, inst_ctx->rules);

        // 4. 通用多语言车牌识别 (PP-OCRv4)
        for (auto& candidate : candidates) {
            ImageBuffer crop_320;
            if (Preprocessor::warp_plate_320x48(prep.original_rgb, candidate.landmarks_8,
                                                candidate.is_double_layer, crop_320, err)) {
                PlateRecOutput rec_out{};
                if (lib_ctx->model_manager->run_rec(crop_320.data.data(), rec_out, err)) {
                    inst_ctx->postprocessor->decode_plate_recognition(
                        rec_out, candidate.is_double_layer, candidate.plate_text,
                        candidate.normalized_text, candidate.plate_color, candidate.plate_type,
                        candidate.ocr_confidence);
                }
            }
        }

        candidates.erase(std::remove_if(candidates.begin(), candidates.end(),
                                        [inst_ctx](const PlateObject& candidate) {
                                            return !inst_ctx->config.is_plate_color_allowed(
                                                candidate.plate_color);
                                        }),
                         candidates.end());

        // 自检模式
        if (inst_ctx->mode == AV_INSTANCE_INSTALL_SELF_TEST) {
            if (!inst_ctx->self_test_emitted && inst_ctx->on_result) {
                const std::string self_test_json = make_self_test_json(
                    static_cast<uint32_t>(candidates.size()));
                av_algo_result result{};
                result.size = sizeof(av_algo_result);
                result.api_version = AV_ALGO_API_VERSION;
                result.kind = AV_RESULT_SELF_TEST;
                result.frame_id = frame->frame_id;
                result.json = self_test_json.c_str();
                result.json_len = static_cast<uint32_t>(self_test_json.size());
                if (!invoke_result_callback(inst_ctx, result)) return AV_ERR_INTERNAL;
                inst_ctx->self_test_emitted = true;
            }
            return AV_OK;
        }

        // 多帧多数表决与跟踪
        auto final_plates = inst_ctx->postprocessor->track_and_vote(
            candidates, static_cast<int64_t>(frame->pts_ns));

        bool has_reporting_plate = std::any_of(final_plates.begin(), final_plates.end(),
                                               [](const PlateObject& p) { return p.should_report; });

        if (has_reporting_plate && inst_ctx->on_result) {
            std::string result_json = inst_ctx->postprocessor->build_result_json(
                frame->frame_id, final_plates);
            prepare_result_images(inst_ctx, final_plates);

            av_algo_result result{};
            result.size = sizeof(av_algo_result);
            result.api_version = AV_ALGO_API_VERSION;
            result.kind = AV_RESULT_ALARM;
            result.frame_id = frame->frame_id;
            result.json = result_json.c_str();
            result.json_len = static_cast<uint32_t>(result_json.size());
            result.image_count = static_cast<uint32_t>(inst_ctx->result_images.size());
            result.images = inst_ctx->result_images.data();

            if (!invoke_result_callback(inst_ctx, result)) return AV_ERR_INTERNAL;
        }

        return AV_OK;
    } catch (const std::bad_alloc&) {
        return fail(inst_ctx, AV_ERR_OUT_OF_MEMORY, "out of memory in process");
    } catch (...) {
        return fail(inst_ctx, AV_ERR_INTERNAL, "exception in process");
    }
}

int algo_instance_flush(av_algo_instance inst) {
    if (!inst) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "null instance in flush");
    return AV_OK;
}

int algo_last_error(av_algo_instance inst, char* buf, uint32_t buf_len) {
    if (!buf || buf_len == 0) return AV_ERR_INVALID_ARG;
    std::memset(buf, 0, buf_len);

    std::string msg = g_last_error;
    if (inst) {
        auto* inst_ctx = static_cast<InstanceContext*>(inst);
        if (!inst_ctx->last_error_msg.empty()) msg = inst_ctx->last_error_msg;
    }
    if (msg.empty()) msg = "no error recorded";

    std::strncpy(buf, msg.c_str(), buf_len - 1);
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
    .last_error = algo_last_error,
};

} // namespace

extern "C" AV_EXPORT const av_algo_abi* av_algo_get_abi(uint32_t api_version) {
    if (api_version != AV_ALGO_API_VERSION) return nullptr;
    return &g_abi;
}
