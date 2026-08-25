#include "aivision/core/logging/log_sanitizer.hpp"
#include <algorithm>
#include <array>
#include <string>
#include <string_view>
#include <unordered_set>

namespace aivision::logging {

namespace {

// 字段白名单
const std::unordered_set<std::string_view> ALLOWED_FIELDS = {
    "ts", "level", "component", "event", "code", "message", "message_truncated",
    "platform_id", "device_id", "camera_id", "task_id",
    "instance_id", "instance_run_id", "algorithm_id", "package_version",
    "frame_id", "revision", "retry_count", "duration_ms",
    "seq", "file", "line", "function",
    // 允许的受控扩展字段
    "error_stage", "error_code", "package_sha256", "relative_path", "model_path",
    "status_code", "pixel_format", "width", "height", "fps", "drop_count"
};

// 敏感字段黑名单（包含子串匹配即拒绝）
constexpr std::array<std::string_view, 6> SENSITIVE_PATTERNS = {
    "password", "token", "secret", "authorization", "credential", "auth"
};

} // namespace

bool LogSanitizer::is_field_allowed(std::string_view key) noexcept {
    if (key.empty() || key.size() > 64) {
        return false;
    }
    // 检查是否包含敏感字段子串
    std::string lower_key;
    lower_key.reserve(key.size());
    for (char c : key) {
        lower_key.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(c))));
    }
    for (const auto& pattern : SENSITIVE_PATTERNS) {
        if (lower_key.find(pattern) != std::string::npos) {
            return false;
        }
    }
    return ALLOWED_FIELDS.find(key) != ALLOWED_FIELDS.end();
}

std::string LogSanitizer::sanitize_url(std::string_view url) {
    if (url.empty()) {
        return "";
    }

    // 1. 查找 scheme 分隔符 "://"
    size_t scheme_pos = url.find("://");
    if (scheme_pos == std::string_view::npos) {
        // 非标准 URL 格式，直接按最大长度截断返回
        return truncate_field(url);
    }

    std::string result;
    result.reserve(url.size());
    result.append(url.substr(0, scheme_pos + 3)); // 加入 "scheme://"

    std::string_view remainder = url.substr(scheme_pos + 3);

    // 2. 检查是否有 userinfo（即 '@' 符号在路径 '/' 之前）
    size_t slash_pos = remainder.find('/');
    size_t at_pos = remainder.find('@');

    if (at_pos != std::string_view::npos && (slash_pos == std::string_view::npos || at_pos < slash_pos)) {
        // 存在 userinfo (例如 "user:pass@")，将其剥离
        remainder = remainder.substr(at_pos + 1);
    }

    // 3. 检查是否有 query string '?'
    size_t query_pos = remainder.find('?');
    if (query_pos != std::string_view::npos) {
        // 剥离 query string
        remainder = remainder.substr(0, query_pos);
    }

    result.append(remainder);
    return truncate_field(result);
}

std::pair<std::string, bool> LogSanitizer::sanitize_message(std::string_view raw, size_t max_len) {
    if (raw.empty()) {
        return {"", false};
    }

    bool truncated = false;
    std::string_view view = raw;
    if (view.size() > max_len) {
        view = view.substr(0, max_len);
        truncated = true;
    }

    std::string sanitized;
    sanitized.reserve(view.size());

    // 过滤与转义控制字符，保证合法 UTF-8 / ASCII 输出
    for (size_t i = 0; i < view.size(); ++i) {
        unsigned char c = static_cast<unsigned char>(view[i]);
        if (c < 0x20) {
            // 控制字符: 保留常见换行与制表符
            if (c == '\n' || c == '\r' || c == '\t') {
                sanitized.push_back(static_cast<char>(c));
            } else {
                sanitized.push_back(' '); // 其他不可见字符替换为空格
            }
        } else if (c == 0x7F) {
            sanitized.push_back(' '); // DEL
        } else {
            sanitized.push_back(static_cast<char>(c));
        }
    }

    return {std::move(sanitized), truncated};
}

std::string LogSanitizer::truncate_field(std::string_view raw, size_t max_len) {
    if (raw.size() <= max_len) {
        return std::string(raw);
    }
    return std::string(raw.substr(0, max_len));
}

const char* LogSanitizer::normalize_file_path(const char* full_path) noexcept {
    if (!full_path) {
        return "";
    }
    std::string_view path(full_path);
    // 查找项目根特征路径 /engine/ 或 /sdk/
    size_t pos = path.rfind("/engine/");
    if (pos != std::string_view::npos) {
        return full_path + pos + 1; // 返回 "engine/..."
    }
    pos = path.rfind("/sdk/");
    if (pos != std::string_view::npos) {
        return full_path + pos + 1; // 返回 "sdk/..."
    }
    // 否则返回最后一个斜杠之后的文件名
    pos = path.rfind('/');
    if (pos != std::string_view::npos) {
        return full_path + pos + 1;
    }
    return full_path;
}

} // namespace aivision::logging
