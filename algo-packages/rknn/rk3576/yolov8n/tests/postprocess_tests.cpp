#include <cassert>
#include <iostream>
#include <vector>
#include "postprocess/postprocessor.hpp"

int main() {
    yolov8n::Postprocessor::set_labels({"person", "bicycle", "car"});
    assert(yolov8n::Postprocessor::get_label(0) == "person");
    assert(yolov8n::Postprocessor::get_label(1) == "bicycle");
    assert(yolov8n::Postprocessor::get_label(2) == "car");
    assert(yolov8n::Postprocessor::get_label(99) == "unknown");

    // Test anchor calculation: 640x384 yields:
    // Stride 8: 80x48 = 3840
    // Stride 16: 40x24 = 960
    // Stride 32: 20x12 = 240
    // Total anchors = 5040
    constexpr int strides[3] = {8, 16, 32};
    int total_anchors = 0;
    for (int s = 0; s < 3; ++s) {
        int gw = 640 / strides[s];
        int gh = 384 / strides[s];
        total_anchors += gw * gh;
    }
    assert(total_anchors == 5040);

    // Verify empty decode
    std::vector<yolov8n::RknnOutputBuffer> empty_outputs;
    auto lb = argus::cv::compute_letterbox(1920, 1080, 640, 384);
    auto boxes = yolov8n::Postprocessor::decode(empty_outputs, lb, 0.5f, 0.45f, 1920, 1080);
    assert(boxes.empty());

    std::cout << "[PASS] test_postprocess" << std::endl;
    return 0;
}
