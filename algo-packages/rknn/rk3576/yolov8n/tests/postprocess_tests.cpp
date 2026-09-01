#include "postprocess/postprocessor.hpp"
#include "core/config.hpp"
#include "argus/cv/letterbox.hpp"
#include <cassert>
#include <cmath>
#include <iostream>
#include <vector>

namespace {

inline int8_t quant_f32(float val, int32_t zp, float scale) {
    float v = val / scale + static_cast<float>(zp);
    v = std::max(-128.0f, std::min(127.0f, v));
    return static_cast<int8_t>(std::round(v));
}

} // namespace

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
    std::bitset<80> all_mask;
    all_mask.set();
    auto lb = argus::cv::compute_letterbox(1920, 1080, 640, 384);
    auto boxes = yolov8n::Postprocessor::decode(empty_outputs, lb, 0.5f, 0.45f, all_mask, 1920, 1080);
    assert(boxes.empty());

    // Construct synthetic 6-branch output
    // Stride 8: grid 80x48 = 3840 anchors. Box: 64 * 3840. Cls: 80 * 3840.
    // Stride 16: grid 40x24 = 960 anchors. Box: 64 * 960. Cls: 80 * 960.
    // Stride 32: grid 20x12 = 240 anchors. Box: 64 * 240. Cls: 80 * 240.
    float scale = 0.05f;
    int32_t zp = 0;

    std::vector<int8_t> s8_box(64 * 3840, 0);
    std::vector<int8_t> s8_cls(80 * 3840, quant_f32(0.01f, zp, scale));
    std::vector<int8_t> s16_box(64 * 960, 0);
    std::vector<int8_t> s16_cls(80 * 960, quant_f32(0.01f, zp, scale));
    std::vector<int8_t> s32_box(64 * 240, 0);
    std::vector<int8_t> s32_cls(80 * 240, quant_f32(0.01f, zp, scale));

    // Anchor 100 on stride 8: set person (cls 0) to 0.85
    int anchor_person = 100;
    s8_cls[0 * 3840 + anchor_person] = quant_f32(0.85f, zp, scale);
    // DFL box: set [4, 4, 4, 4] distribution
    for (int k = 0; k < 4; ++k) {
        // dfl value 4 out of 16
        s8_box[(k * 16 + 4) * 3840 + anchor_person] = quant_f32(5.0f, zp, scale);
    }

    // Anchor 200 on stride 8: set car (cls 2) to 0.88
    int anchor_car = 200;
    s8_cls[2 * 3840 + anchor_car] = quant_f32(0.88f, zp, scale);
    for (int k = 0; k < 4; ++k) {
        s8_box[(k * 16 + 6) * 3840 + anchor_car] = quant_f32(5.0f, zp, scale);
    }

    std::vector<yolov8n::RknnOutputBuffer> mock_6branches(6);
    mock_6branches[0] = {s8_box.data(), s8_box.size(), scale, zp, true};
    mock_6branches[1] = {s8_cls.data(), s8_cls.size(), scale, zp, true};
    mock_6branches[2] = {s16_box.data(), s16_box.size(), scale, zp, true};
    mock_6branches[3] = {s16_cls.data(), s16_cls.size(), scale, zp, true};
    mock_6branches[4] = {s32_box.data(), s32_box.size(), scale, zp, true};
    mock_6branches[5] = {s32_cls.data(), s32_cls.size(), scale, zp, true};

    // Test 1: Person only filter
    std::bitset<80> person_mask{0};
    person_mask.set(0); // person
    auto res_person = yolov8n::Postprocessor::decode(mock_6branches, lb, 0.5f, 0.45f, person_mask, 1920, 1080);
    assert(res_person.size() == 1);
    assert(res_person[0].class_id == 0);
    assert(res_person[0].label == "person");

    // Test 2: Car only filter
    std::bitset<80> car_mask{0};
    car_mask.set(2); // car
    auto res_car = yolov8n::Postprocessor::decode(mock_6branches, lb, 0.5f, 0.45f, car_mask, 1920, 1080);
    assert(res_car.size() == 1);
    assert(res_car[0].class_id == 2);
    assert(res_car[0].label == "car");

    // Test 3: Both mask
    std::bitset<80> both_mask{0};
    both_mask.set(0);
    both_mask.set(2);
    auto res_both = yolov8n::Postprocessor::decode(mock_6branches, lb, 0.5f, 0.45f, both_mask, 1920, 1080);
    assert(res_both.size() == 2);

    // Test 4: Empty mask
    std::bitset<80> empty_mask{0};
    auto res_empty = yolov8n::Postprocessor::decode(mock_6branches, lb, 0.5f, 0.45f, empty_mask, 1920, 1080);
    assert(res_empty.empty());

    std::cout << "[PASS] test_postprocess with INT8 early exit bitset filtering" << std::endl;
    return 0;
}
