/**
 * @file test_toolkit.cpp
 * @brief SDK Toolkit 图像几何变换、NMS、配置解析与性能分析器单元测试
 */

#include <cstdio>
#include <fstream>
#include <gtest/gtest.h>
#include "argus/types.h"
#include "argus/result.h"
#include "argus/cv/resize.hpp"
#include "argus/cv/letterbox.hpp"
#include "argus/cv/nms.hpp"
#include "argus/cv/tracker.hpp"
#include "argus/utils/env.hpp"
#include "argus/utils/json.hpp"
#include "argus/utils/event_id.hpp"
#include "argus/utils/profiler.hpp"


TEST(CVToolkitTest, LetterboxMapping) {
    auto lb = argus::cv::compute_letterbox(1920, 1080, 640, 640);
    EXPECT_GT(lb.scale, 0.0f);
    EXPECT_EQ(lb.net_w, 640);
    EXPECT_EQ(lb.net_h, 640);

    argus::cv::NormalizedBBox detected_box{
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
    const auto mapping = argus::cv::make_resize_mapping(1920, 1080, 640, 640);
    const argus::cv::NormalizedBBox input{0.1f, 0.2f, 0.5f, 0.6f};
    const auto output = mapping.map_bbox(input);
    EXPECT_FLOAT_EQ(output.x_min, input.x_min);
    EXPECT_FLOAT_EQ(output.y_min, input.y_min);
    EXPECT_FLOAT_EQ(output.x_max, input.x_max);
    EXPECT_FLOAT_EQ(output.y_max, input.y_max);
}

TEST(CVToolkitTest, NMSFilter) {
    std::vector<argus::cv::DetectionBox> objs = {
        {"person", 0, 0.9f, 0.1f, 0.1f, 0.4f, 0.4f, 1},
        {"person", 0, 0.8f, 0.12f, 0.12f, 0.4f, 0.4f, 2},
        {"car", 1, 0.95f, 0.1f, 0.1f, 0.4f, 0.4f, 3},
        {"person", 0, 0.85f, 0.7f, 0.7f, 0.2f, 0.2f, 4}
    };

    auto nms_res = argus::cv::nms_filter(objs, 0.45f);
    EXPECT_EQ(nms_res.size(), 3);
    EXPECT_EQ(nms_res[0].label, "car");
    EXPECT_FLOAT_EQ(nms_res[0].confidence, 0.95f);
}

TEST(CVToolkitTest, NMSAcceptsTemporaryInput) {
    const auto result = argus::cv::nms_filter(
        std::vector<argus::cv::DetectionBox>{{"person", 0, 0.9f, 0.0f, 0.0f, 0.5f, 0.5f, -1}}, 0.5f);
    ASSERT_EQ(result.size(), 1);
    EXPECT_EQ(result.front().label, "person");
}

TEST(UtilsToolkitTest, EnvReaderHandlesCrLfAndQuotes) {
    const auto path = std::string("env_reader_test.env");
    {
        std::ofstream file(path, std::ios::binary);
        file << "VALUE=\"hello\"\r\n";
    }
    const auto values = argus::utils::EnvReader::load_file(path);
    ASSERT_NE(values.find("VALUE"), values.end());
    EXPECT_EQ(values.at("VALUE"), "hello");
    std::remove(path.c_str());
}

TEST(UtilsToolkitTest, EventIdAndJson) {
    std::string evt_id = argus::utils::EventIdGenerator::next_event_id(1);
    EXPECT_TRUE(argus::utils::EventIdGenerator::is_valid(evt_id));
    EXPECT_FALSE(argus::utils::EventIdGenerator::is_valid("evt/invalid/id"));

    std::vector<argus::cv::DetectionBox> objs;
    argus::cv::DetectionBox obj{};
    obj.label = "person";
    obj.confidence = 0.88f;
    obj.x = 0.1f;
    obj.y = 0.2f;
    obj.w = 0.3f;
    obj.h = 0.4f;
    obj.track_id = 101;
    objs.push_back(obj);
    auto second = obj;
    second.label = "car";
    second.confidence = 0.77f;
    second.track_id = 202;
    objs.push_back(second);

    std::string json_str = argus::utils::serialize_alarm_json(evt_id, "person_intrude", objs);
    EXPECT_NE(json_str.find("person_intrude"), std::string::npos);
    EXPECT_NE(json_str.find("0.8800"), std::string::npos);
    EXPECT_NE(json_str.find("0.7700"), std::string::npos);
    EXPECT_NE(json_str.find("[0.1000, 0.2000, 0.3000, 0.4000]"), std::string::npos);

    argus::utils::ParsedAlarmJson parsed;
    std::string error;
    ASSERT_TRUE(argus::utils::parse_alarm_json(json_str, parsed, error)) << error;
    EXPECT_EQ(parsed.event_id, evt_id);
    EXPECT_EQ(parsed.objects.size(), 2U);
    EXPECT_EQ(parsed.objects[1].label, "car");
}

TEST(UtilsToolkitTest, ProfilerStats) {
    std::vector<double> samples = {10.0, 12.0, 11.0, 15.0, 9.0};
    auto stats = argus::utils::BenchmarkStats::compute(samples);
    EXPECT_GT(stats.avg_ms, 0.0);
    EXPECT_GT(stats.fps, 0.0);
}

TEST(CVToolkitTest, ByteTrackerMaintainsContinuousTrackIdAcrossMotion) {
    argus::cv::ByteTracker tracker(0.35f, 0.10f, 0.30f, 30, 1);

    // 连续移动的人体
    for (int frame = 0; frame < 10; ++frame) {
        float x = 0.10f + frame * 0.02f; // 每帧向右移动 0.02
        std::vector<argus::cv::DetectionBox> dets = {
            {"person", 0, 0.85f, x, 0.20f, 0.15f, 0.40f, -1}
        };
        auto tracked = tracker.update(dets);
        ASSERT_EQ(tracked.size(), 1U);
        EXPECT_EQ(tracked[0].track_id, 1);
        EXPECT_EQ(tracked[0].label, "person");
    }
}

TEST(CVToolkitTest, ByteTrackerRescuesLowConfidenceDetection) {
    argus::cv::ByteTracker tracker(0.35f, 0.10f, 0.30f, 30, 1);

    // Frame 1: 高分检出
    std::vector<argus::cv::DetectionBox> det1 = {
        {"person", 0, 0.85f, 0.20f, 0.20f, 0.15f, 0.40f, -1}
    };
    auto trk1 = tracker.update(det1);
    ASSERT_EQ(trk1.size(), 1U);
    int64_t person_id = trk1[0].track_id;

    // Frame 2: 转身或模糊导致置信度骤降至 0.18 (低于 high_thresh=0.35)
    std::vector<argus::cv::DetectionBox> det2 = {
        {"person", 0, 0.18f, 0.22f, 0.20f, 0.15f, 0.40f, -1}
    };
    auto trk2 = tracker.update(det2);
    ASSERT_EQ(trk2.size(), 1U);
    // Byte 匹配第二阶段应该成功拯救该轨迹，且保持原有 ID
    EXPECT_EQ(trk2[0].track_id, person_id);
}

TEST(CVToolkitTest, ByteTrackerRecoversBriefOcclusionViaKalmanPrediction) {
    argus::cv::ByteTracker tracker(0.35f, 0.10f, 0.30f, 30, 1);

    // 建立稳定轨迹 (带向右速度)
    for (int frame = 0; frame < 5; ++frame) {
        float x = 0.10f + frame * 0.02f;
        std::vector<argus::cv::DetectionBox> dets = {
            {"person", 0, 0.85f, x, 0.20f, 0.15f, 0.40f, -1}
        };
        auto tracked = tracker.update(dets);
        ASSERT_EQ(tracked.size(), 1U);
        EXPECT_EQ(tracked[0].track_id, 1);
    }

    // 发生 2 帧遮挡 (无检测输出)
    tracker.update({});
    tracker.update({});

    // 遮挡结束，在预测位置附近再次出现
    std::vector<argus::cv::DetectionBox> det_after = {
        {"person", 0, 0.80f, 0.24f, 0.20f, 0.15f, 0.40f, -1}
    };
    auto trk_after = tracker.update(det_after);
    ASSERT_EQ(trk_after.size(), 1U);
    // 应该恢复原有 Track ID=1，而不是分配新 ID
    EXPECT_EQ(trk_after[0].track_id, 1);
}

TEST(CVToolkitTest, ByteTrackerMultiPersonNoIdSwitch) {
    argus::cv::ByteTracker tracker(0.35f, 0.10f, 0.30f, 30, 1);

    // 两人并排前行
    for (int frame = 0; frame < 8; ++frame) {
        float x1 = 0.10f + frame * 0.015f;
        float x2 = 0.40f + frame * 0.015f;
        std::vector<argus::cv::DetectionBox> dets = {
            {"person", 0, 0.88f, x1, 0.20f, 0.15f, 0.40f, -1},
            {"person", 0, 0.82f, x2, 0.20f, 0.15f, 0.40f, -1}
        };
        auto tracked = tracker.update(dets);
        ASSERT_EQ(tracked.size(), 2U);
        EXPECT_EQ(tracked[0].track_id, 1);
        EXPECT_EQ(tracked[1].track_id, 2);
    }
}
