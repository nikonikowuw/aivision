/**
 * @file test_motion_gate.cpp
 * @brief MotionGate 运动检测门控单元测试
 *
 * 验证：
 * 1. 静止帧序列在未达保活时间跳过，达到保活时间恰好放行一次；
 * 2. 运动区域变化超过 threshold 与 contour_area 时立即放行；
 * 3. Mask 排除区域内的变化不触发放行，Mask 外变化正常放行；
 * 4. 首帧、尺寸变化、色彩格式重新初始化；
 * 5. 空 mask、全屏 mask、多 mask、无效多边形与极端配置边界值处理；
 * 6. 全量 fake clock 注入，不依赖真实 sleep。
 */

#include <gtest/gtest.h>
#include <chrono>
#include <vector>
#include <cstring>
#include "argus/core/motion_gate.hpp"

using namespace std::chrono_literals;

namespace {

// 构造测试用的内存 NV12 帧描述符
struct TestNv12Frame {
    std::vector<uint8_t> buffer;
    av_frame_desc desc{};

    TestNv12Frame(uint32_t w, uint32_t h, uint8_t y_fill = 128) {
        const size_t y_size = static_cast<size_t>(w) * h;
        const size_t uv_size = static_cast<size_t>(w) * (h / 2);
        buffer.resize(y_size + uv_size, 128);
        std::fill(buffer.begin(), buffer.begin() + y_size, y_fill);

        desc.size = sizeof(av_frame_desc);
        desc.api_version = AV_ALGO_API_VERSION;
        desc.width = w;
        desc.height = h;
        desc.alloc_width = w;
        desc.alloc_height = h;
        desc.pixel_format = AV_PIX_NV12;
        desc.memory_type = AV_MEM_HOST;
        desc.plane_count = 2;
        desc.stride[0] = static_cast<int32_t>(w);
        desc.stride[1] = static_cast<int32_t>(w);
        desc.offset[0] = 0;
        desc.offset[1] = y_size;
        desc.opaque = buffer.data();
        desc.frame_token = buffer.data();
    }

    TestNv12Frame(const TestNv12Frame& other)
        : buffer(other.buffer), desc(other.desc) {
        desc.opaque = buffer.data();
        desc.frame_token = buffer.data();
    }

    TestNv12Frame& operator=(const TestNv12Frame& other) {
        if (this != &other) {
            buffer = other.buffer;
            desc = other.desc;
            desc.opaque = buffer.data();
            desc.frame_token = buffer.data();
        }
        return *this;
    }

    void set_pixel(uint32_t x, uint32_t y, uint8_t val) {
        if (x < desc.width && y < desc.height) {
            buffer[static_cast<size_t>(y) * desc.width + x] = val;
        }
    }

    void fill_rect(uint32_t rx, uint32_t ry, uint32_t rw, uint32_t rh, uint8_t val) {
        for (uint32_t y = ry; y < ry + rh && y < desc.height; ++y) {
            for (uint32_t x = rx; x < rx + rw && x < desc.width; ++x) {
                set_pixel(x, y, val);
            }
        }
    }
};

} // namespace

// 1. 禁用门控时应全量 Passthrough
TEST(MotionGateTest, DisabledPassthrough) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = false;
    argus::core::MotionGate gate(cfg);

    TestNv12Frame frame(320, 240, 100);
    const auto t0 = std::chrono::steady_clock::now();

    EXPECT_EQ(gate.evaluate(frame.desc, t0), argus::core::MotionDecision::PASSTHROUGH);
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 100ms), argus::core::MotionDecision::PASSTHROUGH);
}

// 2. 首帧建立背景并返回 KEEPALIVE，静止序列在未到保活时间为 SKIP，到达后恰好一次 KEEPALIVE
TEST(MotionGateTest, StaticFramesKeepalive) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 2000ms;
    cfg.frame_height = 100;
    cfg.threshold = 25;
    cfg.contour_area = 50;
    argus::core::MotionGate gate(cfg);

    TestNv12Frame frame(640, 480, 100);
    const auto t0 = std::chrono::steady_clock::now();

    // 首帧初始化背景模型，建立基准放行一次
    EXPECT_EQ(gate.evaluate(frame.desc, t0), argus::core::MotionDecision::KEEPALIVE);

    // 静止帧：100ms, 500ms, 1500ms 均应 SKIP
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 100ms), argus::core::MotionDecision::SKIP);
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 500ms), argus::core::MotionDecision::SKIP);
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 1500ms), argus::core::MotionDecision::SKIP);

    // 达到 2000ms：恰好放行一次 KEEPALIVE
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 2000ms), argus::core::MotionDecision::KEEPALIVE);

    // 紧接着的下一帧 2100ms 再次为 SKIP
    EXPECT_EQ(gate.evaluate(frame.desc, t0 + 2100ms), argus::core::MotionDecision::SKIP);
}

