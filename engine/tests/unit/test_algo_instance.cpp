/**
 * @file test_algo_instance.cpp
 * @brief 算法实例 FPS 滑动窗口统计与上报结算单元测试
 *
 * 通过 get_current_fps(now) 注入单调时钟，验证 1s 窗口的初始化、结算与滚动，
 * 不依赖真实 sleep 推进统计窗口（仅轮询等待 worker 处理帧，与 test_camera_task 一致）。
 */

#include <gtest/gtest.h>
#include <chrono>
#include <thread>

#include "argus/core/algo_instance.hpp"
#include "argus/core/frame_pool.hpp"
#include "argus/platform/mock_platform.hpp"

using namespace std::chrono_literals;

namespace {

// 等待 worker 线程处理完指定帧数（与 test_camera_task 相同的确定性等待模式）
void WaitForProcessedFrames(argus::core::AlgorithmInstance& inst, uint64_t want) {
    for (int i = 0; i < 400 && inst.get_processed_frames() < want; ++i) {
        std::this_thread::sleep_for(5ms);
    }
}

} // namespace

// 实例未收到任何帧时：窗口结算后 FPS 必须归零而非残留旧值。
TEST(AlgoInstanceFpsTest, NoFramesSettlesToZero) {
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto inst = std::make_shared<argus::core::AlgorithmInstance>(
        "inst-zero", "cam-1", "algo-1", "1.0.0", 0, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(argus::core::FramePool::instance().get_frame_ops(),
                         adapter->get_c_image_ops()),
              AV_OK);

    const auto t0 = std::chrono::steady_clock::now();
    // 首次调用初始化窗口起点
    EXPECT_EQ(inst->get_current_fps(t0), 0.0);
    // 满窗口无帧 -> 结算为 0
    EXPECT_EQ(inst->get_current_fps(t0 + 1100ms), 0.0);
    // 下一窗口继续无帧 -> 仍为 0
    EXPECT_EQ(inst->get_current_fps(t0 + 2200ms), 0.0);

    inst->stop();
}

// 有帧进入时：窗口未到期返回上一结算值，到期后按 帧数/窗口时长 结算并滚动。
TEST(AlgoInstanceFpsTest, WindowSettlesWithProcessedFrames) {
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto inst = std::make_shared<argus::core::AlgorithmInstance>(
        "inst-fps", "cam-1", "algo-1", "1.0.0", 0, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(argus::core::FramePool::instance().get_frame_ops(),
                         adapter->get_c_image_ops()),
              AV_OK);

    constexpr uint64_t kFrames = 5;
    const auto t0 = std::chrono::steady_clock::now();
    // 窗口初始化（在推送帧之前，确保所有帧落在首个窗口内）
    EXPECT_EQ(inst->get_current_fps(t0), 0.0);

    // 推送 5 帧（target_fps=0 关闭抽帧节流，全部入队），等待 worker 处理完成
    // push_frame 内部会 retain 一份队列引用；测试持有的 acquire 引用在入队后立即释放，
    // 避免污染 FramePool 单例（影响同进程后续测试）。
    auto& pool = argus::core::FramePool::instance();
    for (uint64_t i = 0; i < kFrames; ++i) {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        inst->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
    }
    WaitForProcessedFrames(*inst, kFrames);
    EXPECT_GE(inst->get_processed_frames(), kFrames);

    // 窗口未到期：不结算，保持上一值（0）
    EXPECT_EQ(inst->get_current_fps(t0 + 500ms), 0.0);
    // 窗口到期：fps = kFrames / 1.1s
    const double fps = inst->get_current_fps(t0 + 1100ms);
    EXPECT_NEAR(fps, static_cast<double>(kFrames) / 1.1, 0.01);

    // 下一窗口无新帧：结算归零
    EXPECT_EQ(inst->get_current_fps(t0 + 2200ms), 0.0);

    inst->stop();
    EXPECT_EQ(argus::core::FramePool::instance().active_frame_count(), 0);
}

// VideoToolbox may deliver decoded frames in bursts. Sampling must follow media PTS rather
// than callback arrival time, otherwise a 24 FPS stream is incorrectly reduced to ~12 FPS.
TEST(AlgoInstanceFpsTest, BurstDeliveryUsesMediaPtsForSampling) {
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto inst = std::make_shared<argus::core::AlgorithmInstance>(
        "inst-pts", "cam-1", "algo-1", "1.0.0", 25, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(argus::core::FramePool::instance().get_frame_ops(),
                         adapter->get_c_image_ops()),
              AV_OK);

    constexpr uint64_t kFrames = 8;
    constexpr int64_t kFrameIntervalNs = 41'708'333; // 24000/1001 FPS
    auto& pool = argus::core::FramePool::instance();
    for (uint64_t i = 0; i < kFrames; ++i) {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->pts_ns = 1'000'000'000 + static_cast<int64_t>(i) * kFrameIntervalNs;
        inst->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
        WaitForProcessedFrames(*inst, i + 1);
    }

    EXPECT_EQ(inst->get_processed_frames(), kFrames);
    EXPECT_EQ(inst->get_dropped_frames(), 0);
    inst->stop();
    EXPECT_EQ(pool.active_frame_count(), 0);
}
