/**
 * @file test_logging.cpp
 * @brief 结构化日志 gtest 契约、并发与安全行为测试
 */
#include <gtest/gtest.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstring>
#include <thread>
#include <vector>
#include <nlohmann/json.hpp>

#include "argus/core/logging/logger.hpp"
#include "argus/core/logging/log_adapter.hpp"

using namespace argus::logging;

namespace {

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

} // namespace

TEST(LoggingParsingTest, AcceptsAliasesAndRejectsUnknown) {
    const auto warning = parse_level("warning");
    ASSERT_TRUE(warning.has_value());
    EXPECT_EQ(*warning, Level::Warn);

    const auto error = parse_level("ERROR");
    ASSERT_TRUE(error.has_value());
    EXPECT_EQ(*error, Level::Error);

    EXPECT_FALSE(parse_level("invalid").has_value());
}

class LoggingTest : public ::testing::Test {
protected:
    void SetUp() override {
        sink_ = std::make_shared<MemorySink>();
        Logger::initialize(Level::Debug, sink_);
    }

    void TearDown() override {
        Logger::shutdown();
    }

    std::shared_ptr<MemorySink> sink_;
};

TEST_F(LoggingTest, BasicFormattingAndJsonFields) {
    LOG_INFO("test_comp", "test.event", "Hello structured logging", "ERR_CODE_NONE",
             {{"camera_id", "cam_01"},
              {"width", int64_t{1920}},
              {"fps", 29.5},
              {"enabled", true}});

    Logger::shutdown();
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);

    auto j = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j["level"], "info");
    EXPECT_EQ(j["component"], "test_comp");
    EXPECT_EQ(j["event"], "test.event");
    EXPECT_EQ(j["message"], "Hello structured logging");
    EXPECT_EQ(j["code"], "ERR_CODE_NONE");
    EXPECT_EQ(j["camera_id"], "cam_01");
    EXPECT_EQ(j["width"], 1920);
    EXPECT_DOUBLE_EQ(j["fps"].get<double>(), 29.5);
    EXPECT_EQ(j["enabled"], true);
    EXPECT_TRUE(j.contains("ts"));
    EXPECT_TRUE(j.contains("seq"));
}

TEST_F(LoggingTest, LevelFiltering) {
    Logger::shutdown();
    sink_ = std::make_shared<MemorySink>();
    Logger::initialize(Level::Warn, sink_);

    LOG_DEBUG("comp", "event.debug", "Should be filtered");
    LOG_INFO("comp", "event.info", "Should be filtered");
    LOG_WARN("comp", "event.warn", "Warning message");
    LOG_ERROR("comp", "event.error", "Error message");
    Logger::shutdown();
    auto lines = sink_->get_lines();
    std::vector<std::string> comp_lines;
    for (const auto& l : lines) {
        auto j = nlohmann::json::parse(l, nullptr, false);
        if (j.is_object() && j.value("component", "") == "comp") {
            comp_lines.push_back(l);
        }
    }
    ASSERT_EQ(comp_lines.size(), 2);

    auto j1 = nlohmann::json::parse(comp_lines[0]);
    EXPECT_EQ(j1["level"], "warn");
    auto j2 = nlohmann::json::parse(comp_lines[1]);
    EXPECT_EQ(j2["level"], "error");
}

TEST_F(LoggingTest, ScopedContextIsolation) {
    {
        LogContextSnapshot ctx;
        ctx.camera_id = "cam_scoped";
        ctx.task_id = "task_99";
        ScopedLogContext scope(ctx);

        LOG_INFO("scoped_comp", "scoped.event", "Scoped message");
    }
    Logger::shutdown();

    Logger::initialize(Level::Info, sink_);
    LOG_INFO("global_comp", "global.event", "Global message");
    Logger::shutdown();
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 2);

    auto j1 = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j1["camera_id"], "cam_scoped");
    EXPECT_EQ(j1["task_id"], "task_99");

    auto j2 = nlohmann::json::parse(lines[1]);
    EXPECT_FALSE(j2.contains("camera_id"));
    EXPECT_FALSE(j2.contains("task_id"));
}

TEST_F(LoggingTest, SanitizationAndTruncation) {
    // 敏感字段过滤
    LOG_INFO("sec_comp", "sec.event",
             "Testing security rtsp://admin:pass123@192.168.1.100:554/live?token=abc password=secret",
             "", {{"password", "123456"},
                  {"url", "rtsp://admin:pass123@192.168.1.100:554/live?token=abc"}});

    // URL 脱敏
    std::string safe_url = LogSanitizer::sanitize_url("rtsp://admin:pass123@192.168.1.100:554/live?token=abc");
    EXPECT_EQ(safe_url, "rtsp://192.168.1.100:554/live");

    Logger::shutdown();
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);
    EXPECT_FALSE(lines[0].find("password=secret") != std::string::npos);
    EXPECT_FALSE(lines[0].find("pass123") != std::string::npos);
    EXPECT_FALSE(lines[0].find("token=abc") != std::string::npos);
    EXPECT_NE(lines[0].find("\"url\":\"rtsp://192.168.1.100:554/live\""), std::string::npos);
}

