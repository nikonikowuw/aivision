#include <gtest/gtest.h>
#include <thread>
#include <vector>
#include <nlohmann/json.hpp>

#include "aivision/core/logging/logger.hpp"
#include "aivision/core/logging/log_adapter.hpp"

using namespace aivision::logging;

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
             {{"camera_id", "cam_01"}});

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);

    auto j = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j["level"], "info");
    EXPECT_EQ(j["component"], "test_comp");
    EXPECT_EQ(j["event"], "test.event");
    EXPECT_EQ(j["message"], "Hello structured logging");
    EXPECT_EQ(j["code"], "ERR_CODE_NONE");
    EXPECT_EQ(j["camera_id"], "cam_01");
    EXPECT_TRUE(j.contains("ts"));
    EXPECT_TRUE(j.contains("seq"));
}

TEST_F(LoggingTest, LevelFiltering) {
    Logger::set_level(Level::Warn);

    LOG_DEBUG("comp", "event.debug", "Should be filtered");
    LOG_INFO("comp", "event.info", "Should be filtered");
    LOG_WARN("comp", "event.warn", "Warning message");
    LOG_ERROR("comp", "event.error", "Error message");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 2);

    auto j1 = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j1["level"], "warn");
    auto j2 = nlohmann::json::parse(lines[1]);
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

    LOG_INFO("global_comp", "global.event", "Global message");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
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
    LOG_INFO("sec_comp", "sec.event", "Testing security", "", {{"password", "123456"}});

    // URL 脱敏
    std::string safe_url = LogSanitizer::sanitize_url("rtsp://admin:pass123@192.168.1.100:554/live?token=abc");
    EXPECT_EQ(safe_url, "rtsp://192.168.1.100:554/live");

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);

    auto j = nlohmann::json::parse(lines[0]);
    EXPECT_FALSE(j.contains("password"));
}

TEST_F(LoggingTest, SdkAlgoLogBridgeAndStatusMapping) {
    EXPECT_EQ(map_abi_status_to_code(AV_ERR_INFERENCE_FAILED), "ALGO_PROCESS_FAILED");
    EXPECT_EQ(map_abi_status_to_code(AV_ERR_MODEL_LOAD_FAILED), "ALGO_MODEL_LOAD_FAILED");

    const char* algo_msg = "Model initialized in 12ms";
    sdk_algo_log_bridge(nullptr, AV_ALGO_LOG_INFO, algo_msg, static_cast<uint32_t>(strlen(algo_msg)));

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    auto lines = sink_->get_lines();
    ASSERT_EQ(lines.size(), 1);

    auto j = nlohmann::json::parse(lines[0]);
    EXPECT_EQ(j["component"], "engine.algo_bridge");
    EXPECT_EQ(j["event"], "algorithm.log");
    EXPECT_EQ(j["message"], "Model initialized in 12ms");
}

TEST_F(LoggingTest, HighConcurrencyAndQueueNonBlocking) {
    constexpr int NUM_THREADS = 8;
    constexpr int LOGS_PER_THREAD = 500;

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

    // 等待后台 writer 处理完毕
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    auto stats = Logger::stats();
    EXPECT_GT(stats.records_written + stats.dropped_normal + stats.dropped_high, 0);
}
