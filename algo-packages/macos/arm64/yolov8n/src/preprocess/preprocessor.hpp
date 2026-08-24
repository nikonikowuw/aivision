#pragma once

#include "aivision/types.h"
#include <vector>

namespace yolov8n {

class Preprocessor {
public:
    // Create 640x640 RGB CVPixelBuffer from native frame descriptor
    // Returns CVPixelBufferRef (as void*) or nullptr
    // Uses the injected platform image operations when available. A null ops
    // table is reserved for the standalone runner and validator fallback.
    static void* create_input_pixelbuffer(const av_frame_desc* src, const av_image_ops* image_ops,
                                          int target_w = 640, int target_h = 640);
    static void release_pixelbuffer(void* pb);
};

} // namespace yolov8n
