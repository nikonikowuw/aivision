#pragma once

#include "aivision/platform/platform_api.hpp"
#include <string>
#include <cstring>
#include <vector>
#include <chrono>
#include <cstdint>

namespace aivision::platform {

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

    av_status encode_jpeg(const av_frame_desc* src, const av_rect* crop_roi, int quality, std::vector<uint8_t>& out_jpeg) override {
        // Minimal fake JPEG header
        out_jpeg = {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01, 0x00, 0x60, 0x00, 0x60, 0x00, 0x00, 0xFF, 0xD9};
        return AV_OK;
    }
};

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
        m.accelerator_supported = false; // Mock doesn't have NPU
        m.temperature_supported = false;
        return m;
    }
};

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

} // namespace aivision::platform
