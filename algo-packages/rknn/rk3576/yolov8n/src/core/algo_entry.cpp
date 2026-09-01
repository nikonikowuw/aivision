#include "argus/algo.h"
#include "argus/utils/event_id.hpp"
#include "argus/utils/json.hpp"
#include "argus/cv/tracker.hpp"
#include "../preprocess/preprocessor.hpp"
#include "../inference/rknn_runner.hpp"
#include "../postprocess/postprocessor.hpp"
#include "config.hpp"
#include "rules.hpp"
#include "env_config.hpp"

#include <atomic>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <limits>
#include <memory>
#include <mutex>
#include <string>
#include <string_view>
#include <unordered_map>
#include <utility>
#include <vector>

namespace yolov8n {

constexpr const char* kAlgorithmId = "general_detection";
constexpr const char* kVersion = "1.0.0";
constexpr const char* kPlatformId = "rk3576-rknn";
constexpr const char* kAlarmTypeId = "object_detect";
constexpr uint32_t kMinFrameWidth = 320;
constexpr uint32_t kMinFrameHeight = 320;
constexpr uint32_t kMaxFrameWidth = 3840;
constexpr uint32_t kMaxFrameHeight = 2160;

struct LibraryContext {
    std::string package_root;
    std::string platform_id;
    std::string model_path;
    PackageEnvConfig env_config;
    av_log_fn log = nullptr;
    void* log_user = nullptr;
    std::atomic_bool color_fallback_logged{false};
    std::shared_ptr<RknnRunner> model_runner;
};

struct InstanceContext {
    LibraryContext* lib = nullptr;
    uint32_t mode = AV_INSTANCE_NORMAL;
    std::string instance_id;
    std::string instance_run_id;
    InstanceConfig config;
    std::vector<RuleState> rules;
    std::unordered_map<int64_t, av_point> previous_points;
    std::unordered_map<int64_t, uint32_t> missed_frames;
    const av_frame_ops* frame_ops = nullptr;
    const av_image_ops* image_ops = nullptr;
    av_algo_result_cb on_result = nullptr;
    void* result_user = nullptr;
    std::string last_error_msg;
    uint32_t event_counter = 0;
    bool self_test_emitted = false;
    std::unordered_map<int64_t, int64_t> track_alarm_cooldown_;
    argus::cv::SimpleTracker tracker;
    std::shared_ptr<RknnInstanceContext> rknn_instance;
};

} // namespace yolov8n

using namespace yolov8n;

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

template <typename Fn>
int invoke_abi(InstanceContext* inst, Fn&& fn) noexcept {
    try {
        return fn();
    } catch (const std::bad_alloc&) {
        return fail(inst, AV_ERR_OUT_OF_MEMORY, "algorithm call ran out of memory");
    } catch (const std::exception& error) {
        set_error(inst, error.what());
        return AV_ERR_INTERNAL;
    } catch (...) {
        return fail(inst, AV_ERR_INTERNAL, "algorithm call raised an unknown exception");
    }
}

template <typename Fn>
int invoke_abi(LibraryContext* lib, Fn&& fn) noexcept {
    try {
        return fn();
    } catch (const std::bad_alloc&) {
        return fail(lib, AV_ERR_OUT_OF_MEMORY, "algorithm call ran out of memory");
    } catch (const std::exception& error) {
        set_error(lib, error.what());
        return AV_ERR_INTERNAL;
    } catch (...) {
        return fail(lib, AV_ERR_INTERNAL, "algorithm call raised an unknown exception");
    }
}

template <size_t N>
void copy_text(char (&destination)[N], const char* source) {
    std::memset(destination, 0, N);
    if (source) {
        std::strncpy(destination, source, N - 1);
    }
}

bool validate_caps_input(const av_frame_caps* caps) {
    if (!caps || caps->size < sizeof(av_frame_caps) || caps->api_version != AV_ALGO_API_VERSION) return false;
    return caps->pixel_format_count <= 8 && caps->memory_type_count <= 4;
}

bool contains(const uint32_t* values, uint32_t count, uint32_t value) {
    for (uint32_t i = 0; i < count; ++i) {
        if (values[i] == value) return true;
    }
    return false;
}

