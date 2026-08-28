#pragma once

#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iomanip>
#include <limits>
#include <sstream>
#include <string>
#include <string_view>
#include <vector>

#include "aivision/cv/nms.hpp"
#include "aivision/result.h"

namespace aivision::utils {

inline std::string escape_json_string(std::string_view value) {
    std::string escaped;
    escaped.reserve(value.size());
    for (const char c : value) {
        switch (c) {
            case '"': escaped += "\\\""; break;
            case '\\': escaped += "\\\\"; break;
            case '\b': escaped += "\\b"; break;
            case '\f': escaped += "\\f"; break;
            case '\n': escaped += "\\n"; break;
            case '\r': escaped += "\\r"; break;
            case '\t': escaped += "\\t"; break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    escaped += "?";
                } else {
                    escaped.push_back(c);
                }
                break;
        }
    }
    return escaped;
}

inline std::string serialize_alarm_json(
    const std::string& event_id,
    const std::string& alarm_type_id,
    const std::vector<aivision::cv::DetectionBox>& objects
) {
    std::ostringstream ss;
    ss << "{\n";
    ss << "  \"event_id\": \"" << escape_json_string(event_id) << "\",\n";
    ss << "  \"alarm_type_id\": \"" << escape_json_string(alarm_type_id) << "\",\n";
    ss << "  \"objects\": [";
    for (size_t i = 0; i < objects.size(); ++i) {
        const auto& obj = objects[i];
        if (i > 0) ss << ",";
        ss << "\n    {\n";
        ss << "      \"label\": \"" << escape_json_string(obj.label) << "\",\n";
        ss << std::fixed << std::setprecision(4);
        ss << "      \"confidence\": " << obj.confidence << ",\n";
        ss << "      \"bbox\": [" << obj.x << ", " << obj.y << ", " << obj.w << ", " << obj.h << "],\n";
        ss << "      \"track_id\": " << obj.track_id << "\n";
        ss << "    }";
    }
    if (!objects.empty()) ss << "\n  ";
    ss << "]\n";
    ss << "}";
    return ss.str();
}

inline std::string serialize_self_test_json(const std::vector<std::string>& stages, uint32_t object_count) {
    std::ostringstream ss;
    ss << "{\n";
    ss << "  \"status\": \"ok\",\n";
    ss << "  \"stages\": [";
    for (size_t i = 0; i < stages.size(); ++i) {
        if (i > 0) ss << ", ";
        ss << "\"" << escape_json_string(stages[i]) << "\"";
    }
    ss << "],\n";
    ss << "  \"object_count\": " << object_count << "\n";
    ss << "}";
    return ss.str();
}

struct ParsedAlarmJson {
    std::string event_id;
    std::string alarm_type_id;
    std::vector<aivision::cv::DetectionBox> objects;
};

namespace detail {

class ResultJsonCursor {
public:
    explicit ResultJsonCursor(std::string_view input) : input_(input) {}

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
            if (c != '\\' || position_ >= input_.size()) {
                if (static_cast<unsigned char>(c) < 0x20) return false;
                out.push_back(c);
                continue;
            }
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
        }
        return false;
    }

    bool parse_number(double& out) {
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
            const size_t begin_fraction = position_;
            while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') ++position_;
            if (position_ == begin_fraction) return false;
        }
        if (position_ < input_.size() && (input_[position_] == 'e' || input_[position_] == 'E')) {
            ++position_;
            if (position_ < input_.size() && (input_[position_] == '+' || input_[position_] == '-')) ++position_;
            const size_t begin_exponent = position_;
            while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') ++position_;
            if (position_ == begin_exponent) return false;
        }
        const std::string token(input_.substr(begin, position_ - begin));
        char* end = nullptr;
        const double value = std::strtod(token.c_str(), &end);
        if (end != token.c_str() + token.size() || !std::isfinite(value)) return false;
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

