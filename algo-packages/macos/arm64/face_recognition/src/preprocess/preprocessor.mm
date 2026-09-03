/**
 * @file preprocessor.mm
 * @brief NV12/CVPixelBuffer -> 原图 RGB -> 640x384 Letterbox 及五点相似变换对齐实现
 */

#include "preprocessor.hpp"
#include "core/profile_stats.hpp"

#import <CoreVideo/CoreVideo.h>
#import <VideoToolbox/VideoToolbox.h>
#import <Accelerate/Accelerate.h>
#include <algorithm>
#include <cmath>
#include <cstring>

namespace face_recognition {

namespace {

// InsightFace 标准 112x112 五点参考模板
constexpr float kArcFaceSrc[5][2] = {
    {38.2946f, 51.6963f},
    {73.5318f, 51.5014f},
    {56.0252f, 71.7366f},
    {41.5493f, 92.3655f},
    {70.7299f, 92.2041f}
};

/**
 * @brief Umeyama 算法求解 2D 相似变换矩阵 M (2x3)，使 src -> dst
 */
bool compute_similarity_transform(const float src[5][2], const float dst[5][2], float M[2][3]) {
    float mu_src_x = 0.0f, mu_src_y = 0.0f;
    float mu_dst_x = 0.0f, mu_dst_y = 0.0f;
    for (int i = 0; i < 5; ++i) {
        mu_src_x += src[i][0];
        mu_src_y += src[i][1];
        mu_dst_x += dst[i][0];
        mu_dst_y += dst[i][1];
    }
    mu_src_x /= 5.0f; mu_src_y /= 5.0f;
    mu_dst_x /= 5.0f; mu_dst_y /= 5.0f;

    float sig_src = 0.0f;
    float H00 = 0.0f, H01 = 0.0f, H10 = 0.0f, H11 = 0.0f;

    for (int i = 0; i < 5; ++i) {
        float sx = src[i][0] - mu_src_x;
        float sy = src[i][1] - mu_src_y;
        float dx = dst[i][0] - mu_dst_x;
        float dy = dst[i][1] - mu_dst_y;

        sig_src += sx * sx + sy * sy;
        H00 += dx * sx;
        H01 += dx * sy;
        H10 += dy * sx;
        H11 += dy * sy;
    }
    sig_src /= 5.0f;
    H00 /= 5.0f; H01 /= 5.0f; H10 /= 5.0f; H11 /= 5.0f;

    if (sig_src <= 1e-6f) return false;

    // 2x2 SVD 解析求解
    // H = [H00, H01; H10, H11]
    // det(H) = H00*H11 - H01*H10
    float det_H = H00 * H11 - H01 * H10;

    // 计算 H * H^T 的特征分解得到 U
    float A = H00 * H00 + H01 * H01;
    float B = H10 * H10 + H11 * H11;
    float C = H00 * H10 + H01 * H11;

    float trace = A + B;
    float disc = std::sqrt(std::max(0.0f, (A - B) * (A - B) + 4.0f * C * C));
    float l1 = (trace + disc) * 0.5f;
    float l2 = (trace - disc) * 0.5f;

    float s1 = std::sqrt(std::max(0.0f, l1));
    float s2 = std::sqrt(std::max(0.0f, l2));

    // U 矩阵
    float u00 = 1.0f, u01 = 0.0f, u10 = 0.0f, u11 = 1.0f;
    if (std::abs(C) > 1e-6f || std::abs(l1 - A) > 1e-6f) {
        float vx = l1 - B;
        float vy = C;
        float norm = std::sqrt(vx * vx + vy * vy);
        if (norm > 1e-6f) {
            u00 = vy / norm;
            u10 = vx / norm;
            u01 = -u10;
            u11 = u00;
        }
    }

    // V 矩阵通过 H^T * U 计算
    // s1 * v_1 = H^T * u_1
    float v00 = 1.0f, v01 = 0.0f, v10 = 0.0f, v11 = 1.0f;
    if (s1 > 1e-6f) {
        v00 = (H00 * u00 + H10 * u10) / s1;
        v10 = (H01 * u00 + H11 * u10) / s1;
    }
    if (s2 > 1e-6f) {
        v01 = (H00 * u01 + H10 * u11) / s2;
        v11 = (H01 * u01 + H11 * u11) / s2;
    } else {
        v01 = -v10;
        v11 = v00;
    }

    float det_U = u00 * u11 - u01 * u10;
    float det_V = v00 * v11 - v01 * v10;

    float S11 = 1.0f;
    if (det_U * det_V < 0.0f || det_H < 0.0f) {
        S11 = -1.0f;
    }

    // R = U * S * V^T
    // S * V^T = [v00, v10; S11*v01, S11*v11]
    float sv00 = v00, sv01 = v10;
    float sv10 = S11 * v01, sv11 = S11 * v11;

    float r00 = u00 * sv00 + u01 * sv10;
    float r01 = u00 * sv01 + u01 * sv11;
    float r10 = u10 * sv00 + u11 * sv10;
    float r11 = u10 * sv01 + u11 * sv11;

    float scale = (s1 + S11 * s2) / sig_src;

    float t_x = mu_dst_x - scale * (r00 * mu_src_x + r01 * mu_src_y);
    float t_y = mu_dst_y - scale * (r10 * mu_src_x + r11 * mu_src_y);

    M[0][0] = scale * r00; M[0][1] = scale * r01; M[0][2] = t_x;
    M[1][0] = scale * r10; M[1][1] = scale * r11; M[1][2] = t_y;
    return true;
}

struct PreprocessScratch {
    std::vector<uint8_t> scaled_y;
    std::vector<uint8_t> scaled_uv;
    std::vector<uint8_t> scaled_argb;

