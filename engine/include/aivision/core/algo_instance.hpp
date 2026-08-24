#pragma once

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
#include "aivision/types.h"
#include "aivision/algo.h"
#include "aivision/media/media_api.hpp"
#include "aivision/platform/platform_api.hpp"

namespace aivision::core {

class AlgorithmInstance {
public:
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

    av_status init(const av_frame_ops* frame_ops, const av_image_ops* image_ops);
    void push_frame(const av_frame_desc& frame);
    av_status update_params(const std::string& new_params);
    av_status set_rules(const std::vector<av_rule>& rules);
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

    void set_result_callback(std::function<void(const av_algo_result&, const av_frame_desc&)> cb) {
        std::lock_guard<std::mutex> lock(callback_mutex_);
        result_cb_ = std::move(cb);
    }

private:
    static void result_bridge(const av_algo_result* res, void* user_data);
    void worker_loop();

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
    std::chrono::time_point<std::chrono::steady_clock> last_sample_time_{};

    std::function<void(const av_algo_result&, const av_frame_desc&)> result_cb_;
};

} // namespace aivision::core
