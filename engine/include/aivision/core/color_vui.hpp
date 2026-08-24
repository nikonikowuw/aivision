#pragma once

#include <cstdint>
#include "aivision/types.h"

namespace aivision::core {

struct ColorVUIInfo {
    av_color_primaries primaries = AV_COLOR_PRIM_BT709;
    av_color_transfer  transfer  = AV_COLOR_TRC_BT709;
    av_color_matrix    matrix    = AV_COLOR_MAT_BT709;
    av_color_range     range     = AV_COLOR_RANGE_LIMITED;
    bool vui_present = false;
};

class ColorVUIParser {
public:
    static ColorVUIInfo parse_h264_sps(const uint8_t* sps_data, size_t size);
    static ColorVUIInfo parse_h265_sps(const uint8_t* sps_data, size_t size);
};

} // namespace aivision::core