    void ensure_size(uint32_t nw, uint32_t nh) {
        size_t y_sz = static_cast<size_t>(nw) * nh;
        size_t uv_sz = static_cast<size_t>(nw) * ((nh + 1) / 2);
        size_t argb_sz = static_cast<size_t>(nw) * nh * 4;
        if (scaled_y.size() < y_sz) scaled_y.resize(y_sz);
        if (scaled_uv.size() < uv_sz) scaled_uv.resize(uv_sz);
        if (scaled_argb.size() < argb_sz) scaled_argb.resize(argb_sz);
    }
};

static thread_local PreprocessScratch t_scratch;

} // namespace

PreprocessResult::~PreprocessResult() {
    if (pixel_buffer) {
        CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        pixel_buffer = nullptr;
    }
}

PreprocessResult::PreprocessResult(PreprocessResult&& other) noexcept
    : orig_width(other.orig_width),
      orig_height(other.orig_height),
      letterbox_rgb(std::move(other.letterbox_rgb)),
      letterbox_info(other.letterbox_info),
      y_plane(other.y_plane),
      uv_plane(other.uv_plane),
      y_stride(other.y_stride),
      uv_stride(other.uv_stride),
      pixel_buffer(other.pixel_buffer) {
    other.pixel_buffer = nullptr;
}

PreprocessResult& PreprocessResult::operator=(PreprocessResult&& other) noexcept {
    if (this != &other) {
        if (pixel_buffer) {
            CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        }
        orig_width = other.orig_width;
        orig_height = other.orig_height;
        letterbox_rgb = std::move(other.letterbox_rgb);
        letterbox_info = other.letterbox_info;
        y_plane = other.y_plane;
        uv_plane = other.uv_plane;
        y_stride = other.y_stride;
        uv_stride = other.uv_stride;
        pixel_buffer = other.pixel_buffer;
        other.pixel_buffer = nullptr;
    }
    return *this;
}

bool Preprocessor::process_frame(const av_frame_desc* frame, PreprocessResult& out, std::string& error) {
    if (!frame) {
        error = "null frame descriptor";
        return false;
    }
    if (frame->pixel_format != AV_PIX_NV12) {
        error = "unsupported pixel format: only NV12 is supported";
        return false;
    }

    uint32_t width = frame->width;
    uint32_t height = frame->height;
    if (width == 0 || height == 0) {
        error = "invalid frame dimensions";
        return false;
    }

    constexpr uint32_t kTargetWidth = 640;
    constexpr uint32_t kTargetHeight = 384;
    out.orig_width = width;
    out.orig_height = height;
    out.letterbox_info = argus::cv::compute_letterbox(width, height, kTargetWidth, kTargetHeight);

    const uint8_t* y_plane = nullptr;
    const uint8_t* uv_plane = nullptr;
    int32_t y_stride = frame->stride[0] > 0 ? frame->stride[0] : static_cast<int32_t>(width);
    int32_t uv_stride = frame->stride[1] > 0 ? frame->stride[1] : static_cast<int32_t>(width);

    CVPixelBufferRef pixel_buffer = nullptr;
    if (frame->opaque != nullptr) {
        if (frame->opaque_kind == AV_OPAQUE_CVPIXELBUFFER || frame->memory_type == AV_MEM_PLATFORM_SURFACE) {
            pixel_buffer = static_cast<CVPixelBufferRef>(frame->opaque);
            CVPixelBufferLockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
            y_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 0));
            uv_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 1));
            y_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 0));
            uv_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 1));
        } else {
            // Host memory contiguous buffer
            y_plane = static_cast<const uint8_t*>(frame->opaque);
            uv_plane = y_plane + static_cast<size_t>(y_stride) * height;
        }
    }

    if (!y_plane || !uv_plane) {
        if (pixel_buffer) CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        error = "missing NV12 buffer data planes";
        return false;
    }

    out.y_plane = y_plane;
    out.uv_plane = uv_plane;
    out.y_stride = y_stride;
    out.uv_stride = uv_stride;
    out.pixel_buffer = pixel_buffer;

