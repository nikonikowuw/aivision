/**
 * @file main.cpp
 * @brief Engine 守护进程主入口
 * 
 * 启动流程：
 * 1. 注册 SIGINT / SIGTERM 信号处理函数实现优雅停机；
 * 2. 依据编译平台初始化并激活对应的 PlatformAdapter 与 MediaBackend；
 * 3. 初始化全局单例（ImageManager / FramePool / ResourceLedger 等）；
 * 4. 启动 UdsServer（监听 engine.sock）并创建 UdsClient（连接 app.sock）；
 * 5. 启动后台遥测采集与上报循环；
 * 6. 停机时安全注销全部任务与实例，清理 socket 文件。
 */

#include "aivision/core/uds_ipc.hpp"
#include "aivision/core/algo_manager.hpp"
#include "aivision/core/image_manager.hpp"
#include "aivision/core/logging/logger.hpp"
#include "aivision/core/resource_ledger.hpp"
#include "aivision/core/task_scheduler.hpp"
#include "aivision/core/telemetry_collector.hpp"
#include "aivision/media/media_api.hpp"
#include "aivision/platform/mock_platform.hpp"
#if defined(AIVISION_PLATFORM_MACOS)
#include "aivision/platform/macos_platform.hpp"
#endif


#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdlib>
#include <limits>
#include <memory>
#include <string>
#include <thread>
#include <utility>

namespace {
std::atomic<bool> g_stop_requested{false};

void request_stop(int) {
    g_stop_requested.store(true, std::memory_order_release);
}

const char* env_or_default(const char* name, const char* fallback) {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}
} // namespace