bool validate_frame(const av_frame_desc* frame) {
    if (!frame || frame->size < sizeof(av_frame_desc) || frame->api_version != AV_ALGO_API_VERSION) return false;
    if (frame->width < kMinFrameWidth || frame->height < kMinFrameHeight ||
        frame->width > kMaxFrameWidth || frame->height > kMaxFrameHeight ||
        frame->alloc_width < frame->width || frame->alloc_height < frame->height) {
        return false;
    }
    if (frame->stride[0] <= 0) {
        return false;
    }
    if (frame->color_primaries != AV_COLOR_PRIM_UNSPECIFIED && frame->color_primaries != AV_COLOR_PRIM_BT709) return false;
    if (frame->color_transfer != AV_COLOR_TRC_UNSPECIFIED && frame->color_transfer != AV_COLOR_TRC_BT709) return false;
    if (frame->color_matrix != AV_COLOR_MAT_UNSPECIFIED && frame->color_matrix != AV_COLOR_MAT_BT709) return false;
    if (frame->color_range != AV_COLOR_RANGE_UNSPECIFIED && frame->color_range != AV_COLOR_RANGE_LIMITED &&
        frame->color_range != AV_COLOR_RANGE_FULL) return false;
    return true;
}

void log_color_fallback_once(InstanceContext* inst, const av_frame_desc* frame) noexcept {
    if (!inst || !inst->lib || !frame) return;
    const bool missing = frame->color_primaries == AV_COLOR_PRIM_UNSPECIFIED ||
                         frame->color_transfer == AV_COLOR_TRC_UNSPECIFIED ||
                         frame->color_matrix == AV_COLOR_MAT_UNSPECIFIED ||
                         frame->color_range == AV_COLOR_RANGE_UNSPECIFIED;
    if (missing && !inst->lib->color_fallback_logged.exchange(true)) {
        log_message(inst->lib, 3, "frame color metadata is incomplete; using BT.709 limited fallback");
    }
}

bool run_pipeline(InstanceContext* inst, const av_frame_desc* frame,
                  std::vector<argus::cv::DetectionBox>& objects) {
    log_color_fallback_once(inst, frame);

    PreparedInput prep_input{};
    if (!Preprocessor::prepare_input(frame, inst->image_ops, 640, 384, prep_input)) {
        set_error(inst, "failed to preprocess frame");
        return false;
    }

    struct PrepGuard {
        PreparedInput& in;
        const av_image_ops* ops;
        ~PrepGuard() { Preprocessor::release_input(in, ops); }
    } guard{prep_input, inst->image_ops};

    std::vector<RknnOutputBuffer> raw_outputs;
    uint32_t input_size = prep_input.view.width * prep_input.view.height * 3;
    if (!inst->rknn_instance->run(prep_input.view.data, input_size, raw_outputs)) {
        set_error(inst, "RKNN inference execution failed");
        return false;
    }

    const auto raw_objects = Postprocessor::decode(
        raw_outputs,
        prep_input.letterbox,
        inst->config.confidence_threshold,
        inst->config.iou_threshold,
        inst->config.enabled_classes_mask,
        frame->width,
        frame->height);

    objects = inst->tracker.update(raw_objects);
    objects = apply_rules(inst->rules, inst->previous_points, inst->missed_frames, objects);
    return true;
}

int yolo_library_open_impl(const av_algo_library_args* args, av_algo_library* out) {
    if (!args || !out) return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INVALID_ARG, "library_open arguments are null");
    *out = nullptr;
    if (args->size < sizeof(av_algo_library_args)) return AV_ERR_INVALID_ARG;
    if (args->api_version != AV_ALGO_API_VERSION) return AV_ERR_UNSUPPORTED_API;
    if (!args->package_root || !args->platform_id || std::string(args->platform_id) != kPlatformId) {
        return fail(static_cast<LibraryContext*>(nullptr), AV_ERR_INVALID_ARG, "library_open platform arguments are invalid");
    }

    auto lib = std::make_unique<LibraryContext>();
    lib->package_root = args->package_root;
    lib->platform_id = args->platform_id;
    lib->log = args->log;
    lib->log_user = args->log_user;

    // Load private package .env
    lib->env_config = load_package_env(lib->package_root);

    // Resolve model path relative to package_root if not absolute
    if (!lib->env_config.model_path.empty() && lib->env_config.model_path[0] == '/') {
        lib->model_path = lib->env_config.model_path;
    } else {
        lib->model_path = lib->package_root + "/" + lib->env_config.model_path;
    }

    // Load labels
    std::string labels_path = lib->package_root + "/model/coco_80_labels_list.txt";
    std::ifstream lf(labels_path);
    if (lf.is_open()) {
        std::vector<std::string> labels;
        std::string line;
        while (std::getline(lf, line)) {
            if (!line.empty() && line.back() == '\r') line.pop_back();
            if (!line.empty()) labels.push_back(line);
        }
        Postprocessor::set_labels(labels);
    }

    lib->model_runner = std::make_shared<RknnRunner>(lib->log, lib->log_user);
    if (!lib->model_runner->load_model(lib->model_path)) {
        return fail(lib.get(), AV_ERR_MODEL_LOAD_FAILED, "failed to load RKNN model");
    }
    *out = lib.release();
    return AV_OK;
}

