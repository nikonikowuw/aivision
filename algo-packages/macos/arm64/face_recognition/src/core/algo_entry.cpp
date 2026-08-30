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

#include <atomic>
#include <cmath>
#include <cstdlib>
#include <cstring>
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
    std::string glintr_path = "model/glintr100.mlpackage";
    InstanceConfig default_config;

    av_log_fn log = nullptr;
    void* log_user = nullptr;
    std::shared_ptr<ModelInferenceManager> model_manager;
};

struct TrackQualityState {
    uint32_t hit_count = 0;
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

// 计算人脸中心是否落在人体框内
bool face_center_in_person(const FaceDetection& face, const argus::cv::DetectionBox& person, uint32_t orig_w, uint32_t orig_h) {
    float face_cx = (face.x1 + face.x2) * 0.5f;
    float face_cy = (face.y1 + face.y2) * 0.5f;

    float person_x1 = person.x * static_cast<float>(orig_w);
    float person_y1 = person.y * static_cast<float>(orig_h);
    float person_x2 = (person.x + person.w) * static_cast<float>(orig_w);
    float person_y2 = (person.y + person.h) * static_cast<float>(orig_h);

    return (face_cx >= person_x1 && face_cx <= person_x2 &&
            face_cy >= person_y1 && face_cy <= person_y2);
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
        if (env_vars.contains("GLINTR_MODEL_PATH")) lib->glintr_path = env_vars["GLINTR_MODEL_PATH"];

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

        lib->model_manager = std::make_shared<ModelInferenceManager>();
        std::string model_err;
        if (!lib->model_manager->load_models(lib->package_root, lib->yolo_path, lib->scrfd_path, lib->glintr_path, model_err)) {
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
        if (inst->has_received_frame && frame->frame_id <= inst->last_frame_id) {
            return fail(inst, AV_ERR_INVALID_ARG, "frame_id must be strictly increasing");
        }
        inst->last_frame_id = frame->frame_id;
        inst->has_received_frame = true;

        if (inst->mode == AV_INSTANCE_INSTALL_SELF_TEST && inst->self_test_emitted) {
            return fail(inst, AV_ERR_INVALID_ARG, "self-test instance already emitted a result");
        }

        auto t0 = std::chrono::high_resolution_clock::now();
        // 1. 预处理
        PreprocessResult prep_res;
        std::string prep_err;
        if (!Preprocessor::process_frame(frame, prep_res, prep_err)) {
            return fail(inst, AV_ERR_INFERENCE_FAILED, ("preprocess failed: " + prep_err).c_str());
        }

        // 2 & 3. 并发执行 YOLOv8n (人体检测) 与 SCRFD (人脸检测)
        YoloOutput yolo_out;
        std::string yolo_err;
        ScrfdOutput scrfd_out;
        std::string scrfd_err;

        auto scrfd_future = std::async(std::launch::async, [&]() {
            return inst->lib->model_manager->run_scrfd(prep_res.letterbox_rgb.data.data(), scrfd_out, scrfd_err);
        });

        bool yolo_ok = inst->lib->model_manager->run_yolo(prep_res.letterbox_rgb.data.data(), yolo_out, yolo_err);
        bool scrfd_ok = scrfd_future.get();

        if (!yolo_ok) {
            return fail(inst, AV_ERR_INTERNAL, ("YOLO inference failed: " + yolo_err).c_str());
        }
        if (!scrfd_ok) {
            return fail(inst, AV_ERR_INTERNAL, ("SCRFD inference failed: " + scrfd_err).c_str());
        }

        auto person_dets = Postprocessor::decode_yolo_persons(
            yolo_out, prep_res.letterbox_info,
            prep_res.original_rgb.width, prep_res.original_rgb.height,
            inst->config.person_detection_threshold, 0.45f
        );

        auto tracked_persons = inst->tracker.update(person_dets);
        if (tracked_persons.size() > inst->config.max_person_count) {
            tracked_persons.resize(inst->config.max_person_count);
        }

        auto detected_faces = Postprocessor::decode_scrfd_faces(
            scrfd_out, prep_res.letterbox_info,
            prep_res.original_rgb.width, prep_res.original_rgb.height,
            inst->config.face_detection_threshold, inst->config.face_nms_threshold
        );

        // 5. 人体与人脸关联 (center-in-person + IoU)
        // 每个人体最多一张脸；未关联人脸丢弃
        std::vector<RecognizedPerson> result_persons;
        result_persons.reserve(tracked_persons.size());
        for (const auto& person : tracked_persons) {
            RecognizedPerson rp{};
            rp.track_id = person.track_id;
            rp.person_bbox[0] = person.x;
            rp.person_bbox[1] = person.y;
            rp.person_bbox[2] = person.w;
            rp.person_bbox[3] = person.h;
            rp.person_confidence = person.confidence;

            // 轨迹命中帧数计数自增
            auto& track_state = inst->track_quality_map[person.track_id];
            track_state.hit_count++;

            const FaceDetection* best_face = nullptr;
            float best_face_score = -1.0f;

            for (const auto& face : detected_faces) {
                if (face_center_in_person(face, person, prep_res.original_rgb.width, prep_res.original_rgb.height)) {
                    float iou = compute_face_person_iou(face, person, prep_res.original_rgb.width, prep_res.original_rgb.height);
                    float score = face.score + 0.1f * iou;
                    if (score > best_face_score) {
                        best_face_score = score;
                        best_face = &face;
                    }
                }
            }

            if (best_face) {
                // 评估抓拍优选条件
                bool need_extract = true;
                float face_w_px = best_face->x2 - best_face->x1;
                float face_h_px = best_face->y2 - best_face->y1;
                float min_dim = std::min(face_w_px, face_h_px);

                float q_score = evaluate_face_quality(*best_face, prep_res.original_rgb.width, prep_res.original_rgb.height);

                if (inst->mode != AV_INSTANCE_INSTALL_SELF_TEST && inst->config.feature_mode == "best_shot") {
                    // 1. 防抖与虚警过滤：轨迹存活帧数需达到 track_confirm_frames
                    if (track_state.hit_count < inst->config.track_confirm_frames) {
                        need_extract = false;
                    } else if (min_dim < static_cast<float>(inst->config.min_face_size) || q_score < inst->config.quality_threshold) {
                        // 2. 人脸最低像素尺寸与质量及格线
                        need_extract = false;
                    } else {
                        // 3. 已有特征提取记录：检查是否显著提升或达到重采样间隔
                        if (track_state.last_extracted_frame > 0) {
                            bool interval_reached = (inst->config.reextract_interval_frames > 0 &&
                                (frame->frame_id - track_state.last_extracted_frame >= inst->config.reextract_interval_frames));
                            bool significantly_better = (q_score >= track_state.highest_quality + inst->config.quality_update_margin);

                            if (!interval_reached && !significantly_better) {
                                need_extract = false;
                            }
                        }
                    }
                }

                // 如果本帧不提取特征，但存在人脸框，仍然输出 face 几何信息（embedding 为空）
                // 这样前端可视化与目标框能连续追踪显示，同时极大降低模型算力
                if (best_face) {
                    rp.has_face = true;
                    rp.face_bbox[0] = best_face->x1 / static_cast<float>(prep_res.original_rgb.width);
                    rp.face_bbox[1] = best_face->y1 / static_cast<float>(prep_res.original_rgb.height);
                    rp.face_bbox[2] = (best_face->x2 - best_face->x1) / static_cast<float>(prep_res.original_rgb.width);
                    rp.face_bbox[3] = (best_face->y2 - best_face->y1) / static_cast<float>(prep_res.original_rgb.height);
                    rp.face_confidence = best_face->score;
                    for (int k = 0; k < 5; ++k) {
                        rp.face_landmarks[k * 2 + 0] = best_face->landmarks[k * 2 + 0] / static_cast<float>(prep_res.original_rgb.width);
                        rp.face_landmarks[k * 2 + 1] = best_face->landmarks[k * 2 + 1] / static_cast<float>(prep_res.original_rgb.height);
                    }
                }

                if (need_extract) {
                    // 6. 从原图直接做五点相似变换对齐截脸 -> 112x112
                    ImageBuffer face_112;
                    std::string align_err;
                    if (Preprocessor::align_face_112x112(prep_res.original_rgb, best_face->landmarks.data(), face_112, align_err)) {
                        // 7. 运行 GLINTR100 提取特征
                        GlintrOutput glintr_out;
                        std::string glintr_err;
                        if (inst->lib->model_manager->run_glintr(face_112.data.data(), glintr_out, glintr_err)) {
                            std::string emb_b64;
                            std::string emb_err;
                            if (Postprocessor::process_and_encode_embedding(glintr_out.embedding, emb_b64, emb_err)) {
                                rp.embedding_base64 = emb_b64;

                                // 更新轨迹优选状态
                                track_state.last_extracted_frame = frame->frame_id;
                                if (q_score > track_state.highest_quality) track_state.highest_quality = q_score;
                                track_state.cached_embedding = std::move(emb_b64);
                            }
                        }
                    }
                }
            }

            result_persons.push_back(std::move(rp));
        }

        // 8. 排序 (按 track_id 升序)
        std::sort(result_persons.begin(), result_persons.end(), [](const RecognizedPerson& a, const RecognizedPerson& b) {
            return a.track_id < b.track_id;
        });

        // 9. 自测结果或正常识别结果回调
        if (inst->mode == AV_INSTANCE_INSTALL_SELF_TEST) {
            std::string self_test_json =
                "{\n"
                "  \"status\": \"ok\",\n"
                "  \"stages\": [\"preprocess\", \"yolo_inference\", \"scrfd_inference\", \"glintr_inference\", \"serialize\"],\n"
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

        // 正常识别模式：只有当至少检测到一个有效人脸且生成了 embedding 时才回调
        bool has_any_face = false;
        for (const auto& p : result_persons) {
            if (p.has_face) {
                has_any_face = true;
                break;
            }
        }

        if (has_any_face && inst->on_result) {
            std::string result_json = Postprocessor::serialize_recognition_json(result_persons);

            av_algo_result res{};
            res.size = sizeof(res);
            res.api_version = AV_ALGO_API_VERSION;
            res.kind = AV_RESULT_RECOGNITION;
            res.frame_id = frame->frame_id;
            res.json = result_json.c_str();
            res.json_len = static_cast<uint32_t>(result_json.size());

            inst->on_result(&res, inst->result_user);
        }

        return static_cast<int>(AV_OK);
    });
}

static int instance_flush(av_algo_instance inst_handle) noexcept {
    if (!inst_handle) return AV_ERR_INVALID_ARG;
    auto* inst = static_cast<InstanceContext*>(inst_handle);
    inst->tracker.reset();
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
