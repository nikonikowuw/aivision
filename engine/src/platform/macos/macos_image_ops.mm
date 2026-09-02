/**
 * @file macos_image_ops.mm
 * @brief macOS 图像原语处理器实现
 * 
 * 基于 Apple Accelerate 框架（vImage）与 CoreImage/ImageIO：
 * 1. 内存管理：CVPixelBuffer 分配与释放；
 * 2. 图像转换：NV12 / BGRA 颜色空间转换、ROI 裁剪、高质量缩放（vImageScale）；
 * 3. 模型预处理：保持宽高比 Letterbox 填充；
 * 4. 告警抓拍：ROI 裁剪并高质量编码为 JPEG 字节流（CGImageDestination）。
 */

#import "argus/platform/macos_platform.hpp"

#import <Accelerate/Accelerate.h>
#import <CoreImage/CoreImage.h>
#import <CoreVideo/CoreVideo.h>
#import <ImageIO/ImageIO.h>


#include <algorithm>
#include <cmath>
#include <cstring>

namespace argus::platform {
namespace {

CVPixelBufferRef pixel_buffer_from_view(const av_image_view* view) {
    return view ? static_cast<CVPixelBufferRef>(view->opaque) : nullptr;
}

bool normalized_roi(const av_rect* roi, float& x, float& y, float& width, float& height) {
    if (!roi) {
        x = 0.0f;
        y = 0.0f;
        width = 1.0f;
        height = 1.0f;
        return true;
    }
    x = roi->x;
    y = roi->y;
    width = roi->width;
    height = roi->height;
    return x >= 0.0f && y >= 0.0f && width > 0.0f && height > 0.0f &&
           x + width <= 1.0f && y + height <= 1.0f;
}

int macos_alloc(void*, uint32_t width, uint32_t height, uint32_t pixel_format, av_image_view* out) {
    if (!out || width == 0 || height == 0) return AV_ERR_INVALID_ARG;
    OSType cv_format = 0;
    uint32_t planes = 1;
    if (pixel_format == AV_PIX_BGRA) cv_format = kCVPixelFormatType_32BGRA;
    else if (pixel_format == AV_PIX_NV12) {
        cv_format = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
        planes = 2;
    } else return AV_ERR_NOT_IMPLEMENTED;

    CVPixelBufferRef buffer = nullptr;
    NSDictionary* options = @{
        (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
        (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
    };
    const CVReturn status = CVPixelBufferCreate(kCFAllocatorDefault, width, height, cv_format,
                                                  (__bridge CFDictionaryRef)options, &buffer);
    if (status != kCVReturnSuccess || !buffer) return AV_ERR_OUT_OF_MEMORY;

    *out = {};
    out->size = sizeof(av_image_view);
    out->api_version = AV_ALGO_API_VERSION;
    out->width = width;
    out->height = height;
    out->pixel_format = pixel_format;
    out->memory_type = AV_MEM_PLATFORM_SURFACE;
    out->plane_count = planes;
    out->opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    out->opaque = buffer;
    for (uint32_t plane = 0; plane < planes; ++plane) {
        out->stride[plane] = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(buffer, plane));
        out->offset[plane] = 0;
    }
    if (planes == 1) out->stride[0] = static_cast<int32_t>(CVPixelBufferGetBytesPerRow(buffer));
    return AV_OK;
}

int macos_free(void*, av_image_view* image) {
    if (!image) return AV_ERR_INVALID_ARG;
    if (image->opaque) CVPixelBufferRelease(pixel_buffer_from_view(image));
    *image = {};
    return AV_OK;
}

// 基于 Apple vImage 库执行 NV12/BGRA 转换、ROI 裁剪与高质量重采样缩放
int macos_convert(void*, const av_frame_desc* src, const av_rect* src_roi,
                  const av_image_view* dst, uint32_t) {
    if (!src || !src->opaque || !dst || !dst->opaque) return AV_ERR_INVALID_ARG;
    if (dst->pixel_format != AV_PIX_BGRA) return AV_ERR_NOT_IMPLEMENTED;
    float roi_x, roi_y, roi_w, roi_h;
    if (!normalized_roi(src_roi, roi_x, roi_y, roi_w, roi_h)) return AV_ERR_INVALID_ARG;

    auto source = static_cast<CVPixelBufferRef>(src->opaque);
    auto target = pixel_buffer_from_view(dst);
    const CVReturn source_lock = CVPixelBufferLockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
    if (source_lock != kCVReturnSuccess) return AV_ERR_INTERNAL;
    const CVReturn target_lock = CVPixelBufferLockBaseAddress(target, 0);
    if (target_lock != kCVReturnSuccess) {
        CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
        return AV_ERR_INTERNAL;
    }

    const size_t src_width = CVPixelBufferGetWidth(source);
    const size_t src_height = CVPixelBufferGetHeight(source);
    const size_t dst_width = CVPixelBufferGetWidth(target);
    const size_t dst_height = CVPixelBufferGetHeight(target);
    const size_t crop_x = std::min<size_t>(src_width - 1, static_cast<size_t>(roi_x * src_width));
    const size_t crop_y = std::min<size_t>(src_height - 1, static_cast<size_t>(roi_y * src_height));
    const size_t crop_w = std::max<size_t>(1, std::min<size_t>(src_width - crop_x,
                                                               static_cast<size_t>(roi_w * src_width)));
    const size_t crop_h = std::max<size_t>(1, std::min<size_t>(src_height - crop_y,
                                                               static_cast<size_t>(roi_h * src_height)));

    vImage_Error error = kvImageNoError;
    vImage_Buffer destination{
        CVPixelBufferGetBaseAddress(target), dst_height, dst_width, CVPixelBufferGetBytesPerRow(target)
    };
    if (src->pixel_format == AV_PIX_BGRA) {
        // BGRA 直接裁剪并缩放
        const auto* base = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(source));
        vImage_Buffer cropped{
            const_cast<uint8_t*>(base) + crop_y * CVPixelBufferGetBytesPerRow(source) + crop_x * 4,
            crop_h, crop_w, CVPixelBufferGetBytesPerRow(source)
        };
        error = vImageScale_ARGB8888(&cropped, &destination, nullptr, kvImageHighQualityResampling);
    } else if (src->pixel_format == AV_PIX_NV12) {
        // NV12 双平面转 ARGB/BGRA（根据色彩矩阵设置 BT.709 或 BT.2020 转换参数）
        vImage_Buffer y_buffer{
            CVPixelBufferGetBaseAddressOfPlane(source, 0),
            CVPixelBufferGetHeightOfPlane(source, 0),
            CVPixelBufferGetWidthOfPlane(source, 0),
            CVPixelBufferGetBytesPerRowOfPlane(source, 0)
        };
        vImage_Buffer uv_buffer{
            CVPixelBufferGetBaseAddressOfPlane(source, 1),
            CVPixelBufferGetHeightOfPlane(source, 1),
            CVPixelBufferGetWidthOfPlane(source, 1),
            CVPixelBufferGetBytesPerRowOfPlane(source, 1)
        };
        vImage_Buffer argb{};
        vImage_Buffer bgra{};
        if (vImageBuffer_Init(&argb, src_height, src_width, 32, kvImageNoFlags) != kvImageNoError ||
            vImageBuffer_Init(&bgra, src_height, src_width, 32, kvImageNoFlags) != kvImageNoError) {
            if (argb.data) free(argb.data);
            if (bgra.data) free(bgra.data);
            CVPixelBufferUnlockBaseAddress(target, 0);
            CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
            return AV_ERR_OUT_OF_MEMORY;
        }
        vImage_YpCbCrToARGBMatrix matrix_values{};
        vImage_YpCbCrToARGB conversion{};
        const vImage_YpCbCrToARGBMatrix* matrix_source = kvImage_YpCbCrToARGBMatrix_ITU_R_709_2;
        if (src->color_matrix == AV_COLOR_MAT_BT2020_NCL) {
            matrix_values.Yp = 1.0f;
            matrix_values.Cb_G = -0.16455f;
            matrix_values.Cb_B = 1.8814f;
            matrix_values.Cr_R = 1.4746f;
            matrix_values.Cr_G = -0.57135f;
            matrix_source = &matrix_values;
        }
        const vImage_YpCbCrPixelRange range = src->color_range == AV_COLOR_RANGE_FULL
            ? vImage_YpCbCrPixelRange{0, 128, 255, 255, 255, 0, 255, 0}
            : vImage_YpCbCrPixelRange{16, 128, 235, 240, 255, 0, 255, 0};
        const vImage_Error conversion_status = vImageConvert_YpCbCrToARGB_GenerateConversion(
            matrix_source, &range, &conversion,
            kvImage420Yp8_CbCr8, kvImageARGB8888, kvImageNoFlags);
        if (conversion_status != kvImageNoError) {
            free(argb.data);
            free(bgra.data);
            CVPixelBufferUnlockBaseAddress(target, 0);
            CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
            return AV_ERR_INTERNAL;
        }
        error = vImageConvert_420Yp8_CbCr8ToARGB8888(
            &y_buffer, &uv_buffer, &argb, &conversion, nullptr, 255, kvImageNoFlags);
        const uint8_t permutation[4] = {3, 2, 1, 0};
        if (error == kvImageNoError) error = vImagePermuteChannels_ARGB8888(&argb, &bgra, permutation, kvImageNoFlags);
        if (error == kvImageNoError) {
            vImage_Buffer cropped{
                static_cast<uint8_t*>(bgra.data) + crop_y * bgra.rowBytes + crop_x * 4,
                crop_h, crop_w, bgra.rowBytes
            };
            error = vImageScale_ARGB8888(&cropped, &destination, nullptr, kvImageHighQualityResampling);
        }
        free(argb.data);
        free(bgra.data);
    } else {
        error = kvImageUnsupportedConversion;
    }

    CVPixelBufferUnlockBaseAddress(target, 0);
    CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
    return error == kvImageNoError ? AV_OK : AV_ERR_INTERNAL;
}

int macos_pad(void*, const av_image_view* dst, const av_rect* region, const uint8_t value[4]) {
    if (!dst || !dst->opaque || !value || dst->pixel_format != AV_PIX_BGRA) return AV_ERR_INVALID_ARG;
    float x, y, width, height;
    if (!normalized_roi(region, x, y, width, height)) return AV_ERR_INVALID_ARG;
    auto buffer = pixel_buffer_from_view(dst);
    if (CVPixelBufferLockBaseAddress(buffer, 0) != kCVReturnSuccess) return AV_ERR_INTERNAL;
    auto* base = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(buffer));
    const uint32_t left = static_cast<uint32_t>(x * dst->width);
    const uint32_t top = static_cast<uint32_t>(y * dst->height);
    const uint32_t right = std::min(dst->width, static_cast<uint32_t>((x + width) * dst->width));
    const uint32_t bottom = std::min(dst->height, static_cast<uint32_t>((y + height) * dst->height));
    const size_t stride = CVPixelBufferGetBytesPerRow(buffer);
    for (uint32_t row = top; row < bottom; ++row) {
        auto* pixels = base + row * stride;
        for (uint32_t col = left; col < right; ++col) std::memcpy(pixels + col * 4, value, 4);
    }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    return AV_OK;
}

static av_image_ops g_macos_c_image_ops{
    sizeof(av_image_ops), AV_ALGO_API_VERSION, nullptr,
    macos_convert, macos_pad, macos_alloc, macos_free
};

class MacosImageProcessor final : public IImageProcessor {
public:
    av_status resize(const av_frame_desc* src, av_image_view* dst) override {
        return static_cast<av_status>(macos_convert(nullptr, src, nullptr, dst, 0));
    }

