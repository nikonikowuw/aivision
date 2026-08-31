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
    constexpr int kDetections = 300;
    std::vector<argus::cv::DetectionBox> candidates;
    if (net_out.size() != static_cast<size_t>(kDetections * 6) || orig_w == 0 || orig_h == 0 ||
        !std::isfinite(conf_thresh) || !std::isfinite(iou_thresh)) return candidates;

    auto lb = argus::cv::compute_letterbox(orig_w, orig_h, 640, 384);

    for (int i = 0; i < kDetections; ++i) {
        float x1 = net_out[i * 6 + 0];
        float y1 = net_out[i * 6 + 1];
        float x2 = net_out[i * 6 + 2];
        float y2 = net_out[i * 6 + 3];
        float score = net_out[i * 6 + 4];
        int cls_id = static_cast<int>(std::round(net_out[i * 6 + 5]));

        if (!std::isfinite(score) || score < conf_thresh || cls_id < 0 || cls_id >= 80) {
            continue;
        }

        if (!std::isfinite(x1) || !std::isfinite(y1) || !std::isfinite(x2) || !std::isfinite(y2) ||
            x2 <= x1 || y2 <= y1) {
            continue;
        }

        argus::cv::NormalizedBBox box{
            .x_min = x1 / 640.0f,
            .y_min = y1 / 384.0f,
            .x_max = x2 / 640.0f,
            .y_max = y2 / 384.0f
        };

        auto unbox = lb.unletterbox_bbox(box, orig_w, orig_h);

        if (!std::isfinite(unbox.x_min) || !std::isfinite(unbox.y_min) ||
            !std::isfinite(unbox.x_max) || !std::isfinite(unbox.y_max) ||
            unbox.x_min >= unbox.x_max || unbox.y_min >= unbox.y_max) {
            continue;
        }

        argus::cv::DetectionBox det{};
        det.class_id = cls_id;
        det.label = (cls_id < 80) ? COCO_CLASSES[cls_id] : "object";
        det.confidence = score;
        det.x = unbox.x_min;
        det.y = unbox.y_min;
        det.w = unbox.x_max - unbox.x_min;
        det.h = unbox.y_max - unbox.y_min;
        det.track_id = -1; // Unassigned before tracker
        candidates.push_back(det);
    }

    return candidates;
}

} // namespace yolov8n
