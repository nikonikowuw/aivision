/**
 * @file log_record.hpp
 * @brief 结构化日志级别、上下文和记录数据模型
 */
#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <map>
#include <optional>
#include <string>
#include <string_view>
#include <variant>

namespace argus::logging {

/**
 * @brief 日志级别定义 (严格对齐规范五级)
 */
enum class Level : uint8_t {
    Debug = 0,
    Info  = 1,
    Warn  = 2,
    Error = 3,
    Fatal = 4
};

/**
 * @brief 将日志级别转为规范小写字符串
 */
[[nodiscard]] inline const char* to_string(Level lvl) noexcept {
    switch (lvl) {
        case Level::Debug: return "debug";
        case Level::Info:  return "info";
        case Level::Warn:  return "warn";
        case Level::Error: return "error";
        case Level::Fatal: return "fatal";
    }
    return "info";
}

/**
 * @brief 从字符串解析日志级别（不区分大小写，非法值返回 std::nullopt）
 */
[[nodiscard]] std::optional<Level> parse_level(std::string_view str) noexcept;

/**
 * @brief 源码位置信息 (std::source_location 提取物)
 */
struct SourceLocation {
    const char* file{nullptr};
    int line{0};
    const char* function{nullptr};
};

/**
 * @brief 上下文快照数据结构 (入队时深拷贝)
 */
struct LogContextSnapshot {
    std::string platform_id;
    std::string device_id;
    std::string camera_id;
    std::string task_id;
    std::string instance_id;
    std::string instance_run_id;
    std::string algorithm_id;
    std::string package_version;
    int64_t frame_id{-1};
    int64_t revision{-1};
    int32_t retry_count{-1};
    double duration_ms{-1.0};
};

/**
 * @brief 额外结构化字段允许的 JSON 标量类型
 */
using LogFieldValue = std::variant<std::string, bool, int64_t, double>;

/**
 * @brief 结构化日志扩展字段集合
 */
using LogFields = std::map<std::string, LogFieldValue>;

/**
 * @brief 结构化日志记录核心结构
 */
struct LogRecord {
    uint64_t seq{0};                          ///< 进程内单调递增序号
    std::chrono::system_clock::time_point ts; ///< UTC 时间戳
    Level level{Level::Info};                 ///< 日志级别
    std::string component;                    ///< 组件名 (如 "engine.algo_host")
    std::string event;                        ///< 事件名 (小写点号分层，如 "algo.process_failed")
    std::string code;                         ///< 稳定错误码 (如 "ALGO_PROCESS_FAILED")
    std::string message;                      ///< 清洗、脱敏与截断后的可读文本
    bool message_truncated{false};            ///< 是否发生文本截断
    size_t raw_message_bytes{0};              ///< 截断前的原始字节大小
    LogContextSnapshot context;               ///< 上下文快照
    SourceLocation loc;                       ///< 源码位置
    LogFields extra_fields; ///< 受控白名单额外标量字段
};

} // namespace argus::logging
