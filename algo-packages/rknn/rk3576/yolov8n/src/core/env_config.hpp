#pragma once

#include <string>
#include <string_view>
#include <unordered_map>
#include <fstream>
#include <sstream>
#include <algorithm>

namespace yolov8n {

struct PackageEnvConfig {
    std::string model_path = "model/yolov8n.rknn";
    float conf_thresh = 0.25f;
    float iou_thresh = 0.45f;
    std::string input_image = "testimage.jpg";
    std::string output_image = "result.jpg";
};

namespace detail {

inline std::string trim(std::string_view s) {
    auto start = s.find_first_not_of(" \t\r\n");
    if (start == std::string_view::npos) return "";
    auto end = s.find_last_not_of(" \t\r\n");
    return std::string(s.substr(start, end - start + 1));
}

inline std::unordered_map<std::string, std::string> parse_env_file(const std::string& filepath) {
    std::unordered_map<std::string, std::string> env_map;
    std::ifstream file(filepath);
    if (!file.is_open()) {
        return env_map;
    }

    std::string line;
    while (std::getline(file, line)) {
        std::string trimmed = trim(line);
        if (trimmed.empty() || trimmed[0] == '#') {
            continue;
        }
        auto eq_pos = trimmed.find('=');
        if (eq_pos != std::string::npos) {
            std::string key = trim(trimmed.substr(0, eq_pos));
            std::string val = trim(trimmed.substr(eq_pos + 1));
            // strip surrounding quotes if present
            if (val.size() >= 2 && ((val.front() == '"' && val.back() == '"') || (val.front() == '\'' && val.back() == '\''))) {
                val = val.substr(1, val.size() - 2);
            }
            if (!key.empty()) {
                env_map[key] = val;
            }
        }
    }
    return env_map;
}

} // namespace detail

inline PackageEnvConfig load_package_env(const std::string& package_root) {
    PackageEnvConfig config;
    std::string env_path = package_root.empty() ? ".env" : (package_root + "/.env");
    auto kv = detail::parse_env_file(env_path);

    auto it = kv.find("MODEL_PATH");
    if (it != kv.end() && !it->second.empty()) {
        config.model_path = it->second;
    }

    it = kv.find("CONF_THRESH");
    if (it != kv.end() && !it->second.empty()) {
        try {
            config.conf_thresh = std::stof(it->second);
        } catch (...) {}
    }

    it = kv.find("IOU_THRESH");
    if (it != kv.end() && !it->second.empty()) {
        try {
            config.iou_thresh = std::stof(it->second);
        } catch (...) {}
    }

    it = kv.find("INPUT_IMAGE");
    if (it != kv.end() && !it->second.empty()) {
        config.input_image = it->second;
    }

    it = kv.find("OUTPUT_IMAGE");
    if (it != kv.end() && !it->second.empty()) {
        config.output_image = it->second;
    }

    return config;
}

} // namespace yolov8n
