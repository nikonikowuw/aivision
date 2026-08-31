#pragma once

#include "argus/types.h"
#include "argus/cv/letterbox.hpp"
#include <vector>
#include <cstdint>
#include <string>

namespace lpr {

struct ImageBuffer {
    std::vector<uint8_t> data;
    uint32_t width = 0;
    uint32_t height = 0;
    uint32_t channels = 3;
};

struct PreprocessResult {
    ImageBuffer original_rgb;
    ImageBuffer letterbox_rgb;
    argus::cv::LetterboxInfo letterbox_info;
};

class Preprocessor {
public:
    Preprocessor() = default;
    ~Preprocessor() = default;

    /**
     * @brief 解码输入帧并生成原图 RGB 及 640x384 letterbox 图像
     */
    static bool process_frame(const av_frame_desc* frame, PreprocessResult& out, std::string& error);

    /**
     * @brief 对 4 点关键点进行透视变换矫正，生成 168x48 RGB 车牌图
     * @param orig_rgb 原图 RGB
     * @param landmarks_8 4 个顶点坐标 (tl_x, tl_y, tr_x, tr_y, br_x, br_y, bl_x, bl_y)
     * @param is_double_layer 是否为双层车牌
     * @param out_plate_168x48 输出 168x48 图像
     */
    static bool warp_plate_168x48(const ImageBuffer& orig_rgb,
                                 const float landmarks_8[8],
                                 bool is_double_layer,
                                 ImageBuffer& out_plate_168x48,
                                 std::string& error);
};

} // namespace lpr
