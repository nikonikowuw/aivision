#include "postprocessor.hpp"
#include <vector>
#include <string>
#include <cstring>
#include <algorithm>
#include <cmath>
#include "argus/cv/nms.hpp"
#include "argus/cv/letterbox.hpp"

namespace yolov8n {

// 80 COCO Classes
static const char* COCO_CLASSES[] = {
    "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat", "traffic light",
    "fire hydrant", "stop sign", "parking meter", "bench", "bird", "cat", "dog", "horse", "sheep", "cow",
    "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
    "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove", "skateboard", "surfboard",
    "tennis racket", "bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana", "apple",
    "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
    "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone",
    "microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock", "vase", "scissors", "teddy bear",
    "hair drier", "toothbrush"
};

std::vector<argus::cv::DetectionBox> Postprocessor::postprocess(
    const std::vector<float>& net_out,
    float conf_thresh,
    float iou_thresh,
    uint32_t orig_w,
    uint32_t orig_h
) {
    std::vector<argus::cv::DetectionBox> candidates;
    if (net_out.size() != static_cast<size_t>(84 * 8400) || orig_w == 0 || orig_h == 0 ||
        !std::isfinite(conf_thresh) || !std::isfinite(iou_thresh)) return candidates;

    auto lb = argus::cv::compute_letterbox(orig_w, orig_h, 640, 640);

    for (int i = 0; i < 8400; ++i) {
        float max_score = 0.0f;
        int max_cls = -1;
        for (int c = 0; c < 80; ++c) {
            const float score = net_out[(4 + c) * 8400 + i];
            if (std::isfinite(score) && score > max_score) {
                max_score = score;
                max_cls = c;
            }
        }

        if (max_score > conf_thresh && max_cls >= 0) {
            float cx = net_out[0 * 8400 + i];
            float cy = net_out[1 * 8400 + i];
            float w = net_out[2 * 8400 + i];
            float h = net_out[3 * 8400 + i];

            if (!std::isfinite(cx) || !std::isfinite(cy) || !std::isfinite(w) || !std::isfinite(h) ||
                w <= 0.0f || h <= 0.0f) {
                continue;
            }

            argus::cv::NormalizedBBox box{
                .x_min = (cx - w * 0.5f) / 640.0f,
                .y_min = (cy - h * 0.5f) / 640.0f,
                .x_max = (cx + w * 0.5f) / 640.0f,
                .y_max = (cy + h * 0.5f) / 640.0f
            };

            auto unbox = lb.unletterbox_bbox(box, orig_w, orig_h);

            if (!std::isfinite(unbox.x_min) || !std::isfinite(unbox.y_min) ||
                !std::isfinite(unbox.x_max) || !std::isfinite(unbox.y_max) ||
                unbox.x_min >= unbox.x_max || unbox.y_min >= unbox.y_max) {
                continue;
            }

            argus::cv::DetectionBox det{};
            det.class_id = max_cls;
            det.label = (max_cls < 80) ? COCO_CLASSES[max_cls] : "object";
            det.confidence = max_score;
            det.x = unbox.x_min;
            det.y = unbox.y_min;
            det.w = unbox.x_max - unbox.x_min;
            det.h = unbox.y_max - unbox.y_min;
            det.track_id = -1; // Unassigned before tracker
            candidates.push_back(det);
        }
    }

    return argus::cv::nms_filter(candidates, iou_thresh);
}

} // namespace yolov8n
