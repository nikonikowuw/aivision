#pragma once

#include "argus/cv/nms.hpp"
#include "argus/cv/letterbox.hpp"
#include "../inference/model_inference.hpp"
#include <vector>
#include <string>
#include <array>
#include <cstdint>

namespace face_recognition {

/**
 * @brief 人脸检测候选框
 */
struct FaceDetection {
    float x1, y1, x2, y2;         // 原图绝对坐标 [px]
    float score;
    std::array<float, 10> landmarks; // 5 点关键点原图坐标 [px]
};

/**
 * @brief 识别流水线中的单个目标（人体或人脸）与关联人脸信息
 */
struct RecognizedPerson {
    int64_t track_id = 0;
    std::string target_type = "face";
    float person_bbox[4] = {0};   // 原图归一化坐标 [x, y, w, h]
    float person_confidence = 0.0f;

    bool has_face = false;
    float face_bbox[4] = {0};     // 原图归一化坐标 [x, y, w, h]
    float face_confidence = 0.0f;
    float face_quality = 0.0f;    // 综合人脸质量得分 [0.0, 1.0]
    float face_landmarks[10] = {0}; // 原图归一化坐标 [x0, y0, ..., x4, y4]
    std::string embedding_base64; // 512 float32 little-endian Base64
};

/**
 * @brief 后处理器
 */
class Postprocessor {
public:
    /**
     * @brief 解码 YOLOv8n 输出，仅保留 COCO class 0 (person)
     */
    static std::vector<argus::cv::DetectionBox> decode_yolo_persons(
        const YoloOutput& yolo_out,
        const argus::cv::LetterboxInfo& lb_info,
        uint32_t orig_w, uint32_t orig_h,
        float conf_thresh, float nms_thresh);

    /**
     * @brief 解码 SCRFD 10G 输出并完成 NMS 及反 Letterbox 映射
     */
    static std::vector<FaceDetection> decode_scrfd_faces(
        const ScrfdOutput& scrfd_out,
        const argus::cv::LetterboxInfo& lb_info,
        uint32_t orig_w, uint32_t orig_h,
        float conf_thresh, float nms_thresh);

    /**
     * @brief 对 512 维 float embedding 进行有限性校验、L2 归一化及 Base64 little-endian 编码
     */
    static bool process_and_encode_embedding(
        const std::vector<float>& raw_embedding,
        std::string& out_base64,
        std::string& error);

    /**
     * @brief 序列化 AV_RESULT_RECOGNITION JSON 协议
     */
    static std::string serialize_recognition_json(
        const std::vector<RecognizedPerson>& persons,
        uint64_t frame_id = 0,
        uint64_t pts_ns = 0);
};

} // namespace face_recognition
