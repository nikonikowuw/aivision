#pragma once

#include "aivision/core/logging/log_context.hpp"
#include "aivision/core/logging/log_record.hpp"
#include "aivision/core/logging/log_sanitizer.hpp"
#include "aivision/core/logging/log_sink.hpp"
#include "aivision/core/logging/log_stats.hpp"
#include "aivision/core/logging/log_writer.hpp"
#include <atomic>
#include <memory>
#include <source_location>
#include <string>

namespace aivision::logging {

/**
 * @brief Engine 统一结构化日志 Facade
 */
class Logger {
public:
    /**
     * @brief 初始化全局日志系统
     * @param min_level 过滤门槛级别 (默认 INFO)
     * @param sink 自定义 sink (若为空则默认使用 StderrSink)
     */
    static void initialize(Level min_level = Level::Info, std::shared_ptr<LogSink> sink = nullptr) noexcept;

    /**
     * @brief 优雅关闭日志系统 (排空队列)
     */
    static void shutdown() noexcept;

    /**
     * @brief 获取当前最低日志过滤级别
     */
    [[nodiscard]] static Level get_level() noexcept;

    /**
     * @brief 动态设置日志级别
     */
    static void set_level(Level lvl) noexcept;

    /**
     * @brief 获取统计信息快照
     */
    [[nodiscard]] static LoggerStatsSnapshot stats() noexcept;

    /**
     * @brief 核心结构化日志记录写入接口
     */
    static void log(Level lvl,
                    std::string_view component,
                    std::string_view event,
                    std::string_view message,
                    std::string_view code = "",
                    const std::map<std::string, std::string>& extra_fields = {},
                    const SourceLocation& loc = {}) noexcept;

    /**
     * @brief 便捷方法
     */
    static void debug(std::string_view comp, std::string_view evt, std::string_view msg,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Debug, comp, evt, msg, "", {}, loc);
    }
    static void debug(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const SourceLocation& loc = {}) noexcept {
        log(Level::Debug, comp, evt, msg, code, {}, loc);
    }
    static void debug(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const std::map<std::string, std::string>& fields,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Debug, comp, evt, msg, code, fields, loc);
    }

    static void info(std::string_view comp, std::string_view evt, std::string_view msg,
                     const SourceLocation& loc = {}) noexcept {
        log(Level::Info, comp, evt, msg, "", {}, loc);
    }
    static void info(std::string_view comp, std::string_view evt, std::string_view msg,
                     std::string_view code, const SourceLocation& loc = {}) noexcept {
        log(Level::Info, comp, evt, msg, code, {}, loc);
    }
    static void info(std::string_view comp, std::string_view evt, std::string_view msg,
                     std::string_view code, const std::map<std::string, std::string>& fields,
                     const SourceLocation& loc = {}) noexcept {
        log(Level::Info, comp, evt, msg, code, fields, loc);
    }

    static void warn(std::string_view comp, std::string_view evt, std::string_view msg,
                     const SourceLocation& loc = {}) noexcept {
        log(Level::Warn, comp, evt, msg, "", {}, loc);
    }
    static void warn(std::string_view comp, std::string_view evt, std::string_view msg,
                     std::string_view code, const SourceLocation& loc = {}) noexcept {
        log(Level::Warn, comp, evt, msg, code, {}, loc);
    }
    static void warn(std::string_view comp, std::string_view evt, std::string_view msg,
                     std::string_view code, const std::map<std::string, std::string>& fields,
                     const SourceLocation& loc = {}) noexcept {
        log(Level::Warn, comp, evt, msg, code, fields, loc);
    }

    static void error(std::string_view comp, std::string_view evt, std::string_view msg,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Error, comp, evt, msg, "", {}, loc);
    }
    static void error(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const SourceLocation& loc = {}) noexcept {
        log(Level::Error, comp, evt, msg, code, {}, loc);
    }
    static void error(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const std::map<std::string, std::string>& fields,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Error, comp, evt, msg, code, fields, loc);
    }

    static void fatal(std::string_view comp, std::string_view evt, std::string_view msg,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Fatal, comp, evt, msg, "", {}, loc);
    }
    static void fatal(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const SourceLocation& loc = {}) noexcept {
        log(Level::Fatal, comp, evt, msg, code, {}, loc);
    }
    static void fatal(std::string_view comp, std::string_view evt, std::string_view msg,
                      std::string_view code, const std::map<std::string, std::string>& fields,
                      const SourceLocation& loc = {}) noexcept {
        log(Level::Fatal, comp, evt, msg, code, fields, loc);
    }

private:
    static std::atomic<Level> min_level_;
    static std::atomic<uint64_t> global_seq_;
    static std::shared_ptr<LoggerStats> stats_;
    static std::unique_ptr<AsyncLogWriter> writer_;
    static std::mutex init_mutex_;
    static std::atomic<bool> initialized_;
};

} // namespace aivision::logging

// 便捷日志宏 (自动捕获源码位置)
#define LOG_DEBUG(comp, evt, msg, ...) \
    ::aivision::logging::Logger::debug(comp, evt, msg, ##__VA_ARGS__, {__FILE__, __LINE__, __func__})

#define LOG_INFO(comp, evt, msg, ...) \
    ::aivision::logging::Logger::info(comp, evt, msg, ##__VA_ARGS__, {__FILE__, __LINE__, __func__})

#define LOG_WARN(comp, evt, msg, ...) \
    ::aivision::logging::Logger::warn(comp, evt, msg, ##__VA_ARGS__, {__FILE__, __LINE__, __func__})

#define LOG_ERROR(comp, evt, msg, ...) \
    ::aivision::logging::Logger::error(comp, evt, msg, ##__VA_ARGS__, {__FILE__, __LINE__, __func__})

#define LOG_FATAL(comp, evt, msg, ...) \
    ::aivision::logging::Logger::fatal(comp, evt, msg, ##__VA_ARGS__, {__FILE__, __LINE__, __func__})
