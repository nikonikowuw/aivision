#include "aivision/media/media_api.hpp"

#include <atomic>
#include <utility>

namespace aivision::media {
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

} // namespace aivision::media
