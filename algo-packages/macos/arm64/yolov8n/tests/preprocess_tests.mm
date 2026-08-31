#import <Accelerate/Accelerate.h>
#import <CoreVideo/CoreVideo.h>

#include "argus/types.h"
#include "preprocess/preprocessor.hpp"

#include <algorithm>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

namespace {

void require_condition(bool condition, const char* message) {
    if (!condition) {
        std::fprintf(stderr, "%s\n", message);
        std::abort();
    }
}

CVPixelBufferRef make_gray_nv12(uint32_t width, uint32_t height) {
    CVPixelBufferRef buffer = nullptr;
    if (CVPixelBufferCreate(kCFAllocatorDefault, width, height,
                            kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
                            nullptr, &buffer) != kCVReturnSuccess || !buffer) {
        return nullptr;
    }
    if (CVPixelBufferLockBaseAddress(buffer, 0) != kCVReturnSuccess) {
        CVPixelBufferRelease(buffer);
        return nullptr;
    }
    auto* y_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(buffer, 0));
    auto* uv_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(buffer, 1));
    const size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(buffer, 0);
    const size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(buffer, 1);
    const size_t y_height = CVPixelBufferGetHeightOfPlane(buffer, 0);
    const size_t uv_height = CVPixelBufferGetHeightOfPlane(buffer, 1);
    if (!y_plane || !uv_plane) {
        CVPixelBufferUnlockBaseAddress(buffer, 0);
        CVPixelBufferRelease(buffer);
        return nullptr;
    }
    for (size_t row = 0; row < y_height; ++row) {
        std::memset(y_plane + row * y_stride, 128, CVPixelBufferGetWidthOfPlane(buffer, 0));
    }
    for (size_t row = 0; row < uv_height; ++row) {
        std::memset(uv_plane + row * uv_stride, 128, CVPixelBufferGetWidthOfPlane(buffer, 1) * 2);
    }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    return buffer;
}

av_frame_desc make_frame(CVPixelBufferRef buffer) {
    av_frame_desc frame{};
    frame.size = sizeof(frame);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.frame_id = 1;
    frame.opaque = buffer;
    frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    frame.memory_type = AV_MEM_PLATFORM_SURFACE;
    frame.pixel_format = AV_PIX_NV12;
    frame.layout = AV_LAYOUT_PLATFORM_NATIVE;
    frame.width = static_cast<uint32_t>(CVPixelBufferGetWidth(buffer));
    frame.height = static_cast<uint32_t>(CVPixelBufferGetHeight(buffer));
    frame.alloc_width = frame.width;
    frame.alloc_height = frame.height;
    frame.stride[0] = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(buffer, 0));
    frame.stride[1] = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(buffer, 1));
    frame.color_primaries = AV_COLOR_PRIM_BT709;
    frame.color_transfer = AV_COLOR_TRC_BT709;
    frame.color_matrix = AV_COLOR_MAT_BT709;
    frame.color_range = AV_COLOR_RANGE_LIMITED;
    frame.plane_count = 2;
    return frame;
}

int test_alloc(void*, uint32_t width, uint32_t height, uint32_t pixel_format, av_image_view* out) {
    if (!out || width == 0 || height == 0 || pixel_format != AV_PIX_BGRA) return AV_ERR_INVALID_ARG;
    CVPixelBufferRef buffer = nullptr;
    if (CVPixelBufferCreate(kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA,
                            nullptr, &buffer) != kCVReturnSuccess || !buffer) {
        return AV_ERR_OUT_OF_MEMORY;
    }
    *out = {};
    out->size = sizeof(*out);
    out->api_version = AV_ALGO_API_VERSION;
    out->width = width;
    out->height = height;
    out->pixel_format = AV_PIX_BGRA;
    out->memory_type = AV_MEM_PLATFORM_SURFACE;
    out->plane_count = 1;
    out->opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    out->stride[0] = static_cast<int32_t>(CVPixelBufferGetBytesPerRow(buffer));
    out->opaque = buffer;
    return AV_OK;
}

int test_free(void*, av_image_view* image) {
    if (!image) return AV_ERR_INVALID_ARG;
    if (image->opaque) CVPixelBufferRelease(static_cast<CVPixelBufferRef>(image->opaque));
    *image = {};
    return AV_OK;
}

int test_pad(void*, const av_image_view* dst, const av_rect* region, const uint8_t value[4]) {
    if (!dst || !dst->opaque || region || !value || dst->pixel_format != AV_PIX_BGRA) return AV_ERR_INVALID_ARG;
    auto buffer = static_cast<CVPixelBufferRef>(dst->opaque);
    if (CVPixelBufferLockBaseAddress(buffer, 0) != kCVReturnSuccess) return AV_ERR_INTERNAL;
    auto* base = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(buffer));
    const size_t stride = CVPixelBufferGetBytesPerRow(buffer);
    for (uint32_t row = 0; row < dst->height; ++row) {
        for (uint32_t column = 0; column < dst->width; ++column) {
            std::memcpy(base + static_cast<size_t>(row) * stride + static_cast<size_t>(column) * 4, value, 4);
        }
    }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    return AV_OK;
}

