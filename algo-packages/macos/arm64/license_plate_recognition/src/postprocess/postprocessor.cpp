/**
 * @file postprocessor.cpp
 * @brief 车牌检测 NMS、CTC 解码、多目标跟踪与多数表决实现
 */

#include "postprocessor.hpp"
#include "argus/utils/json.hpp"
#include <cmath>
#include <algorithm>
#include <sstream>
#include <iomanip>

namespace lpr {

namespace {

const char* const kPlateChars[78] = {
    "", // 0: blank
    "京", "沪", "津", "渝", "冀", "晋", "蒙", "辽", "吉", "黑",
    "苏", "浙", "皖", "闽", "赣", "鲁", "豫", "鄂", "湘", "粤",
    "桂", "琼", "川", "贵", "云", "藏", "陕", "甘", "青", "宁",
    "新", "学", "警", "港", "澳", "挂", "使", "领", "民", "航",
    "危", "0", "1", "2", "3", "4", "5", "6", "7", "8",
    "9", "A", "B", "C", "D", "E", "F", "G", "H", "J",
    "K", "L", "M", "N", "P", "Q", "R", "S", "T", "U",
    "V", "W", "X", "Y", "Z", "险", "品"
};

const char* const kPlateColors[5] = {
    "black", "blue", "green", "white", "yellow"
};

float softmax_score(const float* logits, int size, int target_idx) {
    float max_l = logits[0];
    for (int i = 1; i < size; ++i) {
        if (logits[i] > max_l) max_l = logits[i];
    }
    float sum_exp = 0.0f;
    for (int i = 0; i < size; ++i) {
        sum_exp += std::exp(logits[i] - max_l);
    }
    if (sum_exp <= 1e-7f) return 0.0f;
    return std::exp(logits[target_idx] - max_l) / sum_exp;
}

float compute_iou_xywh_raw(float x1, float y1, float w1, float h1,
                           float x2, float y2, float w2, float h2) {
    float rx1 = std::max(x1, x2);
    float ry1 = std::max(y1, y2);
    float rx2 = std::min(x1 + w1, x2 + w2);
    float ry2 = std::min(y1 + h1, y2 + h2);

    float inter_w = std::max(0.0f, rx2 - rx1);
    float inter_h = std::max(0.0f, ry2 - ry1);
    float inter_area = inter_w * inter_h;

    float area_a = w1 * h1;
    float area_b = w2 * h2;
    float union_area = area_a + area_b - inter_area;
    if (union_area <= 0.0f) return 0.0f;
    return inter_area / union_area;
}

} // namespace

Postprocessor::Postprocessor(const Config& cfg)
    : config_(cfg), tracker_(1.0f - cfg.iou_threshold, 30) {}

void Postprocessor::update_config(const Config& cfg) {
    config_ = cfg;
    tracker_ = argus::cv::SimpleTracker(1.0f - cfg.iou_threshold, 30);
}

std::vector<PlateObject> Postprocessor::filter_and_nms(
    const PlateDetectOutput& detect_out,
    const argus::cv::LetterboxInfo& lb_info,
    uint32_t orig_w, uint32_t orig_h) const {

    std::vector<PlateObject> candidates;
    if (detect_out.data.empty() || detect_out.num_boxes == 0) return candidates;

    struct RawDet {
        float x, y, w, h;
        float score;
        int cls_id;
        uint32_t index;
    };

    std::vector<RawDet> raw_dets;
    raw_dets.reserve(detect_out.num_boxes);

    const float* ptr = detect_out.data.data();
    for (uint32_t i = 0; i < detect_out.num_boxes; ++i) {
        const float* row = ptr + i * 15;
        float score = row[4];
        if (score < config_.confidence_threshold) continue;

        // row[13] = single layer score, row[14] = double layer score
        float class_single = row[13];
        float class_double = row[14];
        int cls_id = (class_double > class_single) ? 1 : 0;

        float cx = row[0];
        float cy = row[1];
        float w = row[2];
        float h = row[3];

        RawDet rd{};
        rd.x = cx - w * 0.5f;
        rd.y = cy - h * 0.5f;
        rd.w = w;
        rd.h = h;
        rd.score = score;
        rd.cls_id = cls_id;
        rd.index = i;

        raw_dets.push_back(rd);
    }

    std::sort(raw_dets.begin(), raw_dets.end(), [](const RawDet& a, const RawDet& b) {
        return a.score > b.score;
    });

    std::vector<bool> suppressed(raw_dets.size(), false);
    for (size_t i = 0; i < raw_dets.size(); ++i) {
        if (suppressed[i]) continue;

        const auto& rd = raw_dets[i];
        const float* row = ptr + rd.index * 15;

        PlateObject obj{};
        obj.confidence = rd.score;
        obj.is_double_layer = (rd.cls_id == 1);

        // Letterbox 坐标映射回原图坐标
        float pad_x = lb_info.pad_x;
        float pad_y = lb_info.pad_y;
        float scale = lb_info.scale > 0.0f ? lb_info.scale : 1.0f;

        float left_orig = (rd.x - pad_x) / scale;
        float top_orig = (rd.y - pad_y) / scale;
        float right_orig = (rd.x + rd.w - pad_x) / scale;
        float bottom_orig = (rd.y + rd.h - pad_y) / scale;

        obj.x_min = std::clamp(left_orig / static_cast<float>(orig_w), 0.0f, 1.0f);
        obj.y_min = std::clamp(top_orig / static_cast<float>(orig_h), 0.0f, 1.0f);
        obj.x_max = std::clamp(right_orig / static_cast<float>(orig_w), 0.0f, 1.0f);
        obj.y_max = std::clamp(bottom_orig / static_cast<float>(orig_h), 0.0f, 1.0f);

        // 4 个顶点关键点 (原图像素坐标 & 归一化坐标)
        for (int k = 0; k < 4; ++k) {
            float kx = row[5 + k * 2];
            float ky = row[5 + k * 2 + 1];

            float kx_orig = (kx - pad_x) / scale;
            float ky_orig = (ky - pad_y) / scale;

            kx_orig = std::clamp(kx_orig, 0.0f, static_cast<float>(orig_w) - 1.0f);
            ky_orig = std::clamp(ky_orig, 0.0f, static_cast<float>(orig_h) - 1.0f);

            obj.landmarks_8[k * 2] = kx_orig;
            obj.landmarks_8[k * 2 + 1] = ky_orig;

            obj.landmarks_norm[k * 2] = kx_orig / static_cast<float>(orig_w);
            obj.landmarks_norm[k * 2 + 1] = ky_orig / static_cast<float>(orig_h);
        }

        candidates.push_back(obj);

        // Suppress overlapping
        for (size_t j = i + 1; j < raw_dets.size(); ++j) {
            if (suppressed[j]) continue;
            if (compute_iou_xywh_raw(raw_dets[i].x, raw_dets[i].y, raw_dets[i].w, raw_dets[i].h,
                                     raw_dets[j].x, raw_dets[j].y, raw_dets[j].w, raw_dets[j].h) > config_.iou_threshold) {
                suppressed[j] = true;
            }
        }
    }

    return candidates;
}

void Postprocessor::decode_plate_recognition(
    const PlateRecOutput& rec_out,
    bool is_double_layer,
    std::string& plate_text,
    std::string& normalized_text,
    std::string& plate_color,
    std::string& plate_type,
    float& ocr_confidence) const {

    plate_text.clear();
    normalized_text.clear();
    ocr_confidence = 0.0f;

    if (rec_out.char_logits.size() >= 21 * 78) {
        int prev_idx = 0;
        float conf_sum = 0.0f;
        int char_count = 0;

        for (int t = 0; t < 21; ++t) {
            const float* logits = rec_out.char_logits.data() + t * 78;
            int max_idx = 0;
            float max_val = logits[0];
            for (int c = 1; c < 78; ++c) {
                if (logits[c] > max_val) {
                    max_val = logits[c];
                    max_idx = c;
                }
            }

            if (max_idx != 0 && max_idx != prev_idx) {
                plate_text += kPlateChars[max_idx];
                conf_sum += softmax_score(logits, 78, max_idx);
                char_count++;
            }
            prev_idx = max_idx;
        }

        if (char_count > 0) {
            ocr_confidence = conf_sum / static_cast<float>(char_count);
        }
    }

    normalized_text = plate_text;

    // Color decoding
    int color_idx = 1; // default blue
    if (rec_out.color_logits.size() >= 5) {
        float max_color_val = rec_out.color_logits[0];
        color_idx = 0;
        for (int c = 1; c < 5; ++c) {
            if (rec_out.color_logits[c] > max_color_val) {
                max_color_val = rec_out.color_logits[c];
                color_idx = c;
            }
        }
    }
    plate_color = kPlateColors[std::clamp(color_idx, 0, 4)];

    // Determine plate_type
    if (is_double_layer) {
        plate_type = "double_yellow";
    } else if (plate_color == "green") {
        plate_type = "new_energy";
    } else if (plate_text.ends_with("警")) {
        plate_type = "police";
    } else if (plate_text.ends_with("学")) {
        plate_type = "coach";
    } else if (plate_text.ends_with("港") || plate_text.ends_with("澳")) {
        plate_type = "hk_macau";
    } else if (plate_text.find("使") != std::string::npos || plate_text.find("领") != std::string::npos) {
        plate_type = "embassy";
    } else {
        plate_type = "standard";
    }
}

std::vector<PlateObject> Postprocessor::track_and_vote(
    std::vector<PlateObject>& plates,
    int64_t wall_time_ns) {

    std::vector<argus::cv::DetectionBox> det_boxes;
    det_boxes.reserve(plates.size());

    for (size_t i = 0; i < plates.size(); ++i) {
        argus::cv::DetectionBox b{};
        b.x = plates[i].x_min;
        b.y = plates[i].y_min;
        b.w = plates[i].x_max - plates[i].x_min;
        b.h = plates[i].y_max - plates[i].y_min;
        b.confidence = plates[i].confidence;
        b.class_id = 0;
        det_boxes.push_back(b);
    }

    auto tracked = tracker_.update(det_boxes);
    std::vector<PlateObject> result_plates;
    result_plates.reserve(tracked.size());

    const int64_t cooldown_ns = static_cast<int64_t>(config_.observation_cooldown_seconds) * 1'000'000'000LL;

    for (size_t i = 0; i < tracked.size() && i < plates.size(); ++i) {
        PlateObject obj = plates[i];
        obj.track_id = tracked[i].track_id;

        auto& state = track_states_[obj.track_id];
        state.track_id = obj.track_id;
        state.observed_count++;
        state.highest_score = std::max(state.highest_score, obj.confidence);
        state.highest_ocr_conf = std::max(state.highest_ocr_conf, obj.ocr_confidence);

        if (!obj.plate_text.empty()) {
            state.text_votes[obj.plate_text]++;
            state.color_votes[obj.plate_color]++;
            state.type_votes[obj.plate_type]++;
        }

        // Majority voting
        std::string best_text = obj.plate_text;
        int max_text_votes = 0;
        for (const auto& [txt, votes] : state.text_votes) {
            if (votes > max_text_votes) {
                max_text_votes = votes;
                best_text = txt;
            }
        }

        std::string best_color = obj.plate_color;
        int max_color_votes = 0;
        for (const auto& [col, votes] : state.color_votes) {
            if (votes > max_color_votes) {
                max_color_votes = votes;
                best_color = col;
            }
        }

        std::string best_type = obj.plate_type;
        int max_type_votes = 0;
        for (const auto& [typ, votes] : state.type_votes) {
            if (votes > max_type_votes) {
                max_type_votes = votes;
                best_type = typ;
            }
        }

        obj.plate_text = best_text;
        obj.normalized_text = best_text;
        obj.plate_color = best_color;
        obj.plate_type = best_type;

        // 判断是否触发上报
        bool mature = state.observed_count >= config_.voting_window_frames;
        bool cooldown_expired = (wall_time_ns - state.last_reported_wall_time_ns) >= cooldown_ns;
        bool ocr_valid = (state.highest_ocr_conf >= config_.ocr_confidence_threshold) && (best_text.size() >= 5);

        if (ocr_valid && (!state.has_reported ? mature : cooldown_expired)) {
            obj.should_report = true;
            state.has_reported = true;
            state.last_reported_wall_time_ns = wall_time_ns;
        }

        result_plates.push_back(obj);
    }

    return result_plates;
}

std::string Postprocessor::build_result_json(uint64_t frame_id, const std::vector<PlateObject>& plates) const {
    std::ostringstream ss;
    ss << std::fixed << std::setprecision(4);

    ss << "{\n";
    ss << "  \"frame_id\": " << frame_id << ",\n";
    ss << "  \"algorithm_type\": \"license_plate_recognition\",\n";
    ss << "  \"plates\": [";

    bool first = true;
    for (const auto& p : plates) {
        if (!p.should_report) continue;
        if (!first) ss << ",";
        first = false;

        float bw = p.x_max - p.x_min;
        float bh = p.y_max - p.y_min;
        float vx_min = std::clamp(p.x_min - bw * 1.5f, 0.0f, 1.0f);
        float vy_min = std::clamp(p.y_min - bh * 2.5f, 0.0f, 1.0f);
        float vx_max = std::clamp(p.x_max + bw * 1.5f, 0.0f, 1.0f);
        float vy_max = std::clamp(p.y_max + bh * 1.5f, 0.0f, 1.0f);

        ss << "\n    {\n";
        ss << "      \"plate_text\": \"" << argus::utils::escape_json_string(p.plate_text) << "\",\n";
        ss << "      \"normalized_text\": \"" << argus::utils::escape_json_string(p.normalized_text) << "\",\n";
        ss << "      \"plate_color\": \"" << argus::utils::escape_json_string(p.plate_color) << "\",\n";
        ss << "      \"plate_type\": \"" << argus::utils::escape_json_string(p.plate_type) << "\",\n";
        ss << "      \"confidence\": " << p.confidence << ",\n";
        ss << "      \"ocr_confidence\": " << p.ocr_confidence << ",\n";
        ss << "      \"track_id\": " << p.track_id << ",\n";
        ss << "      \"bbox\": {\n";
        ss << "        \"x_min\": " << p.x_min << ",\n";
        ss << "        \"y_min\": " << p.y_min << ",\n";
        ss << "        \"x_max\": " << p.x_max << ",\n";
        ss << "        \"y_max\": " << p.y_max << "\n";
        ss << "      },\n";
        ss << "      \"vehicle_bbox\": {\n";
        ss << "        \"x_min\": " << vx_min << ",\n";
        ss << "        \"y_min\": " << vy_min << ",\n";
        ss << "        \"x_max\": " << vx_max << ",\n";
        ss << "        \"y_max\": " << vy_max << "\n";
        ss << "      }\n";
        ss << "    }";
    }

    if (!first) {
        ss << "\n  ";
    }
    ss << "]\n";
    ss << "}";
    return ss.str();
}

} // namespace lpr
