/**
 * @file live_stream_manager.cpp
 * @brief 实时预览流媒体管理器实现（基于 ZLMediaKit PlayerProxy & HttpSession）
 */

#include "argus/core/live_stream_manager.hpp"

#include <chrono>
#include <exception>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>

#include "Common/config.h"
#include "Http/HttpSession.h"
#include "Network/TcpServer.h"
#include "Player/PlayerProxy.h"
#include "Util/logger.h"
#include "argus/core/logging/logger.hpp"

namespace argus::core {

struct LiveStreamEntry {
    std::string stream_id;
    std::string rtsp_url;
    std::shared_ptr<mediakit::PlayerProxy> proxy;
    std::chrono::steady_clock::time_point created_at;
};

struct LiveStreamManager::Impl {
    std::shared_ptr<toolkit::TcpServer> http_server;
    std::unordered_map<std::string, LiveStreamEntry> streams;
};

LiveStreamManager& LiveStreamManager::instance() {
    static LiveStreamManager inst;
    return inst;
}

LiveStreamManager::~LiveStreamManager() {
    if (impl_) {
        impl_->streams.clear();
        impl_->http_server.reset();
    }
}

std::string LiveStreamManager::make_stream_id(const std::string& camera_id, argus::v1::StreamType stream_type) {
    const std::string suffix = (stream_type == argus::v1::STREAM_TYPE_SUB) ? "_sub" : "_main";
    return camera_id + suffix;
}

bool LiveStreamManager::start_server(uint16_t port, const std::string& listen_ip) {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) {
        impl_ = std::make_unique<Impl>();
    }
    if (server_started_ && http_port_ == port) {
        return true;
    }

    try {
        // 配置 ZLMediaKit 全局参数以支持 HTTP/WS-FLV，并启用无读者自动销毁
        toolkit::mINI::Instance()[mediakit::Protocol::kEnableRtmp] = 1;
        toolkit::mINI::Instance()[mediakit::Protocol::kEnableRtsp] = 0;
        toolkit::mINI::Instance()[mediakit::Protocol::kEnableHls] = 0;
        toolkit::mINI::Instance()[mediakit::Protocol::kEnableMP4] = 0;
        toolkit::mINI::Instance()[mediakit::General::kStreamNoneReaderDelayMS] = 10000;
        toolkit::mINI::Instance()[mediakit::Protocol::kAutoClose] = 0;

        auto poller = toolkit::EventPollerPool::Instance().getPoller();
        std::exception_ptr server_error;
        poller->sync([&] {
            try {
                if (impl_->http_server) {
                    impl_->http_server.reset();
                }
                impl_->http_server = std::make_shared<toolkit::TcpServer>();
                impl_->http_server->start<mediakit::HttpSession>(port, listen_ip);
            } catch (...) {
                server_error = std::current_exception();
            }
        });

        if (server_error) {
            std::rethrow_exception(server_error);
        }

        http_port_ = port;
        server_started_ = true;
        LOG_INFO("media.stream", "live_stream.server_started",
                 "Live stream HTTP/WS server started", "",
                 {{"port", std::to_string(port)}, {"ip", listen_ip}});
        return true;
    } catch (const std::exception& ex) {
        LOG_ERROR("media.stream", "live_stream.server_start_failed",
                  "Failed to start Live stream HTTP/WS server", "LIVE_STREAM_SERVER_START_FAILED",
                  {{"error", ex.what()}, {"port", std::to_string(port)}});
        return false;
    }
}

void LiveStreamManager::stop_server() {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) return;

    reset();
    if (impl_->http_server) {
        auto poller = toolkit::EventPollerPool::Instance().getPoller();
        poller->sync([&] {
            impl_->http_server.reset();
        });
    }
    server_started_ = false;
}

