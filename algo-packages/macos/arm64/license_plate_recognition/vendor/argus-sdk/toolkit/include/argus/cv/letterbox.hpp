#pragma once

#include <algorithm>
#include <cmath>
#include <cstdint>

namespace argus::cv {

struct NormalizedBBox {
    float x_min;
    float y_min;
    float x_max;
    float y_max;
};

struct LetterboxInfo {
    float scale;
    float pad_x;
    float pad_y;
    uint32_t net_w;
    uint32_t net_h;

    [[nodiscard]] NormalizedBBox unletterbox_bbox(const NormalizedBBox& box, uint32_t orig_w, uint32_t orig_h) const {
        if (scale <= 0.0f || orig_w == 0 || orig_h == 0) return box;

        float x_min_pix = (box.x_min * static_cast<float>(net_w) - pad_x) / scale;
        float y_min_pix = (box.y_min * static_cast<float>(net_h) - pad_y) / scale;
        float x_max_pix = (box.x_max * static_cast<float>(net_w) - pad_x) / scale;
        float y_max_pix = (box.y_max * static_cast<float>(net_h) - pad_y) / scale;

        return NormalizedBBox{
            .x_min = std::clamp(x_min_pix / static_cast<float>(orig_w), 0.0f, 1.0f),
            .y_min = std::clamp(y_min_pix / static_cast<float>(orig_h), 0.0f, 1.0f),
            .x_max = std::clamp(x_max_pix / static_cast<float>(orig_w), 0.0f, 1.0f),
            .y_max = std::clamp(y_max_pix / static_cast<float>(orig_h), 0.0f, 1.0f)
        };
    }
};

inline LetterboxInfo compute_letterbox(uint32_t src_w, uint32_t src_h, uint32_t dst_w, uint32_t dst_h) {
    if (src_w == 0 || src_h == 0 || dst_w == 0 || dst_h == 0) {
        return LetterboxInfo{1.0f, 0.0f, 0.0f, dst_w, dst_h};
    }
    float r = std::min(static_cast<float>(dst_w) / static_cast<float>(src_w),
                       static_cast<float>(dst_h) / static_cast<float>(src_h));
    float new_unpad_w = static_cast<float>(src_w) * r;
    float new_unpad_h = static_cast<float>(src_h) * r;
    float pad_x = (static_cast<float>(dst_w) - new_unpad_w) * 0.5f;
    float pad_y = (static_cast<float>(dst_h) - new_unpad_h) * 0.5f;

    return LetterboxInfo{r, pad_x, pad_y, dst_w, dst_h};
}

} // namespace argus::cv
