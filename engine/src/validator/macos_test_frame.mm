/**
 * @file macos_test_frame.mm
 * @brief macOS 算法包自检测试帧生成实现（通过 ImageIO 加载 testimage.jpg 渲染为 CVPixelBuffer）
 */

#import <CoreGraphics/CoreGraphics.h>
#import <CoreVideo/CoreVideo.h>
#import <ImageIO/ImageIO.h>


#include "argus/types.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <filesystem>

namespace {

uint8_t clamp_byte(double value) {
    return static_cast<uint8_t>(std::clamp<long>(std::lround(value), 0, 255));
}

CGImageRef load_image(const char* path) {
    if (!path) return nullptr;
    CFURLRef url = CFURLCreateFromFileSystemRepresentation(
        kCFAllocatorDefault, reinterpret_cast<const UInt8*>(path), std::strlen(path), false);
    if (!url) return nullptr;
    CGImageSourceRef source = CGImageSourceCreateWithURL(url, nullptr);
    CFRelease(url);
    if (!source) return nullptr;
    CGImageRef image = CGImageSourceCreateImageAtIndex(source, 0, nullptr);
    CFRelease(source);
    return image;
}

CVPixelBufferRef image_to_bgra(CGImageRef image, size_t width, size_t height) {
    CVPixelBufferRef buffer = nullptr;
    if (CVPixelBufferCreate(kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA,
                            nullptr, &buffer) != kCVReturnSuccess || !buffer) {
        return nullptr;
    }
    if (CVPixelBufferLockBaseAddress(buffer, 0) != kCVReturnSuccess) {
        CVPixelBufferRelease(buffer);
        return nullptr;
    }
    CGColorSpaceRef color_space = CGColorSpaceCreateDeviceRGB();
    CGContextRef context = CGBitmapContextCreate(
        CVPixelBufferGetBaseAddress(buffer), width, height, 8, CVPixelBufferGetBytesPerRow(buffer), color_space,
        static_cast<CGBitmapInfo>(static_cast<uint32_t>(kCGImageAlphaPremultipliedFirst) |
                                  static_cast<uint32_t>(kCGBitmapByteOrder32Little)));
    if (context) CGContextDrawImage(context, CGRectMake(0, 0, width, height), image);
    if (context) CGContextRelease(context);
    CGColorSpaceRelease(color_space);
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    return buffer;
}

} // namespace

extern "C" bool argus_validator_create_test_frame(const char* package_root,
                                                       const char* test_image_file,
                                                       av_frame_desc* out_frame,
                                                       void** owner) {
    if (!package_root || !out_frame || !owner) return false;
    *owner = nullptr;
    const std::filesystem::path image_path = std::filesystem::path(package_root) /
        (test_image_file && *test_image_file ? test_image_file : "testimage.jpg");
    CGImageRef image = load_image(image_path.c_str());
    if (!image) return false;

    const size_t image_width = CGImageGetWidth(image) & ~static_cast<size_t>(1);
    const size_t image_height = CGImageGetHeight(image) & ~static_cast<size_t>(1);
    if (image_width < 320 || image_height < 320) {
        CGImageRelease(image);
        return false;
    }
    CVPixelBufferRef bgra = image_to_bgra(image, image_width, image_height);
    CGImageRelease(image);
    if (!bgra) return false;

    CVPixelBufferRef nv12 = nullptr;
    if (CVPixelBufferCreate(kCFAllocatorDefault, image_width, image_height,
                            kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
                            nullptr, &nv12) != kCVReturnSuccess || !nv12) {
        CVPixelBufferRelease(bgra);
        return false;
    }
    const bool bgra_locked = CVPixelBufferLockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess;
    const bool nv12_locked = bgra_locked && CVPixelBufferLockBaseAddress(nv12, 0) == kCVReturnSuccess;
    if (!nv12_locked) {
        if (bgra_locked) CVPixelBufferUnlockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
        CVPixelBufferRelease(nv12);
        CVPixelBufferRelease(bgra);
        return false;
    }

    const auto* source = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(bgra));
    const size_t source_stride = CVPixelBufferGetBytesPerRow(bgra);
    auto* y_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 0));
    auto* uv_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 1));
    const size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(nv12, 0);
    const size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(nv12, 1);

    for (size_t y = 0; y < image_height; ++y) {
        for (size_t x = 0; x < image_width; ++x) {
            const uint8_t* pixel = source + y * source_stride + x * 4;
            y_plane[y * y_stride + x] = clamp_byte(16.0 +
                (65.481 * pixel[2] + 128.553 * pixel[1] + 24.966 * pixel[0]) / 255.0);
        }
    }
    for (size_t y = 0; y < image_height; y += 2) {
        for (size_t x = 0; x < image_width; x += 2) {
            double cb = 0.0;
            double cr = 0.0;
            for (size_t dy = 0; dy < 2; ++dy) {
                for (size_t dx = 0; dx < 2; ++dx) {
                    const uint8_t* pixel = source + (y + dy) * source_stride + (x + dx) * 4;
                    cb += 128.0 + (-37.797 * pixel[2] - 74.203 * pixel[1] + 112.0 * pixel[0]) / 255.0;
                    cr += 128.0 + (112.0 * pixel[2] - 93.786 * pixel[1] - 18.214 * pixel[0]) / 255.0;
                }
            }
            uint8_t* uv = uv_plane + (y / 2) * uv_stride + x;
            uv[0] = clamp_byte(cb / 4.0);
            uv[1] = clamp_byte(cr / 4.0);
        }
    }

    const auto* plane0 = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 0));
    const auto* plane1 = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 1));
    *out_frame = {};
    out_frame->size = sizeof(av_frame_desc);
    out_frame->api_version = AV_ALGO_API_VERSION;
    out_frame->frame_id = 1;
    out_frame->wall_time_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count();
    out_frame->pts_ns = out_frame->wall_time_ns;
    out_frame->opaque = nv12;
    out_frame->frame_token = nv12;
    out_frame->opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    out_frame->memory_type = AV_MEM_PLATFORM_SURFACE;
    out_frame->pixel_format = AV_PIX_NV12;
    out_frame->layout = AV_LAYOUT_PLATFORM_NATIVE;
    out_frame->width = static_cast<uint32_t>(image_width);
    out_frame->height = static_cast<uint32_t>(image_height);
    out_frame->alloc_width = static_cast<uint32_t>(image_width);
    out_frame->alloc_height = static_cast<uint32_t>(image_height);
    out_frame->stride[0] = static_cast<int32_t>(y_stride);
    out_frame->stride[1] = static_cast<int32_t>(uv_stride);
    out_frame->offset[1] = plane0 && plane1 && reinterpret_cast<uintptr_t>(plane1) >= reinterpret_cast<uintptr_t>(plane0)
        ? static_cast<uint64_t>(reinterpret_cast<uintptr_t>(plane1) - reinterpret_cast<uintptr_t>(plane0))
        : 0;
    out_frame->color_primaries = AV_COLOR_PRIM_BT709;
    out_frame->color_transfer = AV_COLOR_TRC_BT709;
    out_frame->color_matrix = AV_COLOR_MAT_BT709;
    out_frame->color_range = AV_COLOR_RANGE_LIMITED;
    out_frame->plane_count = 2;
    out_frame->time_synced = 1;

    CVPixelBufferUnlockBaseAddress(nv12, 0);
    CVPixelBufferUnlockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferRelease(bgra);
    *owner = nv12;
    return true;
}

extern "C" void argus_validator_release_test_frame(void* owner) {
    if (owner) CVPixelBufferRelease(static_cast<CVPixelBufferRef>(owner));
}
