/**
 * @file test_algo_sandbox.cpp
 * @brief 算法包沙箱校验、动态库加载、参数热更新及多实例并发单元测试
 */

#include <gtest/gtest.h>
#include "argus/core/algo_sandbox.hpp"
#include "argus/core/algo_instance.hpp"
#include "argus/core/frame_pool.hpp"
#include "argus/core/task_scheduler.hpp"
#include "argus/core/uds_ipc.hpp"
#include "argus/core/resource_ledger.hpp"
#include "argus/platform/mock_platform.hpp"

#include <dlfcn.h>
#include <atomic>
#include <chrono>
#include <cstdlib>
#include <cmath>
#include <fstream>
#include <filesystem>
#include <thread>

#ifndef ARGUS_FIXTURE_PACKAGE_DIR
#define ARGUS_FIXTURE_PACKAGE_DIR "tests/fixtures/packages/mock_pkg"
#endif

class NoopSource final : public argus::media::IMediaSource {
public:
    av_status start(const std::string&, argus::media::PacketCallback on_packet,
                    argus::media::StatusCallback on_status) override {
        on_packet_ = std::move(on_packet);
        on_status_ = std::move(on_status);
        return AV_OK;
    }
    void stop() override {}
    bool is_connected() const override { return false; }

    argus::media::ProbeOutcome probe(const std::string&, argus::media::Transport,
                                        std::chrono::milliseconds) override {
        argus::media::ProbeOutcome outcome;
        outcome.failure_code = "RTSP_MEDIA_ERROR";
        return outcome;
    }

private:
    argus::media::PacketCallback on_packet_;
    argus::media::StatusCallback on_status_;
};

class NoopBackend final : public argus::media::IMediaBackend {
public:
    std::unique_ptr<argus::media::IMediaSource> create_source(const std::string&) override {
        return std::make_unique<NoopSource>();
    }
};

