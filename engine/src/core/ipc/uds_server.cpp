/**
 * @file uds_server.cpp
 * @brief UDS gRPC 服务端实现（处理来自 Go 控制面的 RPC 期望与管理指令）
 * 
 * 核心功能：
 * 1. EngineServiceImpl：处理 ApplyDesiredState、算法包安装/升级/回滚/卸载、图片删除与对账、Profile/Metrics 查询；
 * 2. PersonServiceImpl：处理人脸库与特征检索相关契约；
 * 3. 算法库动态装载管理（LibraryContext）与 C ABI 句柄持有。
 */

#include "argus/core/uds_ipc.hpp"
#include "argus/core/image_manager.hpp"
#include "argus/core/frame_pool.hpp"
#include "argus/core/algo_manager.hpp"
#include "argus/core/algo_sandbox.hpp"
#include "argus/core/live_stream_manager.hpp"
#include "argus/core/logging/log_adapter.hpp"
#include "argus/core/probe_rtsp.hpp"
#include "argus/core/resource_ledger.hpp"
#include "argus/core/task_scheduler.hpp"
#include "argus/core/telemetry_collector.hpp"
#include "argus/platform/platform_api.hpp"
#include "argus/utils/package_layout.hpp"


#include <cerrno>
#include <atomic>
#include <algorithm>
#include <array>
#include <condition_variable>
#include <cstring>
#include <cctype>
#include <chrono>
#include <cmath>
#include <cstddef>
#include <nlohmann/json.hpp>
#include <deque>
#include <filesystem>
#include <dlfcn.h>
#include <fstream>
#include <initializer_list>
#include <cstdlib>
#include <string_view>
#include <limits>
#include <mutex>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>
#include <vector>
#include <utility>
#include <unordered_map>
#include <unordered_set>

namespace fs = std::filesystem;

