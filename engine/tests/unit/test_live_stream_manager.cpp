/**
 * @file test_live_stream_manager.cpp
 * @brief 实时预览流媒体管理器单元测试
 */

#include <gtest/gtest.h>
#include "aivision/core/live_stream_manager.hpp"

namespace {

class LiveStreamManagerTest : public ::testing::Test {
protected:
    void SetUp() override {
        aivision::core::LiveStreamManager::instance().reset();
    }

    void TearDown() override {
        aivision::core::LiveStreamManager::instance().reset();
    }
};

TEST_F(LiveStreamManagerTest, InvalidArgumentFails) {
    auto& mgr = aivision::core::LiveStreamManager::instance();
    std::string path;
    std::string err;

    // 空 camera_id
    std::string code = mgr.start_preview("", aivision::v1::STREAM_TYPE_MAIN, "rtsp://127.0.0.1/live/test", &path, &err);
    EXPECT_EQ(code, "RTSP_INVALID_ARGUMENT");

    // 空 URL
    code = mgr.start_preview("cam_01", aivision::v1::STREAM_TYPE_MAIN, "", &path, &err);
    EXPECT_EQ(code, "RTSP_INVALID_ARGUMENT");
}

#if defined(AIVISION_SKIP_REAL_MEDIA_TESTS) || defined(ENGINE_ENABLE_TSAN)
// ZLToolKit EventPoller thread detach in PlayerProxy teardown triggers TSAN thread registry abort on macOS.
TEST_F(LiveStreamManagerTest, DISABLED_StartAndStopMainAndSubStream) {
#else
TEST_F(LiveStreamManagerTest, StartAndStopMainAndSubStream) {
#endif
    auto& mgr = aivision::core::LiveStreamManager::instance();
    std::string path_main;
    std::string path_sub;
    std::string err;

    // 启动主码流
    std::string code = mgr.start_preview("cam_01", aivision::v1::STREAM_TYPE_MAIN,
                                        "rtsp://127.0.0.1:18554/live/test_main",
                                        &path_main, &err);
    EXPECT_EQ(code, "");
    EXPECT_EQ(path_main, "/live/cam_01_main.live.flv");
    EXPECT_TRUE(mgr.is_streaming("cam_01", aivision::v1::STREAM_TYPE_MAIN));

    // 启动子码流
    code = mgr.start_preview("cam_01", aivision::v1::STREAM_TYPE_SUB,
                             "rtsp://127.0.0.1:18554/live/test_sub",
                             &path_sub, &err);
    EXPECT_EQ(code, "");
    EXPECT_EQ(path_sub, "/live/cam_01_sub.live.flv");
    EXPECT_TRUE(mgr.is_streaming("cam_01", aivision::v1::STREAM_TYPE_SUB));

    // 重复请求相同流直接复用
    std::string path_reused;
    code = mgr.start_preview("cam_01", aivision::v1::STREAM_TYPE_MAIN,
                             "rtsp://127.0.0.1:18554/live/test_main",
                             &path_reused, &err);
    EXPECT_EQ(code, "");
    EXPECT_EQ(path_reused, "/live/cam_01_main.live.flv");

    // 停止主码流，子码流仍不受影响
    EXPECT_TRUE(mgr.stop_preview("cam_01", aivision::v1::STREAM_TYPE_MAIN));
    EXPECT_FALSE(mgr.is_streaming("cam_01", aivision::v1::STREAM_TYPE_MAIN));
    EXPECT_TRUE(mgr.is_streaming("cam_01", aivision::v1::STREAM_TYPE_SUB));

    // 停止子码流
    EXPECT_TRUE(mgr.stop_preview("cam_01", aivision::v1::STREAM_TYPE_SUB));
    EXPECT_FALSE(mgr.is_streaming("cam_01", aivision::v1::STREAM_TYPE_SUB));
}

} // namespace
