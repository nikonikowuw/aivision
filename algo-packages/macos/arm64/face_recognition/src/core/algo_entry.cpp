/**
 * @file algo_entry.cpp
 * @brief C ABI 统一导出接口、Library/Instance 生命周期管理与人脸识别流水线驱动
 */

#include "argus/algo.h"
#include "argus/utils/event_id.hpp"
#include "argus/utils/json.hpp"
#include "argus/cv/tracker.hpp"
#include "argus/utils/env.hpp"
#include "../preprocess/preprocessor.hpp"
#include "../inference/model_inference.hpp"
#include "../postprocess/postprocessor.hpp"
#include "config.hpp"
#include "profile_stats.hpp"

#import <CoreGraphics/CoreGraphics.h>
#import <ImageIO/ImageIO.h>
#import <Foundation/Foundation.h>
#import <Accelerate/Accelerate.h>

#include <atomic>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <filesystem>
#include <limits>
#include <memory>
#include <mutex>
#include <string>
#include <string_view>
#include <future>
#include <iostream>
#include <unordered_map>
#include <utility>
#include <vector>

namespace face_recognition {

constexpr const char* kAlgorithmId = "face_recognition";
constexpr const char* kVersion = "1.0.0";
constexpr uint32_t kMinFrameWidth = 320;
constexpr uint32_t kMinFrameHeight = 320;
constexpr uint32_t kMaxFrameWidth = 3840;
constexpr uint32_t kMaxFrameHeight = 2160;

struct LibraryContext {
    std::string package_root;
    std::string platform_id;
    std::string yolo_path = "model/yolov8n.mlpackage";
    std::string scrfd_path = "model/scrfd_10g_bnkps.mlpackage";
    std::string scrfd_reg_path = "model/scrfd_10g_640x640.mlpackage";
    std::string glintr_path = "model/glintr100.mlpackage";
    InstanceConfig default_config;

    av_log_fn log = nullptr;
    void* log_user = nullptr;
    std::shared_ptr<ModelInferenceManager> model_manager;
};

struct TrackQualityState {
    uint32_t hit_count = 0;
    uint32_t recognition_count = 0;
    uint64_t last_extracted_frame = 0;
    float highest_quality = 0.0f;
    std::string cached_embedding;
};

struct InstanceContext {
    LibraryContext* lib = nullptr;
    uint32_t mode = AV_INSTANCE_NORMAL;
    std::string instance_id;
    std::string instance_run_id;
    InstanceConfig config;

    const av_frame_ops* frame_ops = nullptr;
    const av_image_ops* image_ops = nullptr;
    av_algo_result_cb on_result = nullptr;
    void* result_user = nullptr;
    std::string last_error_msg;
    bool self_test_emitted = false;
    uint64_t last_frame_id = 0;
    bool has_received_frame = false;

    argus::cv::SimpleTracker tracker;
    std::unordered_map<int64_t, TrackQualityState> track_quality_map;
};

} // namespace face_recognition

using namespace face_recognition;

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

void copy_text(char* dst, size_t capacity, std::string_view src) noexcept {
    if (!dst || capacity == 0) return;
    const size_t copy_len = std::min(capacity - 1, src.size());
    if (copy_len > 0) {
        std::memcpy(dst, src.data(), copy_len);
    }
    dst[copy_len] = '\0';
}

template <size_t N>
void copy_text(char (&dst)[N], std::string_view src) noexcept {
    copy_text(dst, N, src);
}

// 计算人脸中心是否落在人体框内 (支持顶部 20% 自适应上延，防止人体框在锁骨/下巴截断时头部人脸脱靶)
bool face_center_in_person(const FaceDetection& face, const argus::cv::DetectionBox& person, uint32_t orig_w, uint32_t orig_h) {
    const float face_cx = (face.x1 + face.x2) * 0.5f;
    const float face_cy = (face.y1 + face.y2) * 0.5f;

    const float person_x1 = person.x * static_cast<float>(orig_w);
    const float person_y1 = person.y * static_cast<float>(orig_h);
    const float person_x2 = (person.x + person.w) * static_cast<float>(orig_w);
    const float person_y2 = (person.y + person.h) * static_cast<float>(orig_h);

    const float person_h = person_y2 - person_y1;
    const float person_w = person_x2 - person_x1;

    const float expanded_x1 = person_x1 - person_w * 0.05f;
    const float expanded_x2 = person_x2 + person_w * 0.05f;
    const float expanded_y1 = person_y1 - person_h * 0.20f;

    return (face_cx >= expanded_x1 && face_cx <= expanded_x2 &&
            face_cy >= expanded_y1 && face_cy <= person_y2);
}

// 计算人脸与人体的交并比
float compute_face_person_iou(const FaceDetection& face, const argus::cv::DetectionBox& person, uint32_t orig_w, uint32_t orig_h) {
    float person_x1 = person.x * static_cast<float>(orig_w);
    float person_y1 = person.y * static_cast<float>(orig_h);
    float person_x2 = (person.x + person.w) * static_cast<float>(orig_w);
    float person_y2 = (person.y + person.h) * static_cast<float>(orig_h);

    float xx1 = std::max(face.x1, person_x1);
    float yy1 = std::max(face.y1, person_y1);
    float xx2 = std::min(face.x2, person_x2);
    float yy2 = std::min(face.y2, person_y2);

    float inter_w = std::max(0.0f, xx2 - xx1);
    float inter_h = std::max(0.0f, yy2 - yy1);
    float inter_area = inter_w * inter_h;

    float face_area = (face.x2 - face.x1) * (face.y2 - face.y1);
    float person_area = (person_x2 - person_x1) * (person_y2 - person_y1);
    float union_area = face_area + person_area - inter_area;

    if (union_area <= 0.0f) return 0.0f;
    return inter_area / union_area;
}

