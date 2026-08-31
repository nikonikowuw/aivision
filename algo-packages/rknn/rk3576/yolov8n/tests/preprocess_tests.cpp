#include <cassert>
#include <iostream>
#include "preprocess/preprocessor.hpp"

int main() {
    auto lb = argus::cv::compute_letterbox(1920, 1080, 640, 384);
    assert(lb.scale > 0.0f);
    assert(lb.net_w == 640u);
    assert(lb.net_h == 384u);
    std::cout << "[PASS] test_preprocess" << std::endl;
    return 0;
}
