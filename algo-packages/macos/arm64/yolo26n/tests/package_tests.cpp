#include "core/config.hpp"
#include "postprocess/postprocessor.hpp"
#include "core/rules.hpp"

#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <limits>
#include <string>
#include <unordered_map>
#include <vector>

namespace {

void require_condition(bool condition) {
    if (!condition) std::abort();
}

} // namespace

int main() {
    {
        yolo26n::InstanceConfig config;
        std::string error;
        require_condition(yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.6,"iou_threshold":0.4})", config, error));
        require_condition(std::fabs(config.confidence_threshold - 0.6f) < 1e-6f);
        require_condition(!yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.6,"unknown":0.4})", config, error));
        require_condition(!yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.6,"iou_threshold":0.4} trailing)", config, error));

        // Test target_classes and custom_alarm_label
        require_condition(yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["person","car"],"custom_alarm_label":"gate_alarm"})",
            config, error));
        require_condition(config.target_classes.size() == 2);
        require_condition(config.target_classes[0] == "person" && config.target_classes[1] == "car");
        require_condition(config.enabled_classes_mask.test(0)); // person
        require_condition(config.enabled_classes_mask.test(2)); // car
        require_condition(!config.enabled_classes_mask.test(1)); // bicycle
        require_condition(!config.enabled_classes_mask.test(16)); // dog
        require_condition(config.custom_alarm_label == "gate_alarm");

        // Test invalid class name
        require_condition(!yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["invalid_coco_class"]})",
            config, error));

        // Test duplicate class
        require_condition(!yolo26n::parse_instance_config(
            R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["person","person"]})",
            config, error));
    }

    {
        constexpr int kDetections = 300;
        std::vector<float> output(kDetections * 6, 0.0f);
        output[0 * 6 + 4] = 0.9f; // score
        output[0 * 6 + 0] = std::numeric_limits<float>::quiet_NaN(); // invalid x1
        require_condition(yolo26n::Postprocessor::postprocess(output, 0.5f, 0.45f, nullptr, 640, 384).empty());

        // Test bitset filtering in postprocessor
        std::vector<float> valid_output(kDetections * 6, 0.0f);
        // Detection 0: person (cls 0), score 0.85, bbox [10, 10, 100, 200]
        valid_output[0 * 6 + 0] = 10.0f;
        valid_output[0 * 6 + 1] = 10.0f;
        valid_output[0 * 6 + 2] = 100.0f;
        valid_output[0 * 6 + 3] = 200.0f;
        valid_output[0 * 6 + 4] = 0.85f;
        valid_output[0 * 6 + 5] = 0.0f; // person

        // Detection 1: dog (cls 16), score 0.88, bbox [200, 200, 300, 300]
        valid_output[1 * 6 + 0] = 200.0f;
        valid_output[1 * 6 + 1] = 200.0f;
        valid_output[1 * 6 + 2] = 300.0f;
        valid_output[1 * 6 + 3] = 300.0f;
        valid_output[1 * 6 + 4] = 0.88f;
        valid_output[1 * 6 + 5] = 16.0f; // dog

        // When mask only allows "person" (cls 0)
        std::bitset<80> person_only_mask{0};
        person_only_mask.set(0);
        auto filtered_person = yolo26n::Postprocessor::postprocess(valid_output, 0.5f, 0.45f, &person_only_mask, 640, 384);
        require_condition(filtered_person.size() == 1);
        require_condition(filtered_person[0].class_id == 0);
        require_condition(filtered_person[0].label == "person");

        // When mask only allows "dog" (cls 16)
        std::bitset<80> dog_only_mask{0};
        dog_only_mask.set(16);
        auto filtered_dog = yolo26n::Postprocessor::postprocess(valid_output, 0.5f, 0.45f, &dog_only_mask, 640, 384);
        require_condition(filtered_dog.size() == 1);
        require_condition(filtered_dog[0].class_id == 16);
        require_condition(filtered_dog[0].label == "dog");

        // When mask allows both person and dog
        std::bitset<80> both_mask{0};
        both_mask.set(0);
        both_mask.set(16);
        auto filtered_both = yolo26n::Postprocessor::postprocess(valid_output, 0.5f, 0.45f, &both_mask, 640, 384);
        require_condition(filtered_both.size() == 2);
    }

    {
        const av_point square[] = {{0.0f, 0.0f}, {1.0f, 0.0f}, {1.0f, 1.0f}, {0.0f, 1.0f}};
        av_rule rule{};
        rule.size = sizeof(rule);
        rule.api_version = AV_ALGO_API_VERSION;
        rule.role = AV_RULE_ROI;
        rule.point_count = 4;
        rule.points = square;
        std::vector<yolo26n::RuleState> copied;
        std::string error;
        require_condition(yolo26n::validate_and_copy_rules(&rule, 1, copied, error));

        const av_point degenerate[] = {{0.0f, 0.0f}, {0.5f, 0.5f}, {1.0f, 1.0f}};
        rule.point_count = 3;
        rule.points = degenerate;
        require_condition(!yolo26n::validate_and_copy_rules(&rule, 1, copied, error));
    }

    {
        yolo26n::RuleState line;
        line.role = AV_RULE_LINE;
        line.points = {{0.5f, 0.0f}, {0.5f, 1.0f}};
        std::vector<yolo26n::RuleState> rules = {line};
        std::unordered_map<int64_t, av_point> previous_points;
        std::unordered_map<int64_t, uint32_t> missed_frames;
        previous_points.emplace(99, av_point{0.1f, 0.1f});
        previous_points.emplace(7, av_point{0.4f, 0.1f});
        std::vector<argus::cv::DetectionBox> objects = {
            {.label = "person", .class_id = 0, .confidence = 0.9f,
             .x = 0.6f, .y = 0.1f, .w = 0.1f, .h = 0.1f, .track_id = 7}
        };
        const auto crossed = yolo26n::apply_rules(rules, previous_points, missed_frames, objects);
        require_condition(crossed.size() == 1);
        require_condition(previous_points.contains(99));
        require_condition(previous_points.contains(7));

        const std::vector<argus::cv::DetectionBox> empty_objects;
        for (uint32_t i = 0; i < yolo26n::kMaxRuleTrackMissedFrames - 1; ++i) {
            (void)yolo26n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        }
        require_condition(previous_points.contains(99));
        (void)yolo26n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        require_condition(!previous_points.contains(99));

        previous_points.clear();
        missed_frames.clear();
        previous_points.emplace(7, av_point{0.4f, 0.1f});
        (void)yolo26n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        const auto crossed_after_gap = yolo26n::apply_rules(rules, previous_points, missed_frames, objects);
        require_condition(crossed_after_gap.size() == 1);
    }

    return 0;
}
