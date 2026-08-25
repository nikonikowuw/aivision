#pragma once

#include "aivision/core/logging/log_record.hpp"
#include "aivision/core/logging/log_sink.hpp"
#include "aivision/core/logging/log_stats.hpp"
#include <atomic>
#include <condition_variable>
#include <deque>
#include <memory>
#include <mutex>
#include <thread>
#include <vector>

namespace aivision::logging {

/**
 * @brief 双有界队列与异步日志写入引擎
 */
class AsyncLogWriter {
public:
    static constexpr size_t NORMAL_QUEUE_CAPACITY = 2048;
    static constexpr size_t HIGH_QUEUE_CAPACITY = 256;
    static constexpr size_t BATCH_SIZE = 64;

    explicit AsyncLogWriter(std::shared_ptr<LogSink> sink, std::shared_ptr<LoggerStats> stats);
    ~AsyncLogWriter();

    // 禁用拷贝与移动
    AsyncLogWriter(const AsyncLogWriter&) = delete;
    AsyncLogWriter& operator=(const AsyncLogWriter&) = delete;

    /**
     * @brief 启动后台 Writer 线程
     */
    void start();

    /**
     * @brief 停止写入引擎并排空队列 (最多等待 timeout_ms)
     */
    void stop(uint32_t timeout_ms = 2000);

    /**
     * @brief 非阻塞入队记录
     * @param record 待写入记录 (将被移动)
     * @return true 入队成功, false 队列满丢弃
     */
    bool enqueue(LogRecord&& record) noexcept;

    /**
     * @brief 同步刷新底层 sink
     */
    void flush() noexcept;

    /**
     * @brief 获取当前队列深度快照
     */
    [[nodiscard]] std::pair<size_t, size_t> queue_depths() const noexcept;

private:
    void worker_loop();
    void process_batch(std::vector<LogRecord>& records);
    std::string serialize_jsonl(const LogRecord& record) noexcept;

    std::shared_ptr<LogSink> sink_;
    std::shared_ptr<LoggerStats> stats_;

    mutable std::mutex mutex_;
    std::condition_variable cv_;
    std::deque<LogRecord> normal_queue_;
    std::deque<LogRecord> high_queue_;

    std::atomic<bool> running_{false};
    std::thread worker_thread_;
};

} // namespace aivision::logging
