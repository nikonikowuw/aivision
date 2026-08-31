#pragma once

/**
 * @file algo_instance.hpp
 * @brief 单算法推理实例生命周期与工作线程管理
 * 
 * 封装与具体算法 C ABI 实例的交互、输入帧队列流控（丢帧策略）、
 * FPS 采样控制、检测规则下发、参数热更新以及推理结果桥接与回调。
 */

#include <string>
#include <vector>
#include <memory>
#include <thread>
#include <atomic>
#include <queue>
#include <mutex>
#include <condition_variable>
#include <chrono>
#include <functional>
#include <utility>
#include "argus/types.h"
#include "argus/algo.h"
#include "argus/media/media_api.hpp"
#include "argus/platform/platform_api.hpp"
#include "argus/core/motion_gate.hpp"

namespace argus::core {

/**
 * @brief 运行中的算法实例控制器
 */
class AlgorithmInstance {
public:
    /**
     * @brief 构造算法实例
     * @param instance_id 实例唯一标识
     * @param camera_id 所属摄像头任务标识
     * @param algorithm_id 算法包标识
     * @param version 算法版本号
     * @param target_fps 目标分析抽帧 FPS
     * @param params_json 实例初始化 JSON 参数
     * @param abi 算法包 C ABI 导出函数表
     * @param lib_handle 动态库句柄
     */
    AlgorithmInstance(
        const std::string& instance_id,
        const std::string& camera_id,
        const std::string& algorithm_id,
        const std::string& version,
        int32_t target_fps,
        const std::string& params_json,
        const av_algo_abi* abi,
        av_algo_library lib_handle
    );
    ~AlgorithmInstance();

    /**
     * @brief 初始化算法实例上下文并启动内部处理线程
     * @param frame_ops 帧操作接口表（retain/release）
     * @param image_ops 图像处理接口表
     * @return av_status 操作状态码
     */
    av_status init(const av_frame_ops* frame_ops, const av_image_ops* image_ops);

    /**
     * @brief 压入待处理的视频帧（内部进行 FPS 节流与满队列丢弃处理）
     * @param frame 视频帧描述符
     */
    void push_frame(const av_frame_desc& frame);

    /**
     * @brief 热更新实例自有算法参数
     * @param new_params 新参数 JSON 串
     */
    av_status update_params(const std::string& new_params);

    /**
     * @brief 热更新实例运动门控参数
     * @param config 门控配置结构
     */
    void update_motion_gate(const MotionGateConfig& config);

    /**
     * @brief 设置布防区域/屏蔽区/分界线等检测规则
     * @param rules 规则列表
     */
    av_status set_rules(const std::vector<av_rule>& rules);

    /**
     * @brief 停止实例工作线程并释放底层 ABI 实例句柄
     */
    void stop();

    [[nodiscard]] std::string get_instance_id() const { return instance_id_; }
    [[nodiscard]] std::string get_camera_id() const { return camera_id_; }
    [[nodiscard]] std::string get_algorithm_id() const { return algorithm_id_; }
    [[nodiscard]] std::string get_version() const { return version_; }
    [[nodiscard]] std::string get_run_id() const { return run_id_; }
    [[nodiscard]] std::string get_alarm_type_id() const { return alarm_type_id_; }
    [[nodiscard]] int32_t get_target_fps() const { return target_fps_; }
    [[nodiscard]] bool is_running() const { return running_.load(std::memory_order_acquire); }
    [[nodiscard]] uint64_t get_processed_frames() const { return processed_frames_.load(); }
    [[nodiscard]] uint64_t get_dropped_frames() const { return dropped_frames_.load(); }
    [[nodiscard]] uint64_t get_motion_skips() const { return motion_skipped_total_.load(); }
    [[nodiscard]] uint64_t get_motion_passes() const { return motion_passed_total_.load(); }
    [[nodiscard]] uint64_t get_keepalive_passes() const { return keepalive_passed_total_.load(); }

    /**
     * @brief 获取当前推理帧率（FPS）
     * @return 最近 1s 滑动窗口结算的推理吞吐；尚未满一个窗口或实例无帧进入时返回 0
     */
    [[nodiscard]] double get_current_fps() const { return get_current_fps(std::chrono::steady_clock::now()); }

