#pragma once

/**
 * @file probe_rtsp.hpp
 * @brief 摄像头按需测活编排（协议注册 + TCP→UDP 回退 + 临时源释放）
 *
 * 职责：
 * 1. 通过协议注册表选择测活适配器；MVP 仅注册 "rtsp"；
 * 2. 按固定顺序（TCP → UDP）逐次尝试，每次创建独立临时媒体源并保证释放；
 * 3. 聚合单次尝试结果与整体结果（对齐 engine.proto ProbeCameraResponse）。
 */

#include <chrono>
#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>

#include "aivision/media/media_api.hpp"

namespace aivision::core {

/// 单次传输尝试结果（对齐 proto ProbeAttempt）
struct ProbeAttempt {
    std::string transport;     ///< "tcp" | "udp"
    std::string failure_code;  ///< 空串表示该次成功
    uint64_t elapsed_ms = 0;
};

/// 整体测活结果（对齐 proto ProbeCameraResponse）
struct CameraProbeResult {
    std::string status;             ///< "success" | "failed"
    std::string failure_code;       ///< status=failed 时的整体稳定码（如 RTSP_*）
    std::vector<ProbeAttempt> attempts;
    std::string selected_transport; ///< 实际成功传输方式（status=success 时有值）
    std::string codec;              ///< 编码格式（H264/H265，可获得时）
    uint32_t width = 0;
    uint32_t height = 0;
    double fps = 0.0;
    uint64_t elapsed_ms = 0;
};

/// 取消回调：返回 true 表示整体测活应停止（返回 RTSP_PROBE_CANCELLED）
using ProbeCancelFn = std::function<bool()>;

/**
 * @brief 执行摄像头测活
 * @param backend 媒体后端（ZLM 临时源工厂）
 * @param protocol 协议名；MVP 仅支持 "rtsp"，否则返回 RTSP_UNSUPPORTED_PROTOCOL
 * @param url 完整 RTSP URL（可含百分号编码 userinfo）
 * @param per_attempt_timeout 每种传输方式的单次有界等待超时（默认 5 秒）
 * @param is_cancelled 可选取消回调；在每次尝试前检查
 * @return 聚合测活结果；每次尝试后临时媒体源均已被释放
 */
CameraProbeResult probe_camera(const std::shared_ptr<media::IMediaBackend>& backend,
                               const std::string& protocol,
                               const std::string& url,
                               std::chrono::milliseconds per_attempt_timeout = std::chrono::seconds(5),
                               const ProbeCancelFn& is_cancelled = {});

} // namespace aivision::core
