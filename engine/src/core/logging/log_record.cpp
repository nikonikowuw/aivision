/**
 * @file log_record.cpp
 * @brief 日志级别解析与字符串表示实现
 */
#include "aivision/core/logging/log_record.hpp"
#include "log_text_utils.hpp"

namespace aivision::logging {

std::optional<Level> parse_level(std::string_view str) noexcept {
    try {
        const std::string lower = detail::ascii_lower(str);

        if (lower == "debug") return Level::Debug;
        if (lower == "info")  return Level::Info;
        if (lower == "warn" || lower == "warning") return Level::Warn;
        if (lower == "error") return Level::Error;
        if (lower == "fatal") return Level::Fatal;
    } catch (...) {
        return std::nullopt;
    }

    return std::nullopt;
}

} // namespace aivision::logging
