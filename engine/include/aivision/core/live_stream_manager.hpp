#pragma once

/**
 * @file live_stream_manager.hpp
 * @brief 实时预览流媒体管理器（基于 ZLMediaKit PlayerProxy 的按需拉流、复用与生命周期管理）
 *
 * 核心机制：
 * 1. 管理 HTTP/WebSocket FLV 直播流（端口默认 8080）；
 * 2. 多流独立标识：主码流 `<camera_id>_main`，子码流 `<camera_id>_sub`；
 * 3. 单流多端复用：同一路摄像头的相同码流只建立单个 PlayerProxy；
 * 4. 自动释放：开启 enable_no_reader，无观众观看达到超时（默认 10 秒）后自动释放上游 RTSP 连接；
 * 5. 显式释放：支持通过 stop_preview 显式释放指定的预览流。
 */

#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>

#include "aivision/v1/engine.pb.h"

namespace aivision::core {

class LiveStreamManager {
public:
    static LiveStreamManager& instance();

    /**
     * @brief 启动流媒体服务器（HTTP/WebSocket-FLV 服务）
     * @param port HTTP/WS 服务端口（默认 8080）
     * @param listen_ip 监听 IP（默认 0.0.0.0）
     * @return 是否启动成功
     */
    bool start_server(uint16_t port = 8080, const std::string& listen_ip = "0.0.0.0");

    /**
     * @brief 停止流媒体服务器
     */
    void stop_server();

    /**
     * @brief 获取当前 HTTP/WS 服务端口
     */
    [[nodiscard]] uint16_t get_http_port() const { return http_port_; }

    /**
     * @brief 启动摄像头实时预览（按需创建或复用 PlayerProxy）
     * @param camera_id 摄像头唯一业务 ID (UUID)
     * @param stream_type 码流类型（MAIN / SUB）
     * @param rtsp_url 完整 RTSP URL（可含百分号编码 userinfo）
     * @param stream_path 输出流路径（如 "/live/<camera_id>_main.live.flv"）
     * @param error_message 错误信息（失败时填充）
     * @return 错误码（空表示成功）
     */
    std::string start_preview(const std::string& camera_id,
                              aivision::v1::StreamType stream_type,
                              const std::string& rtsp_url,
                              std::string* stream_path,
                              std::string* error_message);

    /**
     * @brief 显式停止指定摄像头的预览流
     * @param camera_id 摄像头唯一业务 ID
     * @param stream_type 码流类型
     * @return 是否停止成功
     */
    bool stop_preview(const std::string& camera_id, aivision::v1::StreamType stream_type);

    /**
     * @brief 获取指定流当前的活跃状态（供诊断或测试）
     */
    bool is_streaming(const std::string& camera_id, aivision::v1::StreamType stream_type) const;

    /**
     * @brief 重置并清空所有预览流（供单测与销毁）
     */
    void reset();

private:
    LiveStreamManager() = default;
    ~LiveStreamManager();
    LiveStreamManager(const LiveStreamManager&) = delete;
    LiveStreamManager& operator=(const LiveStreamManager&) = delete;

    static std::string make_stream_id(const std::string& camera_id, aivision::v1::StreamType stream_type);

    mutable std::mutex mutex_;
    uint16_t http_port_ = 8080;
    bool server_started_ = false;

    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace aivision::core
