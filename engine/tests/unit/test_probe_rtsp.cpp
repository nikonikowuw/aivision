/**
 * @file test_probe_rtsp.cpp
 * @brief 摄像头测活编排单元测试（协议注册、TCP→UDP 回退、取消、临时源释放）
 *
 * FakeProbeSource 通过共享 FakeProbeState 记录探针调用/释放计数：
 * probe_camera 返回时临时源已被 RAII 销毁，因此测试只能读取共享状态，
 * 不能读取源对象本身（悬垂指针）。
 */

#include <gtest/gtest.h>

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <string>
#include <utility>
#include <vector>

#include "aivision/core/probe_rtsp.hpp"


namespace {

aivision::media::ProbeOutcome make_success(const std::string& codec = "H264", uint32_t width = 1920,
                                           uint32_t height = 1080, double fps = 25.0) {
    aivision::media::ProbeOutcome outcome;
    outcome.success = true;
    outcome.codec = codec;
    outcome.width = width;
    outcome.height = height;
    outcome.fps = fps;
    return outcome;
}

aivision::media::ProbeOutcome make_failure(const std::string& code) {
    aivision::media::ProbeOutcome outcome;
    outcome.failure_code = code;
    return outcome;
}

// 跨源对象生命周期存活的共享计数（源在 probe_camera 返回时已被销毁）。
struct FakeProbeState {
    std::atomic<int> probe_count{0};
    std::atomic<int> stop_count{0};
};

class FakeProbeSource final : public aivision::media::IMediaSource {
public:
    std::function<aivision::media::ProbeOutcome(aivision::media::Transport)> on_probe;
    std::shared_ptr<FakeProbeState> state;

    av_status start(const std::string&, aivision::media::PacketCallback,
                    aivision::media::StatusCallback) override {
        return AV_OK;
    }
    void stop() override { ++state->stop_count; }
    bool is_connected() const override { return false; }

    aivision::media::ProbeOutcome probe(const std::string&, aivision::media::Transport transport,
                                        std::chrono::milliseconds) override {
        ++state->probe_count;
        return on_probe ? on_probe(transport) : aivision::media::ProbeOutcome{};
    }
};

class FakeProbeBackend final : public aivision::media::IMediaBackend {
public:
    std::function<aivision::media::ProbeOutcome(aivision::media::Transport)> on_probe;
    std::shared_ptr<FakeProbeState> state = std::make_shared<FakeProbeState>();
    int created_count = 0;

    std::unique_ptr<aivision::media::IMediaSource> create_source(const std::string&) override {
        ++created_count;
        auto source = std::make_unique<FakeProbeSource>();
        source->state = state;
        source->on_probe = on_probe;
        return source;
    }
    [[nodiscard]] const char* name() const override { return "fake"; }
};

} // namespace

TEST(ProbeRtspTest, SuccessOnTcpWithMetadata) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport) { return make_success(); };

    const auto result = aivision::core::probe_camera(backend, "rtsp", "rtsp://127.0.0.1/live",
                                                     std::chrono::seconds(5));

    EXPECT_EQ(result.status, "success");
    EXPECT_EQ(result.selected_transport, "tcp");
    EXPECT_EQ(result.codec, "H264");
    EXPECT_EQ(result.width, 1920U);
    EXPECT_EQ(result.height, 1080U);
    EXPECT_DOUBLE_EQ(result.fps, 25.0);
    ASSERT_EQ(result.attempts.size(), 1U);
    EXPECT_EQ(result.attempts[0].transport, "tcp");
    EXPECT_TRUE(result.attempts[0].failure_code.empty());
    // 只尝试一次 TCP，临时源在返回前被 stop 释放。
    EXPECT_EQ(backend->created_count, 1);
    EXPECT_EQ(backend->state->probe_count.load(), 1);
    EXPECT_EQ(backend->state->stop_count.load(), 1);
}

TEST(ProbeRtspTest, NoVideoTrackFailsBothTransports) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport) { return make_failure("RTSP_NO_VIDEO_TRACK"); };

    const auto result = aivision::core::probe_camera(backend, "rtsp", "rtsp://127.0.0.1/live");

    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_NO_VIDEO_TRACK");
    ASSERT_EQ(result.attempts.size(), 2U);
    EXPECT_EQ(result.attempts[0].transport, "tcp");
    EXPECT_EQ(result.attempts[0].failure_code, "RTSP_NO_VIDEO_TRACK");
    EXPECT_EQ(result.attempts[1].transport, "udp");
    EXPECT_EQ(result.attempts[1].failure_code, "RTSP_NO_VIDEO_TRACK");
    EXPECT_EQ(backend->created_count, 2);
    EXPECT_EQ(backend->state->probe_count.load(), 2);
    EXPECT_EQ(backend->state->stop_count.load(), 2);
}

