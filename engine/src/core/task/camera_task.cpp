/**
 * @file camera_task.cpp
 * @brief 摄像头拉流、解码分发与看门狗任务实现
 * 
 * 核心逻辑：
 * 1. canonical_codec 归一化编码名称与 NALU 参数集（VPS/SPS/PPS）状态机跟踪；
 * 2. 只有在集齐参数集并遇到第一个关键帧（IDR / IRAP）后才开始向解码器喂入数据；
 * 3. 解码工作线程（decode_loop）从队列取包解码并借出 av_frame_desc 扇出给下游实例；
 * 4. 看门狗巡检（watchdog_loop）监控拉流断流与解码卡死，执行指数退避重连。
 */

#include "aivision/core/camera_task.hpp"

#include <algorithm>
#include <cctype>
#include <iostream>
#include <utility>


namespace aivision::core {
namespace {

std::string canonical_codec(std::string codec) {
    std::transform(codec.begin(), codec.end(), codec.begin(), [](unsigned char value) {
        return static_cast<char>(std::toupper(value));
    });
    if (codec.find("265") != std::string::npos || codec.find("HEVC") != std::string::npos) return "H265";
    if (codec.find("264") != std::string::npos || codec.find("AVC") != std::string::npos) return "H264";
    return {};
}

size_t nal_start_code_size(const uint8_t* data, size_t size, size_t offset) {
    if (offset + 4 <= size && data[offset] == 0 && data[offset + 1] == 0 &&
        data[offset + 2] == 0 && data[offset + 3] == 1) return 4;
    if (offset + 3 <= size && data[offset] == 0 && data[offset + 1] == 0 && data[offset + 2] == 1) return 3;
    return 0;
}

struct PacketNalFlags {
    bool parseable = false;
    bool has_vps = false;
    bool has_sps = false;
    bool has_pps = false;
    bool has_random_access = false;
};

// 检查并提取 H.264 / H.265 NALU 单元的参数集和关键帧类型标志
void inspect_nal(PacketNalFlags& flags, const uint8_t* nal, size_t size, bool hevc) {
    if (!nal || size == 0) return;
    flags.parseable = true;
    // HEVC: nal[0] 右移 1 位取低 6 位作为 nal_unit_type；H.264: nal[0] 取低 5 位
    const uint8_t type = hevc ? static_cast<uint8_t>((nal[0] >> 1) & 0x3F)
                              : static_cast<uint8_t>(nal[0] & 0x1F);
    if (hevc) {
        flags.has_vps = flags.has_vps || type == 32;               // VPS (Video Parameter Set)
        flags.has_sps = flags.has_sps || type == 33;               // SPS (Sequence Parameter Set)
        flags.has_pps = flags.has_pps || type == 34;               // PPS (Picture Parameter Set)
        flags.has_random_access = flags.has_random_access || (type >= 16 && type <= 23); // IRAP 关键帧 (IDR/CRA/BLA)
    } else {
        flags.has_sps = flags.has_sps || type == 7;                // SPS (Sequence Parameter Set)
        flags.has_pps = flags.has_pps || type == 8;                // PPS (Picture Parameter Set)
        flags.has_random_access = flags.has_random_access || type == 5; // IDR 关键帧
    }
}

// 深度解析编码数据包（同时支持 Annex-B 00 00 00 01 起始码模式与 AVCC/HVCC 4字节长度前缀模式）
PacketNalFlags inspect_packet(const media::EncodedPacket& packet, const std::string& codec) {
    PacketNalFlags flags;
    if (!packet.data || packet.size == 0) return flags;
    const bool hevc = codec == "H265";
    const bool annex_b = nal_start_code_size(packet.data, packet.size, 0) != 0;
    if (annex_b) {
        // Annex-B 模式：按 0x000001 / 0x00000001 起始码逐段切分 NALU 单元
        size_t offset = 0;
        while (offset < packet.size) {
            const size_t prefix = nal_start_code_size(packet.data, packet.size, offset);
            if (prefix == 0) {
                ++offset;
                continue;
            }
            const size_t nal_start = offset + prefix;
            size_t nal_end = nal_start;
            while (nal_end < packet.size && nal_start_code_size(packet.data, packet.size, nal_end) == 0) ++nal_end;
            inspect_nal(flags, packet.data + nal_start, nal_end - nal_start, hevc);
            offset = nal_end;
        }
        return flags;
    }

    // AVCC / HVCC 模式：按大端 4 字节 NALU 长度前缀解析
    size_t offset = 0;
    while (offset + 4 <= packet.size) {
        const uint32_t nal_size = (static_cast<uint32_t>(packet.data[offset]) << 24) |
                                  (static_cast<uint32_t>(packet.data[offset + 1]) << 16) |
                                  (static_cast<uint32_t>(packet.data[offset + 2]) << 8) |
                                  packet.data[offset + 3];
        offset += 4;
        if (nal_size == 0 || nal_size > packet.size - offset) {
            // 若长度前缀非法，降级为将整个 payload 作为单一 NALU 处理
            flags = PacketNalFlags{};
            inspect_nal(flags, packet.data, packet.size, hevc);
            return flags;
        }
        inspect_nal(flags, packet.data + offset, nal_size, hevc);
        offset += nal_size;
    }
    if (offset != packet.size) {
        flags = PacketNalFlags{};
        inspect_nal(flags, packet.data, packet.size, hevc);
        return flags;
    }
    return flags;
}

} // namespace

CameraTask::CameraTask(
    const std::string& camera_id,
    const std::string& rtsp_url,
    std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
    std::shared_ptr<media::IMediaBackend> media_backend
) : camera_id_(camera_id),
    rtsp_url_(rtsp_url),
    platform_adapter_(std::move(platform_adapter)),
    media_backend_(std::move(media_backend)) {}

CameraTask::~CameraTask() {
    stop();
}

av_status CameraTask::start() {
    if (running_.exchange(true)) return AV_OK;
    if (!platform_adapter_ || !media_backend_) {
        running_.store(false);
        state_.store(CameraState::ERROR);
        return AV_ERR_INVALID_ARG;
    }

    state_.store(CameraState::CONNECTING);
    decoder_codec_ = "H264";
    saw_vps_ = false;
    saw_sps_ = false;
    saw_pps_ = false;
    decoder_ = platform_adapter_->create_decoder(decoder_codec_);
    media_source_ = media_backend_->create_source(camera_id_);
    if (!decoder_ || !media_source_) {
        decoder_.reset();
        media_source_.reset();
        running_.store(false);
        state_.store(CameraState::ERROR);
        return AV_ERR_INTERNAL;
    }

    const auto now_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count();
    last_packet_time_ms_.store(now_ms);
    last_frame_wall_time_ns_.store(0, std::memory_order_release);
    last_decoder_input_time_ms_.store(0);
    decoder_waiting_for_output_.store(false);
    saw_idr_keyframe_.store(false);
    decoder_reset_requested_.store(false);
    reconnect_backoff_seconds_.store(1, std::memory_order_release);
    reconnect_requested_.store(false);
    accepting_callbacks_.store(true);

    av_status media_status = AV_ERR_INTERNAL;
    {
        std::lock_guard<std::mutex> lock(media_mutex_);
        media_status = start_media_source_locked();
    }
    if (media_status != AV_OK) {
        accepting_callbacks_.store(false, std::memory_order_release);
        std::lock_guard<std::mutex> lock(media_mutex_);
        media_source_->stop();
        media_source_.reset();
        decoder_.reset();
        running_.store(false);
        state_.store(CameraState::ERROR);
        return media_status;
    }

    decode_thread_ = std::thread(&CameraTask::decode_loop, this);
    watchdog_thread_ = std::thread(&CameraTask::watchdog_loop, this);
    state_.store(CameraState::RUNNING);
    encoded_cv_.notify_all();
    return AV_OK;
}

av_status CameraTask::start_media_source_locked() {
    if (!media_source_) return AV_ERR_INVALID_ARG;
    return media_source_->start(
        rtsp_url_,
        [this](const media::EncodedPacket& packet) {
            if (accepting_callbacks_.load(std::memory_order_acquire)) on_encoded_packet(packet);
        },
        [this](const std::string& status, bool is_error) {
            if (accepting_callbacks_.load(std::memory_order_acquire)) on_media_status(status, is_error);
        }
    );
}

void CameraTask::restart_media_source() {
    if (!running_.load(std::memory_order_acquire)) return;
    state_.store(CameraState::RECONNECTING);
    const auto now_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count();
    last_packet_time_ms_.store(now_ms, std::memory_order_release);
    last_decoder_input_time_ms_.store(0, std::memory_order_release);
    decoder_waiting_for_output_.store(false, std::memory_order_release);
    decoder_reset_requested_.store(true, std::memory_order_release);
    saw_idr_keyframe_.store(false, std::memory_order_release);
    saw_vps_ = false;
    saw_sps_ = false;
    saw_pps_ = false;
    reconnect_requested_.store(true, std::memory_order_release);
    {
        std::lock_guard<std::mutex> lock(encoded_mutex_);
        encoded_queue_.clear();
    }

    av_status status = AV_ERR_INTERNAL;
    bool connected = false;
    {
        std::lock_guard<std::mutex> lock(media_mutex_);
        if (!running_.load(std::memory_order_acquire) || !media_source_) return;
        media_source_->stop();
        if (running_.load(std::memory_order_acquire)) {
            status = start_media_source_locked();
            connected = status == AV_OK && media_source_->is_connected();
        }
    }
    if (status != AV_OK) {
        state_.store(CameraState::RECONNECTING);
    } else if (connected || !reconnect_requested_.load(std::memory_order_acquire)) {
        reconnect_backoff_seconds_.store(1, std::memory_order_release);
        reconnect_requested_.store(false, std::memory_order_release);
        state_.store(CameraState::RUNNING);
        encoded_cv_.notify_one();
    } else {
        state_.store(CameraState::CONNECTING);
    }
}

void CameraTask::wait_for_reconnect(std::chrono::seconds delay) {
    std::unique_lock<std::mutex> lock(encoded_mutex_);
    const auto deadline = std::chrono::steady_clock::now() + delay;
    encoded_cv_.wait_until(lock, deadline, [this] {
        return !running_.load(std::memory_order_acquire) ||
               !reconnect_requested_.load(std::memory_order_acquire);
    });
}

void CameraTask::stop() {
    if (!running_.exchange(false)) return;

    accepting_callbacks_.store(false, std::memory_order_release);
    reconnect_requested_.store(false, std::memory_order_release);
    std::unique_ptr<media::IMediaSource> source_to_stop;
    {
        std::lock_guard<std::mutex> lock(media_mutex_);
        source_to_stop = std::move(media_source_);
    }
    if (source_to_stop) source_to_stop->stop();
    encoded_cv_.notify_all();

    if (decode_thread_.joinable()) decode_thread_.join();
    if (watchdog_thread_.joinable()) watchdog_thread_.join();

    {
        std::lock_guard<std::mutex> lock(encoded_mutex_);
        encoded_queue_.clear();
    }

    std::vector<std::shared_ptr<AlgorithmInstance>> instances;
    {
        std::lock_guard<std::mutex> lock(instances_mutex_);
        instances = instances_;
    }
    for (const auto& instance : instances) {
        if (instance) instance->stop();
    }
    decoder_.reset();
    state_.store(CameraState::STOPPED);
}

void CameraTask::add_instance(std::shared_ptr<AlgorithmInstance> instance) {
    if (!instance) return;
    std::lock_guard<std::mutex> lock(instances_mutex_);
    const auto it = std::find_if(instances_.begin(), instances_.end(), [&](const auto& current) {
        return current && current->get_instance_id() == instance->get_instance_id();
    });
    if (it == instances_.end()) instances_.push_back(std::move(instance));
}

void CameraTask::remove_instance(const std::string& instance_id) {
    std::shared_ptr<AlgorithmInstance> removed;
    {
        std::lock_guard<std::mutex> lock(instances_mutex_);
        const auto it = std::find_if(instances_.begin(), instances_.end(), [&](const auto& inst) {
            return inst && inst->get_instance_id() == instance_id;
        });
        if (it == instances_.end()) return;
        removed = *it;
        instances_.erase(it);
    }
    if (removed) removed->stop();
}

void CameraTask::on_encoded_packet(const media::EncodedPacket& packet) {
    if (!running_.load(std::memory_order_acquire) || !packet.data || packet.size == 0) return;
    last_packet_time_ms_.store(std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count());

    media::EncodedPacket owned = packet.clone_owned();
    if (!owned.storage || !owned.data) return;
    {
        std::lock_guard<std::mutex> lock(encoded_mutex_);
        if (encoded_queue_.size() >= kMaxEncodedQueueSize) encoded_queue_.pop_front();
        encoded_queue_.push_back(std::move(owned));
    }
    encoded_cv_.notify_one();
}

void CameraTask::decode_loop() {
    // 重置解码器内部状态与关键帧同步标志
    const auto reset_decoder_state = [this] {
        if (decoder_) decoder_->reset();
        saw_idr_keyframe_.store(false, std::memory_order_release);
        saw_vps_ = false;
        saw_sps_ = false;
        saw_pps_ = false;
        last_decoder_input_time_ms_.store(0, std::memory_order_release);
        decoder_waiting_for_output_.store(false, std::memory_order_release);
    };

    while (running_.load(std::memory_order_acquire)) {
        media::EncodedPacket packet;
        {
            std::unique_lock<std::mutex> lock(encoded_mutex_);
            // 等待待解码的视频包、重连通知或重置请求
            encoded_cv_.wait(lock, [this] {
                return !running_.load(std::memory_order_acquire) || reconnect_requested_.load(std::memory_order_acquire) ||
                       decoder_reset_requested_.load(std::memory_order_acquire) || !encoded_queue_.empty();
            });
            if (!running_.load(std::memory_order_acquire)) break;

            // 处理重连请求：执行媒体拉流重启，并在失败时以指数退避延迟重试
            if (reconnect_requested_.exchange(false, std::memory_order_acq_rel)) {
                lock.unlock();
                restart_media_source();
                if (reconnect_requested_.load(std::memory_order_acquire)) {
                    const uint32_t delay_seconds = reconnect_backoff_seconds_.load(std::memory_order_acquire);
                    reconnect_backoff_seconds_.store(std::min<uint32_t>(30U, delay_seconds * 2U),
                                                     std::memory_order_release);
                    wait_for_reconnect(std::chrono::seconds(delay_seconds));
                }
                continue;
            }

            // 处理解码器重置请求
            if (decoder_reset_requested_.exchange(false, std::memory_order_acq_rel)) {
                lock.unlock();
                reset_decoder_state();
                lock.lock();
                if (!running_.load(std::memory_order_acquire)) break;
            }
            if (encoded_queue_.empty()) continue;
            packet = std::move(encoded_queue_.front());
            encoded_queue_.pop_front();
        }

        // 动态检测编码格式切换（如 H.264 切换到 H.265）并重新初始化匹配的解码器
        const std::string packet_codec = canonical_codec(packet.codec_name);
        if (!packet_codec.empty() && packet_codec != decoder_codec_) {
            auto replacement = platform_adapter_->create_decoder(packet_codec);
            if (!replacement) {
                state_.store(CameraState::ERROR);
                continue;
            }
            decoder_ = std::move(replacement);
            decoder_codec_ = packet_codec;
            decoder_reset_requested_.store(false, std::memory_order_release);
            saw_idr_keyframe_.store(false, std::memory_order_release);
            saw_vps_ = false;
            saw_sps_ = false;
            saw_pps_ = false;
            last_decoder_input_time_ms_.store(0, std::memory_order_release);
            decoder_waiting_for_output_.store(false, std::memory_order_release);
        }
        if (!decoder_) continue;

        // 参数集状态机与首个关键帧过滤：必须在集齐 SPS/PPS 并在首个 IDR 关键帧到来后才开始向解码器送帧，防止花屏或崩溃
        const auto nal_flags = inspect_packet(packet, decoder_codec_);
        if (!nal_flags.parseable) continue;
        saw_vps_ = saw_vps_ || nal_flags.has_vps;
        saw_sps_ = saw_sps_ || nal_flags.has_sps;
        saw_pps_ = saw_pps_ || nal_flags.has_pps;
        const bool parameter_sets_ready = decoder_codec_ == "H265"
            ? saw_vps_ && saw_sps_ && saw_pps_
            : saw_sps_ && saw_pps_;
        const bool waiting_for_idr = !saw_idr_keyframe_.load(std::memory_order_acquire);
        bool is_param_set = false;
        if (decoder_codec_ == "H265" || decoder_codec_ == "HEVC") {
            is_param_set = nal_flags.has_vps || nal_flags.has_sps || nal_flags.has_pps;
        } else {
            is_param_set = nal_flags.has_sps || nal_flags.has_pps;
        }
        if (waiting_for_idr && !is_param_set &&
            !(parameter_sets_ready && nal_flags.has_random_access)) {
            continue;
        }

        // 向硬件/软件解码器喂入压缩数据包
        const av_status send_st = decoder_->send_packet(packet.data, packet.size, packet.pts_us, packet.is_keyframe);
        if (send_st != AV_OK) continue;
        last_decoder_input_time_ms_.store(std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now().time_since_epoch()).count(), std::memory_order_release);
        decoder_waiting_for_output_.store(true, std::memory_order_release);
        if (parameter_sets_ready && nal_flags.has_random_access) {
            saw_idr_keyframe_.store(true, std::memory_order_release);
        }

        // 循环读取解码器输出的原始帧，并扇出分发给绑定的所有算法实例
        for (;;) {
            av_frame_desc* frame = FramePool::instance().acquire_frame();
            if (!frame) break;
            const void* pool_token = frame->frame_token;
            const av_status receive_status = decoder_->receive_frame(frame);
            if (receive_status == AV_OK) {
                frame->frame_token = const_cast<void*>(pool_token);
                decoder_waiting_for_output_.store(false, std::memory_order_release);
                decoded_frames_.fetch_add(1);
                // wall_time_ns 由平台解码器在成功输出时填充；仅正值可作为真实最后帧时间。
                if (frame->wall_time_ns > 0) {
                    last_frame_wall_time_ns_.store(frame->wall_time_ns, std::memory_order_release);
                }

                // 注册底层硬件 surface 的级联析构释放回调
                if (frame->opaque && platform_adapter_) {
                    const auto release_opaque = platform_adapter_->get_opaque_release();
                    if (release_opaque && FramePool::instance().set_opaque_release(frame->frame_token, release_opaque) != AV_OK) {
                        FramePool::instance().release_frame(frame->frame_token);
                        break;
                    }
                }

                // 扇出分发视频帧给所有运行中的算法实例
                std::vector<std::shared_ptr<AlgorithmInstance>> instances;
                {
                    std::lock_guard<std::mutex> lock(instances_mutex_);
                    instances = instances_;
                }
                for (const auto& instance : instances) {
                    if (instance) instance->push_frame(*frame);
                }
                // 释放当前线程在帧池中的借出引用（若下游实例保留了该帧，帧池的内部 refcount 会维护其生命周期）
                FramePool::instance().release_frame(frame->frame_token);
                continue;
            }
            // 无更多输出帧，归还未使用的描述符并退出循环
            FramePool::instance().release_frame(const_cast<void*>(pool_token));
            break;
        }
    }
}

