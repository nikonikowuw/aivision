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

#include "argus/core/uds_ipc.hpp"
#include "argus/core/algo_manager.hpp"
#include "argus/core/face_gallery.hpp"
#include "argus/core/image_manager.hpp"
#include "argus/core/live_stream_manager.hpp"
#include "argus/core/logging/logger.hpp"
#include "argus/core/resource_ledger.hpp"
#include "argus/core/task_scheduler.hpp"
#include "argus/core/telemetry_collector.hpp"
#include "argus/media/media_api.hpp"
#include "argus/platform/mock_platform.hpp"
#if defined(ARGUS_PLATFORM_MACOS)
#include "argus/platform/macos_platform.hpp"
#endif


#include <atomic>
#include <chrono>
#include <cctype>
#include <csignal>
#include <cstdlib>
#include <exception>
#include <filesystem>
#include <fstream>
#include <limits>
#include <memory>
#include <nlohmann/json.hpp>
#include <optional>
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

struct IpcEndpoints {
    std::string engine_socket;
    std::string app_socket;
};

// load_ipc_endpoints 从唯一的部署 Profile 解析两个 UDS；未配置 Profile 时保留开发环境变量。
std::optional<IpcEndpoints> load_ipc_endpoints() {
    const char* profile_env = std::getenv("ARGUS_ENGINE_PROFILE");
    if (!profile_env) {
        return IpcEndpoints{
            env_or_default("ARGUS_ENGINE_SOCKET", "/tmp/argus-engine.sock"),
            env_or_default("ARGUS_APP_SOCKET", "/tmp/argus-app.sock"),
        };
    }

    const auto has_outer_whitespace = [](const std::string& value) {
        return !value.empty() &&
               (std::isspace(static_cast<unsigned char>(value.front())) ||
                std::isspace(static_cast<unsigned char>(value.back())));
    };
    const std::string profile_path = profile_env;
    if (profile_path.empty() || has_outer_whitespace(profile_path) ||
        !std::filesystem::path(profile_path).is_absolute()) {
        LOG_ERROR("engine.app", "engine.profile_invalid", "ARGUS_ENGINE_PROFILE must be an absolute path",
                  "ENGINE_PROFILE_INVALID");
        return std::nullopt;
    }
    if (std::getenv("ARGUS_ENGINE_SOCKET") || std::getenv("ARGUS_APP_SOCKET")) {
        LOG_ERROR("engine.app", "engine.profile_env_conflict",
                  "ARGUS_ENGINE_PROFILE cannot be combined with per-socket environment variables",
                  "ENGINE_PROFILE_ENV_CONFLICT");
        return std::nullopt;
    }

    try {
        std::ifstream input(profile_path);
        if (!input) {
            LOG_ERROR("engine.app", "engine.profile_read_failed", "cannot read ARGUS_ENGINE_PROFILE",
                      "ENGINE_PROFILE_READ_FAILED");
            return std::nullopt;
        }
        const auto profile = nlohmann::json::parse(input);
        if (profile.value("schema_version", 0) != 1) {
            LOG_ERROR("engine.app", "engine.profile_schema_unsupported", "unsupported engine profile schema",
                      "ENGINE_PROFILE_SCHEMA_UNSUPPORTED");
            return std::nullopt;
        }

        const std::string runtime_dir_value = profile.at("paths").at("runtime_dir").get<std::string>();
        if (runtime_dir_value.empty() || has_outer_whitespace(runtime_dir_value)) {
            LOG_ERROR("engine.app", "engine.profile_runtime_invalid", "profile runtime_dir is invalid",
                      "ENGINE_PROFILE_RUNTIME_INVALID");
            return std::nullopt;
        }
        const std::filesystem::path runtime_dir =
            std::filesystem::path(runtime_dir_value).lexically_normal();
        if (!runtime_dir.is_absolute()) {
            LOG_ERROR("engine.app", "engine.profile_runtime_invalid", "profile runtime_dir must be absolute",
                      "ENGINE_PROFILE_RUNTIME_INVALID");
            return std::nullopt;
        }

        const auto resolve = [&runtime_dir, &has_outer_whitespace](const std::string& socket_name) -> std::optional<std::string> {
            const std::filesystem::path relative = socket_name;
            if (socket_name.empty() || has_outer_whitespace(socket_name) ||
                socket_name.find('\0') != std::string::npos || relative.is_absolute() ||
                relative == "." || relative == ".." || relative.lexically_normal() != relative) {
                return std::nullopt;
            }
            const auto resolved = (runtime_dir / relative).lexically_normal();
            const auto relative_to_runtime = resolved.lexically_relative(runtime_dir);
            if (relative_to_runtime.empty() || relative_to_runtime == ".." ||
                relative_to_runtime.string().starts_with("../")) {
                return std::nullopt;
            }
            return resolved.string();
        };

        const auto app_socket = resolve(profile.at("ipc").at("app_socket").get<std::string>());
        const auto engine_socket = resolve(profile.at("ipc").at("engine_socket").get<std::string>());
        if (!app_socket || !engine_socket || *app_socket == *engine_socket) {
            LOG_ERROR("engine.app", "engine.profile_ipc_invalid", "profile IPC sockets are invalid",
                      "ENGINE_PROFILE_IPC_INVALID");
            return std::nullopt;
        }
        return IpcEndpoints{*engine_socket, *app_socket};
    } catch (const std::exception&) {
        LOG_ERROR("engine.app", "engine.profile_parse_failed", "cannot parse engine profile",
                  "ENGINE_PROFILE_PARSE_FAILED");
        return std::nullopt;
    }
}
} // namespace

