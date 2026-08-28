#pragma once

#include <cstdint>
#include <string>
#include <string_view>
#include <sstream>

namespace face_recognition {

/**
 * @brief 算法实例私有配置
 */
struct InstanceConfig {
    float person_detection_threshold = 0.35f;
    float face_detection_threshold = 0.50f;
    float face_nms_threshold = 0.40f;
    uint32_t max_person_count = 16;
    uint32_t track_buffer = 30;
    float track_match_threshold = 0.80f;
    uint32_t track_confirm_frames = 2;
    std::string feature_mode = "best_shot"; // "all" or "best_shot"
    uint32_t min_face_size = 40;
    float quality_threshold = 35.0f;
    float quality_update_margin = 10.0f;
    uint32_t reextract_interval_frames = 45;

    static InstanceConfig parse_from_json(std::string_view json, const InstanceConfig& base) {
        InstanceConfig cfg = base;
        if (json.empty()) return cfg;

        // Simple helper to extract numbers from json string
        const auto extract_number = [&](std::string_view key, double& out_val) -> bool {
            size_t pos = json.find(key);
            if (pos == std::string_view::npos) return false;
            size_t colon = json.find(':', pos);
            if (colon == std::string_view::npos) return false;
            size_t start = colon + 1;
            while (start < json.size() && (json[start] == ' ' || json[start] == '\t')) start++;
            size_t end = start;
            while (end < json.size() && ((json[end] >= '0' && json[end] <= '9') || json[end] == '.' || json[end] == '-')) end++;
            if (end > start) {
                try {
                    out_val = std::stod(std::string(json.substr(start, end - start)));
                    return true;
                } catch (...) {
                    return false;
                }
            }
            return false;
        };

        double v = 0.0;
        if (extract_number("\"person_detection_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.person_detection_threshold = static_cast<float>(v);
        }
        if (extract_number("\"face_detection_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.face_detection_threshold = static_cast<float>(v);
        }
        if (extract_number("\"face_nms_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.face_nms_threshold = static_cast<float>(v);
        }
        if (extract_number("\"max_person_count\"", v) && v >= 1.0 && v <= 64.0) {
            cfg.max_person_count = static_cast<uint32_t>(v);
        }
        if (extract_number("\"track_buffer\"", v) && v >= 1.0 && v <= 120.0) {
            cfg.track_buffer = static_cast<uint32_t>(v);
        }
        if (extract_number("\"track_match_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.track_match_threshold = static_cast<float>(v);
        }
        if (extract_number("\"track_confirm_frames\"", v) && v >= 1.0 && v <= 10.0) {
            cfg.track_confirm_frames = static_cast<uint32_t>(v);
        }
        if (json.find("\"feature_mode\"") != std::string_view::npos) {
            if (json.find("\"all\"") != std::string_view::npos) {
                cfg.feature_mode = "all";
            } else if (json.find("\"best_shot\"") != std::string_view::npos) {
                cfg.feature_mode = "best_shot";
            }
        }
        if (extract_number("\"min_face_size\"", v) && v >= 16.0 && v <= 512.0) {
            cfg.min_face_size = static_cast<uint32_t>(v);
        }
        if (extract_number("\"quality_threshold\"", v) && v >= 0.0 && v <= 100.0) {
            cfg.quality_threshold = static_cast<float>(v);
        }
        if (extract_number("\"quality_update_margin\"", v) && v >= 0.0 && v <= 50.0) {
            cfg.quality_update_margin = static_cast<float>(v);
        }
        if (extract_number("\"reextract_interval_frames\"", v) && v >= 0.0 && v <= 300.0) {
            cfg.reextract_interval_frames = static_cast<uint32_t>(v);
        }

        return cfg;
    }
};

} // namespace face_recognition