TEST(AlgorithmInstanceTest, BridgesResultCallback) {
    const std::filesystem::path library_path = std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / "lib/libmock-detector.dylib";
    void* library = dlopen(library_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    ASSERT_NE(library, nullptr);
    auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(dlsym(library, AV_ALGO_GET_ABI_SYMBOL));
    ASSERT_NE(get_abi, nullptr);
    const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
    ASSERT_NE(abi, nullptr);

    av_algo_library_args library_args{};
    library_args.size = sizeof(library_args);
    library_args.api_version = AV_ALGO_API_VERSION;
    av_algo_library algorithm_library = nullptr;
    ASSERT_EQ(abi->library_open(&library_args, &algorithm_library), AV_OK);
    ASSERT_NE(algorithm_library, nullptr);

    auto instance = std::make_shared<argus::core::AlgorithmInstance>(
        "callback-instance", "camera-1", "mock-detector", "1.0.0", 25, "{}", abi, algorithm_library);
    ASSERT_EQ(instance->init(argus::core::FramePool::instance().get_frame_ops(), nullptr), AV_OK);

    std::atomic<bool> callback_seen{false};
    instance->set_result_callback([&](const av_algo_result& result, const av_frame_desc& frame) {
        callback_seen.store(result.kind == AV_RESULT_ALARM && frame.frame_id == 7);
    });

    auto& pool = argus::core::FramePool::instance();
    ASSERT_EQ(pool.reset(), AV_OK);
    auto* frame = pool.acquire_frame();
    ASSERT_NE(frame, nullptr);
    frame->frame_id = 7;
    frame->width = 640;
    frame->height = 640;
    frame->pixel_format = AV_PIX_NV12;
    frame->memory_type = AV_MEM_HOST;
    instance->push_frame(*frame);
    ASSERT_EQ(pool.release_frame(frame->frame_token), AV_OK);

    for (int i = 0; i < 100 && !callback_seen.load(); ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_TRUE(callback_seen.load());
    instance->stop();
    EXPECT_EQ(pool.active_frame_count(), 0);
    EXPECT_EQ(abi->library_close(algorithm_library), AV_OK);
    dlclose(library);
}

#if defined(ARGUS_RUN_SANDBOX_CHILD_TEST)
TEST(SandboxValidatorTest, RunsValidatorInChildProcess) {
    const auto result = argus::core::PackageValidator::run_sandbox_validator(
        ARGUS_PACKAGE_VALIDATOR_PATH, ARGUS_FIXTURE_PACKAGE_DIR, "var/sandbox-packages");
    EXPECT_TRUE(result.success) << result.error_stage << ": " << result.error_message;
    EXPECT_EQ(result.manifest.algorithm_id, "mock-detector");
    EXPECT_EQ(result.manifest.version, "1.0.0");
}

TEST(SandboxValidatorTest, RunsValidatorByCommandName) {
    const char* previous_path = std::getenv("PATH");
    const bool had_path = previous_path != nullptr;
    const std::string original_path = had_path ? previous_path : "";
    const std::string test_path = std::string(ARGUS_BINARY_DIR) +
                                  (had_path ? ":" + original_path : "");
    ASSERT_EQ(::setenv("PATH", test_path.c_str(), 1), 0);

    const auto result = argus::core::PackageValidator::run_sandbox_validator(
        "package_validator", ARGUS_FIXTURE_PACKAGE_DIR, "var/sandbox-packages-command");

    if (had_path) {
        ASSERT_EQ(::setenv("PATH", original_path.c_str(), 1), 0);
    } else {
        ASSERT_EQ(::unsetenv("PATH"), 0);
    }
    EXPECT_TRUE(result.success) << result.error_stage << ": " << result.error_message;
    EXPECT_EQ(result.manifest.algorithm_id, "mock-detector");
    EXPECT_EQ(result.manifest.version, "1.0.0");
}
#endif
TEST(SandboxValidatorTest, ValidMockPackage) {
    const std::string pkg_dir = ARGUS_FIXTURE_PACKAGE_DIR;
    const std::string install_base = "var/packages";

    auto res = argus::core::PackageValidator::validate_and_extract(pkg_dir, install_base);
    EXPECT_TRUE(res.success) << "Failed with error: " << res.error_message << " at stage: " << res.error_stage;
    EXPECT_EQ(res.manifest.algorithm_id, "mock-detector");
    EXPECT_EQ(res.manifest.version, "1.0.0");
    EXPECT_EQ(res.manifest.platform_id, "mock");
    EXPECT_TRUE(std::filesystem::exists("var/packages/mock-detector/1.0.0/manifest.json"));
}

namespace {
// 组装一份仅替换动态库的 mock 包副本，用于符号审计用例
std::filesystem::path stage_symbol_audit_package(const std::string& package_name,
                                                 const std::string& fixture_library) {
    const std::filesystem::path package_dir = std::filesystem::path("var") / package_name;
    std::filesystem::remove_all(package_dir);
    std::filesystem::create_directories(package_dir / "lib");
    for (const char* asset : {"manifest.json", "config.schema.json", "testimage.jpg"}) {
        std::filesystem::copy_file(std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / asset,
                                   package_dir / asset);
    }
    std::filesystem::copy_file(std::filesystem::path(ARGUS_SYMBOL_FIXTURE_LIB_DIR) / fixture_library,
                               package_dir / "lib/libmock-detector.dylib");
    return package_dir;
}
}  // namespace

TEST(SandboxValidatorTest, RejectsLibraryExportingSymbolOutsideWhitelist) {
    const auto package_dir = stage_symbol_audit_package("test_extra_symbol_package",
                                                        "libmock-extra_symbol.dylib");
    const std::string install_base = "var/packages_extra_symbol";
    std::filesystem::remove_all(install_base);

    auto res = argus::core::PackageValidator::validate_and_extract(package_dir.string(), install_base);
    EXPECT_FALSE(res.success);
    EXPECT_EQ(res.error_stage, "symbol_audit");
    EXPECT_NE(res.error_message.find("mock_extra_exported_symbol"), std::string::npos)
        << "actual message: " << res.error_message;

    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(install_base);
}

TEST(SandboxValidatorTest, AcceptsLibraryExportingOptionalFaceExtractSymbol) {
    const auto package_dir = stage_symbol_audit_package("test_face_extract_symbol_package",
                                                        "libmock-face_extract_symbol.dylib");
    const std::string install_base = "var/packages_face_extract_symbol";
    std::filesystem::remove_all(install_base);

    auto res = argus::core::PackageValidator::validate_and_extract(package_dir.string(), install_base);
    EXPECT_TRUE(res.success) << "Failed with error: " << res.error_message << " at stage: " << res.error_stage;
    EXPECT_EQ(res.manifest.algorithm_id, "mock-detector");

    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(install_base);
}

TEST(SandboxValidatorTest, FaceRecognitionManifestWithoutAlarmTypeIdValidates) {
    const std::filesystem::path temp_dir = std::filesystem::temp_directory_path() / "test_fr_manifest";
    std::filesystem::remove_all(temp_dir);
    std::filesystem::create_directories(temp_dir / "lib");

    // Copy mock lib and testimage
    std::filesystem::copy_file(std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / "testimage.jpg",
                               temp_dir / "testimage.jpg");
    std::filesystem::copy_file(std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / "lib/libmock-detector.dylib",
                               temp_dir / "lib/libmock-face.dylib");

    nlohmann::json manifest = {
        {"manifest_version", 1},
        {"algorithm_id", "mock-face"},
        {"version", "1.0.0"},
        {"name", "Mock Face Recognizer"},
        {"description", "Face recognition test package"},
        {"algorithm_type", "face_recognition"},
        {"platform_id", "mock"},
        {"min_adapter_version", "1.0.0"},
        {"runtime_constraints", {{"min_os_version", "0.0"}}},
        {"resource_profile", {
            {"min_free_memory_mb", 1},
            {"fps_tiers", {{{"fps", 1}, {"units", 1}}}}
        }},
        {"self_test", {
            {"timeout_ms", 10000},
            {"input_mode", "test_image"}
        }}
    };

    std::ofstream ofs(temp_dir / "manifest.json");
    ofs << manifest.dump(2);
    ofs.close();

    const std::string install_base = "var/packages_fr";
    std::filesystem::remove_all(install_base);
    auto res = argus::core::PackageValidator::validate_and_extract(temp_dir.string(), install_base);
    // Since mock library_query returns mock-detector & mock_alarm, it must pass manifest stage and fail at library_query stage
    EXPECT_FALSE(res.success);
    EXPECT_EQ(res.error_stage, "library_query");

    // With alarm_type_id declared, face_recognition manifest must be rejected at manifest stage
    manifest["alarm_type_id"] = "face_alarm";
    std::ofstream ofs_invalid(temp_dir / "manifest.json");
    ofs_invalid << manifest.dump(2);
    ofs_invalid.close();

    auto res_invalid = argus::core::PackageValidator::validate_and_extract(temp_dir.string(), install_base);
    EXPECT_FALSE(res_invalid.success);
    EXPECT_EQ(res_invalid.error_stage, "manifest");

    std::filesystem::remove_all(temp_dir);
    std::filesystem::remove_all(install_base);
}

TEST(PackageValidatorTest, RejectsLicensePlateRecognitionWithAlarmTypeId) {
    const std::filesystem::path temp_dir = "var/test_lpr_package";
    std::filesystem::remove_all(temp_dir);
    std::filesystem::create_directories(temp_dir / "lib");

    std::filesystem::copy_file(std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / "testimage.jpg",
                               temp_dir / "testimage.jpg");
    std::filesystem::copy_file(std::filesystem::path(ARGUS_FIXTURE_PACKAGE_DIR) / "lib/libmock-detector.dylib",
                               temp_dir / "lib/libmock-lpr.dylib");

    nlohmann::json manifest = {
        {"manifest_version", 1},
        {"algorithm_id", "mock-lpr"},
        {"version", "1.0.0"},
        {"name", "Mock License Plate Recognizer"},
        {"description", "License plate recognition test package"},
        {"algorithm_type", "license_plate_recognition"},
        {"platform_id", "mock"},
        {"min_adapter_version", "1.0.0"},
        {"runtime_constraints", {{"min_os_version", "0.0"}}},
        {"resource_profile", {
            {"min_free_memory_mb", 1},
            {"fps_tiers", {{{"fps", 1}, {"units", 1}}}}
        }},
        {"self_test", {
            {"timeout_ms", 10000},
            {"input_mode", "test_image"}
        }}
    };

    std::ofstream ofs(temp_dir / "manifest.json");
    ofs << manifest.dump(2);
    ofs.close();

    const std::string install_base = "var/packages_lpr";
    std::filesystem::remove_all(install_base);
    auto res = argus::core::PackageValidator::validate_and_extract(temp_dir.string(), install_base);
    EXPECT_FALSE(res.success);
    EXPECT_EQ(res.error_stage, "library_query");

    // With alarm_type_id declared, license_plate_recognition manifest must be rejected at manifest stage
    manifest["alarm_type_id"] = "plate_alarm";
    std::ofstream ofs_invalid(temp_dir / "manifest.json");
    ofs_invalid << manifest.dump(2);
    ofs_invalid.close();

    auto res_invalid = argus::core::PackageValidator::validate_and_extract(temp_dir.string(), install_base);
    EXPECT_FALSE(res_invalid.success);
    EXPECT_EQ(res_invalid.error_stage, "manifest");

    std::filesystem::remove_all(temp_dir);
    std::filesystem::remove_all(install_base);
}

#if !defined(ARGUS_SKIP_IPC_TESTS)
TEST(ProtoTest, ApplyResponseLifecycle) {
    argus::v1::ApplyDesiredStateResponse response;
    response.set_code("test");
    auto* item = response.add_results();
    item->set_id("id");
}

TEST(UdsServerTest, AppliesInstalledPackageRevision) {
    const std::string package_dir = "var/service-packages";
    std::filesystem::remove_all(package_dir);
    std::filesystem::create_directories(package_dir + "/mock-detector");
    std::error_code package_copy_error;
    std::filesystem::copy(
        ARGUS_FIXTURE_PACKAGE_DIR,
        package_dir + "/mock-detector/1.0.0",
        std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing,
        package_copy_error);
    ASSERT_FALSE(package_copy_error);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::UdsServer server("/tmp/argus-test-engine.sock", adapter, nullptr);
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState invalid_package;
    invalid_package.set_revision(1);
    auto* invalid_package_ref = invalid_package.add_active_package_versions();
    invalid_package_ref->set_algorithm_id("..");
    invalid_package_ref->set_version("1.0.0");
    argus::v1::ApplyDesiredStateResponse invalid_response;
    ASSERT_TRUE(server.apply_desired_state(invalid_package, &invalid_response));
    EXPECT_EQ(invalid_response.code(), "RECONCILE_FAILED");
    ASSERT_EQ(invalid_response.results_size(), 1);
    EXPECT_EQ(invalid_response.results(0).code(), "PACKAGE_ID_INVALID");

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* package = desired.add_active_package_versions();
    package->set_algorithm_id("mock-detector");
    package->set_version("1.0.0");
    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    EXPECT_TRUE(response.code().empty()) << response.error_message();
    EXPECT_EQ(response.applied_revision(), 1);

    argus::v1::ApplyDesiredStateResponse duplicate_response;
    ASSERT_TRUE(server.apply_desired_state(desired, &duplicate_response));
    EXPECT_TRUE(duplicate_response.code().empty()) << duplicate_response.error_message();
    EXPECT_EQ(duplicate_response.applied_revision(), 1);

    auto conflicting = desired;
    conflicting.mutable_active_package_versions(0)->set_version("2.0.0");
    argus::v1::ApplyDesiredStateResponse conflict_response;
    ASSERT_TRUE(server.apply_desired_state(conflicting, &conflict_response));
    EXPECT_EQ(conflict_response.code(), "STALE_REVISION");
    server.stop();
    ::unsetenv("ARGUS_PACKAGE_DIR");
}

TEST(UdsServerTest, RollsBackRuntimeWhenDesiredStateItemFails) {
    const std::string package_dir = "var/rollback-packages";
    std::filesystem::remove_all(package_dir);
    std::filesystem::create_directories(package_dir + "/mock-detector");
    std::error_code package_copy_error;
    std::filesystem::copy(
        ARGUS_FIXTURE_PACKAGE_DIR,
        package_dir + "/mock-detector/1.0.0",
        std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing,
        package_copy_error);
    ASSERT_FALSE(package_copy_error);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::UdsServer server("/tmp/argus-test-rollback.sock", adapter, backend);
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("rollback-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    auto* package = desired.add_active_package_versions();
    package->set_algorithm_id("mock-detector");
    package->set_version("missing");
    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    EXPECT_EQ(response.code(), "RECONCILE_FAILED");
    ASSERT_GE(response.results_size(), 2);
    EXPECT_EQ(response.results(0).code(), "RECONCILE_ROLLED_BACK");
    EXPECT_EQ(argus::core::TaskScheduler::instance().get_task("rollback-camera"), nullptr);

    server.stop();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
}
#if defined(ARGUS_RUN_PACKAGE_RPC_TEST)
TEST(UdsServerTest, InstallsAndUninstallsPackageThroughRpc) {
    const std::string package_dir = "var/rpc-packages";
    std::filesystem::remove_all(package_dir);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_VALIDATOR", ARGUS_PACKAGE_VALIDATOR_PATH, 1), 0);
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::UdsServer server("/tmp/argus-test-package.sock", adapter, nullptr);
    ASSERT_TRUE(server.start());

    auto channel = grpc::CreateChannel("unix:///tmp/argus-test-package.sock",
                                       grpc::InsecureChannelCredentials());
    auto stub = argus::v1::EngineService::NewStub(channel);

    argus::v1::QueryMetricsRequest metrics_request;
    argus::v1::QueryMetricsResponse metrics_response;
    grpc::ClientContext metrics_context;
    metrics_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->QueryMetrics(&metrics_context, metrics_request, &metrics_response).ok());
    ASSERT_TRUE(metrics_response.code().empty()) << metrics_response.error_message();
    EXPECT_FALSE(metrics_response.telemetry().accelerator_usage_supported());
    EXPECT_TRUE(std::isnan(metrics_response.telemetry().accelerator_usage_percent()));
    EXPECT_FALSE(metrics_response.telemetry().temperature_supported());
    EXPECT_TRUE(std::isnan(metrics_response.telemetry().temperature_celsius()));

    argus::v1::InstallPackageRequest install_request;
    install_request.set_package_path(ARGUS_FIXTURE_PACKAGE_DIR);
    argus::v1::InstallPackageResponse install_response;
    grpc::ClientContext install_context;
    install_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->InstallPackage(&install_context, install_request, &install_response).ok());
    EXPECT_TRUE(install_response.code().empty()) << install_response.error_message();
    EXPECT_EQ(install_response.algorithm_id(), "mock-detector");
    EXPECT_TRUE(std::filesystem::exists(package_dir + "/mock-detector/1.0.0/manifest.json"));

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* active_package = desired.add_active_package_versions();
    active_package->set_algorithm_id("mock-detector");
    active_package->set_version("1.0.0");
    auto* disabled_reference = desired.add_instances();
    disabled_reference->set_instance_id("disabled-reference");
    disabled_reference->set_camera_id("unused-camera");
    disabled_reference->set_algorithm_id("mock-detector");
    disabled_reference->set_algorithm_version("1.0.0");
    disabled_reference->set_enabled(false);
    argus::v1::ApplyDesiredStateResponse desired_response;
    ASSERT_TRUE(server.apply_desired_state(desired, &desired_response));
    ASSERT_TRUE(desired_response.code().empty()) << desired_response.error_message();

    argus::v1::UninstallPackageRequest uninstall_request;
    uninstall_request.set_algorithm_id("mock-detector");
    uninstall_request.set_version("1.0.0");
    argus::v1::UninstallPackageResponse uninstall_response;
    grpc::ClientContext uninstall_context;
    uninstall_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->UninstallPackage(&uninstall_context, uninstall_request, &uninstall_response).ok());
    EXPECT_TRUE(uninstall_response.code().empty()) << uninstall_response.error_message();
    EXPECT_FALSE(std::filesystem::exists(package_dir + "/mock-detector/1.0.0"));
    server.stop();
    ::unsetenv("ARGUS_PACKAGE_VALIDATOR");
    ::unsetenv("ARGUS_PACKAGE_DIR");
}

