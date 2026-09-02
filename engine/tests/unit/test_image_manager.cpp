/**
 * @file test_image_manager.cpp
 * @brief 图片管理器原子保存、Catalog 同步、目录安全检查与对账删除单元测试
 */

#include <gtest/gtest.h>
#include <filesystem>
#include <fstream>
#include <sstream>
#include "argus/core/image_manager.hpp"
#include "argus/platform/mock_platform.hpp"

namespace {

class ThumbnailFailureProcessor final : public argus::platform::MockImageProcessor {
public:
    av_status encode_thumbnail_jpeg(const av_frame_desc*, int, int,
                                    std::vector<uint8_t>& out_jpeg) override {
        out_jpeg.clear();
        return AV_ERR_INTERNAL;
    }
};

} // namespace


TEST(ImageManagerTest, AppendsSuffixWhenImageArtifactsAreOccupied) {
    namespace fs = std::filesystem;
    const fs::path root = "build/test_images_collision";
    fs::remove_all(root);

    constexpr int64_t fixed_now_ns = 1788185888000000000LL;
    const std::string date = "2026-08-31";
    std::ostringstream id_stream;
    id_stream << "img-collision_event-" << std::hex << static_cast<uint64_t>(fixed_now_ns);
    const std::string base_id = id_stream.str();

    fs::create_directories(root / "catalog");
    {
        std::ofstream catalog(root / "catalog" / "catalog.json");
        ASSERT_TRUE(catalog.is_open());
        catalog << "{\"" << base_id
                << "\":{\"event_id\":\"seed\",\"relative_path\":\""
                << date << "/" << base_id
                << ".jpg\",\"created_at_ns\":" << fixed_now_ns
                << ",\"report_status\":\"reported\"}}";
    }

    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    argus::core::ImageManager mgr([] { return fixed_now_ns; });
    mgr.init(root.string(), std::shared_ptr<argus::platform::IImageProcessor>(
        adapter, adapter->get_image_processor()));

    fs::create_directories(root / date);
    std::ofstream(root / date / (base_id + "-1.jpg")) << "occupied-final";
    std::ofstream(root / date / (base_id + "-2_thumb.jpg")) << "occupied-thumb";
    std::ofstream(root / ".tmp" / (base_id + "-3.jpg.part")) << "occupied-temp";
    std::ofstream(root / ".tmp" / (base_id + "-4_thumb.jpg.part")) << "occupied-thumb-temp";

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = 1920;
    frame.height = 1080;
    frame.pixel_format = AV_PIX_NV12;

    argus::core::ImageRecord record;
    ASSERT_EQ(mgr.save_detection_image(&frame, nullptr, "collision/event", &record), AV_OK);
    EXPECT_EQ(record.image_id, base_id + "-5");
    EXPECT_EQ(record.event_id, "collision/event");
    EXPECT_TRUE(fs::exists(root / record.rel_path));

    const auto dot_pos = record.rel_path.rfind('.');
    ASSERT_NE(dot_pos, std::string::npos);
    const std::string thumb_rel = record.rel_path.substr(0, dot_pos) + "_thumb" + record.rel_path.substr(dot_pos);
    EXPECT_TRUE(fs::exists(root / thumb_rel));
    EXPECT_TRUE(fs::exists(root / "catalog" / "catalog.json"));
    EXPECT_TRUE(fs::exists(root / date / (base_id + "-1.jpg")));
    EXPECT_TRUE(fs::exists(root / date / (base_id + "-2_thumb.jpg")));
    EXPECT_TRUE(fs::exists(root / ".tmp" / (base_id + "-3.jpg.part")));
    EXPECT_TRUE(fs::exists(root / ".tmp" / (base_id + "-4_thumb.jpg.part")));

    const auto unreported = mgr.list_unreported_images();
    ASSERT_EQ(unreported.size(), 1U);
    EXPECT_EQ(unreported[0].image_id, record.image_id);

    fs::remove_all(root);
}

TEST(ImageManagerTest, RejectsThumbnailEncodingFailureWithoutCatalogEntry) {
    namespace fs = std::filesystem;
    const fs::path root = "build/test_images_thumbnail_failure";
    fs::remove_all(root);

    auto processor = std::make_shared<ThumbnailFailureProcessor>();
    argus::core::ImageManager mgr([] { return int64_t{1788185888000000000LL}; });
    mgr.init(root.string(), processor);

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = 1920;
    frame.height = 1080;
    frame.pixel_format = AV_PIX_NV12;

    argus::core::ImageRecord record;
    EXPECT_EQ(mgr.save_detection_image(&frame, nullptr, "thumbnail-failure", &record), AV_ERR_INTERNAL);
    EXPECT_TRUE(mgr.list_unreported_images().empty());

    std::error_code ec;
    for (const auto& entry : fs::recursive_directory_iterator(root, ec)) {
        ASSERT_FALSE(ec);
        if (entry.is_regular_file()) {
            EXPECT_NE(entry.path().extension(), ".jpg");
        }
    }
    fs::remove_all(root);
}

TEST(ImageManagerTest, AtomicSaveCatalogAndBatchDelete) {
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto& mgr = argus::core::ImageManager::instance();
    mgr.init("build/test_images", std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = 1920;
    frame.height = 1080;
    frame.pixel_format = AV_PIX_NV12;

    argus::core::ImageRecord record;
    EXPECT_EQ(mgr.save_detection_image(&frame, nullptr, "test-event-123", &record), AV_OK);
    EXPECT_EQ(record.event_id, "test-event-123");
    EXPECT_TRUE(std::filesystem::exists("build/test_images/" + record.rel_path));
    // 验证硬件缩略图 _thumb.jpg 同步生成
    const auto dot_pos = record.rel_path.rfind('.');
    const std::string thumb_rel = record.rel_path.substr(0, dot_pos) + "_thumb" + record.rel_path.substr(dot_pos);
    EXPECT_TRUE(std::filesystem::exists("build/test_images/" + thumb_rel));
    EXPECT_TRUE(std::filesystem::exists("build/test_images/catalog/catalog.json"));
    EXPECT_TRUE(record.rel_path.find(".tmp") == std::string::npos);

    EXPECT_EQ(mgr.delete_image("../outside-root"), AV_ERR_INVALID_ARG);

    auto del_res = mgr.batch_delete_images({record.image_id, "img-non-existent"});
    EXPECT_EQ(del_res.size(), 2);
    EXPECT_TRUE(del_res[0].second);
    EXPECT_TRUE(del_res[1].second);
    EXPECT_FALSE(std::filesystem::exists("build/test_images/" + record.rel_path));
    EXPECT_FALSE(std::filesystem::exists("build/test_images/" + thumb_rel));
}
