/**
 * @file probe_rtsp.cpp
 * @brief 摄像头按需测活编排实现
 *
 * 核心流程：
 * 1. 协议注册表查找（MVP 仅支持 "rtsp"，未知协议返回 RTSP_UNSUPPORTED_PROTOCOL）；
 * 2. 按固定顺序（TCP → UDP）逐次创建临时媒体源并执行单次有界测活；
 * 3. 无论单次成败均通过 RAII 释放临时源，避免留下上游连接；
 * 4. 收到首帧即成功；全部尝试失败时以最后一次失败码作为整体失败码。
 */

#include "argus/core/probe_rtsp.hpp"

#include <chrono>
#include <utility>

namespace argus::core {
namespace {

// 单次尝试的媒体源 RAII 释放守卫：任何退出路径都保证 stop + 析构。
class SourceGuard {
public:
    explicit SourceGuard(std::unique_ptr<media::IMediaSource> source)
        : source_(std::move(source)) {}
    ~SourceGuard() {
        if (source_) {
            source_->stop();
            source_.reset();
        }
    }
    SourceGuard(const SourceGuard&) = delete;
    SourceGuard& operator=(const SourceGuard&) = delete;

    media::IMediaSource* get() const { return source_.get(); }

private:
    std::unique_ptr<media::IMediaSource> source_;
};

} // namespace

CameraProbeResult probe_camera(const std::shared_ptr<media::IMediaBackend>& backend,
                               const std::string& protocol,
                               const std::string& url,
                               std::chrono::milliseconds per_attempt_timeout,
                               const ProbeCancelFn& is_cancelled) {
    CameraProbeResult result;
    const auto start = std::chrono::steady_clock::now();

    // 协议注册表：MVP 仅支持 RTSP。
    if (protocol != "rtsp") {
        result.status = "failed";
        result.failure_code = "RTSP_UNSUPPORTED_PROTOCOL";
        result.elapsed_ms = static_cast<uint64_t>(
            std::chrono::duration_cast<std::chrono::milliseconds>(
                std::chrono::steady_clock::now() - start).count());
        return result;
    }
    if (!backend) {
        result.status = "failed";
        result.failure_code = "RTSP_MEDIA_ERROR";
        result.elapsed_ms = static_cast<uint64_t>(
            std::chrono::duration_cast<std::chrono::milliseconds>(
                std::chrono::steady_clock::now() - start).count());
        return result;
    }

    static const char* const kTransports[] = {"tcp", "udp"};
    for (const char* transport : kTransports) {
        if (is_cancelled && is_cancelled()) {
            result.status = "failed";
            result.failure_code = "RTSP_PROBE_CANCELLED";
            break;
        }
        const auto attempt_start = std::chrono::steady_clock::now();
        SourceGuard guard(backend->create_source("camera-probe"));
        if (!guard.get()) {
            result.status = "failed";
            result.failure_code = "RTSP_MEDIA_ERROR";
            result.elapsed_ms = static_cast<uint64_t>(
                std::chrono::duration_cast<std::chrono::milliseconds>(
                    std::chrono::steady_clock::now() - start).count());
            return result;
        }
        const media::Transport transport_enum =
            std::string(transport) == "udp" ? media::Transport::UDP : media::Transport::TCP;
        const media::ProbeOutcome outcome = guard.get()->probe(url, transport_enum, per_attempt_timeout);

        ProbeAttempt attempt;
        attempt.transport = transport;
        attempt.failure_code = outcome.failure_code;
        attempt.elapsed_ms = static_cast<uint64_t>(
            std::chrono::duration_cast<std::chrono::milliseconds>(
                std::chrono::steady_clock::now() - attempt_start).count());
        result.attempts.push_back(std::move(attempt));

        if (outcome.success) {
            result.status = "success";
            result.selected_transport = transport;
            result.codec = outcome.codec;
            result.width = outcome.width;
            result.height = outcome.height;
            result.fps = outcome.fps;
            break;
        }
    }

    if (result.status != "success") {
        result.status = "failed";
        // 取消等显式失败码优先保留；其余情况取最后一次尝试的失败码。
        if (result.failure_code.empty()) {
            result.failure_code = result.attempts.empty() ? "RTSP_MEDIA_ERROR"
                                                          : result.attempts.back().failure_code;
            if (result.failure_code.empty()) result.failure_code = "RTSP_MEDIA_ERROR";
        }
    }
    result.elapsed_ms = static_cast<uint64_t>(
        std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - start).count());
    return result;
}

} // namespace argus::core
