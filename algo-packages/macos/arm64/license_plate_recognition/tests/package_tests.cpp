#include "postprocess/postprocessor.hpp"
#include "core/config.hpp"
#include "core/rules.hpp"
#include <cassert>
#include <iostream>
#include <cmath>

using namespace lpr;

void test_ctc_and_color_decoding() {
    Config cfg;
    Postprocessor post(cfg);

    PlateRecOutput rec{};
    rec.char_logits.resize(21 * 78, -10.0f);
    rec.color_logits.resize(5, 0.0f);

    // Set "粤" (index 20) at timestep 1
    rec.char_logits[1 * 78 + 20] = 10.0f;
    // Set "B" (index 53) at timestep 3
    rec.char_logits[3 * 78 + 53] = 10.0f;
    // Set "1" (index 43) at timestep 5
    rec.char_logits[5 * 78 + 43] = 10.0f;
    // Set "2" (index 44) at timestep 7
    rec.char_logits[7 * 78 + 44] = 10.0f;
    // Set "3" (index 45) at timestep 9
    rec.char_logits[9 * 78 + 45] = 10.0f;
    // Set "4" (index 46) at timestep 11
    rec.char_logits[11 * 78 + 46] = 10.0f;
    // Set "5" (index 47) at timestep 13
    rec.char_logits[13 * 78 + 47] = 10.0f;

    // Set color to blue (index 1)
    rec.color_logits[1] = 5.0f;

    std::string text, norm_text, color, type;
    float ocr_conf = 0.0f;

    post.decode_plate_recognition(rec, false, text, norm_text, color, type, ocr_conf);

    assert(text == "粤B12345");
    assert(norm_text == "粤B12345");
    assert(color == "blue");
    assert(type == "standard");
    assert(ocr_conf > 0.9f);

    std::cout << "[PASS] test_ctc_and_color_decoding" << std::endl;
}