#if ENABLE_PROFILING
    auto* prof = get_active_profile_record();
    ARGUS_SIGNPOST_BEGIN("preprocess");
    auto t0_nv12 = std::chrono::steady_clock::now();
#endif

    // Letterbox geometry
    uint32_t nw = static_cast<uint32_t>(std::round(static_cast<float>(width) * out.letterbox_info.scale));
    uint32_t nh = static_cast<uint32_t>(std::round(static_cast<float>(height) * out.letterbox_info.scale));
    uint32_t pad_x = static_cast<uint32_t>(std::round(out.letterbox_info.pad_x));
    uint32_t pad_y = static_cast<uint32_t>(std::round(out.letterbox_info.pad_y));

    // Ensure scratch buffer capacity
    t_scratch.ensure_size(nw, nh);

    vImage_Buffer src_y = {
        .data = const_cast<uint8_t*>(y_plane),
        .height = height,
        .width = width,
        .rowBytes = static_cast<size_t>(y_stride)
    };
    vImage_Buffer src_uv = {
        .data = const_cast<uint8_t*>(uv_plane),
        .height = height / 2,
        .width = width / 2,
        .rowBytes = static_cast<size_t>(uv_stride)
    };

    vImage_Buffer dst_y = {
        .data = t_scratch.scaled_y.data(),
        .height = nh,
        .width = nw,
        .rowBytes = static_cast<size_t>(nw)
    };
    vImage_Buffer dst_uv = {
        .data = t_scratch.scaled_uv.data(),
        .height = nh / 2,
        .width = nw / 2,
        .rowBytes = static_cast<size_t>(nw)
    };

    // 1. Direct Planar8 scaling on Y plane
    vImageScale_Planar8(&src_y, &dst_y, nullptr, kvImageHighQualityResampling);

    // 2. Direct CbCr8 scaling on UV interleaved plane
    vImageScale_CbCr8(&src_uv, &dst_uv, nullptr, kvImageHighQualityResampling);

#if ENABLE_PROFILING
    auto t1_nv12 = std::chrono::steady_clock::now();
    if (prof) {
        prof->nv12_conversion_ms = std::chrono::duration<double, std::milli>(t1_nv12 - t0_nv12).count();
    }
    auto t0_lb = std::chrono::steady_clock::now();
