#include "argus/algo.h"
#include "core/config.hpp"
#include "core/rules.hpp"
#include <cassert>
#include <cmath>
#include <iostream>
#include <unordered_map>
#include <vector>

void test_package_lifecycle() {
    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    assert(abi != nullptr);

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = PACKAGE_ROOT_DIR;
    lib_args.platform_id = "rk3576-rknn";

    av_algo_library lib = nullptr;
    int ret = abi->library_open(&lib_args, &lib);
    if (ret != AV_OK) {
        std::cerr << "library_open failed with code " << ret << std::endl;
        char err_buf[256] = {0};
        abi->last_error(lib, err_buf, sizeof(err_buf));
        std::cerr << "last error: " << err_buf << std::endl;
    }
    assert(ret == AV_OK);
    assert(lib != nullptr);

    av_algo_library_info info{};
    info.size = sizeof(info);
    info.api_version = AV_ALGO_API_VERSION;
    ret = abi->library_query(lib, &info);
    if (ret != AV_OK) {
        std::cerr << "library_query failed with code " << ret << std::endl;
    }
    assert(ret == AV_OK);
    assert(std::string(info.algorithm_id) == "general_detection");
    assert(std::string(info.alarm_type_id) == "object_detect");

    abi->library_close(lib);
    std::cout << "[PASS] test_package_lifecycle" << std::endl;
}

void test_config_parsing() {
    yolov8n::InstanceConfig config;
    std::string error;

    // Test basic parsing
    assert(yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.6,"iou_threshold":0.4})", config, error));
    assert(std::fabs(config.confidence_threshold - 0.6f) < 1e-6f);
    assert(std::fabs(config.iou_threshold - 0.4f) < 1e-6f);
    assert(!yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.6,"unknown":0.4})", config, error));
    assert(!yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.6,"iou_threshold":0.4} trailing)", config, error));

    // Test target_classes and custom_alarm_label
    assert(yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["person","car"],"custom_alarm_label":"gate_alarm"})",
        config, error));
    assert(config.target_classes.size() == 2);
    assert(config.target_classes[0] == "person" && config.target_classes[1] == "car");
    assert(config.enabled_classes_mask.test(0)); // person
    assert(config.enabled_classes_mask.test(2)); // car
    assert(!config.enabled_classes_mask.test(1)); // bicycle
    assert(!config.enabled_classes_mask.test(16)); // dog
    assert(config.custom_alarm_label == "gate_alarm");

    // Test invalid class name
    assert(!yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["invalid_coco_class"]})",
        config, error));

    // Test duplicate class
    assert(!yolov8n::parse_instance_config(
        R"({"confidence_threshold":0.5,"iou_threshold":0.45,"target_classes":["person","person"]})",
        config, error));

    std::cout << "[PASS] test_config_parsing" << std::endl;
}

void test_rules_and_tracking() {
    // ROI rule test
    const av_point square[] = {{0.0f, 0.0f}, {1.0f, 0.0f}, {1.0f, 1.0f}, {0.0f, 1.0f}};
    av_rule rule{};
    rule.size = sizeof(rule);
    rule.api_version = AV_ALGO_API_VERSION;
    rule.role = AV_RULE_ROI;
    rule.point_count = 4;
    rule.points = square;
    std::vector<yolov8n::RuleState> copied;
    std::string error;
    assert(yolov8n::validate_and_copy_rules(&rule, 1, copied, error));

    // Degenerate polygon test
    const av_point degenerate[] = {{0.0f, 0.0f}, {0.5f, 0.5f}, {1.0f, 1.0f}};
    rule.point_count = 3;
    rule.points = degenerate;
    assert(!yolov8n::validate_and_copy_rules(&rule, 1, copied, error));

    // Line crossing rule test
    yolov8n::RuleState line;
    line.role = AV_RULE_LINE;
    line.mode = AV_LINE_DIR_BOTH;
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
    assert(crossed.size() == 1);
    assert(previous_points.contains(99));
    assert(previous_points.contains(7));

    std::cout << "[PASS] test_rules_and_tracking" << std::endl;
}

int main() {
    test_package_lifecycle();
    test_config_parsing();
    test_rules_and_tracking();
    std::cout << "All Package tests passed!" << std::endl;
    return 0;
}
