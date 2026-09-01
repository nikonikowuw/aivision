#pragma once

/**
 * @file config.hpp
 * @brief 车牌识别算法参数的结构化 JSON 校验与默认配置
 */

#include <algorithm>
#include <cerrno>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <limits>
#include <string>
#include <string_view>
#include <vector>

namespace lpr {

namespace detail {

class JsonCursor {
public:
    explicit JsonCursor(std::string_view input) : input_(input) {}

    void skip_whitespace() {
        while (position_ < input_.size()) {
            const char c = input_[position_];
            if (c != ' ' && c != '\t' && c != '\n' && c != '\r') break;
            ++position_;
        }
    }

    bool consume(char expected) {
        skip_whitespace();
        if (position_ >= input_.size() || input_[position_] != expected) return false;
        ++position_;
        return true;
    }

    bool parse_string(std::string& out) {
        skip_whitespace();
        if (position_ >= input_.size() || input_[position_] != '"') return false;
        ++position_;
        out.clear();
        while (position_ < input_.size()) {
            const char c = input_[position_++];
            if (c == '"') return true;
            if (static_cast<unsigned char>(c) < 0x20U) return false;
            if (c != '\\') {
                out.push_back(c);
                continue;
            }
            if (position_ >= input_.size()) return false;
            const char escaped = input_[position_++];
            switch (escaped) {
                case '"': out.push_back('"'); break;
                case '\\': out.push_back('\\'); break;
                case '/': out.push_back('/'); break;
                case 'b': out.push_back('\b'); break;
                case 'f': out.push_back('\f'); break;
                case 'n': out.push_back('\n'); break;
                case 'r': out.push_back('\r'); break;
                case 't': out.push_back('\t'); break;
                default:
                    // 配置枚举为 ASCII；拒绝未实现的 \u 转义，避免静默改变字段值。
                    return false;
            }
        }
        return false;
    }

    bool parse_number(double& out) {
        skip_whitespace();
        if (position_ >= input_.size()) return false;
        const char* begin = input_.data() + position_;
        char* end = nullptr;
        errno = 0;
        const double value = std::strtod(begin, &end);
        if (end == begin || errno == ERANGE || !std::isfinite(value)) return false;
        const size_t consumed = static_cast<size_t>(end - begin);
        position_ += consumed;
        out = value;
        return true;
    }

    bool parse_bool(bool& out) {
        skip_whitespace();
        constexpr std::string_view kTrue = "true";
        constexpr std::string_view kFalse = "false";
        if (input_.substr(position_, kTrue.size()) == kTrue) {
            position_ += kTrue.size();
            out = true;
            return true;
        }
        if (input_.substr(position_, kFalse.size()) == kFalse) {
            position_ += kFalse.size();
            out = false;
            return true;
        }
        return false;
    }

    bool at_end() {
        skip_whitespace();
        return position_ == input_.size();
    }

private:
    std::string_view input_;
    size_t position_ = 0;
};

inline bool parse_bounded_number(JsonCursor& cursor, double& value, double min, double max) {
    if (!cursor.parse_number(value) || value < min || value > max) return false;
    return std::isfinite(value);
}

inline bool parse_bounded_integer(JsonCursor& cursor, int32_t& value, int32_t min, int32_t max) {
    double parsed = 0.0;
    if (!parse_bounded_number(cursor, parsed, min, max) || std::floor(parsed) != parsed) return false;
    value = static_cast<int32_t>(parsed);
    return true;
}

} // namespace detail

struct Config {
    float confidence_threshold = 0.5f;
    float iou_threshold = 0.45f;
    float ocr_confidence_threshold = 0.6f;
    int32_t voting_window_frames = 5;
    int32_t observation_cooldown_seconds = 10;
    std::vector<std::string> allowed_plate_colors{
        "black", "blue", "green", "white", "yellow"};
    bool save_plate_crop = true;

    [[nodiscard]] bool is_plate_color_allowed(std::string_view color) const {
        return std::any_of(allowed_plate_colors.begin(), allowed_plate_colors.end(),
                           [color](const std::string& allowed) { return allowed == color; });
    }

