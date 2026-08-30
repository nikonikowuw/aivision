/**
 * @file test_contract_mock.cpp
 * @brief 契约测试：验证 Mock 平台适配器、解码器、图像处理与 C ABI 规范对齐
 */

#include <gtest/gtest.h>
#include <cmath>
#include "argus/platform/mock_platform.hpp"


TEST(ContractMockTest, PlatformProfileAndImageOps) {
    static_assert(static_cast<int>(argus::platform::CapabilityStatus::UNSPECIFIED) == 0);
    static_assert(static_cast<int>(argus::platform::CapabilityStatus::AVAILABLE) == 1);
    static_assert(static_cast<int>(argus::platform::CapabilityStatus::DEGRADED) == 2);
    static_assert(static_cast<int>(argus::platform::CapabilityStatus::UNSUPPORTED) == 3);
    static_assert(static_cast<int>(argus::platform::CapabilityStatus::UNAVAILABLE) == 3);

    argus::platform::MockPlatformAdapter mock_adapter;
    const auto& profile = mock_adapter.get_profile();
    EXPECT_EQ(profile.platform_id, "mock");
    EXPECT_EQ(profile.total_compute_units, 1000);
    EXPECT_EQ(profile.hardware_decode.status, argus::platform::CapabilityStatus::AVAILABLE);

    auto decoder = mock_adapter.create_decoder("H264");
    ASSERT_NE(decoder, nullptr);

    uint8_t sps_packet[] = {0x00, 0x00, 0x00, 0x01, 0x67};
    uint8_t pps_packet[] = {0x00, 0x00, 0x00, 0x01, 0x68};
    uint8_t idr_packet[] = {0x00, 0x00, 0x00, 0x01, 0x65};
    EXPECT_EQ(decoder->send_packet(sps_packet, sizeof(sps_packet), 1000, false), AV_OK);
    EXPECT_EQ(decoder->send_packet(pps_packet, sizeof(pps_packet), 1000, false), AV_OK);
    EXPECT_EQ(decoder->send_packet(idr_packet, sizeof(idr_packet), 1000, true), AV_OK);

    av_frame_desc frame{};
    EXPECT_EQ(decoder->receive_frame(&frame), AV_OK);
    EXPECT_EQ(frame.width, 1920);
    EXPECT_EQ(frame.height, 1080);
    EXPECT_EQ(frame.pixel_format, AV_PIX_NV12);

    auto* mock_decoder = dynamic_cast<argus::platform::MockDecoder*>(decoder.get());
    ASSERT_NE(mock_decoder, nullptr);
    ASSERT_NE(frame.frame_token, nullptr);
    EXPECT_EQ(mock_decoder->get_frame_ops()->retain(mock_decoder->get_frame_ops()->ctx, frame.frame_token), AV_OK);

    const auto* image_ops = mock_adapter.get_c_image_ops();
    ASSERT_NE(image_ops, nullptr);
    ASSERT_NE(image_ops->alloc, nullptr);
    av_image_view image{};
    ASSERT_EQ(image_ops->alloc(image_ops->ctx, 2, 2, AV_PIX_BGRA, &image), AV_OK);
    ASSERT_NE(image.data, nullptr);
    EXPECT_EQ(image_ops->convert(image_ops->ctx, &frame, nullptr, &image, 0), AV_OK);
    const uint8_t pad_value[4] = {1, 2, 3, 4};
    EXPECT_EQ(image_ops->pad(image_ops->ctx, &image, nullptr, pad_value), AV_OK);
    EXPECT_EQ(image_ops->free(image_ops->ctx, &image), AV_OK);
    EXPECT_EQ(mock_decoder->get_frame_ops()->release(mock_decoder->get_frame_ops()->ctx, frame.frame_token), AV_OK);
    EXPECT_EQ(mock_decoder->get_frame_ops()->release(mock_decoder->get_frame_ops()->ctx, frame.frame_token), AV_OK);

    auto* telemetry = mock_adapter.get_telemetry();
    auto metrics = telemetry->collect_metrics();
    EXPECT_FALSE(metrics.accelerator_supported);
    EXPECT_TRUE(std::isnan(metrics.accelerator_usage_percent));
    EXPECT_FALSE(metrics.temperature_supported);
    EXPECT_TRUE(std::isnan(metrics.temperature_celsius));
}
