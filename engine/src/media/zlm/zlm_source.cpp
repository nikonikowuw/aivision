#include "aivision/media/media_api.hpp"

#include <atomic>
#include <memory>
#include <mutex>
#include <utility>

#include "Extension/Frame.h"
#include "Player/PlayerProxy.h"

namespace aivision::media {
namespace {

struct ZlmSourceState {
    std::atomic<bool> active{false};
    std::atomic<bool> connected{false};
    std::mutex mutex;
    PacketCallback on_packet;
    StatusCallback on_status;
    mediakit::PlayerProxy::Ptr player;
    mediakit::Track::Ptr video_track;
    mediakit::FrameWriterInterface* delegate = nullptr;
};

void report_status(const std::shared_ptr<ZlmSourceState>& state, const std::string& message, bool error) {
    if (!state->active.load(std::memory_order_acquire)) return;
    StatusCallback callback;
    {
        std::lock_guard<std::mutex> lock(state->mutex);
        callback = state->on_status;
    }
    if (callback) {
        try {
            callback(message, error);
        } catch (...) {
            // User callbacks must not escape the ZLMediaKit event thread.
        }
    }
}

class ZlmMediaSource final : public IMediaSource {
public:
    explicit ZlmMediaSource(std::string id) : id_(std::move(id)) {}

    ~ZlmMediaSource() override {
        stop();
    }

    av_status start(const std::string& rtsp_url, PacketCallback on_packet, StatusCallback on_status) override {
        stop();
        if (rtsp_url.empty() || !on_packet) return AV_ERR_INVALID_ARG;

        auto state = std::make_shared<ZlmSourceState>();
        state->active.store(true);
        state->on_packet = std::move(on_packet);
        state->on_status = std::move(on_status);
        mediakit::MediaTuple tuple{"__defaultVhost__", "aivision", id_, ""};
        auto player = std::make_shared<mediakit::PlayerProxy>(tuple, mediakit::ProtocolOption{});
        state->player = player;
        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            state_ = state;
        }

        const std::weak_ptr<ZlmSourceState> weak_state = state;
        player->setPlayCallbackOnce([weak_state, player](const toolkit::SockException& error) {
            auto state = weak_state.lock();
            if (!state || !state->active.load()) return;
            if (error) {
                state->connected.store(false);
                report_status(state, error.what(), true);
                return;
            }

            auto video_track = player->getTrack(mediakit::TrackVideo, true);
            if (!video_track) {
                state->connected.store(false);
                report_status(state, "video track is unavailable", true);
                return;
            }
            auto delegate = video_track->addDelegate([weak_state](const mediakit::Frame::Ptr& frame) {
                auto state = weak_state.lock();
                if (!state || !state->active.load() || !frame) return false;

                PacketCallback callback;
                {
                    std::lock_guard<std::mutex> lock(state->mutex);
                    callback = state->on_packet;
                }
                if (!callback) return false;

                auto bytes = std::make_shared<std::vector<uint8_t>>(
                    reinterpret_cast<const uint8_t*>(frame->data()),
                    reinterpret_cast<const uint8_t*>(frame->data()) + frame->size());
                EncodedPacket packet;
                packet.storage = bytes;
                packet.data = bytes->data();
                packet.size = bytes->size();
                packet.pts_us = static_cast<int64_t>(frame->pts()) * 1000;
                packet.dts_us = static_cast<int64_t>(frame->dts()) * 1000;
                packet.is_keyframe = frame->keyFrame();
                packet.codec_name = frame->getCodecName();
                try {
                    callback(packet);
                } catch (...) {
                    return false;
                }
                return true;
            });
            {
                std::lock_guard<std::mutex> lock(state->mutex);
                state->video_track = video_track;
                state->delegate = delegate;
            }
            state->connected.store(delegate != nullptr);
            report_status(state, state->connected.load() ? "connected" : "video delegate registration failed",
                          !state->connected.load());
        });
        player->setOnDisconnect([weak_state] {
            if (auto state = weak_state.lock()) {
                state->connected.store(false);
                report_status(state, "disconnected", true);
            }
        });
        player->setOnClose([weak_state](const toolkit::SockException& error) {
            if (auto state = weak_state.lock()) {
                state->connected.store(false);
                report_status(state, error.what(), true);
            }
        });
        player->play(rtsp_url);
        return AV_OK;
    }

    void stop() override {
        std::shared_ptr<ZlmSourceState> state;
        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            state = std::exchange(state_, nullptr);
        }
        if (!state) return;
        state->active.store(false);
        state->connected.store(false);
        mediakit::Track::Ptr video_track;
        mediakit::FrameWriterInterface* delegate = nullptr;
        mediakit::PlayerProxy::Ptr player;
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            video_track = state->video_track;
            delegate = state->delegate;
            player = state->player;
            state->delegate = nullptr;
            state->video_track.reset();
            state->player.reset();
        }
        if (video_track && delegate) video_track->delDelegate(delegate);
        if (player) player->teardown();
    }

    bool is_connected() const override {
        std::shared_ptr<ZlmSourceState> state;
        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            state = state_;
        }
        return state && state->connected.load();
    }

private:
    std::string id_;
    mutable std::mutex state_mutex_;
    std::shared_ptr<ZlmSourceState> state_;
};

class ZlmMediaBackend final : public IMediaBackend {
public:
    std::unique_ptr<IMediaSource> create_source(const std::string& source_id) override {
        return std::make_unique<ZlmMediaSource>(source_id);
    }
    [[nodiscard]] const char* name() const override { return "ZLMediaKit"; }
};

} // namespace

std::shared_ptr<IMediaBackend> create_zlm_backend() {
    return std::make_shared<ZlmMediaBackend>();
}

} // namespace aivision::media
