/**
 * @file log_writer.cpp
 * @brief 双队列异步日志写出、序列化及有界停机实现
 */
#include "argus/core/logging/log_writer.hpp"
#include "argus/core/logging/log_sanitizer.hpp"
#include <algorithm>
#include <chrono>
#include <cmath>
#include <condition_variable>
#include <cstdio>
#include <ctime>
#include <deque>
#include <iomanip>
#include <mutex>
#include <semaphore>
#include <sstream>
#include <type_traits>

namespace argus::logging {

namespace {

// 序列化失败或极端超限时的兜底 JSONL 行 (必须保持单行合法 JSON)。
constexpr std::string_view SERIALIZATION_FALLBACK_LINE =
    "{\"seq\":0,\"ts\":\"1970-01-01T00:00:00.000000000Z\",\"level\":\"fatal\",\"component\":\"engine.logger\",\"event\":\"logger.serialization_error\",\"message\":\"log record dropped\"}\n";

// JSON 字符串安全转义；消息在入队前已经完成 UTF-8 和控制字符清洗。
std::string escape_json_str(std::string_view value) {
    std::string escaped;
    escaped.reserve(value.size() + 8);
    for (const char c : value) {
        switch (c) {
            case '"':  escaped += "\\\""; break;
            case '\\': escaped += "\\\\"; break;
            case '\b':  escaped += "\\b"; break;
            case '\f':  escaped += "\\f"; break;
            case '\n':  escaped += "\\n"; break;
            case '\r':  escaped += "\\r"; break;
            case '\t':  escaped += "\\t"; break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    escaped += "?";
                } else {
                    escaped.push_back(c);
                }
                break;
        }
    }
    return escaped;
}

// 将系统时钟格式化为 RFC 3339 纳秒字符串 (UTC)。
std::string format_rfc3339_nano(std::chrono::system_clock::time_point tp) {
    const auto duration = tp.time_since_epoch();
    const auto seconds = std::chrono::duration_cast<std::chrono::seconds>(duration);
    const auto nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(duration - seconds).count();

    const std::time_t tt = std::chrono::system_clock::to_time_t(tp);
    std::tm gmt{};
    if (gmtime_r(&tt, &gmt) == nullptr) {
        return "1970-01-01T00:00:00.000000000Z";
    }

    char buf[64]{};
    const size_t len = std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%S", &gmt);
    if (len == 0) {
        return "1970-01-01T00:00:00.000000000Z";
    }
    std::snprintf(buf + len, sizeof(buf) - len, ".%09lldZ", static_cast<long long>(nanos));
    return std::string(buf);
}

template <typename Queue>
void drop_queue(Queue& queue,
                const std::shared_ptr<LoggerStats>& stats,
                std::atomic<uint64_t>& pending,
                bool high_priority) noexcept {
    const uint64_t dropped = static_cast<uint64_t>(queue.size());
    if (dropped == 0) return;
    if (stats) {
        auto& counter = high_priority ? stats->dropped_high : stats->dropped_normal;
        counter.fetch_add(dropped, std::memory_order_relaxed);
    }
    pending.fetch_add(dropped, std::memory_order_relaxed);
    queue.clear();
}

void record_drop(const std::shared_ptr<LoggerStats>& stats,
                 std::atomic<uint64_t>& pending,
                 bool high_priority,
                 uint64_t count = 1) noexcept {
    if (stats) {
        auto& counter = high_priority ? stats->dropped_high : stats->dropped_normal;
        counter.fetch_add(count, std::memory_order_relaxed);
    }
    pending.fetch_add(count, std::memory_order_relaxed);
}

void record_sink_failure(const std::shared_ptr<LoggerStats>& stats,
                         std::atomic<uint64_t>& pending,
                         uint64_t count = 1) noexcept {
    if (stats) stats->sink_write_failures.fetch_add(count, std::memory_order_relaxed);
    pending.fetch_add(count, std::memory_order_relaxed);
}

std::string serialize_scalar(const LogFieldValue& value) {
    return std::visit([](const auto& item) -> std::string {
        using ValueType = std::decay_t<decltype(item)>;
        if constexpr (std::is_same_v<ValueType, std::string>) {
            return std::string("\"") + escape_json_str(item) + "\"";
        } else if constexpr (std::is_same_v<ValueType, bool>) {
            return item ? "true" : "false";
        } else if constexpr (std::is_same_v<ValueType, int64_t>) {
            return std::to_string(item);
        } else {
            if (!std::isfinite(item)) return "0.0";
            std::ostringstream value_stream;
            value_stream << std::setprecision(15) << item;
            return value_stream.str();
        }
    }, value);
}

std::string serialize_unbounded(const LogRecord& record) {
    std::ostringstream ss;
    ss << "{";
    ss << "\"seq\":" << record.seq << ",";
    ss << "\"ts\":\"" << format_rfc3339_nano(record.ts) << "\",";
    ss << "\"level\":\"" << to_string(record.level) << "\",";
    ss << "\"component\":\"" << escape_json_str(record.component) << "\",";
    ss << "\"event\":\"" << escape_json_str(record.event) << "\",";
    ss << "\"message\":\"" << escape_json_str(record.message) << "\"";

    if (!record.code.empty()) {
        ss << ",\"code\":\"" << escape_json_str(record.code) << "\"";
    }
    if (record.message_truncated) {
        ss << ",\"message_truncated\":true,\"raw_message_bytes\":" << record.raw_message_bytes;
    }

    const auto& ctx = record.context;
    const auto append_context = [&](std::string_view key, const std::string& value) {
        if (!value.empty()) {
            const auto sanitized = LogSanitizer::sanitize_message(value, LogSanitizer::MAX_FIELD_SIZE).first;
            ss << ",\"" << key << "\":\"" << escape_json_str(sanitized) << "\"";
        }
    };
    append_context("platform_id", ctx.platform_id);
    append_context("device_id", ctx.device_id);
    append_context("camera_id", ctx.camera_id);
    append_context("task_id", ctx.task_id);
    append_context("instance_id", ctx.instance_id);
    append_context("instance_run_id", ctx.instance_run_id);
    append_context("algorithm_id", ctx.algorithm_id);
    append_context("package_version", ctx.package_version);
    if (ctx.frame_id >= 0) ss << ",\"frame_id\":" << ctx.frame_id;
    if (ctx.revision >= 0) ss << ",\"revision\":" << ctx.revision;
    if (ctx.retry_count >= 0) ss << ",\"retry_count\":" << ctx.retry_count;
    if (std::isfinite(ctx.duration_ms) && ctx.duration_ms >= 0.0) {
        ss << ",\"duration_ms\":" << std::fixed << std::setprecision(3) << ctx.duration_ms;
    }

    // DEBUG、ERROR 和 FATAL 才携带源码位置，避免默认 INFO 增加额外负担。
    if (record.level == Level::Debug || record.level >= Level::Error) {
        if (record.loc.file) {
            ss << ",\"file\":\"" << escape_json_str(LogSanitizer::normalize_file_path(record.loc.file)) << "\"";
            ss << ",\"line\":" << record.loc.line;
        }
        if (record.loc.function) {
            ss << ",\"function\":\"" << escape_json_str(record.loc.function) << "\"";
        }
    }

    for (const auto& [key, value] : record.extra_fields) {
        ss << ",\"" << escape_json_str(key) << "\":" << serialize_scalar(value);
    }

    ss << "}\n";
    return ss.str();
}

} // namespace