    av_status letterbox(const av_frame_desc* src, av_image_view* dst, float* pad_w, float* pad_h, float* scale) override {
        if (!src || !dst || !pad_w || !pad_h || !scale || src->width == 0 || src->height == 0 ||
            dst->pixel_format != AV_PIX_BGRA) return AV_ERR_INVALID_ARG;
        const float ratio = std::min(static_cast<float>(dst->width) / src->width,
                                     static_cast<float>(dst->height) / src->height);
        const uint32_t scaled_width = std::min(dst->width,
            std::max<uint32_t>(1, static_cast<uint32_t>(std::lround(src->width * ratio))));
        const uint32_t scaled_height = std::min(dst->height,
            std::max<uint32_t>(1, static_cast<uint32_t>(std::lround(src->height * ratio))));
        *scale = ratio;
        *pad_w = (static_cast<float>(dst->width) - scaled_width) * 0.5f;
        *pad_h = (static_cast<float>(dst->height) - scaled_height) * 0.5f;

        av_image_view resized{};
        const av_status alloc_status = static_cast<av_status>(macos_alloc(nullptr, scaled_width, scaled_height,
                                                                           AV_PIX_BGRA, &resized));
        if (alloc_status != AV_OK) return alloc_status;
        const av_status resize_status = resize(src, &resized);
        if (resize_status != AV_OK) {
            macos_free(nullptr, &resized);
            return resize_status;
        }
        const uint8_t background[4] = {114, 114, 114, 255};
        const av_status pad_status = static_cast<av_status>(macos_pad(nullptr, dst, nullptr, background));
        if (pad_status != AV_OK) {
            macos_free(nullptr, &resized);
            return pad_status;
        }

        auto source = pixel_buffer_from_view(&resized);
        auto target = pixel_buffer_from_view(dst);
        const CVReturn source_lock = CVPixelBufferLockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
        const CVReturn target_lock = source_lock == kCVReturnSuccess
            ? CVPixelBufferLockBaseAddress(target, 0) : kCVReturnError;
        av_status copy_status = AV_OK;
        if (target_lock != kCVReturnSuccess) {
            copy_status = AV_ERR_INTERNAL;
        } else {
            vImage_Buffer source_buffer{
                CVPixelBufferGetBaseAddress(source), scaled_height, scaled_width,
                CVPixelBufferGetBytesPerRow(source)
            };
            auto* target_base = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(target));
            vImage_Buffer target_region{
                target_base + static_cast<size_t>(std::lround(*pad_h)) * CVPixelBufferGetBytesPerRow(target) +
                    static_cast<size_t>(std::lround(*pad_w)) * 4,
                scaled_height, scaled_width, CVPixelBufferGetBytesPerRow(target)
            };
            if (vImageCopyBuffer(&source_buffer, &target_region, 4, kvImageNoFlags) != kvImageNoError) {
                copy_status = AV_ERR_INTERNAL;
            }
            CVPixelBufferUnlockBaseAddress(target, 0);
        }
        if (source_lock == kCVReturnSuccess) CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
        macos_free(nullptr, &resized);
        return copy_status;
    }

