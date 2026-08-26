/**
 * @file test_camera_task.cpp
 * @brief 摄像头拉流、NALU 解析、解码分发与看门狗重连单元测试
 */

#include <gtest/gtest.h>
#include <chrono>
#include "aivision/core/camera_task.hpp"
#include "aivision/platform/mock_platform.hpp"


class DummyMediaSource : public aivision::media::IMediaSource {
public:
    av_status start(const std::string&, aivision::media::PacketCallback on_packet, aivision::media::StatusCallback on_status) override {
        on_packet_ = std::move(on_packet);
        on_status_ = std::move(on_status);
        ++start_count_;
        return AV_OK;
    }
    void stop() override {}
    bool is_connected() const override { return true; }

    aivision::media::ProbeOutcome probe(const std::string&, aivision::media::Transport,
                                        std::chrono::milliseconds) override {
        aivision::media::ProbeOutcome outcome;
        outcome.success = true;
        outcome.codec = "H264";
        outcome.width = 1920;
        outcome.height = 1080;
        outcome.fps = 25.0;
        return outcome;
    }

    void emit_packet(bool is_keyframe, int64_t pts, const std::string& codec = "H264") {
        emit_nal(codec == "H265" ? (is_keyframe ? 0x26 : 0x02) : (is_keyframe ? 0x65 : 0x41),
                 is_keyframe, pts, codec);
    }

    void emit_nal(uint8_t nal_header, bool is_keyframe, int64_t pts, const std::string& codec = "H264") {
        if (on_packet_) {
            uint8_t dummy[5] = {0, 0, 0, 1, nal_header};
            aivision::media::EncodedPacket pkt{
                .data = dummy,
                .size = sizeof(dummy),
                .pts_us = pts,
                .is_keyframe = is_keyframe,
                .codec_name = codec
            };
            on_packet_(pkt);
        }
    }

    void emit_status(const std::string& status, bool is_error) {
        if (on_status_) on_status_(status, is_error);
    }

    uint64_t start_count() const { return start_count_; }

private:
    aivision::media::PacketCallback on_packet_;
    aivision::media::StatusCallback on_status_;
    std::atomic<uint64_t> start_count_{0};
};

class DummyMediaBackend : public aivision::media::IMediaBackend {
public:
    std::unique_ptr<aivision::media::IMediaSource> create_source(const std::string&) override {
        auto src = std::make_unique<DummyMediaSource>();
        last_source_ = src.get();
        return src;
    }
    DummyMediaSource* last_source_ = nullptr;
};

class WatchdogDecoder final : public aivision::platform::IDecoder {
public:
    explicit WatchdogDecoder(std::shared_ptr<std::atomic<int>> reset_count)
        : reset_count_(std::move(reset_count)) {}

    av_status send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool is_keyframe) override {
        return delegate_.send_packet(data, size, pts_us, is_keyframe);
    }

    av_status receive_frame(av_frame_desc* out_frame) override {
        if (stalled_) return AV_ERR_RETRY;
        return delegate_.receive_frame(out_frame);
    }

    void flush() override { delegate_.flush(); }

    void reset() override {
        ++*reset_count_;
        stalled_ = false;
        delegate_.reset();
    }

private:
    aivision::platform::MockDecoder delegate_;
    std::shared_ptr<std::atomic<int>> reset_count_;
    bool stalled_ = true;
};

class WatchdogPlatformAdapter final : public aivision::platform::MockPlatformAdapter {
public:
    explicit WatchdogPlatformAdapter(std::shared_ptr<std::atomic<int>> reset_count)
        : reset_count_(std::move(reset_count)) {}

    std::unique_ptr<aivision::platform::IDecoder> create_decoder(const std::string&) override {
        return std::make_unique<WatchdogDecoder>(reset_count_);
    }

private:
    std::shared_ptr<std::atomic<int>> reset_count_;
};