struct AsyncLogWriter::State {
    std::shared_ptr<LogSink> sink;
    std::shared_ptr<LoggerStats> stats;
    mutable std::mutex mutex;
    std::deque<LogRecord> normal_queue;
    std::deque<LogRecord> high_queue;
    std::atomic<bool> running{false};
    std::atomic<bool> done{true};
    std::mutex done_mutex;
    std::condition_variable done_cv;
    std::counting_semaphore<4096> wake{0};
    std::chrono::steady_clock::time_point stop_deadline = std::chrono::steady_clock::time_point::max();
    std::atomic<uint64_t> pending_normal_drops{0};
    std::atomic<uint64_t> pending_high_drops{0};
    std::atomic<uint64_t> pending_sink_failures{0};
    std::atomic<uint64_t>* sequence{nullptr};
    std::chrono::steady_clock::time_point last_diagnostic = std::chrono::steady_clock::time_point::min();
};

AsyncLogWriter::AsyncLogWriter(std::shared_ptr<LogSink> sink,
                               std::shared_ptr<LoggerStats> stats,
                               std::atomic<uint64_t>* sequence)
    : state_(std::make_shared<State>()) {
    state_->sink = std::move(sink);
    state_->stats = std::move(stats);
    state_->sequence = sequence;
}

AsyncLogWriter::~AsyncLogWriter() {
    stop();
}

