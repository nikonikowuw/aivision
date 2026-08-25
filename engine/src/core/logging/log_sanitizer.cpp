/**
 * @file log_sanitizer.cpp
 * @brief 日志字段白名单、凭据 URL 脱敏及 UTF-8 清洗实现
 */
#include "aivision/core/logging/log_sanitizer.hpp"
#include "log_text_utils.hpp"
#include <algorithm>
#include <array>
#include <cctype>
#include <string>
#include <string_view>
#include <unordered_set>

namespace aivision::logging {

namespace {

const std::unordered_set<std::string_view> ALLOWED_FIELDS = {
    "ts", "level", "component", "event", "code", "message", "message_truncated",
    "platform_id", "device_id", "camera_id", "task_id",
    "instance_id", "instance_run_id", "algorithm_id", "package_version",
    "frame_id", "revision", "retry_count", "duration_ms",
    "seq", "file", "line", "function",
    "error_stage", "error_code", "package_sha256", "relative_path", "url",
    "status_code", "pixel_format", "width", "height", "fps", "drop_count", "enabled"
};

constexpr std::array<std::string_view, 6> SENSITIVE_PATTERNS = {
    "password", "token", "secret", "authorization", "credential", "auth"
};

bool is_continuation(unsigned char value) noexcept {
    return (value & 0xc0U) == 0x80U;
}

size_t valid_utf8_prefix(std::string_view value) noexcept {
    size_t index = 0;
    while (index < value.size()) {
        const unsigned char first = static_cast<unsigned char>(value[index]);
        size_t length = 1;
        if (first >= 0xc2U && first <= 0xdfU) {
            length = 2;
        } else if (first >= 0xe0U && first <= 0xefU) {
            length = 3;
        } else if (first >= 0xf0U && first <= 0xf4U) {
            length = 4;
        } else if (first >= 0x80U) {
            break;
        }
        if (index + length > value.size()) break;
        for (size_t offset = 1; offset < length; ++offset) {
            if (!is_continuation(static_cast<unsigned char>(value[index + offset]))) return index;
        }
        index += length;
    }
    return index;
}

bool is_url_boundary(char value) noexcept {
    return std::isspace(static_cast<unsigned char>(value)) != 0 ||
           value == '"' || value == '\'' || value == '<' || value == '>' ||
           value == '(' || value == ')' || value == '[' || value == ']' ||
           value == '{' || value == '}';
}

void redact_urls(std::string& value) {
    size_t search_from = 0;
    while (search_from < value.size()) {
        const size_t scheme_end = value.find("://", search_from);
        if (scheme_end == std::string::npos) break;

        size_t scheme_start = scheme_end;
        while (scheme_start > 0) {
            const unsigned char previous = static_cast<unsigned char>(value[scheme_start - 1]);
            if (!std::isalnum(previous) && value[scheme_start - 1] != '+' &&
                value[scheme_start - 1] != '-' && value[scheme_start - 1] != '.') {
                break;
            }
            --scheme_start;
        }
        if (scheme_start == scheme_end) {
            search_from = scheme_end + 3;
            continue;
        }

        size_t end = scheme_end + 3;
        while (end < value.size() && !is_url_boundary(value[end])) ++end;
        const std::string safe = LogSanitizer::sanitize_url(
            std::string_view(value).substr(scheme_start, end - scheme_start));
        value.replace(scheme_start, end - scheme_start, safe);
        search_from = scheme_start + safe.size();
    }
}

void redact_assignments(std::string& value) {
    for (const auto pattern : SENSITIVE_PATTERNS) {
        size_t search_from = 0;
        while (search_from < value.size()) {
            size_t found = std::string::npos;
            for (size_t index = search_from; index + pattern.size() < value.size(); ++index) {
                bool matches = true;
                for (size_t offset = 0; offset < pattern.size(); ++offset) {
                    if (static_cast<char>(std::tolower(static_cast<unsigned char>(value[index + offset]))) !=
                        pattern[offset]) {
                        matches = false;
                        break;
                    }
                }
                if (matches && value[index + pattern.size()] == '=') {
                    found = index;
                    break;
                }
            }
            if (found == std::string::npos) break;

            const size_t value_start = found + pattern.size() + 1;
            size_t value_end = value_start;
            while (value_end < value.size() && !is_url_boundary(value[value_end]) &&
                   value[value_end] != ',' && value[value_end] != ';') {
                ++value_end;
            }
            value.replace(value_start, value_end - value_start, "[redacted]");
            search_from = value_start + sizeof("[redacted]") - 1;
        }
    }
}

} // namespace

bool LogSanitizer::is_field_allowed(std::string_view key) noexcept {
    try {
        if (key.empty() || key.size() > 64) return false;
        const std::string lower_key = detail::ascii_lower(key);
        for (const auto pattern : SENSITIVE_PATTERNS) {
            if (lower_key.find(pattern) != std::string::npos) return false;
        }
        return ALLOWED_FIELDS.find(key) != ALLOWED_FIELDS.end();
    } catch (...) {
        return false;
    }
}

std::string LogSanitizer::sanitize_url(std::string_view url) {
    if (url.empty()) return {};

    const size_t scheme_pos = url.find("://");
    if (scheme_pos == std::string_view::npos) return truncate_field(url);

    std::string result;
    result.reserve(url.size());
    result.append(url.substr(0, scheme_pos + 3));
    std::string_view remainder = url.substr(scheme_pos + 3);

    const size_t slash_pos = remainder.find('/');
    const size_t at_pos = remainder.find('@');
    if (at_pos != std::string_view::npos &&
        (slash_pos == std::string_view::npos || at_pos < slash_pos)) {
        remainder = remainder.substr(at_pos + 1);
    }

    const size_t query_pos = remainder.find_first_of("?#");
    if (query_pos != std::string_view::npos) remainder = remainder.substr(0, query_pos);
    result.append(remainder);
    return truncate_field(result);
}

std::pair<std::string, bool> LogSanitizer::sanitize_message(std::string_view raw, size_t max_len) {
    if (raw.empty()) return {"", false};

    bool truncated = raw.size() > max_len;
    const size_t input_size = std::min(raw.size(), max_len);
    std::string sanitized;
    sanitized.reserve(input_size);

    // 过滤控制字符并拒绝非法 UTF-8，避免异步序列化产生不可解析的 JSON。
    for (size_t index = 0; index < input_size;) {
        const unsigned char first = static_cast<unsigned char>(raw[index]);
        if (first < 0x20U) {
            sanitized.push_back(first == '\n' || first == '\r' || first == '\t' ?
                                    static_cast<char>(first) : ' ');
            ++index;
            continue;
        }
        if (first == 0x7fU) {
            sanitized.push_back(' ');
            ++index;
            continue;
        }
        if (first < 0x80U) {
            sanitized.push_back(static_cast<char>(first));
            ++index;
            continue;
        }

        size_t sequence_length = 0;
        uint32_t code_point = 0;
        if (first >= 0xc2U && first <= 0xdfU) {
            sequence_length = 2;
            code_point = first & 0x1fU;
        } else if (first >= 0xe0U && first <= 0xefU) {
            sequence_length = 3;
            code_point = first & 0x0fU;
        } else if (first >= 0xf0U && first <= 0xf4U) {
            sequence_length = 4;
            code_point = first & 0x07U;
        }

        bool valid = sequence_length != 0 && index + sequence_length <= input_size;
        for (size_t offset = 1; valid && offset < sequence_length; ++offset) {
            valid = is_continuation(static_cast<unsigned char>(raw[index + offset]));
            code_point = (code_point << 6U) |
                         (static_cast<unsigned char>(raw[index + offset]) & 0x3fU);
        }
        valid = valid && code_point <= 0x10ffffU &&
                !(code_point >= 0xd800U && code_point <= 0xdfffU) &&
                !(sequence_length == 3 && code_point < 0x800U) &&
                !(sequence_length == 4 && code_point < 0x10000U);
        if (!valid) {
            sanitized.push_back('?');
            ++index;
            continue;
        }
        sanitized.append(raw.substr(index, sequence_length));
        index += sequence_length;
    }

    redact_urls(sanitized);
    redact_assignments(sanitized);
    if (sanitized.size() > max_len) {
        sanitized.resize(max_len);
        sanitized.resize(valid_utf8_prefix(sanitized));
        truncated = true;
    }
    return {std::move(sanitized), truncated};
}

std::string LogSanitizer::truncate_field(std::string_view raw, size_t max_len) {
    if (raw.size() <= max_len) return std::string(raw);
    const std::string_view prefix = raw.substr(0, max_len);
    return std::string(prefix.substr(0, valid_utf8_prefix(prefix)));
}

const char* LogSanitizer::normalize_file_path(const char* full_path) noexcept {
    if (!full_path) return "";
    const std::string_view path(full_path);
    size_t position = path.rfind("/engine/");
    if (position != std::string_view::npos) return full_path + position + 1;
    position = path.rfind("/sdk/");
    if (position != std::string_view::npos) return full_path + position + 1;
    position = path.rfind('/');
    return position == std::string_view::npos ? full_path : full_path + position + 1;
}

} // namespace aivision::logging