// 评估人脸质量 (0.0 ~ 100.0)
float evaluate_face_quality(const FaceDetection& face, uint32_t orig_w, uint32_t orig_h) {
    float fw = face.x2 - face.x1;
    float fh = face.y2 - face.y1;
    if (fw <= 8.0f || fh <= 8.0f) return 0.0f;

    // 1. 尺寸得分 (40px 及格, 120px+ 满分 40 分)
    float min_dim = std::min(fw, fh);
    float size_score = std::clamp((min_dim - 20.0f) / 100.0f, 0.0f, 1.0f) * 40.0f;

    // 2. 检测置信度得分 (满分 20 分)
    float conf_score = std::clamp((face.score - 0.5f) / 0.5f, 0.0f, 1.0f) * 20.0f;

    // 3. 姿态与对称性 (满分 30 分)
    // 关键点: 0=左眼, 1=右眼, 2=鼻尖, 3=左嘴角, 4=右嘴角
    float lx = face.landmarks[0], ly = face.landmarks[1];
    float rx = face.landmarks[2], ry = face.landmarks[3];
    float nx = face.landmarks[4], ny = face.landmarks[5];

    float eye_dist = std::hypot(rx - lx, ry - ly);
    float pose_score = 0.0f;
    if (eye_dist > 4.0f) {
        // Roll (偏头角)：两眼连线与水平夹角
        float roll_sin = std::abs(ry - ly) / eye_dist; // 0=平直, 1=垂直
        float roll_penalty = std::clamp(roll_sin / 0.5f, 0.0f, 1.0f); // 30度以内轻微扣分

        // Yaw (左右偏航)：鼻尖到左眼与鼻尖到右眼的水平距离比例
        float d_left = std::abs(nx - lx);
        float d_right = std::abs(rx - nx);
        float yaw_ratio = (d_left < d_right) ? (d_left / std::max(1e-3f, d_right)) : (d_right / std::max(1e-3f, d_left));
        // 正脸 yaw_ratio 接近 1.0，侧脸接近 0
        float symmetry = std::clamp(yaw_ratio, 0.0f, 1.0f);

        pose_score = (1.0f - roll_penalty * 0.5f) * symmetry * 30.0f;
    }

    // 4. 边缘惩罚 (靠近画幅边缘扣 10 分)
    float margin_x = std::min(face.x1, static_cast<float>(orig_w) - face.x2);
    float margin_y = std::min(face.y1, static_cast<float>(orig_h) - face.y2);
    float margin_min = std::min(margin_x, margin_y);
    float margin_score = std::clamp(margin_min / 30.0f, 0.0f, 1.0f) * 10.0f;

    return size_score + conf_score + pose_score + margin_score;
}

constexpr uint32_t kImagePurposeFaceCrop = 0; // 高清人脸特写裁剪 ROI 图像用途

// 评估抓拍优选与提取人脸特征向量并更新轨迹状态
void process_face_feature_and_track_state(
    InstanceContext* inst,
    const av_frame_desc* frame,
    PreprocessResult& prep_res,
    const FaceDetection* best_face,
    RecognizedPerson& rp,
    TrackQualityState& track_state) {

    if (!best_face) return;

    const uint32_t orig_w = prep_res.orig_width;
    const uint32_t orig_h = prep_res.orig_height;

    rp.has_face = true;
    float fx = std::clamp(best_face->x1 / static_cast<float>(orig_w), 0.0f, 0.9999f);
    float fy = std::clamp(best_face->y1 / static_cast<float>(orig_h), 0.0f, 0.9999f);
    float fw = std::clamp((best_face->x2 - best_face->x1) / static_cast<float>(orig_w), 1e-4f, 1.0f - fx);
    float fh = std::clamp((best_face->y2 - best_face->y1) / static_cast<float>(orig_h), 1e-4f, 1.0f - fy);
    rp.face_bbox[0] = fx;
    rp.face_bbox[1] = fy;
    rp.face_bbox[2] = fw;
    rp.face_bbox[3] = fh;
    rp.face_confidence = std::clamp(best_face->score, 0.0f, 1.0f);
    for (int k = 0; k < 5; ++k) {
        rp.face_landmarks[k * 2 + 0] = std::clamp(best_face->landmarks[k * 2 + 0] / static_cast<float>(orig_w), 0.0f, 1.0f);
        rp.face_landmarks[k * 2 + 1] = std::clamp(best_face->landmarks[k * 2 + 1] / static_cast<float>(orig_h), 0.0f, 1.0f);
    }

    float face_w_px = best_face->x2 - best_face->x1;
    float face_h_px = best_face->y2 - best_face->y1;
    float min_dim = std::min(face_w_px, face_h_px);
    float q_score = evaluate_face_quality(*best_face, orig_w, orig_h);
    rp.face_quality = std::clamp(q_score / 100.0f, 0.0f, 1.0f);

    bool need_extract = true;
    if (inst->mode != AV_INSTANCE_INSTALL_SELF_TEST && inst->config.feature_mode == "best_shot") {
        // 1. 防抖与虚警过滤：轨迹存活帧数需达到 track_confirm_frames
        if (track_state.hit_count < inst->config.track_confirm_frames) {
            need_extract = false;
        } else if (min_dim < static_cast<float>(inst->config.min_face_size) || q_score < inst->config.quality_threshold) {
            // 2. 人脸最低像素尺寸与质量及格线
            need_extract = false;
        } else if (track_state.recognition_count >= inst->config.max_recognitions_per_track) {
            // 3. 单轨迹最大抓拍提取特征次数限制
            need_extract = false;
        } else if (track_state.last_extracted_frame > 0) {
            // 4. 已有特征提取记录：检查是否显著提升或达到重采样间隔
            bool interval_reached = (inst->config.reextract_interval_frames > 0 &&
                (frame->frame_id - track_state.last_extracted_frame >= inst->config.reextract_interval_frames));
            bool significantly_better = (q_score >= track_state.highest_quality + inst->config.quality_update_margin);

            if (!interval_reached && !significantly_better) {
                need_extract = false;
            }
        }
    }

    if (need_extract) {
        // 从 NV12 原图双平面直接做五点相似变换对齐截脸 -> 112x112 (零全图 RGB 分配)
        ImageBuffer face_112;
        std::string align_err;
        if (Preprocessor::align_face_112x112(prep_res, best_face->landmarks.data(), face_112, align_err)) {
            // 运行 GLINTR100 提取特征
            GlintrOutput glintr_out;
            std::string glintr_err;
            if (inst->lib->model_manager->run_glintr(face_112.data.data(), glintr_out, glintr_err)) {
                std::string emb_b64;
                std::string emb_err;
#if ENABLE_PROFILING
                auto t0_enc = std::chrono::steady_clock::now();
#endif
                if (Postprocessor::process_and_encode_embedding(glintr_out.embedding, emb_b64, emb_err)) {
#if ENABLE_PROFILING
                    auto t1_enc = std::chrono::steady_clock::now();
                    auto* prof = get_active_profile_record();
                    if (prof) {
                        prof->embedding_encode_ms += std::chrono::duration<double, std::milli>(t1_enc - t0_enc).count();
                    }
#endif
                    rp.embedding_base64 = emb_b64;
                    track_state.recognition_count++;
                    track_state.last_extracted_frame = frame->frame_id;
                    if (q_score > track_state.highest_quality) track_state.highest_quality = q_score;
                    track_state.cached_embedding = std::move(emb_b64);
                }
            }
        }
    }
}

} // namespace

