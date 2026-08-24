#import "preprocessor.hpp"
#import <Foundation/Foundation.h>
#import <CoreVideo/CoreVideo.h>
#import <Accelerate/Accelerate.h>

#include "aivision/cv/letterbox.hpp"

#include <algorithm>
#include <cstdint>

namespace yolov8n {
namespace {

const vImage_YpCbCrToARGBMatrix* select_matrix(const av_frame_desc* frame) {
    if (frame->color_matrix == AV_COLOR_MAT_UNSPECIFIED || frame->color_matrix == AV_COLOR_MAT_BT709) {
        return kvImage_YpCbCrToARGBMatrix_ITU_R_709_2;
    }
    return nullptr;
}

vImage_YpCbCrPixelRange select_range(uint8_t color_range) {
    if (color_range == AV_COLOR_RANGE_FULL) {
        return {0, 128, 255, 255, 255, 1, 255, 0};
    }
    return {16, 128, 235, 240, 255, 0, 255, 1};
}

void release_buffer(vImage_Buffer* buffer) {
    if (buffer && buffer->data) {
        free(buffer->data);
        buffer->data = nullptr;
    }
}

bool copy_scaled_bgra(const av_image_view& source, const av_image_view& target,
                      uint32_t offset_x, uint32_t offset_y) {
    if (source.pixel_format != AV_PIX_BGRA || target.pixel_format != AV_PIX_BGRA ||
        !source.opaque || !target.opaque || source.width == 0 || source.height == 0 ||
        offset_x > target.width || offset_y > target.height ||
        source.width > target.width - offset_x || source.height > target.height - offset_y) {
        return false;
    }

    auto source_buffer = static_cast<CVPixelBufferRef>(source.opaque);
    auto target_buffer = static_cast<CVPixelBufferRef>(target.opaque);
    const bool source_locked = CVPixelBufferLockBaseAddress(source_buffer, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess;
    const bool target_locked = source_locked && CVPixelBufferLockBaseAddress(target_buffer, 0) == kCVReturnSuccess;
    if (!target_locked) {
        if (source_locked) CVPixelBufferUnlockBaseAddress(source_buffer, kCVPixelBufferLock_ReadOnly);
        return false;
    }

    const auto* source_base = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(source_buffer));
    auto* target_base = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(target_buffer));
    const size_t source_stride = CVPixelBufferGetBytesPerRow(source_buffer);
    const size_t target_stride = CVPixelBufferGetBytesPerRow(target_buffer);
    vImage_Buffer source_view{const_cast<uint8_t*>(source_base), source.height, source.width, source_stride};
    vImage_Buffer target_view{
        target_base + static_cast<size_t>(offset_y) * target_stride + static_cast<size_t>(offset_x) * 4,
        source.height, source.width, target_stride};
    const bool copied = source_base && target_base &&
        vImageCopyBuffer(&source_view, &target_view, 4, kvImageNoFlags) == kvImageNoError;