    [[nodiscard]] static bool parse_json(const char* json_str, uint32_t len,
                                         Config& out, std::string& error) {
        if (!json_str || len == 0) {
            error = "config JSON is empty";
            return false;
        }

        detail::JsonCursor cursor(std::string_view(json_str, len));
        if (!cursor.consume('{')) {
            error = "config JSON must be an object";
            return false;
        }

        Config candidate;
        bool first = true;
        while (!cursor.consume('}')) {
            if (!first && !cursor.consume(',')) {
                error = "config object separator is invalid";
                return false;
            }
            first = false;

            std::string key;
            if (!cursor.parse_string(key) || !cursor.consume(':')) {
                error = "config object member is invalid";
                return false;
            }

            if (key == "confidence_threshold") {
                double value = 0.0;
                if (!detail::parse_bounded_number(cursor, value, 0.0, 1.0)) {
                    error = "confidence_threshold must be a finite number in [0,1]";
                    return false;
                }
                candidate.confidence_threshold = static_cast<float>(value);
            } else if (key == "iou_threshold") {
                double value = 0.0;
                if (!detail::parse_bounded_number(cursor, value, 0.0, 1.0)) {
                    error = "iou_threshold must be a finite number in [0,1]";
                    return false;
                }
                candidate.iou_threshold = static_cast<float>(value);
            } else if (key == "ocr_confidence_threshold") {
                double value = 0.0;
                if (!detail::parse_bounded_number(cursor, value, 0.0, 1.0)) {
                    error = "ocr_confidence_threshold must be a finite number in [0,1]";
                    return false;
                }
                candidate.ocr_confidence_threshold = static_cast<float>(value);
            } else if (key == "voting_window_frames") {
                if (!detail::parse_bounded_integer(cursor, candidate.voting_window_frames, 1, 100)) {
                    error = "voting_window_frames must be an integer in [1,100]";
                    return false;
                }
            } else if (key == "observation_cooldown_seconds") {
                if (!detail::parse_bounded_integer(
                        cursor, candidate.observation_cooldown_seconds, 1, 3600)) {
                    error = "observation_cooldown_seconds must be an integer in [1,3600]";
                    return false;
                }
            } else if (key == "allowed_plate_colors") {
                if (!cursor.consume('[')) {
                    error = "allowed_plate_colors must be an array";
                    return false;
                }
                candidate.allowed_plate_colors.clear();
                bool first_color = true;
                while (!cursor.consume(']')) {
                    if (!first_color && !cursor.consume(',')) {
                        error = "allowed_plate_colors separator is invalid";
                        return false;
                    }
                    first_color = false;
                    std::string color;
                    if (!cursor.parse_string(color) ||
                        !Config{}.is_plate_color_allowed(color) ||
                        std::find(candidate.allowed_plate_colors.begin(),
                                  candidate.allowed_plate_colors.end(), color) !=
                            candidate.allowed_plate_colors.end()) {
                        error = "allowed_plate_colors contains an unsupported or duplicate color";
                        return false;
                    }
                    candidate.allowed_plate_colors.push_back(std::move(color));
                    if (candidate.allowed_plate_colors.size() > 5) {
                        error = "allowed_plate_colors contains too many values";
                        return false;
                    }
                }
                if (candidate.allowed_plate_colors.empty()) {
                    error = "allowed_plate_colors must not be empty";
                    return false;
                }
            } else if (key == "save_plate_crop") {
                if (!cursor.parse_bool(candidate.save_plate_crop)) {
                    error = "save_plate_crop must be a boolean";
                    return false;
                }
            } else {
                error = "unknown config field: " + key;
                return false;
            }
        }

        if (!cursor.at_end()) {
            error = "trailing data after config JSON";
            return false;
        }
        out = std::move(candidate);
        return true;
    }

    static Config from_json(const char* json_str, uint32_t len) {
        Config cfg;
        std::string error;
        if (!parse_json(json_str, len, cfg, error)) return Config{};
        return cfg;
    }
};

} // namespace lpr
