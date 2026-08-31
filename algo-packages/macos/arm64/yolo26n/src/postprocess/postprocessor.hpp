#pragma once

#include <vector>
#include <cstdint>
#include <string>
#include "argus/cv/nms.hpp"

namespace yolo26n {

class Postprocessor {
public:
    static std::vector<argus::cv::DetectionBox> postprocess(
        const std::vector<float>& net_out,
        float conf_thresh,
        float iou_thresh,
        uint32_t orig_w = 1920,
        uint32_t orig_h = 1080
    );
};

} // namespace yolo26n
