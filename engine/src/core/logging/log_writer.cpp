#include "aivision/core/logging/log_writer.hpp"
#include "aivision/core/logging/log_sanitizer.hpp"
#include <chrono>
#include <iomanip>
#include <sstream>

namespace aivision::logging {

namespace {

// JSON 字符串安全转义
std::string escape_json_str(std::string_view value) {
    std::string escaped;
    escaped.reserve(value.size() + 8);
    for (const char c : value) {
        switch (c) {
            case '"':  escaped += "\\\""; break;
            case '\\': escaped += "\\\\"; break;
            case '\b': escaped += "\\b"; break;
            case '\f': escaped += "\\f"; break;
            case '\n': escaped += "\\n"; break;
            case '\r': escaped += "\\r"; break;
            case '\t': escaped += "\\t"; break;
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

// 将系统时钟格式化为 RFC 3339 纳秒字符串 (UTC)
std::string format_rfc3339_nano(std::chrono::system_clock::time_point tp) {
    auto duration = tp.time_since_epoch();
    auto seconds = std::chrono::duration_cast<std::chrono::seconds>(duration);
    auto nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(duration - seconds).count();

    std::time_t tt = std::chrono::system_clock::to_time_t(tp);
    std::tm gmt{};
    gmtime_r(&tt, &gmt);

    char buf[64];
    size_t len = std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%S", &gmt);
    std::snprintf(buf + len, sizeof(buf) - len, ".%09ldZ", nanos);
    return std::string(buf);
}

} // namespace

AsyncLogWriter::AsyncLogWriter(std::shared_ptr<LogSink> sink, std::shared_ptr<LoggerStats> stats)
    : sink_(std::move(sink)), stats_(std::move(stats)) {}

AsyncLogWriter::~AsyncLogWriter() {
    stop();
}

void AsyncLogWriter::start() {
    if (running_.exchange(true)) {
        return;
    }
    worker_thread_ = std::thread(&AsyncLogWriter::worker_loop, this);
}

void AsyncLogWriter::stop(uint32_t timeout_ms) {
    if (!running_.exchange(false)) {
        return;
    }
    cv_.notify_all();

    if (worker_thread_.joinable()) {
        worker_thread_.join();
    }
    if (sink_) {
        sink_->flush();
    }
}

bool AsyncLogWriter::enqueue(LogRecord&& record) noexcept {
    try {
        std::unique_lock<std::mutex> lock(mutex_);
        if (record.level >= Level::Warn) {
            // 高级别队列
            if (high_queue_.size() >= HIGH_QUEUE_CAPACITY) {
                if (stats_) stats_->dropped_high.fetch_add(1, std::memory_order_relaxed);
                return false;
            }
            high_queue_.emplace_back(std::move(record));
        } else {
            // 普通级别队列
            if (normal_queue_.size() >= NORMAL_QUEUE_CAPACITY) {
                if (stats_) stats_->dropped_normal.fetch_add(1, std::memory_order_relaxed);
                return false;
            }
            normal_queue_.emplace_back(std::move(record));
        }
        lock.unlock();
        cv_.notify_one();
        return true;
    } catch (...) {
        return false;
    }
}

void AsyncLogWriter::flush() noexcept {
    if (sink_) {
        sink_->flush();
    }
}

std::pair<size_t, size_t> AsyncLogWriter::queue_depths() const noexcept {
    std::lock_guard<std::mutex> lock(mutex_);
    return {normal_queue_.size(), high_queue_.size()};
}

void AsyncLogWriter::worker_loop() {
    std::vector<LogRecord> batch;
    batch.reserve(BATCH_SIZE);

    while (running_.load(std::memory_order_relaxed)) {
        {
            std::unique_lock<std::mutex> lock(mutex_);
            cv_.wait_for(lock, std::chrono::milliseconds(10), [this] {
                return !running_.load(std::memory_order_relaxed) ||
                       !high_queue_.empty() || !normal_queue_.empty();
            });

            // 调度策略: 优先消费 High Queue
            while (!high_queue_.empty() && batch.size() < BATCH_SIZE) {
                batch.emplace_back(std::move(high_queue_.front()));
                high_queue_.pop_front();
            }

            // 消费 Normal Queue 补充批量
            while (!normal_queue_.empty() && batch.size() < BATCH_SIZE) {
                batch.emplace_back(std::move(normal_queue_.front()));
                normal_queue_.pop_front();
            }
        }

        if (!batch.empty()) {
            process_batch(batch);
            batch.clear();
        }
    }

    // 停机排空剩余队列
    std::unique_lock<std::mutex> lock(mutex_);
    while (!high_queue_.empty() || !normal_queue_.empty()) {
        while (!high_queue_.empty() && batch.size() < BATCH_SIZE) {
            batch.emplace_back(std::move(high_queue_.front()));
            high_queue_.pop_front();
        }
        while (!normal_queue_.empty() && batch.size() < BATCH_SIZE) {
            batch.emplace_back(std::move(normal_queue_.front()));
            normal_queue_.pop_front();
        }
        lock.unlock();
        process_batch(batch);
        batch.clear();
        lock.lock();
    }
}

void AsyncLogWriter::process_batch(std::vector<LogRecord>& records) {
    if (!sink_) return;

    for (const auto& rec : records) {
        std::string json_line = serialize_jsonl(rec);
        if (!sink_->write_line(json_line)) {
            if (stats_) stats_->sink_write_failures.fetch_add(1, std::memory_order_relaxed);
        } else {
            if (stats_) stats_->records_written.fetch_add(1, std::memory_order_relaxed);
        }
    }
}

std::string AsyncLogWriter::serialize_jsonl(const LogRecord& record) noexcept {
    try {
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

        // 上下文字段序列化
        const auto& ctx = record.context;
        if (!ctx.platform_id.empty()) ss << ",\"platform_id\":\"" << escape_json_str(ctx.platform_id) << "\"";
        if (!ctx.device_id.empty()) ss << ",\"device_id\":\"" << escape_json_str(ctx.device_id) << "\"";
        if (!ctx.camera_id.empty()) ss << ",\"camera_id\":\"" << escape_json_str(ctx.camera_id) << "\"";
        if (!ctx.task_id.empty()) ss << ",\"task_id\":\"" << escape_json_str(ctx.task_id) << "\"";
        if (!ctx.instance_id.empty()) ss << ",\"instance_id\":\"" << escape_json_str(ctx.instance_id) << "\"";
        if (!ctx.instance_run_id.empty()) ss << ",\"instance_run_id\":\"" << escape_json_str(ctx.instance_run_id) << "\"";
        if (!ctx.algorithm_id.empty()) ss << ",\"algorithm_id\":\"" << escape_json_str(ctx.algorithm_id) << "\"";
        if (!ctx.package_version.empty()) ss << ",\"package_version\":\"" << escape_json_str(ctx.package_version) << "\"";
        if (ctx.frame_id >= 0) ss << ",\"frame_id\":" << ctx.frame_id;
        if (ctx.revision >= 0) ss << ",\"revision\":" << ctx.revision;
        if (ctx.retry_count >= 0) ss << ",\"retry_count\":" << ctx.retry_count;
        if (ctx.duration_ms >= 0.0) ss << ",\"duration_ms\":" << std::fixed << std::setprecision(3) << ctx.duration_ms;

        // 源码位置序列化 (DEBUG / ERROR / FATAL 携带)
        if (record.level == Level::Debug || record.level >= Level::Error) {
            if (record.loc.file) {
                ss << ",\"file\":\"" << escape_json_str(LogSanitizer::normalize_file_path(record.loc.file)) << "\"";
                ss << ",\"line\":" << record.loc.line;
            }
            if (record.loc.function) {
                ss << ",\"function\":\"" << escape_json_str(record.loc.function) << "\"";
            }
        }

        // 白名单额外字段序列化
        for (const auto& [k, v] : record.extra_fields) {
            ss << ",\"" << escape_json_str(k) << "\":\"" << escape_json_str(v) << "\"";
        }

        ss << "}\n";
        return ss.str();
    } catch (...) {
        return "{\"level\":\"fatal\",\"component\":\"engine.logger\",\"event\":\"logger.serialization_error\",\"message\":\"Failed to serialize log record\"}\n";
    }
}

} // namespace aivision::logging