TEST(UdsServerTest, LoadsMockInstanceFromInstalledPackage) {
    const std::string package_dir = "var/instance-packages";
    std::filesystem::remove_all(package_dir);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_VALIDATOR", ARGUS_PACKAGE_VALIDATOR_PATH, 1), 0);
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::UdsServer server("/tmp/argus-test-instance.sock", adapter, backend);
    ASSERT_TRUE(server.start());

    auto channel = grpc::CreateChannel("unix:///tmp/argus-test-instance.sock",
                                       grpc::InsecureChannelCredentials());
    auto stub = argus::v1::EngineService::NewStub(channel);
    argus::v1::InstallPackageRequest install_request;
    install_request.set_package_path(ARGUS_FIXTURE_PACKAGE_DIR);
    argus::v1::InstallPackageResponse install_response;
    grpc::ClientContext install_context;
    install_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->InstallPackage(&install_context, install_request, &install_response).ok());
    ASSERT_TRUE(install_response.code().empty()) << install_response.error_message();

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("camera-1");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    auto* instance = desired.add_instances();
    instance->set_instance_id("instance-1");
    instance->set_camera_id("camera-1");
    instance->set_algorithm_id("mock-detector");
    instance->set_algorithm_version("1.0.0");
    instance->set_analysis_fps(1);
    instance->set_params_json("{}");
    instance->set_enabled(true);
    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    EXPECT_TRUE(response.code().empty()) << response.error_message();
    EXPECT_EQ(response.applied_revision(), 1);

    argus::v1::UpdateInstanceConfigRequest update_request;
    update_request.set_instance_id("instance-1");
    update_request.set_params_json("{}");
    argus::v1::UpdateInstanceConfigResponse update_response;
    grpc::ClientContext update_context;
    update_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->UpdateInstanceConfig(&update_context, update_request, &update_response).ok());
    EXPECT_TRUE(update_response.code().empty()) << update_response.error_message();

    argus::v1::UninstallPackageRequest protected_uninstall_request;
    protected_uninstall_request.set_algorithm_id("mock-detector");
    protected_uninstall_request.set_version("1.0.0");
    argus::v1::UninstallPackageResponse protected_uninstall_response;
    grpc::ClientContext protected_uninstall_context;
    protected_uninstall_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->UninstallPackage(&protected_uninstall_context, protected_uninstall_request,
                                       &protected_uninstall_response).ok());
    EXPECT_EQ(protected_uninstall_response.code(), "PACKAGE_IN_USE");

    argus::v1::DesiredState empty;
    empty.set_revision(2);
    argus::v1::ApplyDesiredStateResponse empty_response;
    ASSERT_TRUE(server.apply_desired_state(empty, &empty_response));
    EXPECT_TRUE(empty_response.code().empty()) << empty_response.error_message();

    argus::v1::UninstallPackageRequest uninstall_request;
    uninstall_request.set_algorithm_id("mock-detector");
    uninstall_request.set_version("1.0.0");
    argus::v1::UninstallPackageResponse uninstall_response;
    grpc::ClientContext uninstall_context;
    uninstall_context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->UninstallPackage(&uninstall_context, uninstall_request, &uninstall_response).ok());
    EXPECT_TRUE(uninstall_response.code().empty()) << uninstall_response.error_message();
    EXPECT_FALSE(std::filesystem::exists(package_dir + "/mock-detector/1.0.0"));
    server.stop();
    ::unsetenv("ARGUS_PACKAGE_VALIDATOR");
    ::unsetenv("ARGUS_PACKAGE_DIR");
}
#endif
#endif