void AsyncLogWriter::start() {
    if (!state_ || state_->running.exchange(true, std::memory_order_acq_rel)) {
        return;
    }
    state_->done.store(false, std::memory_order_release);
    {
        std::lock_guard<std::mutex> lock(state_->mutex);
        state_->stop_deadline = std::chrono::steady_clock::time_point::max();
    }

    const auto state = state_;
    try {
        worker_thread_ = std::thread([state] { worker_loop(state); });
    } catch (...) {
        state->running.store(false, std::memory_order_release);
        state->done.store(true, std::memory_order_release);
        state->done_cv.notify_all();
        throw;
    }
}

void AsyncLogWriter::stop(uint32_t timeout_ms) noexcept {
    if (!state_) {
        return;
    }

    const bool worker_joinable = worker_thread_.joinable();
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(timeout_ms);
    {
        std::lock_guard<std::mutex> lock(state_->mutex);
        state_->stop_deadline = deadline;
        state_->running.store(false, std::memory_order_release);
    }
    if (worker_joinable) {
        state_->wake.release();
    }

    if (worker_thread_.joinable()) {
        std::unique_lock<std::mutex> done_lock(state_->done_mutex);
        const bool finished = state_->done_cv.wait_until(
            done_lock, deadline, [this] { return state_->done.load(std::memory_order_acquire); });
        done_lock.unlock();
        if (finished) {
            worker_thread_.join();
            if (state_->sink) state_->sink->flush();
        } else {
            // worker 只捕获共享 State，超时后分离不会产生悬空 this；剩余记录由 worker 丢弃。
            worker_thread_.detach();
        }
    } else if (state_->done.load(std::memory_order_acquire) && state_->sink) {
        state_->sink->flush();
    }
}

bool AsyncLogWriter::enqueue(LogRecord&& record) noexcept {
    if (!state_) return false;

    const bool high_priority = record.level >= Level::Warn;
    std::atomic<uint64_t>& drop_counter =
        high_priority ? state_->pending_high_drops : state_->pending_normal_drops;
    try {
        for (int attempt = 0; attempt < 8; ++attempt) {
            std::unique_lock<std::mutex> lock(state_->mutex, std::try_to_lock);
            if (!lock.owns_lock()) {
                std::this_thread::yield();
                continue;
            }
            if (!state_->running.load(std::memory_order_acquire)) {
                record_drop(state_->stats, drop_counter, high_priority);
                return false;
            }

            auto& queue = high_priority ? state_->high_queue : state_->normal_queue;
            const size_t capacity = high_priority ? HIGH_QUEUE_CAPACITY : NORMAL_QUEUE_CAPACITY;
            if (queue.size() >= capacity) {
                record_drop(state_->stats, drop_counter, high_priority);
                return false;
            }
            queue.emplace_back(std::move(record));
            lock.unlock();
            state_->wake.release();
            return true;
        }

        record_drop(state_->stats, drop_counter, high_priority);
        return false;
    } catch (...) {
        record_drop(state_->stats, drop_counter, high_priority);
        return false;
    }
}

