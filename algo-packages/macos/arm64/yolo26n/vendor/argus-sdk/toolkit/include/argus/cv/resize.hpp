#pragma once

#include <cmath>
#include "letterbox.hpp"

namespace argus::cv {

struct ResizeMapping {
    float scale_x;
    float scale_y;

    [[nodiscard]] NormalizedBBox map_bbox(const NormalizedBBox& in) const {
        // Normalized coordinates are invariant under a pure resize. Pixel-space
        // mapping must be performed before normalization, not by scaling these values.
        return in;
    }
};

inline ResizeMapping make_resize_mapping(uint32_t src_w, uint32_t src_h, uint32_t dst_w, uint32_t dst_h) {
    return ResizeMapping{
        .scale_x = (src_w > 0) ? static_cast<float>(dst_w) / static_cast<float>(src_w) : 1.0f,
        .scale_y = (src_h > 0) ? static_cast<float>(dst_h) / static_cast<float>(src_h) : 1.0f
    };
}

} // namespace argus::cv
