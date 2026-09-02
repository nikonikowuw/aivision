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
#ifdef __APPLE__
#include "argus/platform/macos_platform.hpp"
#include <CoreVideo/CoreVideo.h>
#include <ImageIO/ImageIO.h>

TEST(ImageManagerTest, MacosRoiCropThumbnailDimensions) {
    namespace fs = std::filesystem;
    const fs::path root = "build/test_images_macos_roi";
    fs::remove_all(root);

    auto adapter = std::make_shared<argus::platform::MacosPlatformAdapter>();
    argus::core::ImageManager mgr([] { return int64_t{1788185888000000000LL}; });
    mgr.init(root.string(), std::shared_ptr<argus::platform::IImageProcessor>(
        adapter, adapter->get_image_processor()));

    CVPixelBufferRef buffer = nullptr;
    CVReturn cv_res = CVPixelBufferCreate(kCFAllocatorDefault, 1920, 1080, kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange, nullptr, &buffer);
    ASSERT_EQ(cv_res, kCVReturnSuccess);
    ASSERT_NE(buffer, nullptr);

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = 1920;
    frame.height = 1080;
    frame.pixel_format = AV_PIX_NV12;
    frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    frame.opaque = buffer;

    // 1. 全景图
    argus::core::ImageRecord pano_record;
    ASSERT_EQ(mgr.save_detection_image(&frame, nullptr, "event-pano", &pano_record), AV_OK);

    // 2. 特写图：人脸 ROI (x=0.4, y=0.3, w=0.1, h=0.2) -> 192x216
    av_rect roi{};
    roi.size = sizeof(av_rect);
    roi.api_version = AV_ALGO_API_VERSION;
    roi.x = 0.4f;
    roi.y = 0.3f;
    roi.width = 0.1f;
    roi.height = 0.2f;

    argus::core::ImageRecord face_record;
    ASSERT_EQ(mgr.save_detection_image(&frame, &roi, "event-face", &face_record), AV_OK);

    // 检查特写原图和特写缩略图的分辨率
    auto get_image_size = [](const fs::path& path, int& out_w, int& out_h) {
        CFURLRef url = CFURLCreateFromFileSystemRepresentation(kCFAllocatorDefault, (const UInt8*)path.string().c_str(), path.string().length(), false);
        CGImageSourceRef src = CGImageSourceCreateWithURL(url, nullptr);
        CFRelease(url);
        if (!src) return false;
        CFDictionaryRef props = CGImageSourceCopyPropertiesAtIndex(src, 0, nullptr);
        CFRelease(src);
        if (!props) return false;
        CFNumberRef w_num = (CFNumberRef)CFDictionaryGetValue(props, kCGImagePropertyPixelWidth);
        CFNumberRef h_num = (CFNumberRef)CFDictionaryGetValue(props, kCGImagePropertyPixelHeight);
        CFNumberGetValue(w_num, kCFNumberIntType, &out_w);
        CFNumberGetValue(h_num, kCFNumberIntType, &out_h);
        CFRelease(props);
        return true;
    };

    int pano_w = 0, pano_h = 0;
    int pano_thumb_w = 0, pano_thumb_h = 0;
    int face_w = 0, face_h = 0;
    int face_thumb_w = 0, face_thumb_h = 0;

    const std::string pano_thumb = pano_record.rel_path.substr(0, pano_record.rel_path.rfind('.')) + "_thumb.jpg";
    const std::string face_thumb = face_record.rel_path.substr(0, face_record.rel_path.rfind('.')) + "_thumb.jpg";

    EXPECT_TRUE(get_image_size(root / pano_record.rel_path, pano_w, pano_h));
    EXPECT_TRUE(get_image_size(root / pano_thumb, pano_thumb_w, pano_thumb_h));
    EXPECT_TRUE(get_image_size(root / face_record.rel_path, face_w, face_h));
    EXPECT_TRUE(get_image_size(root / face_thumb, face_thumb_w, face_thumb_h));

    EXPECT_EQ(pano_w, 1920);
    EXPECT_EQ(pano_h, 1080);
    EXPECT_EQ(pano_thumb_w, 360);
    EXPECT_EQ(pano_thumb_h, 203);

    EXPECT_EQ(face_w, 192);
    EXPECT_EQ(face_h, 216);
    EXPECT_EQ(face_thumb_w, 192);
    EXPECT_EQ(face_thumb_h, 216);

    CVPixelBufferRelease(buffer);
    fs::remove_all(root);
}
#endif


namespace {

class ThumbnailFailureProcessor final : public argus::platform::MockImageProcessor {
public:
    av_status encode_thumbnail_jpeg(const av_frame_desc*, const av_rect*, int, int,
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

    // 验证带 ROI 裁剪的目标特写图及其缩略图生成
    av_rect face_roi{};
    face_roi.size = sizeof(av_rect);
    face_roi.api_version = AV_ALGO_API_VERSION;
    face_roi.x = 0.2f;
    face_roi.y = 0.3f;
    face_roi.width = 0.15f;
    face_roi.height = 0.25f;
    argus::core::ImageRecord face_record;
    EXPECT_EQ(mgr.save_detection_image(&frame, &face_roi, "face-event-456", &face_record), AV_OK);
    EXPECT_EQ(face_record.event_id, "face-event-456");
    EXPECT_TRUE(std::filesystem::exists("build/test_images/" + face_record.rel_path));
    const auto face_dot_pos = face_record.rel_path.rfind('.');
    const std::string face_thumb_rel = face_record.rel_path.substr(0, face_dot_pos) + "_thumb" + face_record.rel_path.substr(face_dot_pos);
    EXPECT_TRUE(std::filesystem::exists("build/test_images/" + face_thumb_rel));
    EXPECT_TRUE(mgr.delete_image(face_record.image_id) == AV_OK);
    EXPECT_FALSE(std::filesystem::exists("build/test_images/" + face_record.rel_path));
    EXPECT_FALSE(std::filesystem::exists("build/test_images/" + face_thumb_rel));
}