void AsyncLogWriter::flush() noexcept {
    if (state_ && state_->sink) state_->sink->flush();
}

std::pair<size_t, size_t> AsyncLogWriter::queue_depths() const noexcept {
    if (!state_) return {0, 0};
    std::lock_guard<std::mutex> lock(state_->mutex);
    return {state_->normal_queue.size(), state_->high_queue.size()};
}

void AsyncLogWriter::worker_loop(std::shared_ptr<State> state) noexcept {
    try {
        std::vector<LogRecord> batch;
        batch.reserve(BATCH_SIZE);
        bool prefer_normal = false;

        const auto take_batch = [&] {
            batch.clear();
            auto pop = [&](auto& queue) {
                while (!queue.empty() && batch.size() < BATCH_SIZE) {
                    batch.emplace_back(std::move(queue.front()));
                    queue.pop_front();
                }
            };
            if (!state->normal_queue.empty() && (state->high_queue.empty() || prefer_normal)) {
                pop(state->normal_queue);
                prefer_normal = false;
            } else if (!state->high_queue.empty()) {
                pop(state->high_queue);
                prefer_normal = !state->normal_queue.empty();
            } else {
                pop(state->normal_queue);
                prefer_normal = false;
            }
        };

        while (state->running.load(std::memory_order_acquire)) {
            state->wake.try_acquire_for(std::chrono::milliseconds(10));
            if (!state->running.load(std::memory_order_acquire)) break;
            {
                std::unique_lock<std::mutex> lock(state->mutex, std::try_to_lock);
                if (!lock.owns_lock()) continue;
                if (state->high_queue.empty() && state->normal_queue.empty()) continue;
                take_batch();
            }
            if (!batch.empty()) {
                process_batch(state, batch);
                emit_diagnostic_summary(state);
            }
        }

        // 停机阶段在 deadline 内优先排空高低队列，超时则统计并丢弃剩余记录。
        for (;;) {
            {
                std::unique_lock<std::mutex> lock(state->mutex);
                if (state->high_queue.empty() && state->normal_queue.empty()) break;
                if (std::chrono::steady_clock::now() >= state->stop_deadline) {
                    drop_queue(state->high_queue, state->stats, state->pending_high_drops, true);
                    drop_queue(state->normal_queue, state->stats, state->pending_normal_drops, false);
                    break;
                }
                take_batch();
            }
            if (!batch.empty()) {
                process_batch(state, batch);
                emit_diagnostic_summary(state);
            }
        }
    } catch (...) {
        std::lock_guard<std::mutex> lock(state->mutex);
        drop_queue(state->high_queue, state->stats, state->pending_high_drops, true);
        drop_queue(state->normal_queue, state->stats, state->pending_normal_drops, false);
    }

    emit_diagnostic_summary(state);
    state->done.store(true, std::memory_order_release);
    state->done_cv.notify_all();
}

