#include "aivision/core/uds_ipc.hpp"
#include "aivision/core/image_manager.hpp"
#include "aivision/core/algo_manager.hpp"
#include "aivision/core/algo_sandbox.hpp"
#include "aivision/core/resource_ledger.hpp"
#include "aivision/core/task_scheduler.hpp"
#include "aivision/core/telemetry_collector.hpp"
#include "aivision/platform/platform_api.hpp"

#include <cerrno>
#include <atomic>
#include <algorithm>
#include <array>
#include <cstring>
#include <cctype>
#include <cmath>
#include <nlohmann/json.hpp>
#include <filesystem>
#include <dlfcn.h>
#include <fstream>
#include <cstdlib>
#include <mutex>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>
#include <vector>
#include <utility>
#include <unordered_map>
#include <unordered_set>

namespace fs = std::filesystem;

namespace aivision::core {
namespace {

std::string env_or_default(const char* name, const char* fallback) {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}

bool safe_package_component(const std::string& value) {
    if (value.empty() || value.size() > 128) return false;
    for (const unsigned char ch : value) {
        if (!(std::isalnum(ch) || ch == '-' || ch == '_' || ch == '.')) return false;
    }
    return true;
}

fs::path package_root() {
    return fs::path(env_or_default("AIVISION_PACKAGE_DIR", "var/packages"));
}

bool safe_package_relative(const std::string& value) {
    if (value.empty() || value.find('\\') != std::string::npos) return false;
    const fs::path path(value);
    if (path.is_absolute()) return false;
    for (const auto& part : path) {
        if (part.empty() || part == "." || part == "..") return false;
    }
    return true;
}


bool write_active_version(const std::string& algorithm_id, const std::string& version, std::string& error) {
    const fs::path active_dir = package_root() / "active";
    std::error_code ec;
    fs::create_directories(active_dir, ec);
    if (ec) {
        error = ec.message();
        return false;
    }
    const fs::path target = active_dir / (algorithm_id + ".version");
    const fs::path temporary = target.string() + ".part";
    std::ofstream output(temporary, std::ios::trunc);
    if (!output) {
        error = "cannot write active package marker";
        return false;
    }
    output << version << '\n';
    output.flush();
    if (!output) {
        error = "cannot flush active package marker";
        return false;
    }
    output.close();
    fs::rename(temporary, target, ec);
    if (ec) {
        fs::remove(temporary, ec);
        error = "cannot activate package version: " + ec.message();
        return false;
    }
    return true;
}

void set_package_validation_failure(const ValidationResult& result, const char* operation,
                                    std::string* code, std::string* message) {
    *code = result.error_code.empty() ? "PACKAGE_VALIDATION_FAILED" : result.error_code;
    *message = std::string(operation) + " failed at " + result.error_stage + ": " + result.error_message;
}

struct LoadedPackage {
    std::string package_root;
    std::string platform_id;
    void* dynamic_library = nullptr;
    const av_algo_abi* abi = nullptr;
    av_algo_library library = nullptr;
    std::vector<std::pair<int32_t, uint32_t>> fps_tiers;
    uint64_t memory_bytes = 0;

    ~LoadedPackage() {
        if (abi && library && abi->library_close) abi->library_close(library);
        if (dynamic_library) ::dlclose(dynamic_library);
    }
};

bool prepare_socket_path(const std::string& path) {
    struct stat st{};
    if (::lstat(path.c_str(), &st) != 0) {
        return errno == ENOENT;
    }
    if (!S_ISSOCK(st.st_mode)) return false;

    const int fd = ::socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) return false;
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    if (path.size() >= sizeof(address.sun_path)) {
        ::close(fd);
        return false;
    }
    std::memcpy(address.sun_path, path.c_str(), path.size() + 1);
    const int connect_result = ::connect(fd, reinterpret_cast<sockaddr*>(&address), sizeof(address));
    const int connect_errno = errno;
    ::close(fd);
    if (connect_result == 0) return false;
    if (connect_errno != ECONNREFUSED && connect_errno != ENOENT && connect_errno != ENOTCONN) return false;
    return ::unlink(path.c_str()) == 0 || errno == ENOENT;
}

class EngineServiceImpl final : public aivision::v1::EngineService::Service {
public:
    EngineServiceImpl(std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
                      std::shared_ptr<media::IMediaBackend> media_backend)
        : platform_adapter_(std::move(platform_adapter)),
          media_backend_(std::move(media_backend)),
          app_client_(std::make_shared<UdsClient>(env_or_default("AIVISION_APP_SOCKET", "/tmp/aivision-app.sock"))) {}

    ~EngineServiceImpl() override {
        TaskScheduler::instance().stop_all();
        AlgoManager::instance().stop_all();
        loaded_packages_.clear();
    }

    grpc::Status ApplyDesiredState(grpc::ServerContext*, const aivision::v1::ApplyDesiredStateRequest* request,
                                   aivision::v1::ApplyDesiredStateResponse* response) override {
        if (!request || !request->has_desired_state()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("desired_state is required");
            return grpc::Status::OK;
        }
        return apply_desired_state(&request->desired_state(), response);
    }