int yolo_library_open(const av_algo_library_args* args, av_algo_library* out) noexcept {
    return invoke_abi(static_cast<LibraryContext*>(nullptr), [&] { return yolo_library_open_impl(args, out); });
}

int yolo_library_query_impl(av_algo_library lib_handle, av_algo_library_info* out) {
    if (!lib_handle || !out) return fail(static_cast<LibraryContext*>(lib_handle), AV_ERR_INVALID_ARG, "library_query arguments are invalid");
    if (out->size < sizeof(av_algo_library_info)) return AV_ERR_INVALID_ARG;
    if (out->api_version != AV_ALGO_API_VERSION) return AV_ERR_UNSUPPORTED_API;
    const uint32_t capacity = out->size;
    auto* lib = static_cast<LibraryContext*>(lib_handle);
    out->api_version = AV_ALGO_API_VERSION;
    copy_text(out->algorithm_id, kAlgorithmId);
    copy_text(out->version, kVersion);
    copy_text(out->algorithm_type, "object_detection");
    copy_text(out->alarm_type_id, kAlarmTypeId);
    out->size = capacity;
    (void)lib;
    return AV_OK;
}

int yolo_library_query(av_algo_library lib_handle, av_algo_library_info* out) noexcept {
    return invoke_abi(static_cast<LibraryContext*>(lib_handle), [&] { return yolo_library_query_impl(lib_handle, out); });
}

int yolo_library_close_impl(av_algo_library lib_handle) {
    if (!lib_handle) return AV_ERR_INVALID_ARG;
    delete static_cast<LibraryContext*>(lib_handle);
    return AV_OK;
}

int yolo_library_close(av_algo_library lib_handle) noexcept {
    return invoke_abi(static_cast<LibraryContext*>(lib_handle), [&] { return yolo_library_close_impl(lib_handle); });
}

int yolo_instance_create_impl(av_algo_library lib_handle, const av_algo_instance_args* args, av_algo_instance* out) {
    if (!lib_handle || !args || !out) return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, "instance_create arguments are invalid");
    *out = nullptr;
    if (args->size < sizeof(av_algo_instance_args)) return AV_ERR_INVALID_ARG;
    if (args->api_version != AV_ALGO_API_VERSION) return AV_ERR_UNSUPPORTED_API;
    if (args->mode != AV_INSTANCE_NORMAL && args->mode != AV_INSTANCE_INSTALL_SELF_TEST) return AV_ERR_INVALID_ARG;
    if (args->config_json_len > AV_MAX_RESULT_JSON_BYTES ||
        (args->config_json_len > 0 && !args->config_json)) return AV_ERR_INVALID_ARG;
    if (args->mode == AV_INSTANCE_NORMAL && args->config_json_len == 0) {
        return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_CONFIG_INVALID,
                    "normal instances require the complete algorithm configuration");
    }
    if (args->mode == AV_INSTANCE_INSTALL_SELF_TEST && !args->on_result) {
        return AV_ERR_INVALID_ARG;
    }
    if (args->frame_ops && (args->frame_ops->size < sizeof(av_frame_ops) ||
                            args->frame_ops->api_version != AV_ALGO_API_VERSION ||
                            !args->frame_ops->retain || !args->frame_ops->release)) {
        return AV_ERR_INVALID_ARG;
    }
    if (args->image_ops && (args->image_ops->size < sizeof(av_image_ops) ||
                            args->image_ops->api_version != AV_ALGO_API_VERSION ||
                            !args->image_ops->alloc || !args->image_ops->convert ||
                            !args->image_ops->pad || !args->image_ops->free)) {
        return AV_ERR_INVALID_ARG;
    }

    auto* lib = static_cast<LibraryContext*>(lib_handle);
    auto instance = std::make_unique<InstanceContext>();
    instance->lib = lib;
    instance->mode = args->mode;
    instance->instance_id = args->instance_id ? args->instance_id : "";
    instance->instance_run_id = args->instance_run_id ? args->instance_run_id : "";
    instance->frame_ops = args->frame_ops;
    instance->image_ops = args->image_ops;
    instance->on_result = args->on_result;
    instance->result_user = args->result_user;

    // Create RKNN instance execution context (dup_context)
    instance->rknn_instance = lib->model_runner->create_instance();
    if (!instance->rknn_instance) {
        return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INTERNAL, "failed to create RKNN instance context");
    }

    if (args->config_json_len == 0) {
        instance->config = InstanceConfig{};
        instance->config.confidence_threshold = lib->env_config.conf_thresh;
        instance->config.iou_threshold = lib->env_config.iou_thresh;
        instance->config.update_mask();
    } else {
        std::string error;
        if (!parse_instance_config(std::string_view(args->config_json, args->config_json_len), instance->config, error)) {
            return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_CONFIG_INVALID, error.c_str());
        }
    }
    std::string rule_error;
    if (!validate_and_copy_rules(args->rules, args->rule_count, instance->rules, rule_error)) {
        return fail(static_cast<InstanceContext*>(nullptr), AV_ERR_INVALID_ARG, rule_error.c_str());
    }

    *out = instance.release();
    return AV_OK;
}