TEST_F(LoggingTest, InvalidUtf8IsSanitized) {
    const std::string invalid("\xf0\x28\x8c\x28", 4);
    LOG_INFO("utf8", "utf8.invalid", invalid);

    Logger::shutdown();
    const auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);
    const auto json = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(json["message"], "?(?(");
}

TEST_F(LoggingTest, RecordSizeLimit) {
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
    const auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);
    EXPECT_LE(lines[0].size(), LogSanitizer::MAX_RECORD_SIZE);
    const auto json = nlohmann::json::parse(lines[0]);
    EXPECT_TRUE(json["message_truncated"]);
}

TEST_F(LoggingTest, SdkAlgoLogBridgeAndStatusMapping) {
    EXPECT_EQ(map_abi_status_to_code(AV_ERR_INFERENCE_FAILED), "ALGO_PROCESS_FAILED");
    EXPECT_EQ(map_abi_status_to_code(AV_ERR_MODEL_LOAD_FAILED), "ALGO_MODEL_LOAD_FAILED");

    const char* algo_msg = "Model initialized in 12ms";
    AlgoLogContext context{"mock-detector", "1.0.0", "mock"};
    sdk_algo_log_bridge(&context, AV_ALGO_LOG_INFO, algo_msg, static_cast<uint32_t>(strlen(algo_msg)));

    Logger::shutdown();
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);

    auto j = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j["component"], "engine.algo_bridge");
    EXPECT_EQ(j["event"], "algorithm.log");
    EXPECT_EQ(j["message"], "Model initialized in 12ms");
    EXPECT_EQ(j["algorithm_id"], "mock-detector");
    EXPECT_EQ(j["package_version"], "1.0.0");
    EXPECT_EQ(j["platform_id"], "mock");
}

TEST_F(LoggingTest, UnknownAlgoLogLevel) {
    AlgoLogContext context{"mock-detector", "1.0.0", "mock"};
    const char* message = "unknown algorithm level";
    sdk_algo_log_bridge(&context, 99, message, static_cast<uint32_t>(strlen(message)));

    Logger::shutdown();
    const auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);
    EXPECT_NE(lines[0].find("algorithm.log_level_unknown"), std::string::npos);
    EXPECT_NE(lines[0].find("ALGO_LOG_LEVEL_UNKNOWN"), std::string::npos);
    EXPECT_GT(Logger::stats().unknown_algo_levels, 0);
}

TEST_F(LoggingTest, QueueOverflowIsNonBlocking) {
    Logger::shutdown();
    auto sink = std::make_shared<GateSink>();
    Logger::initialize(Level::Info, sink);
    const uint64_t dropped_before = Logger::stats().dropped_normal;
    LOG_INFO("overflow", "overflow.gate", "gate record");

    const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(2);
    while (!sink->entered.load(std::memory_order_acquire) &&
           std::chrono::steady_clock::now() < deadline) {
        std::this_thread::yield();
    }
    ASSERT_TRUE(sink->entered.load(std::memory_order_acquire));

    for (size_t index = 0; index < AsyncLogWriter::NORMAL_QUEUE_CAPACITY + 64; ++index) {
        LOG_INFO("overflow", "overflow.fill", "bounded record");
    }
    const auto stats_while_blocked = Logger::stats();
    EXPECT_LE(stats_while_blocked.current_normal_queue_depth, AsyncLogWriter::NORMAL_QUEUE_CAPACITY);
    EXPECT_GT(stats_while_blocked.dropped_normal, dropped_before);

    sink->open.store(true, std::memory_order_release);
    Logger::shutdown();

    const auto summary = std::find_if(sink->lines.begin(), sink->lines.end(), [](const std::string& line) {
        return line.find("logger.normal_queue_dropped") != std::string::npos;
    });
    ASSERT_NE(summary, sink->lines.end());
    EXPECT_NE(summary->find("\"drop_count\":"), std::string::npos);
}

TEST_F(LoggingTest, HighConcurrencyAndQueueNonBlocking) {
    constexpr int NUM_THREADS = 8;
    constexpr int LOGS_PER_THREAD = 500;

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
    EXPECT_GT(stats.records_written + stats.dropped_normal + stats.dropped_high, 0);
}