    grpc::Status apply_desired_state(const aivision::v1::DesiredState* desired_ptr,
                                     aivision::v1::ApplyDesiredStateResponse* response) {
        if (!desired_ptr || !response) return grpc::Status::OK;
        const auto& desired = *desired_ptr;
        std::lock_guard<std::mutex> reconcile_lock(reconcile_mutex_);
        const uint64_t current_revision = applied_revision_.load(std::memory_order_acquire);
        if (desired.revision() <= current_revision) {
            response->set_applied_revision(current_revision);
            response->set_code("STALE_REVISION");
            response->set_error_message("desired state revision is not newer than the applied revision");
            return grpc::Status::OK;
        }

        bool failed = false;
        for (const auto& task_config : desired.tasks()) {
            auto* item = response->add_results();
            item->set_kind(aivision::v1::RECONCILE_ITEM_KIND_TASK);
            item->set_id(task_config.camera_id());
            const std::string error = upsert_task(task_config);
            if (error.empty()) {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_OK);
            } else {
                failed = true;
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code(error);
                item->set_error_message(error);
            }
        }
        std::unordered_set<std::string> desired_task_ids;
        for (const auto& task_config : desired.tasks()) desired_task_ids.insert(task_config.camera_id());
        for (const auto& camera_id : TaskScheduler::instance().task_ids()) {
            if (desired_task_ids.find(camera_id) != desired_task_ids.end()) continue;
            TaskScheduler::instance().stop_task(camera_id);
            auto* item = response->add_results();
            item->set_kind(aivision::v1::RECONCILE_ITEM_KIND_TASK);
            item->set_id(camera_id);
            item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_OK);
        }

