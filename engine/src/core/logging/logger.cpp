#include "aivision/core/logging/logger.hpp"
#include <iostream>

namespace aivision::logging {

std::atomic<Level> Logger::min_level_{Level::Info};
std::atomic<uint64_t> Logger::global_seq_{1};
std::shared_ptr<LoggerStats> Logger::stats_ = std::make_shared<LoggerStats>();
std::unique_ptr<AsyncLogWriter> Logger::writer_{nullptr};
std::mutex Logger::init_mutex_;
std::atomic<bool> Logger::initialized_{false};

void Logger::initialize(Level min_level, std::shared_ptr<LogSink> sink) noexcept {
    std::lock_guard<std::mutex> lock(init_mutex_);
    if (initialized_.load()) {
        return;
    }

    min_level_.store(min_level, std::memory_order_relaxed);
    if (!sink) {
        sink = std::make_shared<StderrSink>();
    }
    if (!stats_) {
        stats_ = std::make_shared<LoggerStats>();
    }

    writer_ = std::make_unique<AsyncLogWriter>(std::move(sink), stats_);
    writer_->start();
    initialized_.store(true);
}

void Logger::shutdown() noexcept {
    std::lock_guard<std::mutex> lock(init_mutex_);
    if (!initialized_.load()) {
        return;
    }
    if (writer_) {
        writer_->stop(2000);
        writer_.reset();
    }
    initialized_.store(false);
}

Level Logger::get_level() noexcept {
    return min_level_.load(std::memory_order_relaxed);
}

void Logger::set_level(Level lvl) noexcept {
    min_level_.store(lvl, std::memory_order_relaxed);
}

LoggerStatsSnapshot Logger::stats() noexcept {
    if (!stats_) {
        return {};
    }
    if (writer_) {
        auto [norm, high] = writer_->queue_depths();
        return stats_->snapshot(norm, high);
    }
    return stats_->snapshot();
}

void Logger::log(Level lvl,
                 std::string_view component,
                 std::string_view event,
                 std::string_view message,
                 std::string_view code,
                 const std::map<std::string, std::string>& extra_fields,
                 const SourceLocation& loc) noexcept {
    // 快速过滤级别
    if (lvl < min_level_.load(std::memory_order_relaxed)) {
        return;
    }

    try {
        LogRecord record;
        record.seq = global_seq_.fetch_add(1, std::memory_order_relaxed);
        record.ts = std::chrono::system_clock::now();
        record.level = lvl;
        record.component = LogSanitizer::truncate_field(component, 64);
        record.event = LogSanitizer::truncate_field(event, 64);
        record.code = LogSanitizer::truncate_field(code, 64);

        // 清洗 message
        auto [sanitized_msg, truncated] = LogSanitizer::sanitize_message(message);
        record.message = std::move(sanitized_msg);
        record.message_truncated = truncated;
        record.raw_message_bytes = message.size();
        if (truncated && stats_) {
            stats_->message_truncations.fetch_add(1, std::memory_order_relaxed);
        }

        // 捕获上下文快照
        record.context = LogContext::current();
        record.loc = loc;

        // 校验额外字段
        for (const auto& [k, v] : extra_fields) {
            if (LogSanitizer::is_field_allowed(k)) {
                record.extra_fields[k] = LogSanitizer::truncate_field(v);
            } else if (stats_) {
                stats_->rejected_fields.fetch_add(1, std::memory_order_relaxed);
            }
        }

        if (initialized_.load(std::memory_order_acquire) && writer_) {
            writer_->enqueue(std::move(record));
        } else {
            // 初始化前的 fallback 输出 (直接写 stderr)
            std::cerr << "[" << to_string(lvl) << "] " << component << " " << event << ": " << message << "\n";
        }
    } catch (...) {
        // 全异常捕获安全
    }
}

} // namespace aivision::logging
