#include "aivision/core/uds_ipc.hpp"
#include "aivision/core/algo_manager.hpp"
#include "aivision/core/image_manager.hpp"
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
#include <iostream>
#include <memory>
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
    std::signal(SIGINT, request_stop);
    std::signal(SIGTERM, request_stop);

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

    auto* image_processor = platform_adapter->get_image_processor();
    if (!image_processor) {
        std::cerr << "failed to create image processor" << std::endl;
        return 1;
    }
    std::shared_ptr<aivision::platform::IImageProcessor> image_processor_owner(
        platform_adapter, image_processor);
    aivision::core::ImageManager::instance().init(
        env_or_default("AIVISION_IMAGE_DIR", "var/images"), std::move(image_processor_owner));

    auto media_backend =
#if defined(AIVISION_PLATFORM_MACOS)
        aivision::media::create_zlm_backend();
#else
        aivision::media::create_mock_backend();
#endif
    if (!media_backend) {
        std::cerr << "failed to create media backend" << std::endl;
        return 1;
    }

    const std::string engine_socket = env_or_default("AIVISION_ENGINE_SOCKET", "/tmp/aivision-engine.sock");
    aivision::core::UdsServer server(engine_socket, platform_adapter, media_backend);
    if (!server.start()) {
        std::cerr << "failed to start engine UDS server at " << engine_socket << std::endl;
        return 1;
    }

    const std::string app_socket = env_or_default("AIVISION_APP_SOCKET", "/tmp/aivision-app.sock");
    std::thread control_plane_thread([&server, platform_adapter, app_socket] {
        aivision::core::UdsClient client(app_socket);
        uint64_t applied_revision = 0;
        auto last_telemetry = std::chrono::steady_clock::now() - std::chrono::seconds(10);
        while (!g_stop_requested.load(std::memory_order_acquire)) {
            aivision::v1::DesiredState desired;
            aivision::v1::ApplyDesiredStateResponse response;
            const bool desired_received = client.get_desired_state(applied_revision, &desired);
            if (desired_received && desired.revision() > applied_revision &&
                server.apply_desired_state(desired, &response) && response.code().empty()) {
                applied_revision = response.applied_revision();
            }
            if (desired_received) {
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

            const auto now = std::chrono::steady_clock::now();
            if (now - last_telemetry >= std::chrono::seconds(10)) {
                const auto metrics = aivision::core::TelemetryCollector(platform_adapter).collect();
                aivision::v1::DeviceTelemetry telemetry;
                telemetry.set_uptime_seconds(metrics.uptime_seconds);
                telemetry.set_cpu_usage_percent(metrics.cpu_usage_percent);
                telemetry.set_memory_usage_percent(metrics.memory_usage_percent);
                telemetry.set_disk_usage_percent(metrics.disk_usage_percent);
                telemetry.set_accelerator_usage_percent(metrics.accelerator_usage_percent);
                telemetry.set_accelerator_usage_supported(metrics.accelerator_supported);
                telemetry.set_temperature_celsius(metrics.temperature_celsius);
                telemetry.set_temperature_supported(metrics.temperature_supported);
                client.report_telemetry(telemetry);
                last_telemetry = now;
            }

            for (int i = 0; i < 20 && !g_stop_requested.load(std::memory_order_acquire); ++i) {
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
            }
        }
    });

    std::cout << "aivision-engine started platform=" << platform_id
              << " socket=" << engine_socket << std::endl;
    while (!g_stop_requested.load(std::memory_order_acquire)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    if (control_plane_thread.joinable()) control_plane_thread.join();
    aivision::core::TaskScheduler::instance().stop_all();
    aivision::core::AlgoManager::instance().stop_all();
    server.stop();
    return 0;
}
