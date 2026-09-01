#pragma once

#include <bitset>
#include <cerrno>
#include <cmath>
#include <cstdlib>
#include <string>
#include <string_view>
#include <unordered_set>
#include <vector>

namespace yolo26n {

inline const char* const kCocoClasses[80] = {
    "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat", "traffic light",
    "fire hydrant", "stop sign", "parking meter", "bench", "bird", "cat", "dog", "horse", "sheep", "cow",
    "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
    "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove", "skateboard", "surfboard",
    "tennis racket", "bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana", "apple",
    "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
    "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone",
    "microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock", "vase", "scissors", "teddy bear",
    "hair drier", "toothbrush"
};

inline int get_coco_class_id(std::string_view name) {
    for (int i = 0; i < 80; ++i) {
        if (name == kCocoClasses[i]) return i;
    }
    return -1;
}

struct InstanceConfig {
    float confidence_threshold = 0.45f;
    float iou_threshold = 0.45f;
    std::vector<std::string> target_classes = {"person", "car", "motorcycle", "bicycle", "bus", "truck"};
    std::bitset<80> enabled_classes_mask{0};
    std::string custom_alarm_label;

    void update_mask() {
        enabled_classes_mask.reset();
        if (target_classes.empty()) {
            enabled_classes_mask.set(); // 默认全开
            return;
        }
        for (const auto& cls : target_classes) {
            int id = get_coco_class_id(cls);
            if (id >= 0 && id < 80) {
                enabled_classes_mask.set(static_cast<size_t>(id));
            }
        }
    }
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

    bool parse_string_array(std::vector<std::string>& out) {
        skip_whitespace();
        if (!consume('[')) return false;
        out.clear();
        skip_whitespace();
        if (consume(']')) return true;
        while (true) {
            std::string item;
            if (!parse_string(item)) return false;
            out.push_back(std::move(item));
            skip_whitespace();
            if (consume(']')) return true;
            if (!consume(',')) return false;
        }
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
    bool has_target_classes = false;
    bool has_custom_label = false;
    cursor.skip_whitespace();
    if (!cursor.consume('}')) {
        while (true) {
            std::string key;
            if (!cursor.parse_string(key) || !cursor.consume(':')) {
                error = "config contains an invalid member";
                return false;
            }

            if (key == "confidence_threshold") {
                if (has_confidence) {
                    error = "config contains duplicate confidence_threshold";
                    return false;
                }
                float value = 0.0f;
                if (!cursor.parse_number(value) || value < 0.0f || value > 1.0f) {
                    error = "config confidence_threshold must be a finite number in [0, 1]";
                    return false;
                }
                out.confidence_threshold = value;
                has_confidence = true;
            } else if (key == "iou_threshold") {
                if (has_iou) {
                    error = "config contains duplicate iou_threshold";
                    return false;
                }
                float value = 0.0f;
                if (!cursor.parse_number(value) || value < 0.0f || value > 1.0f) {
                    error = "config iou_threshold must be a finite number in [0, 1]";
                    return false;
                }
                out.iou_threshold = value;
                has_iou = true;
            } else if (key == "target_classes") {
                if (has_target_classes) {
                    error = "config contains duplicate target_classes";
                    return false;
                }
                std::vector<std::string> classes;
                if (!cursor.parse_string_array(classes)) {
                    error = "config target_classes must be an array of strings";
                    return false;
                }
                std::unordered_set<std::string> seen;
                for (const auto& cls : classes) {
                    if (get_coco_class_id(cls) < 0) {
                        error = "config target_classes contains unknown COCO class: " + cls;
                        return false;
                    }
                    if (seen.count(cls) > 0) {
                        error = "config target_classes contains duplicate class: " + cls;
                        return false;
                    }
                    seen.insert(cls);
                }
                out.target_classes = std::move(classes);
                has_target_classes = true;
            } else if (key == "custom_alarm_label") {
                if (has_custom_label) {
                    error = "config contains duplicate custom_alarm_label";
                    return false;
                }
                std::string label;
                if (!cursor.parse_string(label)) {
                    error = "config custom_alarm_label must be a string";
                    return false;
                }
                if (label.size() > 64) {
                    error = "config custom_alarm_label exceeds max length 64";
                    return false;
                }
                out.custom_alarm_label = std::move(label);
                has_custom_label = true;
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

    out.update_mask();
    return true;
}

} // namespace yolo26n