#endif

    // 3. Prepare target letterbox_rgb buffer (640x384, neutral gray 114 padded)
    out.letterbox_rgb.width = kTargetWidth;
    out.letterbox_rgb.height = kTargetHeight;
    out.letterbox_rgb.channels = 3;
    if (out.letterbox_rgb.data.size() != kTargetWidth * kTargetHeight * 3) {
        out.letterbox_rgb.data.resize(kTargetWidth * kTargetHeight * 3);
    }
    std::fill(out.letterbox_rgb.data.begin(), out.letterbox_rgb.data.end(), 114);

    // 4. Color conversion: only convert downsampled (nw x nh) pixels (e.g. 640x360 = 230K pixels, not 8.3M!)
    vImage_YpCbCrToARGB matrix;
    vImage_YpCbCrPixelRange pixel_range = {16, 128, 235, 240, 255, 0, 255, 1};
    vImageConvert_YpCbCrToARGB_GenerateConversion(
        kvImage_YpCbCrToARGBMatrix_ITU_R_709_2,
        &pixel_range,
        &matrix,
        kvImage420Yp8_CbCr8,
        kvImageARGB8888,
        kvImageNoFlags
    );

    vImage_Buffer small_argb = {
        .data = t_scratch.scaled_argb.data(),
        .height = nh,
        .width = nw,
        .rowBytes = static_cast<size_t>(nw * 4)
    };

    vImageConvert_420Yp8_CbCr8ToARGB8888(&dst_y, &dst_uv, &small_argb, &matrix, nullptr, 255, kvImageNoFlags);

    vImage_Buffer roi_rgb_buf = {
        .data = out.letterbox_rgb.data.data() + (pad_y * kTargetWidth + pad_x) * 3,
        .height = nh,
        .width = nw,
        .rowBytes = kTargetWidth * 3
    };

    vImageConvert_ARGB8888toRGB888(&small_argb, &roi_rgb_buf, kvImageNoFlags);

#if ENABLE_PROFILING
    auto t1_lb = std::chrono::steady_clock::now();
    ARGUS_SIGNPOST_END("preprocess");
    if (prof) {
        prof->letterbox_ms = std::chrono::duration<double, std::milli>(t1_lb - t0_lb).count();
    }
#endif

    return true;
}

