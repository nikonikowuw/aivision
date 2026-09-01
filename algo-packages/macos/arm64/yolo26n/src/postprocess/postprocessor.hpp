#pragma once

#include <bitset>
#include <cstdint>
#include <string>
#include <vector>
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
    ) {
        return postprocess(net_out, conf_thresh, iou_thresh, nullptr, orig_w, orig_h);
    }

    static std::vector<argus::cv::DetectionBox> postprocess(
        const std::vector<float>& net_out,
        float conf_thresh,
        float iou_thresh,
        const std::bitset<80>* class_mask,
        uint32_t orig_w = 1920,
        uint32_t orig_h = 1080
    );
};

} // namespace yolo26n
