/**
 * @file log_writer.hpp
 * @brief 双有界队列异步日志 writer 及停机控制接口
 */
#pragma once

#include "argus/core/logging/log_record.hpp"
#include "argus/core/logging/log_sink.hpp"
#include "argus/core/logging/log_stats.hpp"
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace argus::logging {

/**
 * @brief 双有界队列与异步日志写入引擎
 *
 * 生产线程只执行 try-lock 入队；队列、sink 和停机状态由共享状态对象持有，
 * 这样超时停机时 writer 即使被分离也不会访问已析构的宿主对象。
 */
class AsyncLogWriter {
public:
    static constexpr size_t NORMAL_QUEUE_CAPACITY = 2048;
    static constexpr size_t HIGH_QUEUE_CAPACITY = 256;
    static constexpr size_t BATCH_SIZE = 64;

    explicit AsyncLogWriter(std::shared_ptr<LogSink> sink,
                             std::shared_ptr<LoggerStats> stats,
                             std::atomic<uint64_t>* sequence = nullptr);
    ~AsyncLogWriter();

    AsyncLogWriter(const AsyncLogWriter&) = delete;
    AsyncLogWriter& operator=(const AsyncLogWriter&) = delete;

    /**
     * @brief 启动后台 Writer 线程
     */
    void start();

    /**
     * @brief 停止写入引擎并排空队列 (最多等待 timeout_ms)
     */
    void stop(uint32_t timeout_ms = 2000) noexcept;

    /**
     * @brief 非阻塞入队记录
     * @param record 待写入记录 (将被移动)
     * @return true 入队成功, false 队列满或竞争失败
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
    friend class Logger;

    struct State;

    static void worker_loop(std::shared_ptr<State> state) noexcept;
    static void process_batch(const std::shared_ptr<State>& state,
                              std::vector<LogRecord>& records) noexcept;
    static void emit_diagnostic_summary(const std::shared_ptr<State>& state) noexcept;
    static std::string serialize_jsonl(const LogRecord& record,
                                        const std::shared_ptr<LoggerStats>& stats) noexcept;

    std::shared_ptr<State> state_;
    std::thread worker_thread_;
};

} // namespace argus::logging