bool Preprocessor::align_face_112x112(const PreprocessResult& prep_res, const float landmarks_10[10],
                                      ImageBuffer& out_face_112, std::string& error) {
#if ENABLE_PROFILING
    ARGUS_SIGNPOST_BEGIN("alignment");
    auto t0_align = std::chrono::steady_clock::now();
#endif
    if (prep_res.orig_width == 0 || prep_res.orig_height == 0 || !prep_res.y_plane || !prep_res.uv_plane) {
        error = "empty or invalid NV12 input planes in PreprocessResult";
        return false;
    }

    float src[5][2];
    for (int i = 0; i < 5; ++i) {
        src[i][0] = landmarks_10[i * 2];
        src[i][1] = landmarks_10[i * 2 + 1];
    }

    float M[2][3];
    if (!compute_similarity_transform(src, kArcFaceSrc, M)) {
        error = "failed to compute similarity transform";
        return false;
    }

    float a = M[0][0], b = M[0][1], tx = M[0][2];
    float c = M[1][0], d = M[1][1], ty = M[1][2];
    float det = a * d - b * c;
    if (std::abs(det) < 1e-7f) {
        error = "singular similarity matrix";
        return false;
    }

    float inv_a = d / det;
    float inv_b = -b / det;
    float inv_c = -c / det;
    float inv_d = a / det;
    float inv_tx = (b * ty - d * tx) / det;
    float inv_ty = (c * tx - a * ty) / det;

    out_face_112.width = 112;
    out_face_112.height = 112;
    out_face_112.channels = 3;
    out_face_112.data.resize(112 * 112 * 3);

    const uint8_t* y_plane = prep_res.y_plane;
    const uint8_t* uv_plane = prep_res.uv_plane;
    const int32_t y_stride = prep_res.y_stride;
    const int32_t uv_stride = prep_res.uv_stride;
    const int src_w = static_cast<int>(prep_res.orig_width);
    const int src_h = static_cast<int>(prep_res.orig_height);
    const int uv_w = src_w / 2;
    const int uv_h = src_h / 2;

    uint8_t* dst_ptr = out_face_112.data.data();

    // Direct bilinear sampling from NV12 planes with BT.709 conversion
    for (int y = 0; y < 112; ++y) {
        for (int x = 0; x < 112; ++x) {
            float src_x = inv_a * static_cast<float>(x) + inv_b * static_cast<float>(y) + inv_tx;
            float src_y = inv_c * static_cast<float>(x) + inv_d * static_cast<float>(y) + inv_ty;

            src_x = std::clamp(src_x, 0.0f, static_cast<float>(src_w - 1));
            src_y = std::clamp(src_y, 0.0f, static_cast<float>(src_h - 1));

            // Y plane bilinear interpolation
            int x0 = static_cast<int>(std::floor(src_x));
            int y0 = static_cast<int>(std::floor(src_y));
            int x1 = std::min(x0 + 1, src_w - 1);
            int y1 = std::min(y0 + 1, src_h - 1);

            float wx1 = src_x - static_cast<float>(x0);
            float wy1 = src_y - static_cast<float>(y0);
            float wx0 = 1.0f - wx1;
            float wy0 = 1.0f - wy1;

            float y_val = wy0 * (wx0 * y_plane[y0 * y_stride + x0] + wx1 * y_plane[y0 * y_stride + x1]) +
                          wy1 * (wx0 * y_plane[y1 * y_stride + x0] + wx1 * y_plane[y1 * y_stride + x1]);

            // UV plane bilinear interpolation (chroma is 2x2 subsampled)
            float uv_x = src_x * 0.5f;
            float uv_y = src_y * 0.5f;

            int ux0 = static_cast<int>(std::floor(uv_x));
            int uy0 = static_cast<int>(std::floor(uv_y));
            int ux1 = std::min(ux0 + 1, uv_w - 1);
            int uy1 = std::min(uy0 + 1, uv_h - 1);

            float uwx1 = uv_x - static_cast<float>(ux0);
            float uwy1 = uv_y - static_cast<float>(uy0);
            float uwx0 = 1.0f - uwx1;
            float uwy0 = 1.0f - uwy1;

            const uint8_t* p_uv00 = uv_plane + uy0 * uv_stride + ux0 * 2;
            const uint8_t* p_uv01 = uv_plane + uy0 * uv_stride + ux1 * 2;
            const uint8_t* p_uv10 = uv_plane + uy1 * uv_stride + ux0 * 2;
            const uint8_t* p_uv11 = uv_plane + uy1 * uv_stride + ux1 * 2;

            float u_val = uwy0 * (uwx0 * p_uv00[0] + uwx1 * p_uv01[0]) +
                          uwy1 * (uwx0 * p_uv10[0] + uwx1 * p_uv11[0]);
            float v_val = uwy0 * (uwx0 * p_uv00[1] + uwx1 * p_uv01[1]) +
                          uwy1 * (uwx0 * p_uv10[1] + uwx1 * p_uv11[1]);

            // BT.709 standard video range color conversion
            float y_norm = (y_val - 16.0f) * 1.164383f;
            float cb = u_val - 128.0f;
            float cr = v_val - 128.0f;

            float r_val = y_norm + 1.792741f * cr;
            float g_val = y_norm - 0.213249f * cb - 0.532909f * cr;
            float b_val = y_norm + 2.112402f * cb;

            uint8_t* out_pixel = dst_ptr + (y * 112 + x) * 3;
            out_pixel[0] = static_cast<uint8_t>(std::clamp(std::round(r_val), 0.0f, 255.0f));
            out_pixel[1] = static_cast<uint8_t>(std::clamp(std::round(g_val), 0.0f, 255.0f));
            out_pixel[2] = static_cast<uint8_t>(std::clamp(std::round(b_val), 0.0f, 255.0f));
        }
    }

#if ENABLE_PROFILING
    auto t1_align = std::chrono::steady_clock::now();
    ARGUS_SIGNPOST_END("alignment");
    auto* prof = get_active_profile_record();
    if (prof) {
        prof->alignment_ms += std::chrono::duration<double, std::milli>(t1_align - t0_align).count();
    }
#endif

    return true;
}

