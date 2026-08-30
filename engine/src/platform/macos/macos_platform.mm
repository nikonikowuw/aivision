/**
 * @file macos_platform.mm
 * @brief macOS 平台适配器主实现与能力档案配置
 */

#import "argus/platform/macos_platform.hpp"
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>

namespace argus::platform {

MacosPlatformAdapter::MacosPlatformAdapter() {
    // 初始化 macOS Apple Silicon 平台能力档案
    profile_.platform_id = "macos-arm64-coreml";
    profile_.platform_tag = 0x4D41434F; // 'MACO'
    profile_.total_compute_units = 1000; // 统一定义为 1000 算力点数基准
    profile_.reserved_compute_units = 100; // 预留 100 点给系统服务
    profile_.hardware_decode.status = CapabilityStatus::AVAILABLE;
    profile_.vector_image_ops.status = CapabilityStatus::AVAILABLE;
    profile_.telemetry_metrics.status = CapabilityStatus::AVAILABLE;
}

const PlatformProfile& MacosPlatformAdapter::get_profile() const {
    return profile_;
}

OpaqueReleaseFn MacosPlatformAdapter::get_opaque_release() const {
    // 提供对底层 CVPixelBufferRef 的 CoreVideo 原生安全释放函数指针
    return [](void* opaque) {
        if (opaque) CVPixelBufferRelease(static_cast<CVPixelBufferRef>(opaque));
    };
}

} // namespace argus::platform
