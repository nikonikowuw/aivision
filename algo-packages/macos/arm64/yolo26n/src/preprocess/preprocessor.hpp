#pragma once

#include "argus/types.h"
#include <vector>

namespace yolo26n {

class Preprocessor {
public:
    // Create 640x384 RGB CVPixelBuffer from native frame descriptor
    // Returns CVPixelBufferRef (as void*) or nullptr
    // Uses the injected platform image operations when available. A null ops
    // table is reserved for the standalone runner and validator fallback.
    static void* create_input_pixelbuffer(const av_frame_desc* src, const av_image_ops* image_ops,
                                          int target_w = 640, int target_h = 384);
    static void release_pixelbuffer(void* pb);
};

} // namespace yolo26n
