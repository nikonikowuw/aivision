#include <cassert>
#include <chrono>
#include <cstring>
#include <iostream>
#include <thread>
#include <vector>

#include "aivision/core/logging/logger.hpp"
#include "aivision/core/logging/log_adapter.hpp"

using namespace aivision::logging;

void test_basic_formatting() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Debug, sink);

    LOG_INFO("test_comp", "test.event", "Hello structured logging", "ERR_CODE_NONE",
             {{"camera_id", "cam_01"}});

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("\"level\":\"info\"") != std::string::npos);
    assert(lines[0].find("\"component\":\"test_comp\"") != std::string::npos);
    assert(lines[0].find("\"event\":\"test.event\"") != std::string::npos);
    assert(lines[0].find("\"message\":\"Hello structured logging\"") != std::string::npos);
    assert(lines[0].find("\"code\":\"ERR_CODE_NONE\"") != std::string::npos);
    assert(lines[0].find("\"camera_id\":\"cam_01\"") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_basic_formatting\n";
}

void test_level_filtering() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Warn, sink);

    LOG_DEBUG("comp", "event.debug", "Should be filtered");
    LOG_INFO("comp", "event.info", "Should be filtered");
    LOG_WARN("comp", "event.warn", "Warning message");
    LOG_ERROR("comp", "event.error", "Error message");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
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

    LOG_INFO("global_comp", "global.event", "Global message");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
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

    LOG_INFO("sec_comp", "sec.event", "Testing security", "", {{"password", "123456"}});

    std::string safe_url = LogSanitizer::sanitize_url("rtsp://admin:pass123@192.168.1.100:554/live?token=abc");
    assert(safe_url == "rtsp://192.168.1.100:554/live");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("password") == std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_sanitization\n";
}

void test_sdk_bridge_and_mapping() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Info, sink);

    assert(map_abi_status_to_code(AV_ERR_INFERENCE_FAILED) == "ALGO_PROCESS_FAILED");
    assert(map_abi_status_to_code(AV_ERR_MODEL_LOAD_FAILED) == "ALGO_MODEL_LOAD_FAILED");

    const char* algo_msg = "Model initialized in 12ms";
    sdk_algo_log_bridge(nullptr, AV_ALGO_LOG_INFO, algo_msg, static_cast<uint32_t>(strlen(algo_msg)));

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink->get_lines();
    assert(lines.size() == 1);
    assert(lines[0].find("\"component\":\"engine.algo_bridge\"") != std::string::npos);
    assert(lines[0].find("\"event\":\"algorithm.log\"") != std::string::npos);
    assert(lines[0].find("\"message\":\"Model initialized in 12ms\"") != std::string::npos);

    Logger::shutdown();
    std::cout << "[PASS] test_sdk_bridge_and_mapping\n";
}

void test_concurrency() {
    auto sink = std::make_shared<MemorySink>();
    Logger::initialize(Level::Debug, sink);

    constexpr int NUM_THREADS = 8;
    constexpr int LOGS_PER_THREAD = 200;

    std::vector<std::thread> threads;
    threads.reserve(NUM_THREADS);

    for (int t = 0; t < NUM_THREADS; ++t) {
        threads.emplace_back([t] {
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

    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    auto stats = Logger::stats();
    assert(stats.records_written + stats.dropped_normal + stats.dropped_high > 0);

    Logger::shutdown();
    std::cout << "[PASS] test_concurrency\n";
}

int main() {
    test_basic_formatting();
    test_level_filtering();
    test_scoped_context();
    test_sanitization();
    test_sdk_bridge_and_mapping();
    test_concurrency();
    std::cout << "\nALL 6 STANDALONE LOGGING TESTS PASSED CLEANLY!\n";
    return 0;
}
