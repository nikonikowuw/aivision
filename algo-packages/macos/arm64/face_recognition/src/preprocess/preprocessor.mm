/**
 * @file preprocessor.mm
 * @brief NV12/CVPixelBuffer -> 原图 RGB -> 640x640 Letterbox 及五点相似变换对齐实现
 */

#include "preprocessor.hpp"

#import <CoreVideo/CoreVideo.h>
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

} // namespace

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

    out.original_rgb.width = width;
    out.original_rgb.height = height;
    out.original_rgb.channels = 3;
    out.original_rgb.data.resize(static_cast<size_t>(width) * height * 3);

    const uint8_t* y_plane = nullptr;
    const uint8_t* uv_plane = nullptr;
    int32_t y_stride = frame->stride[0];
    int32_t uv_stride = frame->stride[1];

    CVPixelBufferRef pixel_buffer = nullptr;
    if (frame->opaque != nullptr) {
        pixel_buffer = static_cast<CVPixelBufferRef>(frame->opaque);
        CVPixelBufferLockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        y_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 0));
        uv_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 1));
        y_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 0));
        uv_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 1));
    }

    if (!y_plane || !uv_plane) {
        if (pixel_buffer) CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        error = "missing NV12 buffer data planes";
        return false;
    }

    // NV12 to RGB24 conversion using vImage
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
    vImage_Buffer dst_rgb = {
        .data = out.original_rgb.data.data(),
        .height = height,
        .width = width,
        .rowBytes = static_cast<size_t>(width) * 3
    };

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

    std::vector<uint8_t> temp_argb(static_cast<size_t>(width) * height * 4);
    vImage_Buffer dst_argb = {
        .data = temp_argb.data(),
        .height = height,
        .width = width,
        .rowBytes = static_cast<size_t>(width) * 4
    };

    vImageConvert_420Yp8_CbCr8ToARGB8888(&src_y, &src_uv, &dst_argb, &matrix, nullptr, 255, kvImageNoFlags);

    if (pixel_buffer) {
        CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
    }

    // ARGB to RGB for original image
    vImageConvert_ARGB8888toRGB888(&dst_argb, &dst_rgb, kvImageNoFlags);

    // Letterbox to 640x640
    out.letterbox_info = argus::cv::compute_letterbox(width, height, 640, 640);
    out.letterbox_rgb.width = 640;
    out.letterbox_rgb.height = 640;
    out.letterbox_rgb.channels = 3;
    out.letterbox_rgb.data.assign(640 * 640 * 3, 114); // 114 pad color

    uint32_t nw = static_cast<uint32_t>(std::round(static_cast<float>(width) * out.letterbox_info.scale));
    uint32_t nh = static_cast<uint32_t>(std::round(static_cast<float>(height) * out.letterbox_info.scale));
    uint32_t pad_x = static_cast<uint32_t>(std::round(out.letterbox_info.pad_x));
    uint32_t pad_y = static_cast<uint32_t>(std::round(out.letterbox_info.pad_y));

    std::vector<uint8_t> scaled_argb(static_cast<size_t>(nw) * nh * 4);
    vImage_Buffer scaled_argb_buf = {
        .data = scaled_argb.data(),
        .height = nh,
        .width = nw,
        .rowBytes = static_cast<size_t>(nw * 4)
    };

    vImageScale_ARGB8888(&dst_argb, &scaled_argb_buf, nullptr, kvImageHighQualityResampling);

    vImage_Buffer roi_rgb_buf = {
        .data = out.letterbox_rgb.data.data() + (pad_y * 640 + pad_x) * 3,
        .height = nh,
        .width = nw,
        .rowBytes = 640 * 3
    };

    vImageConvert_ARGB8888toRGB888(&scaled_argb_buf, &roi_rgb_buf, kvImageNoFlags);

    return true;
}

bool Preprocessor::align_face_112x112(const ImageBuffer& orig_rgb, const float landmarks_10[10],
                                     ImageBuffer& out_face_112, std::string& error) {
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

    return true;
}

} // namespace face_recognition
