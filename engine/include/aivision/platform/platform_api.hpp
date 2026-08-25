#pragma once

#include <string>
#include <vector>
#include <memory>
#include <unordered_map>
#include <cstdint>
#include <limits>
#include "aivision/types.h"
#include "aivision/result.h"

namespace aivision::platform {

enum class CapabilityStatus {
    UNSPECIFIED = 0,
    AVAILABLE = 1,
    DEGRADED = 2,
    UNSUPPORTED = 3,
    UNAVAILABLE = 3, // Alias for UNSUPPORTED for compatibility
};

struct CapabilityItem {
    CapabilityStatus status = CapabilityStatus::UNAVAILABLE;
    std::string reason;
};

struct PlatformProfile {
    std::string platform_id;
    uint32_t platform_tag = 0;
    std::string profile_version = "1.0.0";

    uint32_t total_compute_units = 1000;
    uint32_t reserved_compute_units = 100;
    uint64_t min_free_memory_bytes = 256 * 1024 * 1024; // 256MB

    CapabilityItem hardware_decode;
    CapabilityItem vector_image_ops;
    CapabilityItem telemetry_metrics;
};

class IDecoder {
public:
    virtual ~IDecoder() = default;
    virtual av_status send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool is_keyframe) = 0;
    virtual av_status receive_frame(av_frame_desc* out_frame) = 0;
    virtual void flush() = 0;
    virtual void reset() = 0;
};

class IImageProcessor {
public:
    virtual ~IImageProcessor() = default;
    virtual av_status resize(const av_frame_desc* src, av_image_view* dst) = 0;
    virtual av_status letterbox(const av_frame_desc* src, av_image_view* dst, float* pad_w, float* pad_h, float* scale) = 0;
    virtual av_status convert_color(const av_image_view* src, av_image_view* dst) = 0;
    virtual av_status encode_jpeg(const av_frame_desc* src, const av_rect* crop_roi, int quality, std::vector<uint8_t>& out_jpeg) = 0;
};

struct SystemMetrics {
    int64_t uptime_seconds = 0;
    float cpu_usage_percent = 0.0f;
    float memory_usage_percent = 0.0f;
    float disk_usage_percent = 0.0f;
    float accelerator_usage_percent = std::numeric_limits<float>::quiet_NaN();
    bool accelerator_supported = false;
    float temperature_celsius = std::numeric_limits<float>::quiet_NaN();
    bool temperature_supported = false;
};

class ITelemetry {
public:
    virtual ~ITelemetry() = default;
    virtual SystemMetrics collect_metrics() = 0;
};

using OpaqueReleaseFn = void (*)(void* opaque);

class IPlatformAdapter {
public:
    virtual ~IPlatformAdapter() = default;
    [[nodiscard]] virtual const PlatformProfile& get_profile() const = 0;
    [[nodiscard]] virtual std::unique_ptr<IDecoder> create_decoder(const std::string& codec_type) = 0;
    [[nodiscard]] virtual IImageProcessor* get_image_processor() = 0;
    [[nodiscard]] virtual ITelemetry* get_telemetry() = 0;
    [[nodiscard]] virtual const av_image_ops* get_c_image_ops() = 0;
    [[nodiscard]] virtual OpaqueReleaseFn get_opaque_release() const = 0;
};

class PlatformRegistry {
public:
    static PlatformRegistry& instance();
    void register_adapter(const std::string& platform_id, std::shared_ptr<IPlatformAdapter> adapter);
    std::shared_ptr<IPlatformAdapter> get_adapter(const std::string& platform_id);
    void set_active_platform(const std::string& platform_id);
    std::shared_ptr<IPlatformAdapter> get_active_adapter();

private:
    PlatformRegistry() = default;
    std::unordered_map<std::string, std::shared_ptr<IPlatformAdapter>> adapters_;
    std::shared_ptr<IPlatformAdapter> active_adapter_;
    std::string active_platform_id_;
};

} // namespace aivision::platform
