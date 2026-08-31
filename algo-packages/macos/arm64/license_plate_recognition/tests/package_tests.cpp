#include "postprocess/postprocessor.hpp"
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

    std::cout << "[PASS] test_json_serialization" << std::endl;
}

int main() {
    test_ctc_and_color_decoding();
    test_tracking_and_voting();
    test_json_serialization();
    std::cout << "All license_plate_recognition postprocess tests passed!" << std::endl;
    return 0;
}
