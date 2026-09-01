#include "preprocess/preprocessor.hpp"
#include <cassert>
#include <cmath>
#include <iostream>

int main() {
    auto lb = argus::cv::compute_letterbox(1920, 1080, 640, 384);
    assert(lb.scale > 0.0f);
    assert(lb.net_w == 640u);
    assert(lb.net_h == 384u);
    assert(std::fabs(lb.pad_x - 0.0f) < 1e-4f);
    assert(std::fabs(lb.pad_y - 12.0f) < 1e-4f);

    argus::cv::NormalizedBBox norm_box{
        .x_min = 0.0f,
        .y_min = 12.0f / 384.0f,
        .x_max = 1.0f,
        .y_max = (12.0f + 360.0f) / 384.0f
    };
    auto unletterboxed = lb.unletterbox_bbox(norm_box, 1920, 1080);
    assert(std::fabs(unletterboxed.x_min - 0.0f) < 1e-3f);
    assert(std::fabs(unletterboxed.y_min - 0.0f) < 1e-3f);
    assert(std::fabs(unletterboxed.x_max - 1.0f) < 1e-3f);
    assert(std::fabs(unletterboxed.y_max - 1.0f) < 1e-3f);

    std::cout << "[PASS] test_preprocess" << std::endl;
    return 0;
}
