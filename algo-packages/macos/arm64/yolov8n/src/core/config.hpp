#pragma once

#include <cerrno>
#include <cmath>
#include <cstdlib>
#include <string>
#include <string_view>

namespace yolov8n {

struct InstanceConfig {
    float confidence_threshold = 0.5f;
    float iou_threshold = 0.45f;
};

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
            if (c == '\\') {
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
                    default: return false;
                }
                continue;
            }
            if (static_cast<unsigned char>(c) < 0x20) return false;
            out.push_back(c);
        }
        return false;
    }

    bool parse_number(float& out) {
        skip_whitespace();
        const size_t begin = position_;
        if (position_ < input_.size() && input_[position_] == '-') ++position_;
        if (position_ >= input_.size() || input_[position_] < '0' || input_[position_] > '9') return false;
        if (input_[position_] == '0') {
            ++position_;
        } else {
            while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') ++position_;
        }
        if (position_ < input_.size() && input_[position_] == '.') {
            ++position_;
            const size_t fraction_begin = position_;
            while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') ++position_;
            if (position_ == fraction_begin) return false;
        }
        if (position_ < input_.size() && (input_[position_] == 'e' || input_[position_] == 'E')) {
            ++position_;
            if (position_ < input_.size() && (input_[position_] == '+' || input_[position_] == '-')) ++position_;
            const size_t exponent_begin = position_;
            while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') ++position_;
            if (position_ == exponent_begin) return false;
        }

        const std::string token(input_.substr(begin, position_ - begin));
        char* end = nullptr;
        errno = 0;
        const float value = std::strtof(token.c_str(), &end);
        if (errno == ERANGE || end != token.c_str() + token.size() || !std::isfinite(value)) return false;
        out = value;
        return true;
    }

    bool at_end() {
        skip_whitespace();
        return position_ == input_.size();
    }

private:
    std::string_view input_;
    size_t position_ = 0;
};

} // namespace detail

inline bool parse_instance_config(std::string_view json, InstanceConfig& out, std::string& error) {
    detail::JsonCursor cursor(json);
    if (!cursor.consume('{')) {
        error = "config must be a JSON object";
        return false;
    }

    bool has_confidence = false;
    bool has_iou = false;
    cursor.skip_whitespace();
    if (!cursor.consume('}')) {
        while (true) {
            std::string key;
            if (!cursor.parse_string(key) || !cursor.consume(':')) {
                error = "config contains an invalid member";
                return false;
            }

            float value = 0.0f;
            if (!cursor.parse_number(value) || value < 0.0f || value > 1.0f) {
                error = "config thresholds must be finite numbers in [0, 1]";
                return false;
            }

            if (key == "confidence_threshold") {
                if (has_confidence) {
                    error = "config contains duplicate confidence_threshold";
                    return false;
                }
                out.confidence_threshold = value;
                has_confidence = true;
            } else if (key == "iou_threshold") {
                if (has_iou) {
                    error = "config contains duplicate iou_threshold";
                    return false;
                }
                out.iou_threshold = value;
                has_iou = true;
            } else {
                error = "config contains an unknown property: " + key;
                return false;
            }

            cursor.skip_whitespace();
            if (cursor.consume('}')) break;
            if (!cursor.consume(',')) {
                error = "config must separate members with commas";
                return false;
            }
        }
    }

    if (!cursor.at_end()) {
        error = "config contains trailing data";
        return false;
    }
    if (!has_confidence || !has_iou) {
        error = "config requires confidence_threshold and iou_threshold";
        return false;
    }
    return true;
}

} // namespace yolov8n
