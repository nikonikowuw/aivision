#pragma once

#include "aivision/types.h"
#include "aivision/cv/letterbox.hpp"
#include <cstdint>
#include <vector>

namespace yolov8n {

struct PreparedInput {
    av_image_view view{};
    aivision::cv::LetterboxInfo letterbox{};
    std::vector<uint8_t> host_buffer{};
    bool from_image_ops = false;
};

class Preprocessor {
public:
    static bool prepare_input(const av_frame_desc* frame,
                              const av_image_ops* image_ops,
                              uint32_t net_width,
                              uint32_t net_height,
                              PreparedInput& out);

    static void release_input(PreparedInput& input, const av_image_ops* image_ops);

    // CPU fallback for NV12 -> RGB888 letterbox
    static bool cpu_fallback_nv12_to_rgb(const av_frame_desc* frame,
                                         uint32_t net_width,
                                         uint32_t net_height,
                                         PreparedInput& out);
};

} // namespace yolov8n