    CVPixelBufferUnlockBaseAddress(target_buffer, 0);
    CVPixelBufferUnlockBaseAddress(source_buffer, kCVPixelBufferLock_ReadOnly);
    return copied;
}

void* create_input_pixelbuffer_with_ops(const av_frame_desc* src, const av_image_ops* image_ops,
                                        int target_w, int target_h) {
    if (!src || !image_ops || image_ops->size < sizeof(av_image_ops) ||
        image_ops->api_version != AV_ALGO_API_VERSION || !image_ops->alloc ||
        !image_ops->convert || !image_ops->pad || !image_ops->free || target_w <= 0 || target_h <= 0 ||
        src->width == 0 || src->height == 0) {
        return nullptr;
    }

    const auto letterbox = aivision::cv::compute_letterbox(
        src->width, src->height, static_cast<uint32_t>(target_w), static_cast<uint32_t>(target_h));
    const uint32_t scaled_w = std::max<uint32_t>(1, std::min<uint32_t>(
        static_cast<uint32_t>(target_w), static_cast<uint32_t>(src->width * letterbox.scale)));
    const uint32_t scaled_h = std::max<uint32_t>(1, std::min<uint32_t>(
        static_cast<uint32_t>(target_h), static_cast<uint32_t>(src->height * letterbox.scale)));
    const uint32_t offset_x = std::min<uint32_t>(
        static_cast<uint32_t>(target_w) - scaled_w, static_cast<uint32_t>(letterbox.pad_x));
    const uint32_t offset_y = std::min<uint32_t>(
        static_cast<uint32_t>(target_h) - scaled_h, static_cast<uint32_t>(letterbox.pad_y));

    av_image_view output{};
    if (image_ops->alloc(image_ops->ctx, static_cast<uint32_t>(target_w), static_cast<uint32_t>(target_h),
                         AV_PIX_BGRA, &output) != AV_OK) {
        return nullptr;
    }
    av_image_view scaled{};
    if (image_ops->alloc(image_ops->ctx, scaled_w, scaled_h, AV_PIX_BGRA, &scaled) != AV_OK) {
        image_ops->free(image_ops->ctx, &output);
        return nullptr;
    }

    const uint8_t padding[4] = {114, 114, 114, 255};
    const bool converted = image_ops->convert(image_ops->ctx, src, nullptr, &scaled, 0) == AV_OK;
    const bool padded = converted && image_ops->pad(image_ops->ctx, &output, nullptr, padding) == AV_OK;
    const bool copied = padded && copy_scaled_bgra(scaled, output, offset_x, offset_y);
    image_ops->free(image_ops->ctx, &scaled);
    if (!copied || output.opaque_kind != AV_OPAQUE_CVPIXELBUFFER || !output.opaque) {
        image_ops->free(image_ops->ctx, &output);
        return nullptr;
    }
    void* retained_buffer = output.opaque;
    CVPixelBufferRetain(static_cast<CVPixelBufferRef>(output.opaque));
    image_ops->free(image_ops->ctx, &output);
    return retained_buffer;
}

} // namespace