void test_tracking_and_voting() {
    Config cfg;
    cfg.voting_window_frames = 3;
    cfg.observation_cooldown_seconds = 10;
    cfg.ocr_confidence_threshold = 0.5f;

    Postprocessor post(cfg);

    PlateObject obj{};
    obj.x_min = 0.2f; obj.y_min = 0.3f; obj.x_max = 0.4f; obj.y_max = 0.4f;
    obj.confidence = 0.95f;
    obj.ocr_confidence = 0.92f;
    obj.plate_text = "粤B12345";
    obj.normalized_text = "粤B12345";
    obj.plate_color = "blue";
    obj.plate_type = "standard";

    int64_t t0 = 1'000'000'000LL;

    // Frame 1
    std::vector<PlateObject> frame1 = {obj};
    auto out1 = post.track_and_vote(frame1, t0);
    assert(out1.size() == 1);
    assert(!out1[0].should_report); // Not mature yet (count = 1 < 3)

    // Frame 2
    auto out2 = post.track_and_vote(frame1, t0 + 40'000'000LL);
    assert(out2.size() == 1);
    assert(!out2[0].should_report); // count = 2 < 3

    // Frame 3
    auto out3 = post.track_and_vote(frame1, t0 + 80'000'000LL);
    assert(out3.size() == 1);
    assert(out3[0].should_report); // count = 3 >= 3, reports!

    // Frame 4 immediately after -> should be suppressed by cooldown
    auto out4 = post.track_and_vote(frame1, t0 + 120'000'000LL);
    assert(out4.size() == 1);
    assert(!out4[0].should_report);

    // Frame after cooldown expires (> 10s)
    auto out5 = post.track_and_vote(frame1, t0 + 11'000'000'000LL);
    assert(out5.size() == 1);
    assert(out5[0].should_report); // Cooldown expired, reports again!

    std::cout << "[PASS] test_tracking_and_voting" << std::endl;
}

void test_json_serialization() {
    Config cfg;
    Postprocessor post(cfg);

    PlateObject obj{};
    obj.x_min = 0.2f; obj.y_min = 0.3f; obj.x_max = 0.4f; obj.y_max = 0.4f;
    obj.confidence = 0.95f;
    obj.ocr_confidence = 0.92f;
    obj.plate_text = "粤B12345";
    obj.normalized_text = "粤B12345";
    obj.plate_color = "blue";
    obj.plate_type = "standard";
    obj.track_id = 42;
    obj.should_report = true;

    std::vector<PlateObject> plates = {obj};
    std::string json_str = post.build_result_json(1001, plates);

    assert(json_str.find("\"schema_version\": 1") != std::string::npos);
    assert(json_str.find("\"algorithm_type\": \"license_plate_recognition\"") != std::string::npos ||
           json_str.find("\"algorithm_type\":\"license_plate_recognition\"") != std::string::npos);
    assert(json_str.find("\"frame_id\": 1001") != std::string::npos ||
           json_str.find("\"frame_id\":1001") != std::string::npos);
    assert(json_str.find("\"plate_text\": \"粤B12345\"") != std::string::npos ||
           json_str.find("\"plate_text\":\"粤B12345\"") != std::string::npos);
    assert(json_str.find("\"plate_color\": \"blue\"") != std::string::npos ||
           json_str.find("\"plate_color\":\"blue\"") != std::string::npos);
    assert(json_str.find("\"track_id\": 42") != std::string::npos ||
           json_str.find("\"track_id\":42") != std::string::npos);
    assert(json_str.find("\"bbox\": [0.2000, 0.3000, 0.2000, 0.1000]") != std::string::npos);
    assert(json_str.find("\"vehicle_bbox\": [") != std::string::npos);

    std::cout << "[PASS] test_json_serialization" << std::endl;
}

void test_config_validation() {
    Config cfg;
    std::string error;
    assert(Config::parse_json("{}", 2, cfg, error));
    assert(cfg.save_plate_crop);
    assert(cfg.allowed_plate_colors.size() == 5);

    assert(Config::parse_json(
        R"({"confidence_threshold":0.7,"allowed_plate_colors":["green"],"save_plate_crop":false})",
        0, cfg, error) == false);
    const std::string valid =
        R"({"confidence_threshold":0.7,"allowed_plate_colors":["green"],"save_plate_crop":false})";
    assert(Config::parse_json(valid.data(), static_cast<uint32_t>(valid.size()), cfg, error));
    assert(cfg.confidence_threshold == 0.7f);
    assert(cfg.allowed_plate_colors.size() == 1 && cfg.allowed_plate_colors[0] == "green");
    assert(!cfg.save_plate_crop);

    const std::string unknown = R"({"unknown":1})";
    assert(!Config::parse_json(unknown.data(), static_cast<uint32_t>(unknown.size()), cfg, error));
    const std::string fraction = R"({"voting_window_frames":1.5})";
    assert(!Config::parse_json(fraction.data(), static_cast<uint32_t>(fraction.size()), cfg, error));

    std::cout << "[PASS] test_config_validation" << std::endl;
}

void test_track_binding_survives_detection_reordering() {
    Config cfg;
    cfg.voting_window_frames = 1;
    cfg.ocr_confidence_threshold = 0.5f;
    Postprocessor post(cfg);

    PlateObject left{};
    left.x_min = 0.1f; left.y_min = 0.2f; left.x_max = 0.2f; left.y_max = 0.3f;
    left.confidence = 0.9f; left.ocr_confidence = 0.9f;
    left.plate_text = "粤A11111"; left.normalized_text = left.plate_text;
    left.plate_color = "blue"; left.plate_type = "standard";

    PlateObject right = left;
    right.x_min = 0.7f; right.x_max = 0.8f;
    right.plate_text = "粤B22222"; right.normalized_text = right.plate_text;

    std::vector<PlateObject> first_frame{left, right};
    auto first = post.track_and_vote(first_frame, 1'000'000'000LL);
    assert(first.size() == 2);
    const int64_t left_id = first[0].track_id;
    const int64_t right_id = first[1].track_id;

    std::vector<PlateObject> second_frame{right, left};
    auto second = post.track_and_vote(second_frame, 1'040'000'000LL);
    assert(second.size() == 2);
    for (const auto& item : second) {
        if (item.track_id == left_id) assert(item.plate_text == "粤A11111");
        if (item.track_id == right_id) assert(item.plate_text == "粤B22222");
    }

    std::cout << "[PASS] test_track_binding_survives_detection_reordering" << std::endl;
}

void test_rule_validation_and_filtering() {
    av_point roi_points[] = {{0.1f, 0.1f}, {0.6f, 0.1f}, {0.6f, 0.6f}, {0.1f, 0.6f}};
    av_rule roi{};
    roi.size = sizeof(av_rule);
    roi.api_version = AV_ALGO_API_VERSION;
    roi.role = AV_RULE_ROI;
    roi.point_count = 4;
    roi.points = roi_points;

    std::vector<RuleState> rules;
    std::string error;
    assert(validate_and_copy_rules(&roi, 1, rules, error));

    PlateObject inside{};
    inside.x_min = 0.2f; inside.y_min = 0.2f; inside.x_max = 0.3f; inside.y_max = 0.3f;
    PlateObject outside = inside;
    outside.x_min = 0.8f; outside.x_max = 0.9f;
    std::vector<PlateObject> objects{inside, outside};
    filter_region_rules(objects, rules);
    assert(objects.size() == 1);
    assert(objects[0].x_min == inside.x_min);

    av_rule invalid = roi;
    invalid.mode = 1;
    assert(!validate_and_copy_rules(&invalid, 1, rules, error));
    std::cout << "[PASS] test_rule_validation_and_filtering" << std::endl;
}

int main() {
    test_ctc_and_color_decoding();
    test_tracking_and_voting();
    test_json_serialization();
    test_config_validation();
    test_track_binding_survives_detection_reordering();
    test_rule_validation_and_filtering();
    std::cout << "All license_plate_recognition postprocess tests passed!" << std::endl;
    return 0;
}
