#pragma once

#include "aivision/platform/platform_api.hpp"

namespace aivision::platform {

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