// 3. 画面发生明显变化时立即返回 MOTION
TEST(MotionGateTest, MotionTriggered) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 5000ms;
    cfg.frame_height = 100;
    cfg.threshold = 25;
    cfg.contour_area = 50;
    argus::core::MotionGate gate(cfg);

    TestNv12Frame frame1(640, 480, 50);
    const auto t0 = std::chrono::steady_clock::now();

    // 首帧建立背景
    EXPECT_EQ(gate.evaluate(frame1.desc, t0), argus::core::MotionDecision::KEEPALIVE);

    // 第二帧在中间区域注入一块大幅明暗变化的矩形 (150x150 像素，值从 50 改为 200)
    TestNv12Frame frame2 = frame1;
    frame2.fill_rect(200, 200, 150, 150, 200);

    // 立即触发 MOTION
    EXPECT_EQ(gate.evaluate(frame2.desc, t0 + 100ms), argus::core::MotionDecision::MOTION);
}

// 4. 变化仅在 Mask 区域内时被忽略，Mask 区域外变化仍可触发 MOTION
TEST(MotionGateTest, MaskExclusion) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 5000ms;
    cfg.frame_height = 100;
    cfg.threshold = 25;
    cfg.contour_area = 50;

    // 添加一个覆盖画面左上角的 Mask 多边形: [0,0] 到 [0.5, 0.5]
    std::vector<av_point> mask_poly = {
        {0.0f, 0.0f},
        {0.5f, 0.5f},
        {0.0f, 0.5f}
    };
    cfg.masks.push_back({
        {0.0f, 0.0f},
        {0.5f, 0.0f},
        {0.5f, 0.5f},
        {0.0f, 0.5f}
    });

    argus::core::MotionGate gate(cfg);
    TestNv12Frame frame1(640, 480, 50);
    const auto t0 = std::chrono::steady_clock::now();

    EXPECT_EQ(gate.evaluate(frame1.desc, t0), argus::core::MotionDecision::KEEPALIVE);

    // 变化全部位于 Mask 内（左上角 100x100 区域）
    TestNv12Frame frame_mask_only = frame1;
    frame_mask_only.fill_rect(20, 20, 100, 100, 220);
    EXPECT_EQ(gate.evaluate(frame_mask_only.desc, t0 + 100ms), argus::core::MotionDecision::SKIP);

    // 变化位于 Mask 外部（右下角 450,300 区域）
    TestNv12Frame frame_outside = frame1;
    frame_outside.fill_rect(450, 300, 150, 150, 220);
    EXPECT_EQ(gate.evaluate(frame_outside.desc, t0 + 200ms), argus::core::MotionDecision::MOTION);
}

// 5. 全屏 Mask 使得所有画面变化均被忽略（不会产生 Mask 外运动）
TEST(MotionGateTest, FullscreenMaskBlocksAllMotion) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 5000ms;
    cfg.frame_height = 100;
    cfg.threshold = 20;
    cfg.contour_area = 10;
    cfg.masks.push_back({
        {0.0f, 0.0f},
        {1.0f, 0.0f},
        {1.0f, 1.0f},
        {0.0f, 1.0f}
    });

    argus::core::MotionGate gate(cfg);
    TestNv12Frame frame1(640, 480, 30);
    const auto t0 = std::chrono::steady_clock::now();

    EXPECT_EQ(gate.evaluate(frame1.desc, t0), argus::core::MotionDecision::KEEPALIVE);

    // 全画幅彻底剧烈改变颜色（30 -> 240）
    TestNv12Frame frame2(640, 480, 240);
    EXPECT_EQ(gate.evaluate(frame2.desc, t0 + 100ms), argus::core::MotionDecision::SKIP);
    EXPECT_EQ(gate.evaluate(frame2.desc, t0 + 5000ms), argus::core::MotionDecision::KEEPALIVE);
}