        for (const auto& instance : desired.instances()) {
            auto* item = response->add_results();
            item->set_kind(aivision::v1::RECONCILE_ITEM_KIND_INSTANCE);
            item->set_id(instance.instance_id());
            const std::string error = reconcile_instance(instance);
            if (error.empty()) {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_OK);
            } else {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code(error);
                item->set_error_message(error);
                failed = true;
            }
        }
        std::unordered_set<std::string> desired_instance_ids;
        for (const auto& instance : desired.instances()) desired_instance_ids.insert(instance.instance_id());
        for (const auto& instance_id : AlgoManager::instance().instance_ids()) {
            if (desired_instance_ids.find(instance_id) == desired_instance_ids.end()) remove_instance(instance_id);
        }

        for (const auto& package : desired.active_package_versions()) {
            auto* item = response->add_results();
            item->set_kind(aivision::v1::RECONCILE_ITEM_KIND_PACKAGE);
            item->set_id(package.algorithm_id());
            if (!safe_package_component(package.algorithm_id()) || !safe_package_component(package.version())) {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code("PACKAGE_ID_INVALID");
                item->set_error_message("package identity contains unsafe characters");
                failed = true;
                continue;
            }
            const fs::path target = package_root() / package.algorithm_id() / package.version();
            std::error_code package_error;
            const auto target_status = fs::symlink_status(target, package_error);
            std::string activation_error;
            if (package_error || !fs::is_directory(target_status) || fs::is_symlink(target_status)) {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code("PACKAGE_NOT_FOUND");
                item->set_error_message("package version is not installed");
                failed = true;
            } else if (!write_active_version(package.algorithm_id(), package.version(), activation_error)) {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code("PACKAGE_ACTIVATION_FAILED");
                item->set_error_message(activation_error);
                failed = true;
            } else {
                item->set_status(aivision::v1::RECONCILE_ITEM_STATUS_OK);
            }
        }
        if (failed) {
            response->set_applied_revision(current_revision);
            response->set_code("RECONCILE_FAILED");
            response->set_error_message("one or more desired-state items failed");
            return grpc::Status::OK;
        }
        applied_revision_.store(desired.revision(), std::memory_order_release);
        response->set_applied_revision(desired.revision());
        return grpc::Status::OK;
    }

    grpc::Status UpsertTask(grpc::ServerContext*, const aivision::v1::UpsertTaskRequest* request,
                            aivision::v1::UpsertTaskResponse* response) override {
        if (!request || !request->has_task()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("task is required");
            return grpc::Status::OK;
        }
        const std::string error = upsert_task(request->task());
        if (!error.empty()) {
            response->set_code(error);
            response->set_error_message(error);
        }
        return grpc::Status::OK;
    }

    grpc::Status SetInstanceState(grpc::ServerContext*, const aivision::v1::SetInstanceStateRequest* request,
                                   aivision::v1::SetInstanceStateResponse* response) override {
        std::lock_guard<std::mutex> reconcile_lock(reconcile_mutex_);
        if (!request || request->instance_id().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("instance_id is required");
            return grpc::Status::OK;
        }
        const auto instance = AlgoManager::instance().get(request->instance_id());
        if (!instance) {
            response->set_code("INSTANCE_NOT_FOUND");
            response->set_error_message("algorithm instance does not exist");
        } else if (!request->enabled()) {
            remove_instance(request->instance_id());
        } else {
            response->set_code("INSTANCE_RESTART_UNSUPPORTED");
            response->set_error_message("an instance must be recreated after it is stopped");
        }
        return grpc::Status::OK;
    }

    grpc::Status UpdateInstanceConfig(grpc::ServerContext*, const aivision::v1::UpdateInstanceConfigRequest* request,
                                      aivision::v1::UpdateInstanceConfigResponse* response) override {
        std::lock_guard<std::mutex> reconcile_lock(reconcile_mutex_);
        if (!request || request->instance_id().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("instance_id is required");
            return grpc::Status::OK;
        }
        const auto instance = AlgoManager::instance().get(request->instance_id());
        if (!instance) {
            response->set_code("INSTANCE_NOT_FOUND");
            response->set_error_message("algorithm instance does not exist");
            return grpc::Status::OK;
        }
        const av_status status = instance->update_params(request->params_json());
        if (status != AV_OK) {
            response->set_code("CONFIG_UPDATE_FAILED");
            response->set_error_message("algorithm rejected the new configuration");
        } else {
            instance_configs_[request->instance_id()].set_params_json(request->params_json());
        }
        return grpc::Status::OK;
    }

    grpc::Status InstallPackage(grpc::ServerContext*, const aivision::v1::InstallPackageRequest* request,
                                aivision::v1::InstallPackageResponse* response) override {
        if (!request || request->package_path().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("package_path is required");
            return grpc::Status::OK;
        }
        install_package(request->package_path(), "InstallPackage", response);
        return grpc::Status::OK;
    }

    grpc::Status UpgradePackage(grpc::ServerContext*, const aivision::v1::UpgradePackageRequest* request,
                                aivision::v1::UpgradePackageResponse* response) override {
        if (!request || request->package_path().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("package_path is required");
            return grpc::Status::OK;
        }
        install_package(request->package_path(), "UpgradePackage", response);
        return grpc::Status::OK;
    }

    grpc::Status RollbackPackage(grpc::ServerContext*, const aivision::v1::RollbackPackageRequest* request,
                                 aivision::v1::RollbackPackageResponse* response) override {
        std::lock_guard<std::mutex> lifecycle_lock(reconcile_mutex_);
        if (!request || !safe_package_component(request->algorithm_id()) ||
            !safe_package_component(request->target_version())) {
            response->set_code("INVALID_ARG");
            response->set_error_message("algorithm_id and target_version are required and must be safe");
            return grpc::Status::OK;
        }
        const fs::path target = package_root() / request->algorithm_id() / request->target_version();
        std::error_code ec;
        if (!fs::is_directory(target, ec) || fs::is_symlink(fs::symlink_status(target, ec))) {
            response->set_code("PACKAGE_NOT_FOUND");
            response->set_error_message("target package version is not installed");
            return grpc::Status::OK;
        }
        std::string error;
        if (!write_active_version(request->algorithm_id(), request->target_version(), error)) {
            response->set_code("PACKAGE_ACTIVATION_FAILED");
            response->set_error_message(error);
        } else {
            const std::string restart_error = restart_package_instances(
                request->algorithm_id(), request->target_version());
            if (!restart_error.empty()) {
                response->set_code("PACKAGE_RESTART_FAILED");
                response->set_error_message(restart_error);
            }
        }
        return grpc::Status::OK;
    }

    grpc::Status UninstallPackage(grpc::ServerContext*, const aivision::v1::UninstallPackageRequest* request,
                                  aivision::v1::UninstallPackageResponse* response) override {
        std::lock_guard<std::mutex> lifecycle_lock(reconcile_mutex_);
        if (!request || !safe_package_component(request->algorithm_id()) ||
            !safe_package_component(request->version())) {
            response->set_code("INVALID_ARG");
            response->set_error_message("algorithm_id and version are required and must be safe");
            return grpc::Status::OK;
        }
        if (AlgoManager::instance().has_package_reference(request->algorithm_id(), request->version())) {
            response->set_code("PACKAGE_IN_USE");
            response->set_error_message("package version is referenced by a running instance");
            return grpc::Status::OK;
        }
        const fs::path target = package_root() / request->algorithm_id() / request->version();
        std::error_code ec;
        const auto status = fs::symlink_status(target, ec);
        if (ec || !fs::is_directory(status) || fs::is_symlink(status)) {
            response->set_code("PACKAGE_NOT_FOUND");
            response->set_error_message("package version is not installed");
            return grpc::Status::OK;
        }
        fs::remove_all(target, ec);
        if (ec) {
            response->set_code("PACKAGE_DELETE_FAILED");
            response->set_error_message(ec.message());
            return grpc::Status::OK;
        }
        const fs::path active_marker = package_root() / "active" / (request->algorithm_id() + ".version");
        std::ifstream marker(active_marker);
        std::string active_version;
        std::getline(marker, active_version);
        marker.close();
        if (active_version == request->version()) fs::remove(active_marker, ec);
        return grpc::Status::OK;
    }

    grpc::Status DeleteImages(grpc::ServerContext*, const aivision::v1::DeleteImagesRequest* request,
                              aivision::v1::DeleteImagesResponse* response) override {
        if (!request) {
            response->set_code("INVALID_ARG");
            response->set_error_message("request is required");
            return grpc::Status::OK;
        }
        bool failed = false;
        for (const auto& id : request->image_ids()) {
            auto* item = response->add_results();
            item->set_image_id(id);
            const auto status = ImageManager::instance().delete_image_with_status(id);
            item->set_status(status == ImageDeleteStatus::DELETED
                ? aivision::v1::IMAGE_DELETE_STATUS_DELETED
                : status == ImageDeleteStatus::ALREADY_ABSENT
                    ? aivision::v1::IMAGE_DELETE_STATUS_ALREADY_ABSENT
                    : aivision::v1::IMAGE_DELETE_STATUS_FAILED);
            if (status == ImageDeleteStatus::FAILED) {
                failed = true;
                item->set_error_message("IMAGE_DELETE_FAILED");
            }
        }
        if (failed) response->set_code("IMAGE_DELETE_FAILED");
        return grpc::Status::OK;
    }

    grpc::Status ReconcileImages(grpc::ServerContext*, const aivision::v1::ReconcileImagesRequest* request,
                                 aivision::v1::ReconcileImagesResponse* response) override {
        if (!request) {
            response->set_code("INVALID_ARG");
            response->set_error_message("request is required");
            return grpc::Status::OK;
        }
        bool failed = false;
        const auto results = ImageManager::instance().reconcile_images(
            std::vector<std::string>(request->retain_image_ids().begin(), request->retain_image_ids().end()));
        for (const auto& [image_id, status] : results) {
            auto* item = response->add_results();
            item->set_image_id(image_id);
            item->set_status(status == ImageDeleteStatus::DELETED
                ? aivision::v1::IMAGE_DELETE_STATUS_DELETED
                : status == ImageDeleteStatus::ALREADY_ABSENT
                    ? aivision::v1::IMAGE_DELETE_STATUS_ALREADY_ABSENT
                    : aivision::v1::IMAGE_DELETE_STATUS_FAILED);
            if (status == ImageDeleteStatus::FAILED) {
                failed = true;
                item->set_error_message("IMAGE_DELETE_FAILED");
            }
        }
        if (failed) response->set_code("IMAGE_DELETE_FAILED");
        return grpc::Status::OK;
    }

    grpc::Status QueryProfile(grpc::ServerContext*, const aivision::v1::QueryProfileRequest*,
                              aivision::v1::QueryProfileResponse* response) override {
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter) {
            response->set_code("PLATFORM_UNAVAILABLE");
            response->set_error_message("no active platform adapter");
            return grpc::Status::OK;
        }
        const auto& source = adapter->get_profile();
        auto* profile = response->mutable_profile();
        profile->set_schema_version(source.profile_version);
        profile->set_platform_id(source.platform_id);
        profile->set_adapter_version(source.profile_version);
        profile->set_arch(source.platform_id.find("arm64") != std::string::npos ? "arm64" : "unknown");
        profile->set_os_or_bsp(source.platform_id.find("macos") != std::string::npos ? "macOS" : "mock");
        profile->set_media_backend(media_backend_ ? media_backend_->name() : "unavailable");
        profile->set_inference_runtime(source.platform_id.find("coreml") != std::string::npos ? "coreml" : "none");
        profile->add_frame_caps(aivision::v1::FRAME_PIX_NV12);
        profile->add_frame_caps(aivision::v1::FRAME_PIX_BGRA);
        profile->add_frame_caps(aivision::v1::FRAME_PIX_RGB24);
        profile->set_max_cameras(static_cast<int32_t>(source.total_compute_units > source.reserved_compute_units ? 16 : 0));
        profile->set_max_instances(32);
        auto add_capability = [profile](const char* id, platform::CapabilityStatus status, const std::string& reason) {
            auto* capability = profile->add_capabilities();
            capability->set_id(id);
            switch (status) {
                case platform::CapabilityStatus::AVAILABLE:
                    capability->set_status(aivision::v1::CAPABILITY_STATUS_AVAILABLE);
                    break;
                case platform::CapabilityStatus::DEGRADED:
                    capability->set_status(aivision::v1::CAPABILITY_STATUS_DEGRADED);
                    break;
                default:
                    capability->set_status(aivision::v1::CAPABILITY_STATUS_UNSUPPORTED);
                    break;
            }
            if (status != platform::CapabilityStatus::AVAILABLE) capability->set_reason(reason);
        };
        add_capability("hardware_decode", source.hardware_decode.status, source.hardware_decode.reason);
        add_capability("image_ops", source.vector_image_ops.status, source.vector_image_ops.reason);
        add_capability("telemetry", source.telemetry_metrics.status, source.telemetry_metrics.reason);
        return grpc::Status::OK;
    }

    grpc::Status QueryMetrics(grpc::ServerContext*, const aivision::v1::QueryMetricsRequest*,
                              aivision::v1::QueryMetricsResponse* response) override {
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter) {
            response->set_code("PLATFORM_UNAVAILABLE");
            response->set_error_message("no active platform adapter");
            return grpc::Status::OK;
        }
        TelemetryCollector collector(adapter);
        if (!collector.available()) {
            response->set_code("TELEMETRY_UNAVAILABLE");
            response->set_error_message("platform telemetry is unavailable");
            return grpc::Status::OK;
        }
        const auto metrics = collector.collect();
        auto* telemetry = response->mutable_telemetry();
        telemetry->set_uptime_seconds(metrics.uptime_seconds);
        telemetry->set_cpu_usage_percent(metrics.cpu_usage_percent);
        telemetry->set_memory_usage_percent(metrics.memory_usage_percent);
        telemetry->set_disk_usage_percent(metrics.disk_usage_percent);
        telemetry->set_accelerator_usage_percent(metrics.accelerator_usage_percent);
        telemetry->set_accelerator_usage_supported(metrics.accelerator_supported);
        telemetry->set_temperature_celsius(metrics.temperature_celsius);
        telemetry->set_temperature_supported(metrics.temperature_supported);
        return grpc::Status::OK;
    }
