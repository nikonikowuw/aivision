#pragma once

/**
 * @file mock_platform.hpp
 * @brief 单元测试与无硬件环境使用的 Mock 平台适配器
 * 
 * 提供不依赖真实解码器或硬件图像加速的桩实现（MockDecoder, MockImageProcessor, MockTelemetry, MockPlatformAdapter）。
 */

#include "argus/platform/platform_api.hpp"
#include <string>
#include <cstring>
#include <vector>
#include <chrono>
#include <cstdint>

namespace argus::platform {

/**
 * @brief 模拟解码器，按时间戳递增直接生成测试帧
 */
class MockDecoder : public IDecoder {
public:
    explicit MockDecoder(std::string codec = "H264");
    ~MockDecoder() override = default;

    av_status send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool is_keyframe) override;
    av_status receive_frame(av_frame_desc* out_frame) override;
    void flush() override;
    void reset() override;

    [[nodiscard]] const av_frame_ops* get_frame_ops() const;

private:
    int64_t last_pts_ = 0;
    std::string codec_;
    bool has_packet_ = false;
};

/**
 * @brief 模拟图像处理器，支持空转缩放与基础伪 JPEG 头生成
 */
class MockImageProcessor : public IImageProcessor {
public:
    MockImageProcessor() = default;
    ~MockImageProcessor() override = default;

    av_status resize(const av_frame_desc* src, av_image_view* dst) override {
        if (!src || !dst) return AV_ERR_INVALID_ARG;
        return AV_OK;
    }

    av_status letterbox(const av_frame_desc* src, av_image_view* dst, float* pad_w, float* pad_h, float* scale) override {
        if (!src || !dst) return AV_ERR_INVALID_ARG;
        if (pad_w) *pad_w = 0.0f;
        if (pad_h) *pad_h = 0.0f;
        if (scale) *scale = 1.0f;
        return AV_OK;
    }

    av_status convert_color(const av_image_view* src, av_image_view* dst) override {
        if (!src || !dst) return AV_ERR_INVALID_ARG;
        return AV_OK;
    }

    av_status encode_jpeg(const av_frame_desc* /*src*/, const av_rect* /*crop_roi*/, int /*quality*/, std::vector<uint8_t>& out_jpeg) override {
        // 生成合法的最小虚拟 JPEG 头字节流
        out_jpeg = {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01, 0x00, 0x60, 0x00, 0x60, 0x00, 0x00, 0xFF, 0xD9};
        return AV_OK;
    }

    av_status encode_thumbnail_jpeg(const av_frame_desc* /*src*/, int /*max_width*/, int /*quality*/, std::vector<uint8_t>& out_jpeg) override {
        // 生成合法的最小虚拟缩略图 JPEG 头字节流
        out_jpeg = {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01, 0x00, 0x60, 0x00, 0x60, 0x00, 0x00, 0xFF, 0xD9};
        return AV_OK;
    }
};

/**
 * @brief 模拟系统遥测指标采集器
 */
class MockTelemetry : public ITelemetry {
public:
    MockTelemetry() = default;
    ~MockTelemetry() override = default;

    SystemMetrics collect_metrics() override {
        SystemMetrics m;
        m.uptime_seconds = 3600;
        m.cpu_usage_percent = 12.5f;
        m.memory_usage_percent = 35.0f;
        m.disk_usage_percent = 50.0f;
        m.accelerator_supported = false; // Mock 环境不挂载实际 NPU
        m.temperature_supported = false;
        return m;
    }
};

/**
 * @brief 模拟平台适配器，聚合 Mock 子系统
 */
class MockPlatformAdapter : public IPlatformAdapter {
public:
    MockPlatformAdapter();
    ~MockPlatformAdapter() override = default;

    const PlatformProfile& get_profile() const override { return profile_; }
    std::unique_ptr<IDecoder> create_decoder(const std::string& codec_type) override {
        return std::make_unique<MockDecoder>(codec_type);
    }
    IImageProcessor* get_image_processor() override { return &image_processor_; }
    ITelemetry* get_telemetry() override { return &telemetry_; }
    const av_image_ops* get_c_image_ops() override { return &c_image_ops_; }
    OpaqueReleaseFn get_opaque_release() const override;

private:
    PlatformProfile profile_;
    MockImageProcessor image_processor_;
    MockTelemetry telemetry_;
    av_image_ops c_image_ops_{};
};

} // namespace argus::platform

