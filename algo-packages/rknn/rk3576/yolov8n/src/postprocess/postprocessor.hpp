#pragma once

#include "aivision/cv/letterbox.hpp"
#include "aivision/cv/nms.hpp"
#include <cstdint>
#include <vector>
#include <string>

namespace yolov8n {

using DetectionBox = aivision::cv::DetectionBox;

struct RknnOutputBuffer {
    void* data{nullptr};
    size_t size{0};
    float scale{1.0f};
    int32_t zero_point{0};
    bool is_quantized{true};
};

class Postprocessor {
public:
    static std::vector<DetectionBox> decode(
        const std::vector<RknnOutputBuffer>& outputs,
        const aivision::cv::LetterboxInfo& letterbox,
        float conf_threshold,
        float nms_threshold,
        int src_w,
        int src_h
    );

    static void set_labels(const std::vector<std::string>& labels);
    static const std::string& get_label(int class_id);
};

} // namespace yolov8n