private:
    std::shared_ptr<LoadedPackage> load_package(const std::string& algorithm_id,
                                                 const std::string& version,
                                                 std::string& error) {
        const std::string key = algorithm_id + "@" + version;
        const auto existing = loaded_packages_.find(key);
        if (existing != loaded_packages_.end()) return existing->second;
        if (!safe_package_component(algorithm_id) || !safe_package_component(version)) {
            error = "PACKAGE_ID_INVALID";
            return nullptr;
        }

        const fs::path root = package_root() / algorithm_id / version;
        const fs::path manifest_path = root / "manifest.json";
        std::ifstream manifest_input(manifest_path);
        if (!manifest_input) {
            error = "PACKAGE_NOT_FOUND";
            return nullptr;
        }
        nlohmann::json manifest;
        try {
            manifest = nlohmann::json::parse(manifest_input);
        } catch (const std::exception& exception) {
            error = std::string("PACKAGE_MANIFEST_INVALID: ") + exception.what();
            return nullptr;
        }
        std::string entry_library = manifest.value("entry_library", "");
        if (entry_library.empty()) entry_library = manifest.value("library_name", "");
        const std::string platform_id = manifest.value("platform_id", "");
        if (!safe_package_relative(entry_library) || platform_id.empty()) {
            error = "PACKAGE_MANIFEST_INVALID";
            return nullptr;
        }
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter || platform_id != adapter->get_profile().platform_id) {
            error = "PLATFORM_MISMATCH";
            return nullptr;
        }
        const fs::path library_path = root / entry_library;
        if (!fs::is_regular_file(library_path)) {
            error = "PACKAGE_LIBRARY_MISSING";
            return nullptr;
        }

        auto package = std::make_shared<LoadedPackage>();
        package->package_root = root.string();
        if (manifest.contains("resource_profile") && manifest["resource_profile"].is_object()) {
            const auto& resource_profile = manifest["resource_profile"];
            const uint32_t min_memory_mb = resource_profile.value("min_free_memory_mb", 0U);
            package->memory_bytes = static_cast<uint64_t>(min_memory_mb) * 1024 * 1024;
            const auto& tiers = resource_profile.value("fps_tiers", nlohmann::json::array());
            if (tiers.is_array()) {
                for (const auto& tier : tiers) {
                    if (!tier.is_object()) continue;
                    const int32_t fps = tier.value("fps", 0);
                    const uint32_t units = tier.value("units", 0U);
                    if (fps > 0 && units > 0) package->fps_tiers.emplace_back(fps, units);
                }
            }
            std::sort(package->fps_tiers.begin(), package->fps_tiers.end());
        }
        package->platform_id = platform_id;
        package->dynamic_library = ::dlopen(library_path.c_str(), RTLD_NOW | RTLD_LOCAL);
        if (!package->dynamic_library) {
            error = "PACKAGE_DLOPEN_FAILED";
            return nullptr;
        }
        auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(::dlsym(
            package->dynamic_library, AV_ALGO_GET_ABI_SYMBOL));
        if (!get_abi) {
            error = "PACKAGE_ABI_MISSING";
            return nullptr;
        }
        package->abi = get_abi(AV_ALGO_API_VERSION);
        if (!package->abi || package->abi->size < sizeof(av_algo_abi) ||
            package->abi->api_version != AV_ALGO_API_VERSION || !package->abi->library_open ||
            !package->abi->library_query || !package->abi->library_close || !package->abi->instance_create ||
            !package->abi->instance_negotiate || !package->abi->instance_update_config ||
            !package->abi->instance_set_rules || !package->abi->instance_process ||
            !package->abi->instance_flush || !package->abi->instance_destroy) {
            error = "PACKAGE_ABI_INCOMPATIBLE";
            return nullptr;
        }

        av_algo_library_args args{};
        args.size = sizeof(args);
        args.api_version = AV_ALGO_API_VERSION;
        args.package_root = package->package_root.c_str();
        args.platform_id = package->platform_id.c_str();
        args.platform_tag = adapter->get_profile().platform_tag;
        if (package->abi->library_open(&args, &package->library) != AV_OK || !package->library) {
            error = "PACKAGE_LIBRARY_OPEN_FAILED";
            return nullptr;
        }
        av_algo_library_info info{};
        info.size = sizeof(info);
        info.api_version = AV_ALGO_API_VERSION;
        if (package->abi->library_query(package->library, &info) != AV_OK ||
            std::string(info.algorithm_id) != algorithm_id || std::string(info.version) != version ||
            std::string(info.algorithm_type) != "object_detection") {
            error = "PACKAGE_METADATA_MISMATCH";
            return nullptr;
        }
        loaded_packages_.emplace(key, package);
        return package;
    }

    std::string make_rules(const aivision::v1::AlgorithmInstanceConfig& config,
                           std::vector<av_rule>& rules,
                           std::vector<std::vector<av_point>>& points) const {
        rules.clear();
        points.clear();
        points.reserve(config.rules_size());
        for (const auto& source : config.rules()) {
            const uint32_t role = static_cast<uint32_t>(source.role());
            const size_t minimum_points = role == AV_RULE_LINE ? 2 : 3;
            if ((role != AV_RULE_ROI && role != AV_RULE_MASK && role != AV_RULE_LINE) ||
                source.points_size() < static_cast<int>(minimum_points) || source.points_size() > 1024) {
                return "CONFIG_INVALID";
            }
            points.emplace_back();
            auto& destination_points = points.back();
            destination_points.reserve(source.points_size());
            for (const auto& point : source.points()) {
                if (point.x() < 0.0f || point.x() > 1.0f || point.y() < 0.0f || point.y() > 1.0f) {
                    return "CONFIG_INVALID";
                }
                destination_points.push_back({point.x(), point.y()});
            }
            av_rule rule{};
            rule.size = sizeof(av_rule);
            rule.api_version = AV_ALGO_API_VERSION;
            rule.role = role;
            rule.mode = role == AV_RULE_LINE ? static_cast<uint32_t>(source.line_direction()) : 0;
            rule.point_count = static_cast<uint32_t>(destination_points.size());
            rule.points = destination_points.data();
            rules.push_back(rule);
        }
        return {};
    }

    void remove_instance(const std::string& instance_id, bool forget_config = true) {
        const auto instance = AlgoManager::instance().get(instance_id);
        if (!instance) {
            instance_resources_.erase(instance_id);
            if (forget_config) instance_configs_.erase(instance_id);
            return;
        }
        const auto task = TaskScheduler::instance().get_task(instance->get_camera_id());
        AlgoManager::instance().remove(instance_id);
        if (task) task->remove_instance(instance_id);
        ResourceLedger::instance().release(instance_id);
        instance_resources_.erase(instance_id);
        if (forget_config) instance_configs_.erase(instance_id);
    }

    std::string reconcile_instance(const aivision::v1::AlgorithmInstanceConfig& config) {
        if (config.instance_id().empty() || config.camera_id().empty() || config.algorithm_id().empty() ||
            config.algorithm_version().empty()) return "CONFIG_INVALID";
        const auto task = TaskScheduler::instance().get_task(config.camera_id());
        const auto existing = AlgoManager::instance().get(config.instance_id());
        if (!config.enabled()) {
            remove_instance(config.instance_id());
            return {};
        }
        if (!task) return "TASK_NOT_FOUND";
        if (existing && (existing->get_algorithm_id() != config.algorithm_id() ||
                         existing->get_version() != config.algorithm_version())) {
            remove_instance(config.instance_id());
        }
        const auto current = AlgoManager::instance().get(config.instance_id());
        if (current) {
            std::vector<av_rule> rules;
            std::vector<std::vector<av_point>> points;
            const std::string rule_error = make_rules(config, rules, points);
            if (!rule_error.empty()) return "CONFIG_INVALID";
            if (!config.params_json().empty()) {
                const av_status status = current->update_params(config.params_json());
                if (status != AV_OK) return "CONFIG_UPDATE_FAILED";
            }
            if (current->set_rules(rules) != AV_OK) return "CONFIG_INVALID";
            instance_configs_[config.instance_id()] = config;
            return {};
        }

        std::string package_error;
        const auto package = load_package(config.algorithm_id(), config.algorithm_version(), package_error);
        if (!package) return package_error;
        const int32_t target_fps = config.analysis_fps() > 0 ? config.analysis_fps() : 25;
        uint32_t compute_units = 0;
        bool matched_fps_tier = false;
        for (const auto& [tier_fps, tier_units] : package->fps_tiers) {
            if (tier_fps >= target_fps) {
                compute_units = tier_units;
                matched_fps_tier = true;
                break;
            }
        }
        if (!matched_fps_tier) return "RESOURCE_LIMIT_EXCEEDED";
        ResourceRequirement requirement{
            .instance_id = config.instance_id(),
            .algorithm_id = config.algorithm_id(),
            .target_fps = target_fps,
            .compute_units = compute_units,
            .memory_bytes = package->memory_bytes
        };
        std::string resource_reason;
        if (ResourceLedger::instance().allocate(requirement, &resource_reason) != AV_OK) {
            return "RESOURCE_LIMIT_EXCEEDED";
        }
        auto instance = std::make_shared<AlgorithmInstance>(
            config.instance_id(), config.camera_id(), config.algorithm_id(), config.algorithm_version(),
            target_fps, config.params_json().empty() ? "{}" : config.params_json(), package->abi, package->library);
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter || instance->init(FramePool::instance().get_frame_ops(), adapter->get_c_image_ops()) != AV_OK) {
            ResourceLedger::instance().release(config.instance_id());
            return "INSTANCE_CREATE_FAILED";
        }
        std::vector<av_rule> rules;
        std::vector<std::vector<av_point>> points;
        const std::string rule_error = make_rules(config, rules, points);
        if (!rule_error.empty() || instance->set_rules(rules) != AV_OK) {
            instance->stop();
            ResourceLedger::instance().release(config.instance_id());
            return "CONFIG_INVALID";
        }
        const std::weak_ptr<AlgorithmInstance> weak_instance = instance;
        instance->set_result_callback([this, weak_instance](const av_algo_result& result, const av_frame_desc& frame) {
            if (const auto locked = weak_instance.lock()) handle_result(locked, result, frame);
        });
        if (!AlgoManager::instance().add(instance)) {
            instance->stop();
            ResourceLedger::instance().release(config.instance_id());
            return "INSTANCE_ALREADY_EXISTS";
        }
        task->add_instance(instance);
        instance_resources_[config.instance_id()] = requirement;
        instance_configs_[config.instance_id()] = config;
        return {};
    }

    void handle_result(const std::shared_ptr<AlgorithmInstance>& instance,
                       const av_algo_result& result, const av_frame_desc& frame) {
        if (result.kind != AV_RESULT_ALARM || result.size < sizeof(av_algo_result) ||
            !result.json || result.json_len == 0 || result.json_len > AV_MAX_RESULT_JSON_BYTES ||
            result.image_count > 4096 || (result.image_count > 0 && !result.images)) return;
        try {
            const auto value = nlohmann::json::parse(result.json, result.json + result.json_len);
            if (!value.is_object() || !value.contains("event_id") || !value["event_id"].is_string() ||
                !value.contains("alarm_type_id") || !value["alarm_type_id"].is_string() ||
                !value.contains("objects") || !value["objects"].is_array() || value["objects"].size() > 4096) return;

            const std::string algorithm_event_id = value["event_id"].get<std::string>();
            const std::string alarm_type_id = value["alarm_type_id"].get<std::string>();
            const auto is_safe_component = [](const std::string& component) {
                if (component.empty() || component.size() > 128) return false;
                for (const unsigned char ch : component) {
                    if (!(std::isalnum(ch) || ch == '.' || ch == '_' || ch == '-')) return false;
                }
                return true;
            };
            if (!is_safe_component(algorithm_event_id) || !is_safe_component(alarm_type_id) ||
                (!instance->get_alarm_type_id().empty() && alarm_type_id != instance->get_alarm_type_id())) return;

            aivision::v1::AlarmEvent alarm;
            alarm.set_instance_id(instance->get_instance_id());
            alarm.set_camera_id(instance->get_camera_id());
            alarm.set_algorithm_id(instance->get_algorithm_id());
            alarm.set_algorithm_version(instance->get_version());
            alarm.set_alarm_type_id(alarm_type_id);
            alarm.set_wall_time_ns(frame.wall_time_ns);
            alarm.set_time_synced(frame.time_synced != 0);
            for (const auto& object : value["objects"]) {
                if (!object.is_object() || !object.contains("label") || !object["label"].is_string() ||
                    !object.contains("confidence") || !object["confidence"].is_number() ||
                    !object.contains("bbox") || !object["bbox"].is_array() || object["bbox"].size() != 4) return;
                const double confidence = object["confidence"].get<double>();
                if (!std::isfinite(confidence) || confidence < 0.0 || confidence > 1.0) return;
                std::array<double, 4> bbox{};
                for (size_t index = 0; index < bbox.size(); ++index) {
                    if (!object["bbox"][index].is_number()) return;
                    bbox[index] = object["bbox"][index].get<double>();
                    if (!std::isfinite(bbox[index]) || bbox[index] < 0.0 || bbox[index] > 1.0) return;
                }
                if (bbox[0] + bbox[2] > 1.0 || bbox[1] + bbox[3] > 1.0) return;
                int64_t track_id = 0;
                if (object.contains("track_id")) {
                    if (!object["track_id"].is_number_integer()) return;
                    track_id = object["track_id"].get<int64_t>();
                }
                auto* destination = alarm.add_objects();
                destination->set_label(object["label"].get<std::string>());
                destination->set_confidence(static_cast<float>(confidence));
                destination->mutable_bbox()->set_x_min(static_cast<float>(bbox[0]));
                destination->mutable_bbox()->set_y_min(static_cast<float>(bbox[1]));
                destination->mutable_bbox()->set_x_max(static_cast<float>(bbox[0] + bbox[2]));
                destination->mutable_bbox()->set_y_max(static_cast<float>(bbox[1] + bbox[3]));
                destination->set_track_id(track_id);
            }

            const std::string event_id = instance->get_run_id() + "/" + algorithm_event_id;
            {
                std::lock_guard<std::mutex> lock(result_mutex_);
                if (!reported_events_.insert(event_id).second) return;
            }
            alarm.set_event_id(event_id);
            if (result.image_count > 0) {
                const auto& request = result.images[0];
                if (request.size < sizeof(av_algo_image_req) || request.api_version != AV_ALGO_API_VERSION ||
                    !std::isfinite(request.x) || !std::isfinite(request.y) ||
                    !std::isfinite(request.w) || !std::isfinite(request.h) || request.x < 0.0f ||
                    request.y < 0.0f || request.w <= 0.0f || request.h <= 0.0f ||
                    request.x + request.w > 1.0f || request.y + request.h > 1.0f) return;
                av_rect roi{};
                roi.size = sizeof(av_rect);
                roi.api_version = AV_ALGO_API_VERSION;
                roi.x = request.x;
                roi.y = request.y;
                roi.width = request.w;
                roi.height = request.h;
                ImageRecord record;
                if (ImageManager::instance().save_detection_image(&frame, &roi, event_id, &record) == AV_OK) {
                    alarm.set_image_id(record.image_id);
                    alarm.set_image_rel_path(record.rel_path);
                }
            }
            if (app_client_ && app_client_->report_alarm(alarm) && !alarm.image_id().empty()) {
                ImageManager::instance().mark_reported(alarm.image_id());
            }
        } catch (...) {
            // Malformed plugin output is isolated to this result callback.
        }
    }

    std::string restart_package_instances(const std::string& algorithm_id, const std::string& version) {
        std::vector<aivision::v1::AlgorithmInstanceConfig> replacements;
        for (auto& [instance_id, config] : instance_configs_) {
            if (config.algorithm_id() != algorithm_id || config.algorithm_version() == version) continue;
            config.set_algorithm_version(version);
            replacements.push_back(config);
        }
        for (const auto& config : replacements) remove_instance(config.instance_id(), false);
        for (const auto& config : replacements) {
            const std::string error = reconcile_instance(config);
            if (!error.empty()) return error;
        }
        return {};
    }

    template <typename Response>
    void install_package(const std::string& package_path, const char* operation, Response* response) {
        std::lock_guard<std::mutex> lifecycle_lock(reconcile_mutex_);
        const std::string validator_bin = env_or_default("AIVISION_PACKAGE_VALIDATOR_PATH",
            env_or_default("AIVISION_PACKAGE_VALIDATOR", "package_validator").c_str());
        const auto validation = PackageValidator::run_sandbox_validator(
            validator_bin,
            package_path, package_root().string());
        if (!validation.success) {
            std::string code;
            std::string message;
            set_package_validation_failure(validation, operation, &code, &message);
            response->set_code(code);
            response->set_error_message(message);
            return;
        }
        if (!safe_package_component(validation.manifest.algorithm_id) ||
            !safe_package_component(validation.manifest.version)) {
            response->set_code("PACKAGE_ID_INVALID");
            response->set_error_message("validator returned an unsafe package identity");
            return;
        }
        std::string activation_error;
        if (!write_active_version(validation.manifest.algorithm_id, validation.manifest.version, activation_error)) {
            response->set_code("PACKAGE_ACTIVATION_FAILED");
            response->set_error_message(activation_error);
            return;
        }
        response->set_algorithm_id(validation.manifest.algorithm_id);
        response->set_version(validation.manifest.version);
        if (std::string(operation) == "UpgradePackage") {
            const std::string restart_error = restart_package_instances(
                validation.manifest.algorithm_id, validation.manifest.version);
            if (!restart_error.empty()) {
                response->set_code("PACKAGE_RESTART_FAILED");
                response->set_error_message(restart_error);
            }
        }
    }

    std::string upsert_task(const aivision::v1::CameraTaskConfig& config) {
        if (config.camera_id().empty() || config.rtsp_url().empty()) return "INVALID_ARG";
        if (!config.enabled()) {
            TaskScheduler::instance().stop_task(config.camera_id());
            return {};
        }
        if (!platform_adapter_ || !media_backend_) return "PLATFORM_UNAVAILABLE";

        TaskScheduler::instance().stop_task(config.camera_id());
        auto task = std::make_shared<CameraTask>(
            config.camera_id(), config.rtsp_url(), platform_adapter_, media_backend_);
        if (!TaskScheduler::instance().add_task(task)) return "TASK_ALREADY_EXISTS";
        if (task->start() != AV_OK) {
            TaskScheduler::instance().stop_task(config.camera_id());
            return "MEDIA_START_FAILED";
        }
        return {};
    }

    std::shared_ptr<platform::IPlatformAdapter> platform_adapter_;
    std::shared_ptr<media::IMediaBackend> media_backend_;
    std::shared_ptr<UdsClient> app_client_;
    std::unordered_map<std::string, std::shared_ptr<LoadedPackage>> loaded_packages_;
    std::unordered_map<std::string, ResourceRequirement> instance_resources_;
    std::unordered_map<std::string, aivision::v1::AlgorithmInstanceConfig> instance_configs_;
    std::mutex result_mutex_;
    std::unordered_set<std::string> reported_events_;
    std::mutex reconcile_mutex_;
    std::atomic<uint64_t> applied_revision_{0};
};