int yolo_instance_create(av_algo_library lib_handle, const av_algo_instance_args* args, av_algo_instance* out) noexcept {
    return invoke_abi(static_cast<LibraryContext*>(lib_handle), [&] { return yolo_instance_create_impl(lib_handle, args, out); });
}

int yolo_instance_negotiate_impl(av_algo_instance inst_handle, const av_frame_caps* offered, av_frame_caps* accepted) {
    if (!inst_handle || !validate_caps_input(offered) || !validate_caps_input(accepted)) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INVALID_ARG, "frame capabilities are invalid");
    }
    if (offered->required_opaque_kind != AV_OPAQUE_NONE && offered->required_opaque_kind != AV_OPAQUE_DMABUF) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INCOMPATIBLE_FRAME, "offered opaque kind is unsupported");
    }
    if (!contains(offered->pixel_formats, offered->pixel_format_count, AV_PIX_NV12) &&
        !contains(offered->pixel_formats, offered->pixel_format_count, AV_PIX_RGB24)) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INCOMPATIBLE_FRAME, "offered frame has no supported format option");
    }

    bool has_surface = contains(offered->memory_types, offered->memory_type_count, AV_MEM_PLATFORM_SURFACE);
    bool has_host = contains(offered->memory_types, offered->memory_type_count, AV_MEM_HOST);
    if (!has_surface && !has_host) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INCOMPATIBLE_FRAME, "offered memory types have no supported option");
    }

    const uint32_t offered_min_width = offered->min_width == 0 ? 1 : offered->min_width;
    const uint32_t offered_min_height = offered->min_height == 0 ? 1 : offered->min_height;
    const uint32_t offered_max_width = offered->max_width == 0 ? kMaxFrameWidth : offered->max_width;
    const uint32_t offered_max_height = offered->max_height == 0 ? kMaxFrameHeight : offered->max_height;
    if (offered_min_width > offered_max_width || offered_min_height > offered_max_height ||
        offered_min_width > kMaxFrameWidth || offered_min_height > kMaxFrameHeight ||
        offered_max_width < kMinFrameWidth || offered_max_height < kMinFrameHeight) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INCOMPATIBLE_FRAME, "offered frame dimensions do not intersect");
    }

    const uint32_t capacity = accepted->size;
    *accepted = {};
    accepted->size = capacity;
    accepted->api_version = AV_ALGO_API_VERSION;
    accepted->pixel_format_count = 1;
    if (contains(offered->pixel_formats, offered->pixel_format_count, AV_PIX_NV12)) {
        accepted->pixel_formats[0] = AV_PIX_NV12;
    } else {
        accepted->pixel_formats[0] = AV_PIX_RGB24;
    }

    uint32_t mem_count = 0;
    if (has_surface) {
        accepted->memory_types[mem_count++] = AV_MEM_PLATFORM_SURFACE;
    }
    if (has_host) {
        accepted->memory_types[mem_count++] = AV_MEM_HOST;
    }
    accepted->memory_type_count = mem_count;
    accepted->required_opaque_kind = has_surface ? AV_OPAQUE_DMABUF : AV_OPAQUE_NONE;

    accepted->min_width = std::max(kMinFrameWidth, offered_min_width);
    accepted->min_height = std::max(kMinFrameHeight, offered_min_height);
    accepted->max_width = std::min(kMaxFrameWidth, offered_max_width);
    accepted->max_height = std::min(kMaxFrameHeight, offered_max_height);
    return AV_OK;
}

