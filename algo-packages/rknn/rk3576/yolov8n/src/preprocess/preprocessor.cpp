#include "preprocessor.hpp"
#include <cstring>
#include <algorithm>
#include <cmath>
#include <iostream>
#include <sys/mman.h>

#if defined(HAVE_RGA)
#include <rga/im2d.h>
#include <rga/rga.h>
#endif

namespace yolov8n {

void Preprocessor::release_input(PreparedInput& input, const av_image_ops* image_ops) {
    if (input.from_image_ops && image_ops && image_ops->free) {
        image_ops->free(image_ops->ctx, &input.view);
        input.from_image_ops = false;
    }
}

bool Preprocessor::prepare_input(
    const av_frame_desc* frame,
    const av_image_ops* image_ops,
    uint32_t net_width,
    uint32_t net_height,
    PreparedInput& out
) {
    if (!frame) return false;

    out.letterbox = argus::cv::compute_letterbox(
        frame->width, frame->height,
        net_width, net_height
    );

    // Path 1: av_image_ops hardware acceleration (if provided by host runtime)
    if (image_ops && image_ops->convert) {
        av_image_view target_view;
        std::memset(&target_view, 0, sizeof(target_view));
        target_view.size = sizeof(av_image_view);
        target_view.api_version = AV_ALGO_API_VERSION;
        target_view.width = net_width;
        target_view.height = net_height;
        target_view.pixel_format = AV_PIX_RGB24;
        target_view.memory_type = AV_MEM_HOST;

        out.host_buffer.resize(net_width * net_height * 3);
        target_view.data = out.host_buffer.data();
        target_view.stride[0] = net_width * 3;

        uint8_t pad_val[4] = {114, 114, 114, 0};
        if (image_ops->pad) {
            image_ops->pad(image_ops->ctx, &target_view, nullptr, pad_val);
        } else {
            std::fill(out.host_buffer.begin(), out.host_buffer.end(), 114);
        }

        if (image_ops->convert(image_ops->ctx, frame, nullptr, &target_view, 0) == AV_OK) {
            out.view = target_view;
            out.from_image_ops = false;
            return true;
        }
    }

#if defined(HAVE_RGA)
    // Path 2: Direct Rockchip RGA (Hardware 2D Accelerator)
    if (frame->pixel_format == AV_PIX_NV12 && frame->opaque != nullptr) {
        int src_w = static_cast<int>(frame->width);
        int src_h = static_cast<int>(frame->height);
        int src_stride = frame->stride[0] > 0 ? frame->stride[0] : src_w;

        // Stride must be valid
        if (src_w % 2 == 0 && src_h % 2 == 0) {
            out.host_buffer.assign(net_width * net_height * 3, 114);

            rga_buffer_t src_buf;
            if (frame->opaque_kind == AV_OPAQUE_DMABUF) {
                int dma_fd = static_cast<int>(reinterpret_cast<intptr_t>(frame->opaque));
                src_buf = wrapbuffer_fd(
                    dma_fd,
                    src_w, src_h,
                    RK_FORMAT_YCbCr_420_SP,
                    src_stride, src_h
                );
            } else {
                src_buf = wrapbuffer_virtualaddr(
                    const_cast<void*>(frame->opaque),
                    src_w, src_h,
                    RK_FORMAT_YCbCr_420_SP,
                    src_stride, src_h
                );
            }

            rga_buffer_t dst_buf = wrapbuffer_virtualaddr(
                out.host_buffer.data(),
                static_cast<int>(net_width), static_cast<int>(net_height),
                RK_FORMAT_RGB_888,
                static_cast<int>(net_width), static_cast<int>(net_height)
            );

            im_rect srect = {0, 0, src_w, src_h};
            int pad_x = static_cast<int>(std::round(out.letterbox.pad_x));
            int pad_y = static_cast<int>(std::round(out.letterbox.pad_y));
            int scaled_w = static_cast<int>(std::round(static_cast<float>(src_w) * out.letterbox.scale));
            int scaled_h = static_cast<int>(std::round(static_cast<float>(src_h) * out.letterbox.scale));

            im_rect drect = {pad_x, pad_y, scaled_w, scaled_h};

            IM_STATUS status = improcess(src_buf, dst_buf, {}, srect, drect, {}, 0);
            if (status == IM_STATUS_SUCCESS || status > 0) {
                out.view.size = sizeof(av_image_view);
                out.view.api_version = AV_ALGO_API_VERSION;
                out.view.data = out.host_buffer.data();
                out.view.width = net_width;
                out.view.height = net_height;
                out.view.stride[0] = net_width * 3;
                out.view.pixel_format = AV_PIX_RGB24;
                out.view.memory_type = AV_MEM_HOST;
                out.from_image_ops = false;
                return true;
            }
        }
    }
#endif

    // Path 3: CPU fallback path
    return cpu_fallback_nv12_to_rgb(frame, net_width, net_height, out);
}

bool Preprocessor::cpu_fallback_nv12_to_rgb(
    const av_frame_desc* frame,
    uint32_t net_width,
    uint32_t net_height,
    PreparedInput& out
) {
    if (!frame || frame->pixel_format != AV_PIX_NV12) {
        return false;
    }

    const uint8_t* y_plane = nullptr;
    const uint8_t* uv_plane = nullptr;
    void* mapped_ptr = nullptr;
    size_t mapped_size = 0;

    if (frame->opaque_kind == AV_OPAQUE_DMABUF) {
        int dma_fd = static_cast<int>(reinterpret_cast<intptr_t>(frame->opaque));
        mapped_size = (frame->stride[0] > 0 ? frame->stride[0] : frame->width) * frame->height * 3 / 2;
        mapped_ptr = mmap(NULL, mapped_size, PROT_READ, MAP_SHARED, dma_fd, 0);
        if (mapped_ptr != MAP_FAILED) {
            y_plane = static_cast<const uint8_t*>(mapped_ptr) + frame->offset[0];
            uv_plane = static_cast<const uint8_t*>(mapped_ptr) + frame->offset[1];
        } else {
            return false;
        }
    } else if (frame->opaque) {
        y_plane = static_cast<const uint8_t*>(frame->opaque) + frame->offset[0];
        uv_plane = static_cast<const uint8_t*>(frame->opaque) + frame->offset[1];
    } else {
        return false;
    }

    const int32_t src_stride_y = frame->stride[0] > 0 ? frame->stride[0] : static_cast<int32_t>(frame->width);
    const int32_t src_stride_uv = frame->stride[1] > 0 ? frame->stride[1] : static_cast<int32_t>(frame->width);

    out.host_buffer.assign(net_width * net_height * 3, 114);
    uint8_t* dst = out.host_buffer.data();
    const uint32_t dst_stride = net_width * 3;

    const auto& lb = out.letterbox;
    uint32_t scaled_w = static_cast<uint32_t>(std::round(static_cast<float>(frame->width) * lb.scale));
    uint32_t scaled_h = static_cast<uint32_t>(std::round(static_cast<float>(frame->height) * lb.scale));
    uint32_t pad_left = static_cast<uint32_t>(std::round(lb.pad_x));
    uint32_t pad_top = static_cast<uint32_t>(std::round(lb.pad_y));

    for (uint32_t dst_y = 0; dst_y < scaled_h; ++dst_y) {
        uint32_t src_y = static_cast<uint32_t>(std::min(static_cast<float>(dst_y) / lb.scale, static_cast<float>(frame->height - 1)));
        uint32_t out_y = pad_top + dst_y;
        if (out_y >= net_height) break;

        uint8_t* row_dst = dst + out_y * dst_stride + pad_left * 3;

        for (uint32_t dst_x = 0; dst_x < scaled_w; ++dst_x) {
            uint32_t src_x = static_cast<uint32_t>(std::min(static_cast<float>(dst_x) / lb.scale, static_cast<float>(frame->width - 1)));
            if (pad_left + dst_x >= net_width) break;

            uint8_t y_val = y_plane[src_y * src_stride_y + src_x];
            uint32_t uv_x = (src_x / 2) * 2;
            uint32_t uv_y = src_y / 2;
            uint8_t u_val = uv_plane[uv_y * src_stride_uv + uv_x];
            uint8_t v_val = uv_plane[uv_y * src_stride_uv + uv_x + 1];

            // BT.709 limited range NV12 -> sRGB
            int32_t c = static_cast<int32_t>(y_val) - 16;
            int32_t d = static_cast<int32_t>(u_val) - 128;
            int32_t e = static_cast<int32_t>(v_val) - 128;

            int32_t r = (298 * c + 409 * e + 128) >> 8;
            int32_t g = (298 * c - 100 * d - 208 * e + 128) >> 8;
            int32_t b = (298 * c + 516 * d + 128) >> 8;

            row_dst[dst_x * 3 + 0] = static_cast<uint8_t>(std::clamp(r, 0, 255));
            row_dst[dst_x * 3 + 1] = static_cast<uint8_t>(std::clamp(g, 0, 255));
            row_dst[dst_x * 3 + 2] = static_cast<uint8_t>(std::clamp(b, 0, 255));
        }
    }

    if (mapped_ptr && mapped_size > 0) {
        munmap(mapped_ptr, mapped_size);
    }

    out.view.size = sizeof(av_image_view);
    out.view.api_version = AV_ALGO_API_VERSION;
    out.view.data = out.host_buffer.data();
    out.view.width = net_width;
    out.view.height = net_height;
    out.view.stride[0] = dst_stride;
    out.view.pixel_format = AV_PIX_RGB24;
    out.view.memory_type = AV_MEM_HOST;
    out.from_image_ops = false;

    return true;
}

} // namespace yolov8n
