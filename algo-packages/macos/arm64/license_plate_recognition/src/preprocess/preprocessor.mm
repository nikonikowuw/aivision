/**
 * @file preprocessor.mm
 * @brief NV12/CVPixelBuffer -> RGB 640x384 Letterbox 及 4 点透视变换矫正实现
 */

#include "preprocessor.hpp"

#import <CoreVideo/CoreVideo.h>
#import <Accelerate/Accelerate.h>
#include <algorithm>
#include <cmath>
#include <cstring>
#include <array>

namespace lpr {

namespace {

// 求解 8x8 线性方程组 A x = b (高斯消元法)
bool solve_linear_system_8x8(std::array<std::array<float, 8>, 8>& A,
                             std::array<float, 8>& b,
                             std::array<float, 8>& x) {
    for (int i = 0; i < 8; ++i) {
        int pivot = i;
        float max_val = std::abs(A[i][i]);
        for (int row = i + 1; row < 8; ++row) {
            if (std::abs(A[row][i]) > max_val) {
                max_val = std::abs(A[row][i]);
                pivot = row;
            }
        }
        if (max_val < 1e-7f) return false;

        if (pivot != i) {
            std::swap(A[i], A[pivot]);
            std::swap(b[i], b[pivot]);
        }

        float diag = A[i][i];
        for (int col = i; col < 8; ++col) {
            A[i][col] /= diag;
        }
        b[i] /= diag;

        for (int row = 0; row < 8; ++row) {
            if (row != i) {
                float factor = A[row][i];
                for (int col = i; col < 8; ++col) {
                    A[row][col] -= factor * A[i][col];
                }
                b[row] -= factor * b[i];
            }
        }
    }

    for (int i = 0; i < 8; ++i) {
        x[i] = b[i];
    }
    return true;
}

// 计算从目标矩形 [0, dst_w) x [0, dst_h) 到原图 4 顶点的单应性变换矩阵 H (3x3)
bool compute_homography_dst_to_src(float dst_w, float dst_h,
                                   const float src_pts[4][2],
                                   std::array<float, 9>& H) {
    float dst_pts[4][2] = {
        {0.0f, 0.0f},
        {dst_w, 0.0f},
        {dst_w, dst_h},
        {0.0f, dst_h}
    };

    std::array<std::array<float, 8>, 8> A{};
    std::array<float, 8> b{};

    for (int i = 0; i < 4; ++i) {
        float u = dst_pts[i][0];
        float v = dst_pts[i][1];
        float x = src_pts[i][0];
        float y = src_pts[i][1];

        // u*h0 + v*h1 + h2 - u*x*h6 - v*x*h7 = x
        A[2 * i][0] = u;
        A[2 * i][1] = v;
        A[2 * i][2] = 1.0f;
        A[2 * i][3] = 0.0f;
        A[2 * i][4] = 0.0f;
        A[2 * i][5] = 0.0f;
        A[2 * i][6] = -u * x;
        A[2 * i][7] = -v * x;
        b[2 * i] = x;

        // u*h3 + v*h4 + h5 - u*y*h6 - v*y*h7 = y
        A[2 * i + 1][0] = 0.0f;
        A[2 * i + 1][1] = 0.0f;
        A[2 * i + 1][2] = 0.0f;
        A[2 * i + 1][3] = u;
        A[2 * i + 1][4] = v;
        A[2 * i + 1][5] = 1.0f;
        A[2 * i + 1][6] = -u * y;
        A[2 * i + 1][7] = -v * y;
        b[2 * i + 1] = y;
    }

    std::array<float, 8> x_sol{};
    if (!solve_linear_system_8x8(A, b, x_sol)) return false;

    H[0] = x_sol[0]; H[1] = x_sol[1]; H[2] = x_sol[2];
    H[3] = x_sol[3]; H[4] = x_sol[4]; H[5] = x_sol[5];
    H[6] = x_sol[6]; H[7] = x_sol[7]; H[8] = 1.0f;
    return true;
}

// 目标图像双线性插值采样 (支持 BGR 翻转)
void warp_perspective_bilinear(const ImageBuffer& src,
                               const std::array<float, 9>& H,
                               ImageBuffer& dst,
                               bool swap_rb = false) {
    const uint8_t* s_data = src.data.data();
    uint32_t sw = src.width;
    uint32_t sh = src.height;
    uint32_t dw = dst.width;
    uint32_t dh = dst.height;
    uint8_t* d_data = dst.data.data();

    for (uint32_t v = 0; v < dh; ++v) {
        for (uint32_t u = 0; u < dw; ++u) {
            float denom = H[6] * u + H[7] * v + H[8];
            if (std::abs(denom) < 1e-7f) denom = 1e-7f;

            float x = (H[0] * u + H[1] * v + H[2]) / denom;
            float y = (H[3] * u + H[4] * v + H[5]) / denom;

            int x0 = static_cast<int>(std::floor(x));
            int y0 = static_cast<int>(std::floor(y));
            int x1 = x0 + 1;
            int y1 = y0 + 1;

            float dx = x - static_cast<float>(x0);
            float dy = y - static_cast<float>(y0);

            x0 = std::clamp(x0, 0, static_cast<int>(sw) - 1);
            x1 = std::clamp(x1, 0, static_cast<int>(sw) - 1);
            y0 = std::clamp(y0, 0, static_cast<int>(sh) - 1);
            y1 = std::clamp(y1, 0, static_cast<int>(sh) - 1);

            size_t idx00 = (static_cast<size_t>(y0) * sw + x0) * 3;
            size_t idx01 = (static_cast<size_t>(y0) * sw + x1) * 3;
            size_t idx10 = (static_cast<size_t>(y1) * sw + x0) * 3;
            size_t idx11 = (static_cast<size_t>(y1) * sw + x1) * 3;

            float w00 = (1.0f - dx) * (1.0f - dy);
            float w01 = dx * (1.0f - dy);
            float w10 = (1.0f - dx) * dy;
            float w11 = dx * dy;

            size_t out_idx = (static_cast<size_t>(v) * dw + u) * 3;
            for (int c = 0; c < 3; ++c) {
                int src_c = swap_rb ? (2 - c) : c;
                float val = w00 * s_data[idx00 + src_c] +
                            w01 * s_data[idx01 + src_c] +
                            w10 * s_data[idx10 + src_c] +
                            w11 * s_data[idx11 + src_c];
                d_data[out_idx + c] = static_cast<uint8_t>(std::clamp(std::round(val), 0.0f, 255.0f));
            }
        }
    }
}

// 图像简单双线性缩放 (RGB/BGR)
void resize_bilinear(const ImageBuffer& src, ImageBuffer& dst) {
    const uint8_t* s_data = src.data.data();
    uint32_t sw = src.width;
    uint32_t sh = src.height;
    uint32_t dw = dst.width;
    uint32_t dh = dst.height;
    uint8_t* d_data = dst.data.data();

    float scale_x = static_cast<float>(sw) / dw;
    float scale_y = static_cast<float>(sh) / dh;

    for (uint32_t dy_idx = 0; dy_idx < dh; ++dy_idx) {
        float sy = (dy_idx + 0.5f) * scale_y - 0.5f;
        int y0 = static_cast<int>(std::floor(sy));
        int y1 = y0 + 1;
        float fy = sy - y0;
        y0 = std::clamp(y0, 0, static_cast<int>(sh) - 1);
        y1 = std::clamp(y1, 0, static_cast<int>(sh) - 1);

        for (uint32_t dx_idx = 0; dx_idx < dw; ++dx_idx) {
            float sx = (dx_idx + 0.5f) * scale_x - 0.5f;
            int x0 = static_cast<int>(std::floor(sx));
            int x1 = x0 + 1;
            float fx = sx - x0;
            x0 = std::clamp(x0, 0, static_cast<int>(sw) - 1);
            x1 = std::clamp(x1, 0, static_cast<int>(sw) - 1);

            size_t idx00 = (static_cast<size_t>(y0) * sw + x0) * 3;
            size_t idx01 = (static_cast<size_t>(y0) * sw + x1) * 3;
            size_t idx10 = (static_cast<size_t>(y1) * sw + x0) * 3;
            size_t idx11 = (static_cast<size_t>(y1) * sw + x1) * 3;

            float w00 = (1.0f - fx) * (1.0f - fy);
            float w01 = fx * (1.0f - fy);
            float w10 = (1.0f - fx) * fy;
            float w11 = fx * fy;

            size_t out_idx = (static_cast<size_t>(dy_idx) * dw + dx_idx) * 3;
            for (int c = 0; c < 3; ++c) {
                float val = w00 * s_data[idx00 + c] +
                            w01 * s_data[idx01 + c] +
                            w10 * s_data[idx10 + c] +
                            w11 * s_data[idx11 + c];
                d_data[out_idx + c] = static_cast<uint8_t>(std::clamp(std::round(val), 0.0f, 255.0f));
            }
        }
    }
}

} // namespace

bool Preprocessor::process_frame(const av_frame_desc* frame, PreprocessResult& out, std::string& error) {
    if (!frame) {
        error = "null frame pointer";
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

    if (frame->opaque_kind == AV_OPAQUE_CVPIXELBUFFER && frame->opaque) {
        pixel_buffer = static_cast<CVPixelBufferRef>(frame->opaque);
        CVPixelBufferLockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        y_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 0));
        uv_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 1));
        y_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 0));
        uv_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 1));
    } else if (frame->opaque) {
        y_plane = static_cast<const uint8_t*>(frame->opaque);
        uv_plane = y_plane + (static_cast<size_t>(y_stride > 0 ? y_stride : width) * height);
    } else {
        error = "unsupported memory type or missing buffer data";
        return false;
    }

    if (!y_plane || !uv_plane) {
        if (pixel_buffer) CVPixelBufferUnlockBaseAddress(pixel_buffer, kCVPixelBufferLock_ReadOnly);
        error = "null plane pointers for NV12 frame";
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

    // Letterbox to 640x384
    out.letterbox_info = argus::cv::compute_letterbox(width, height, 640, 384);
    out.letterbox_rgb.width = 640;
    out.letterbox_rgb.height = 384;
    out.letterbox_rgb.channels = 3;
    out.letterbox_rgb.data.assign(640 * 384 * 3, 114); // 114 pad color

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

    std::vector<uint8_t> scaled_rgb(static_cast<size_t>(nw) * nh * 3);
    vImage_Buffer scaled_rgb_buf = {
        .data = scaled_rgb.data(),
        .height = nh,
        .width = nw,
        .rowBytes = static_cast<size_t>(nw * 3)
    };
    vImageConvert_ARGB8888toRGB888(&scaled_argb_buf, &scaled_rgb_buf, kvImageNoFlags);

    // Copy scaled RGB into centered letterbox canvas
    for (uint32_t row = 0; row < nh; ++row) {
        const uint8_t* src_row = scaled_rgb.data() + (static_cast<size_t>(row) * nw * 3);
        uint8_t* dst_row = out.letterbox_rgb.data.data() + (static_cast<size_t>(pad_y + row) * 640 * 3) + (pad_x * 3);
        std::memcpy(dst_row, src_row, static_cast<size_t>(nw) * 3);
    }

    return true;
}