int yolo_instance_negotiate(av_algo_instance inst_handle, const av_frame_caps* offered, av_frame_caps* accepted) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] {
        return yolo_instance_negotiate_impl(inst_handle, offered, accepted);
    });
}

int yolo_instance_update_config_impl(av_algo_instance inst_handle, const char* json, uint32_t len) {
    if (!inst_handle || !json || len == 0 || len > AV_MAX_RESULT_JSON_BYTES) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INVALID_ARG, "config update arguments are invalid");
    }
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    InstanceConfig candidate = inst->config;
    std::string error;
    if (!parse_instance_config(std::string_view(json, len), candidate, error)) {
        return fail(inst, AV_ERR_CONFIG_INVALID, error.c_str());
    }
    inst->config = candidate;
    return AV_OK;
}

int yolo_instance_update_config(av_algo_instance inst_handle, const char* json, uint32_t len) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] {
        return yolo_instance_update_config_impl(inst_handle, json, len);
    });
}

int yolo_instance_set_rules_impl(av_algo_instance inst_handle, const av_rule* rules, uint32_t count) {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    std::vector<RuleState> candidate;
    std::string error;
    if (!validate_and_copy_rules(rules, count, candidate, error)) {
        return fail(inst, AV_ERR_INVALID_ARG, error.c_str());
    }
    inst->rules = std::move(candidate);
    inst->previous_points.clear();
    inst->missed_frames.clear();
    return AV_OK;
}

int yolo_instance_set_rules(av_algo_instance inst_handle, const av_rule* rules, uint32_t count) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] {
        return yolo_instance_set_rules_impl(inst_handle, rules, count);
    });
}