int main() {
    // 0. 初始化结构化日志系统并读取环境变量配置
    aivision::logging::Level log_lvl = aivision::logging::Level::Info;
    bool invalid_log_level = false;
    const char* env_log_level = std::getenv("AIVISION_LOG_LEVEL");
    if (env_log_level) {
        auto parsed = aivision::logging::parse_level(env_log_level);
        if (parsed.has_value()) {
            log_lvl = *parsed;
        } else {
            invalid_log_level = true;
        }
    }
    aivision::logging::Logger::initialize(log_lvl);
    if (invalid_log_level) {
        LOG_WARN("engine.app", "engine.log_level_invalid", "Invalid AIVISION_LOG_LEVEL; falling back to INFO",
                 "ENGINE_LOG_LEVEL_INVALID");
    }

    // 注册系统退出信号监听（SIGINT / SIGTERM），触发原子停止标志
    std::signal(SIGINT, request_stop);
    std::signal(SIGTERM, request_stop);

    // 1. 初始化平台适配器并配置资源账本上限
    std::shared_ptr<aivision::platform::IPlatformAdapter> platform_adapter;
#if defined(AIVISION_PLATFORM_MACOS)
    platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string platform_id = "macos-arm64-coreml";
#else
    platform_adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    const std::string platform_id = "mock";
#endif
    auto& registry = aivision::platform::PlatformRegistry::instance();
    registry.register_adapter(platform_id, platform_adapter);
    registry.set_active_platform(platform_id);
    const auto& profile = platform_adapter->get_profile();
    aivision::core::ResourceLedger::instance().set_limits(
        profile.total_compute_units, profile.reserved_compute_units, profile.min_free_memory_bytes);

    // 2. 初始化抓拍图片管理器
    auto* image_processor = platform_adapter->get_image_processor();
    if (!image_processor) {
        LOG_ERROR("engine.app", "engine.image_processor_init_failed",
                  "failed to create image processor", "ENGINE_IMAGE_PROCESSOR_INIT_FAILED");
        aivision::logging::Logger::shutdown();
        return 1;
    }
    std::shared_ptr<aivision::platform::IImageProcessor> image_processor_owner(
        platform_adapter, image_processor);
    aivision::core::ImageManager::instance().init(
        env_or_default("AIVISION_IMAGE_DIR", "var/images"), std::move(image_processor_owner));

    // 3. 初始化媒体拉流后端
    auto media_backend =
#if defined(AIVISION_PLATFORM_MACOS)
        aivision::media::create_zlm_backend();
#else
        aivision::media::create_mock_backend();
#endif
    if (!media_backend) {
        LOG_ERROR("engine.app", "engine.media_backend_init_failed",
                  "failed to create media backend", "MEDIA_BACKEND_INIT_FAILED");
        aivision::logging::Logger::shutdown();
        return 1;
    }

    // 4. 启动 Unix Domain Socket (UDS) gRPC 服务端监听
    const std::string engine_socket = env_or_default("AIVISION_ENGINE_SOCKET", "/tmp/aivision-engine.sock");
    aivision::core::UdsServer server(engine_socket, platform_adapter, media_backend);
    if (!server.start()) {
        LOG_ERROR("engine.app", "engine.uds_start_failed",
                  "failed to start engine UDS server", "ENGINE_UDS_START_FAILED",
                  {{"platform_id", platform_id}});
        aivision::logging::Logger::shutdown();
        return 1;
    }

    // 5. 启动控制面心跳、期望状态同步与遥测指标上报后台线程
    const std::string app_socket = env_or_default("AIVISION_APP_SOCKET", "/tmp/aivision-app.sock");
    std::thread control_plane_thread([&server, platform_adapter, app_socket] {
        aivision::core::UdsClient client(app_socket);
        uint64_t applied_revision = 0;
        auto last_telemetry = std::chrono::steady_clock::now() - std::chrono::seconds(10);
        while (!g_stop_requested.load(std::memory_order_acquire)) {
            // 拉取并应用控制面的期望状态（DesiredState）
            aivision::v1::DesiredState desired;
            aivision::v1::ApplyDesiredStateResponse response;
            const bool desired_received = client.get_desired_state(applied_revision, &desired);
            if (desired_received && desired.revision() > applied_revision &&
                server.apply_desired_state(desired, &response) && response.code().empty()) {
                applied_revision = response.applied_revision();
            }
            if (desired_received) {
                // 上报各摄像头任务与算法实例的当前运行状态
                for (const auto& camera_id : aivision::core::TaskScheduler::instance().task_ids()) {
                    const auto task = aivision::core::TaskScheduler::instance().get_task(camera_id);
                    if (!task) continue;
                    aivision::v1::TaskState state;
                    state.set_camera_id(camera_id);
                    switch (task->get_state()) {
                        case aivision::core::CameraState::CONNECTING:
                            state.set_status(aivision::v1::TASK_STATUS_STARTING);
                            break;
                        case aivision::core::CameraState::RUNNING:
                            state.set_status(aivision::v1::TASK_STATUS_RUNNING);
                            break;
                        case aivision::core::CameraState::RECONNECTING:
                            state.set_status(aivision::v1::TASK_STATUS_RECONNECTING);
                            break;
                        case aivision::core::CameraState::ERROR:
                            state.set_status(aivision::v1::TASK_STATUS_ERROR);
                            break;
                        default:
                            state.set_status(aivision::v1::TASK_STATUS_STOPPED);
                            break;
                    }
                    client.report_task_state(state);
                }
                for (const auto& instance_id : aivision::core::AlgoManager::instance().instance_ids()) {
                    const auto instance = aivision::core::AlgoManager::instance().get(instance_id);
                    if (!instance) continue;
                    aivision::v1::InstanceState state;
                    state.set_instance_id(instance_id);
                    state.set_status(instance->is_running()
                        ? aivision::v1::INSTANCE_STATUS_RUNNING
                        : aivision::v1::INSTANCE_STATUS_STOPPED);
                    client.report_instance_state(state);
                }
            }

            // 定期（每 10 秒）采集并向 Go 后端上报宿主机遥测指标
            const auto now = std::chrono::steady_clock::now();
            if (now - last_telemetry >= std::chrono::seconds(10)) {
                const auto metrics = aivision::core::TelemetryCollector(platform_adapter).collect();
                aivision::v1::DeviceTelemetry telemetry;
                telemetry.set_uptime_seconds(metrics.uptime_seconds);
                telemetry.set_cpu_usage_percent(metrics.cpu_usage_percent);
                telemetry.set_memory_usage_percent(metrics.memory_usage_percent);
                telemetry.set_disk_usage_percent(metrics.disk_usage_percent);
                const float unsupported_metric = std::numeric_limits<float>::quiet_NaN();
                telemetry.set_accelerator_usage_percent(
                    metrics.accelerator_supported ? metrics.accelerator_usage_percent : unsupported_metric);
                telemetry.set_accelerator_usage_supported(metrics.accelerator_supported);
                telemetry.set_temperature_celsius(
                    metrics.temperature_supported ? metrics.temperature_celsius : unsupported_metric);
                telemetry.set_temperature_supported(metrics.temperature_supported);
                client.report_telemetry(telemetry);
                last_telemetry = now;
            }

            for (int i = 0; i < 20 && !g_stop_requested.load(std::memory_order_acquire); ++i) {
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
            }
        }
    });

    LOG_INFO("engine.app", "engine.started", "aivision-engine started successfully", "",
             {{"platform_id", platform_id}});
    // 主线程等待退出信号
    while (!g_stop_requested.load(std::memory_order_acquire)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    LOG_INFO("engine.app", "engine.stopping", "Shutting down aivision-engine");

    // 优雅停机并清理资源
    if (control_plane_thread.joinable()) control_plane_thread.join();
    aivision::core::TaskScheduler::instance().stop_all();
    aivision::core::AlgoManager::instance().stop_all();
    server.stop();

    aivision::logging::Logger::shutdown();
    return 0;
}