TEST(ProbeRtspTest, NoFirstFrameTimeoutFails) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport) { return make_failure("RTSP_NO_FIRST_FRAME"); };

    const auto result = aivision::core::probe_camera(backend, "rtsp", "rtsp://127.0.0.1/live");

    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_NO_FIRST_FRAME");
    EXPECT_EQ(result.attempts.size(), 2U);
    EXPECT_TRUE(result.codec.empty());
}

TEST(ProbeRtspTest, TcpFailThenUdpSuccessFallback) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport transport) {
        return transport == aivision::media::Transport::UDP
                   ? make_success("H265", 1280, 720, 30.0)
                   : make_failure("RTSP_CONNECT_FAILED");
    };

    const auto result = aivision::core::probe_camera(backend, "rtsp", "rtsp://127.0.0.1/live");

    EXPECT_EQ(result.status, "success");
    EXPECT_EQ(result.selected_transport, "udp");
    EXPECT_EQ(result.codec, "H265");
    EXPECT_EQ(result.width, 1280U);
    EXPECT_EQ(result.height, 720U);
    EXPECT_DOUBLE_EQ(result.fps, 30.0);
    ASSERT_EQ(result.attempts.size(), 2U);
    EXPECT_EQ(result.attempts[0].transport, "tcp");
    EXPECT_EQ(result.attempts[0].failure_code, "RTSP_CONNECT_FAILED");
    EXPECT_EQ(result.attempts[1].transport, "udp");
    EXPECT_TRUE(result.attempts[1].failure_code.empty());
    EXPECT_EQ(backend->created_count, 2);
    EXPECT_EQ(backend->state->stop_count.load(), 2);
}

TEST(ProbeRtspTest, BothTransportsFailUsesLastFailureCode) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport transport) {
        return transport == aivision::media::Transport::TCP
                   ? make_failure("RTSP_CONNECT_FAILED")
                   : make_failure("RTSP_PLAY_TIMEOUT");
    };

    const auto result = aivision::core::probe_camera(backend, "rtsp", "rtsp://127.0.0.1/live");

    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_PLAY_TIMEOUT");
    ASSERT_EQ(result.attempts.size(), 2U);
    EXPECT_EQ(result.attempts[0].failure_code, "RTSP_CONNECT_FAILED");
    EXPECT_EQ(result.attempts[1].failure_code, "RTSP_PLAY_TIMEOUT");
}

TEST(ProbeRtspTest, CancelBeforeAnyAttemptReturnsCancelled) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport) { return make_success(); };

    const auto result = aivision::core::probe_camera(
        backend, "rtsp", "rtsp://127.0.0.1/live", std::chrono::seconds(5),
        [] { return true; });

    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_PROBE_CANCELLED");
    EXPECT_TRUE(result.attempts.empty());
    EXPECT_EQ(backend->created_count, 0);
    EXPECT_EQ(backend->state->probe_count.load(), 0);
}

TEST(ProbeRtspTest, CancelBetweenAttemptsReleasesSource) {
    auto backend = std::make_shared<FakeProbeBackend>();
    backend->on_probe = [](aivision::media::Transport) { return make_failure("RTSP_CONNECT_FAILED"); };
    int cancel_calls = 0;
    const auto is_cancelled = [&] { return ++cancel_calls >= 2; };

    const auto result = aivision::core::probe_camera(
        backend, "rtsp", "rtsp://127.0.0.1/live", std::chrono::seconds(5), is_cancelled);

    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_PROBE_CANCELLED");
    ASSERT_EQ(result.attempts.size(), 1U);
    EXPECT_EQ(result.attempts[0].transport, "tcp");
    // TCP 尝试的临时源在取消返回前已被释放。
    EXPECT_EQ(backend->created_count, 1);
    EXPECT_EQ(backend->state->probe_count.load(), 1);
    EXPECT_EQ(backend->state->stop_count.load(), 1);
}

TEST(ProbeRtspTest, UnsupportedProtocol) {
    auto backend = std::make_shared<FakeProbeBackend>();
    const auto result = aivision::core::probe_camera(backend, "onvif", "rtsp://127.0.0.1/live");
    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_UNSUPPORTED_PROTOCOL");
    EXPECT_TRUE(result.attempts.empty());
    EXPECT_EQ(backend->created_count, 0);
}

TEST(ProbeRtspTest, NullBackendReturnsMediaError) {
    const auto result = aivision::core::probe_camera(nullptr, "rtsp", "rtsp://127.0.0.1/live");
    EXPECT_EQ(result.status, "failed");
    EXPECT_EQ(result.failure_code, "RTSP_MEDIA_ERROR");
}