void CameraTask::on_media_status(const std::string&, bool is_error) {
    if (is_error) {
        state_.store(CameraState::RECONNECTING);
        saw_idr_keyframe_.store(false, std::memory_order_release);
        decoder_reset_requested_.store(true, std::memory_order_release);
        reconnect_requested_.store(true, std::memory_order_release);
        encoded_cv_.notify_one();
    } else if (running_.load(std::memory_order_acquire)) {
        reconnect_backoff_seconds_.store(1, std::memory_order_release);
        reconnect_requested_.store(false, std::memory_order_release);
        state_.store(CameraState::RUNNING);
    }
}

void CameraTask::trigger_watchdog_check() {
    const auto now_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count();
    const auto last_packet = last_packet_time_ms_.load();
    if (last_packet != 0 && now_ms - last_packet > 5000) {
        state_.store(CameraState::RECONNECTING);
        saw_idr_keyframe_.store(false, std::memory_order_release);
        decoder_reset_requested_.store(true, std::memory_order_release);
        reconnect_requested_.store(true, std::memory_order_release);
    }

    const auto last_decoder_input = last_decoder_input_time_ms_.load(std::memory_order_acquire);
    if (decoder_waiting_for_output_.load(std::memory_order_acquire) && last_decoder_input != 0 &&
        now_ms - last_decoder_input > 3000) {
        decoder_reset_requested_.store(true, std::memory_order_release);
        saw_idr_keyframe_.store(false, std::memory_order_release);
        encoded_cv_.notify_one();
    }
    encoded_cv_.notify_one();
}

void CameraTask::watchdog_loop() {
    while (running_.load(std::memory_order_acquire)) {
        std::unique_lock<std::mutex> lock(encoded_mutex_);
        encoded_cv_.wait_for(lock, std::chrono::seconds(1), [this] {
            return !running_.load(std::memory_order_acquire) || reconnect_requested_.load(std::memory_order_acquire);
        });
        lock.unlock();
        if (running_.load(std::memory_order_acquire)) trigger_watchdog_check();
    }
}

} // namespace aivision::core
