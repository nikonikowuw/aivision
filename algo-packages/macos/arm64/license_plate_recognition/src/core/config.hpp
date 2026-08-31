#pragma once

#include <string>
#include <string_view>
#include <cstdint>
#include <cstdlib>

namespace lpr {

struct Config {
    float confidence_threshold = 0.5f;
    float iou_threshold = 0.45f;
    float ocr_confidence_threshold = 0.6f;
    int32_t voting_window_frames = 5;
    int32_t observation_cooldown_seconds = 10;

    static Config from_json(const char* json_str, uint32_t len) {
        Config cfg;
        if (!json_str || len == 0) return cfg;
        std::string_view json(json_str, len);

        const auto extract_number = [&](std::string_view key, double& out_val) -> bool {
            size_t pos = json.find(key);
            if (pos == std::string_view::npos) return false;
            size_t colon = json.find(':', pos);
            if (colon == std::string_view::npos) return false;
            size_t start = colon + 1;
            while (start < json.size() && (json[start] == ' ' || json[start] == '\t' || json[start] == '\n')) start++;
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
        if (extract_number("\"confidence_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.confidence_threshold = static_cast<float>(v);
        }
        if (extract_number("\"iou_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.iou_threshold = static_cast<float>(v);
        }
        if (extract_number("\"ocr_confidence_threshold\"", v) && v >= 0.0 && v <= 1.0) {
            cfg.ocr_confidence_threshold = static_cast<float>(v);
        }
        if (extract_number("\"voting_window_frames\"", v) && v >= 1.0 && v <= 100.0) {
            cfg.voting_window_frames = static_cast<int32_t>(v);
        }
        if (extract_number("\"observation_cooldown_seconds\"", v) && v >= 1.0 && v <= 3600.0) {
            cfg.observation_cooldown_seconds = static_cast<int32_t>(v);
        }

        return cfg;
    }
};

} // namespace lpr