void* Preprocessor::create_input_pixelbuffer(const av_frame_desc* src, const av_image_ops* image_ops,
                                             int target_w, int target_h) {
    if (image_ops) return create_input_pixelbuffer_with_ops(src, image_ops, target_w, target_h);
    if (!src || target_w <= 0 || target_h <= 0 || src->pixel_format != AV_PIX_NV12 ||
        src->plane_count != 2 || src->opaque_kind != AV_OPAQUE_CVPIXELBUFFER || !src->opaque) {
        return nullptr;
    }
    if (src->color_primaries != AV_COLOR_PRIM_UNSPECIFIED && src->color_primaries != AV_COLOR_PRIM_BT709) {
        return nullptr;
    }
    if (src->color_transfer != AV_COLOR_TRC_UNSPECIFIED && src->color_transfer != AV_COLOR_TRC_BT709) {
        return nullptr;
    }
    if (!select_matrix(src)) return nullptr;

    @autoreleasepool {
        NSDictionary* options = @{
            (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
            (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
        };

        CVPixelBufferRef dst_pb = nullptr;
        const CVReturn create_status = CVPixelBufferCreate(
            kCFAllocatorDefault,
            static_cast<size_t>(target_w),
            static_cast<size_t>(target_h),
            kCVPixelFormatType_32BGRA,
            (__bridge CFDictionaryRef)options,
            &dst_pb
        );
        if (create_status != kCVReturnSuccess || !dst_pb) return nullptr;

        CVPixelBufferRef src_pb = static_cast<CVPixelBufferRef>(src->opaque);
        const OSType src_format = CVPixelBufferGetPixelFormatType(src_pb);
        if (src_format != kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange &&
            src_format != kCVPixelFormatType_420YpCbCr8BiPlanarFullRange) {
            CVPixelBufferRelease(dst_pb);
            return nullptr;
        }
        const bool source_locked = CVPixelBufferLockBaseAddress(src_pb, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess;
        const bool target_locked = source_locked && CVPixelBufferLockBaseAddress(dst_pb, 0) == kCVReturnSuccess;
        if (!target_locked) {
            if (source_locked) CVPixelBufferUnlockBaseAddress(src_pb, kCVPixelBufferLock_ReadOnly);
            CVPixelBufferRelease(dst_pb);
            return nullptr;
        }

        const size_t src_w = CVPixelBufferGetWidth(src_pb);
        const size_t src_h = CVPixelBufferGetHeight(src_pb);
        const size_t dst_stride = CVPixelBufferGetBytesPerRow(dst_pb);
        uint8_t* dst_ptr = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(dst_pb));
        const auto letterbox = aivision::cv::compute_letterbox(
            static_cast<uint32_t>(src_w), static_cast<uint32_t>(src_h),
            static_cast<uint32_t>(target_w), static_cast<uint32_t>(target_h));
        const uint32_t scaled_w = std::min<uint32_t>(target_w, static_cast<uint32_t>(src_w * letterbox.scale));
        const uint32_t scaled_h = std::min<uint32_t>(target_h, static_cast<uint32_t>(src_h * letterbox.scale));
        const uint32_t offset_x = std::min<uint32_t>(target_w - scaled_w, static_cast<uint32_t>(letterbox.pad_x));
        const uint32_t offset_y = std::min<uint32_t>(target_h - scaled_h, static_cast<uint32_t>(letterbox.pad_y));

        for (int y = 0; y < target_h; ++y) {
            uint32_t* row = reinterpret_cast<uint32_t*>(dst_ptr + static_cast<size_t>(y) * dst_stride);
            for (int x = 0; x < target_w; ++x) row[x] = 0xFF727272;
        }

        const size_t y_width = CVPixelBufferGetWidthOfPlane(src_pb, 0);
        const size_t y_height = CVPixelBufferGetHeightOfPlane(src_pb, 0);
        const size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(src_pb, 0);
        const size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(src_pb, 1);
        vImage_Buffer y_buffer{
            CVPixelBufferGetBaseAddressOfPlane(src_pb, 0), y_height, y_width, y_stride};
        vImage_Buffer uv_buffer{
            CVPixelBufferGetBaseAddressOfPlane(src_pb, 1), y_height / 2, y_width / 2, uv_stride};
        vImage_Buffer argb_full{};
        vImage_Buffer bgra_full{};
        vImage_YpCbCrToARGB conversion{};
        const vImage_YpCbCrPixelRange pixel_range = select_range(src->color_range);
        const vImage_Error init_argb = vImageBuffer_Init(&argb_full, y_height, y_width, 32, kvImageNoFlags);
        const vImage_Error init_bgra = vImageBuffer_Init(&bgra_full, y_height, y_width, 32, kvImageNoFlags);
        vImage_Error conversion_error = kvImageNoError;
        if (init_argb != kvImageNoError) {
            conversion_error = init_argb;
        } else if (init_bgra != kvImageNoError) {
            conversion_error = init_bgra;
        } else {
            conversion_error = vImageConvert_YpCbCrToARGB_GenerateConversion(
                select_matrix(src), &pixel_range, &conversion, kvImage420Yp8_CbCr8,
                kvImageARGB8888, kvImageNoFlags);
        }
        vImage_Error convert_error = conversion_error;
        if (convert_error == kvImageNoError) {
            convert_error = vImageConvert_420Yp8_CbCr8ToARGB8888(
                &y_buffer, &uv_buffer, &argb_full, &conversion, nullptr, 255, kvImageNoFlags);
        }
        const uint8_t permute_map[4] = {3, 2, 1, 0};
        const vImage_Error permute_error = convert_error == kvImageNoError
            ? vImagePermuteChannels_ARGB8888(&argb_full, &bgra_full, permute_map, kvImageNoFlags)
            : convert_error;

        vImage_Error scale_error = permute_error;
        if (permute_error == kvImageNoError) {
            uint8_t* dst_roi = dst_ptr + static_cast<size_t>(offset_y) * dst_stride + offset_x * 4;
            vImage_Buffer dst_roi_buffer{dst_roi, scaled_h, scaled_w, dst_stride};
            scale_error = vImageScale_ARGB8888(&bgra_full, &dst_roi_buffer, nullptr, kvImageHighQualityResampling);
        }

        release_buffer(&argb_full);
        release_buffer(&bgra_full);
        CVPixelBufferUnlockBaseAddress(dst_pb, 0);
        CVPixelBufferUnlockBaseAddress(src_pb, kCVPixelBufferLock_ReadOnly);
        if (scale_error != kvImageNoError) {
            CVPixelBufferRelease(dst_pb);
            return nullptr;
        }
        return dst_pb;
    }
}

void Preprocessor::release_pixelbuffer(void* pb) {
    if (pb) CVPixelBufferRelease(static_cast<CVPixelBufferRef>(pb));
}

} // namespace yolov8n
