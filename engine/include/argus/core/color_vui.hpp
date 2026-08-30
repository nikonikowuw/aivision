#pragma once

/**
 * @file color_vui.hpp
 * @brief H.264 / H.265 SPS VUI（Video Usability Information）色彩空间元数据解析器
 * 
 * 用于从码流的 SPS NALU 中解析色彩基底（Primaries）、传输特性（Transfer）、
 * 矩阵系数（Matrix）以及色彩范围（Full / Limited Range），准确填充 av_color_info。
 */

#include <cstdint>
#include "argus/types.h"

namespace argus::core {

/**
 * @brief SPS VUI 色彩描述信息
 */
struct ColorVUIInfo {
    av_color_primaries primaries = AV_COLOR_PRIM_BT709;  ///< 色彩原色基底
    av_color_transfer  transfer  = AV_COLOR_TRC_BT709;   ///< 光电传输特性（Gamma / Transfer）
    av_color_matrix    matrix    = AV_COLOR_MAT_BT709;   ///< 颜色矩阵系数
    av_color_range     range     = AV_COLOR_RANGE_LIMITED; ///< 量化范围（Full / Limited）
    bool vui_present = false;                            ///< 码流中是否显式携带 VUI 参数
};

/**
 * @brief VUI 解析工具类
 */
class ColorVUIParser {
public:
    /**
     * @brief 解析 H.264 SPS 中的 VUI 色彩元数据
     * @param sps_data SPS NALU 字节流（已去除起始码）
     * @param size 字节流大小
     */
    static ColorVUIInfo parse_h264_sps(const uint8_t* sps_data, size_t size);

    /**
     * @brief 解析 H.265 (HEVC) SPS 中的 VUI 色彩元数据
     * @param sps_data SPS NALU 字节流（已去除起始码）
     * @param size 字节流大小
     */
    static ColorVUIInfo parse_h265_sps(const uint8_t* sps_data, size_t size);
};

} // namespace argus::core