void AsyncLogWriter::emit_diagnostic_summary(const std::shared_ptr<State>& state) noexcept {
    try {
        if (!state || !state->sink) return;

        const auto now = std::chrono::steady_clock::now();
        if (state->last_diagnostic != std::chrono::steady_clock::time_point::min()
            && now - state->last_diagnostic < std::chrono::seconds(5)) {
            return;
        }

        const uint64_t normal_drops = state->pending_normal_drops.exchange(0, std::memory_order_acq_rel);
        const uint64_t high_drops = state->pending_high_drops.exchange(0, std::memory_order_acq_rel);
        const uint64_t sink_failures = state->pending_sink_failures.exchange(0, std::memory_order_acq_rel);
        if (normal_drops == 0 && high_drops == 0 && sink_failures == 0) return;

        state->last_diagnostic = now;
        const auto write_summary = [&](std::string_view event,
                                       std::string_view code,
                                       std::string_view message,
                                       uint64_t count) {
            std::ostringstream line;
            const uint64_t seq = state->sequence
                ? state->sequence->fetch_add(1, std::memory_order_relaxed)
                : 0;
            line << "{\"seq\":" << seq
                 << ",\"ts\":\"" << format_rfc3339_nano(std::chrono::system_clock::now())
                 << "\",\"level\":\"warn\",\"component\":\"engine.logger\""
                 << ",\"event\":\"" << event
                 << "\",\"code\":\"" << code
                 << "\",\"message\":\"" << message
                 << "\",\"drop_count\":" << count << "}\n";
            if (!state->sink->write_line(line.str())) {
                if (state->stats) state->stats->sink_write_failures.fetch_add(1, std::memory_order_relaxed);
            }
        };

        if (normal_drops > 0) {
            write_summary("logger.normal_queue_dropped", "LOGGER_QUEUE_DROPPED",
                          "normal-priority log records were dropped", normal_drops);
        }
        if (high_drops > 0) {
            write_summary("logger.high_queue_dropped", "LOGGER_QUEUE_DROPPED",
                          "high-priority log records were dropped", high_drops);
        }
        if (sink_failures > 0) {
            write_summary("logger.sink_write_failed", "LOGGER_SINK_WRITE_FAILED",
                          "log sink writes failed", sink_failures);
        }
    } catch (...) {
        // 诊断摘要不得影响 Writer 线程的排空与退出。
    }
}

void AsyncLogWriter::process_batch(const std::shared_ptr<State>& state,
                                   std::vector<LogRecord>& records) noexcept {
    if (!state->sink) {
        record_sink_failure(state->stats, state->pending_sink_failures, records.size());
        return;
    }

    for (const auto& record : records) {
        const std::string line = serialize_jsonl(record, state->stats);
        if (!state->sink->write_line(line)) {
            record_sink_failure(state->stats, state->pending_sink_failures);
        } else if (state->stats) {
            state->stats->records_written.fetch_add(1, std::memory_order_relaxed);
        }
    }
}

std::string AsyncLogWriter::serialize_jsonl(const LogRecord& record,
                                           const std::shared_ptr<LoggerStats>& stats) noexcept {
    try {
        std::string line = serialize_unbounded(record);
        if (line.size() <= LogSanitizer::MAX_RECORD_SIZE) return line;

        // 先丢弃可选扩展字段，尽量保留完整的标准上下文。
        LogRecord bounded = record;
        bounded.extra_fields.clear();
        line = serialize_unbounded(bounded);
        if (line.size() <= LogSanitizer::MAX_RECORD_SIZE) return line;

        // 仍超限时二分查找可容纳的消息字节数，避免生成半截 JSON。
        if (!record.message_truncated && stats) {
            stats->message_truncations.fetch_add(1, std::memory_order_relaxed);
        }
        bounded.message_truncated = true;
        bounded.raw_message_bytes = std::max(record.raw_message_bytes, record.message.size());
        const std::string original_message = record.message;
        size_t low = 0;
        size_t high = original_message.size();
        while (low < high) {
            const size_t middle = low + (high - low + 1) / 2;
            bounded.message = LogSanitizer::sanitize_message(
                std::string_view(original_message).substr(0, middle), middle).first;
            if (serialize_unbounded(bounded).size() <= LogSanitizer::MAX_RECORD_SIZE) {
                low = middle;
            } else {
                high = middle - 1;
            }
        }
        bounded.message = LogSanitizer::sanitize_message(
            std::string_view(original_message).substr(0, low), low).first;
        line = serialize_unbounded(bounded);

        if (line.size() > LogSanitizer::MAX_RECORD_SIZE) {
            // 极端情况下标准上下文本身已超限，只保留稳定必填字段。
            bounded.context = {};
            bounded.loc = {};
            bounded.code.clear();
            bounded.message = "log record truncated";
            line = serialize_unbounded(bounded);
        }
        return line.size() <= LogSanitizer::MAX_RECORD_SIZE
            ? line
            : std::string(SERIALIZATION_FALLBACK_LINE);
    } catch (...) {
        return std::string(SERIALIZATION_FALLBACK_LINE);
    }
}

} // namespace argus::logging