std::string LiveStreamManager::start_preview(const std::string& camera_id,
                                            argus::v1::StreamType stream_type,
                                            const std::string& rtsp_url,
                                            std::string* stream_path,
                                            std::string* error_message) {
    if (camera_id.empty() || rtsp_url.empty()) {
        if (error_message) *error_message = "camera_id and rtsp_url must not be empty";
        return "RTSP_INVALID_ARGUMENT";
    }

    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) {
        impl_ = std::make_unique<Impl>();
    }

    const std::string stream_id = make_stream_id(camera_id, stream_type);
    const std::string path = "/live/" + stream_id + ".live.flv";

    if (stream_path) {
        *stream_path = path;
    }

    // 1. 检查当前是否已有相同流存在且 URL 相同
    auto it = impl_->streams.find(stream_id);
    if (it != impl_->streams.end() && it->second.proxy && it->second.rtsp_url == rtsp_url) {
        // _live_status: 0 代表 playing, 1 代表 init/connecting
        const int status = it->second.proxy->getStatus();
        if (status == 0 || status == 1) {
            LOG_DEBUG("media.stream", "live_stream.reused", "Reusing existing live stream proxy", "",
                      {{"camera_id", camera_id}, {"stream_id", stream_id}, {"status", std::to_string(status)}});
            return "";
        }
        // 若底层已断开、失败，销毁旧代理重建
        LOG_WARN("media.stream", "live_stream.stale_proxy", "Existing proxy is closed or failed, recreating", "",
                 {{"camera_id", camera_id}, {"stream_id", stream_id}, {"status", std::to_string(status)}});
        if (it->second.proxy) {
            auto poller = toolkit::EventPollerPool::Instance().getPoller();
            auto proxy = it->second.proxy;
            poller->sync([proxy] {
                proxy->setPlayCallbackOnce(nullptr);
                proxy->setOnClose(nullptr);
                proxy->teardown();
            });
        }
        impl_->streams.erase(it);
    }

    // 2. 若存在但 URL 变更，先销毁旧代理
    if (it != impl_->streams.end()) {
        if (it->second.proxy) {
            auto poller = toolkit::EventPollerPool::Instance().getPoller();
            auto proxy = it->second.proxy;
            poller->sync([proxy] {
                // 显式关闭底层媒体源并触发 PlayerProxy 清理
                proxy->setPlayCallbackOnce(nullptr);
                proxy->setOnClose(nullptr);
                proxy->teardown();
            });
        }
        impl_->streams.erase(it);
    }

    // 3. 创建新的 PlayerProxy
    try {
        mediakit::ProtocolOption option;
        option.enable_hls = false;
        option.enable_mp4 = false;
        option.enable_rtsp = false;
        option.enable_rtmp = true;  // 必须开启 RTMP/FLV 转封装以供 HTTP-FLV / WS-FLV 消费
        option.enable_ts = false;
        option.enable_fmp4 = false;
        option.modify_stamp = mediakit::ProtocolOption::kModifyStampRelative; // 采用源视频流时间戳相对时间戳并矫正跳跃与回退
        option.auto_close = false; // 不直接强杀 MediaSource，允许通过无读者机制或上层平滑管理

        const mediakit::MediaTuple tuple{"__defaultVhost__", "live", stream_id, ""};
        auto poller = toolkit::EventPollerPool::Instance().getPoller();
        std::shared_ptr<mediakit::PlayerProxy> proxy;

        poller->sync([&] {
            // retry_count = 3, rtp_type = RTP_TCP (0)
            proxy = std::make_shared<mediakit::PlayerProxy>(tuple, option, 3, poller, 0);
            (*proxy)[mediakit::Client::kRtpType] = 0; // TCP 优先
            (*proxy)[mediakit::Client::kTimeoutMS] = 10000;
            proxy->setPlayCallbackOnce([camera_id, stream_id](const toolkit::SockException& ex) {
                if (ex) {
                    LOG_WARN("media.stream", "live_stream.play_failed",
                             "Live stream player proxy play callback failed", "",
                             {{"camera_id", camera_id}, {"stream_id", stream_id}, {"error", ex.what()}});
                } else {
                    LOG_INFO("media.stream", "live_stream.play_success",
                             "Live stream player proxy play success", "",
                             {{"camera_id", camera_id}, {"stream_id", stream_id}});
                }
            });
            proxy->setOnClose([camera_id, stream_id](const toolkit::SockException& ex) {
                LOG_WARN("media.stream", "live_stream.on_close",
                         "Live stream player proxy closed", "",
                         {{"camera_id", camera_id}, {"stream_id", stream_id}, {"error", ex.what()}});
                LiveStreamManager::instance().stop_preview(camera_id, 
                    stream_id.ends_with("_sub") ? argus::v1::STREAM_TYPE_SUB : argus::v1::STREAM_TYPE_MAIN);
            });
            proxy->play(rtsp_url);
        });

        LiveStreamEntry entry;
        entry.stream_id = stream_id;
        entry.rtsp_url = rtsp_url;
        entry.proxy = proxy;
        entry.created_at = std::chrono::steady_clock::now();
        impl_->streams[stream_id] = std::move(entry);

        LOG_INFO("media.stream", "live_stream.started", "Started live stream player proxy", "",
                 {{"camera_id", camera_id}, {"stream_id", stream_id}, {"path", path}});
        return "";
    } catch (const std::exception& ex) {
        if (error_message) *error_message = ex.what();
        LOG_ERROR("media.stream", "live_stream.create_failed",
                  "Failed to create live stream player proxy", "LIVE_STREAM_CREATE_FAILED",
                  {{"camera_id", camera_id}, {"error", ex.what()}});
        return "LIVE_STREAM_CREATE_FAILED";
    }
}

bool LiveStreamManager::stop_preview(const std::string& camera_id, argus::v1::StreamType stream_type) {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) return true;

    const std::string stream_id = make_stream_id(camera_id, stream_type);
    auto it = impl_->streams.find(stream_id);
    if (it == impl_->streams.end()) {
        return true;
    }

    if (it->second.proxy) {
        auto poller = toolkit::EventPollerPool::Instance().getPoller();
        auto proxy = it->second.proxy;
        poller->sync([proxy] {
            // 清空回调并主动关闭拉流代理
            proxy->setPlayCallbackOnce(nullptr);
            proxy->setOnClose(nullptr);
            proxy->teardown();
        });
    }

    impl_->streams.erase(it);
    LOG_INFO("media.stream", "live_stream.stopped", "Stopped live stream player proxy", "",
             {{"camera_id", camera_id}, {"stream_id", stream_id}});
    return true;
}

bool LiveStreamManager::is_streaming(const std::string& camera_id, argus::v1::StreamType stream_type) const {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) return false;
    const std::string stream_id = make_stream_id(camera_id, stream_type);
    return impl_->streams.find(stream_id) != impl_->streams.end();
}

void LiveStreamManager::reset() {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!impl_) return;
    try {
        auto poller = toolkit::EventPollerPool::Instance().getPoller();
        poller->sync([this] {
            impl_->streams.clear();
        });
    } catch (...) {
        impl_->streams.clear();
    }
}

} // namespace argus::core
