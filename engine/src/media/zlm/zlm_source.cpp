/**
 * @file zlm_source.cpp
 * @brief 基于 ZLMediaKit 的 RTSP 流客户端媒体源实现
 * 
 * 管理 MediaPlayer 实例与 EventPoller 线程同步，
 * 实现 TCP 模式拉流、视频 Track 委托监听（FrameWriterInterface）、
 * 原始帧包（EncodedPacket）组装与连接中断/恢复事件广播。
 */

#include "aivision/media/media_api.hpp"

#include <atomic>
#include <memory>
#include <mutex>
#include <utility>
#include <vector>

#include "Common/config.h"
#include "Extension/Frame.h"
#include "Player/MediaPlayer.h"
#include "Rtsp/Rtsp.h"


namespace aivision::media {
namespace {

struct ZlmSourceState {
    std::atomic<bool> active{false};
    std::atomic<bool> connected{false};
    std::mutex mutex;
    PacketCallback on_packet;
    StatusCallback on_status;
    mediakit::MediaPlayer::Ptr player;
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
        auto player_poller = toolkit::EventPollerPool::Instance().getPoller();
        auto player = std::make_shared<mediakit::MediaPlayer>(player_poller);
        state->player = player;
        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            state_ = state;
        }

        const std::weak_ptr<ZlmSourceState> weak_state = state;
        player_poller->sync([&] {
            if (!state->active.load(std::memory_order_acquire)) return;
            // 强制等待音视频 Track 准备就绪并强制使用 TCP 传输（规避 UDP 丢包导致花屏）
            (*player)[mediakit::Client::kWaitTrackReady] = true;
            (*player)[mediakit::Client::kRtpType] = mediakit::Rtsp::RTP_TCP;

            // 监听播放开始事件
            player->setOnPlayResult([weak_state](const toolkit::SockException& error) {
                auto state = weak_state.lock();
                if (!state || !state->active.load()) return;

                mediakit::MediaPlayer::Ptr player;
                {
                    std::lock_guard<std::mutex> lock(state->mutex);
                    player = state->player;
                }
                if (!player) return;

                if (error) {
                    state->connected.store(false);
                    report_status(state, error.what(), true);
                    return;
                }

                auto video_track = player->getTrack(mediakit::TrackVideo, false);
                if (!video_track) {
                    state->connected.store(false);
                    report_status(state, "video track is unavailable", true);
                    return;
                }

                // 注册视频帧回调委托，将 ZLM Frame 结构体转换封装为 aivision EncodedPacket
                auto delegate = video_track->addDelegate([weak_state](const mediakit::Frame::Ptr& frame) {
                    if (!frame) return false;
                    auto state = weak_state.lock();
                    if (!state || !state->active.load()) return false;

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
                bool keep_delegate = false;
                {
                    std::lock_guard<std::mutex> lock(state->mutex);
                    if (state->active.load()) {
                        state->video_track = video_track;
                        state->delegate = delegate;
                        keep_delegate = true;
                    }
                }
                if (!keep_delegate) {
                    if (delegate) video_track->delDelegate(delegate);
                    return;
                }
                state->connected.store(delegate != nullptr);
                report_status(state, state->connected.load() ? "connected" : "video delegate registration failed",
                              !state->connected.load());
            });

            // 监听网络连接断开事件
            player->setOnShutdown([weak_state](const toolkit::SockException& error) {
                if (auto state = weak_state.lock()) {
                    state->connected.store(false);
                    report_status(state, error.what(), true);
                }
            });
            player->setOnResume([weak_state] {
                if (auto state = weak_state.lock()) {
                    state->connected.store(true);
                    report_status(state, "resumed", false);
                }
            });
            player->play(rtsp_url);
        });
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
        mediakit::MediaPlayer::Ptr player;
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            video_track = state->video_track;
            delegate = state->delegate;
            player = state->player;
            state->delegate = nullptr;
            state->video_track.reset();
            state->player.reset();
        }
        auto player_poller = player ? player->getPoller() : nullptr;
        auto cleanup = [video_track, delegate, player] {
            if (video_track && delegate) video_track->delDelegate(delegate);
            if (player) player->teardown();
        };
        if (player_poller) {
            player_poller->sync(cleanup);
        } else {
            cleanup();
        }
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
