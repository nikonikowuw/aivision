/**
 * @file log_sink.hpp
 * @brief 结构化日志 stderr 与线程安全内存输出 sink
 */
#pragma once

#include "aivision/core/logging/log_record.hpp"
#include <cstddef>
#include <memory>
#include <mutex>
#include <string>
#include <string_view>
#include <vector>

namespace aivision::logging {

/**
 * @brief 日志输出目标抽象基类
 */
class LogSink {
public:
    virtual ~LogSink() = default;

    /**
     * @brief 写入已序列化的单行 JSONL 字符串
     * @param line 包含换行符的完整字符串
     * @return 写入是否成功
     */
    virtual bool write_line(std::string_view line) noexcept = 0;

    /**
     * @brief 刷新输出缓冲区
     */
    virtual void flush() noexcept = 0;
};

/**
 * @brief 生产默认 Stderr Sink
 */
class StderrSink : public LogSink {
public:
    bool write_line(std::string_view line) noexcept override;
    void flush() noexcept override;
};

/**
 * @brief 单元测试专用的内存缓冲 Sink
 */
class MemorySink : public LogSink {
public:
    bool write_line(std::string_view line) noexcept override;
    void flush() noexcept override;

    [[nodiscard]] std::vector<std::string> get_lines() const;
    void clear();
    [[nodiscard]] size_t size() const;

private:
    mutable std::mutex mutex_;
    std::vector<std::string> lines_;
};

} // namespace aivision::logging
