/**
 * @file test_frame_pool.cpp
 * @brief 帧对象池（FramePool）分配、引用计数、重置与生命周期单元测试
 */

#include <gtest/gtest.h>
#include "argus/core/frame_pool.hpp"


TEST(FramePoolTest, AcquireRetainRelease) {
    auto& pool = argus::core::FramePool::instance();
    ASSERT_EQ(pool.reset(), AV_OK);

    EXPECT_EQ(pool.active_frame_count(), 0);

    auto* frame = pool.acquire_frame();
    ASSERT_NE(frame, nullptr);
    EXPECT_EQ(pool.active_frame_count(), 1);

    void* token = frame->frame_token;
    EXPECT_EQ(pool.retain_frame(token), AV_OK);
    EXPECT_EQ(pool.active_frame_count(), 1);

    EXPECT_EQ(pool.release_frame(token), AV_OK);
    EXPECT_EQ(pool.active_frame_count(), 1);

    EXPECT_EQ(pool.release_frame(token), AV_OK);
    EXPECT_EQ(pool.active_frame_count(), 0);
}

TEST(FramePoolTest, ResetPreservesActiveToken) {
    auto& pool = argus::core::FramePool::instance();
    ASSERT_EQ(pool.reset(), AV_OK);

    auto* frame = pool.acquire_frame();
    ASSERT_NE(frame, nullptr);
    void* token = frame->frame_token;

    EXPECT_EQ(pool.reset(), AV_ERR_INVALID_ARG);
    EXPECT_EQ(pool.active_frame_count(), 1);
    EXPECT_EQ(pool.release_frame(token), AV_OK);
    EXPECT_EQ(pool.reset(), AV_OK);
}
