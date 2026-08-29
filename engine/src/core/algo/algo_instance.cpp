/**
 * @file algo_instance.cpp
 * @brief 算法实例处理线程、帧流控与 ABI 结果桥接实现
 */

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
    // 生成单调递增的 run_id，用于区分同一实例 ID 在生命周期内的不同运行代次
    static std::atomic<uint64_t> next_run_id{1};
    run_id_ = instance_id_ + "-run-" + std::to_string(next_run_id.fetch_add(1));
}

AlgorithmInstance::~AlgorithmInstance() {
    // 析构时强制停止工作线程并释放底层 ABI 资源
    stop();
}

void AlgorithmInstance::result_bridge(const av_algo_result* res, void* user_data) {
    auto* instance = static_cast<AlgorithmInstance*>(user_data);
    if (!instance || !res) return;

    // 获取并拷贝外部设置的结果回调函数（避免在回调执行期间长期持有锁）
    std::function<void(const av_algo_result&, const av_frame_desc&)> callback;
    {
        std::lock_guard<std::mutex> lock(instance->callback_mutex_);
        callback = instance->result_cb_;
    }
    if (!callback) return;

    // C ABI 契约保证结果回调仅在 instance_process / flush 同步执行期间被触发，
    // 因此 instance->callback_frame_ 在此同步回调上下文中绝对有效。
    try {
        callback(*res, instance->callback_frame_);
    } catch (...) {
        // 跨越 C ABI 边界时严禁抛出 C++ 异常，捕获所有异常防止进程崩溃
    }
}

av_status AlgorithmInstance::init(const av_frame_ops* frame_ops, const av_image_ops* image_ops) {
    // 若已处于运行状态，直接返回成功，避免重复初始化
    if (running_.load(std::memory_order_acquire)) return AV_OK;
    frame_ops_ = frame_ops;

    {
        std::lock_guard<std::mutex> lock(abi_mutex_);
        if (!abi_) {
            // 当 abi 为空时，通常用于调度器逻辑单元测试或作为禁用的实例
        } else {
            // 校验 C ABI 导出函数表各必填函数指针、参数合法性与库句柄有效性
            if (!abi_->instance_create || !abi_->instance_negotiate || !abi_->instance_process ||
                !abi_->instance_update_config || !abi_->instance_flush || !abi_->instance_destroy || !frame_ops_ || !lib_handle_ ||
                params_json_.size() > std::numeric_limits<uint32_t>::max()) {
                return AV_ERR_INVALID_ARG;
            }

            // 构造传递给 C ABI instance_create 的初始化参数
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

            // 调用 C ABI 创建算法实例上下文
            const av_status create_status = static_cast<av_status>(abi_->instance_create(
                lib_handle_, &args, &inst_handle_));
            if (create_status != AV_OK || !inst_handle_) return create_status == AV_OK ? AV_ERR_INTERNAL : create_status;

            // 构造主机端支持并提供的视频帧能力集（格式、内存类型、分辨率范围）
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

            // 与算法实例协商所接受的帧格式
            accepted_caps_ = {};
            accepted_caps_.size = sizeof(av_frame_caps);
            accepted_caps_.api_version = AV_ALGO_API_VERSION;
            const av_status negotiate_status = static_cast<av_status>(abi_->instance_negotiate(
                inst_handle_, &offered, &accepted_caps_));

            // 校验协商返回结果的结构合法性与能力子集约束
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

            // 确保算法接受的所有像素格式均在主机提供的集合中
            for (uint32_t index = 0; index < accepted_caps_.pixel_format_count; ++index) {
                if (!std::any_of(offered.pixel_formats, offered.pixel_formats + offered.pixel_format_count,
                                 [&](uint32_t value) { return value == accepted_caps_.pixel_formats[index]; })) {
                    if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                    inst_handle_ = nullptr;
                    return AV_ERR_INCOMPATIBLE_FRAME;
                }
            }

            // 确保算法接受的所有内存类型均在主机提供的集合中
            for (uint32_t index = 0; index < accepted_caps_.memory_type_count; ++index) {
                if (!std::any_of(offered.memory_types, offered.memory_types + offered.memory_type_count,
                                 [&](uint32_t value) { return value == accepted_caps_.memory_types[index]; })) {
                    if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                    inst_handle_ = nullptr;
                    return AV_ERR_INCOMPATIBLE_FRAME;
                }
            }

            // 校验底层句柄类型匹配（opaque_kind）
            if (accepted_caps_.required_opaque_kind != AV_OPAQUE_NONE &&
                offered.required_opaque_kind != AV_OPAQUE_NONE &&
                accepted_caps_.required_opaque_kind != offered.required_opaque_kind) {
                if (abi_->instance_destroy) abi_->instance_destroy(inst_handle_);
                inst_handle_ = nullptr;
                return AV_ERR_INCOMPATIBLE_FRAME;
            }
        }
    }

    // 标记运行状态并启动后台工作线程
    running_.store(true);
    worker_thread_ = std::thread(&AlgorithmInstance::worker_loop, this);
    return AV_OK;
}

