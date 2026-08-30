/**
 * @file test_color_vui.cpp
 * @brief H.264 / H.265 SPS VUI 色彩空间解析器单元测试
 */

#include <gtest/gtest.h>
#include "argus/core/color_vui.hpp"


TEST(ColorVUITest, FallbackBT709) {
    auto info = argus::core::ColorVUIParser::parse_h264_sps(nullptr, 0);
    EXPECT_FALSE(info.vui_present);
    EXPECT_EQ(info.primaries, AV_COLOR_PRIM_BT709);
    EXPECT_EQ(info.range, AV_COLOR_RANGE_LIMITED);
}
