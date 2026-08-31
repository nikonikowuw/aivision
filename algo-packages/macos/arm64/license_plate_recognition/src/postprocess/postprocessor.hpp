#pragma once

#include "core/config.hpp"
#include "inference/model_inference.hpp"
#include "preprocess/preprocessor.hpp"
#include "argus/cv/nms.hpp"
#include "argus/cv/tracker.hpp"
#include <string>
#include <vector>
#include <unordered_map>
#include <cstdint>

namespace lpr {

struct PlateObject {
    float x_min = 0.0f;
    float y_min = 0.0f;
    float x_max = 0.0f;
    float y_max = 0.0f;
    float landmarks_8[8] = {0.0f}; // 原图像素坐标
    float landmarks_norm[8] = {0.0f}; // 原图归一化 0..1 坐标
    float confidence = 0.0f;
    bool is_double_layer = false;

    std::string plate_text;
    std::string normalized_text;
    std::string plate_color;
    std::string plate_type;
    float ocr_confidence = 0.0f;

    int64_t track_id = 0;
    bool should_report = false;
};

struct TrackObservationState {
    int64_t track_id = 0;
    std::unordered_map<std::string, int> text_votes;
    std::unordered_map<std::string, int> color_votes;
    std::unordered_map<std::string, int> type_votes;
    float highest_score = 0.0f;
    float highest_ocr_conf = 0.0f;
    int observed_count = 0;
    int64_t last_reported_wall_time_ns = 0;
    bool has_reported = false;
};

class Postprocessor {
public:
    explicit Postprocessor(const Config& cfg);
    ~Postprocessor() = default;

    void update_config(const Config& cfg);

    /**
     * @brief 过滤检测模型输出，执行 NMS，输出原图坐标框与 4 点关键点
     */
    std::vector<PlateObject> filter_and_nms(const PlateDetectOutput& detect_out,
                                            const argus::cv::LetterboxInfo& lb_info,
                                            uint32_t orig_w, uint32_t orig_h) const;

    /**
     * @brief CTC 解码字符序列与颜色分类
     */
    void decode_plate_recognition(const PlateRecOutput& rec_out,
                                  bool is_double_layer,
                                  std::string& plate_text,
                                  std::string& normalized_text,
                                  std::string& plate_color,
                                  std::string& plate_type,
                                  float& ocr_confidence) const;

    /**
     * @brief 多目标跟踪与多帧多数表决
     */
    std::vector<PlateObject> track_and_vote(std::vector<PlateObject>& plates,
                                            int64_t wall_time_ns);

    /**
     * @brief 组装 C ABI 规范的 JSON 结果串
     */
    std::string build_result_json(uint64_t frame_id, const std::vector<PlateObject>& plates) const;

private:
    Config config_;
    argus::cv::SimpleTracker tracker_;
    std::unordered_map<int64_t, TrackObservationState> track_states_;
};

} // namespace lpr
