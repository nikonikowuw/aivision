/**
 * @file test_main.cpp
 * @brief GoogleTest 单元测试主入口
 */

#include <gtest/gtest.h>

#include "aivision/core/logging/logger.hpp"

int main(int argc, char **argv) {
    ::testing::InitGoogleTest(&argc, argv);
    const int result = RUN_ALL_TESTS();
    // 测试进程显式停止异步日志线程，避免静态析构阶段仍等待 writer 导致门禁挂起。
    aivision::logging::Logger::shutdown();
    return result;
}