namespace argus::core {
namespace {

std::string env_or_default(const char* name, const char* fallback) {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}

bool safe_package_component(const std::string& value) {
    if (value.empty() || value == "." || value == ".." || value.size() > 128) return false;
    for (const unsigned char ch : value) {
        if (!(std::isalnum(ch) || ch == '-' || ch == '_' || ch == '.')) return false;
    }
    return true;
}

fs::path package_root() {
    return fs::path(env_or_default("ARGUS_PACKAGE_DIR", "var/packages"));
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

bool read_active_version(const std::string& algorithm_id, std::string& version, bool& present, std::string& error) {
    version.clear();
    present = false;
    error.clear();
    const fs::path marker_path = package_root() / "active" / (algorithm_id + ".version");
    std::error_code ec;
    const auto status = fs::symlink_status(marker_path, ec);
    if (ec == std::errc::no_such_file_or_directory) return true;
    if (ec) {
        error = "cannot inspect active package marker: " + ec.message();
        return false;
    }
    if (!fs::exists(status)) return true;
    if (fs::is_symlink(status) || !fs::is_regular_file(status)) {
        error = "active package marker is not a regular file";
        return false;
    }
    std::ifstream input(marker_path);
    if (!input || !std::getline(input, version)) {
        error = "active package marker is unreadable";
        return false;
    }
    while (!version.empty() && (version.back() == '\r' || version.back() == '\n' ||
                                version.back() == ' ' || version.back() == '\t')) {
        version.pop_back();
    }
    size_t start = 0;
    while (start < version.size() && (version[start] == ' ' || version[start] == '\t')) {
        ++start;
    }
    if (start > 0) {
        version = version.substr(start);
    }
    if (version.empty()) {
        error = "active package marker is unreadable";
        return false;
    }
    present = true;
    return true;
}

bool restore_active_version(const std::string& algorithm_id, const std::string& version,
                            bool present, std::string& error) {
    if (present) return write_active_version(algorithm_id, version, error);
    const fs::path marker_path = package_root() / "active" / (algorithm_id + ".version");
    std::error_code ec;
    fs::remove(marker_path, ec);
    if (ec) {
        error = "cannot remove active package marker: " + ec.message();
        return false;
    }
    return true;
}

bool snapshot_active_versions(std::unordered_map<std::string, std::string>& versions, std::string& error) {
    versions.clear();
    const fs::path active_dir = package_root() / "active";
    std::error_code ec;
    const auto status = fs::symlink_status(active_dir, ec);
    if (ec == std::errc::no_such_file_or_directory) return true;
    if (ec) {
        error = "cannot inspect active package directory: " + ec.message();
        return false;
    }
    if (!fs::exists(status)) return true;
    if (fs::is_symlink(status) || !fs::is_directory(status)) {
        error = "active package directory is not a regular directory";
        return false;
    }
    fs::directory_iterator it(active_dir, ec);
    if (ec) {
        error = "cannot inspect active package directory: " + ec.message();
        return false;
    }
    const fs::directory_iterator end;
    for (; it != end; it.increment(ec)) {
        if (ec) {
            error = "cannot inspect active package directory: " + ec.message();
            return false;
        }
        const fs::path marker = it->path();
        if (marker.extension() != ".version") continue;
        const auto marker_status = it->symlink_status(ec);
        if (ec || fs::is_symlink(marker_status) || !fs::is_regular_file(marker_status)) {
            error = "active package marker is not a regular file";
            return false;
        }
        const std::string algorithm_id = marker.stem().string();
        if (!safe_package_component(algorithm_id)) {
            error = "active package marker has an unsafe name";
            return false;
        }
        std::string version;
        bool present = false;
        if (!read_active_version(algorithm_id, version, present, error)) return false;
        if (present) versions.emplace(algorithm_id, std::move(version));
    }
    return true;
}

bool restore_active_versions(const std::unordered_map<std::string, std::string>& versions,
                             std::string& error) {
    const fs::path active_dir = package_root() / "active";
    std::error_code ec;
    fs::create_directories(active_dir, ec);
    if (ec) {
        error = "cannot create active package directory: " + ec.message();
        return false;
    }
    fs::directory_iterator it(active_dir, ec);
    if (ec) {
        error = "cannot inspect active package directory: " + ec.message();
        return false;
    }
    const fs::directory_iterator end;
    for (; it != end; it.increment(ec)) {
        if (ec) {
            error = "cannot inspect active package directory: " + ec.message();
            return false;
        }
        if (it->path().extension() == ".version") {
            fs::remove(it->path(), ec);
            if (ec) {
                error = "cannot remove active package marker: " + ec.message();
                return false;
            }
        }
    }
    for (const auto& [algorithm_id, version] : versions) {
        if (!write_active_version(algorithm_id, version, error)) return false;
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
    std::string algorithm_type;
    argus::logging::AlgoLogContext log_context;
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
    // 探测期间路径可能被替换；只在 inode/device 仍与首次 lstat 一致时清理。
    struct stat current{};
    if (::lstat(path.c_str(), &current) != 0) {
        return errno == ENOENT;
    }
    if (current.st_dev != st.st_dev || current.st_ino != st.st_ino) return false;
    return ::unlink(path.c_str()) == 0 || errno == ENOENT;
}

class EngineServiceImpl final : public argus::v1::EngineService::Service {
public:
    EngineServiceImpl(std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
                      std::shared_ptr<media::IMediaBackend> media_backend,
                      const std::string& app_socket_path)
        : platform_adapter_(std::move(platform_adapter)),
          media_backend_(std::move(media_backend)),
          app_client_(std::make_shared<UdsClient>(
              app_socket_path.empty() ? env_or_default("ARGUS_APP_SOCKET", "/tmp/argus-app.sock")
                                      : app_socket_path)) {
        // 告警抓拍/上报 worker 与算法实例线程分离；回调线程只负责校验和非阻塞入队。
        capture_worker_thread_ = std::thread(&EngineServiceImpl::capture_worker_loop, this);
    }

    ~EngineServiceImpl() override {
        // 先阻止新的结果进入队列，再停止实例，保证正在执行的 ABI 回调仍能安全返回。
        capture_accepting_.store(false, std::memory_order_release);
        TaskScheduler::instance().stop_all();
        AlgoManager::instance().stop_all();
        stop_capture_worker();
        loaded_packages_.clear();
    }

    grpc::Status ApplyDesiredState(grpc::ServerContext*, const argus::v1::ApplyDesiredStateRequest* request,
                                   argus::v1::ApplyDesiredStateResponse* response) override {
        if (!request || !request->has_desired_state()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("desired_state is required");
            return grpc::Status::OK;
        }
        return apply_desired_state(&request->desired_state(), response);
    }

    // 执行期望状态应用（Desired State Reconcile 状态机）：
    // 1. 检查版本号（revision）防止旧版本乱序覆盖；
    // 2. 创建当前运行时快照（RuntimeSnapshot）用于失败回滚；
    // 3. 逐项应用任务（Task）与算法实例（Instance）配置；
    // 4. 若出现失败，执行事务性回滚（rollback_runtime_state），回滚失败则标记降级（RUNTIME_DEGRADED）；
    // 5. 成功后原子更新应用版本号与期望状态缓存。
    grpc::Status apply_desired_state(const argus::v1::DesiredState* desired_ptr,
                                     argus::v1::ApplyDesiredStateResponse* response) {
        if (!desired_ptr || !response) return grpc::Status::OK;
        const auto& desired = *desired_ptr;
        std::lock_guard<std::mutex> reconcile_lock(reconcile_mutex_);
        const uint64_t current_revision = applied_revision_.load(std::memory_order_acquire);
        const std::string desired_serialized = desired.SerializeAsString();
        if (desired.revision() < current_revision ||
            (desired.revision() == current_revision && desired_serialized != applied_desired_state_serialized_)) {
            response->set_applied_revision(current_revision);
            response->set_code("STALE_REVISION");
            response->set_error_message("desired state revision is stale or conflicts with the applied state");
            return grpc::Status::OK;
        }
        if (desired.revision() == current_revision && desired_serialized == applied_desired_state_serialized_) {
            response->set_applied_revision(current_revision);
            if (runtime_degraded_) {
                RuntimeSnapshot degraded_snapshot;
                std::string degraded_snapshot_error;
                if (!snapshot_runtime_state(degraded_snapshot, degraded_snapshot_error)) {
                    degraded_snapshot.task_configs = task_configs_;
                    degraded_snapshot.instance_configs = instance_configs_;
                    degraded_snapshot.loaded_packages = loaded_packages_;
                }
                report_runtime_degraded(
                    degraded_snapshot, desired,
                    "runtime remains degraded after desired-state rollback failure", response);
                response->set_code("RUNTIME_DEGRADED");
                response->set_error_message("desired state is unchanged, but the runtime remains degraded");
            }
            return grpc::Status::OK;
        }

        // 保存对齐前的运行时快照
        RuntimeSnapshot snapshot;
        std::string snapshot_error;
        if (!snapshot_runtime_state(snapshot, snapshot_error)) {
            response->set_applied_revision(current_revision);
            response->set_code("RECONCILE_SNAPSHOT_FAILED");
            response->set_error_message(snapshot_error);
            return grpc::Status::OK;
        }

        bool failed = false;
        // 增量更新或新建摄像头任务
        for (const auto& task_config : desired.tasks()) {
            auto* item = response->add_results();
            item->set_kind(argus::v1::RECONCILE_ITEM_KIND_TASK);
            item->set_id(task_config.camera_id());
            const std::string error = upsert_task(task_config);
            if (error.empty()) {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_OK);
            } else {
                failed = true;
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code(error);
                item->set_error_message(error);
            }
        }
        std::unordered_set<std::string> desired_task_ids;
        for (const auto& task_config : desired.tasks()) desired_task_ids.insert(task_config.camera_id());
        for (const auto& camera_id : TaskScheduler::instance().task_ids()) {
            if (desired_task_ids.find(camera_id) != desired_task_ids.end()) continue;
            TaskScheduler::instance().stop_task(camera_id);
            task_configs_.erase(camera_id);
            auto* item = response->add_results();
            item->set_kind(argus::v1::RECONCILE_ITEM_KIND_TASK);
            item->set_id(camera_id);
            item->set_status(argus::v1::RECONCILE_ITEM_STATUS_OK);
        }

        for (const auto& instance : desired.instances()) {
            auto* item = response->add_results();
            item->set_kind(argus::v1::RECONCILE_ITEM_KIND_INSTANCE);
            item->set_id(instance.instance_id());
            const std::string error = reconcile_instance(instance);
            if (error.empty()) {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_OK);
            } else {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
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
            item->set_kind(argus::v1::RECONCILE_ITEM_KIND_PACKAGE);
            item->set_id(package.algorithm_id());
            if (!safe_package_component(package.algorithm_id()) || !safe_package_component(package.version())) {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
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
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code("PACKAGE_NOT_FOUND");
                item->set_error_message("package version is not installed");
                failed = true;
            } else if (!write_active_version(package.algorithm_id(), package.version(), activation_error)) {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
                item->set_code("PACKAGE_ACTIVATION_FAILED");
                item->set_error_message(activation_error);
                failed = true;
            } else {
                item->set_status(argus::v1::RECONCILE_ITEM_STATUS_OK);
            }
        }
        if (failed) {
            std::string rollback_error;
            const bool rolled_back = restore_runtime_state(snapshot, rollback_error);
            for (int index = 0; index < response->results_size(); ++index) {
                auto* item = response->mutable_results(index);
                if (item->status() == argus::v1::RECONCILE_ITEM_STATUS_OK) {
                    item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
                    item->set_code("RECONCILE_ROLLED_BACK");
                    item->set_error_message("item was rolled back after another desired-state item failed");
                }
            }
            // R10a：失败实例经 ReportInstanceState 异步回流 ERROR，避免 Go 侧实例永久停在 STARTING。
            // 被回滚牵连的原 OK 实例同样上报 ERROR 属正常瞬态——rollback 已恢复其运行，
            // 控制面上报循环会在下一轮（≤2s）以 RUNNING/STOPPED 覆盖。上报失败记入
            // restart_failures_（对齐 mark_package_degraded 模式），下一轮上报会重试。
            for (int index = 0; index < response->results_size(); ++index) {
                const auto& item = response->results(index);
                if (item.kind() != argus::v1::RECONCILE_ITEM_KIND_INSTANCE ||
                    item.status() != argus::v1::RECONCILE_ITEM_STATUS_FAILED) {
                    continue;
                }
                argus::v1::InstanceState state;
                state.set_instance_id(item.id());
                state.set_status(argus::v1::INSTANCE_STATUS_ERROR);
                state.set_message(item.code() + ": " + item.error_message());
                if (app_client_ && !app_client_->report_instance_state(state)) {
                    auto& report_error = restart_failures_[item.id()];
                    if (!report_error.empty()) report_error += "; ";
                    report_error += "ERROR_STATE_REPORT_FAILED";
                }
            }
            response->set_applied_revision(current_revision);
            if (rolled_back) {
                response->set_code("RECONCILE_FAILED");
                response->set_error_message("one or more desired-state items failed; previous state was restored");
            } else {
                report_runtime_degraded(
                    snapshot, desired,
                    "desired-state application failed and previous runtime state could not be restored", response);
                response->set_code("RECONCILE_ROLLBACK_FAILED");
                response->set_error_message("desired-state application failed and previous runtime state could not be restored: " +
                                            rollback_error);
            }
            return grpc::Status::OK;
        }
        applied_revision_.store(desired.revision(), std::memory_order_release);
        applied_desired_state_ = desired;
        applied_desired_state_serialized_ = desired_serialized;
        runtime_degraded_ = false;
        degraded_instance_ids_.clear();
        response->set_applied_revision(desired.revision());
        return grpc::Status::OK;
    }

    grpc::Status UpsertTask(grpc::ServerContext*, const argus::v1::UpsertTaskRequest* request,
                            argus::v1::UpsertTaskResponse* response) override {
        std::lock_guard<std::mutex> reconcile_lock(reconcile_mutex_);
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

    grpc::Status SetInstanceState(grpc::ServerContext*, const argus::v1::SetInstanceStateRequest* request,
                                   argus::v1::SetInstanceStateResponse* response) override {
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

    grpc::Status UpdateInstanceConfig(grpc::ServerContext*, const argus::v1::UpdateInstanceConfigRequest* request,
                                      argus::v1::UpdateInstanceConfigResponse* response) override {
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

    grpc::Status InstallPackage(grpc::ServerContext*, const argus::v1::InstallPackageRequest* request,
                                argus::v1::InstallPackageResponse* response) override {
        if (!request || request->package_path().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("package_path is required");
            return grpc::Status::OK;
        }
        install_package(request->package_path(), "InstallPackage", response);
        return grpc::Status::OK;
    }

    grpc::Status UpgradePackage(grpc::ServerContext*, const argus::v1::UpgradePackageRequest* request,
                                argus::v1::UpgradePackageResponse* response) override {
        if (!request || request->package_path().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("package_path is required");
            return grpc::Status::OK;
        }
        install_package(request->package_path(), "UpgradePackage", response);
        return grpc::Status::OK;
    }

    grpc::Status RollbackPackage(grpc::ServerContext*, const argus::v1::RollbackPackageRequest* request,
                                 argus::v1::RollbackPackageResponse* response) override {
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
        std::string previous_active_version;
        bool had_previous_active_version = false;
        std::string active_read_error;
        if (!read_active_version(request->algorithm_id(), previous_active_version,
                                 had_previous_active_version, active_read_error)) {
            response->set_code("PACKAGE_ACTIVATION_FAILED");
            response->set_error_message(active_read_error);
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
                std::string restore_error;
                const bool restored = restore_active_version(
                    request->algorithm_id(), previous_active_version, had_previous_active_version, restore_error);
                if (!restored) {
                    mark_package_degraded_for_algorithm(
                        request->algorithm_id(), "active package marker restore failed: " + restore_error);
                }
                if (!restored || restart_error == "PACKAGE_ROLLBACK_FAILED") {
                    response->set_code("PACKAGE_ROLLBACK_FAILED");
                    response->set_error_message(restart_error +
                                                (restored ? "" : "; active marker restore failed: " + restore_error));
                } else {
                    response->set_code("PACKAGE_RESTART_FAILED");
                    response->set_error_message(restart_error);
                }
            }
        }
        return grpc::Status::OK;
    }

    grpc::Status UninstallPackage(grpc::ServerContext*, const argus::v1::UninstallPackageRequest* request,
                                  argus::v1::UninstallPackageResponse* response) override {
        std::lock_guard<std::mutex> lifecycle_lock(reconcile_mutex_);
        if (!request || !safe_package_component(request->algorithm_id()) ||
            !safe_package_component(request->version())) {
            response->set_code("INVALID_ARG");
            response->set_error_message("algorithm_id and version are required and must be safe");
            return grpc::Status::OK;
        }
        if (AlgoManager::instance().has_package_reference(request->algorithm_id(), request->version()) ||
            has_desired_instance_reference(request->algorithm_id())) {
            response->set_code("PACKAGE_IN_USE");
            response->set_error_message("package version is referenced by an active desired instance or a running instance");
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

    grpc::Status DeleteImages(grpc::ServerContext*, const argus::v1::DeleteImagesRequest* request,
                              argus::v1::DeleteImagesResponse* response) override {
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
                ? argus::v1::IMAGE_DELETE_STATUS_DELETED
                : status == ImageDeleteStatus::ALREADY_ABSENT
                    ? argus::v1::IMAGE_DELETE_STATUS_ALREADY_ABSENT
                    : argus::v1::IMAGE_DELETE_STATUS_FAILED);
            if (status == ImageDeleteStatus::FAILED) {
                failed = true;
                item->set_error_message("IMAGE_DELETE_FAILED");
            }
        }
        if (failed) response->set_code("IMAGE_DELETE_FAILED");
        return grpc::Status::OK;
    }

    grpc::Status ReconcileImages(grpc::ServerContext*, const argus::v1::ReconcileImagesRequest* request,
                                 argus::v1::ReconcileImagesResponse* response) override {
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
                ? argus::v1::IMAGE_DELETE_STATUS_DELETED
                : status == ImageDeleteStatus::ALREADY_ABSENT
                    ? argus::v1::IMAGE_DELETE_STATUS_ALREADY_ABSENT
                    : argus::v1::IMAGE_DELETE_STATUS_FAILED);
            if (status == ImageDeleteStatus::FAILED) {
                failed = true;
                item->set_error_message("IMAGE_DELETE_FAILED");
            }
        }
        if (failed) response->set_code("IMAGE_DELETE_FAILED");
        return grpc::Status::OK;
    }

    grpc::Status QueryProfile(grpc::ServerContext*, const argus::v1::QueryProfileRequest*,
                              argus::v1::QueryProfileResponse* response) override {
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
        profile->add_frame_caps(argus::v1::FRAME_PIX_NV12);
        profile->add_frame_caps(argus::v1::FRAME_PIX_BGRA);
        profile->add_frame_caps(argus::v1::FRAME_PIX_RGB24);
        profile->set_max_cameras(static_cast<int32_t>(source.total_compute_units > source.reserved_compute_units ? 16 : 0));
        profile->set_max_instances(32);
        profile->set_total_compute_units(static_cast<int32_t>(source.total_compute_units));
        profile->set_reserved_compute_units(static_cast<int32_t>(source.reserved_compute_units));
        auto add_capability = [profile](const char* id, platform::CapabilityStatus status, const std::string& reason) {
            auto* capability = profile->add_capabilities();
            capability->set_id(id);
            switch (status) {
                case platform::CapabilityStatus::AVAILABLE:
                    capability->set_status(argus::v1::CAPABILITY_STATUS_AVAILABLE);
                    break;
                case platform::CapabilityStatus::DEGRADED:
                    capability->set_status(argus::v1::CAPABILITY_STATUS_DEGRADED);
                    break;
                default:
                    capability->set_status(argus::v1::CAPABILITY_STATUS_UNSUPPORTED);
                    break;
            }
            if (status != platform::CapabilityStatus::AVAILABLE) capability->set_reason(reason);
        };
        add_capability("hardware_decode", source.hardware_decode.status, source.hardware_decode.reason);
        add_capability("image_ops", source.vector_image_ops.status, source.vector_image_ops.reason);
        add_capability("telemetry", source.telemetry_metrics.status, source.telemetry_metrics.reason);
        return grpc::Status::OK;
    }

    grpc::Status QueryMetrics(grpc::ServerContext*, const argus::v1::QueryMetricsRequest*,
                              argus::v1::QueryMetricsResponse* response) override {
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
        const float unsupported_metric = std::numeric_limits<float>::quiet_NaN();
        telemetry->set_accelerator_usage_percent(
            metrics.accelerator_supported ? metrics.accelerator_usage_percent : unsupported_metric);
        telemetry->set_accelerator_usage_supported(metrics.accelerator_supported);
        telemetry->set_temperature_celsius(
            metrics.temperature_supported ? metrics.temperature_celsius : unsupported_metric);
        telemetry->set_temperature_supported(metrics.temperature_supported);
        return grpc::Status::OK;
    }

    grpc::Status ProbeCamera(grpc::ServerContext* context,
                             const argus::v1::ProbeCameraRequest* request,
                             argus::v1::ProbeCameraResponse* response) override {
        if (!request || request->protocol().empty() || request->url().empty()) {
            response->set_code("INVALID_ARG");
            response->set_error_message("protocol and url are required");
            return grpc::Status::OK;
        }
        if (!media_backend_) {
            response->set_code("PLATFORM_UNAVAILABLE");
            response->set_error_message("media backend is unavailable");
            return grpc::Status::OK;
        }
        const ProbeCancelFn is_cancelled = context
            ? [context] { return context->IsCancelled(); }
            : ProbeCancelFn{};
        // 每种传输方式 5 秒，TCP 优先失败后回退 UDP（总请求约 10 秒）。
        const CameraProbeResult result = probe_camera(
            media_backend_, request->protocol(), request->url(),
            std::chrono::seconds(5), is_cancelled);
        // RPC code 仅表示处理成功；测活失败放在结构化 status/failure_code 中。
        response->set_status(result.status);
        response->set_failure_code(result.failure_code);
        response->set_selected_transport(result.selected_transport);
        response->set_codec(result.codec);
        response->set_width(result.width);
        response->set_height(result.height);
        response->set_fps(result.fps);
        response->set_elapsed_ms(result.elapsed_ms);
        for (const auto& attempt : result.attempts) {
            auto* proto_attempt = response->add_attempts();
            proto_attempt->set_transport(attempt.transport);
            proto_attempt->set_failure_code(attempt.failure_code);
            proto_attempt->set_elapsed_ms(attempt.elapsed_ms);
        }
        return grpc::Status::OK;
    }

    grpc::Status StartCameraPreview(grpc::ServerContext*,
                                   const argus::v1::StartCameraPreviewRequest* request,
                                   argus::v1::StartCameraPreviewResponse* response) override {
        if (!request || request->camera_id().empty() || request->url().empty()) {
            response->set_code("INVALID_ARGUMENT");
            response->set_error_message("camera_id and url must not be empty");
            return grpc::Status::OK;
        }

        std::string stream_path;
        std::string error_message;
        const std::string code = LiveStreamManager::instance().start_preview(
            request->camera_id(), request->stream_type(), request->url(),
            &stream_path, &error_message);

        response->set_code(code);
        response->set_error_message(error_message);
        response->set_stream_path(stream_path);
        const int32_t port = static_cast<int32_t>(LiveStreamManager::instance().get_http_port());
        response->set_http_port(port);
        response->set_ws_port(port);
        return grpc::Status::OK;
    }

    grpc::Status StopCameraPreview(grpc::ServerContext*,
                                  const argus::v1::StopCameraPreviewRequest* request,
                                  argus::v1::StopCameraPreviewResponse* response) override {
        if (!request || request->camera_id().empty()) {
            response->set_code("INVALID_ARGUMENT");
            response->set_error_message("camera_id must not be empty");
            return grpc::Status::OK;
        }

        LiveStreamManager::instance().stop_preview(request->camera_id(), request->stream_type());
        response->set_code("");
        return grpc::Status::OK;
    }
private:
    // 一个异步 pending 对应一个检测或识别批次：多个目标事件共享同一帧引用和抓拍图片。
    struct PendingCapture {
        enum class Kind { Alarm, PlateObservation };
        Kind kind = Kind::Alarm;
        std::vector<argus::v1::AlarmEvent> alarms;
        std::vector<argus::v1::PlateObservation> plates;
        std::vector<av_rect> plate_crop_rois;
        std::string capture_id;
        av_frame_desc frame{};
        av_rect image_roi{};
        const av_frame_ops* frame_ops = nullptr;
        bool has_image = false;
        bool images_ready = false;
        bool frame_retained = false;
        uint32_t retry_count = 0;

        PendingCapture() = default;
        PendingCapture(const PendingCapture&) = delete;
        PendingCapture& operator=(const PendingCapture&) = delete;

        PendingCapture(PendingCapture&& other) noexcept
            : kind(other.kind), alarms(std::move(other.alarms)), plates(std::move(other.plates)),
              plate_crop_rois(std::move(other.plate_crop_rois)), capture_id(std::move(other.capture_id)),
              frame(other.frame), image_roi(other.image_roi), frame_ops(other.frame_ops),
              has_image(other.has_image), images_ready(other.images_ready),
              frame_retained(other.frame_retained), retry_count(other.retry_count) {
            other.frame_retained = false;
            other.frame.frame_token = nullptr;
        }

        PendingCapture& operator=(PendingCapture&& other) noexcept {
            if (this == &other) return *this;
            release_frame();
            kind = other.kind;
            alarms = std::move(other.alarms);
            plates = std::move(other.plates);
            plate_crop_rois = std::move(other.plate_crop_rois);
            capture_id = std::move(other.capture_id);
            frame = other.frame;
            image_roi = other.image_roi;
            frame_ops = other.frame_ops;
            has_image = other.has_image;
            images_ready = other.images_ready;
            frame_retained = other.frame_retained;
            retry_count = other.retry_count;
            other.frame_retained = false;
            other.frame.frame_token = nullptr;
            return *this;
        }

        ~PendingCapture() noexcept { release_frame(); }

        void release_frame() noexcept {
            if (!frame_retained || !frame_ops || !frame.frame_token) return;
            const av_frame_ops* ops = frame_ops;
            void* token = frame.frame_token;
            frame_retained = false;
            frame.frame_token = nullptr;
            try {
                if (ops->release) ops->release(ops->ctx, token);
            } catch (...) {
                // C ABI release 不应抛出异常；即使第三方实现违规，也不能让 worker 线程退出。
            }
        }
    };

    static constexpr size_t kMaxPendingAlarmQueueSize = 256;
    static constexpr size_t kMaxReportedEventIds = 65536;
    static constexpr uint32_t kMaxPlateReportRetries = 3;

    bool remember_event_id(const std::string& event_id) {
        const auto [it, inserted] = reported_events_.insert(event_id);
        if (!inserted) return false;
        reported_event_order_.push_back(*it);
        while (reported_event_order_.size() > kMaxReportedEventIds) {
            reported_events_.erase(reported_event_order_.front());
            reported_event_order_.pop_front();
        }
        return true;
    }

    void forget_pending_event_ids_locked(const PendingCapture& pending) {
        const auto forget = [this](const std::string& event_id) {
            if (reported_events_.erase(event_id) == 0) return;
            const auto it = std::find(reported_event_order_.begin(), reported_event_order_.end(), event_id);
            if (it != reported_event_order_.end()) reported_event_order_.erase(it);
        };
        if (pending.kind == PendingCapture::Kind::Alarm) {
            for (const auto& alarm : pending.alarms) forget(alarm.event_id());
        } else if (pending.kind == PendingCapture::Kind::PlateObservation) {
            for (const auto& plate : pending.plates) forget(plate.event_id());
        }
    }

    void forget_pending_event_ids(const PendingCapture& pending) {
        std::lock_guard<std::mutex> queue_lock(capture_mutex_);
        forget_pending_event_ids_locked(pending);
    }

    void enqueue_pending_capture(PendingCapture pending, bool bypass_event_dedup = false) {
        PendingCapture dropped;
        bool accepted = false;
        {
            std::lock_guard<std::mutex> queue_lock(capture_mutex_);
            if (capture_accepting_.load(std::memory_order_acquire) && capture_worker_running_) {
                if (pending.kind == PendingCapture::Kind::Alarm) {
                    std::vector<argus::v1::AlarmEvent> new_alarms;
                    new_alarms.reserve(pending.alarms.size());
                    for (auto& alarm : pending.alarms) {
                        if (bypass_event_dedup || remember_event_id(alarm.event_id())) {
                            new_alarms.push_back(std::move(alarm));
                        }
                    }
                    if (!new_alarms.empty()) {
                        pending.alarms = std::move(new_alarms);
                        accepted = true;
                    }
                } else if (pending.kind == PendingCapture::Kind::PlateObservation) {
                    if (bypass_event_dedup) {
                        accepted = !pending.plates.empty();
                    } else {
                        std::vector<argus::v1::PlateObservation> new_plates;
                        std::vector<av_rect> new_rois;
                        new_plates.reserve(pending.plates.size());
                        new_rois.reserve(pending.plate_crop_rois.size());
                        for (size_t i = 0; i < pending.plates.size(); ++i) {
                            if (remember_event_id(pending.plates[i].event_id())) {
                                new_plates.push_back(std::move(pending.plates[i]));
                                if (i < pending.plate_crop_rois.size()) {
                                    new_rois.push_back(pending.plate_crop_rois[i]);
                                }
                            }
                        }
                        if (!new_plates.empty()) {
                            pending.plates = std::move(new_plates);
                            pending.plate_crop_rois = std::move(new_rois);
                            accepted = true;
                        }
                    }
                }
                if (accepted) {
                    if (capture_queue_.size() >= kMaxPendingAlarmQueueSize) {
                        dropped = std::move(capture_queue_.front());
                        capture_queue_.pop_front();
                        forget_pending_event_ids_locked(dropped);
                        capture_queue_dropped_.fetch_add(1, std::memory_order_relaxed);
                    }
                    capture_queue_.push_back(std::move(pending));
                    capture_enqueued_.fetch_add(1, std::memory_order_relaxed);
                }
            }
        }
        // 释放被丢弃的旧帧和未入队（重复/停机）的当前帧，均不在队列锁内执行平台回调。
        dropped.release_frame();
        if (!accepted) pending.release_frame();
        if (accepted) capture_cv_.notify_one();
    }

    void process_pending_capture(PendingCapture& pending) noexcept {
        try {
            if (pending.kind == PendingCapture::Kind::Alarm) {
                std::string image_id;
                std::string image_rel_path;
                if (pending.has_image) {
                    ImageRecord record;
                    const av_status image_status = ImageManager::instance().save_detection_image(
                        &pending.frame, &pending.image_roi, pending.capture_id, &record);
                    // 图片编码落盘后立即归还帧缓冲，避免持有帧引用跨越同步 IPC 阻塞等待。
                    pending.release_frame();
                    if (image_status == AV_OK) {
                        image_id = record.image_id;
                        image_rel_path = record.rel_path;
                        for (auto& alarm : pending.alarms) {
                            alarm.set_image_id(image_id);
                            alarm.set_image_rel_path(image_rel_path);
                        }
                    } else {
                        capture_image_failures_.fetch_add(1, std::memory_order_relaxed);
                    }
                } else {
                    pending.release_frame();
                }

                bool reported_any = false;
                for (const auto& alarm : pending.alarms) {
                    if (!app_client_ || !app_client_->report_alarm(alarm)) {
                        capture_report_failures_.fetch_add(1, std::memory_order_relaxed);
                    } else {
                        reported_any = true;
                    }
                }
                // 图片是批次级资源；至少一个目标事件获得 ACK 后即可把共享图片标为 reported。
                if (reported_any && !image_id.empty() &&
                    ImageManager::instance().mark_reported(image_id) != AV_OK) {
                    capture_mark_failures_.fetch_add(1, std::memory_order_relaxed);
                }
            } else if (pending.kind == PendingCapture::Kind::PlateObservation) {
                std::string pano_image_id;
                std::string pano_image_rel_path;
                std::vector<std::pair<std::string, std::string>> plate_images;
                bool capture_succeeded = pending.has_image;
                if (pending.has_image && !pending.images_ready) {
                    ImageRecord pano_record;
                    const av_status pano_status = ImageManager::instance().save_detection_image(
                        &pending.frame, nullptr, pending.capture_id + "-pano", &pano_record);
                    if (pano_status == AV_OK) {
                        pano_image_id = pano_record.image_id;
                        pano_image_rel_path = pano_record.rel_path;
                    } else {
                        capture_succeeded = false;
                        capture_image_failures_.fetch_add(1, std::memory_order_relaxed);
                    }

                    if (capture_succeeded) {
                        plate_images.resize(pending.plates.size());
                        if (pending.plate_crop_rois.size() != 0 &&
                            pending.plate_crop_rois.size() != pending.plates.size()) {
                            capture_succeeded = false;
                            capture_image_failures_.fetch_add(1, std::memory_order_relaxed);
                        }
                        for (size_t i = 0; capture_succeeded && i < pending.plates.size(); ++i) {
                            if (pending.plate_crop_rois.empty()) break;
                            ImageRecord plate_record;
                            const av_status plate_status = ImageManager::instance().save_detection_image(
                                &pending.frame, &pending.plate_crop_rois[i],
                                pending.capture_id + "-plate-" + std::to_string(i + 1), &plate_record);
                            if (plate_status == AV_OK) {
                                plate_images[i] = {plate_record.image_id, plate_record.rel_path};
                            } else {
                                capture_succeeded = false;
                                capture_image_failures_.fetch_add(1, std::memory_order_relaxed);
                            }
                        }
                    }
                    // JPEG 编码完成后立即归还帧引用；后续 protobuf/IPC 不再依赖平台帧。
                    pending.release_frame();

                    if (capture_succeeded) {
                        for (size_t i = 0; i < pending.plates.size(); ++i) {
                            pending.plates[i].set_image_id(pano_image_id);
                            pending.plates[i].set_image_rel_path(pano_image_rel_path);
                            if (!pending.plate_crop_rois.empty()) {
                                pending.plates[i].set_plate_image_id(plate_images[i].first);
                                pending.plates[i].set_plate_image_rel_path(plate_images[i].second);
                            }
                        }
                    }
                } else if (!pending.has_image) {
                    pending.release_frame();
                }

                if (!capture_succeeded) {
                    // 没有完整图片集合时不发送观测，允许后续同一 event_id 的完整回调重新入队。
                    forget_pending_event_ids(pending);
                    capture_processed_.fetch_add(1, std::memory_order_relaxed);
                    return;
                }

                PendingCapture retry;
                retry.kind = PendingCapture::Kind::PlateObservation;
                retry.capture_id = pending.capture_id;
                retry.has_image = true;
                retry.images_ready = true;
                retry.retry_count = pending.retry_count + 1;
                for (const auto& plate : pending.plates) {
                    if (!app_client_ || !app_client_->report_plate_observation(plate)) {
                        capture_report_failures_.fetch_add(1, std::memory_order_relaxed);
                        if (retry.retry_count <= kMaxPlateReportRetries) {
                            retry.plates.push_back(plate);
                        }
                    } else {
                        if (!plate.image_id().empty() &&
                            ImageManager::instance().mark_reported(plate.image_id()) != AV_OK) {
                            capture_mark_failures_.fetch_add(1, std::memory_order_relaxed);
                        }
                        if (!plate.plate_image_id().empty() &&
                            ImageManager::instance().mark_reported(plate.plate_image_id()) != AV_OK) {
                            capture_mark_failures_.fetch_add(1, std::memory_order_relaxed);
                        }
                    }
                }
                if (!retry.plates.empty()) {
                    // 图片已落盘，重试只复用 protobuf 中的图片引用，不再次访问已释放的帧。
                    enqueue_pending_capture(std::move(retry), true);
                }
            }
        } catch (...) {
            capture_processing_failures_.fetch_add(1, std::memory_order_relaxed);
        }
        pending.release_frame();
        capture_processed_.fetch_add(1, std::memory_order_relaxed);
    }

    void reconcile_unreported_images() noexcept {
        try {
            const auto records = ImageManager::instance().list_unreported_images();
            if (records.empty() || !app_client_) return;

            std::vector<argus::v1::OrphanImageEntry> entries;
            entries.reserve(records.size());
            std::unordered_set<std::string> submitted_ids;
            for (const auto& record : records) {
                if (record.image_id.empty() || !submitted_ids.insert(record.image_id).second) continue;
                argus::v1::OrphanImageEntry entry;
                entry.set_event_id(record.event_id);
                entry.set_image_id(record.image_id);
                entry.set_relative_path(record.rel_path);
                entry.set_created_at_ns(record.created_at_ns);
                entries.push_back(std::move(entry));
            }
            if (entries.empty()) return;

            argus::v1::ReportOrphanImagesResponse response;
            if (!app_client_->report_orphan_images(entries, &response)) return;

            for (const auto& image_id : response.retain_image_ids()) {
                if (!submitted_ids.contains(image_id)) continue;
                if (ImageManager::instance().mark_reported(image_id) != AV_OK) {
                    capture_mark_failures_.fetch_add(1, std::memory_order_relaxed);
                }
            }
            for (const auto& image_id : response.delete_image_ids()) {
                if (!submitted_ids.contains(image_id)) continue;
                if (ImageManager::instance().delete_image_with_status(image_id) == ImageDeleteStatus::FAILED) {
                    capture_image_failures_.fetch_add(1, std::memory_order_relaxed);
                }
            }
        } catch (...) {
            capture_processing_failures_.fetch_add(1, std::memory_order_relaxed);
        }
    }

    void log_capture_metrics() {
        const auto now = std::chrono::steady_clock::now();
        if (last_capture_debug_log_.time_since_epoch().count() == 0) {
            last_capture_debug_log_ = now;
            return;
        }
        const double elapsed_seconds = std::chrono::duration<double>(now - last_capture_debug_log_).count();
        if (elapsed_seconds < 1.0) return;
        last_capture_debug_log_ = now;

        const uint64_t enqueued = capture_enqueued_.exchange(0, std::memory_order_relaxed);
        const uint64_t processed = capture_processed_.exchange(0, std::memory_order_relaxed);
        const uint64_t queue_drops = capture_queue_dropped_.exchange(0, std::memory_order_relaxed);
        const uint64_t image_failures = capture_image_failures_.exchange(0, std::memory_order_relaxed);
        const uint64_t report_failures = capture_report_failures_.exchange(0, std::memory_order_relaxed);
        const uint64_t mark_failures = capture_mark_failures_.exchange(0, std::memory_order_relaxed);
        const uint64_t processing_failures = capture_processing_failures_.exchange(0, std::memory_order_relaxed);
        size_t queue_depth = 0;
        {
            std::lock_guard<std::mutex> lock(capture_mutex_);
            queue_depth = capture_queue_.size();
        }
        LOG_DEBUG(
            "engine.result", "alarm.capture_pipeline_metrics", "asynchronous alarm capture metrics", "",
            {{"capture_queued_fps", static_cast<double>(enqueued) / elapsed_seconds},
             {"capture_processed_fps", static_cast<double>(processed) / elapsed_seconds},
             {"capture_queue_depth", static_cast<int64_t>(queue_depth)},
             {"capture_queue_drops", static_cast<int64_t>(queue_drops)},
             {"capture_retain_failures", static_cast<int64_t>(
                  capture_retain_failures_.exchange(0, std::memory_order_relaxed))},
             {"capture_image_failures", static_cast<int64_t>(image_failures)},
             {"capture_report_failures", static_cast<int64_t>(report_failures)},
             {"capture_mark_failures", static_cast<int64_t>(mark_failures)},
             {"capture_processing_failures", static_cast<int64_t>(processing_failures)}});
    }

    void capture_worker_loop() {
        auto last_orphan_reconcile = std::chrono::steady_clock::now() - std::chrono::seconds(5);
        for (;;) {
            PendingCapture pending;
            {
                std::unique_lock<std::mutex> lock(capture_mutex_);
                const bool notified = capture_cv_.wait_for(lock, std::chrono::seconds(1), [this] {
                    return !capture_queue_.empty() || !capture_worker_running_;
                });
                if (!notified) {
                    lock.unlock();
                    log_capture_metrics();
                    if (std::chrono::steady_clock::now() - last_orphan_reconcile >= std::chrono::seconds(5)) {
                        reconcile_unreported_images();
                        last_orphan_reconcile = std::chrono::steady_clock::now();
                    }
                    continue;
                }
                if (!capture_worker_running_) break;
                if (capture_queue_.empty()) continue;
                pending = std::move(capture_queue_.front());
                capture_queue_.pop_front();
            }
            // 所有 JPEG、文件系统和 ReportAlarm/ReportPlateObservation 操作均在该线程执行，不阻塞算法 worker。
            process_pending_capture(pending);
            log_capture_metrics();
            if (std::chrono::steady_clock::now() - last_orphan_reconcile >= std::chrono::seconds(5)) {
                reconcile_unreported_images();
                last_orphan_reconcile = std::chrono::steady_clock::now();
            }
        }
    }

    void stop_capture_worker() {
        std::deque<PendingCapture> discarded;
        {
            std::lock_guard<std::mutex> lock(capture_mutex_);
            capture_worker_running_ = false;
            discarded.swap(capture_queue_);
        }
        for (auto& pending : discarded) pending.release_frame();
        capture_cv_.notify_one();
        if (capture_worker_thread_.joinable()) capture_worker_thread_.join();
    }

    struct RuntimeSnapshot {
        std::unordered_map<std::string, argus::v1::CameraTaskConfig> task_configs;
        std::unordered_map<std::string, argus::v1::AlgorithmInstanceConfig> instance_configs;
        std::unordered_map<std::string, std::shared_ptr<LoadedPackage>> loaded_packages;
        std::unordered_map<std::string, std::string> active_versions;
    };

    bool snapshot_runtime_state(RuntimeSnapshot& snapshot, std::string& error) const {
        snapshot.task_configs = task_configs_;
        snapshot.instance_configs = instance_configs_;
        snapshot.loaded_packages = loaded_packages_;
        return snapshot_active_versions(snapshot.active_versions, error);
    }

    void clear_runtime_state() {
        std::unordered_set<std::string> instance_ids;
        for (const auto& instance_id : AlgoManager::instance().instance_ids()) {
            instance_ids.insert(instance_id);
        }
        for (const auto& [instance_id, resource] : instance_resources_) {
            (void)resource;
            instance_ids.insert(instance_id);
        }
        for (const auto& instance_id : instance_ids) remove_instance(instance_id, false);
        for (const auto& camera_id : TaskScheduler::instance().task_ids()) {
            TaskScheduler::instance().stop_task(camera_id);
        }
        TaskScheduler::instance().stop_all();
        ResourceLedger::instance().clear();
        task_configs_.clear();
        instance_configs_.clear();
        instance_resources_.clear();
        loaded_packages_.clear();
    }

    bool restore_runtime_state(const RuntimeSnapshot& snapshot, std::string& error) {
        clear_runtime_state();
        loaded_packages_ = snapshot.loaded_packages;
        bool restored = true;
        const auto remember_error = [&](const std::string& message) {
            if (error.empty()) {
                error = message;
            } else {
                error += "; " + message;
            }
            restored = false;
        };
        error.clear();
        for (const auto& [camera_id, config] : snapshot.task_configs) {
            (void)camera_id;
            if (const std::string task_error = upsert_task(config); !task_error.empty()) {
                remember_error("cannot restore task " + config.camera_id() + ": " + task_error);
            }
        }
        for (const auto& [instance_id, config] : snapshot.instance_configs) {
            (void)instance_id;
            if (const std::string instance_error = reconcile_instance(config); !instance_error.empty()) {
                remember_error("cannot restore instance " + config.instance_id() + ": " + instance_error);
            }
        }
        std::string marker_error;
        if (!restore_active_versions(snapshot.active_versions, marker_error)) {
            remember_error("cannot restore active package markers: " + marker_error);
        }
        if (!restored) clear_runtime_state();
        return restored;
    }

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
        const std::string manifest_algorithm_id = manifest.value("algorithm_id", "");
        const std::string platform_id = manifest.value("platform_id", "");
        if (manifest_algorithm_id != algorithm_id || !safe_package_component(manifest_algorithm_id) || platform_id.empty()) {
            error = "PACKAGE_MANIFEST_INVALID";
            return nullptr;
        }
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter || platform_id != adapter->get_profile().platform_id) {
            error = "PLATFORM_MISMATCH";
            return nullptr;
        }
        argus::utils::PackageLibraryEntry library_entry;
        std::string library_error;
        if (!argus::utils::resolve_conventional_package_library(root, manifest_algorithm_id,
                                                                   library_entry, library_error)) {
            error = library_error == "conventional library entry is not a regular file"
                ? "PACKAGE_LIBRARY_INVALID"
                : "PACKAGE_LIBRARY_MISSING";
            return nullptr;
        }
        const fs::path library_path = library_entry.path;
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
        package->algorithm_type = manifest.value("algorithm_type", "object_detection");
        package->log_context.algorithm_id = algorithm_id;
        package->log_context.package_version = version;
        package->log_context.platform_id = platform_id;
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
        args.log = argus::logging::sdk_algo_log_bridge;
        args.log_user = &package->log_context;
        if (package->abi->library_open(&args, &package->library) != AV_OK || !package->library) {
            error = "PACKAGE_LIBRARY_OPEN_FAILED";
            return nullptr;
        }
        av_algo_library_info info{};
        info.size = sizeof(info);
        info.api_version = AV_ALGO_API_VERSION;
        if (package->abi->library_query(package->library, &info) != AV_OK ||
            info.size < sizeof(av_algo_library_info) || info.api_version != AV_ALGO_API_VERSION ||
            std::string(info.algorithm_id) != algorithm_id || std::string(info.version) != version ||
            std::string(info.algorithm_type) != package->algorithm_type ||
            !is_supported_algorithm_type(std::string(info.algorithm_type))) {
            error = "PACKAGE_METADATA_MISMATCH";
            return nullptr;
        }
        loaded_packages_.emplace(key, package);
        return package;
    }