void AlgorithmInstance::push_frame(const av_frame_desc& frame) {
    // 基础参数有效性校验
    if (!running_.load() || frame.size < sizeof(av_frame_desc) || frame.api_version != AV_ALGO_API_VERSION ||
        !frame.frame_token) return;

    // 校验输入帧是否匹配算法协商确定的能力要求
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

    // 抽帧节流：优先按媒体 PTS 采样。硬件解码器可能批量输出帧，使用回调到达时间
    // 会把时间戳连续的视频帧误判为过密；PTS 缺失时才回退到单调时钟。
    if (target_fps_ > 0 && should_throttle_sample(frame.pts_ns)) {
        dropped_frames_.fetch_add(1, std::memory_order_relaxed);
        return;
    }

    // 增加视频帧引用计数，确保其在队列缓存期间不被上游释放
    if (frame_ops_ && frame_ops_->retain && frame_ops_->retain(frame_ops_->ctx, frame.frame_token) != AV_OK) {
        return;
    }

    std::lock_guard<std::mutex> lock(queue_mutex_);
    // 队列满时丢弃最旧的未处理帧，保证实时性（丢最老帧策略）
    if (frame_queue_.size() >= max_queue_size_) {
        av_frame_desc oldest = frame_queue_.front();
        frame_queue_.pop();
        if (frame_ops_ && frame_ops_->release) {
            frame_ops_->release(frame_ops_->ctx, oldest.frame_token);
        }
        dropped_frames_.fetch_add(1);
    }

    // 入队并唤醒推理工作线程
    frame_queue_.push(frame);
    cv_.notify_one();
}

av_status AlgorithmInstance::update_params(const std::string& new_params) {
    if (new_params.size() > std::numeric_limits<uint32_t>::max()) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(abi_mutex_);
    // 调用 C ABI 的 instance_update_config 热更新实例参数
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
    // 调用 C ABI 的 instance_set_rules 下发检测规则（ROI/屏蔽区/警戒线）
    if (abi_ && inst_handle_ && abi_->instance_set_rules) {
        return static_cast<av_status>(abi_->instance_set_rules(
            inst_handle_, rules.empty() ? nullptr : rules.data(), static_cast<uint32_t>(rules.size())));
    }
    return AV_OK;
}

void AlgorithmInstance::stop() {
    // 原子标记停止，防止多重调用
    if (!running_.exchange(false)) return;

    // 唤醒并等待工作线程退出
    cv_.notify_all();
    if (worker_thread_.joinable()) {
        worker_thread_.join();
    }

    // 清空帧队列并释放所有已缓存帧的引用计数
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

    // 调用 C ABI 刷新内部状态并销毁实例句柄
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
            // 阻塞等待待处理帧或停止信号
            cv_.wait(lock, [this] {
                return !running_.load() || !frame_queue_.empty();
            });

            if (!running_.load() && frame_queue_.empty()) {
                break;
            }

            frame = frame_queue_.front();
            frame_queue_.pop();
        }

        // 执行单帧推理处理
        {
            std::lock_guard<std::mutex> lock(abi_mutex_);
            if (abi_ && abi_->instance_process && inst_handle_) {
                callback_frame_ = frame;
                abi_->instance_process(inst_handle_, &frame);
                callback_frame_ = {};
            }
        }

        processed_frames_.fetch_add(1, std::memory_order_relaxed);
        // 无锁累加滑动窗口帧计数（由 get_current_fps 在结算时原子提取并清零）
        fps_window_frames_.fetch_add(1, std::memory_order_relaxed);

        // 推理完成后释放该帧在实例队列中的引用
        if (frame_ops_ && frame_ops_->release) {
            frame_ops_->release(frame_ops_->ctx, frame.frame_token);
        }
    }
}

double AlgorithmInstance::get_current_fps(std::chrono::steady_clock::time_point now) const {
    // 1 秒滑动窗口：上报周期（~100ms）远小于窗口，结算值平滑稳定；
    // 实例无帧进入或已停止时窗口归零，避免前端残留旧值。
    constexpr double kFpsWindowMs = 1000.0;

    std::lock_guard<std::mutex> lock(fps_calc_mutex_);
    if (fps_window_start_.time_since_epoch().count() == 0) {
        // 首次调用初始化窗口起点
        fps_window_start_ = now;
    }
    const double elapsed_ms = std::chrono::duration<double, std::milli>(now - fps_window_start_).count();
    if (elapsed_ms >= kFpsWindowMs) {
        // 进入结算时 elapsed_ms 恒 ≥ 窗口，无需再判 0
        const uint64_t frames = fps_window_frames_.exchange(0, std::memory_order_relaxed);
        current_fps_ = static_cast<double>(frames) / (elapsed_ms / 1000.0);
        fps_window_start_ = now;
    }
    return current_fps_;
}

bool AlgorithmInstance::should_throttle_sample(int64_t pts_ns) {
    constexpr double kSamplingTolerance = 0.9;
    const int64_t interval_ns = static_cast<int64_t>(
        (1'000'000'000.0 / target_fps_) * kSamplingTolerance);
    const auto now = std::chrono::steady_clock::now();
    std::lock_guard<std::mutex> sample_lock(sample_mutex_);

    if (pts_ns > 0) {
        // PTS 采样：与上一被采样帧的时间戳间隔不足则丢弃；缺失基准时接受首帧
        bool throttled = false;
        if (last_sample_pts_ns_ > 0) {
            const int64_t elapsed_ns = pts_ns - last_sample_pts_ns_;
            throttled = elapsed_ns >= 0 && elapsed_ns < interval_ns;
        }
        if (!throttled) last_sample_pts_ns_ = pts_ns;
        last_sample_time_ = now;
        return throttled;
    }

    // 回退到单调时钟采样：PTS 缺失且距上次采样不足则丢弃；首帧记录基准
    if (last_sample_time_.time_since_epoch().count() > 0) {
        const int64_t elapsed_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
            now - last_sample_time_).count();
        if (elapsed_ns < interval_ns) return true;
    }
    last_sample_time_ = now;
    return false;
}

} // namespace aivision::core
