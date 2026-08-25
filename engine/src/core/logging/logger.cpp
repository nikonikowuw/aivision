/**
 * @file logger.cpp
 * @brief 结构化日志 Facade 生命周期、过滤与入队实现
 */
#include "aivision/core/logging/logger.hpp"
#include <cmath>
#include <type_traits>

namespace aivision::logging {

std::atomic<Level> Logger::min_level_{Level::Info};
std::atomic<uint64_t> Logger::global_seq_{1};
std::shared_ptr<LoggerStats> Logger::stats_ = std::make_shared<LoggerStats>();
std::atomic<std::shared_ptr<AsyncLogWriter>> Logger::writer_;
std::mutex Logger::init_mutex_;
std::atomic<bool> Logger::initialized_{false};

void Logger::initialize(Level min_level, std::shared_ptr<LogSink> sink) noexcept {
    std::lock_guard<std::mutex> lock(init_mutex_);
    if (writer_.load(std::memory_order_acquire)) {
        return;
    }

    if (min_level > Level::Fatal) min_level = Level::Info;
    min_level_.store(min_level, std::memory_order_release);
    try {
        if (!sink) sink = std::make_shared<StderrSink>();
        auto writer = std::make_shared<AsyncLogWriter>(std::move(sink), stats_, &global_seq_);
        writer->start();
        writer_.store(std::move(writer), std::memory_order_release);
        initialized_.store(true, std::memory_order_release);
    } catch (...) {
        writer_.store(std::shared_ptr<AsyncLogWriter>{}, std::memory_order_release);
        initialized_.store(false, std::memory_order_release);
        // 保持降级路径也是结构化 JSONL，且不在异常处理路径再次分配内存。
        StderrSink fallback_sink;
        fallback_sink.write_line(
            "{\"seq\":0,\"ts\":\"1970-01-01T00:00:00.000000000Z\",\"level\":\"warn\",\"component\":\"engine.logger\",\"event\":\"logger.initialization_failed\",\"message\":\"structured logger initialization failed\"}\n");
    }
}

void Logger::shutdown() noexcept {
    std::shared_ptr<AsyncLogWriter> writer;
    {
        std::lock_guard<std::mutex> lock(init_mutex_);
        initialized_.store(false, std::memory_order_release);
        writer = writer_.exchange(std::shared_ptr<AsyncLogWriter>{}, std::memory_order_acq_rel);
    }
    if (writer) writer->stop(2000);
}

Level Logger::get_level() noexcept {
    return min_level_.load(std::memory_order_relaxed);
}

LoggerStatsSnapshot Logger::stats() noexcept {
    const auto stats = stats_;
    if (!stats) return {};
    const auto writer = writer_.load(std::memory_order_acquire);
    if (writer) {
        const auto [normal, high] = writer->queue_depths();
        return stats->snapshot(normal, high);
    }
    return stats->snapshot();
}

void Logger::record_unknown_algo_level() noexcept {
    if (stats_) stats_->unknown_algo_levels.fetch_add(1, std::memory_order_relaxed);
}

void Logger::log(Level lvl,
                 std::string_view component,
                 std::string_view event,
                 std::string_view message,
                 std::string_view code,
                 const LogFields& extra_fields,
                 const SourceLocation& loc) noexcept {
    if (lvl < min_level_.load(std::memory_order_relaxed)) return;

    try {
        const auto stats = stats_;
        LogRecord record;
        record.seq = global_seq_.fetch_add(1, std::memory_order_relaxed);
        record.ts = std::chrono::system_clock::now();
        record.level = lvl;
        record.component = LogSanitizer::sanitize_message(component, 64).first;
        record.event = LogSanitizer::sanitize_message(event, 64).first;
        record.code = LogSanitizer::sanitize_message(code, 64).first;
        if (lvl >= Level::Error && record.code.empty()) {
            record.code = "INTERNAL_ERROR";
        }

        auto [sanitized_message, message_truncated] = LogSanitizer::sanitize_message(message);
        record.message = std::move(sanitized_message);
        record.message_truncated = message_truncated;
        record.raw_message_bytes = message.size();
        if (message_truncated && stats) {
            stats->message_truncations.fetch_add(1, std::memory_order_relaxed);
        }

        record.context = LogContext::current();
        record.loc = loc;

        for (const auto& [key, value] : extra_fields) {
            if (!LogSanitizer::is_field_allowed(key)) {
                if (stats) stats->rejected_fields.fetch_add(1, std::memory_order_relaxed);
                continue;
            }

            // 与序列化端 serialize_scalar 保持一致的访问器：字符串清洗，非有限 double 拒绝。
            const auto accepted = std::visit(
                [](const auto& item) -> std::optional<LogFieldValue> {
                    using ItemType = std::decay_t<decltype(item)>;
                    if constexpr (std::is_same_v<ItemType, std::string>) {
                        return LogSanitizer::sanitize_message(
                                   item, LogSanitizer::MAX_FIELD_SIZE)
                            .first;
                    } else if constexpr (std::is_same_v<ItemType, double>) {
                        if (!std::isfinite(item)) return std::nullopt;
                        return item;
                    } else {
                        return item;
                    }
                },
                value);
            if (!accepted) {
                if (stats) stats->rejected_fields.fetch_add(1, std::memory_order_relaxed);
                continue;
            }
            record.extra_fields.emplace(key, std::move(*accepted));
        }

        const auto writer = writer_.load(std::memory_order_acquire);
        if (initialized_.load(std::memory_order_acquire) && writer) {
            writer->enqueue(std::move(record));
        } else {
            // 初始化前或 Writer 创建失败时仍输出合法 JSONL，避免降级路径绕过脱敏与结构化契约。
            static const auto fallback_sink = std::make_shared<StderrSink>();
            fallback_sink->write_line(AsyncLogWriter::serialize_jsonl(record, stats));
        }
    } catch (...) {
        // 日志异常不能越过业务、C ABI 或 shutdown 边界。
    }
}

} // namespace aivision::logging
