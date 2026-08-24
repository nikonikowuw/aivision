#pragma once

#include <string>
#include <vector>
#include <memory>
#include <thread>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <chrono>
#include "aivision/types.h"
#include "aivision/media/media_api.hpp"
#include "aivision/platform/platform_api.hpp"
#include "aivision/core/algo_instance.hpp"
#include "aivision/core/frame_pool.hpp"

namespace aivision::core {

enum class CameraState {
    STOPPED,
    CONNECTING,
    RUNNING,
    RECONNECTING,
    ERROR
};

class CameraTask {
public:
    CameraTask(
        const std::string& camera_id,
        const std::string& rtsp_url,
        std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
        std::shared_ptr<media::IMediaBackend> media_backend
    );
    ~CameraTask();

    av_status start();
    void stop();

    void add_instance(std::shared_ptr<AlgorithmInstance> instance);
    void remove_instance(const std::string& instance_id);

    [[nodiscard]] std::string get_camera_id() const { return camera_id_; }
    [[nodiscard]] CameraState get_state() const { return state_.load(); }
    [[nodiscard]] uint64_t get_decoded_frames() const { return decoded_frames_.load(); }

    void trigger_watchdog_check();

private:
    void on_encoded_packet(const media::EncodedPacket& packet);
    void on_media_status(const std::string& status, bool is_error);
    void decode_loop();
    void watchdog_loop();
    av_status start_media_source_locked();
    void restart_media_source();
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

} // namespace aivision::core