int yolo_instance_process_impl(av_algo_instance inst_handle, const av_frame_desc* frame) {
    if (!inst_handle || !validate_frame(frame)) {
        return fail(static_cast<InstanceContext*>(inst_handle), AV_ERR_INVALID_ARG, "frame descriptor is invalid or unsupported");
    }
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    std::vector<argus::cv::DetectionBox> objects;
    if (!run_pipeline(inst, frame, objects)) return AV_ERR_INFERENCE_FAILED;

    if (inst->mode == AV_INSTANCE_INSTALL_SELF_TEST) {
        if (inst->self_test_emitted) return fail(inst, AV_ERR_INVALID_ARG, "self-test instance was processed more than once");
        const std::vector<std::string> stages = {"preprocess", "inference", "postprocess", "serialize"};
        const std::string result_json = argus::utils::serialize_self_test_json(stages, static_cast<uint32_t>(objects.size()));
        if (result_json.size() > AV_MAX_RESULT_JSON_BYTES) return fail(inst, AV_ERR_INTERNAL, "self-test result is too large");
        av_algo_result result{};
        result.size = sizeof(result);
        result.api_version = AV_ALGO_API_VERSION;
        result.kind = AV_RESULT_SELF_TEST;
        result.frame_id = frame->frame_id;
        result.json = result_json.c_str();
        result.json_len = static_cast<uint32_t>(result_json.size());
        result.image_count = 0;
        result.images = nullptr;
        inst->self_test_emitted = true;
        inst->on_result(&result, inst->result_user);
        return AV_OK;
    }

    if (objects.empty() || !inst->on_result) return AV_OK;

    const int64_t current_time_ns = frame->wall_time_ns > 0 ? frame->wall_time_ns : frame->pts_ns;
    constexpr int64_t kCooldownNs = 5LL * 1000 * 1000 * 1000; // 默认 5 秒冷却

    std::vector<argus::cv::DetectionBox> alarm_objects;
    alarm_objects.reserve(objects.size());

    for (const auto& object : objects) {
        if (object.track_id > 0) {
            auto it = inst->track_alarm_cooldown_.find(object.track_id);
            if (it != inst->track_alarm_cooldown_.end() && (current_time_ns - it->second) < kCooldownNs) {
                // 处于冷却期内，跳过该目标的告警触发
                continue;
            }
            inst->track_alarm_cooldown_[object.track_id] = current_time_ns;
        }
        alarm_objects.push_back(object);
    }

    if (alarm_objects.empty()) return AV_OK;

    if (inst->event_counter == std::numeric_limits<uint32_t>::max()) {
        return fail(inst, AV_ERR_INTERNAL, "event counter exhausted");
    }
    ++inst->event_counter;
    const std::string batch_id = argus::utils::EventIdGenerator::next_event_id(inst->event_counter);

    const std::string result_json = argus::utils::serialize_alarm_json(batch_id, kAlarmTypeId, alarm_objects);
    if (result_json.size() > AV_MAX_RESULT_JSON_BYTES) return fail(inst, AV_ERR_INTERNAL, "alarm result is too large");

    // 请求全景大图 [0, 0, 1, 1]，批次内所有目标共享该抓拍
    av_algo_image_req request{};
    request.size = sizeof(request);
    request.api_version = AV_ALGO_API_VERSION;
    request.x = 0.0f;
    request.y = 0.0f;
    request.w = 1.0f;
    request.h = 1.0f;
    request.purpose = 0; // 0: 全景大图

    av_algo_result result{};
    result.size = sizeof(result);
    result.api_version = AV_ALGO_API_VERSION;
    result.kind = AV_RESULT_ALARM;
    result.frame_id = frame->frame_id;
    result.json = result_json.c_str();
    result.json_len = static_cast<uint32_t>(result_json.size());
    result.image_count = 1;
    result.images = &request;
    inst->on_result(&result, inst->result_user);
    return AV_OK;
}

int yolo_instance_process(av_algo_instance inst_handle, const av_frame_desc* frame) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] {
        return yolo_instance_process_impl(inst_handle, frame);
    });
}

int yolo_instance_flush_impl(av_algo_instance inst_handle) {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    inst->tracker.reset();
    inst->previous_points.clear();
    inst->missed_frames.clear();
    inst->track_alarm_cooldown_.clear();
    return AV_OK;
}

int yolo_instance_flush(av_algo_instance inst_handle) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] { return yolo_instance_flush_impl(inst_handle); });
}

int yolo_instance_destroy_impl(av_algo_instance inst_handle) {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    delete static_cast<InstanceContext*>(inst_handle);
    return AV_OK;
}

int yolo_instance_destroy(av_algo_instance inst_handle) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] { return yolo_instance_destroy_impl(inst_handle); });
}

int yolo_last_error_impl(av_algo_instance inst_handle, char* buf, uint32_t cap) {
    const std::string& message = inst_handle ? static_cast<InstanceContext*>(inst_handle)->last_error_msg : g_last_error;
    const uint32_t required = static_cast<uint32_t>(message.size());
    if (buf && cap > 0) {
        const size_t copy_size = std::min<size_t>(message.size(), cap - 1);
        std::memcpy(buf, message.data(), copy_size);
        buf[copy_size] = '\0';
    }
    return static_cast<int>(required);
}

int yolo_last_error(av_algo_instance inst_handle, char* buf, uint32_t cap) noexcept {
    return invoke_abi(static_cast<InstanceContext*>(inst_handle), [&] { return yolo_last_error_impl(inst_handle, buf, cap); });
}

} // namespace

static av_algo_abi g_yolo_abi = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = yolo_library_open,
    .library_query = yolo_library_query,
    .library_close = yolo_library_close,
    .instance_create = yolo_instance_create,
    .instance_negotiate = yolo_instance_negotiate,
    .instance_update_config = yolo_instance_update_config,
    .instance_set_rules = yolo_instance_set_rules,
    .instance_process = yolo_instance_process,
    .instance_flush = yolo_instance_flush,
    .instance_destroy = yolo_instance_destroy,
    .last_error = yolo_last_error
};

extern "C" {
AV_EXPORT const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return nullptr;
    return &g_yolo_abi;
}
}
