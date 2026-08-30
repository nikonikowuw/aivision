/**
 * @file mock_platform.cpp
 * @brief 单元测试专用的 Mock 平台适配器实现
 * 
 * 包含 CPU 软件模拟的图像格式转换（BGRA/RGB24/NV12 互转与 ROI 裁剪采样）、
 * 内存分配释放、模拟视频帧生成及引用计数生命周期。
 */

#include "argus/platform/mock_platform.hpp"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <limits>
#include <memory>
#include <mutex>
#include <new>
#include <unordered_map>


namespace argus::platform {
namespace {

struct MockFrameStorage {
    std::vector<uint8_t> bytes;
    int32_t refs = 1;
};

std::mutex g_frame_mutex;
std::unordered_map<void*, std::unique_ptr<MockFrameStorage>> g_frames;
uint64_t g_next_frame_id = 1;

int mock_frame_retain(void*, void* token) {
    if (!token) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(g_frame_mutex);
    auto it = g_frames.find(token);
    if (it == g_frames.end() || it->second->refs <= 0 || it->second->refs == std::numeric_limits<int32_t>::max()) {
        return AV_ERR_INVALID_ARG;
    }
    ++it->second->refs;
    return AV_OK;
}

int mock_frame_release(void*, void* token) {
    if (!token) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(g_frame_mutex);
    auto it = g_frames.find(token);
    if (it == g_frames.end() || it->second->refs <= 0) return AV_ERR_INVALID_ARG;
    if (--it->second->refs == 0) g_frames.erase(it);
    return AV_OK;
}

void mock_release_opaque(void* opaque) {
    if (!opaque) return;
    std::lock_guard<std::mutex> lock(g_frame_mutex);
    for (auto it = g_frames.begin(); it != g_frames.end(); ++it) {
        if (it->second->bytes.data() == opaque) {
            g_frames.erase(it);
            return;
        }
    }
}

const av_frame_ops g_mock_frame_ops = {
    sizeof(av_frame_ops), AV_ALGO_API_VERSION, nullptr,
    mock_frame_retain, mock_frame_release
};

uint8_t clamp_byte(float value) {
    return static_cast<uint8_t>(std::clamp(value, 0.0f, 255.0f));
}

bool get_roi(const av_rect* roi, float& x, float& y, float& width, float& height) {
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

bool read_source_pixel(const av_frame_desc* src, uint32_t x, uint32_t y, uint8_t rgb[3]) {
    if (!src || !src->opaque || x >= src->width || y >= src->height) return false;
    const auto* base = static_cast<const uint8_t*>(src->opaque);
    if (src->pixel_format == AV_PIX_BGRA) {
        const auto* pixel = base + src->offset[0] + static_cast<size_t>(y) * std::abs(src->stride[0]) + x * 4;
        rgb[0] = pixel[2];
        rgb[1] = pixel[1];
        rgb[2] = pixel[0];
        return true;
    }
    if (src->pixel_format == AV_PIX_RGB24) {
        const auto* pixel = base + src->offset[0] + static_cast<size_t>(y) * std::abs(src->stride[0]) + x * 3;
        std::memcpy(rgb, pixel, 3);
        return true;
    }
    if (src->pixel_format == AV_PIX_NV12 && src->plane_count >= 2) {
        const auto* y_plane = base + src->offset[0] + static_cast<size_t>(y) * std::abs(src->stride[0]);
        const auto* uv_plane = base + src->offset[1] + static_cast<size_t>(y / 2) * std::abs(src->stride[1]);
        const float y_value = src->color_range == AV_COLOR_RANGE_FULL
            ? static_cast<float>(y_plane[x])
            : 1.164f * (static_cast<float>(y_plane[x]) - 16.0f);
        const float u = static_cast<float>(uv_plane[(x / 2) * 2]) - 128.0f;
        const float v = static_cast<float>(uv_plane[(x / 2) * 2 + 1]) - 128.0f;
        rgb[0] = clamp_byte(y_value + 1.5748f * v);
        rgb[1] = clamp_byte(y_value - 0.1873f * u - 0.4681f * v);
        rgb[2] = clamp_byte(y_value + 1.8556f * u);
        return true;
    }
    return false;
}

int mock_c_convert(void*, const av_frame_desc* src, const av_rect* src_roi,
                   const av_image_view* dst, uint32_t) {
    if (!src || !dst || !dst->data || dst->width == 0 || dst->height == 0) return AV_ERR_INVALID_ARG;
    if (dst->pixel_format != AV_PIX_BGRA && dst->pixel_format != AV_PIX_RGB24) return AV_ERR_NOT_IMPLEMENTED;

    float roi_x, roi_y, roi_w, roi_h;
    if (!get_roi(src_roi, roi_x, roi_y, roi_w, roi_h)) return AV_ERR_INVALID_ARG;
    auto* output = static_cast<uint8_t*>(dst->data);
    const size_t output_bpp = dst->pixel_format == AV_PIX_BGRA ? 4 : 3;
    for (uint32_t y = 0; y < dst->height; ++y) {
        auto* row = output + static_cast<size_t>(y) * std::abs(dst->stride[0]);
        const uint32_t src_y = std::min(src->height - 1, static_cast<uint32_t>((roi_y + roi_h * (static_cast<float>(y) / dst->height)) * src->height));
        for (uint32_t x = 0; x < dst->width; ++x) {
            const uint32_t src_x = std::min(src->width - 1, static_cast<uint32_t>((roi_x + roi_w * (static_cast<float>(x) / dst->width)) * src->width));
            uint8_t rgb[3];
            if (!read_source_pixel(src, src_x, src_y, rgb)) return AV_ERR_INVALID_ARG;
            auto* pixel = row + static_cast<size_t>(x) * output_bpp;
            if (output_bpp == 4) {
                pixel[0] = rgb[2];
                pixel[1] = rgb[1];
                pixel[2] = rgb[0];
                pixel[3] = 255;
            } else {
                std::memcpy(pixel, rgb, 3);
            }
        }
    }
    return AV_OK;
}

int mock_c_pad(void*, const av_image_view* dst, const av_rect* region, const uint8_t value[4]) {
    if (!dst || !dst->data || !value || dst->width == 0 || dst->height == 0) return AV_ERR_INVALID_ARG;
    if (dst->pixel_format != AV_PIX_BGRA && dst->pixel_format != AV_PIX_RGB24) return AV_ERR_NOT_IMPLEMENTED;
    float roi_x, roi_y, roi_w, roi_h;
    if (!get_roi(region, roi_x, roi_y, roi_w, roi_h)) return AV_ERR_INVALID_ARG;
    const uint32_t left = static_cast<uint32_t>(roi_x * dst->width);
    const uint32_t top = static_cast<uint32_t>(roi_y * dst->height);
    const uint32_t right = std::min(dst->width, static_cast<uint32_t>((roi_x + roi_w) * dst->width));
    const uint32_t bottom = std::min(dst->height, static_cast<uint32_t>((roi_y + roi_h) * dst->height));
    const size_t bpp = dst->pixel_format == AV_PIX_BGRA ? 4 : 3;
    auto* output = static_cast<uint8_t*>(dst->data);
    for (uint32_t y = top; y < bottom; ++y) {
        auto* row = output + static_cast<size_t>(y) * std::abs(dst->stride[0]);
        for (uint32_t x = left; x < right; ++x) {
            std::memcpy(row + static_cast<size_t>(x) * bpp, value, bpp);
        }
    }
    return AV_OK;
}

int mock_c_alloc(void*, uint32_t width, uint32_t height, uint32_t pixel_format, av_image_view* out) {
    if (!out || width == 0 || height == 0) return AV_ERR_INVALID_ARG;
    size_t bytes_per_pixel = 0;
    uint32_t plane_count = 1;
    size_t bytes = 0;
    if (pixel_format == AV_PIX_BGRA) bytes_per_pixel = 4;
    else if (pixel_format == AV_PIX_RGB24) bytes_per_pixel = 3;
    else if (pixel_format == AV_PIX_NV12) {
        plane_count = 2;
        bytes = static_cast<size_t>(width) * height + static_cast<size_t>(width) * ((height + 1) / 2);
    } else {
        return AV_ERR_NOT_IMPLEMENTED;
    }
    if (bytes_per_pixel != 0) bytes = static_cast<size_t>(width) * height * bytes_per_pixel;
    if (bytes == 0) return AV_ERR_OUT_OF_MEMORY;
    auto* data = new (std::nothrow) uint8_t[bytes]();
    if (!data) return AV_ERR_OUT_OF_MEMORY;

    *out = {};
    out->size = sizeof(av_image_view);
    out->api_version = AV_ALGO_API_VERSION;
    out->width = width;
    out->height = height;
    out->pixel_format = pixel_format;
    out->memory_type = AV_MEM_HOST;
    out->plane_count = plane_count;
    out->stride[0] = static_cast<int32_t>(width * (bytes_per_pixel == 0 ? 1 : bytes_per_pixel));
    out->offset[0] = 0;
    if (plane_count == 2) {
        out->stride[1] = static_cast<int32_t>(width);
        out->offset[1] = static_cast<uint64_t>(width) * height;
    }
    out->data = data;
    return AV_OK;
}

int mock_c_free(void*, av_image_view* image) {
    if (!image) return AV_ERR_INVALID_ARG;
    delete[] static_cast<uint8_t*>(image->data);
    *image = {};
    return AV_OK;
}

} // namespace

MockDecoder::MockDecoder(std::string codec) : codec_(std::move(codec)) {}

av_status MockDecoder::send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool) {
    if (!data || size == 0) return AV_ERR_INVALID_ARG;
    const uint8_t* nal = data;
    size_t nal_size = size;
    if (size >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1) {
        nal = data + 4;
        nal_size = size - 4;
    } else if (size >= 3 && data[0] == 0 && data[1] == 0 && data[2] == 1) {
        nal = data + 3;
        nal_size = size - 3;
    } else if (size >= 4) {
        const uint32_t avcc_size = (static_cast<uint32_t>(data[0]) << 24) |
                                   (static_cast<uint32_t>(data[1]) << 16) |
                                   (static_cast<uint32_t>(data[2]) << 8) |
                                   data[3];
        if (avcc_size > 0 && avcc_size <= size - 4) {
            nal = data + 4;
            nal_size = avcc_size;
        }
    }
    if (nal_size == 0) return AV_ERR_INVALID_ARG;
    const uint8_t type = codec_ == "H265" || codec_ == "HEVC"
        ? static_cast<uint8_t>((nal[0] >> 1) & 0x3F)
        : static_cast<uint8_t>(nal[0] & 0x1F);
    const bool parameter_set = codec_ == "H265" || codec_ == "HEVC"
        ? type >= 32 && type <= 34
        : type == 7 || type == 8;
    last_pts_ = pts_us;
    has_packet_ = !parameter_set;
    return AV_OK;
}

av_status MockDecoder::receive_frame(av_frame_desc* out_frame) {
    if (!has_packet_ || !out_frame) return AV_ERR_RETRY;

    constexpr uint32_t width = 1920;
    constexpr uint32_t height = 1080;
    auto storage = std::make_unique<MockFrameStorage>();
    storage->bytes.resize(static_cast<size_t>(width) * height + static_cast<size_t>(width) * (height / 2), 128);
    std::fill(storage->bytes.begin(), storage->bytes.begin() + static_cast<size_t>(width) * height, 16);
    void* token = storage.get();
    void* opaque = storage->bytes.data();
    {
        std::lock_guard<std::mutex> lock(g_frame_mutex);
        g_frames.emplace(token, std::move(storage));
    }

    *out_frame = {};
    out_frame->size = sizeof(av_frame_desc);
    out_frame->api_version = AV_ALGO_API_VERSION;
    out_frame->frame_id = g_next_frame_id++;
    out_frame->pts_ns = last_pts_ * 1000;
    out_frame->wall_time_ns = last_pts_ * 1000;
    out_frame->platform_tag = 0x4D4F434B;
    out_frame->memory_type = AV_MEM_HOST;
    out_frame->pixel_format = AV_PIX_NV12;
    out_frame->layout = AV_LAYOUT_LINEAR;
    out_frame->width = width;
    out_frame->height = height;
    out_frame->alloc_width = width;
    out_frame->alloc_height = height;
    out_frame->plane_count = 2;
    out_frame->offset[0] = 0;
    out_frame->offset[1] = static_cast<uint64_t>(width) * height;
    out_frame->stride[0] = static_cast<int32_t>(width);
    out_frame->stride[1] = static_cast<int32_t>(width);
    out_frame->opaque = opaque;
    out_frame->frame_token = token;
    out_frame->color_primaries = AV_COLOR_PRIM_BT709;
    out_frame->color_transfer = AV_COLOR_TRC_BT709;
    out_frame->color_matrix = AV_COLOR_MAT_BT709;
    out_frame->color_range = AV_COLOR_RANGE_LIMITED;
    out_frame->time_synced = 1;
    has_packet_ = false;
    return AV_OK;
}

void MockDecoder::flush() {
    has_packet_ = false;
}

void MockDecoder::reset() {
    flush();
}

const av_frame_ops* MockDecoder::get_frame_ops() const {
    return &g_mock_frame_ops;
}

MockPlatformAdapter::MockPlatformAdapter() {
    profile_.platform_id = "mock";
    profile_.platform_tag = 0x4D4F434B; // 'MOCK'
    profile_.total_compute_units = 1000;
    profile_.reserved_compute_units = 100;
    profile_.hardware_decode.status = CapabilityStatus::AVAILABLE;
    profile_.vector_image_ops.status = CapabilityStatus::DEGRADED;
    profile_.vector_image_ops.reason = "CPU fallback for mock platform";
    profile_.telemetry_metrics.status = CapabilityStatus::AVAILABLE;

    c_image_ops_.size = sizeof(av_image_ops);
    c_image_ops_.api_version = AV_ALGO_API_VERSION;
    c_image_ops_.ctx = this;
    c_image_ops_.convert = mock_c_convert;
    c_image_ops_.pad = mock_c_pad;
    c_image_ops_.alloc = mock_c_alloc;
    c_image_ops_.free = mock_c_free;
}

OpaqueReleaseFn MockPlatformAdapter::get_opaque_release() const {
    return mock_release_opaque;
}

} // namespace argus::platform
