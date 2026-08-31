/**
 * @file motion_gate.cpp
 * @brief Frigate 风格运动检测门控实现
 */

#include "argus/core/motion_gate.hpp"
#include <algorithm>
#include <cmath>
#include <cstring>
#include <nlohmann/json.hpp>

#if defined(__APPLE__)
#include <CoreVideo/CoreVideo.h>
#endif

namespace argus::core {
namespace {

void sanitize_config(MotionGateConfig& config) {
    if (config.frame_height == 0) config.frame_height = 100;
    if (config.threshold == 0) config.threshold = 25;
    if (config.contour_area == 0) config.contour_area = 50;
    if (config.frame_alpha <= 0.0f || config.frame_alpha > 1.0f) config.frame_alpha = 0.05f;
}

// 检查点是否在多边形内部（Ray casting 算法）
bool point_in_polygon(float px, float py, const std::vector<av_point>& polygon) {
    if (polygon.size() < 3) return false;
    bool inside = false;
    const size_t n = polygon.size();
    for (size_t i = 0, j = n - 1; i < n; j = i++) {
        const float xi = polygon[i].x;
        const float yi = polygon[i].y;
        const float xj = polygon[j].x;
        const float yj = polygon[j].y;

        const bool intersect = ((yi > py) != (yj > py)) &&
            (px < (xj - xi) * (py - yi) / (yj - yi) + xi);
        if (intersect) {
            inside = !inside;
        }
    }
    return inside;
}

} // namespace

MotionGate::MotionGate(MotionGateConfig config)
    : config_(std::move(config)) {
    sanitize_config(config_);
}

void MotionGate::update_config(const MotionGateConfig& config) {
    config_ = config;
    sanitize_config(config_);
    reset();
}

void MotionGate::update_config_from_json(const std::string& params_json) {
    if (params_json.empty()) return;
    try {
        auto parsed = nlohmann::json::parse(params_json);
        if (!parsed.contains("motion_gate") || !parsed["motion_gate"].is_object()) {
            return;
        }
        const auto& mg = parsed["motion_gate"];
        MotionGateConfig new_config = config_;
        new_config.enabled = mg.value("enabled", new_config.enabled);
        new_config.frame_height = mg.value("frame_height", new_config.frame_height);
        new_config.threshold = mg.value("threshold", new_config.threshold);
        new_config.contour_area = mg.value("contour_area", new_config.contour_area);
        new_config.frame_alpha = mg.value("frame_alpha", new_config.frame_alpha);
        if (mg.contains("keepalive_interval_ms") && mg["keepalive_interval_ms"].is_number_unsigned()) {
            new_config.keepalive_interval = std::chrono::milliseconds(mg["keepalive_interval_ms"].get<uint64_t>());
        }
        if (mg.contains("masks") && mg["masks"].is_array()) {
            new_config.masks.clear();
            for (const auto& mask_arr : mg["masks"]) {
                if (!mask_arr.is_array()) continue;
                std::vector<av_point> poly;
                for (const auto& pt : mask_arr) {
                    if (pt.is_array() && pt.size() >= 2) {
                        poly.push_back({pt[0].get<float>(), pt[1].get<float>()});
                    } else if (pt.is_object() && pt.contains("x") && pt.contains("y")) {
                        poly.push_back({pt["x"].get<float>(), pt["y"].get<float>()});
                    }
                }
                if (poly.size() >= 3) {
                    new_config.masks.push_back(std::move(poly));
                }
            }
        }
        update_config(new_config);
    } catch (...) {
        // 忽略格式非法的 JSON
    }
}

void MotionGate::sync_rule_masks(const av_rule* rules, size_t count) {
    if (!rules || count == 0) return;
    std::vector<std::vector<av_point>> mask_polys;
    for (size_t i = 0; i < count; ++i) {
        const auto& r = rules[i];
        if (r.role == AV_RULE_MASK && r.points && r.point_count >= 3) {
            std::vector<av_point> poly;
            poly.reserve(r.point_count);
            for (uint32_t j = 0; j < r.point_count; ++j) {
                poly.push_back(r.points[j]);
            }
            mask_polys.push_back(std::move(poly));
        }
    }
    config_.masks = std::move(mask_polys);
    rebuild_mask_map();
}

void MotionGate::reset() {
    bg_initialized_ = false;
    bg_model_.clear();
    curr_luma_.clear();
    mask_map_.clear();
    motion_width_ = 0;
    motion_height_ = 0;
    last_src_width_ = 0;
    last_src_height_ = 0;
    last_pixel_format_ = 0;
    last_infer_time_ = {};
}

void MotionGate::rebuild_mask_map() {
    if (motion_width_ == 0 || motion_height_ == 0) {
        mask_map_.clear();
        return;
    }
    mask_map_.assign(static_cast<size_t>(motion_width_) * motion_height_, 0);
    if (config_.masks.empty()) {
        return;
    }

    for (uint32_t y = 0; y < motion_height_; ++y) {
        const float py = (static_cast<float>(y) + 0.5f) / static_cast<float>(motion_height_);
        for (uint32_t x = 0; x < motion_width_; ++x) {
            const float px = (static_cast<float>(x) + 0.5f) / static_cast<float>(motion_width_);
            for (const auto& poly : config_.masks) {
                if (point_in_polygon(px, py, poly)) {
                    mask_map_[static_cast<size_t>(y) * motion_width_ + x] = 255;
                    break;
                }
            }
        }
    }
}

bool MotionGate::extract_lowres_luma(const av_frame_desc& frame, std::vector<uint8_t>& out_luma,
                                     uint32_t out_w, uint32_t out_h) {
    if (out_w == 0 || out_h == 0 || frame.width == 0 || frame.height == 0) {
        return false;
    }
    out_luma.resize(static_cast<size_t>(out_w) * out_h);

    const uint8_t* src_y_plane = nullptr;
    int32_t src_stride = 0;

#if defined(__APPLE__)
    struct CVPixelBufferUnlocker {
        CVPixelBufferRef buf = nullptr;
        bool locked = false;
        ~CVPixelBufferUnlocker() {
            if (locked && buf) {
                CVPixelBufferUnlockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
            }
        }
    };

    CVPixelBufferUnlocker unlocker;
    if (frame.opaque_kind == AV_OPAQUE_CVPIXELBUFFER && frame.opaque) {
        unlocker.buf = static_cast<CVPixelBufferRef>(frame.opaque);
        if (CVPixelBufferLockBaseAddress(unlocker.buf, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess) {
            unlocker.locked = true;
            if (CVPixelBufferIsPlanar(unlocker.buf)) {
                src_y_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(unlocker.buf, 0));
                src_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRowOfPlane(unlocker.buf, 0));
            } else {
                src_y_plane = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(unlocker.buf));
                src_stride = static_cast<int32_t>(CVPixelBufferGetBytesPerRow(unlocker.buf));
            }
        }
    }