bool parse_object(ResultJsonCursor& cursor, aivision::cv::DetectionBox& object) {
    if (!cursor.consume('{')) return false;
    bool has_label = false;
    bool has_confidence = false;
    bool has_bbox = false;
    bool has_track_id = false;
    cursor.skip_whitespace();
    if (!cursor.consume('}')) {
        while (true) {
            std::string key;
            if (!cursor.parse_string(key) || !cursor.consume(':')) return false;
            if (key == "label") {
                if (has_label || !cursor.parse_string(object.label)) return false;
                has_label = true;
            } else if (key == "confidence") {
                double value = 0.0;
                if (has_confidence || !cursor.parse_number(value) || value < 0.0 || value > 1.0) return false;
                object.confidence = static_cast<float>(value);
                has_confidence = true;
            } else if (key == "bbox") {
                if (has_bbox || !cursor.consume('[')) return false;
                double values[4]{};
                for (double& value : values) {
                    if (!cursor.parse_number(value) || value < 0.0 || value > 1.0) return false;
                    if (&value != &values[3] && !cursor.consume(',')) return false;
                }
                if (!cursor.consume(']') || values[0] + values[2] > 1.0 || values[1] + values[3] > 1.0) return false;
                object.x = static_cast<float>(values[0]);
                object.y = static_cast<float>(values[1]);
                object.w = static_cast<float>(values[2]);
                object.h = static_cast<float>(values[3]);
                has_bbox = true;
            } else if (key == "track_id") {
                double value = 0.0;
                if (has_track_id || !cursor.parse_number(value) || value < static_cast<double>(std::numeric_limits<int64_t>::min()) ||
                    value > static_cast<double>(std::numeric_limits<int64_t>::max()) || std::floor(value) != value) return false;
                object.track_id = static_cast<int64_t>(value);
                has_track_id = true;
            } else {
                return false;
            }
            cursor.skip_whitespace();
            if (cursor.consume('}')) break;
            if (!cursor.consume(',')) return false;
        }
    }
    return has_label && has_confidence && has_bbox && has_track_id;
}

} // namespace detail

inline bool parse_alarm_json(std::string_view json, ParsedAlarmJson& out, std::string& error) {
    if (json.empty() || json.size() > AV_MAX_RESULT_JSON_BYTES) {
        error = "result JSON size is invalid";
        return false;
    }
    detail::ResultJsonCursor cursor(json);
    if (!cursor.consume('{')) {
        error = "result JSON must be an object";
        return false;
    }
    bool has_event_id = false;
    bool has_alarm_type_id = false;
    bool has_objects = false;
    cursor.skip_whitespace();
    if (!cursor.consume('}')) {
        while (true) {
            std::string key;
            if (!cursor.parse_string(key) || !cursor.consume(':')) {
                error = "result JSON contains an invalid member";
                return false;
            }
            if (key == "event_id") {
                if (has_event_id || !cursor.parse_string(out.event_id) || out.event_id.empty()) {
                    error = "result event_id is invalid";
                    return false;
                }
                has_event_id = true;
            } else if (key == "alarm_type_id") {
                if (has_alarm_type_id || !cursor.parse_string(out.alarm_type_id) || out.alarm_type_id.empty()) {
                    error = "result alarm_type_id is invalid";
                    return false;
                }
                has_alarm_type_id = true;
            } else if (key == "objects") {
                if (has_objects || !cursor.consume('[')) {
                    error = "result objects is invalid";
                    return false;
                }
                out.objects.clear();
                cursor.skip_whitespace();
                if (!cursor.consume(']')) {
                    while (true) {
                        aivision::cv::DetectionBox object{};
                        if (!detail::parse_object(cursor, object)) {
                            error = "result object is invalid";
                            return false;
                        }
                        out.objects.push_back(std::move(object));
                        if (out.objects.size() > 4096) {
                            error = "result contains too many objects";
                            return false;
                        }
                        cursor.skip_whitespace();
                        if (cursor.consume(']')) break;
                        if (!cursor.consume(',')) {
                            error = "result objects must be comma separated";
                            return false;
                        }
                    }
                }
                has_objects = true;
            } else {
                error = "result contains an unknown property: " + key;
                return false;
            }
            cursor.skip_whitespace();
            if (cursor.consume('}')) break;
            if (!cursor.consume(',')) {
                error = "result members must be comma separated";
                return false;
            }
        }
    }
    if (!cursor.at_end() || !has_event_id || !has_alarm_type_id || !has_objects) {
        error = "result JSON is missing required fields";
        return false;
    }
    return true;
}

} // namespace aivision::utils
