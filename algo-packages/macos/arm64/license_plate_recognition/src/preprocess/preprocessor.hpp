#pragma once

/**
 * @file preprocessor.hpp
 * @brief NV12 图像解码、Letterbox (640x384) 缩放与车牌 4 点透视变换 (320x48) 预处理
 */

#include "argus/types.h"
#include "argus/cv/letterbox.hpp"
#include <vector>
#include <string>
#include <cstdint>

namespace lpr {

struct ImageBuffer {
    uint32_t width = 0;
    uint32_t height = 0;
    uint32_t channels = 3;
    std::vector<uint8_t> data;
};

struct PreprocessResult {
    ImageBuffer original_rgb;
    ImageBuffer letterbox_rgb;
    argus::cv::LetterboxInfo letterbox_info{};
};

class Preprocessor {
public:
    /**
     * @brief 解码 NV12 帧并生成原图 RGB 与 640x384 Letterbox RGB
     */
    static bool process_frame(const av_frame_desc* frame,
                              PreprocessResult& out,
                              std::string& error);

    /**
     * @brief 基于 4 点关键点执行透视变换矫正车牌为 320x48 RGB 图像 (支持双层车牌分割拼接)
     */
    static bool warp_plate_320x48(const ImageBuffer& orig_rgb,
                                  const float landmarks_8[8],
                                  bool is_double_layer,
                                  ImageBuffer& out_plate_320x48,
                                  std::string& error);
};

} // namespace lpr