int test_convert(void*, const av_frame_desc* src, const av_rect* src_roi,
                 const av_image_view* dst, uint32_t) {
    if (!src || src_roi || !src->opaque || !dst || !dst->opaque ||
        src->pixel_format != AV_PIX_NV12 || dst->pixel_format != AV_PIX_BGRA) {
        return AV_ERR_INVALID_ARG;
    }
    auto source = static_cast<CVPixelBufferRef>(src->opaque);
    auto target = static_cast<CVPixelBufferRef>(dst->opaque);
    const bool source_locked = CVPixelBufferLockBaseAddress(source, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess;
    const bool target_locked = source_locked && CVPixelBufferLockBaseAddress(target, 0) == kCVReturnSuccess;
    if (!target_locked) {
        if (source_locked) CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
        return AV_ERR_INTERNAL;
    }

    vImage_Buffer y_buffer{
        CVPixelBufferGetBaseAddressOfPlane(source, 0),
        CVPixelBufferGetHeightOfPlane(source, 0),
        CVPixelBufferGetWidthOfPlane(source, 0),
        CVPixelBufferGetBytesPerRowOfPlane(source, 0)};
    vImage_Buffer uv_buffer{
        CVPixelBufferGetBaseAddressOfPlane(source, 1),
        CVPixelBufferGetHeightOfPlane(source, 1),
        CVPixelBufferGetWidthOfPlane(source, 1),
        CVPixelBufferGetBytesPerRowOfPlane(source, 1)};
    vImage_Buffer argb{};
    vImage_Buffer bgra{};
    const size_t source_width = CVPixelBufferGetWidth(source);
    const size_t source_height = CVPixelBufferGetHeight(source);
    vImage_Error conversion_error = vImageBuffer_Init(&argb, source_height, source_width, 32, kvImageNoFlags);
    if (conversion_error == kvImageNoError) {
        conversion_error = vImageBuffer_Init(&bgra, source_height, source_width, 32, kvImageNoFlags);
    }
    vImage_YpCbCrToARGB conversion{};
    const vImage_YpCbCrPixelRange range = {16, 128, 235, 240, 255, 0, 255, 1};
    if (conversion_error == kvImageNoError) {
        conversion_error = vImageConvert_YpCbCrToARGB_GenerateConversion(
            kvImage_YpCbCrToARGBMatrix_ITU_R_709_2, &range, &conversion,
            kvImage420Yp8_CbCr8, kvImageARGB8888, kvImageNoFlags);
    }
    if (conversion_error == kvImageNoError) {
        conversion_error = vImageConvert_420Yp8_CbCr8ToARGB8888(
            &y_buffer, &uv_buffer, &argb, &conversion, nullptr, 255, kvImageNoFlags);
    }
    const uint8_t permutation[4] = {3, 2, 1, 0};
    if (conversion_error == kvImageNoError) {
        conversion_error = vImagePermuteChannels_ARGB8888(&argb, &bgra, permutation, kvImageNoFlags);
    }
    if (conversion_error == kvImageNoError) {
        vImage_Buffer destination{
            CVPixelBufferGetBaseAddress(target),
            CVPixelBufferGetHeight(target),
            CVPixelBufferGetWidth(target),
            CVPixelBufferGetBytesPerRow(target)};
        conversion_error = vImageScale_ARGB8888(&bgra, &destination, nullptr, kvImageHighQualityResampling);
    }
    if (argb.data) free(argb.data);
    if (bgra.data) free(bgra.data);
    CVPixelBufferUnlockBaseAddress(target, 0);
    CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
    return conversion_error == kvImageNoError ? AV_OK : AV_ERR_INTERNAL;
}

std::vector<uint8_t> read_bgra(void* value, uint32_t width, uint32_t height) {
    auto buffer = static_cast<CVPixelBufferRef>(value);
    require_condition(CVPixelBufferLockBaseAddress(buffer, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess,
                      "failed to lock BGRA output");
    const auto* base = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(buffer));
    const size_t stride = CVPixelBufferGetBytesPerRow(buffer);
    std::vector<uint8_t> pixels(static_cast<size_t>(width) * height * 4);
    for (uint32_t row = 0; row < height; ++row) {
        std::memcpy(pixels.data() + static_cast<size_t>(row) * width * 4,
                    base + static_cast<size_t>(row) * stride, static_cast<size_t>(width) * 4);
    }
    CVPixelBufferUnlockBaseAddress(buffer, kCVPixelBufferLock_ReadOnly);
    return pixels;
}

} // namespace

int main() {
    @autoreleasepool {
        constexpr uint32_t kSourceWidth = 320;
        constexpr uint32_t kSourceHeight = 480;
        constexpr uint32_t kTargetWidth = 640;
        constexpr uint32_t kTargetHeight = 384;
        CVPixelBufferRef source = make_gray_nv12(kSourceWidth, kSourceHeight);
        require_condition(source != nullptr, "failed to create NV12 test frame");
        const av_frame_desc frame = make_frame(source);

        av_image_ops image_ops{};
        image_ops.size = sizeof(image_ops);
        image_ops.api_version = AV_ALGO_API_VERSION;
        image_ops.convert = test_convert;
        image_ops.pad = test_pad;
        image_ops.alloc = test_alloc;
        image_ops.free = test_free;

        void* fallback = yolov8n::Preprocessor::create_input_pixelbuffer(
            &frame, nullptr, kTargetWidth, kTargetHeight);
        void* injected = yolov8n::Preprocessor::create_input_pixelbuffer(
            &frame, &image_ops, kTargetWidth, kTargetHeight);
        require_condition(fallback != nullptr, "fallback preprocessing failed");
        require_condition(injected != nullptr, "image_ops preprocessing failed");

        const auto fallback_pixels = read_bgra(fallback, kTargetWidth, kTargetHeight);
        const auto injected_pixels = read_bgra(injected, kTargetWidth, kTargetHeight);
        require_condition(fallback_pixels == injected_pixels, "image_ops output differs from fallback output");

        yolov8n::Preprocessor::release_pixelbuffer(fallback);
        yolov8n::Preprocessor::release_pixelbuffer(injected);
        CVPixelBufferRelease(source);
    }
    return 0;
}
