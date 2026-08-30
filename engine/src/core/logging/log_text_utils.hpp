/**
 * @file log_text_utils.hpp
 * @brief 日志内部文本规范化辅助函数
 */
#pragma once

#include <string>
#include <string_view>

namespace argus::logging::detail {

/**
 * @brief 将 ASCII 字符串转换为小写
 * @param value 待转换的文本
 * @return 转换后的字符串
 */
inline std::string ascii_lower(std::string_view value) {
    std::string lower;
    lower.reserve(value.size());
    for (const unsigned char ch : value) {
        const unsigned char lower_ch = ch >= 'A' && ch <= 'Z'
            ? static_cast<unsigned char>(ch + ('a' - 'A'))
            : ch;
        lower.push_back(static_cast<char>(lower_ch));
    }
    return lower;
}

} // namespace argus::logging::detail
