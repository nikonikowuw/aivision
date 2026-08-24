#import "aivision/platform/macos_platform.hpp"
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>

namespace aivision::platform {

MacosPlatformAdapter::MacosPlatformAdapter() {
    profile_.platform_id = "macos-arm64-coreml";
    profile_.platform_tag = 0x4D41434F; // 'MACO'
    profile_.total_compute_units = 1000;
    profile_.reserved_compute_units = 100;
    profile_.hardware_decode.status = CapabilityStatus::AVAILABLE;
    profile_.vector_image_ops.status = CapabilityStatus::AVAILABLE;
    profile_.telemetry_metrics.status = CapabilityStatus::AVAILABLE;
}

const PlatformProfile& MacosPlatformAdapter::get_profile() const {
    return profile_;
}

OpaqueReleaseFn MacosPlatformAdapter::get_opaque_release() const {
    return [](void* opaque) {
        if (opaque) CVPixelBufferRelease(static_cast<CVPixelBufferRef>(opaque));
    };
}

} // namespace aivision::platform