bool Preprocessor::align_face_112x112(const ImageBuffer& orig_rgb, const float landmarks_10[10],
                                     ImageBuffer& out_face_112, std::string& error) {
#if ENABLE_PROFILING
    ARGUS_SIGNPOST_BEGIN("alignment");
    auto t0_align = std::chrono::steady_clock::now();
#endif
    if (orig_rgb.width == 0 || orig_rgb.height == 0 || orig_rgb.data.empty()) {
        error = "empty original image";
        return false;
    }

    float src[5][2];
    for (int i = 0; i < 5; ++i) {
        src[i][0] = landmarks_10[i * 2];
        src[i][1] = landmarks_10[i * 2 + 1];
    }

    float M[2][3];
    if (!compute_similarity_transform(src, kArcFaceSrc, M)) {
        error = "failed to compute similarity transform";
        return false;
    }

    // Inverse affine transform matrix to sample from original image
    // dst = M * src => src = M_inv * dst
    // M = [a, b, tx; c, d, ty]
    float a = M[0][0], b = M[0][1], tx = M[0][2];
    float c = M[1][0], d = M[1][1], ty = M[1][2];
    float det = a * d - b * c;
    if (std::abs(det) < 1e-7f) {
        error = "singular similarity matrix";
        return false;
    }

    float inv_a = d / det;
    float inv_b = -b / det;
    float inv_c = -c / det;
    float inv_d = a / det;
    float inv_tx = (b * ty - d * tx) / det;
    float inv_ty = (c * tx - a * ty) / det;

    out_face_112.width = 112;
    out_face_112.height = 112;
    out_face_112.channels = 3;
    out_face_112.data.resize(112 * 112 * 3);

    const uint8_t* src_ptr = orig_rgb.data.data();
    uint8_t* dst_ptr = out_face_112.data.data();
    int src_w = static_cast<int>(orig_rgb.width);
    int src_h = static_cast<int>(orig_rgb.height);
    int src_stride = src_w * 3;

    // Bilinear sampling directly from original RGB
    for (int y = 0; y < 112; ++y) {
        for (int x = 0; x < 112; ++x) {
            float src_x = inv_a * static_cast<float>(x) + inv_b * static_cast<float>(y) + inv_tx;
            float src_y = inv_c * static_cast<float>(x) + inv_d * static_cast<float>(y) + inv_ty;

            int x0 = static_cast<int>(std::floor(src_x));
            int y0 = static_cast<int>(std::floor(src_y));
            int x1 = x0 + 1;
            int y1 = y0 + 1;

            float wx1 = src_x - static_cast<float>(x0);
            float wy1 = src_y - static_cast<float>(y0);
            float wx0 = 1.0f - wx1;
            float wy0 = 1.0f - wy1;

            x0 = std::clamp(x0, 0, src_w - 1);
            x1 = std::clamp(x1, 0, src_w - 1);
            y0 = std::clamp(y0, 0, src_h - 1);
            y1 = std::clamp(y1, 0, src_h - 1);

            const uint8_t* p00 = src_ptr + y0 * src_stride + x0 * 3;
            const uint8_t* p01 = src_ptr + y0 * src_stride + x1 * 3;
            const uint8_t* p10 = src_ptr + y1 * src_stride + x0 * 3;
            const uint8_t* p11 = src_ptr + y1 * src_stride + x1 * 3;

            uint8_t* out_pixel = dst_ptr + (y * 112 + x) * 3;
            for (int c_idx = 0; c_idx < 3; ++c_idx) {
                float val = wy0 * (wx0 * p00[c_idx] + wx1 * p01[c_idx]) +
                            wy1 * (wx0 * p10[c_idx] + wx1 * p11[c_idx]);
                out_pixel[c_idx] = static_cast<uint8_t>(std::clamp(std::round(val), 0.0f, 255.0f));
            }
        }
    }

#if ENABLE_PROFILING
    auto t1_align = std::chrono::steady_clock::now();
    ARGUS_SIGNPOST_END("alignment");
    auto* prof = get_active_profile_record();
    if (prof) {
        prof->alignment_ms += std::chrono::duration<double, std::milli>(t1_align - t0_align).count();
    }
#endif

    return true;
}

} // namespace face_recognition