#endif

    if (!src_y_plane && frame.opaque) {
        src_y_plane = static_cast<const uint8_t*>(frame.opaque) + frame.offset[0];
        src_stride = frame.stride[0];
    }

    if (!src_y_plane) {
        return false;
    }

    const uint32_t src_w = frame.width;
    const uint32_t src_h = frame.height;
    const int32_t abs_stride = std::abs(src_stride);

    // 双线性/近邻采样提取灰度小图
    if (frame.pixel_format == AV_PIX_NV12 || frame.pixel_format == AV_PIX_I420) {
        // 单通道 Y 分量直接采样
        for (uint32_t dst_y = 0; dst_y < out_h; ++dst_y) {
            const uint32_t sy = std::min(src_h - 1, (dst_y * src_h) / out_h);
            const uint8_t* row_src = src_y_plane + static_cast<size_t>(sy) * abs_stride;
            uint8_t* row_dst = out_luma.data() + static_cast<size_t>(dst_y) * out_w;
            for (uint32_t dst_x = 0; dst_x < out_w; ++dst_x) {
                const uint32_t sx = std::min(src_w - 1, (dst_x * src_w) / out_w);
                row_dst[dst_x] = row_src[sx];
            }
        }
    } else if (frame.pixel_format == AV_PIX_BGRA || frame.pixel_format == AV_PIX_RGB24) {
        const bool is_bgra = (frame.pixel_format == AV_PIX_BGRA);
        const uint32_t channels = is_bgra ? 4 : 3;
        const uint32_t b_off = is_bgra ? 0 : 2;
        const uint32_t g_off = 1;
        const uint32_t r_off = is_bgra ? 2 : 0;
        for (uint32_t dst_y = 0; dst_y < out_h; ++dst_y) {
            const uint32_t sy = std::min(src_h - 1, (dst_y * src_h) / out_h);
            const uint8_t* row_src = src_y_plane + static_cast<size_t>(sy) * abs_stride;
            uint8_t* row_dst = out_luma.data() + static_cast<size_t>(dst_y) * out_w;
            for (uint32_t dst_x = 0; dst_x < out_w; ++dst_x) {
                const uint32_t sx = std::min(src_w - 1, (dst_x * src_w) / out_w);
                const uint8_t b = row_src[sx * channels + b_off];
                const uint8_t g = row_src[sx * channels + g_off];
                const uint8_t r = row_src[sx * channels + r_off];
                row_dst[dst_x] = static_cast<uint8_t>((r * 77 + g * 150 + b * 29) >> 8);
            }
        }
    } else {
        return false;
    }

    return true;
}

