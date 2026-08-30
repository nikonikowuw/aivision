/**
 * @file mock_source.cpp
 * @brief 单元测试专用的 Mock 流媒体源与后端实现
 */

#include "argus/media/media_api.hpp"

#include <atomic>
#include <utility>


namespace argus::media {
namespace {

class MockMediaSource final : public IMediaSource {
public:
    av_status start(const std::string&, PacketCallback on_packet, StatusCallback on_status) override {
        on_packet_ = std::move(on_packet);
        on_status_ = std::move(on_status);
        connected_.store(true, std::memory_order_release);
        if (on_status_) on_status_("connected", false);
        return AV_OK;
    }

    void stop() override {
        connected_.store(false, std::memory_order_release);
        on_packet_ = {};
        on_status_ = {};
    }

    bool is_connected() const override {
        return connected_.load(std::memory_order_acquire);
    }

    ProbeOutcome probe(const std::string&, Transport, std::chrono::milliseconds) override {
        // Mock 源恒为连接成功：直接返回首个视频帧成功与固定媒体元数据。
        ProbeOutcome outcome;
        outcome.success = true;
        outcome.codec = "H264";
        outcome.width = 1920;
        outcome.height = 1080;
        outcome.fps = 25.0;
        return outcome;
    }

private:
    PacketCallback on_packet_;
    StatusCallback on_status_;
    std::atomic<bool> connected_{false};
};

class MockMediaBackend final : public IMediaBackend {
public:
    std::unique_ptr<IMediaSource> create_source(const std::string&) override {
        return std::make_unique<MockMediaSource>();
    }
    [[nodiscard]] const char* name() const override { return "mock"; }
};

} // namespace

std::shared_ptr<IMediaBackend> create_mock_backend() {
    return std::make_shared<MockMediaBackend>();
}

} // namespace argus::media