class PersonServiceImpl final : public aivision::v1::PersonService::Service {
public:
    grpc::Status SyncPersons(grpc::ServerContext*, const aivision::v1::SyncPersonsRequest*,
                             aivision::v1::SyncPersonsResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED,
                            "Person synchronization is not implemented in this phase");
    }
};

} // namespace

UdsServer::UdsServer(const std::string& sock_path,
                     std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
                     std::shared_ptr<media::IMediaBackend> media_backend)
    : sock_path_(sock_path),
      platform_adapter_(std::move(platform_adapter)),
      media_backend_(std::move(media_backend)) {}

UdsServer::~UdsServer() {
    stop();
}

bool UdsServer::apply_desired_state(const aivision::v1::DesiredState& desired_state,
                                     aivision::v1::ApplyDesiredStateResponse* response) {
    if (!response || !engine_service_) return false;
    auto* service = dynamic_cast<EngineServiceImpl*>(engine_service_.get());
    if (!service) return false;
    service->apply_desired_state(&desired_state, response);
    return true;
}

bool UdsServer::start() {
    if (server_ || !prepare_socket_path(sock_path_)) return false;

    grpc::ServerBuilder builder;
    builder.AddListeningPort("unix://" + sock_path_, grpc::InsecureServerCredentials());
    engine_service_ = std::make_unique<EngineServiceImpl>(platform_adapter_, media_backend_);
    person_service_ = std::make_unique<PersonServiceImpl>();
    builder.RegisterService(engine_service_.get());
    builder.RegisterService(person_service_.get());
    server_ = builder.BuildAndStart();
    if (!server_) {
        engine_service_.reset();
        person_service_.reset();
        return false;
    }
    owns_socket_ = true;
    return true;
}

void UdsServer::stop() {
    if (server_) {
        server_->Shutdown();
        server_->Wait();
        server_.reset();
    }
    person_service_.reset();
    engine_service_.reset();
    if (owns_socket_) {
        ::unlink(sock_path_.c_str());
        owns_socket_ = false;
    }
}

} // namespace aivision::core