MotionDecision MotionGate::evaluate(const av_frame_desc& frame,
                                   std::chrono::steady_clock::time_point now) {
    stats_.total_frames++;

    if (!config_.enabled) {
        stats_.passthrough_frames++;
        return MotionDecision::PASSTHROUGH;
    }

    if (frame.width == 0 || frame.height == 0) {
        stats_.passthrough_frames++;
        return MotionDecision::PASSTHROUGH;
    }

    // 计算采样目标宽度与高度（等比例）
    const uint32_t target_h = std::max<uint32_t>(16, config_.frame_height);
    const uint32_t target_w = std::max<uint32_t>(16, (frame.width * target_h) / frame.height);

    // 检查分辨率或格式变化，必要时重建背景模型
    if (!bg_initialized_ || motion_width_ != target_w || motion_height_ != target_h ||
        last_src_width_ != frame.width || last_src_height_ != frame.height ||
        last_pixel_format_ != frame.pixel_format) {
        motion_width_ = target_w;
        motion_height_ = target_h;
        last_src_width_ = frame.width;
        last_src_height_ = frame.height;
        last_pixel_format_ = frame.pixel_format;

        rebuild_mask_map();

        if (!extract_lowres_luma(frame, curr_luma_, motion_width_, motion_height_)) {
            stats_.passthrough_frames++;
            return MotionDecision::PASSTHROUGH;
        }

        bg_model_.resize(curr_luma_.size());
        for (size_t i = 0; i < curr_luma_.size(); ++i) {
            bg_model_[i] = static_cast<float>(curr_luma_[i]);
        }
        bg_initialized_ = true;
        last_infer_time_ = now;

        // 首帧建立背景模型，放行一次以建立基线
        stats_.keepalive_passed++;
        return MotionDecision::KEEPALIVE;
    }

    // 提取当前帧灰度
    if (!extract_lowres_luma(frame, curr_luma_, motion_width_, motion_height_)) {
        stats_.passthrough_frames++;
        return MotionDecision::PASSTHROUGH;
    }

    // 计算差分并统计变化区域像素
    uint32_t changed_pixels = 0;
    const float alpha = config_.frame_alpha;
    const float threshold = static_cast<float>(config_.threshold);
    const size_t total_pixels = static_cast<size_t>(motion_width_) * motion_height_;

    for (size_t i = 0; i < total_pixels; ++i) {
        const float curr_val = static_cast<float>(curr_luma_[i]);
        const float bg_val = bg_model_[i];
        const float diff = std::abs(curr_val - bg_val);

        // 如果不在排除 Mask 区域内，且差分超过阈值，计入变化面积
        if ((mask_map_.empty() || mask_map_[i] == 0) && diff >= threshold) {
            changed_pixels++;
        }

        // 滑动平均更新背景模型
        bg_model_[i] = alpha * curr_val + (1.0f - alpha) * bg_val;
    }

    // 判定是否超过运动轮廓面积阈值
    stats_.last_changed_pixels = changed_pixels;
    stats_.max_changed_pixels = std::max(stats_.max_changed_pixels, changed_pixels);
    if (changed_pixels >= config_.contour_area) {
        last_infer_time_ = now;
        stats_.motion_passed++;
        return MotionDecision::MOTION;
    }

    // 检查保活计时
    if (last_infer_time_.time_since_epoch().count() == 0 ||
        now - last_infer_time_ >= config_.keepalive_interval) {
        last_infer_time_ = now;
        stats_.keepalive_passed++;
        return MotionDecision::KEEPALIVE;
    }

    stats_.skipped_frames++;
    return MotionDecision::SKIP;
}

} // namespace argus::core