int main() {
    // 0. 初始化结构化日志系统并读取环境变量配置
    argus::logging::Level log_lvl = argus::logging::Level::Info;
    bool invalid_log_level = false;
    const char* env_log_level = std::getenv("ARGUS_LOG_LEVEL");
    if (env_log_level) {
        auto parsed = argus::logging::parse_level(env_log_level);
        if (parsed.has_value()) {
            log_lvl = *parsed;
        } else {
            invalid_log_level = true;
        }
    }
    argus::logging::Logger::initialize(log_lvl);
    if (invalid_log_level) {
        LOG_WARN("engine.app", "engine.log_level_invalid", "Invalid ARGUS_LOG_LEVEL; falling back to INFO",
                 "ENGINE_LOG_LEVEL_INVALID");
    }

    // 注册系统退出信号监听（SIGINT / SIGTERM），触发原子停止标志
    std::signal(SIGINT, request_stop);
    std::signal(SIGTERM, request_stop);

    // 1. 初始化平台适配器并配置资源账本上限
    std::shared_ptr<argus::platform::IPlatformAdapter> platform_adapter;
#if defined(ARGUS_PLATFORM_MACOS)
    platform_adapter = std::make_shared<argus::platform::MacosPlatformAdapter>();
    const std::string platform_id = "macos-arm64-coreml";
#else
    platform_adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    const std::string platform_id = "mock";
#endif
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter(platform_id, platform_adapter);
    registry.set_active_platform(platform_id);
    const auto& profile = platform_adapter->get_profile();
    argus::core::ResourceLedger::instance().set_limits(
        profile.total_compute_units, profile.reserved_compute_units, profile.min_free_memory_bytes);

    // 2. 初始化抓拍图片管理器
    auto* image_processor = platform_adapter->get_image_processor();
    if (!image_processor) {
        LOG_ERROR("engine.app", "engine.image_processor_init_failed",
                  "failed to create image processor", "ENGINE_IMAGE_PROCESSOR_INIT_FAILED");
        argus::logging::Logger::shutdown();
        return 1;
    }
    std::shared_ptr<argus::platform::IImageProcessor> image_processor_owner(
        platform_adapter, image_processor);
    argus::core::ImageManager::instance().init(
        env_or_default("ARGUS_IMAGE_DIR", "var/images"), std::move(image_processor_owner));

    // 3. 初始化媒体拉流后端
    auto media_backend =
#if defined(ARGUS_PLATFORM_MACOS)
        argus::media::create_zlm_backend();
#else
        argus::media::create_mock_backend();
#endif
    if (!media_backend) {
        LOG_ERROR("engine.app", "engine.media_backend_init_failed",
                  "failed to create media backend", "MEDIA_BACKEND_INIT_FAILED");
        argus::logging::Logger::shutdown();
        return 1;
    }

    // 3.5 启动流媒体 HTTP/WebSocket 服务（默认 8080）
    const auto live_http_port = static_cast<uint16_t>(
        std::strtoul(env_or_default("ARGUS_LIVE_HTTP_PORT", "8080"), nullptr, 10));
    argus::core::LiveStreamManager::instance().start_server(live_http_port);

    // 4. 解析统一的 UDS Profile（未配置 Profile 时保留开发环境变量兼容）
    const auto ipc_endpoints = load_ipc_endpoints();
    if (!ipc_endpoints) {
        argus::logging::Logger::shutdown();
        return 1;
    }
    argus::core::UdsServer server(ipc_endpoints->engine_socket, platform_adapter, media_backend,
                                     ipc_endpoints->app_socket);
    if (!server.start()) {
        LOG_ERROR("engine.app", "engine.uds_start_failed",
                  "failed to start engine UDS server", "ENGINE_UDS_START_FAILED",
                  {{"platform_id", platform_id}});
        argus::logging::Logger::shutdown();
        return 1;
    }

    // 5. 启动控制面心跳、期望状态同步与遥测指标上报后台线程
    const std::string app_socket = ipc_endpoints->app_socket;
    std::thread control_plane_thread([&server, platform_adapter, app_socket] {
        argus::core::UdsClient client(app_socket);
        uint64_t applied_revision = 0;
        uint64_t applied_gallery_revision = argus::core::FaceGallery::instance().revision();
        auto last_telemetry = std::chrono::steady_clock::now() - std::chrono::seconds(10);
        while (!g_stop_requested.load(std::memory_order_acquire)) {
            // 拉取并应用控制面的期望状态（DesiredState）
            argus::v1::DesiredState desired;
            argus::v1::ApplyDesiredStateResponse response;
            const bool desired_received = client.get_desired_state(applied_revision, &desired);
            if (desired_received && desired.revision() > applied_revision &&
                server.apply_desired_state(desired, &response) && response.code().empty()) {
                applied_revision = response.applied_revision();
            }

            // 人脸底库使用独立 revision 主动拉取，避免注册样本触发媒体管线重应用。
            argus::v1::GetFaceGalleryResponse gallery_response;
            if (client.get_face_gallery(applied_gallery_revision, &gallery_response)) {
                if (gallery_response.changed()) {
                    if (gallery_response.gallery_revision() == 0 ||
                        gallery_response.gallery_revision() <= applied_gallery_revision) {
                        LOG_WARN("engine.face_gallery", "face_gallery.response_invalid",
                                 "changed face gallery response has stale revision",
                                 "FACE_GALLERY_RESPONSE_INVALID",
                                 {{"revision", std::to_string(gallery_response.gallery_revision())},
                                  {"applied_revision", std::to_string(applied_gallery_revision)}});
                    } else {
                        std::string gallery_error;
                        if (argus::core::FaceGallery::instance().load_from(gallery_response, &gallery_error)) {
                            applied_gallery_revision = gallery_response.gallery_revision();
                        } else {
                            LOG_WARN("engine.face_gallery", "face_gallery.sync_failed",
                                     gallery_error.empty() ? "face gallery snapshot rejected" : gallery_error,
                                     "FACE_GALLERY_SYNC_FAILED",
                                     {{"revision", std::to_string(gallery_response.gallery_revision())}});
                        }
                    }
                } else if (gallery_response.gallery_revision() != applied_gallery_revision ||
                           gallery_response.entries_size() != 0) {
                    LOG_WARN("engine.face_gallery", "face_gallery.response_invalid",
                             "unchanged face gallery response has unexpected revision or entries",
                             "FACE_GALLERY_RESPONSE_INVALID",
                             {{"revision", std::to_string(gallery_response.gallery_revision())},
                              {"applied_revision", std::to_string(applied_gallery_revision)},
                              {"entry_count", std::to_string(gallery_response.entries_size())}});
                }
            }
            // 运行态上报独立于 DesiredState 拉取：控制面短暂不可用时仍需刷新 FPS/状态，
            // 上报失败由下一轮重试，避免 Go 侧长期停留在旧的 STARTING/0。
            for (const auto& camera_id : argus::core::TaskScheduler::instance().task_ids()) {
                const auto task = argus::core::TaskScheduler::instance().get_task(camera_id);
                if (!task) continue;
                argus::v1::TaskState state;
                state.set_camera_id(camera_id);
                state.set_last_frame_wall_time_ns(task->get_last_frame_wall_time_ns());
                switch (task->get_state()) {
                    case argus::core::CameraState::CONNECTING:
                        state.set_status(argus::v1::TASK_STATUS_STARTING);
                        break;
                    case argus::core::CameraState::RUNNING:
                        state.set_status(argus::v1::TASK_STATUS_RUNNING);
                        break;
                    case argus::core::CameraState::RECONNECTING:
                        state.set_status(argus::v1::TASK_STATUS_RECONNECTING);
                        break;
                    case argus::core::CameraState::ERROR:
                        state.set_status(argus::v1::TASK_STATUS_ERROR);
                        break;
                    default:
                        state.set_status(argus::v1::TASK_STATUS_STOPPED);
                        break;
                }
                client.report_task_state(state);
            }
            for (const auto& instance_id : argus::core::AlgoManager::instance().instance_ids()) {
                const auto instance = argus::core::AlgoManager::instance().get(instance_id);
                if (!instance) continue;
                argus::v1::InstanceState state;
                state.set_instance_id(instance_id);
                state.set_status(instance->is_running()
                    ? argus::v1::INSTANCE_STATUS_RUNNING
                    : argus::v1::INSTANCE_STATUS_STOPPED);
                // 上报 1s 滑动窗口结算的推理帧率；未满窗口或未运行时为 0
                state.set_current_fps(static_cast<float>(instance->get_current_fps()));
                client.report_instance_state(state);
            }

            // 定期（每 10 秒）采集并向 Go 后端上报宿主机遥测指标
            const auto now = std::chrono::steady_clock::now();
            if (now - last_telemetry >= std::chrono::seconds(10)) {
                const auto metrics = argus::core::TelemetryCollector(platform_adapter).collect();
                argus::v1::DeviceTelemetry telemetry;
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

    LOG_INFO("engine.app", "engine.started", "argus-engine started successfully", "",
             {{"platform_id", platform_id}});
    // 主线程等待退出信号
    while (!g_stop_requested.load(std::memory_order_acquire)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    LOG_INFO("engine.app", "engine.stopping", "Shutting down argus-engine");

    // 优雅停机并清理资源
    if (control_plane_thread.joinable()) control_plane_thread.join();
    argus::core::TaskScheduler::instance().stop_all();
    argus::core::AlgoManager::instance().stop_all();
    server.stop();

    argus::logging::Logger::shutdown();
    return 0;
}