    std::string make_rules(const argus::v1::AlgorithmInstanceConfig& config,
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

    void apply_motion_gate_proto(const std::shared_ptr<AlgorithmInstance>& instance,
                                 const argus::v1::MotionGateConfig& proto_mg) const {
        if (!instance) return;
        MotionGateConfig mg_cfg = instance->get_motion_gate().get_config();
        mg_cfg.enabled = proto_mg.enabled();
        if (proto_mg.threshold() > 0) mg_cfg.threshold = proto_mg.threshold();
        if (proto_mg.contour_area() > 0) mg_cfg.contour_area = proto_mg.contour_area();
        if (proto_mg.frame_alpha() > 0.0f) mg_cfg.frame_alpha = proto_mg.frame_alpha();
        if (proto_mg.keepalive_interval_ms() > 0) {
            mg_cfg.keepalive_interval = std::chrono::milliseconds(proto_mg.keepalive_interval_ms());
        }
        instance->update_motion_gate(mg_cfg);
    }

    bool has_desired_instance_reference(const std::string& algorithm_id) const {
        // active_package_versions 只是激活标记，不代表有任务使用该算法包。
        for (const auto& instance : applied_desired_state_.instances()) {
            if (instance.enabled() && instance.algorithm_id() == algorithm_id) return true;
        }
        return false;
    }

    void remove_instance(const std::string& instance_id, bool forget_config = true) {
        const auto instance = AlgoManager::instance().get(instance_id);
        if (!instance) {
            ResourceLedger::instance().release(instance_id);
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

    std::string reconcile_instance(const argus::v1::AlgorithmInstanceConfig& config) {
        if (!safe_package_component(config.algorithm_id()) || !safe_package_component(config.algorithm_version()) ||
            config.algorithm_version().empty()) return "CONFIG_INVALID";
        const auto task = TaskScheduler::instance().get_task(config.camera_id());
        const auto existing = AlgoManager::instance().get(config.instance_id());
        if (!config.enabled()) {
            remove_instance(config.instance_id());
            return {};
        }
        if (!task) return "TASK_NOT_FOUND";
        const int32_t target_fps = config.analysis_fps() > 0 ? config.analysis_fps() : 25;
        if (existing && (existing->get_algorithm_id() != config.algorithm_id() ||
                         existing->get_version() != config.algorithm_version() ||
                         existing->get_target_fps() != target_fps)) {
            // FPS 变更不能只改成员变量：资源账本按档位结算，必须释放旧账单
            // （remove_instance 内 release）后落入下方 create 路径重新 allocate（R10b）。
            remove_instance(config.instance_id());
        }
        const auto current = AlgoManager::instance().get(config.instance_id());
        if (current && current->is_running()) {
            std::vector<av_rule> rules;
            std::vector<std::vector<av_point>> points;
            const std::string rule_error = make_rules(config, rules, points);
            if (!rule_error.empty()) return "CONFIG_INVALID";
            if (!config.params_json().empty()) {
                const av_status status = current->update_params(config.params_json());
                if (status != AV_OK) return "CONFIG_UPDATE_FAILED";
            }
            if (config.has_motion_gate()) {
                apply_motion_gate_proto(current, config.motion_gate());
            }
            if (current->set_rules(rules) != AV_OK) return "CONFIG_INVALID";
            task->add_instance(current);
            instance_configs_[config.instance_id()] = config;
            return {};
        }
        if (current) {
            remove_instance(config.instance_id());
        }

        std::string package_error;
        const auto package = load_package(config.algorithm_id(), config.algorithm_version(), package_error);
        if (!package) return package_error;
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
            target_fps, config.params_json().empty() ? "{}" : config.params_json(), package->abi, package->library,
            package->algorithm_type);
        const auto adapter = platform::PlatformRegistry::instance().get_active_adapter();
        if (!adapter || instance->init(FramePool::instance().get_frame_ops(), adapter->get_c_image_ops()) != AV_OK) {
            ResourceLedger::instance().release(config.instance_id());
            return "INSTANCE_CREATE_FAILED";
        }
        if (config.has_motion_gate()) {
            apply_motion_gate_proto(instance, config.motion_gate());
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

    void log_invalid_result(const std::shared_ptr<AlgorithmInstance>& instance,
                            const av_algo_result& result, const av_frame_desc& frame,
                            std::string_view reason) noexcept {
        const uint64_t count = invalid_result_count_.fetch_add(1, std::memory_order_relaxed) + 1;
        const uint32_t result_kind = result.size >= offsetof(av_algo_result, kind) + sizeof(result.kind)
            ? result.kind
            : 0;
        const uint32_t result_image_count =
            result.size >= offsetof(av_algo_result, image_count) + sizeof(result.image_count)
            ? result.image_count
            : 0;
        LOG_WARN(
            "engine.result", "ALGO_RESULT_INVALID", "algorithm result rejected", "ALGO_RESULT_INVALID",
            {{"algorithm_id", instance ? instance->get_algorithm_id() : ""},
             {"instance_id", instance ? instance->get_instance_id() : ""},
             {"frame_id", static_cast<int64_t>(frame.frame_id)},
             {"result_kind", static_cast<int64_t>(result_kind)},
             {"result_image_count", static_cast<int64_t>(result_image_count)},
             {"invalid_result_count", static_cast<int64_t>(count)},
             {"reason", std::string(reason)}});
    }

    void handle_result(const std::shared_ptr<AlgorithmInstance>& instance,
                       const av_algo_result& result, const av_frame_desc& frame) {
        if (!instance) return;
        if (result.size < sizeof(av_algo_result) || result.api_version != AV_ALGO_API_VERSION ||
            !result.json || result.json_len == 0 || result.json_len > AV_MAX_RESULT_JSON_BYTES ||
            result.image_count > 4096 || (result.image_count > 0 && !result.images)) {
            log_invalid_result(instance, result, frame, "result header or image pointer is invalid");
            return;
        }

        if (result.kind == AV_RESULT_ALARM) {
            handle_alarm_result(instance, result, frame);
        } else if (result.kind == AV_RESULT_RECOGNITION) {
            if (instance->get_algorithm_type() == "license_plate_recognition") {
                handle_license_plate_result(instance, result, frame);
            }
        } else if (result.kind != AV_RESULT_SELF_TEST) {
            log_invalid_result(instance, result, frame, "unsupported result kind");
        }
    }

    void handle_alarm_result(const std::shared_ptr<AlgorithmInstance>& instance,
                             const av_algo_result& result, const av_frame_desc& frame) {
        try {
            const auto value = nlohmann::json::parse(result.json, result.json + result.json_len);
            if (!value.is_object() || !value.contains("event_id") || !value["event_id"].is_string() ||
                !value.contains("alarm_type_id") || !value["alarm_type_id"].is_string() ||
                !value.contains("objects") || !value["objects"].is_array() ||
                value["objects"].empty() || value["objects"].size() > 4096) return;

            const std::string batch_event_id = value["event_id"].get<std::string>();
            const std::string alarm_type_id = value["alarm_type_id"].get<std::string>();
            const auto is_safe_component = [](const std::string& component) {
                if (component.empty() || component.size() > 128) return false;
                for (const unsigned char ch : component) {
                    if (!(std::isalnum(ch) || ch == '.' || ch == '_' || ch == '-')) return false;
                }
                return true;
            };
            if (!is_safe_component(batch_event_id) || !is_safe_component(alarm_type_id) ||
                (!instance->get_alarm_type_id().empty() && alarm_type_id != instance->get_alarm_type_id())) return;

            std::vector<argus::v1::DetectedObject> detected_objects;
            detected_objects.reserve(value["objects"].size());
            for (const auto& object : value["objects"]) {
                if (!object.is_object() || !object.contains("label") || !object["label"].is_string() ||
                    !object.contains("confidence") || !object["confidence"].is_number() ||
                    !object.contains("bbox") || !object["bbox"].is_array() || object["bbox"].size() != 4 ||
                    !object.contains("track_id") || !object["track_id"].is_number_integer()) return;
                const double confidence = object["confidence"].get<double>();
                if (!std::isfinite(confidence) || confidence < 0.0 || confidence > 1.0) return;
                std::array<double, 4> bbox{};
                for (size_t index = 0; index < bbox.size(); ++index) {
                    if (!object["bbox"][index].is_number()) return;
                    bbox[index] = object["bbox"][index].get<double>();
                    if (!std::isfinite(bbox[index]) || bbox[index] < 0.0 || bbox[index] > 1.0) return;
                }
                if (bbox[0] + bbox[2] > 1.0 || bbox[1] + bbox[3] > 1.0) return;

                argus::v1::DetectedObject detected;
                detected.set_label(object["label"].get<std::string>());
                detected.set_confidence(static_cast<float>(confidence));
                detected.mutable_bbox()->set_x_min(static_cast<float>(bbox[0]));
                detected.mutable_bbox()->set_y_min(static_cast<float>(bbox[1]));
                detected.mutable_bbox()->set_x_max(static_cast<float>(bbox[0] + bbox[2]));
                detected.mutable_bbox()->set_y_max(static_cast<float>(bbox[1] + bbox[3]));
                detected.set_track_id(object["track_id"].get<int64_t>());
                detected_objects.push_back(std::move(detected));
            }

            PendingCapture pending;
            pending.kind = PendingCapture::Kind::Alarm;
            pending.capture_id = instance->get_run_id() + "/" + batch_event_id;
            pending.alarms.reserve(detected_objects.size());
            std::unordered_set<std::string> batch_event_ids;
            for (size_t index = 0; index < detected_objects.size(); ++index) {
                argus::v1::AlarmEvent alarm;
                alarm.set_instance_id(instance->get_instance_id());
                alarm.set_camera_id(instance->get_camera_id());
                alarm.set_algorithm_id(instance->get_algorithm_id());
                alarm.set_algorithm_version(instance->get_version());
                alarm.set_alarm_type_id(alarm_type_id);
                alarm.set_wall_time_ns(frame.wall_time_ns);
                alarm.set_time_synced(frame.time_synced != 0);
                const std::string event_id = pending.capture_id + "-" + std::to_string(index + 1);
                if (!batch_event_ids.insert(event_id).second) return;
                alarm.set_event_id(event_id);
                *alarm.add_objects() = detected_objects[index];
                pending.alarms.push_back(std::move(alarm));
            }

            if (result.image_count > 0) {
                const auto& request = result.images[0];
                if (request.size < sizeof(av_algo_image_req) || request.api_version != AV_ALGO_API_VERSION ||
                    !std::isfinite(request.x) || !std::isfinite(request.y) ||
                    !std::isfinite(request.w) || !std::isfinite(request.h) || request.x < 0.0f ||
                    request.y < 0.0f || request.w <= 0.0f || request.h <= 0.0f ||
                    request.x + request.w > 1.0f || request.y + request.h > 1.0f) return;
                pending.has_image = true;
                pending.frame = frame;
                pending.image_roi.size = sizeof(av_rect);
                pending.image_roi.api_version = AV_ALGO_API_VERSION;
                pending.image_roi.x = request.x;
                pending.image_roi.y = request.y;
                pending.image_roi.width = request.w;
                pending.image_roi.height = request.h;
                pending.frame_ops = FramePool::instance().get_frame_ops();
                if (!pending.frame_ops || !pending.frame_ops->retain || !pending.frame_ops->release ||
                    pending.frame_ops->retain(pending.frame_ops->ctx, pending.frame.frame_token) != AV_OK) {
                    capture_retain_failures_.fetch_add(1, std::memory_order_relaxed);
                    pending.has_image = false;
                    pending.frame_ops = nullptr;
                    pending.frame.frame_token = nullptr;
                } else {
                    pending.frame_retained = true;
                }
            }
            // 仅在任务成功进入有界队列后记录各目标 event_id；满队列/停机丢弃不会阻塞算法线程。
            enqueue_pending_capture(std::move(pending));
        } catch (...) {
            // Malformed plugin output is isolated to this result callback.
        }
    }

    void handle_license_plate_result(const std::shared_ptr<AlgorithmInstance>& instance,
                                     const av_algo_result& result, const av_frame_desc& frame) {
        const auto reject = [this, &instance, &result, &frame](std::string_view reason) {
            log_invalid_result(instance, result, frame, reason);
        };

        try {
            const auto value = nlohmann::json::parse(result.json, result.json + result.json_len);
            const auto field_allowed = [](const nlohmann::json& object,
                                          std::initializer_list<const char*> allowed) {
                for (const auto& [key, ignored] : object.items()) {
                    (void)ignored;
                    if (std::none_of(allowed.begin(), allowed.end(),
                                     [&key](const char* name) { return key == name; })) {
                        return false;
                    }
                }
                return true;
            };
            const auto valid_bbox = [](const nlohmann::json& json,
                                       std::array<double, 4>& bbox) {
                if (!json.is_array() || json.size() != bbox.size()) return false;
                for (size_t index = 0; index < bbox.size(); ++index) {
                    if (!json[index].is_number()) return false;
                    bbox[index] = json[index].get<double>();
                    if (!std::isfinite(bbox[index]) || bbox[index] < 0.0 || bbox[index] > 1.0) {
                        return false;
                    }
                }
                return bbox[2] > 0.0 && bbox[3] > 0.0 &&
                       bbox[0] + bbox[2] <= 1.0 && bbox[1] + bbox[3] <= 1.0;
            };
            constexpr size_t kMaxPlateTextBytes = 32;
            const auto supported_color = [](std::string_view color) {
                constexpr std::array<std::string_view, 5> colors{
                    "black", "blue", "green", "white", "yellow"};
                return std::find(colors.begin(), colors.end(), color) != colors.end();
            };
            const auto supported_type = [](std::string_view type) {
                constexpr std::array<std::string_view, 7> types{
                    "standard", "double_yellow", "new_energy", "police",
                    "coach", "hk_macau", "embassy"};
                return std::find(types.begin(), types.end(), type) != types.end();
            };
            const auto is_safe_component = [](const std::string& component) {
                if (component.empty() || component.size() > 128) return false;
                for (const unsigned char ch : component) {
                    if (!(std::isalnum(ch) || ch == '.' || ch == '_' || ch == '-')) return false;
                }
                return true;
            };
            const auto valid_image_request = [](const av_algo_image_req& request) {
                return request.size >= sizeof(av_algo_image_req) &&
                       request.api_version == AV_ALGO_API_VERSION && request.purpose == 0 &&
                       std::isfinite(request.x) && std::isfinite(request.y) &&
                       std::isfinite(request.w) && std::isfinite(request.h) &&
                       request.x >= 0.0f && request.y >= 0.0f && request.w > 0.0f &&
                       request.h > 0.0f && request.x + request.w <= 1.0f &&
                       request.y + request.h <= 1.0f;
            };
            const auto read_nonnegative_u64 = [](const nlohmann::json& json, uint64_t& value) {
                if (json.is_number_unsigned()) {
                    value = json.get<uint64_t>();
                    return true;
                }
                if (!json.is_number_integer()) return false;
                const int64_t signed_value = json.get<int64_t>();
                if (signed_value < 0) return false;
                value = static_cast<uint64_t>(signed_value);
                return true;
            };
            const auto read_nonnegative_i64 = [](const nlohmann::json& json, int64_t& value) {
                if (json.is_number_integer()) {
                    value = json.get<int64_t>();
                    return value >= 0;
                }
                if (!json.is_number_unsigned()) return false;
                const uint64_t unsigned_value = json.get<uint64_t>();
                if (unsigned_value > static_cast<uint64_t>(std::numeric_limits<int64_t>::max())) {
                    return false;
                }
                value = static_cast<int64_t>(unsigned_value);
                return true;
            };
            const auto close_to = [](float left, float right) {
                return std::fabs(left - right) <= 1e-4f;
            };

            uint64_t parsed_frame_id = 0;
            if (!value.is_object() || !field_allowed(value, {
                    "schema_version", "frame_id", "algorithm_type", "event_id", "plates"}) ||
                !value.contains("schema_version") || !value["schema_version"].is_number_integer() ||
                value["schema_version"].get<int64_t>() != 1 ||
                !value.contains("frame_id") || !read_nonnegative_u64(value["frame_id"], parsed_frame_id) ||
                parsed_frame_id != frame.frame_id ||
                !value.contains("algorithm_type") || !value["algorithm_type"].is_string() ||
                value["algorithm_type"].get<std::string>() != "license_plate_recognition" ||
                !value.contains("plates") || !value["plates"].is_array() ||
                value["plates"].empty() || value["plates"].size() > 4096) {
                reject("recognition result envelope is invalid");
                return;
            }

            const std::string batch_event_id = value.contains("event_id")
                ? value["event_id"].get<std::string>()
                : std::to_string(frame.frame_id);
            if (!is_safe_component(batch_event_id)) {
                reject("recognition event_id is invalid");
                return;
            }

            std::vector<argus::v1::PlateObservation> observations;
            observations.reserve(value["plates"].size());
            std::vector<av_rect> parsed_crop_rois;
            parsed_crop_rois.reserve(value["plates"].size());
            std::unordered_set<std::string> batch_event_ids;

            for (const auto& item : value["plates"]) {
                int64_t track_id = 0;
                if (!item.is_object() || !field_allowed(item, {
                        "track_id", "plate_text", "normalized_text", "plate_color", "plate_type",
                        "confidence", "ocr_confidence", "bbox", "vehicle_bbox"}) ||
                    !item.contains("track_id") || !read_nonnegative_i64(item["track_id"], track_id) ||
                    !item.contains("plate_text") || !item["plate_text"].is_string() ||
                    !item.contains("normalized_text") || !item["normalized_text"].is_string() ||
                    !item.contains("plate_color") || !item["plate_color"].is_string() ||
                    !item.contains("plate_type") || !item["plate_type"].is_string() ||
                    !item.contains("confidence") || !item["confidence"].is_number() ||
                    !item.contains("ocr_confidence") || !item["ocr_confidence"].is_number() ||
                    !item.contains("bbox")) {
                    reject("plate item fields are invalid");
                    return;
                }

                const std::string plate_text = item["plate_text"].get<std::string>();
                const std::string normalized_text = item["normalized_text"].get<std::string>();
                const std::string plate_color = item["plate_color"].get<std::string>();
                const std::string plate_type = item["plate_type"].get<std::string>();
                if (plate_text.empty() || plate_text.size() > kMaxPlateTextBytes || normalized_text.empty() ||
                    normalized_text.size() > kMaxPlateTextBytes || plate_color.size() > 16 ||
                    plate_type.size() > 32 || !supported_color(plate_color) ||
                    !supported_type(plate_type)) {
                    reject("plate text, color, or type is invalid");
                    return;
                }

                const double confidence = item["confidence"].get<double>();
                const double ocr_confidence = item["ocr_confidence"].get<double>();
                if (!std::isfinite(confidence) || confidence < 0.0 || confidence > 1.0 ||
                    !std::isfinite(ocr_confidence) || ocr_confidence < 0.0 || ocr_confidence > 1.0) {
                    reject("plate confidence is invalid");
                    return;
                }

                std::array<double, 4> bbox{};
                if (!valid_bbox(item["bbox"], bbox)) {
                    reject("plate bbox is invalid");
                    return;
                }
                std::array<double, 4> vehicle_bbox{};
                const bool has_vehicle_bbox = item.contains("vehicle_bbox");
                if (has_vehicle_bbox && !valid_bbox(item["vehicle_bbox"], vehicle_bbox)) {
                    reject("vehicle bbox is invalid");
                    return;
                }

                const std::string event_id = instance->get_run_id() + "/" + batch_event_id + "-" +
                                             std::to_string(track_id);
                if (!batch_event_ids.insert(event_id).second) {
                    reject("plate event_id is duplicated");
                    return;
                }

                argus::v1::PlateObservation observation;
                observation.set_event_id(event_id);
                observation.set_instance_id(instance->get_instance_id());
                observation.set_camera_id(instance->get_camera_id());
                observation.set_algorithm_id(instance->get_algorithm_id());
                observation.set_algorithm_version(instance->get_version());
                observation.set_wall_time_ns(frame.wall_time_ns);
                observation.set_time_synced(frame.time_synced != 0);
                observation.set_track_id(track_id);
                observation.set_plate_text(plate_text);
                observation.set_normalized_text(normalized_text);
                observation.set_plate_color(plate_color);
                observation.set_plate_type(plate_type);
                observation.set_confidence(static_cast<float>(confidence));
                observation.set_ocr_confidence(static_cast<float>(ocr_confidence));
                observation.mutable_plate_bbox()->set_x_min(static_cast<float>(bbox[0]));
                observation.mutable_plate_bbox()->set_y_min(static_cast<float>(bbox[1]));
                observation.mutable_plate_bbox()->set_x_max(static_cast<float>(bbox[0] + bbox[2]));
                observation.mutable_plate_bbox()->set_y_max(static_cast<float>(bbox[1] + bbox[3]));
                if (has_vehicle_bbox) {
                    observation.mutable_vehicle_bbox()->set_x_min(static_cast<float>(vehicle_bbox[0]));
                    observation.mutable_vehicle_bbox()->set_y_min(static_cast<float>(vehicle_bbox[1]));
                    observation.mutable_vehicle_bbox()->set_x_max(static_cast<float>(vehicle_bbox[0] + vehicle_bbox[2]));
                    observation.mutable_vehicle_bbox()->set_y_max(static_cast<float>(vehicle_bbox[1] + vehicle_bbox[3]));
                }
                observations.push_back(std::move(observation));

                av_rect crop_roi{};
                crop_roi.size = sizeof(av_rect);
                crop_roi.api_version = AV_ALGO_API_VERSION;
                crop_roi.x = static_cast<float>(bbox[0]);
                crop_roi.y = static_cast<float>(bbox[1]);
                crop_roi.width = static_cast<float>(bbox[2]);
                crop_roi.height = static_cast<float>(bbox[3]);
                parsed_crop_rois.push_back(crop_roi);
            }

            if (!frame.frame_token || !frame.opaque || frame.size < sizeof(av_frame_desc)) {
                reject("capture frame is invalid");
                return;
            }
            if (result.image_count == 0 || !result.images) {
                reject("recognition image request is missing");
                return;
            }
            const uint32_t expected_crop_count = static_cast<uint32_t>(1 + observations.size());
            const bool has_crop_images = result.image_count == expected_crop_count;
            if (result.image_count != 1 && !has_crop_images) {
                reject("image request count does not match plates");
                return;
            }
            if (!valid_image_request(result.images[0]) ||
                !close_to(result.images[0].x, 0.0f) || !close_to(result.images[0].y, 0.0f) ||
                !close_to(result.images[0].w, 1.0f) || !close_to(result.images[0].h, 1.0f)) {
                reject("panorama image request is invalid");
                return;
            }
            if (has_crop_images) {
                for (size_t index = 0; index < parsed_crop_rois.size(); ++index) {
                    const auto& request = result.images[index + 1];
                    const auto& expected = parsed_crop_rois[index];
                    if (!valid_image_request(request) ||
                        !close_to(request.x, expected.x) || !close_to(request.y, expected.y) ||
                        !close_to(request.w, expected.width) || !close_to(request.h, expected.height)) {
                        reject("plate crop image request does not match plate order");
                        return;
                    }
                }
            }

            PendingCapture pending;
            pending.kind = PendingCapture::Kind::PlateObservation;
            pending.capture_id = instance->get_run_id() + "/" + batch_event_id;
            pending.plates = std::move(observations);
            if (has_crop_images) pending.plate_crop_rois = std::move(parsed_crop_rois);
            pending.has_image = true;
            pending.frame = frame;
            pending.image_roi.size = sizeof(av_rect);
            pending.image_roi.api_version = AV_ALGO_API_VERSION;
            pending.image_roi.x = 0.0f;
            pending.image_roi.y = 0.0f;
            pending.image_roi.width = 1.0f;
            pending.image_roi.height = 1.0f;
            pending.frame_ops = FramePool::instance().get_frame_ops();
            if (!pending.frame_ops || !pending.frame_ops->retain || !pending.frame_ops->release ||
                pending.frame_ops->retain(pending.frame_ops->ctx, pending.frame.frame_token) != AV_OK) {
                capture_retain_failures_.fetch_add(1, std::memory_order_relaxed);
                reject("capture frame retain failed");
                return;
            }
            pending.frame_retained = true;
            enqueue_pending_capture(std::move(pending));
        } catch (...) {
            reject("malformed recognition result");
        }
    }

    void mark_package_degraded(const std::vector<argus::v1::AlgorithmInstanceConfig>& affected,
                               const std::string& reason) {
        runtime_degraded_ = true;
        for (const auto& config : affected) {
            degraded_instance_ids_.insert(config.instance_id());
            std::string message = reason;
            const auto failure = restart_failures_.find(config.instance_id());
            if (failure != restart_failures_.end()) message += ": " + failure->second;
            argus::v1::InstanceState state;
            state.set_instance_id(config.instance_id());
            state.set_status(argus::v1::INSTANCE_STATUS_DEGRADED);
            state.set_message(message);
            if (app_client_ && !app_client_->report_instance_state(state)) {
                auto& report_error = restart_failures_[config.instance_id()];
                if (!report_error.empty()) report_error += "; ";
                report_error += "DEGRADED_STATE_REPORT_FAILED";
            }
        }
    }

    void mark_package_degraded_for_algorithm(const std::string& algorithm_id,
                                              const std::string& reason) {
        std::vector<argus::v1::AlgorithmInstanceConfig> affected;
        for (const auto& [instance_id, config] : instance_configs_) {
            (void)instance_id;
            if (config.algorithm_id() == algorithm_id) affected.push_back(config);
        }
        mark_package_degraded(affected, reason);
    }

    void append_degraded_results(const RuntimeSnapshot& snapshot,
                                 const argus::v1::DesiredState& desired,
                                 const std::string& reason,
                                 argus::v1::ApplyDesiredStateResponse* response) const {
        if (!response) return;
        const auto item_key = [](argus::v1::ReconcileItemKind kind, const std::string& id) {
            return std::to_string(static_cast<int>(kind)) + ":" + id;
        };
        std::unordered_set<std::string> seen;
        for (int index = 0; index < response->results_size(); ++index) {
            auto* item = response->mutable_results(index);
            seen.insert(item_key(item->kind(), item->id()));
            const std::string original_code = item->code();
            item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
            item->set_code("RECONCILE_ROLLBACK_FAILED");
            std::string message = "runtime is degraded; desired-state item was not confirmed restored: " + reason;
            if (!original_code.empty()) message += " (original result: " + original_code + ")";
            item->set_error_message(message);
        }
        const auto add_item = [&](argus::v1::ReconcileItemKind kind, const std::string& id,
                                  const std::string& detail) {
            if (!seen.insert(item_key(kind, id)).second) return;
            auto* item = response->add_results();
            item->set_kind(kind);
            item->set_id(id);
            item->set_status(argus::v1::RECONCILE_ITEM_STATUS_FAILED);
            item->set_code("RECONCILE_ROLLBACK_FAILED");
            item->set_error_message("runtime is degraded; desired-state item was not confirmed restored: " + detail);
        };
        for (const auto& [camera_id, config] : snapshot.task_configs) {
            (void)camera_id;
            add_item(argus::v1::RECONCILE_ITEM_KIND_TASK, config.camera_id(), reason);
        }
        for (const auto& [instance_id, config] : snapshot.instance_configs) {
            (void)instance_id;
            add_item(argus::v1::RECONCILE_ITEM_KIND_INSTANCE, config.instance_id(), reason);
        }
        for (const auto& [algorithm_id, version] : snapshot.active_versions) {
            add_item(argus::v1::RECONCILE_ITEM_KIND_PACKAGE, algorithm_id,
                     reason + " (previous version: " + version + ")");
        }
        for (const auto& config : desired.tasks()) {
            add_item(argus::v1::RECONCILE_ITEM_KIND_TASK, config.camera_id(), reason);
        }
        for (const auto& config : desired.instances()) {
            add_item(argus::v1::RECONCILE_ITEM_KIND_INSTANCE, config.instance_id(), reason);
        }
        for (const auto& package : desired.active_package_versions()) {
            add_item(argus::v1::RECONCILE_ITEM_KIND_PACKAGE, package.algorithm_id(), reason);
        }
    }

    void report_runtime_degraded(const RuntimeSnapshot& snapshot,
                                 const argus::v1::DesiredState& desired,
                                 const std::string& reason,
                                 argus::v1::ApplyDesiredStateResponse* response) {
        std::unordered_map<std::string, argus::v1::AlgorithmInstanceConfig> affected_instances;
        for (const auto& [instance_id, config] : snapshot.instance_configs) {
            affected_instances.emplace(instance_id, config);
        }
        for (const auto& [instance_id, config] : instance_configs_) {
            affected_instances.emplace(instance_id, config);
        }
        for (const auto& config : desired.instances()) {
            if (config.enabled()) affected_instances.emplace(config.instance_id(), config);
        }
        std::vector<argus::v1::AlgorithmInstanceConfig> affected;
        affected.reserve(affected_instances.size());
        for (const auto& [instance_id, config] : affected_instances) {
            (void)instance_id;
            affected.push_back(config);
        }
        mark_package_degraded(affected, reason);

        std::unordered_map<std::string, argus::v1::CameraTaskConfig> affected_tasks;
        for (const auto& [camera_id, config] : snapshot.task_configs) {
            affected_tasks.emplace(camera_id, config);
        }
        for (const auto& [camera_id, config] : task_configs_) {
            affected_tasks.emplace(camera_id, config);
        }
        for (const auto& config : desired.tasks()) {
            if (config.enabled()) affected_tasks.emplace(config.camera_id(), config);
        }
        for (const auto& [camera_id, config] : affected_tasks) {
            (void)config;
            argus::v1::TaskState state;
            state.set_camera_id(camera_id);
            state.set_status(argus::v1::TASK_STATUS_DEGRADED);
            state.set_message(reason);
            if (app_client_ && !app_client_->report_task_state(state)) {
                // The degraded state remains authoritative locally; the next report retries it.
            }
        }
        append_degraded_results(snapshot, desired, reason, response);
    }

    std::string restart_package_instances(const std::string& algorithm_id, const std::string& version) {
        restart_failures_.clear();
        std::vector<argus::v1::AlgorithmInstanceConfig> previous_configs;
        for (const auto& [instance_id, config] : instance_configs_) {
            (void)instance_id;
            if (config.algorithm_id() == algorithm_id && config.algorithm_version() != version) {
                previous_configs.push_back(config);
            }
        }
        if (previous_configs.empty()) return {};

        const std::vector<std::string> affected_instance_ids = [&] {
            std::vector<std::string> ids;
            ids.reserve(previous_configs.size());
            for (const auto& config : previous_configs) ids.push_back(config.instance_id());
            return ids;
        }();
        const auto remove_affected_instances = [&] {
            for (const auto& instance_id : affected_instance_ids) remove_instance(instance_id, false);
        };

        remove_affected_instances();
        std::string restart_error;
        for (const auto& previous : previous_configs) {
            auto replacement = previous;
            replacement.set_algorithm_version(version);
            if (const std::string error = reconcile_instance(replacement); !error.empty()) {
                if (restart_error.empty()) restart_error = error;
                restart_failures_[previous.instance_id()] = error;
            }
        }
        if (restart_failures_.empty()) return {};

        remove_affected_instances();
        std::unordered_map<std::string, std::string> restore_failures;
        for (const auto& previous : previous_configs) {
            if (const std::string error = reconcile_instance(previous); !error.empty()) {
                restore_failures[previous.instance_id()] = error;
            }
        }
        for (const auto& [instance_id, error] : restore_failures) {
            restart_failures_[instance_id] = error;
        }
        if (!restore_failures.empty()) {
            std::vector<argus::v1::AlgorithmInstanceConfig> degraded_configs;
            degraded_configs.reserve(restore_failures.size());
            for (const auto& previous : previous_configs) {
                if (restore_failures.contains(previous.instance_id())) degraded_configs.push_back(previous);
            }
            mark_package_degraded(degraded_configs, "package rollback left runtime degraded");
            return "PACKAGE_ROLLBACK_FAILED";
        }
        restart_failures_.clear();
        return restart_error.empty() ? "PACKAGE_RESTART_FAILED" : restart_error;
    }

    template <typename Response>
    void install_package(const std::string& package_path, const char* operation, Response* response) {
        std::lock_guard<std::mutex> lifecycle_lock(reconcile_mutex_);
        const std::string validator_bin = env_or_default("ARGUS_PACKAGE_VALIDATOR_PATH",
            env_or_default("ARGUS_PACKAGE_VALIDATOR", "package_validator").c_str());
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
        const bool is_upgrade = std::string(operation) == "UpgradePackage";
        std::string previous_active_version;
        bool had_previous_active_version = false;
        std::string active_read_error;
        if (is_upgrade && !read_active_version(validation.manifest.algorithm_id, previous_active_version,
                                               had_previous_active_version, active_read_error)) {
            response->set_code("PACKAGE_ACTIVATION_FAILED");
            response->set_error_message(active_read_error);
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
        if (is_upgrade) {
            const std::string restart_error = restart_package_instances(
                validation.manifest.algorithm_id, validation.manifest.version);
            if (!restart_error.empty()) {
                std::string restore_error;
                const bool restored = restore_active_version(
                    validation.manifest.algorithm_id, previous_active_version, had_previous_active_version, restore_error);
                if (!restored) {
                    mark_package_degraded_for_algorithm(
                        validation.manifest.algorithm_id, "active package marker restore failed: " + restore_error);
                }
                if (!restored || restart_error == "PACKAGE_ROLLBACK_FAILED") {
                    response->set_code("PACKAGE_ROLLBACK_FAILED");
                    response->set_error_message(restart_error +
                                                (restored ? "" : "; active marker restore failed: " + restore_error));
                } else {
                    response->set_code("PACKAGE_RESTART_FAILED");
                    response->set_error_message(restart_error);
                }
            }
        }
    }

    std::string upsert_task(const argus::v1::CameraTaskConfig& config) {
        if (config.camera_id().empty() || config.rtsp_url().empty()) return "INVALID_ARG";
        if (!config.enabled()) {
            TaskScheduler::instance().stop_task(config.camera_id());
            task_configs_.erase(config.camera_id());
            return {};
        }
        if (!platform_adapter_ || !media_backend_) return "PLATFORM_UNAVAILABLE";

        const auto existing_task = TaskScheduler::instance().get_task(config.camera_id());
        const auto it = task_configs_.find(config.camera_id());
        if (existing_task && it != task_configs_.end() &&
            it->second.rtsp_url() == config.rtsp_url() &&
            it->second.enabled() == config.enabled()) {
            task_configs_[config.camera_id()] = config;
            return {};
        }

        TaskScheduler::instance().stop_task(config.camera_id());
        task_configs_.erase(config.camera_id());
        auto task = std::make_shared<CameraTask>(
            config.camera_id(), config.rtsp_url(), platform_adapter_, media_backend_);
        if (!TaskScheduler::instance().add_task(task)) return "TASK_ALREADY_EXISTS";
        if (task->start() != AV_OK) {
            TaskScheduler::instance().stop_task(config.camera_id());
            return "MEDIA_START_FAILED";
        }
        task_configs_[config.camera_id()] = config;
        return {};
    }

    std::shared_ptr<platform::IPlatformAdapter> platform_adapter_;
    std::shared_ptr<media::IMediaBackend> media_backend_;
    std::shared_ptr<UdsClient> app_client_;
    std::deque<PendingCapture> capture_queue_;
    std::mutex capture_mutex_;
    std::condition_variable capture_cv_;
    std::thread capture_worker_thread_;
    bool capture_worker_running_ = true;
    std::atomic<bool> capture_accepting_{true};
    std::atomic<uint64_t> capture_enqueued_{0};
    std::atomic<uint64_t> capture_processed_{0};
    std::atomic<uint64_t> capture_queue_dropped_{0};
    std::atomic<uint64_t> capture_retain_failures_{0};
    std::atomic<uint64_t> capture_image_failures_{0};
    std::atomic<uint64_t> capture_report_failures_{0};
    std::atomic<uint64_t> capture_mark_failures_{0};
    std::atomic<uint64_t> capture_processing_failures_{0};
    std::atomic<uint64_t> invalid_result_count_{0};
    std::chrono::steady_clock::time_point last_capture_debug_log_{};
    std::unordered_map<std::string, std::shared_ptr<LoadedPackage>> loaded_packages_;
    std::unordered_map<std::string, ResourceRequirement> instance_resources_;
    std::unordered_map<std::string, argus::v1::CameraTaskConfig> task_configs_;
    std::unordered_map<std::string, argus::v1::AlgorithmInstanceConfig> instance_configs_;
    bool runtime_degraded_ = false;
    std::unordered_set<std::string> degraded_instance_ids_;
    std::unordered_map<std::string, std::string> restart_failures_;
    std::unordered_set<std::string> reported_events_;
    std::deque<std::string> reported_event_order_;
    std::mutex reconcile_mutex_;
    std::atomic<uint64_t> applied_revision_{0};
    argus::v1::DesiredState applied_desired_state_;
    std::string applied_desired_state_serialized_;
};

class PersonServiceImpl final : public argus::v1::PersonService::Service {
public:
    grpc::Status SyncPersons(grpc::ServerContext*, const argus::v1::SyncPersonsRequest*,
                             argus::v1::SyncPersonsResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED,
                            "Person synchronization is not implemented in this phase");
    }
};

} // namespace

UdsServer::UdsServer(const std::string& sock_path,
                     std::shared_ptr<platform::IPlatformAdapter> platform_adapter,
                     std::shared_ptr<media::IMediaBackend> media_backend,
                     std::string app_sock_path)
    : sock_path_(sock_path),
      app_sock_path_(std::move(app_sock_path)),
      platform_adapter_(std::move(platform_adapter)),
      media_backend_(std::move(media_backend)) {}

UdsServer::~UdsServer() {
    stop();
}

bool UdsServer::apply_desired_state(const argus::v1::DesiredState& desired_state,
                                     argus::v1::ApplyDesiredStateResponse* response) {
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
    engine_service_ = std::make_unique<EngineServiceImpl>(platform_adapter_, media_backend_, app_sock_path_);
    person_service_ = std::make_unique<PersonServiceImpl>();
    builder.RegisterService(engine_service_.get());
    builder.RegisterService(person_service_.get());
    server_ = builder.BuildAndStart();
    if (!server_) {
        engine_service_.reset();
        person_service_.reset();
        return false;
    }
    struct stat socket_stat{};
    if (::lstat(sock_path_.c_str(), &socket_stat) != 0 || !S_ISSOCK(socket_stat.st_mode)) {
        server_->Shutdown();
        server_->Wait();
        server_.reset();
        engine_service_.reset();
        person_service_.reset();
        return false;
    }
    socket_device_ = static_cast<uint64_t>(socket_stat.st_dev);
    socket_inode_ = static_cast<uint64_t>(socket_stat.st_ino);
    socket_identity_valid_ = true;
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
        struct stat socket_stat{};
        if (socket_identity_valid_ && ::lstat(sock_path_.c_str(), &socket_stat) == 0 &&
            S_ISSOCK(socket_stat.st_mode) && static_cast<uint64_t>(socket_stat.st_dev) == socket_device_ &&
            static_cast<uint64_t>(socket_stat.st_ino) == socket_inode_) {
            ::unlink(sock_path_.c_str());
        }
        owns_socket_ = false;
        socket_identity_valid_ = false;
    }
}

} // namespace argus::core