bool Preprocessor::warp_plate_320x48(const ImageBuffer& orig_rgb,
                                    const float landmarks_8[8],
                                    bool is_double_layer,
                                    ImageBuffer& out_plate_320x48,
                                    std::string& error) {
    if (orig_rgb.data.empty() || orig_rgb.width == 0 || orig_rgb.height == 0) {
        error = "empty source image for plate warp";
        return false;
    }

    // 4 点角点自适应边缘外扩 (安全边距，避免车牌检测角点紧贴导致首字汉字或尾字被裁切)
    float h_top_x = landmarks_8[2] - landmarks_8[0];
    float h_top_y = landmarks_8[3] - landmarks_8[1];
    float h_bot_x = landmarks_8[4] - landmarks_8[6];
    float h_bot_y = landmarks_8[5] - landmarks_8[7];
    float h_vec_x = (h_top_x + h_bot_x) * 0.5f;
    float h_vec_y = (h_top_y + h_bot_y) * 0.5f;

    float v_left_x = landmarks_8[6] - landmarks_8[0];
    float v_left_y = landmarks_8[7] - landmarks_8[1];
    float v_right_x = landmarks_8[4] - landmarks_8[2];
    float v_right_y = landmarks_8[5] - landmarks_8[3];
    float v_vec_x = (v_left_x + v_right_x) * 0.5f;
    float v_vec_y = (v_left_y + v_right_y) * 0.5f;

    const float exp_w = 0.25f;
    const float exp_h = 0.25f;

    float src_pts[4][2] = {
        {landmarks_8[0] - exp_w * h_vec_x - exp_h * v_vec_x,
         landmarks_8[1] - exp_w * h_vec_y - exp_h * v_vec_y},
        {landmarks_8[2] + exp_w * h_vec_x - exp_h * v_vec_x,
         landmarks_8[3] + exp_w * h_vec_y - exp_h * v_vec_y},
        {landmarks_8[4] + exp_w * h_vec_x + exp_h * v_vec_x,
         landmarks_8[5] + exp_w * h_vec_y + exp_h * v_vec_y},
        {landmarks_8[6] - exp_w * h_vec_x + exp_h * v_vec_x,
         landmarks_8[7] - exp_w * h_vec_y + exp_h * v_vec_y}
    };

    if (!is_double_layer) {
        out_plate_320x48.width = 320;
        out_plate_320x48.height = 48;
        out_plate_320x48.channels = 3;
        out_plate_320x48.data.resize(320 * 48 * 3);

        std::array<float, 9> H{};
        if (!compute_homography_dst_to_src(320.0f, 48.0f, src_pts, H)) {
            error = "failed to compute perspective homography for single layer plate";
            return false;
        }

        warp_perspective_bilinear(orig_rgb, H, out_plate_320x48, false);
        return true;
    } else {
        // 双层车牌：先透视变换到一个正方形/大矩形，然后上下分割并水平拼接
        float wA = std::hypot(src_pts[2][0] - src_pts[3][0], src_pts[2][1] - src_pts[3][1]);
        float wB = std::hypot(src_pts[1][0] - src_pts[0][0], src_pts[1][0] - src_pts[0][0]);
        float hA = std::hypot(src_pts[1][0] - src_pts[2][0], src_pts[1][0] - src_pts[2][1]);
        float hB = std::hypot(src_pts[0][0] - src_pts[3][0], src_pts[0][0] - src_pts[3][1]);
        uint32_t maxWidth = static_cast<uint32_t>(std::max(320.0f, std::max(wA, wB)));
        uint32_t maxHeight = static_cast<uint32_t>(std::max(96.0f, std::max(hA, hB)));

        ImageBuffer raw_warped;
        raw_warped.width = maxWidth;
        raw_warped.height = maxHeight;
        raw_warped.channels = 3;
        raw_warped.data.resize(static_cast<size_t>(maxWidth) * maxHeight * 3);

        std::array<float, 9> H{};
        if (!compute_homography_dst_to_src(static_cast<float>(maxWidth), static_cast<float>(maxHeight), src_pts, H)) {
            error = "failed to compute perspective homography for double layer plate";
            return false;
        }
        warp_perspective_bilinear(orig_rgb, H, raw_warped, false);

        // 上半部分: y in [0, 5/12 * H]
        // 下半部分: y in [1/3 * H, H]
        uint32_t upper_h = static_cast<uint32_t>(std::round(5.0f / 12.0f * maxHeight));
        uint32_t lower_y = static_cast<uint32_t>(std::round(1.0f / 3.0f * maxHeight));
        uint32_t lower_h = maxHeight - lower_y;

        ImageBuffer upper_crop;
        upper_crop.width = maxWidth;
        upper_crop.height = upper_h;
        upper_crop.channels = 3;
        upper_crop.data.resize(static_cast<size_t>(maxWidth) * upper_h * 3);
        std::memcpy(upper_crop.data.data(), raw_warped.data.data(), upper_crop.data.size());

        ImageBuffer lower_crop;
        lower_crop.width = maxWidth;
        lower_crop.height = lower_h;
        lower_crop.channels = 3;
        lower_crop.data.resize(static_cast<size_t>(maxWidth) * lower_h * 3);
        std::memcpy(lower_crop.data.data(), raw_warped.data.data() + (static_cast<size_t>(lower_y) * maxWidth * 3), lower_crop.data.size());

        // 将 upper_crop 缩放到 lower_crop 的高度 (lower_h)
        ImageBuffer upper_resized;
        upper_resized.width = maxWidth;
        upper_resized.height = lower_h;
        upper_resized.channels = 3;
        upper_resized.data.resize(static_cast<size_t>(maxWidth) * lower_h * 3);
        resize_bilinear(upper_crop, upper_resized);

        // 水平拼接: 宽度 = 2 * maxWidth, 高度 = lower_h
        ImageBuffer merged;
        merged.width = 2 * maxWidth;
        merged.height = lower_h;
        merged.channels = 3;
        merged.data.resize(static_cast<size_t>(merged.width) * merged.height * 3);

        for (uint32_t r = 0; r < lower_h; ++r) {
            uint8_t* dst_row = merged.data.data() + (static_cast<size_t>(r) * merged.width * 3);
            const uint8_t* u_row = upper_resized.data.data() + (static_cast<size_t>(r) * maxWidth * 3);
            const uint8_t* l_row = lower_crop.data.data() + (static_cast<size_t>(r) * maxWidth * 3);
            std::memcpy(dst_row, u_row, maxWidth * 3);
            std::memcpy(dst_row + (maxWidth * 3), l_row, maxWidth * 3);
        }

        // 最终缩放到 320 x 48
        out_plate_320x48.width = 320;
        out_plate_320x48.height = 48;
        out_plate_320x48.channels = 3;
        out_plate_320x48.data.resize(320 * 48 * 3);
        resize_bilinear(merged, out_plate_320x48);
        return true;
    }
}

} // namespace lpr
