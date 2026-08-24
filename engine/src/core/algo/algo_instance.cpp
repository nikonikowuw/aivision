#include "aivision/core/algo_instance.hpp"

#include <algorithm>
#include <limits>

namespace aivision::core {

AlgorithmInstance::AlgorithmInstance(
    const std::string& instance_id,
    const std::string& camera_id,
    const std::string& algorithm_id,
    const std::string& version,
    int32_t target_fps,
    const std::string& params_json,
    const av_algo_abi* abi,
    av_algo_library lib_handle
) : instance_id_(instance_id),
    camera_id_(camera_id),
    algorithm_id_(algorithm_id),
    version_(version),
    target_fps_(target_fps),
    params_json_(params_json),
    abi_(abi),
    lib_handle_(lib_handle) {
    static std::atomic<uint64_t> next_run_id{1};
    run_id_ = instance_id_ + "-run-" + std::to_string(next_run_id.fetch_add(1));
}

AlgorithmInstance::~AlgorithmInstance() {
    stop();
}

void AlgorithmInstance::result_bridge(const av_algo_result* res, void* user_data) {
    auto* instance = static_cast<AlgorithmInstance*>(user_data);
    if (!instance || !res) return;

    std::function<void(const av_algo_result&, const av_frame_desc&)> callback;
    {
        std::lock_guard<std::mutex> lock(instance->callback_mutex_);
        callback = instance->result_cb_;
    }
    if (!callback) return;

    // The ABI contract permits callbacks only during instance_process/flush, so
    // callback_frame_ remains valid for the duration of this synchronous call.
    try {
        callback(*res, instance->callback_frame_);
    } catch (...) {
        // A C callback must never let a C++ exception cross the ABI boundary.
    }
}

av_status AlgorithmInstance::init(const av_frame_ops* frame_ops, const av_image_ops* image_ops) {
    if (running_.load(std::memory_order_acquire)) return AV_OK;
    frame_ops_ = frame_ops;
    {
        std::lock_guard<std::mutex> lock(abi_mutex_);
        if (!abi_) {
            // A null ABI is used by scheduler tests and represents a disabled instance.
        } else {
            if (!abi_->instance_create || !abi_->instance_negotiate || !abi_->instance_process ||
                !abi_->instance_update_config || !abi_->instance_flush || !abi_->instance_destroy || !frame_ops_ || !lib_handle_ ||
                params_json_.size() > std::numeric_limits<uint32_t>::max()) {
                return AV_ERR_INVALID_ARG;
            }
            av_algo_instance_args args{};
            args.size = sizeof(av_algo_instance_args);
            args.api_version = AV_ALGO_API_VERSION;
            args.mode = AV_INSTANCE_NORMAL;
            args.instance_id = instance_id_.c_str();
            args.instance_run_id = run_id_.c_str();
            args.config_json = params_json_.c_str();
            args.config_json_len = static_cast<uint32_t>(params_json_.length());
            args.frame_ops = frame_ops_;
            args.image_ops = image_ops;
            args.on_result = result_bridge;
            args.result_user = this;

            const av_status create_status = static_cast<av_status>(abi_->instance_create(
                lib_handle_, &args, &inst_handle_));
            if (create_status != AV_OK || !inst_handle_) return create_status == AV_OK ? AV_ERR_INTERNAL : create_status;

            av_frame_caps offered{};
            offered.size = sizeof(av_frame_caps);
            offered.api_version = AV_ALGO_API_VERSION;
            offered.pixel_format_count = 2;
            offered.pixel_formats[0] = AV_PIX_NV12;
            offered.pixel_formats[1] = AV_PIX_BGRA;
            offered.memory_type_count = 2;
            offered.memory_types[0] = AV_MEM_PLATFORM_SURFACE;
            offered.memory_types[1] = AV_MEM_HOST;
            offered.required_opaque_kind = AV_OPAQUE_NONE;
            offered.min_width = 1;
            offered.min_height = 1;
            offered.max_width = 16384;
            offered.max_height = 16384;
            accepted_caps_ = {};
            accepted_caps_.size = sizeof(av_frame_caps);
            accepted_caps_.api_version = AV_ALGO_API_VERSION;
            const av_status negotiate_status = static_cast<av_status>(abi_->instance_negotiate(
                inst_handle_, &offered, &accepted_caps_));
            if (negotiate_status != AV_OK || accepted_caps_.size < sizeof(av_frame_caps) ||
                accepted_caps_.api_version != AV_ALGO_API_VERSION || accepted_caps_.pixel_format_count == 0 ||
                accepted_caps_.pixel_format_count > 8 || accepted_caps_.memory_type_count == 0 ||
                accepted_caps_.memory_type_count > 4 || accepted_caps_.min_width > accepted_caps_.max_width ||
                accepted_caps_.min_height > accepted_caps_.max_height ||
                accepted_caps_.min_width < offered.min_width || accepted_caps_.max_width > offered.max_width ||
                accepted_caps_.min_height < offered.min_height || accepted_caps_.max_height > offered.max_height) {
                if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                inst_handle_ = nullptr;
                return negotiate_status != AV_OK ? negotiate_status : AV_ERR_INCOMPATIBLE_FRAME;
            }
            for (uint32_t index = 0; index < accepted_caps_.pixel_format_count; ++index) {
                if (!std::any_of(offered.pixel_formats, offered.pixel_formats + offered.pixel_format_count,
                                 [&](uint32_t value) { return value == accepted_caps_.pixel_formats[index]; })) {
                    if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                    inst_handle_ = nullptr;
                    return AV_ERR_INCOMPATIBLE_FRAME;
                }
            }
            for (uint32_t index = 0; index < accepted_caps_.memory_type_count; ++index) {
                if (!std::any_of(offered.memory_types, offered.memory_types + offered.memory_type_count,
                                 [&](uint32_t value) { return value == accepted_caps_.memory_types[index]; })) {
                    if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                    inst_handle_ = nullptr;
                    return AV_ERR_INCOMPATIBLE_FRAME;
                }
            }
            if (accepted_caps_.required_opaque_kind != AV_OPAQUE_NONE &&
                offered.required_opaque_kind != AV_OPAQUE_NONE &&
                accepted_caps_.required_opaque_kind != offered.required_opaque_kind) {
                if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                inst_handle_ = nullptr;
                return AV_ERR_INCOMPATIBLE_FRAME;
            }
        }
    }

    running_.store(true);
    worker_thread_ = std::thread(&AlgorithmInstance::worker_loop, this);
    return AV_OK;
}

void AlgorithmInstance::push_frame(const av_frame_desc& frame) {
    if (!running_.load() || frame.size < sizeof(av_frame_desc) || frame.api_version != AV_ALGO_API_VERSION ||
        !frame.frame_token) return;
    if (accepted_caps_.size >= sizeof(av_frame_caps)) {
        const uint32_t pixel_count = std::min(accepted_caps_.pixel_format_count, 8U);
        const uint32_t memory_count = std::min(accepted_caps_.memory_type_count, 4U);
        const bool pixel_supported = std::any_of(
            accepted_caps_.pixel_formats, accepted_caps_.pixel_formats + pixel_count,
            [&](uint32_t value) { return value == frame.pixel_format; });
        const bool memory_supported = std::any_of(
            accepted_caps_.memory_types, accepted_caps_.memory_types + memory_count,
            [&](uint32_t value) { return value == frame.memory_type; });
        if (!pixel_supported || !memory_supported ||
            (accepted_caps_.required_opaque_kind != AV_OPAQUE_NONE &&
             accepted_caps_.required_opaque_kind != frame.opaque_kind) ||
            frame.width < accepted_caps_.min_width || frame.height < accepted_caps_.min_height ||
            frame.width > accepted_caps_.max_width || frame.height > accepted_caps_.max_height) {
            dropped_frames_.fetch_add(1, std::memory_order_relaxed);
            return;
        }
    }

    // Rate limiting / sampling check
    const auto now = std::chrono::steady_clock::now();
    {
        std::lock_guard<std::mutex> sample_lock(sample_mutex_);
        if (target_fps_ > 0 && last_sample_time_.time_since_epoch().count() > 0) {
            const auto interval_ms = 1000.0 / target_fps_;
            const auto elapsed_ms = std::chrono::duration<double, std::milli>(now - last_sample_time_).count();
            if (elapsed_ms < interval_ms * 0.9) {
                return;
            }
        }
        last_sample_time_ = now;
    }

    // Retain frame for instance queue
    if (frame_ops_ && frame_ops_->retain && frame_ops_->retain(frame_ops_->ctx, frame.frame_token) != AV_OK) {
        return;
    }

    std::lock_guard<std::mutex> lock(queue_mutex_);
    if (frame_queue_.size() >= max_queue_size_) {
        // Drop oldest frame
        av_frame_desc oldest = frame_queue_.front();
        frame_queue_.pop();
        if (frame_ops_ && frame_ops_->release) {
            frame_ops_->release(frame_ops_->ctx, oldest.frame_token);
        }
        dropped_frames_.fetch_add(1);
    }

    frame_queue_.push(frame);
    cv_.notify_one();
}

av_status AlgorithmInstance::update_params(const std::string& new_params) {
    if (new_params.size() > std::numeric_limits<uint32_t>::max()) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(abi_mutex_);
    if (abi_ && abi_->instance_update_config && inst_handle_) {
        const av_status status = static_cast<av_status>(abi_->instance_update_config(
            inst_handle_, new_params.c_str(), static_cast<uint32_t>(new_params.length())));
        if (status != AV_OK) return status;
    }
    params_json_ = new_params;
    return AV_OK;
}

av_status AlgorithmInstance::set_rules(const std::vector<av_rule>& rules) {
    if (rules.size() > std::numeric_limits<uint32_t>::max()) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(abi_mutex_);
    if (abi_ && inst_handle_ && abi_->instance_set_rules) {
        return static_cast<av_status>(abi_->instance_set_rules(
            inst_handle_, rules.empty() ? nullptr : rules.data(), static_cast<uint32_t>(rules.size())));
    }
    return AV_OK;
}

void AlgorithmInstance::stop() {
    if (!running_.exchange(false)) return;

    cv_.notify_all();
    if (worker_thread_.joinable()) {
        worker_thread_.join();
    }

    {
        std::lock_guard<std::mutex> lock(queue_mutex_);
        while (!frame_queue_.empty()) {
            av_frame_desc f = frame_queue_.front();
            frame_queue_.pop();
            if (frame_ops_ && frame_ops_->release) {
                frame_ops_->release(frame_ops_->ctx, f.frame_token);
            }
        }
    }

    std::lock_guard<std::mutex> lock(abi_mutex_);
    if (abi_ && inst_handle_) {
        if (abi_->instance_flush) {
            abi_->instance_flush(inst_handle_);
        }
        if (abi_->instance_destroy) {
            abi_->instance_destroy(inst_handle_);
        }
        inst_handle_ = nullptr;
    }
}

void AlgorithmInstance::worker_loop() {
    while (running_.load()) {
        av_frame_desc frame{};
        {
            std::unique_lock<std::mutex> lock(queue_mutex_);
            cv_.wait(lock, [this] {
                return !running_.load() || !frame_queue_.empty();
            });

            if (!running_.load() && frame_queue_.empty()) {
                break;
            }

            frame = frame_queue_.front();
            frame_queue_.pop();
        }

        {
            std::lock_guard<std::mutex> lock(abi_mutex_);
            if (abi_ && abi_->instance_process && inst_handle_) {
                callback_frame_ = frame;
                abi_->instance_process(inst_handle_, &frame);
                callback_frame_ = {};
            }
        }

        processed_frames_.fetch_add(1);

        if (frame_ops_ && frame_ops_->release) {
            frame_ops_->release(frame_ops_->ctx, frame.frame_token);
        }
    }
}

} // namespace aivision::core
