/**
 * @file test_toolkit.cpp
 * @brief SDK Toolkit 图像几何变换、NMS、配置解析与性能分析器单元测试
 */

#include <cstdio>
#include <fstream>
#include <gtest/gtest.h>
#include "aivision/types.h"
#include "aivision/result.h"
#include "aivision/cv/resize.hpp"
#include "aivision/cv/letterbox.hpp"
#include "aivision/cv/nms.hpp"
#include "aivision/utils/env.hpp"
#include "aivision/utils/json.hpp"
#include "aivision/utils/event_id.hpp"
#include "aivision/utils/profiler.hpp"


TEST(CVToolkitTest, LetterboxMapping) {
    auto lb = aivision::cv::compute_letterbox(1920, 1080, 640, 640);
    EXPECT_GT(lb.scale, 0.0f);
    EXPECT_EQ(lb.net_w, 640);
    EXPECT_EQ(lb.net_h, 640);

    aivision::cv::NormalizedBBox detected_box{
        .x_min = 0.1f,
        .y_min = 0.2f,
        .x_max = 0.5f,
        .y_max = 0.6f
    };
    auto orig_box = lb.unletterbox_bbox(detected_box, 1920, 1080);
    EXPECT_GE(orig_box.x_min, 0.0f);
    EXPECT_LE(orig_box.x_max, 1.0f);
    EXPECT_GE(orig_box.y_min, 0.0f);
    EXPECT_LE(orig_box.y_max, 1.0f);
}

TEST(CVToolkitTest, ResizeMappingPreservesNormalizedCoordinates) {
    const auto mapping = aivision::cv::make_resize_mapping(1920, 1080, 640, 640);
    const aivision::cv::NormalizedBBox input{0.1f, 0.2f, 0.5f, 0.6f};
    const auto output = mapping.map_bbox(input);
    EXPECT_FLOAT_EQ(output.x_min, input.x_min);
    EXPECT_FLOAT_EQ(output.y_min, input.y_min);
    EXPECT_FLOAT_EQ(output.x_max, input.x_max);
    EXPECT_FLOAT_EQ(output.y_max, input.y_max);
}

TEST(CVToolkitTest, NMSFilter) {
    std::vector<aivision::cv::DetectionBox> objs = {
        {"person", 0, 0.9f, 0.1f, 0.1f, 0.4f, 0.4f, 1},
        {"person", 0, 0.8f, 0.12f, 0.12f, 0.4f, 0.4f, 2},
        {"car", 1, 0.95f, 0.1f, 0.1f, 0.4f, 0.4f, 3},
        {"person", 0, 0.85f, 0.7f, 0.7f, 0.2f, 0.2f, 4}
    };

    auto nms_res = aivision::cv::nms_filter(objs, 0.45f);
    EXPECT_EQ(nms_res.size(), 3);
    EXPECT_EQ(nms_res[0].label, "car");
    EXPECT_FLOAT_EQ(nms_res[0].confidence, 0.95f);
}

TEST(CVToolkitTest, NMSAcceptsTemporaryInput) {
    const auto result = aivision::cv::nms_filter(
        std::vector<aivision::cv::DetectionBox>{{"person", 0, 0.9f, 0.0f, 0.0f, 0.5f, 0.5f, -1}}, 0.5f);
    ASSERT_EQ(result.size(), 1);
    EXPECT_EQ(result.front().label, "person");
}

TEST(UtilsToolkitTest, EnvReaderHandlesCrLfAndQuotes) {
    const auto path = std::string("env_reader_test.env");
    {
        std::ofstream file(path, std::ios::binary);
        file << "VALUE=\"hello\"\r\n";
    }
    const auto values = aivision::utils::EnvReader::load_file(path);
    ASSERT_NE(values.find("VALUE"), values.end());
    EXPECT_EQ(values.at("VALUE"), "hello");
    std::remove(path.c_str());
}

TEST(UtilsToolkitTest, EventIdAndJson) {
    std::string evt_id = aivision::utils::EventIdGenerator::next_event_id(1);
    EXPECT_TRUE(aivision::utils::EventIdGenerator::is_valid(evt_id));
    EXPECT_FALSE(aivision::utils::EventIdGenerator::is_valid("evt/invalid/id"));

    std::vector<aivision::cv::DetectionBox> objs;
    aivision::cv::DetectionBox obj{};
    obj.label = "person";
    obj.confidence = 0.88f;
    obj.x = 0.1f;
    obj.y = 0.2f;
    obj.w = 0.3f;
    obj.h = 0.4f;
    obj.track_id = 101;
    objs.push_back(obj);

    std::string json_str = aivision::utils::serialize_alarm_json(evt_id, "person_intrude", objs);
    EXPECT_NE(json_str.find("person_intrude"), std::string::npos);
    EXPECT_NE(json_str.find("0.8800"), std::string::npos);
    EXPECT_NE(json_str.find("[0.1000, 0.2000, 0.3000, 0.4000]"), std::string::npos);
}

TEST(UtilsToolkitTest, ProfilerStats) {
    std::vector<double> samples = {10.0, 12.0, 11.0, 15.0, 9.0};
    auto stats = aivision::utils::BenchmarkStats::compute(samples);
    EXPECT_GT(stats.avg_ms, 0.0);
    EXPECT_GT(stats.fps, 0.0);
}
