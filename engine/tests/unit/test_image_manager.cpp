/**
 * @file test_image_manager.cpp
 * @brief 图片管理器原子保存、Catalog 同步、目录安全检查与对账删除单元测试
 */

#include <gtest/gtest.h>
#include <filesystem>
#include "aivision/core/image_manager.hpp"
#include "aivision/platform/mock_platform.hpp"


TEST(ImageManagerTest, AtomicSaveCatalogAndBatchDelete) {
    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto& mgr = aivision::core::ImageManager::instance();
    mgr.init("build/test_images", std::shared_ptr<aivision::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = 1920;
    frame.height = 1080;
    frame.pixel_format = AV_PIX_NV12;

    aivision::core::ImageRecord record;
    EXPECT_EQ(mgr.save_detection_image(&frame, nullptr, "test-event-123", &record), AV_OK);
    EXPECT_EQ(record.event_id, "test-event-123");
    EXPECT_TRUE(std::filesystem::exists("build/test_images/" + record.rel_path));
    EXPECT_TRUE(std::filesystem::exists("build/test_images/catalog/catalog.json"));
    EXPECT_TRUE(record.rel_path.find(".tmp") == std::string::npos);

    EXPECT_EQ(mgr.delete_image("../outside-root"), AV_ERR_INVALID_ARG);

    auto del_res = mgr.batch_delete_images({record.image_id, "img-non-existent"});
    EXPECT_EQ(del_res.size(), 2);
    EXPECT_TRUE(del_res[0].second);
    EXPECT_TRUE(del_res[1].second);
    EXPECT_FALSE(std::filesystem::exists("build/test_images/" + record.rel_path));
}
