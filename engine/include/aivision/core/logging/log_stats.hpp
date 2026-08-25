#pragma once

#include <atomic>
#include <cstddef>
#include <cstdint>

namespace aivision::logging {

/**
 * @brief Logger 统计快照结构体
 */
struct LoggerStatsSnapshot {
    uint64_t records_written{0};
    uint64_t dropped_normal{0};
    uint64_t dropped_high{0};
    uint64_t sink_write_failures{0};
    uint64_t message_truncations{0};
    uint64_t rejected_fields{0};
    uint64_t unknown_algo_levels{0};
    size_t current_normal_queue_depth{0};
    size_t current_high_queue_depth{0};
};

/**
 * @brief 线程安全的本地原子统计管理器
 */
class LoggerStats {
public:
    std::atomic<uint64_t> records_written{0};
    std::atomic<uint64_t> dropped_normal{0};
    std::atomic<uint64_t> dropped_high{0};
    std::atomic<uint64_t> sink_write_failures{0};
    std::atomic<uint64_t> message_truncations{0};
    std::atomic<uint64_t> rejected_fields{0};
    std::atomic<uint64_t> unknown_algo_levels{0};

    [[nodiscard]] LoggerStatsSnapshot snapshot(size_t normal_depth = 0, size_t high_depth = 0) const noexcept {
        LoggerStatsSnapshot s;
        s.records_written = records_written.load(std::memory_order_relaxed);
        s.dropped_normal = dropped_normal.load(std::memory_order_relaxed);
        s.dropped_high = dropped_high.load(std::memory_order_relaxed);
        s.sink_write_failures = sink_write_failures.load(std::memory_order_relaxed);
        s.message_truncations = message_truncations.load(std::memory_order_relaxed);
        s.rejected_fields = rejected_fields.load(std::memory_order_relaxed);
        s.unknown_algo_levels = unknown_algo_levels.load(std::memory_order_relaxed);
        s.current_normal_queue_depth = normal_depth;
        s.current_high_queue_depth = high_depth;
        return s;
    }

    void reset() noexcept {
        records_written.store(0, std::memory_order_relaxed);
        dropped_normal.store(0, std::memory_order_relaxed);
        dropped_high.store(0, std::memory_order_relaxed);
        sink_write_failures.store(0, std::memory_order_relaxed);
        message_truncations.store(0, std::memory_order_relaxed);
        rejected_fields.store(0, std::memory_order_relaxed);
        unknown_algo_levels.store(0, std::memory_order_relaxed);
    }
};

} // namespace aivision::logging
