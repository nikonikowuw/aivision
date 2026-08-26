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
#include <chrono>
#include <condition_variable>
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

// 将 ZLMediaKit 的播放/断开错误归类为测活稳定失败码。
// 鉴权失败（401/403）在 RtspPlayer 中以 Err_other + 状态码文本上报，
// 此处按状态码文本识别；其余错误按 ZLToolKit ErrCode 分类。
std::string classify_rtsp_error(const toolkit::SockException& error) {
    const std::string message = error.what();
    if (message.find("401") != std::string::npos || message.find("403") != std::string::npos) {
        return "RTSP_AUTH_FAILED";
    }
    switch (error.getErrCode()) {
        case toolkit::Err_timeout:
            return "RTSP_PLAY_TIMEOUT";
        case toolkit::Err_refused:
        case toolkit::Err_reset:
        case toolkit::Err_dns:
        case toolkit::Err_eof:
            return "RTSP_CONNECT_FAILED";
        default:
            return "RTSP_MEDIA_ERROR";
    }
}

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

    ProbeOutcome probe(const std::string& rtsp_url, Transport transport,
                       std::chrono::milliseconds timeout) override {
        ProbeOutcome outcome;
        if (rtsp_url.empty()) {
            outcome.failure_code = "RTSP_MEDIA_ERROR";
            return outcome;
        }

        // 一次性测活状态：回调线程（EventPoller）只置位并通知，阻塞等待在调用方线程。
        struct ProbeState {
            std::mutex mutex;
            std::condition_variable cv;
            bool done = false;               // 探针已结束，忽略后续回调
            bool play_succeeded = false;     // PLAY 已完成（track 已就绪）
            bool first_frame_received = false;
            bool failed = false;
            std::string failure_code;
            std::string codec;
            mediakit::MediaPlayer::Ptr player;
            mediakit::Track::Ptr video_track;
            mediakit::FrameWriterInterface* delegate = nullptr;
        };
        auto state = std::make_shared<ProbeState>();
        auto player_poller = toolkit::EventPollerPool::Instance().getPoller();
        auto player = std::make_shared<mediakit::MediaPlayer>(player_poller);
        const std::weak_ptr<ProbeState> weak_state = state;

        player_poller->sync([&] {
            std::lock_guard<std::mutex> lock(state->mutex);
            state->player = player;
            // 强制等待 track 就绪；按当前尝试的传输方式设置 TCP/UDP。
            (*player)[mediakit::Client::kWaitTrackReady] = true;
            (*player)[mediakit::Client::kRtpType] =
                transport == Transport::UDP ? mediakit::Rtsp::RTP_UDP : mediakit::Rtsp::RTP_TCP;

            player->setOnPlayResult([weak_state](const toolkit::SockException& error) {
                auto st = weak_state.lock();
                if (!st) return;
                mediakit::MediaPlayer::Ptr player;
                {
                    std::lock_guard<std::mutex> lock(st->mutex);
                    if (st->done) return;
                    player = st->player;
                    if (error) {
                        st->failed = true;
                        st->failure_code = classify_rtsp_error(error);
                        st->cv.notify_all();
                        return;
                    }
                    st->play_succeeded = true;
                }
                if (!player) return;
                // track 已就绪；无视频 Track 直接判定失败。
                const auto video_track = player->getTrack(mediakit::TrackVideo, false);
                if (!video_track) {
                    std::lock_guard<std::mutex> lock(st->mutex);
                    if (st->done) return;
                    st->failed = true;
                    st->failure_code = "RTSP_NO_VIDEO_TRACK";
                    st->cv.notify_all();
                    return;
                }
                // 注册首帧委托：收到首个有效编码视频帧即判定成功。
                auto delegate = video_track->addDelegate([weak_state](const mediakit::Frame::Ptr& frame) {
                    if (!frame) return false;
                    auto st = weak_state.lock();
                    if (!st) return false;
                    std::lock_guard<std::mutex> lock(st->mutex);
                    if (st->done || st->first_frame_received) return false;
                    st->first_frame_received = true;
                    st->codec = frame->getCodecName();
                    st->cv.notify_all();
                    return true;
                });
                {
                    std::lock_guard<std::mutex> lock(st->mutex);
                    if (st->done) {
                        if (delegate) video_track->delDelegate(delegate);
                        return;
                    }
                    st->video_track = video_track;
                    st->delegate = delegate;
                }
            });

            player->setOnShutdown([weak_state](const toolkit::SockException& error) {
                auto st = weak_state.lock();
                if (!st) return;
                std::lock_guard<std::mutex> lock(st->mutex);
                if (st->done || st->first_frame_received) return;
                st->failed = true;
                st->failure_code = classify_rtsp_error(error);
                st->cv.notify_all();
            });

            player->play(rtsp_url);
        });

        // 有界等待首个视频帧或失败；超时按已发生阶段归类。
        {
            std::unique_lock<std::mutex> lock(state->mutex);
            state->cv.wait_for(lock, timeout, [&] {
                return state->failed || state->first_frame_received;
            });
            if (state->failed) {
                outcome.failure_code = state->failure_code;
            } else if (state->first_frame_received) {
                outcome.success = true;
                outcome.codec = state->codec;
                const auto video_track =
                    std::dynamic_pointer_cast<mediakit::VideoTrack>(state->video_track);
                if (video_track) {
                    outcome.width = static_cast<uint32_t>(video_track->getVideoWidth());
                    outcome.height = static_cast<uint32_t>(video_track->getVideoHeight());
                    outcome.fps = video_track->getVideoFps();
                }
            } else if (state->play_succeeded) {
                // PLAY 完成、有视频 Track，但超时未收到首帧。
                outcome.failure_code = "RTSP_NO_FIRST_FRAME";
            } else {
                outcome.failure_code = "RTSP_PLAY_TIMEOUT";
            }
        }

        // 无论成败：停止接受回调并释放临时媒体源（先移除 delegate 再 teardown）。
        mediakit::Track::Ptr video_track;
        mediakit::FrameWriterInterface* delegate = nullptr;
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            state->done = true;
            video_track = state->video_track;
            delegate = state->delegate;
            state->video_track.reset();
            state->delegate = nullptr;
            state->player.reset();
        }
        const auto cleanup = [video_track, delegate, player] {
            if (video_track && delegate) video_track->delDelegate(delegate);
            if (player) player->teardown();
        };
        if (player_poller) {
            player_poller->sync(cleanup);
        } else {
            cleanup();
        }
        return outcome;
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