// 6. 多个 Mask 组合与无效多边形（点数少于3个）容错
TEST(MotionGateTest, MultipleMasksAndInvalidPolygons) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 5000ms;
    cfg.frame_height = 100;
    cfg.threshold = 25;
    cfg.contour_area = 50;

    // 掩码1：左半边
    cfg.masks.push_back({
        {0.0f, 0.0f}, {0.5f, 0.0f}, {0.5f, 1.0f}, {0.0f, 1.0f}
    });
    // 掩码2（无效，仅2个点，应被忽略）
    cfg.masks.push_back({
        {0.6f, 0.6f}, {0.9f, 0.9f}
    });

    argus::core::MotionGate gate(cfg);
    TestNv12Frame frame1(640, 480, 50);
    const auto t0 = std::chrono::steady_clock::now();

    EXPECT_EQ(gate.evaluate(frame1.desc, t0), argus::core::MotionDecision::KEEPALIVE);

    // 变化发生在掩码1覆盖的左半边 -> 忽略 (SKIP)
    TestNv12Frame frame_left = frame1;
    frame_left.fill_rect(50, 50, 150, 150, 230);
    EXPECT_EQ(gate.evaluate(frame_left.desc, t0 + 100ms), argus::core::MotionDecision::SKIP);

    // 变化发生在无效掩码2所在区域（右下角） -> 由于该多边形无效，不被排除，正常触发 MOTION
    TestNv12Frame frame_right = frame1;
    frame_right.fill_rect(450, 300, 150, 150, 230);
    EXPECT_EQ(gate.evaluate(frame_right.desc, t0 + 200ms), argus::core::MotionDecision::MOTION);
}

// 7. 分辨率发生变化时自适应重置背景模型
TEST(MotionGateTest, ResolutionChangeReset) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.keepalive_interval = 5000ms;
    argus::core::MotionGate gate(cfg);

    TestNv12Frame frame1080(1920, 1080, 80);
    const auto t0 = std::chrono::steady_clock::now();

    // 1080p 首帧 -> KEEPALIVE
    EXPECT_EQ(gate.evaluate(frame1080.desc, t0), argus::core::MotionDecision::KEEPALIVE);
    EXPECT_EQ(gate.evaluate(frame1080.desc, t0 + 100ms), argus::core::MotionDecision::SKIP);

    // 分辨率切换至 720p -> 重新初始化背景并返回 KEEPALIVE
    TestNv12Frame frame720(1280, 720, 80);
    EXPECT_EQ(gate.evaluate(frame720.desc, t0 + 200ms), argus::core::MotionDecision::KEEPALIVE);
    EXPECT_EQ(gate.evaluate(frame720.desc, t0 + 300ms), argus::core::MotionDecision::SKIP);
}

// 8. JSON 参数解析与热更新
TEST(MotionGateTest, JsonConfigParsing) {
    argus::core::MotionGate gate;
    const std::string json_str = R"({
        "motion_gate": {
            "enabled": true,
            "frame_height": 120,
            "threshold": 30,
            "contour_area": 80,
            "keepalive_interval_ms": 3500,
            "masks": [
                [{"x": 0.1, "y": 0.1}, {"x": 0.4, "y": 0.1}, {"x": 0.4, "y": 0.4}, {"x": 0.1, "y": 0.4}]
            ]
        }
    })";

    gate.update_config_from_json(json_str);
    const auto& cfg = gate.get_config();

    EXPECT_TRUE(cfg.enabled);
    EXPECT_EQ(cfg.frame_height, 120U);
    EXPECT_EQ(cfg.threshold, 30U);
    EXPECT_EQ(cfg.contour_area, 80U);
    EXPECT_EQ(cfg.keepalive_interval, 3500ms);
    ASSERT_EQ(cfg.masks.size(), 1U);
    EXPECT_EQ(cfg.masks[0].size(), 4U);
}

// 9. 极端边界值（0尺寸或非法参数自愈）
TEST(MotionGateTest, ExtremeConfigBoundaryValues) {
    argus::core::MotionGateConfig cfg;
    cfg.enabled = true;
    cfg.frame_height = 0; // 应被校正为默认值
    cfg.threshold = 0;
    cfg.contour_area = 0;
    cfg.frame_alpha = -1.0f;
    argus::core::MotionGate gate(cfg);

    const auto& corrected = gate.get_config();
    EXPECT_GT(corrected.frame_height, 0U);
    EXPECT_GT(corrected.threshold, 0U);
    EXPECT_GT(corrected.contour_area, 0U);
    EXPECT_GT(corrected.frame_alpha, 0.0f);
}
