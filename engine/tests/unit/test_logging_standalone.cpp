/**
 * @file test_logging_standalone.cpp
 * @brief 无第三方测试依赖的结构化日志回归测试
 */
#include <cassert>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstring>
#include <iostream>
#include <thread>
#include <vector>

#include "argus/core/logging/logger.hpp"
#include "argus/core/logging/log_adapter.hpp"

using namespace argus::logging;

class GateSink final : public LogSink {
public:
    std::atomic<bool> entered{false};
    std::atomic<bool> open{false};
    std::vector<std::string> lines;

    bool write_line(std::string_view line) noexcept override {
        entered.store(true, std::memory_order_release);
        while (!open.load(std::memory_order_acquire)) {
            std::this_thread::yield();
        }
        lines.emplace_back(line);
        return true;
    }

    void flush() noexcept override {
        open.store(true, std::memory_order_release);
    }
};

void test_level_parsing() {
    assert(parse_level("warning") == Level::Warn);
    assert(parse_level("ERROR") == Level::Error);
    assert(!parse_level("invalid"));
    std::cout << "[PASS] test_level_parsing\n";
}

void test_basic_formatting() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Debug, sink);

    LOG_INFO("test_comp", "test.event", "Hello structured logging", "ERR_CODE_NONE",
             {{"camera_id", "cam_01"},
              {"width", int64_t{1920}},
              {"fps", 29.5},
              {"enabled", true}});

    Logger::shutdown();
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("\"level\":\"info\"") != std::string::npos);
    assert(lines[0].find("\"component\":\"test_comp\"") != std::string::npos);
    assert(lines[0].find("\"event\":\"test.event\"") != std::string::npos);
    assert(lines[0].find("\"message\":\"Hello structured logging\"") != std::string::npos);
    assert(lines[0].find("\"code\":\"ERR_CODE_NONE\"") != std::string::npos);
    assert(lines[0].find("\"camera_id\":\"cam_01\"") != std::string::npos);
    assert(lines[0].find("\"width\":1920") != std::string::npos);
    assert(lines[0].find("\"fps\":29.5") != std::string::npos);
    assert(lines[0].find("\"enabled\":true") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_basic_formatting\n";
}

void test_level_filtering() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Warn, sink);

    LOG_DEBUG("comp", "event.debug", "Should be filtered");
    LOG_INFO("comp", "event.info", "Should be filtered");
    LOG_WARN("comp", "event.warn", "Warning message");
    Logger::shutdown();

    Logger::initialize(Level::Warn, sink);
    LOG_ERROR("comp", "event.error", "Error message");
    Logger::shutdown();
    auto lines = sink->get_lines();
    assert(lines.size() == 2);
    assert(lines[0].find("\"level\":\"warn\"") != std::string::npos);
    assert(lines[1].find("\"level\":\"error\"") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_level_filtering\n";
}

void test_scoped_context() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);

    {
        LogContextSnapshot ctx;
        ctx.camera_id = "cam_scoped";
        ctx.task_id = "task_99";
        ScopedLogContext scope(ctx);

        LOG_INFO("scoped_comp", "scoped.event", "Scoped message");
    }
    Logger::shutdown();

    Logger::initialize(Level::Info, sink);
    LOG_INFO("global_comp", "global.event", "Global message");
    Logger::shutdown();
    auto lines = sink->get_lines();
    assert(lines.size() == 2);
    assert(lines[0].find("\"camera_id\":\"cam_scoped\"") != std::string::npos);
    assert(lines[0].find("\"task_id\":\"task_99\"") != std::string::npos);
    assert(lines[1].find("\"camera_id\"") == std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_scoped_context\n";
}

void test_sanitization() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);

    LOG_INFO("sec_comp", "sec.event",
             "Testing security rtsp://admin:pass123@192.168.1.100:554/live?token=abc password=secret",
             "", {{"password", "123456"},
                  {"url", "rtsp://admin:pass123@192.168.1.100:554/live?token=abc"}});

    std::string safe_url = LogSanitizer::sanitize_url("rtsp://admin:pass123@192.168.1.100:554/live?token=abc");
    assert(safe_url == "rtsp://192.168.1.100:554/live");

    Logger::shutdown();
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("password=secret") == std::string::npos);
    assert(lines[0].find("pass123") == std::string::npos);
    assert(lines[0].find("token=abc") == std::string::npos);
    assert(lines[0].find("\"url\":\"rtsp://192.168.1.100:554/live\"") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_sanitization\n";
}

void test_invalid_utf8() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);
    const std::string invalid("\xf0\x28\x8c\x28", 4);
    LOG_INFO("utf8", "utf8.invalid", invalid);
    Logger::shutdown();
    const auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("\"message\":\"?(?(\"") != std::string::npos);
    std::cout << "[PASS] test_invalid_utf8\n";
}