    av_status convert_color(const av_image_view* src, av_image_view* dst) override {
        if (!src || !dst || !src->opaque || !dst->opaque) return AV_ERR_INVALID_ARG;
        av_frame_desc frame{};
        frame.size = sizeof(frame);
        frame.api_version = AV_ALGO_API_VERSION;
        frame.opaque = src->opaque;
        frame.opaque_kind = src->opaque_kind;
        frame.pixel_format = src->pixel_format;
        frame.width = src->width;
        frame.height = src->height;
        frame.plane_count = src->plane_count;
        frame.color_primaries = AV_COLOR_PRIM_BT709;
        frame.color_transfer = AV_COLOR_TRC_BT709;
        frame.color_matrix = AV_COLOR_MAT_BT709;
        frame.color_range = AV_COLOR_RANGE_LIMITED;
        return static_cast<av_status>(macos_convert(nullptr, &frame, nullptr, dst, 0));
    }

    av_status encode_jpeg(const av_frame_desc* src, const av_rect* crop_roi, int quality,
                          std::vector<uint8_t>& out_jpeg) override {
        if (!src || !src->opaque || src->opaque_kind != AV_OPAQUE_CVPIXELBUFFER) return AV_ERR_INVALID_ARG;
        @autoreleasepool {
            auto buffer = static_cast<CVPixelBufferRef>(src->opaque);
            CIImage* input = [CIImage imageWithCVPixelBuffer:buffer];
            if (!input) return AV_ERR_INTERNAL;
            CGRect rect = input.extent;
            if (crop_roi) {
                float x, y, width, height;
                if (!normalized_roi(crop_roi, x, y, width, height)) return AV_ERR_INVALID_ARG;
                const CGFloat origin_x = std::round(input.extent.origin.x + static_cast<double>(x) * input.extent.size.width);
                const CGFloat origin_y = std::round(input.extent.origin.y + (1.0 - static_cast<double>(y) - static_cast<double>(height)) * input.extent.size.height);
                const CGFloat crop_w = std::round(static_cast<double>(width) * input.extent.size.width);
                const CGFloat crop_h = std::round(static_cast<double>(height) * input.extent.size.height);
                rect = CGRectMake(origin_x, origin_y, crop_w, crop_h);
            }
            CIContext* context = [CIContext contextWithOptions:nil];
            CGImageRef image = [context createCGImage:input fromRect:rect];
            if (!image) return AV_ERR_INTERNAL;
            CFMutableDataRef data = CFDataCreateMutable(kCFAllocatorDefault, 0);
            CGImageDestinationRef destination = CGImageDestinationCreateWithData(data, CFSTR("public.jpeg"), 1, nullptr);
            if (!destination) {
                CGImageRelease(image);
                CFRelease(data);
                return AV_ERR_INTERNAL;
            }
            const float compression = std::clamp(quality, 1, 100) / 100.0f;
            CFNumberRef number = CFNumberCreate(kCFAllocatorDefault, kCFNumberFloatType, &compression);
            const void* keys[] = {kCGImageDestinationLossyCompressionQuality};
            const void* values[] = {number};
            CFDictionaryRef options = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
                                                         &kCFTypeDictionaryKeyCallBacks,
                                                         &kCFTypeDictionaryValueCallBacks);
            CGImageDestinationAddImage(destination, image, options);
            const bool finalized = CGImageDestinationFinalize(destination);
            if (finalized) {
                const auto* bytes = CFDataGetBytePtr(data);
                out_jpeg.assign(bytes, bytes + CFDataGetLength(data));
            }
            CFRelease(options);
            CFRelease(number);
            CFRelease(destination);
            CFRelease(data);
            CGImageRelease(image);
            return finalized ? AV_OK : AV_ERR_INTERNAL;
        }
    }
    av_status encode_thumbnail_jpeg(const av_frame_desc* src, const av_rect* crop_roi, int max_width, int quality,
                                    std::vector<uint8_t>& out_jpeg) override {
        if (!src || !src->opaque || src->opaque_kind != AV_OPAQUE_CVPIXELBUFFER) return AV_ERR_INVALID_ARG;
        @autoreleasepool {
            auto buffer = static_cast<CVPixelBufferRef>(src->opaque);
            CIImage* input = [CIImage imageWithCVPixelBuffer:buffer];
            if (!input) return AV_ERR_INTERNAL;

            // 如果指定了 crop_roi，先裁剪出目标区域
            CGRect crop_rect = input.extent;
            if (crop_roi) {
                float x, y, width, height;
                if (!normalized_roi(crop_roi, x, y, width, height)) return AV_ERR_INVALID_ARG;
                const CGFloat origin_x = std::round(input.extent.origin.x + static_cast<double>(x) * input.extent.size.width);
                const CGFloat origin_y = std::round(input.extent.origin.y + (1.0 - static_cast<double>(y) - static_cast<double>(height)) * input.extent.size.height);
                const CGFloat crop_w = std::round(static_cast<double>(width) * input.extent.size.width);
                const CGFloat crop_h = std::round(static_cast<double>(height) * input.extent.size.height);
                crop_rect = CGRectMake(origin_x, origin_y, crop_w, crop_h);
                input = [input imageByCroppingToRect:crop_rect];
            }

            // 依据指定最大宽度（默认 360px）使用 CoreImage / GPU 进行高保真等比降采样
            const float orig_w = static_cast<float>(crop_rect.size.width);
            if (orig_w > static_cast<float>(max_width) && max_width > 0) {
                const float scale = static_cast<float>(max_width) / orig_w;
                CGAffineTransform transform = CGAffineTransformMakeTranslation(-crop_rect.origin.x, -crop_rect.origin.y);
                transform = CGAffineTransformScale(transform, scale, scale);
                input = [input imageByApplyingTransform:transform];
            }

            CIContext* context = [CIContext contextWithOptions:nil];
            CGImageRef image = [context createCGImage:input fromRect:input.extent];
            if (!image) return AV_ERR_INTERNAL;
            CFMutableDataRef data = CFDataCreateMutable(kCFAllocatorDefault, 0);
            CGImageDestinationRef destination = CGImageDestinationCreateWithData(data, CFSTR("public.jpeg"), 1, nullptr);
            if (!destination) {
                CGImageRelease(image);
                CFRelease(data);
                return AV_ERR_INTERNAL;
            }
            const float compression = std::clamp(quality, 1, 100) / 100.0f;
            CFNumberRef number = CFNumberCreate(kCFAllocatorDefault, kCFNumberFloatType, &compression);
            const void* keys[] = {kCGImageDestinationLossyCompressionQuality};
            const void* values[] = {number};
            CFDictionaryRef options = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
                                                         &kCFTypeDictionaryKeyCallBacks,
                                                         &kCFTypeDictionaryValueCallBacks);
            CGImageDestinationAddImage(destination, image, options);
            const bool finalized = CGImageDestinationFinalize(destination);
            if (finalized) {
                const auto* bytes = CFDataGetBytePtr(data);
                out_jpeg.assign(bytes, bytes + CFDataGetLength(data));
            }
            CFRelease(options);
            CFRelease(number);
            CFRelease(destination);
            CFRelease(data);
            CGImageRelease(image);
            return finalized ? AV_OK : AV_ERR_INTERNAL;
        }
    }
};

static MacosImageProcessor g_macos_image_processor;

} // namespace

IImageProcessor* MacosPlatformAdapter::get_image_processor() {
    return &g_macos_image_processor;
}

const av_image_ops* MacosPlatformAdapter::get_c_image_ops() {
    return &g_macos_c_image_ops;
}

} // namespace argus::platform