TEST(CameraTaskTest, MultiInstanceAndIDRGate) {
    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<DummyMediaBackend>();

    aivision::core::CameraTask task("cam-1", "rtsp://127.0.0.1/live/test", adapter, backend);
    EXPECT_EQ(task.start(), AV_OK);

    // Create 2 instances with different target FPS
    auto inst1 = std::make_shared<aivision::core::AlgorithmInstance>("inst-1", "cam-1", "yolo", "1.0.0", 25, "{}", nullptr, nullptr);
    auto inst2 = std::make_shared<aivision::core::AlgorithmInstance>("inst-2", "cam-1", "yolo", "1.0.0", 10, "{}", nullptr, nullptr);

    inst1->init(aivision::core::FramePool::instance().get_frame_ops(), adapter->get_c_image_ops());
    inst2->init(aivision::core::FramePool::instance().get_frame_ops(), adapter->get_c_image_ops());

    task.add_instance(inst1);
    task.add_instance(inst2);

    ASSERT_NE(backend->last_source_, nullptr);

    // 1. Send non-keyframe first -> must be dropped by IDR Gate
    backend->last_source_->emit_packet(false, 1000);
    EXPECT_EQ(task.get_decoded_frames(), 0);

    // Parameter sets must precede the first random-access frame.
    backend->last_source_->emit_nal(0x67, false, 1500);
    backend->last_source_->emit_nal(0x68, false, 1600);
    backend->last_source_->emit_packet(true, 2000);
    // Decode runs off the media callback thread; wait for the worker deterministically.
    for (int i = 0; i < 400 && task.get_decoded_frames() == 0; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(task.get_decoded_frames(), 1);

    // Allow worker threads to pick up frame
    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    EXPECT_GE(inst1->get_processed_frames(), 1);
    EXPECT_GE(inst2->get_processed_frames(), 1);

    task.stop();
    EXPECT_EQ(aivision::core::FramePool::instance().active_frame_count(), 0);
}

TEST(CameraTaskTest, H265RequiresParameterSetsAndRandomAccessFrame) {
    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<DummyMediaBackend>();
    aivision::core::CameraTask task("cam-h265", "rtsp://127.0.0.1/live/h265", adapter, backend);
    ASSERT_EQ(task.start(), AV_OK);
    ASSERT_NE(backend->last_source_, nullptr);

    // Container metadata alone must not open the gate for a non-IRAP H.265 frame.
    backend->last_source_->emit_nal(0x02, true, 1000, "H265");
    std::this_thread::sleep_for(std::chrono::milliseconds(25));
    EXPECT_EQ(task.get_decoded_frames(), 0);

    // An IDR without VPS/SPS/PPS is also rejected.
    backend->last_source_->emit_nal(0x26, true, 2000, "H265");
    std::this_thread::sleep_for(std::chrono::milliseconds(25));
    EXPECT_EQ(task.get_decoded_frames(), 0);

    backend->last_source_->emit_nal(0x40, false, 3000, "H265");
    backend->last_source_->emit_nal(0x42, false, 3100, "H265");
    backend->last_source_->emit_nal(0x44, false, 3200, "H265");
    backend->last_source_->emit_nal(0x26, true, 3300, "H265");
    for (int i = 0; i < 400 && task.get_decoded_frames() == 0; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(task.get_decoded_frames(), 1);
    task.stop();
}
TEST(CameraTaskTest, DecoderWatchdogResetsSilentDecoder) {
    auto reset_count = std::make_shared<std::atomic<int>>(0);
    auto adapter = std::make_shared<WatchdogPlatformAdapter>(reset_count);
    auto backend = std::make_shared<DummyMediaBackend>();
    aivision::core::CameraTask task("cam-watchdog", "rtsp://127.0.0.1/live/watchdog", adapter, backend);
    ASSERT_EQ(task.start(), AV_OK);
    ASSERT_NE(backend->last_source_, nullptr);

    backend->last_source_->emit_nal(0x67, false, 1000);
    backend->last_source_->emit_nal(0x68, false, 1100);
    backend->last_source_->emit_nal(0x65, true, 1200);
    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    EXPECT_EQ(task.get_decoded_frames(), 0);

    std::this_thread::sleep_for(std::chrono::milliseconds(3100));
    task.trigger_watchdog_check();
    for (int i = 0; i < 100 && reset_count->load() == 0; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_GE(reset_count->load(), 1);

    backend->last_source_->emit_nal(0x67, false, 2000);
    backend->last_source_->emit_nal(0x68, false, 2100);
    backend->last_source_->emit_nal(0x65, true, 2200);
    for (int i = 0; i < 400 && task.get_decoded_frames() == 0; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(task.get_decoded_frames(), 1);
    task.stop();
}

TEST(CameraTaskTest, ReconnectsAfterMediaError) {
    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<DummyMediaBackend>();
    aivision::core::CameraTask task("cam-reconnect", "rtsp://127.0.0.1/live/reconnect", adapter, backend);
    ASSERT_EQ(task.start(), AV_OK);
    ASSERT_NE(backend->last_source_, nullptr);
    auto* source = backend->last_source_;
    source->emit_status("disconnected", true);
    for (int i = 0; i < 100 && source->start_count() < 2; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_GE(source->start_count(), 2);
    task.stop();
}
