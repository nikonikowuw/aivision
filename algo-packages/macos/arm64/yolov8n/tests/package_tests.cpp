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
        yolov8n::InstanceConfig config;
        std::string error;
        require_condition(yolov8n::parse_instance_config(
            R"({"confidence_threshold":0.6,"iou_threshold":0.4})", config, error));
        require_condition(std::fabs(config.confidence_threshold - 0.6f) < 1e-6f);
        require_condition(!yolov8n::parse_instance_config(
            R"({"confidence_threshold":0.6,"unknown":0.4})", config, error));
        require_condition(!yolov8n::parse_instance_config(
            R"({"confidence_threshold":0.6,"iou_threshold":0.4} trailing)", config, error));
    }

    {
        constexpr int kAnchors = 5040;
        std::vector<float> output(84 * kAnchors, 0.0f);
        output[4 * kAnchors] = 0.9f;
        output[0] = std::numeric_limits<float>::quiet_NaN();
        require_condition(yolov8n::Postprocessor::postprocess(output, 0.5f, 0.45f, 640, 384).empty());
    }

    {
        const av_point square[] = {{0.0f, 0.0f}, {1.0f, 0.0f}, {1.0f, 1.0f}, {0.0f, 1.0f}};
        av_rule rule{};
        rule.size = sizeof(rule);
        rule.api_version = AV_ALGO_API_VERSION;
        rule.role = AV_RULE_ROI;
        rule.point_count = 4;
        rule.points = square;
        std::vector<yolov8n::RuleState> copied;
        std::string error;
        require_condition(yolov8n::validate_and_copy_rules(&rule, 1, copied, error));

        const av_point degenerate[] = {{0.0f, 0.0f}, {0.5f, 0.5f}, {1.0f, 1.0f}};
        rule.point_count = 3;
        rule.points = degenerate;
        require_condition(!yolov8n::validate_and_copy_rules(&rule, 1, copied, error));
    }

    {
        yolov8n::RuleState line;
        line.role = AV_RULE_LINE;
        line.points = {{0.5f, 0.0f}, {0.5f, 1.0f}};
        std::vector<yolov8n::RuleState> rules = {line};
        std::unordered_map<int64_t, av_point> previous_points;
        std::unordered_map<int64_t, uint32_t> missed_frames;
        previous_points.emplace(99, av_point{0.1f, 0.1f});
        previous_points.emplace(7, av_point{0.4f, 0.1f});
        std::vector<argus::cv::DetectionBox> objects = {
            {.label = "person", .class_id = 0, .confidence = 0.9f,
             .x = 0.6f, .y = 0.1f, .w = 0.1f, .h = 0.1f, .track_id = 7}
        };
        const auto crossed = yolov8n::apply_rules(rules, previous_points, missed_frames, objects);
        require_condition(crossed.size() == 1);
        require_condition(previous_points.contains(99));
        require_condition(previous_points.contains(7));

        const std::vector<argus::cv::DetectionBox> empty_objects;
        for (uint32_t i = 0; i < yolov8n::kMaxRuleTrackMissedFrames - 1; ++i) {
            (void)yolov8n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        }
        require_condition(previous_points.contains(99));
        (void)yolov8n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        require_condition(!previous_points.contains(99));

        previous_points.clear();
        missed_frames.clear();
        previous_points.emplace(7, av_point{0.4f, 0.1f});
        (void)yolov8n::apply_rules(rules, previous_points, missed_frames, empty_objects);
        const auto crossed_after_gap = yolov8n::apply_rules(rules, previous_points, missed_frames, objects);
        require_condition(crossed_after_gap.size() == 1);
    }

    return 0;
}
