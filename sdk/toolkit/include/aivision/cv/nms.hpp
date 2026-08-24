#pragma once

#include <vector>
#include <algorithm>
#include <string>
#include "letterbox.hpp"

namespace aivision::cv {

struct DetectionBox {
    std::string label;
    int class_id = 0;
    float confidence = 0.0f;
    float x = 0.0f; // [x, y, w, h] in normalized coordinates
    float y = 0.0f;
    float w = 0.0f;
    float h = 0.0f;
    int64_t track_id = -1;
};

inline float compute_iou_xywh(const DetectionBox& a, const DetectionBox& b) {
    float x1 = std::max(a.x, b.x);
    float y1 = std::max(a.y, b.y);
    float x2 = std::min(a.x + a.w, b.x + b.w);
    float y2 = std::min(a.y + a.h, b.y + b.h);

    float inter_w = std::max(0.0f, x2 - x1);
    float inter_h = std::max(0.0f, y2 - y1);
    float inter_area = inter_w * inter_h;

    float area_a = a.w * a.h;
    float area_b = b.w * b.h;
    float union_area = area_a + area_b - inter_area;
    if (union_area <= 0.0f) return 0.0f;
    return inter_area / union_area;
}

inline std::vector<DetectionBox> nms_filter(std::vector<DetectionBox> candidates, float iou_threshold) {
    std::sort(candidates.begin(), candidates.end(), [](const DetectionBox& a, const DetectionBox& b) {
        return a.confidence > b.confidence;
    });

    std::vector<DetectionBox> result;
    std::vector<bool> suppressed(candidates.size(), false);

    for (size_t i = 0; i < candidates.size(); ++i) {
        if (suppressed[i]) continue;
        result.push_back(candidates[i]);
        for (size_t j = i + 1; j < candidates.size(); ++j) {
            if (suppressed[j]) continue;
            if (candidates[i].class_id == candidates[j].class_id) {
                if (compute_iou_xywh(candidates[i], candidates[j]) > iou_threshold) {
                    suppressed[j] = true;
                }
            }
        }
    }
    return result;
}

} // namespace aivision::cv
