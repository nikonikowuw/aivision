#pragma once

/**
 * @file camera_task.hpp
 * @brief 单摄像头拉流、硬解分发与健康看门狗任务管理
 * 
 * 职责包括：
 * 1. 管理单个 RTSP 流生命周期（媒体连接、重连退避、流状态监控）；
 * 2. H.264/H.265 NALU 队列缓冲与平台硬件解码器对接；
 * 3. 关联并扇出分发解码后的 av_frame_desc 给绑定的多个 AlgorithmInstance；
 * 4. 内置独立看门狗线程（Watchdog），检测包到达超时、解码卡顿并自动触发恢复。
 */

#include <string>
#include <vector>
#include <memory>
#include <thread>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <chrono>
#include "argus/types.h"
#include "argus/media/media_api.hpp"
#include "argus/platform/platform_api.hpp"
#include "argus/core/algo_instance.hpp"
#include "argus/core/frame_pool.hpp"

namespace argus::core {

/**
 * @brief 摄像头任务状态机
 */
enum class CameraState {
    STOPPED,      ///< 已停止
    CONNECTING,   ///< 正在连接 RTSP
    RUNNING,      ///< 正常拉流与解码中
    RECONNECTING, ///< 发生异常，处于指数退避重连中
    ERROR         ///< 发生严重错误
};

/**
 * @brief 摄像头拉流与解码任务控制器
 */
class CameraTask {
public:
    /**
     * @brief 构造摄像头任务
     * @param camera_id 摄像头唯一标识
     * @param rtsp_url RTSP 拉流 URL
     * @param platform_adapter 平台适配器（提供硬件解码器）
     * @param media_backend 流媒体后端（提供 RTSP 客户端）
     */
    CameraTask(
        const std::string& camera_id,
        const std::string& rtsp_url,
        std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
        std::shared_ptr<media::IMediaBackend> media_backend
    );
    ~CameraTask();

    /**
     * @brief 启动拉流与解码流水线
     */
    av_status start();

    /**
     * @brief 停止任务并释放所有资源
     */
    void stop();

    /**
     * @brief 绑定一个算法分析实例至该视频流
     */
    void add_instance(std::shared_ptr<AlgorithmInstance> instance);

    /**
     * @brief 解绑指定算法实例
     */
    void remove_instance(const std::string& instance_id);

    [[nodiscard]] std::string get_camera_id() const { return camera_id_; }
    [[nodiscard]] CameraState get_state() const { return state_.load(); }
    [[nodiscard]] uint64_t get_decoded_frames() const { return decoded_frames_.load(); }
    /**
     * @brief 返回最近一次成功解码帧的 wall clock 纳秒时间；尚无帧时返回 0
     */
    [[nodiscard]] int64_t get_last_frame_wall_time_ns() const {
        return last_frame_wall_time_ns_.load(std::memory_order_acquire);
    }

    /**
     * @brief 手动触发一次看门狗巡检（常用于单元测试）
     */
    void trigger_watchdog_check();

private:
    /// 底层 RTSP 数据包到达回调
    void on_encoded_packet(const media::EncodedPacket& packet);
    /// 底层连接状态变化回调
    void on_media_status(const std::string& status, bool is_error);
    /// 解码工作线程主循环
    void decode_loop();
    /// 任务健康看门狗巡检线程主循环
    void watchdog_loop();
    /// 加锁启动媒体源
    av_status start_media_source_locked();
    /// 重启底层拉流源
    void restart_media_source();
    /// 等待指定重连延迟
    void wait_for_reconnect(std::chrono::seconds delay);

    std::string camera_id_;
    std::string rtsp_url_;
    std::shared_ptr<platform::IPlatformAdapter> platform_adapter_;
    std::shared_ptr<media::IMediaBackend> media_backend_;

    std::mutex media_mutex_;
    std::unique_ptr<media::IMediaSource> media_source_;
    std::unique_ptr<platform::IDecoder> decoder_;
    std::string decoder_codec_ = "H264";
    bool saw_vps_ = false;
    bool saw_sps_ = false;
    bool saw_pps_ = false;

    std::atomic<CameraState> state_{CameraState::STOPPED};
    std::atomic<bool> running_{false};
    std::atomic<bool> saw_idr_keyframe_{false};

    std::mutex instances_mutex_;
    std::vector<std::shared_ptr<AlgorithmInstance>> instances_;

    std::atomic<uint64_t> decoded_frames_{0};
    /// 最近一次成功解码帧的墙上时钟纳秒值，供跨进程任务状态上报。
    std::atomic<int64_t> last_frame_wall_time_ns_{0};
    std::atomic<int64_t> last_packet_time_ms_{0};
    std::atomic<int64_t> last_decoder_input_time_ms_{0};
    std::atomic<bool> decoder_waiting_for_output_{false};

    std::mutex encoded_mutex_;
    std::condition_variable encoded_cv_;
    std::deque<media::EncodedPacket> encoded_queue_;
    static constexpr size_t kMaxEncodedQueueSize = 32;
    std::thread decode_thread_;

    std::atomic<bool> decoder_reset_requested_{false};
    std::atomic<uint32_t> reconnect_backoff_seconds_{1};
    std::atomic<bool> reconnect_requested_{false};
    std::atomic<bool> accepting_callbacks_{false};
    std::thread watchdog_thread_;
};

} // namespace argus::core

