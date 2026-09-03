#pragma once

#include "argus/types.h"
#include "argus/cv/letterbox.hpp"
#include <vector>
#include <cstdint>

#ifdef __APPLE__
typedef struct __CVBuffer* CVPixelBufferRef;
#else
typedef void* CVPixelBufferRef;
#endif

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
 * @brief 预处理管线上下文 (零全图 RGB 内存分配)
 */
struct PreprocessResult {
    uint32_t orig_width = 0;
    uint32_t orig_height = 0;
    ImageBuffer letterbox_rgb;                  // 目标固定 640x384 (737 KB)
    argus::cv::LetterboxInfo letterbox_info;

    // 只读借用当前帧的 NV12 裸指针与跨距（零拷贝）
    const uint8_t* y_plane = nullptr;
    const uint8_t* uv_plane = nullptr;
    int32_t y_stride = 0;
    int32_t uv_stride = 0;

    // 可选：如果输入为 CVPixelBuffer，保持在当前帧作用域内有效并自动解锁
    CVPixelBufferRef pixel_buffer = nullptr;

    PreprocessResult() = default;
    ~PreprocessResult();

    PreprocessResult(const PreprocessResult&) = delete;
    PreprocessResult& operator=(const PreprocessResult&) = delete;

    PreprocessResult(PreprocessResult&& other) noexcept;
    PreprocessResult& operator=(PreprocessResult&& other) noexcept;
};

/**
 * @brief 图像预处理器（支持 SIMD 硬件加速转码及五点相似变换截取）
 */
class Preprocessor {
public:
    Preprocessor() = default;
    ~Preprocessor() = default;

    /**
     * @brief 解码输入帧并生成 640x384 letterbox 缓冲区 (安防 16:9 优化，彻底废除全图 RGB)
     */
    static bool process_frame(const av_frame_desc* frame, PreprocessResult& out, std::string& error);

    /**
     * @brief 基于五点关键点（原图坐标）直接从 NV12 双平面进行相似变换对齐采样，生成 112x112 RGB 脸图 (零全图 RGB 依赖)
     */
    static bool align_face_112x112(const PreprocessResult& prep_res, const float landmarks_10[10],
                                  ImageBuffer& out_face_112, std::string& error);

    /**
     * @brief 兼容重载：从已有 RGB 图像中进行相似变换对齐（用于单元测试等场景）
     */
    static bool align_face_112x112(const ImageBuffer& orig_rgb, const float landmarks_10[10],
                                  ImageBuffer& out_face_112, std::string& error);
};

} // namespace face_recognition