void test_record_size_limit() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);
    LogContextSnapshot context;
    context.platform_id = std::string(1024, 'p');
    context.device_id = std::string(1024, 'd');
    context.camera_id = std::string(1024, 'c');
    context.task_id = std::string(1024, 't');
    context.instance_id = std::string(1024, 'i');
    context.instance_run_id = std::string(1024, 'r');
    context.algorithm_id = std::string(1024, 'a');
    context.package_version = std::string(1024, 'v');
    {
        ScopedLogContext scope(context);
        LOG_INFO("size", "size.limit", std::string(8192, 'm'));
    }
    Logger::shutdown();
    const auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].size() <= LogSanitizer::MAX_RECORD_SIZE);
    assert(lines[0].find("\"message_truncated\":true") != std::string::npos);
    std::cout << "[PASS] test_record_size_limit\n";
}

void test_queue_overflow_is_non_blocking() {
    auto sink = std::make_shared<GateSink>();
    Logger::initialize(Level::Info, sink);
    const uint64_t dropped_before = Logger::stats().dropped_normal;
    LOG_INFO("overflow", "overflow.gate", "gate record");

    const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(2);
    while (!sink->entered.load(std::memory_order_acquire) &&
           std::chrono::steady_clock::now() < deadline) {
        std::this_thread::yield();
    }
    assert(sink->entered.load(std::memory_order_acquire));

    for (size_t index = 0; index < AsyncLogWriter::NORMAL_QUEUE_CAPACITY + 64; ++index) {
        LOG_INFO("overflow", "overflow.fill", "bounded record");
    }
    const auto stats_while_blocked = Logger::stats();
    assert(stats_while_blocked.current_normal_queue_depth <= AsyncLogWriter::NORMAL_QUEUE_CAPACITY);
    assert(stats_while_blocked.dropped_normal > dropped_before);

    sink->open.store(true, std::memory_order_release);
    Logger::shutdown();
    const auto summary = std::find_if(sink->lines.begin(), sink->lines.end(), [](const std::string& line) {
        return line.find("logger.normal_queue_dropped") != std::string::npos;
    });
    assert(summary != sink->lines.end());
    assert(summary->find("\"drop_count\":") != std::string::npos);
    std::cout << "[PASS] test_queue_overflow_is_non_blocking\n";
}

void test_sdk_bridge_and_mapping() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);

    assert(map_abi_status_to_code(AV_ERR_INFERENCE_FAILED) == "ALGO_PROCESS_FAILED");
    assert(map_abi_status_to_code(AV_ERR_MODEL_LOAD_FAILED) == "ALGO_MODEL_LOAD_FAILED");

    const char* algo_msg = "Model initialized in 12ms";
    AlgoLogContext context{"mock-detector", "1.0.0", "mock"};
    sdk_algo_log_bridge(&context, AV_ALGO_LOG_INFO, algo_msg, static_cast<uint32_t>(strlen(algo_msg)));

    Logger::shutdown();
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("\"component\":\"engine.algo_bridge\"") != std::string::npos);
    assert(lines[0].find("\"event\":\"algorithm.log\"") != std::string::npos);
    assert(lines[0].find("\"message\":\"Model initialized in 12ms\"") != std::string::npos);
    assert(lines[0].find("\"algorithm_id\":\"mock-detector\"") != std::string::npos);
    assert(lines[0].find("\"package_version\":\"1.0.0\"") != std::string::npos);
    assert(lines[0].find("\"platform_id\":\"mock\"") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_sdk_bridge_and_mapping\n";
}

void test_unknown_algo_level() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);
    AlgoLogContext context{"mock-detector", "1.0.0", "mock"};
    const char* message = "unknown algorithm level";
    sdk_algo_log_bridge(&context, 99, message, static_cast<uint32_t>(strlen(message)));
    Logger::shutdown();
    const auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("algorithm.log_level_unknown") != std::string::npos);
    assert(lines[0].find("ALGO_LOG_LEVEL_UNKNOWN") != std::string::npos);
    assert(Logger::stats().unknown_algo_levels > 0);
    std::cout << "[PASS] test_unknown_algo_level\n";
}

void test_concurrency() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Debug, sink);

    constexpr int NUM_THREADS = 8;
    constexpr int LOGS_PER_THREAD = 200;

    std::vector<std::thread> threads;
    threads.reserve(NUM_THREADS);

    for (int t = 0; t < NUM_THREADS; ++t) {
        threads.emplace_back([] {
            for (int i = 0; i < LOGS_PER_THREAD; ++i) {
                if (i % 2 == 0) {
                    LOG_INFO("stress", "stress.info", "Concurrent info log");
                } else {
                    LOG_ERROR("stress", "stress.error", "Concurrent error log");
                }
            }
        });
    }

    for (auto& th : threads) {
        th.join();
    }

    Logger::shutdown();
    auto stats = Logger::stats();
    assert(stats.records_written + stats.dropped_normal + stats.dropped_high > 0);

    Logger::shutdown();
    std::cout << "[PASS] test_concurrency\n";
}

int main() {
    test_level_parsing();
    test_basic_formatting();
    test_level_filtering();
    test_scoped_context();
    test_sanitization();
    test_invalid_utf8();
    test_record_size_limit();
    test_queue_overflow_is_non_blocking();
    test_sdk_bridge_and_mapping();
    test_unknown_algo_level();
    test_concurrency();
    std::cout << "\nALL 10 STANDALONE LOGGING TESTS PASSED CLEANLY!\n";
    return 0;
}
