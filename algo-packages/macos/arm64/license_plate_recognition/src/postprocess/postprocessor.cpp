/**
 * @file postprocessor.cpp
 * @brief 车牌检测 NMS、通用多语言 CTC 解码、多目标跟踪与多数表决实现
 */

#include "postprocessor.hpp"
#include "ppocr_dict.hpp"
#include "argus/utils/json.hpp"
#include <cmath>
#include <algorithm>
#include <sstream>
#include <iomanip>
#include <unordered_set>
#include <cctype>

#if defined(__APPLE__)
#include <Accelerate/Accelerate.h>
#endif

namespace lpr {

namespace {

const std::unordered_set<std::string> kChineseProvinces = {
    "京", "沪", "津", "渝", "冀", "晋", "蒙", "辽", "吉", "黑",
    "苏", "浙", "皖", "闽", "赣", "鲁", "豫", "鄂", "湘", "粤",
    "桂", "琼", "川", "贵", "云", "藏", "陕", "甘", "青", "宁",
    "新"
};

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

// 标准化车牌检索文本：过滤标点、分隔符、中点与空格，保留汉字与大写英数字
std::string normalize_plate_string(const std::string& text) {
    std::string norm;
    for (size_t i = 0; i < text.size();) {
        unsigned char c = static_cast<unsigned char>(text[i]);
        if (c < 0x80) {
            if (std::isalnum(c)) {
                norm += static_cast<char>(std::toupper(c));
            }
            i += 1;
        } else {
            size_t len = 1;
            if ((c & 0xE0) == 0xC0) len = 2;
            else if ((c & 0xF0) == 0xE0) len = 3;
            else if ((c & 0xF8) == 0xF0) len = 4;

            std::string utf8_char = text.substr(i, std::min(len, text.size() - i));
            if (utf8_char != "·" && utf8_char != "•" && utf8_char != "　" &&
                utf8_char != "。" && utf8_char != "，" && utf8_char != "、") {
                norm += utf8_char;
            }
            i += len;
        }
    }
    return norm;
}
std::string get_first_utf8_char(const std::string& str) {
    if (str.empty()) return "";
    unsigned char c = static_cast<unsigned char>(str[0]);
    size_t len = 1;
    if ((c & 0xE0) == 0xC0) len = 2;
    else if ((c & 0xF0) == 0xE0) len = 3;
    else if ((c & 0xF8) == 0xF0) len = 4;
    return str.substr(0, std::min(len, str.size()));
}

// 计算 UTF-8 字符长度
size_t count_utf8_chars(const std::string& str) {
    size_t len = 0;
    for (size_t i = 0; i < str.size();) {
        unsigned char c = static_cast<unsigned char>(str[i]);
        if (c < 0x80) i += 1;
        else if ((c & 0xE0) == 0xC0) i += 2;
        else if ((c & 0xF0) == 0xE0) i += 3;
        else i += 4;
        len++;
    }
    return len;
}

// 根据车牌文本内容与物理先验确定颜色与类型
void assign_plate_color_and_type(
    const std::string& plate_text,
    bool is_double_layer,
    std::string& out_color,
    std::string& out_type) {

    if (is_double_layer) {
        out_color = "yellow";
        out_type = "double_yellow";
        return;
    }

    // 1. 公安警车 / 武警 / 军车 (白底)
    if (plate_text.ends_with("警") || plate_text.starts_with("WJ") || plate_text.starts_with("军")) {
        out_color = "white";
        out_type = "police";
        return;
    }

    // 2. 教练车 (黄底)
    if (plate_text.ends_with("学")) {
        out_color = "yellow";
        out_type = "coach";
        return;
    }

    // 3. 港澳车 / 使领馆车 (黑底)
    if (plate_text.ends_with("港") || plate_text.ends_with("澳")) {
        out_color = "black";
        out_type = "hk_macau";
        return;
    }
    if (plate_text.find("使") != std::string::npos || plate_text.find("领") != std::string::npos) {
        out_color = "black";
        out_type = "embassy";
        return;
    }

    std::string first_ch = get_first_utf8_char(plate_text);
    size_t utf8_len = count_utf8_chars(plate_text);
    bool has_chinese_province = kChineseProvinces.contains(first_ch);

    // 4. 中国民用车牌 (首字为 31 省份汉字)
    if (has_chinese_province) {
        if (utf8_len == 8) {
            out_color = "green";
            out_type = "new_energy";
            return;
        }
        out_color = "blue";
        out_type = "standard";
        return;
    }

    // 5. 国际 / 东南亚车牌 (例如越南 34A-231.26 等): 标准白底黑字
    out_color = "white";
    out_type = "standard";
}

} // namespace

Postprocessor::Postprocessor(const Config& cfg)
    : config_(cfg), tracker_(1.0f - cfg.iou_threshold, 30) {}

void Postprocessor::update_config(const Config& cfg) {
    config_ = cfg;
    tracker_ = argus::cv::SimpleTracker(1.0f - cfg.iou_threshold, 30);
    track_states_.clear();
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

    if (rec_out.char_probs.size() < 40 * kPPOCRDictSize) {
        assign_plate_color_and_type(plate_text, is_double_layer, plate_color, plate_type);
        return;
    }

    int prev_idx = 0;
    float conf_sum = 0.0f;
    int char_count = 0;

    for (size_t t = 0; t < 40; ++t) {
        const float* probs = rec_out.char_probs.data() + t * kPPOCRDictSize;

#if defined(__APPLE__)
        float max_val = 0.0f;
        vDSP_Length max_idx_v = 0;
        vDSP_maxvi(probs, 1, &max_val, &max_idx_v, kPPOCRDictSize);
        int max_idx = static_cast<int>(max_idx_v);
#else
        int max_idx = 0;
        float max_val = probs[0];
        for (size_t c = 1; c < kPPOCRDictSize; ++c) {
            if (probs[c] > max_val) {
                max_val = probs[c];
                max_idx = static_cast<int>(c);
            }
        }
#endif

        if (max_idx != 0 && max_idx != prev_idx) {
            const char* ch = kPPOCRDict[max_idx];
            if (ch != nullptr && ch[0] != '\0' && ch[0] != ' ' && std::string(ch) != "　") {
                plate_text += ch;
                conf_sum += probs[max_idx];
                char_count++;
            }
        }
        prev_idx = max_idx;
    }

    if (char_count > 0) {
        ocr_confidence = conf_sum / static_cast<float>(char_count);
    }

    // 格式标准化: 去除空格、点、减号、中点等分隔符，生成大写标准检索串
    normalized_text = normalize_plate_string(plate_text);

    assign_plate_color_and_type(plate_text, is_double_layer, plate_color, plate_type);
}

std::vector<PlateObject> Postprocessor::track_and_vote(
    std::vector<PlateObject>& plates,
    int64_t wall_time_ns) {

    std::vector<argus::cv::DetectionBox> det_boxes;
    det_boxes.reserve(plates.size());

    for (const auto& plate : plates) {
        argus::cv::DetectionBox box{};
        box.x = plate.x_min;
        box.y = plate.y_min;
        box.w = plate.x_max - plate.x_min;
        box.h = plate.y_max - plate.y_min;
        box.confidence = plate.confidence;
        box.class_id = 0;
        det_boxes.push_back(box);
    }

    const auto tracked = tracker_.update(det_boxes);
    std::vector<PlateObject> result_plates;
    result_plates.reserve(tracked.size());

    const int64_t cooldown_ns = static_cast<int64_t>(config_.observation_cooldown_seconds) * 1'000'000'000LL;
    const int64_t state_retention_ns = std::max<int64_t>(cooldown_ns, 30'000'000'000LL);
    if (wall_time_ns > 0) {
        for (auto it = track_states_.begin(); it != track_states_.end();) {
            if (it->second.last_seen_wall_time_ns > 0 &&
                wall_time_ns > it->second.last_seen_wall_time_ns &&
                wall_time_ns - it->second.last_seen_wall_time_ns > state_retention_ns) {
                it = track_states_.erase(it);
            } else {
                ++it;
            }
        }
    }

    std::vector<bool> used(plates.size(), false);
    for (const auto& tracked_box : tracked) {
        size_t source_index = plates.size();
        float best_iou = -1.0f;
        for (size_t candidate_index = 0; candidate_index < plates.size(); ++candidate_index) {
            if (used[candidate_index]) continue;
            const auto& candidate = plates[candidate_index];
            const float iou = compute_iou_xywh_raw(
                tracked_box.x, tracked_box.y, tracked_box.w, tracked_box.h,
                candidate.x_min, candidate.y_min,
                candidate.x_max - candidate.x_min, candidate.y_max - candidate.y_min);
            if (iou > best_iou) {
                best_iou = iou;
                source_index = candidate_index;
            }
        }
        if (source_index == plates.size() || best_iou < 0.0f) continue;
        used[source_index] = true;

        PlateObject obj = plates[source_index];
        obj.track_id = tracked_box.track_id;

        auto& state = track_states_[obj.track_id];
        state.track_id = obj.track_id;
        state.last_seen_wall_time_ns = wall_time_ns;
        state.observed_count++;
        state.highest_score = std::max(state.highest_score, obj.confidence);

        if (obj.ocr_confidence > state.highest_ocr_conf) {
            state.highest_ocr_conf = obj.ocr_confidence;
            state.best_text = obj.plate_text;
            state.best_color = obj.plate_color;
            state.best_type = obj.plate_type;
        }

        if (!obj.plate_text.empty()) {
            // 置信度平方加权表决 (Confidence-Weighted Voting)，高清晰度帧压倒远景模糊低置信度帧
            float weight = std::max(0.01f, obj.ocr_confidence * obj.ocr_confidence);
            state.text_weights[obj.plate_text] += weight;
            state.color_weights[obj.plate_color] += weight;
            state.type_weights[obj.plate_type] += weight;
        }

        std::string best_text = state.best_text.empty() ? obj.plate_text : state.best_text;
        float max_text_weight = 0.0f;
        for (const auto& [text, w] : state.text_weights) {
            if (w > max_text_weight) {
                max_text_weight = w;
                best_text = text;
            }
        }

        std::string best_color = state.best_color.empty() ? obj.plate_color : state.best_color;
        float max_color_weight = 0.0f;
        for (const auto& [color, w] : state.color_weights) {
            if (w > max_color_weight) {
                max_color_weight = w;
                best_color = color;
            }
        }

        std::string best_type = state.best_type.empty() ? obj.plate_type : state.best_type;
        float max_type_weight = 0.0f;
        for (const auto& [type, w] : state.type_weights) {
            if (w > max_type_weight) {
                max_type_weight = w;
                best_type = type;
            }
        }

        obj.plate_text = best_text;
        obj.normalized_text = normalize_plate_string(best_text);
        obj.plate_color = best_color;
        obj.plate_type = best_type;
        obj.ocr_confidence = std::max(obj.ocr_confidence, state.highest_ocr_conf);

        const bool mature = state.observed_count >= config_.voting_window_frames;
        const bool cooldown_expired =
            (wall_time_ns - state.last_reported_wall_time_ns) >= cooldown_ns;
        const bool ocr_valid = (state.highest_ocr_conf >= config_.ocr_confidence_threshold) &&
                               (best_text.size() >= 4);

        if (ocr_valid && (!state.has_reported ? mature : cooldown_expired)) {
            obj.should_report = true;
            state.has_reported = true;
            state.last_reported_wall_time_ns = wall_time_ns;
        }

        result_plates.push_back(std::move(obj));
    }

    return result_plates;
}

std::string Postprocessor::build_result_json(uint64_t frame_id, const std::vector<PlateObject>& plates) const {
    std::ostringstream ss;
    ss << std::fixed << std::setprecision(4);

    ss << "{\n";
    ss << "  \"schema_version\": 1,\n";
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
        ss << "      \"bbox\": [" << p.x_min << ", " << p.y_min << ", "
           << bw << ", " << bh << "],\n";
        ss << "      \"vehicle_bbox\": [" << vx_min << ", " << vy_min << ", "
           << (vx_max - vx_min) << ", " << (vy_max - vy_min) << "]\n";
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