    /**
     * @brief 按指定时间点结算推理帧率（测试可注入时间，避免依赖真实 sleep）
     * @param now 当前时间点（单调时钟）
     */
    [[nodiscard]] double get_current_fps(std::chrono::steady_clock::time_point now) const;

    [[nodiscard]] MotionGate& get_motion_gate() { return motion_gate_; }
    [[nodiscard]] const MotionGate& get_motion_gate() const { return motion_gate_; }

    /**
     * @brief 设置推理结果到达时的外部处理回调
     */
    void set_result_callback(std::function<void(const av_algo_result&, const av_frame_desc&)> cb) {
        std::lock_guard<std::mutex> lock(callback_mutex_);
        result_cb_ = std::move(cb);
    }

private:
    /// C ABI 静态结果回调桥接函数
    static void result_bridge(const av_algo_result* res, void* user_data);
    /// 工作线程主循环
    void worker_loop();
    /// 按目标 FPS 判定当前帧是否应被抽帧节流丢弃（内部加锁更新采样基准）
    bool should_throttle_sample(int64_t pts_ns);
    /// 每秒输出一次输入、丢帧、队列和实际 process 耗时汇总
    void log_debug_metrics();

    std::string instance_id_;
    std::string camera_id_;
    std::string algorithm_id_;
    std::string version_;
    std::string alarm_type_id_;
    int32_t target_fps_ = 25;
    std::string params_json_;
    std::string run_id_;

    const av_algo_abi* abi_ = nullptr;
    av_algo_library lib_handle_ = nullptr;
    av_algo_instance inst_handle_ = nullptr;
    const av_frame_ops* frame_ops_ = nullptr;
    av_frame_caps accepted_caps_{};

    std::queue<av_frame_desc> frame_queue_;
    const size_t max_queue_size_ = 5;
    std::mutex queue_mutex_;
    std::condition_variable cv_;
    std::mutex abi_mutex_;
    std::mutex callback_mutex_;
    std::mutex sample_mutex_;
    av_frame_desc callback_frame_{};
    std::atomic<bool> running_{false};
    std::thread worker_thread_;

    std::atomic<uint64_t> processed_frames_{0};
    std::atomic<uint64_t> dropped_frames_{0};
    // 以下计数器按诊断窗口累计，并由 worker 线程每秒交换清零。
    std::atomic<uint64_t> received_frames_{0};
    std::atomic<uint64_t> sampled_frames_{0};
    std::atomic<uint64_t> sample_dropped_frames_{0};
    std::atomic<uint64_t> caps_dropped_frames_{0};
    std::atomic<uint64_t> retain_failed_frames_{0};
    std::atomic<uint64_t> queued_frames_{0};
    std::atomic<uint64_t> queue_dropped_frames_{0};
    std::atomic<uint64_t> process_calls_{0};
    std::atomic<uint64_t> process_failures_{0};
    std::atomic<uint64_t> process_duration_us_{0};
    std::atomic<uint64_t> process_max_duration_us_{0};
    std::atomic<int64_t> last_process_status_{AV_OK};
    std::chrono::steady_clock::time_point last_debug_log_{};
    int64_t last_sample_pts_ns_ = 0;
    std::chrono::time_point<std::chrono::steady_clock> last_sample_time_{};

    MotionGate motion_gate_;
    std::atomic<uint64_t> motion_skipped_total_{0};
    std::atomic<uint64_t> motion_passed_total_{0};
    std::atomic<uint64_t> keepalive_passed_total_{0};
    std::atomic<uint64_t> motion_skipped_frames_{0};
    std::atomic<uint64_t> motion_passed_frames_{0};
    std::atomic<uint64_t> keepalive_passed_frames_{0};

    /// FPS 滑动窗口统计（worker 线程无锁累加，上报线程互斥结算）
    mutable std::mutex fps_calc_mutex_;
    mutable std::atomic<uint64_t> fps_window_frames_{0};
    mutable std::chrono::time_point<std::chrono::steady_clock> fps_window_start_{};
    mutable double current_fps_ = 0.0;

    std::function<void(const av_algo_result&, const av_frame_desc&)> result_cb_;
};

} // namespace argus::core