static int library_open(const av_algo_library_args* args, av_algo_library* out) noexcept {
    if (!args || !out || args->size < sizeof(av_algo_library_args) || args->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    *out = nullptr;

    auto lib = std::make_unique<LibraryContext>();
    lib->package_root = args->package_root ? args->package_root : "";
    lib->platform_id = args->platform_id ? args->platform_id : "";
    lib->log = args->log;
    lib->log_user = args->log_user;

    return invoke_abi(lib.get(), [&] {
        // 读取 package_root/.env 私有配置
        std::string env_path = lib->package_root + "/.env";
        auto env_vars = argus::utils::EnvReader::load_file(env_path);

        if (env_vars.contains("YOLO_MODEL_PATH")) lib->yolo_path = env_vars["YOLO_MODEL_PATH"];
        if (env_vars.contains("SCRFD_MODEL_PATH")) lib->scrfd_path = env_vars["SCRFD_MODEL_PATH"];
        if (env_vars.contains("SCRFD_REG_MODEL_PATH")) lib->scrfd_reg_path = env_vars["SCRFD_REG_MODEL_PATH"];
        if (env_vars.contains("GLINTR_MODEL_PATH")) {
            lib->glintr_path = env_vars["GLINTR_MODEL_PATH"];
        } else {
            std::filesystem::path root(lib->package_root);
            if (!std::filesystem::exists(root / lib->glintr_path) && std::filesystem::exists(root / "model/adaface_ir101.mlpackage")) {
                lib->glintr_path = "model/adaface_ir101.mlpackage";
            }
        }

        if (env_vars.contains("ENABLE_PERSON_DETECTION")) {
            lib->default_config.enable_person_detection = (env_vars["ENABLE_PERSON_DETECTION"] == "true" || env_vars["ENABLE_PERSON_DETECTION"] == "1");
        }
        if (env_vars.contains("PERSON_DETECTION_THRESHOLD")) {
            lib->default_config.person_detection_threshold = std::stof(env_vars["PERSON_DETECTION_THRESHOLD"]);
        }
        if (env_vars.contains("FACE_DETECTION_THRESHOLD")) {
            lib->default_config.face_detection_threshold = std::stof(env_vars["FACE_DETECTION_THRESHOLD"]);
        }
        if (env_vars.contains("FACE_NMS_THRESHOLD")) {
            lib->default_config.face_nms_threshold = std::stof(env_vars["FACE_NMS_THRESHOLD"]);
        }
        if (env_vars.contains("MAX_PERSON_COUNT")) {
            lib->default_config.max_person_count = static_cast<uint32_t>(std::stoul(env_vars["MAX_PERSON_COUNT"]));
        }
        if (env_vars.contains("TRACK_BUFFER")) {
            lib->default_config.track_buffer = static_cast<uint32_t>(std::stoul(env_vars["TRACK_BUFFER"]));
        }
        if (env_vars.contains("TRACK_MATCH_THRESHOLD")) {
            lib->default_config.track_match_threshold = std::stof(env_vars["TRACK_MATCH_THRESHOLD"]);
        }
        if (env_vars.contains("MAX_RECOGNITIONS_PER_TRACK")) {
            lib->default_config.max_recognitions_per_track = static_cast<uint32_t>(std::stoul(env_vars["MAX_RECOGNITIONS_PER_TRACK"]));
        }

        lib->model_manager = std::make_shared<ModelInferenceManager>();
        std::string model_err;
        if (!lib->model_manager->load_models(lib->package_root, lib->yolo_path, lib->scrfd_path, lib->scrfd_reg_path, lib->glintr_path, model_err)) {
            return fail(lib.get(), AV_ERR_MODEL_LOAD_FAILED, ("failed to load models: " + model_err).c_str());
        }

        *out = static_cast<av_algo_library>(lib.release());
        return static_cast<int>(AV_OK);
    });
}

static int library_query(av_algo_library lib_handle, av_algo_library_info* out) noexcept {
    if (!lib_handle || !out || out->size < sizeof(av_algo_library_info) || out->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    copy_text(out->algorithm_id, kAlgorithmId);
    copy_text(out->version, kVersion);
    copy_text(out->algorithm_type, "face_recognition");
    copy_text(out->alarm_type_id, ""); // 识别类包 alarm_type_id 为空字符串
    return AV_OK;
}

static int library_close(av_algo_library lib_handle) noexcept {
    if (!lib_handle) return AV_ERR_INVALID_ARG;
    auto* lib = static_cast<LibraryContext*>(lib_handle);
    delete lib;
    return AV_OK;
}

static int instance_create(av_algo_library lib_handle, const av_algo_instance_args* args, av_algo_instance* out) noexcept {
    if (!lib_handle || !args || !out || args->size < sizeof(av_algo_instance_args) || args->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    *out = nullptr;
    auto* lib = static_cast<LibraryContext*>(lib_handle);

    auto inst = std::make_unique<InstanceContext>();
    inst->lib = lib;
    inst->mode = args->mode;
    inst->instance_id = args->instance_id ? args->instance_id : "";
    inst->instance_run_id = args->instance_run_id ? args->instance_run_id : "";
    inst->frame_ops = args->frame_ops;
    inst->image_ops = args->image_ops;
    inst->on_result = args->on_result;
    inst->result_user = args->result_user;
    inst->config = lib->default_config;

    if (args->config_json && args->config_json_len > 0) {
        inst->config = InstanceConfig::parse_from_json(
            std::string_view(args->config_json, args->config_json_len), inst->config);
    }

    inst->tracker = argus::cv::SimpleTracker(
        1.0f - inst->config.track_match_threshold,
        static_cast<int>(inst->config.track_buffer)
    );

    *out = static_cast<av_algo_instance>(inst.release());
    return AV_OK;
}

static int instance_negotiate(av_algo_instance inst_handle, const av_frame_caps* offered, av_frame_caps* accepted) noexcept {
    if (!inst_handle || !offered || !accepted ||
        offered->size < sizeof(av_frame_caps) || accepted->size < sizeof(av_frame_caps) ||
        offered->api_version != AV_ALGO_API_VERSION || accepted->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
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
    accepted->min_width = kMinFrameWidth;
    accepted->min_height = kMinFrameHeight;
    accepted->max_width = kMaxFrameWidth;
    accepted->max_height = kMaxFrameHeight;

    return AV_OK;
}

static int instance_update_config(av_algo_instance inst_handle, const char* json, uint32_t len) noexcept {
    if (!inst_handle || (!json && len > 0)) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    return invoke_abi(inst, [&] {
        if (json && len > 0) {
            inst->config = InstanceConfig::parse_from_json(std::string_view(json, len), inst->config);
            inst->tracker = argus::cv::SimpleTracker(
                1.0f - inst->config.track_match_threshold,
                static_cast<int>(inst->config.track_buffer)
            );
        }
        return static_cast<int>(AV_OK);
    });
}

static int instance_set_rules(av_algo_instance inst_handle, const av_rule* /*rules*/, uint32_t /*count*/) noexcept {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    // 识别类算法包忽略检测规则
    return AV_OK;
}

static int instance_process(av_algo_instance inst_handle, const av_frame_desc* frame) noexcept {
    if (!inst_handle || !frame || frame->size < sizeof(av_frame_desc) || frame->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    auto* inst = static_cast<InstanceContext*>(inst_handle);

    return invoke_abi(inst, [&] {
#if ENABLE_PROFILING
        FrameProfileRecord current_profile{};
        current_profile.frame_id = frame->frame_id;
        set_active_profile_record(&current_profile);
        auto t_proc_start = std::chrono::steady_clock::now();
#endif
        if (inst->has_received_frame && frame->frame_id <= inst->last_frame_id) {
            return fail(inst, AV_ERR_INVALID_ARG, "frame_id must be strictly increasing");
        }
        inst->last_frame_id = frame->frame_id;
        inst->has_received_frame = true;

        if (inst->mode == AV_INSTANCE_INSTALL_SELF_TEST && inst->self_test_emitted) {
            return fail(inst, AV_ERR_INVALID_ARG, "self-test instance already emitted a result");
        }

        // 1. 预处理：生成 640x384 Letterbox 图并持有 NV12 零拷贝视图 (彻底废除全图 RGB)
        PreprocessResult prep_res;
        std::string prep_err;
        if (!Preprocessor::process_frame(frame, prep_res, prep_err)) {
            return fail(inst, AV_ERR_INFERENCE_FAILED, ("preprocess failed: " + prep_err).c_str());
        }

        const uint32_t orig_w = prep_res.orig_width;
        const uint32_t orig_h = prep_res.orig_height;

        // 2. SCRFD 10G 人脸检测（主路径必须模型）
        ScrfdOutput scrfd_out;
        std::string scrfd_err;
        if (!inst->lib->model_manager->run_scrfd(prep_res.letterbox_rgb.data.data(), scrfd_out, scrfd_err)) {
            return fail(inst, AV_ERR_INTERNAL, ("SCRFD inference failed: " + scrfd_err).c_str());
        }

#if ENABLE_PROFILING
        auto t0_dec = std::chrono::steady_clock::now();
#endif
        auto detected_faces = Postprocessor::decode_scrfd_faces(
            scrfd_out, prep_res.letterbox_info,
            orig_w, orig_h,
            inst->config.face_detection_threshold, inst->config.face_nms_threshold
        );
#if ENABLE_PROFILING
        auto t1_dec = std::chrono::steady_clock::now();
        current_profile.decode_nms_ms = std::chrono::duration<double, std::milli>(t1_dec - t0_dec).count();
        current_profile.detected_faces = static_cast<uint32_t>(detected_faces.size());
#endif

        std::vector<RecognizedPerson> result_persons;

        // 3. 分支：默认纯人脸主路径 (enable_person_detection == false) vs 人体/人脸联合路径
        if (!inst->config.enable_person_detection) {
#if ENABLE_PROFILING
            auto t0_tq = std::chrono::steady_clock::now();
#endif
            // 将 detected_faces 转换为 DetectionBox 驱动 tracker
            std::vector<argus::cv::DetectionBox> face_dets;
            face_dets.reserve(detected_faces.size());
            for (const auto& face : detected_faces) {
                argus::cv::DetectionBox box{};
                box.class_id = 0;
                box.label = "face";
                box.confidence = face.score;
                box.x = face.x1 / static_cast<float>(orig_w);
                box.y = face.y1 / static_cast<float>(orig_h);
                box.w = (face.x2 - face.x1) / static_cast<float>(orig_w);
                box.h = (face.y2 - face.y1) / static_cast<float>(orig_h);
                face_dets.push_back(box);
            }

            auto tracked_faces = inst->tracker.update(face_dets);
            if (tracked_faces.size() > inst->config.max_person_count) {
                tracked_faces.resize(inst->config.max_person_count);
            }

            result_persons.reserve(tracked_faces.size());
            for (const auto& tracked_face : tracked_faces) {
                RecognizedPerson rp{};
                rp.track_id = tracked_face.track_id;
                rp.target_type = "face";
                rp.person_bbox[0] = std::clamp(tracked_face.x, 0.0f, 0.9999f);
                rp.person_bbox[1] = std::clamp(tracked_face.y, 0.0f, 0.9999f);
                rp.person_bbox[2] = std::clamp(tracked_face.w, 1e-4f, 1.0f - rp.person_bbox[0]);
                rp.person_bbox[3] = std::clamp(tracked_face.h, 1e-4f, 1.0f - rp.person_bbox[1]);
                rp.person_confidence = std::clamp(tracked_face.confidence, 0.0f, 1.0f);

                auto& track_state = inst->track_quality_map[tracked_face.track_id];
                track_state.hit_count++;

                // 寻找最匹配的 FaceDetection (IoU 最大；若无重叠则匹配中心点最近)
                const FaceDetection* best_face = nullptr;
                float best_face_iou = 0.0f;
                float min_center_dist_sq = 1e9f;

                float t_cx = tracked_face.x + tracked_face.w * 0.5f;
                float t_cy = tracked_face.y + tracked_face.h * 0.5f;

                for (const auto& face : detected_faces) {
                    float fx = face.x1 / static_cast<float>(orig_w);
                    float fy = face.y1 / static_cast<float>(orig_h);
                    float fw = (face.x2 - face.x1) / static_cast<float>(orig_w);
                    float fh = (face.y2 - face.y1) / static_cast<float>(orig_h);

                    float f_cx = fx + fw * 0.5f;
                    float f_cy = fy + fh * 0.5f;
                    float dist_sq = (t_cx - f_cx) * (t_cx - f_cx) + (t_cy - f_cy) * (t_cy - f_cy);

                    float xx1 = std::max(tracked_face.x, fx);
                    float yy1 = std::max(tracked_face.y, fy);
                    float xx2 = std::min(tracked_face.x + tracked_face.w, fx + fw);
                    float yy2 = std::min(tracked_face.y + tracked_face.h, fy + fh);
                    float inter_w = std::max(0.0f, xx2 - xx1);
                    float inter_h = std::max(0.0f, yy2 - yy1);
                    float inter_area = inter_w * inter_h;
                    float union_area = tracked_face.w * tracked_face.h + fw * fh - inter_area;
                    float iou = (union_area > 0.0f) ? (inter_area / union_area) : 0.0f;

                    if (iou > best_face_iou) {
                        best_face_iou = iou;
                        best_face = &face;
                    } else if (best_face_iou <= 0.0f && dist_sq < min_center_dist_sq) {
                        min_center_dist_sq = dist_sq;
                        best_face = &face;
                    }
                }

                process_face_feature_and_track_state(inst, frame, prep_res, best_face, rp, track_state);
                result_persons.push_back(std::move(rp));
            }
#if ENABLE_PROFILING
            auto t1_tq = std::chrono::steady_clock::now();
            double raw_tq_ms = std::chrono::duration<double, std::milli>(t1_tq - t0_tq).count();
            double inner_ms = current_profile.alignment_ms + current_profile.glintr_infer_ms +
                              current_profile.glintr_copy_ms + current_profile.embedding_encode_ms;
            current_profile.tracker_quality_ms = (raw_tq_ms > inner_ms) ? (raw_tq_ms - inner_ms) : 0.0;
#endif
        } else {
            // 可选人体检测 + 人脸联合路径 (YOLOv8n + SCRFD)
            YoloOutput yolo_out;
            std::string yolo_err;
            if (!inst->lib->model_manager->run_yolo(prep_res.letterbox_rgb.data.data(), yolo_out, yolo_err)) {
                return fail(inst, AV_ERR_INTERNAL, ("YOLO inference failed: " + yolo_err).c_str());
            }

            auto person_dets = Postprocessor::decode_yolo_persons(
                yolo_out, prep_res.letterbox_info,
                orig_w, orig_h,
                inst->config.person_detection_threshold, 0.45f
            );

            auto tracked_persons = inst->tracker.update(person_dets);
            if (tracked_persons.size() > inst->config.max_person_count) {
                tracked_persons.resize(inst->config.max_person_count);
            }

            result_persons.reserve(tracked_persons.size());
            for (const auto& person : tracked_persons) {
                RecognizedPerson rp{};
                rp.track_id = person.track_id;
                rp.target_type = "person";
                rp.person_bbox[0] = std::clamp(person.x, 0.0f, 0.9999f);
                rp.person_bbox[1] = std::clamp(person.y, 0.0f, 0.9999f);
                rp.person_bbox[2] = std::clamp(person.w, 1e-4f, 1.0f - rp.person_bbox[0]);
                rp.person_bbox[3] = std::clamp(person.h, 1e-4f, 1.0f - rp.person_bbox[1]);
                rp.person_confidence = std::clamp(person.confidence, 0.0f, 1.0f);

                auto& track_state = inst->track_quality_map[person.track_id];
                track_state.hit_count++;

                const FaceDetection* best_face = nullptr;
                float best_face_score = -1.0f;

                for (const auto& face : detected_faces) {
                    if (face_center_in_person(face, person, orig_w, orig_h)) {
                        float iou = compute_face_person_iou(face, person, orig_w, orig_h);
                        float score = face.score + 0.1f * iou;
                        if (score > best_face_score) {
                            best_face_score = score;
                            best_face = &face;
                        }
                    }
                }

                process_face_feature_and_track_state(inst, frame, prep_res, best_face, rp, track_state);
                result_persons.push_back(std::move(rp));
            }
        }

        // 4. 排序 (按 track_id 升序)
        std::sort(result_persons.begin(), result_persons.end(), [](const RecognizedPerson& a, const RecognizedPerson& b) {
            return a.track_id < b.track_id;
        });

        // 5. 自测结果或正常识别结果回调
        if (inst->mode == AV_INSTANCE_INSTALL_SELF_TEST) {
            std::string self_test_json =
                "{\n"
                "  \"status\": \"ok\",\n"
                "  \"stages\": [\"preprocess\", \"scrfd_inference\", \"glintr_inference\", \"serialize\"],\n"
                "  \"object_count\": " + std::to_string(result_persons.size()) + "\n"
                "}";

            av_algo_result res{};
            res.size = sizeof(res);
            res.api_version = AV_ALGO_API_VERSION;
            res.kind = AV_RESULT_SELF_TEST;
            res.frame_id = frame->frame_id;
            res.json = self_test_json.c_str();
            res.json_len = static_cast<uint32_t>(self_test_json.size());

            inst->self_test_emitted = true;
            if (inst->on_result) {
                inst->on_result(&res, inst->result_user);
            }
            return static_cast<int>(AV_OK);
        }

        // 正常模式：任一有效目标（包括未检测到人脸的人体）都回调，便于引擎生成通用抓拍事件。
        std::vector<av_algo_image_req> img_reqs;

        for (const auto& p : result_persons) {
            if (p.has_face) {
                if (!p.embedding_base64.empty()) {
                    // 若有特征提取，构造高清人脸裁剪请求 ROI
                    av_algo_image_req req{};
                    req.size = sizeof(req);
                    req.api_version = AV_ALGO_API_VERSION;
                    req.x = p.face_bbox[0];
                    req.y = p.face_bbox[1];
                    req.w = p.face_bbox[2];
                    req.h = p.face_bbox[3];
                    req.purpose = kImagePurposeFaceCrop;
                    img_reqs.push_back(req);
                }
            }
        }

        if (!result_persons.empty() && inst->on_result) {
#if ENABLE_PROFILING
            auto t0_ser = std::chrono::steady_clock::now();
#endif
            std::string result_json = Postprocessor::serialize_recognition_json(
                result_persons, frame->frame_id, frame->pts_ns);
#if ENABLE_PROFILING
            auto t1_ser = std::chrono::steady_clock::now();
            current_profile.serialization_ms = std::chrono::duration<double, std::milli>(t1_ser - t0_ser).count();
#endif

            av_algo_result res{};
            res.size = sizeof(res);
            res.api_version = AV_ALGO_API_VERSION;
            res.kind = AV_RESULT_RECOGNITION;
            res.frame_id = frame->frame_id;
            res.json = result_json.c_str();
            res.json_len = static_cast<uint32_t>(result_json.size());

            if (!img_reqs.empty()) {
                res.image_count = static_cast<uint32_t>(img_reqs.size());
                res.images = img_reqs.data();
            }

            inst->on_result(&res, inst->result_user);
        }

#if ENABLE_PROFILING
        auto t_proc_end = std::chrono::steady_clock::now();
        current_profile.total_ms = std::chrono::duration<double, std::milli>(t_proc_end - t_proc_start).count();
        current_profile.tracks = static_cast<uint32_t>(result_persons.size());
        current_profile.image_requests = static_cast<uint32_t>(img_reqs.size());
        set_active_profile_record(nullptr);
        log_message(inst->lib, 0, current_profile.to_json());
#endif

        return static_cast<int>(AV_OK);
    });
}

static int instance_flush(av_algo_instance inst_handle) noexcept {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    inst->tracker.reset();
    inst->track_quality_map.clear();
    inst->has_received_frame = false;
    inst->last_frame_id = 0;
    return AV_OK;
}

static int instance_destroy(av_algo_instance inst_handle) noexcept {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    delete inst;
    return AV_OK;
}

static int last_error(av_algo_instance inst_handle, char* buf, uint32_t cap) noexcept {
    if (!buf || cap == 0) return 0;
    std::string msg = g_last_error;
    if (inst_handle) {
        auto* inst = static_cast<InstanceContext*>(inst_handle);
        if (!inst->last_error_msg.empty()) {
            msg = inst->last_error_msg;
        }
    }
    copy_text(buf, cap, msg);
    return static_cast<int>(std::min<size_t>(cap - 1, msg.size()));
}

static const av_algo_abi g_face_recognition_abi = {
    .size = sizeof(av_algo_abi),
    .api_version = AV_ALGO_API_VERSION,
    .library_open = library_open,
    .library_query = library_query,
    .library_close = library_close,
    .instance_create = instance_create,
    .instance_negotiate = instance_negotiate,
    .instance_update_config = instance_update_config,
    .instance_set_rules = instance_set_rules,
    .instance_process = instance_process,
    .instance_flush = instance_flush,
    .instance_destroy = instance_destroy,
    .last_error = last_error
};

extern "C" AV_EXPORT const av_algo_abi* av_algo_get_abi(uint32_t requested_api_version) {
    if (requested_api_version != AV_ALGO_API_VERSION) return nullptr;
    return &g_face_recognition_abi;
}

extern "C" AV_EXPORT int av_algo_extract_face(av_algo_library lib_handle,
                                             const av_face_extract_input* in,
                                             av_face_extract_output* out) noexcept {
    if (!lib_handle || !in || !out) return AV_ERR_INVALID_ARG;
    if (in->size < sizeof(av_face_extract_input) || in->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    if (out->size < sizeof(av_face_extract_output) || out->api_version != AV_ALGO_API_VERSION) {
        return AV_ERR_INVALID_ARG;
    }
    if (!in->image_bytes || in->image_bytes_len == 0) {
        out->status_code = 4; // decode err
        copy_text(out->error_message, sizeof(out->error_message), "image data is empty");
        return AV_OK;
    }
    if (in->image_bytes_len > 10 * 1024 * 1024) {
        out->status_code = 5; // too large
        copy_text(out->error_message, sizeof(out->error_message), "image size exceeds 10MB limit");
        return AV_OK;
    }

    auto* lib = static_cast<LibraryContext*>(lib_handle);
    if (!lib || !lib->model_manager) {
        out->status_code = 7; // internal err
        copy_text(out->error_message, sizeof(out->error_message), "library model manager is not initialized");
        return AV_OK;
    }

    @autoreleasepool {
        NSData* ns_data = [NSData dataWithBytesNoCopy:(void*)in->image_bytes length:in->image_bytes_len freeWhenDone:NO];
        CGImageSourceRef source = CGImageSourceCreateWithData((__bridge CFDataRef)ns_data, nullptr);
        if (!source) {
            out->status_code = 4;
            copy_text(out->error_message, sizeof(out->error_message), "cannot create image source from bytes");
            return AV_OK;
        }
        CGImageRef image = CGImageSourceCreateImageAtIndex(source, 0, nullptr);
        CFRelease(source);
        if (!image) {
            out->status_code = 4;
            copy_text(out->error_message, sizeof(out->error_message), "cannot decode image index 0");
            return AV_OK;
        }

        size_t width = CGImageGetWidth(image);
        size_t height = CGImageGetHeight(image);
        if (width < 48 || height < 48) {
            CGImageRelease(image);
            out->status_code = 4;
            copy_text(out->error_message, sizeof(out->error_message), "image resolution is too small (<48x48)");
            return AV_OK;
        }
        if (width > kMaxFrameWidth || height > kMaxFrameHeight || (width * height) > 8294400) {
            CGImageRelease(image);
            out->status_code = 5; // too large
            copy_text(out->error_message, sizeof(out->error_message), "image resolution exceeds 3840x2160 or 8.29M pixels");
            return AV_OK;
        }

        ImageBuffer orig_rgb;
        orig_rgb.width = static_cast<uint32_t>(width);
        orig_rgb.height = static_cast<uint32_t>(height);
        orig_rgb.channels = 3;
        orig_rgb.data.resize(width * height * 3);

        std::vector<uint8_t> rgba(width * height * 4);
        CGColorSpaceRef color_space = CGColorSpaceCreateDeviceRGB();
        CGContextRef context = CGBitmapContextCreate(
            rgba.data(), width, height, 8, width * 4, color_space,
            static_cast<CGBitmapInfo>(static_cast<uint32_t>(kCGImageAlphaPremultipliedLast) | static_cast<uint32_t>(kCGBitmapByteOrder32Big))
        );
        CGColorSpaceRelease(color_space);
        if (!context) {
            CGImageRelease(image);
            out->status_code = 4;
            copy_text(out->error_message, sizeof(out->error_message), "cannot create bitmap context");
            return AV_OK;
        }
        CGContextDrawImage(context, CGRectMake(0, 0, width, height), image);
        CGContextRelease(context);
        CGImageRelease(image);

        vImage_Buffer full_rgba_buf = {
            .data = rgba.data(),
            .height = static_cast<vImagePixelCount>(height),
            .width = static_cast<vImagePixelCount>(width),
            .rowBytes = width * 4
        };
        vImage_Buffer full_rgb_buf = {
            .data = orig_rgb.data.data(),
            .height = static_cast<vImagePixelCount>(height),
            .width = static_cast<vImagePixelCount>(width),
            .rowBytes = width * 3
        };
        vImageConvert_RGBA8888toRGB888(&full_rgba_buf, &full_rgb_buf, kvImageNoFlags);

        // Letterbox to 640x640 (1:1 square ratio for static registration photos: IDs, selfies, passports)
        constexpr uint32_t kTargetWidth = 640;
        constexpr uint32_t kTargetHeight = 640;
        ImageBuffer letterbox_rgb;
        letterbox_rgb.width = kTargetWidth;
        letterbox_rgb.height = kTargetHeight;
        letterbox_rgb.channels = 3;
        letterbox_rgb.data.assign(kTargetWidth * kTargetHeight * 3, 114);

        argus::cv::LetterboxInfo letterbox_info = argus::cv::compute_letterbox(
            orig_rgb.width, orig_rgb.height, kTargetWidth, kTargetHeight
        );

        uint32_t nw = static_cast<uint32_t>(std::round(static_cast<float>(orig_rgb.width) * letterbox_info.scale));
        uint32_t nh = static_cast<uint32_t>(std::round(static_cast<float>(orig_rgb.height) * letterbox_info.scale));
        uint32_t pad_x = static_cast<uint32_t>(std::round(letterbox_info.pad_x));
        uint32_t pad_y = static_cast<uint32_t>(std::round(letterbox_info.pad_y));

        std::vector<uint8_t> scaled_argb(static_cast<size_t>(nw) * nh * 4);
        vImage_Buffer src_argb_buf = {
            .data = rgba.data(),
            .height = static_cast<vImagePixelCount>(height),
            .width = static_cast<vImagePixelCount>(width),
            .rowBytes = width * 4
        };
        vImage_Buffer scaled_argb_buf = {
            .data = scaled_argb.data(),
            .height = static_cast<vImagePixelCount>(nh),
            .width = static_cast<vImagePixelCount>(nw),
            .rowBytes = static_cast<size_t>(nw * 4)
        };
        vImageScale_ARGB8888(&src_argb_buf, &scaled_argb_buf, nullptr, kvImageHighQualityResampling);

        vImage_Buffer roi_rgb_buf = {
            .data = letterbox_rgb.data.data() + (pad_y * kTargetWidth + pad_x) * 3,
            .height = static_cast<vImagePixelCount>(nh),
            .width = static_cast<vImagePixelCount>(nw),
            .rowBytes = kTargetWidth * 3
        };
        // CGBitmapContext 产出的字节序为 RGBA，须用 RGBA 语义丢弃 alpha；
        // 误用 ARGB8888toRGB888 会丢掉 R 通道并把 alpha 当作 B，导致 SCRFD 漏检。
        vImageConvert_RGBA8888toRGB888(&scaled_argb_buf, &roi_rgb_buf, kvImageNoFlags);

        ScrfdOutput scrfd_out;
        std::string scrfd_err;
        if (!lib->model_manager->run_scrfd_reg(letterbox_rgb.data.data(), scrfd_out, scrfd_err)) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), ("SCRFD inference failed: " + scrfd_err).c_str());
            return AV_OK;
        }

        float min_det_score = in->min_detection_score > 0.0f ? in->min_detection_score : 0.50f;
        float min_face_sz = in->min_face_size > 0.0f ? in->min_face_size : 40.0f;
        float min_quality = in->min_quality_score > 0.0f ? in->min_quality_score : 35.0f;

        auto detected_faces = Postprocessor::decode_scrfd_faces(
            scrfd_out, letterbox_info,
            orig_rgb.width, orig_rgb.height,
            min_det_score, 0.45f
        );

        if (detected_faces.empty()) {
            out->status_code = 1; // NO_FACE
            copy_text(out->error_message, sizeof(out->error_message), "no face detected");
            return AV_OK;
        }
        if (detected_faces.size() > 1) {
            out->status_code = 2; // MULTI_FACE
            copy_text(out->error_message, sizeof(out->error_message), "multiple faces detected, single face photo required");
            return AV_OK;
        }

        const auto& face = detected_faces[0];
        float fw = face.x2 - face.x1;
        float fh = face.y2 - face.y1;
        float min_dim = std::min(fw, fh);
        if (min_dim < min_face_sz) {
            out->status_code = 6; // FACE_TOO_SMALL
            copy_text(out->error_message, sizeof(out->error_message), "face bounding box is too small (<40px)");
            return AV_OK;
        }

        float q_score = evaluate_face_quality(face, orig_rgb.width, orig_rgb.height);
        if (q_score < min_quality) {
            out->status_code = 3; // QUALITY_LOW
            copy_text(out->error_message, sizeof(out->error_message), "face quality score is below threshold (35)");
            return AV_OK;
        }

        ImageBuffer face_112;
        std::string align_err;
        if (!Preprocessor::align_face_112x112(orig_rgb, face.landmarks.data(), face_112, align_err)) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), ("face alignment failed: " + align_err).c_str());
            return AV_OK;
        }

        GlintrOutput glintr_out;
        std::string glintr_err;
        if (!lib->model_manager->run_glintr(face_112.data.data(), glintr_out, glintr_err)) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), ("GLINTR inference failed: " + glintr_err).c_str());
            return AV_OK;
        }

        if (glintr_out.embedding.size() != 512) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), "invalid embedding dimension");
            return AV_OK;
        }

        float norm_sq = 0.0f;
        for (float v : glintr_out.embedding) {
            if (!std::isfinite(v)) {
                out->status_code = 7;
                copy_text(out->error_message, sizeof(out->error_message), "embedding contains non-finite values");
                return AV_OK;
            }
            norm_sq += v * v;
        }
        float norm = std::sqrt(norm_sq);
        if (norm < 1e-6f) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), "embedding norm is too small");
            return AV_OK;
        }

        for (size_t i = 0; i < 512; ++i) {
            out->embedding[i] = glintr_out.embedding[i] / norm;
        }
        out->embedding_dim = 512;

        std::vector<uint8_t> face_rgba(112 * 112 * 4);
        for (int i = 0; i < 112 * 112; ++i) {
            face_rgba[i * 4 + 0] = face_112.data[i * 3 + 0];
            face_rgba[i * 4 + 1] = face_112.data[i * 3 + 1];
            face_rgba[i * 4 + 2] = face_112.data[i * 3 + 2];
            face_rgba[i * 4 + 3] = 255;
        }

        CGColorSpaceRef rgb_cs = CGColorSpaceCreateDeviceRGB();
        CGContextRef face_ctx = CGBitmapContextCreate(
            face_rgba.data(), 112, 112, 8, 112 * 4, rgb_cs,
            static_cast<CGBitmapInfo>(static_cast<uint32_t>(kCGImageAlphaNoneSkipLast) | static_cast<uint32_t>(kCGBitmapByteOrder32Big))
        );
        CGColorSpaceRelease(rgb_cs);

        CGImageRef face_img = face_ctx ? CGBitmapContextCreateImage(face_ctx) : nullptr;
        if (face_ctx) CGContextRelease(face_ctx);

        if (!face_img) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), "cannot create 112x112 image context");
            return AV_OK;
        }

        CFMutableDataRef jpeg_dest_data = CFDataCreateMutable(kCFAllocatorDefault, 0);
        CGImageDestinationRef dest = CGImageDestinationCreateWithData(jpeg_dest_data, CFSTR("public.jpeg"), 1, nullptr);
        if (dest) {
            float quality = 0.90f;
            CFNumberRef qual_num = CFNumberCreate(kCFAllocatorDefault, kCFNumberFloatType, &quality);
            const void* keys[] = { kCGImageDestinationLossyCompressionQuality };
            const void* values[] = { qual_num };
            CFDictionaryRef opts = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
            CGImageDestinationAddImage(dest, face_img, opts);
            CGImageDestinationFinalize(dest);
            CFRelease(opts);
            CFRelease(qual_num);
            CFRelease(dest);
        }
        CGImageRelease(face_img);

        CFIndex jpeg_len = CFDataGetLength(jpeg_dest_data);
        bool encode_ok = (jpeg_len > 0 && jpeg_len <= static_cast<CFIndex>(sizeof(out->aligned_jpeg_data)));
        if (encode_ok) {
            CFDataGetBytes(jpeg_dest_data, CFRangeMake(0, jpeg_len), out->aligned_jpeg_data);
            out->aligned_jpeg_len = static_cast<uint32_t>(jpeg_len);
        }
        CFRelease(jpeg_dest_data);

        if (!encode_ok) {
            out->status_code = 7;
            copy_text(out->error_message, sizeof(out->error_message), "JPEG encoding failed or exceeded buffer");
            return AV_OK;
        }

        out->bbox[0] = face.x1 / static_cast<float>(orig_rgb.width);
        out->bbox[1] = face.y1 / static_cast<float>(orig_rgb.height);
        out->bbox[2] = (face.x2 - face.x1) / static_cast<float>(orig_rgb.width);
        out->bbox[3] = (face.y2 - face.y1) / static_cast<float>(orig_rgb.height);
        out->quality_score = q_score;
        out->detection_score = face.score;
        out->status_code = 0; // OK
        out->error_message[0] = '\0';

        return AV_OK;
    }
}
