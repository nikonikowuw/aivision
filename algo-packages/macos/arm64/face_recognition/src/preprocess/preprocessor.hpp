#pragma once

#include "argus/types.h"
#include "argus/cv/letterbox.hpp"
#include <vector>
#include <cstdint>

namespace face_recognition {

/**
 * @brief RGB 图像缓冲持有对象
 */
struct ImageBuffer {
    std::vector<uint8_t> data;
    uint32_t width = 0;
    uint32_t height = 0;
    uint32_t channels = 3;
};

/**
 * @brief 预处理管线上下文
 */
struct PreprocessResult {
    ImageBuffer original_rgb;
    ImageBuffer letterbox_rgb;
    argus::cv::LetterboxInfo letterbox_info;
};

/**
 * @brief 图像预处理器（支持 NV12/CVPixelBuffer -> 原图 RGB -> 640x384 letterbox 及五点相似变换截取）
 */
class Preprocessor {
public:
    Preprocessor() = default;
    ~Preprocessor() = default;

    /**
     * @brief 解码输入帧并生成原图 RGB 及 640x384 letterbox (安防 16:9 优化)
     */
    static bool process_frame(const av_frame_desc* frame, PreprocessResult& out, std::string& error);

    /**
     * @brief 基于五点关键点（原图坐标）直接从原图 RGB 进行相似变换对齐采样，生成 112x112 RGB 脸图
     */
    static bool align_face_112x112(const ImageBuffer& orig_rgb, const float landmarks_10[10],
                                  ImageBuffer& out_face_112, std::string& error);
};

} // namespace face_recognition
