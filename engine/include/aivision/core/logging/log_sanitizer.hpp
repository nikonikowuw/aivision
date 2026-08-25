#pragma once

#include <cstdint>
#include <string>
#include <string_view>
#include <utility>

namespace aivision::logging {

/**
 * @brief 字段安全与脱敏清洗工具集
 */
class LogSanitizer {
public:
    static constexpr size_t MAX_MESSAGE_SIZE = 8 * 1024;    // 8 KiB
    static constexpr size_t MAX_FIELD_SIZE = 1 * 1024;      // 1 KiB
    static constexpr size_t MAX_RECORD_SIZE = 16 * 1024;    // 16 KiB

    /**
     * @brief 校验字段名是否在白名单中
     */
    [[nodiscard]] static bool is_field_allowed(std::string_view key) noexcept;

    /**
     * @brief 对 URL 进行安全脱敏（剥离 userinfo 与 query 参数）
     */
    [[nodiscard]] static std::string sanitize_url(std::string_view url);

    /**
     * @brief 校验并清洗算法包不可信自由文本
     * @param raw 原始字节流
     * @param max_len 最大允许长度
     * @return pair<清洗后字符串, 是否截断>
     */
    [[nodiscard]] static std::pair<std::string, bool> sanitize_message(
        std::string_view raw, size_t max_len = MAX_MESSAGE_SIZE);

    /**
     * @brief 截断标量字符串字段
     */
    [[nodiscard]] static std::string truncate_field(
        std::string_view raw, size_t max_len = MAX_FIELD_SIZE);

    /**
     * @brief 提取标准化的短文件名（去除绝对构建路径前缀）
     */
    [[nodiscard]] static const char* normalize_file_path(const char* full_path) noexcept;
};

} // namespace aivision::logging
