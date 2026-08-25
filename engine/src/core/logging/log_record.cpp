#include "aivision/core/logging/log_record.hpp"
#include <algorithm>
#include <cctype>

namespace aivision::logging {

std::optional<Level> parse_level(std::string_view str) noexcept {
    std::string lower;
    lower.reserve(str.size());
    for (char ch : str) {
        lower.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(ch))));
    }

    if (lower == "debug") return Level::Debug;
    if (lower == "info")  return Level::Info;
    if (lower == "warn" || lower == "warning")  return Level::Warn;
    if (lower == "error") return Level::Error;
    if (lower == "fatal") return Level::Fatal;

    return std::nullopt;
}

} // namespace aivision::logging
