#pragma once

/**
 * @file macos_platform.hpp
 * @brief macOS (Apple Silicon / Intel) 平台适配器实现
 * 
 * 整合 VideoToolbox 硬件解码、vImage/CoreGraphics 图像处理、sysctl/IOKit 遥测监控。
 */

#include "aivision/platform/platform_api.hpp"

namespace aivision::platform {

/**
 * @brief macOS 专用平台适配器
 */
class MacosPlatformAdapter : public IPlatformAdapter {
public:
    MacosPlatformAdapter();
    ~MacosPlatformAdapter() override = default;

    const PlatformProfile& get_profile() const override;
    std::unique_ptr<IDecoder> create_decoder(const std::string& codec_type) override;
    IImageProcessor* get_image_processor() override;
    ITelemetry* get_telemetry() override;
    const av_image_ops* get_c_image_ops() override;
    OpaqueReleaseFn get_opaque_release() const override;

private:
    PlatformProfile profile_;
};

} // namespace aivision::platform

