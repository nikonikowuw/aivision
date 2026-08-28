/**
 * @file test_uds_reconcile.cpp
 * @brief R9/R10 单测：QueryProfile 算力单位暴露、reconcile 失败 ERROR 异步回流、
 *        FPS 热更新触发实例重建与资源账本重算。
 */

#include <gtest/gtest.h>
#include "aivision/core/algo_manager.hpp"
#include "aivision/core/resource_ledger.hpp"
#include "aivision/core/task_scheduler.hpp"
#include "aivision/core/uds_ipc.hpp"
#include "aivision/platform/mock_platform.hpp"
#include "aivision/media/media_api.hpp"

#include <nlohmann/json.hpp>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <string>
#include <vector>

#ifndef AIVISION_FIXTURE_PACKAGE_DIR
#define AIVISION_FIXTURE_PACKAGE_DIR "tests/fixtures/packages/mock_pkg"
#endif

namespace {

class NoopSource final : public aivision::media::IMediaSource {
public:
    av_status start(const std::string&, aivision::media::PacketCallback,
                    aivision::media::StatusCallback) override {
        return AV_OK;
    }
    void stop() override {}
    bool is_connected() const override { return false; }
    aivision::media::ProbeOutcome probe(const std::string&, aivision::media::Transport,
                                        std::chrono::milliseconds) override {
        aivision::media::ProbeOutcome outcome;
        outcome.failure_code = "RTSP_MEDIA_ERROR";
        return outcome;
    }
};

class NoopBackend final : public aivision::media::IMediaBackend {
public:
    std::unique_ptr<aivision::media::IMediaSource> create_source(const std::string&) override {
        return std::make_unique<NoopSource>();
    }
};

// CaptureReportService 在 app.sock 上扮演 Go 侧 ReportService，
// 捕获 Engine 主动上报的 InstanceState 供断言（其余方法 fail-closed）。
class CaptureReportService final : public aivision::v1::ReportService::Service {
public:
    grpc::Status ReportInstanceState(grpc::ServerContext*,
                                     const aivision::v1::ReportInstanceStateRequest* request,
                                     aivision::v1::ReportInstanceStateResponse* response) override {
        if (!request || !request->has_instance_state()) {
            response->set_code("INVALID_ARG");
            return grpc::Status::OK;
        }
        std::lock_guard<std::mutex> lock(mutex_);
        states_.push_back(request->instance_state());
        return grpc::Status::OK; // code 留空 = 接受
    }
    grpc::Status ReportAlarm(grpc::ServerContext*, const aivision::v1::ReportAlarmRequest*,
                             aivision::v1::ReportAlarmResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportTaskState(grpc::ServerContext*, const aivision::v1::ReportTaskStateRequest*,
                                 aivision::v1::ReportTaskStateResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportMetrics(grpc::ServerContext*, const aivision::v1::ReportMetricsRequest*,
                               aivision::v1::ReportMetricsResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportOrphanImages(grpc::ServerContext*, const aivision::v1::ReportOrphanImagesRequest*,
                                    aivision::v1::ReportOrphanImagesResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }

    std::vector<aivision::v1::InstanceState> captured() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return states_;
    }

private:
    mutable std::mutex mutex_;
    std::vector<aivision::v1::InstanceState> states_;
};

// StubAppServer 承载 CaptureReportService 的 gRPC UDS 服务端。
class StubAppServer {
public:
    bool start(const std::string& socket_path) {
        ::unlink(socket_path.c_str());
        grpc::ServerBuilder builder;
        builder.AddListeningPort("unix://" + socket_path, grpc::InsecureServerCredentials());
        builder.RegisterService(&service_);
        server_ = builder.BuildAndStart();
        return server_ != nullptr;
    }
    void stop() {
        if (server_) {
            server_->Shutdown();
            server_->Wait();
            server_.reset();
        }
    }
    CaptureReportService& service() { return service_; }

private:
    CaptureReportService service_;
    std::unique_ptr<grpc::Server> server_;
};

aivision::v1::AlgorithmInstanceConfig* add_instance(aivision::v1::DesiredState* desired,
                                                    const std::string& instance_id,
                                                    const std::string& camera_id,
                                                    const std::string& algorithm_id,
                                                    const std::string& version,
                                                    int32_t analysis_fps) {
    auto* instance = desired->add_instances();
    instance->set_instance_id(instance_id);
    instance->set_camera_id(camera_id);
    instance->set_algorithm_id(algorithm_id);
    instance->set_algorithm_version(version);
    instance->set_analysis_fps(analysis_fps);
    instance->set_params_json("{}");
    instance->set_enabled(true);
    return instance;
}

} // namespace

#if !defined(AIVISION_SKIP_IPC_TESTS)

TEST(UdsReconcileTest, QueryProfileExposesComputeUnits) {
    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto& registry = aivision::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    aivision::core::UdsServer server("/tmp/aivision-test-profile.sock", adapter, nullptr);
    ASSERT_TRUE(server.start());

    auto channel = grpc::CreateChannel("unix:///tmp/aivision-test-profile.sock",
                                       grpc::InsecureChannelCredentials());
    auto stub = aivision::v1::EngineService::NewStub(channel);
    aivision::v1::QueryProfileRequest request;
    aivision::v1::QueryProfileResponse response;
    grpc::ClientContext context;
    context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(5));
    ASSERT_TRUE(stub->QueryProfile(&context, request, &response).ok());
    EXPECT_TRUE(response.code().empty()) << response.error_message();
    EXPECT_EQ(response.profile().total_compute_units(), 1000);
    EXPECT_EQ(response.profile().reserved_compute_units(), 100);
    server.stop();
}

TEST(UdsReconcileTest, ReportsErrorStateWhenInstanceReconcileFails) {
    const std::string package_dir = "var/error-report-packages";
    std::filesystem::remove_all(package_dir);
    ASSERT_EQ(::setenv("AIVISION_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    // 在 app.sock 上跑捕获服务，接收 Engine 的 InstanceState 主动上报。
    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/aivision-test-app-report.sock"));

    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = aivision::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    aivision::core::UdsServer server("/tmp/aivision-test-reconcile.sock", adapter, backend,
                                     "/tmp/aivision-test-app-report.sock");
    ASSERT_TRUE(server.start());

    // 任务项成功、实例项失败（算法包未安装 → PACKAGE_NOT_FOUND），触发整体回滚。
    aivision::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("fail-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "fail-instance", "fail-camera", "mock-detector", "1.0.0", 1);

    aivision::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    EXPECT_EQ(response.code(), "RECONCILE_FAILED");
    // 回滚块将原 OK 的任务项重标记为 RECONCILE_ROLLED_BACK，实例项保留原始失败码。
    ASSERT_GE(response.results_size(), 2);
    EXPECT_EQ(response.results(0).code(), "RECONCILE_ROLLED_BACK");
    EXPECT_EQ(response.results(1).code(), "PACKAGE_NOT_FOUND");
    // 运行时回滚到快照（空状态），失败实例未注册。
    EXPECT_EQ(aivision::core::AlgoManager::instance().get("fail-instance"), nullptr);
    EXPECT_EQ(aivision::core::TaskScheduler::instance().get_task("fail-camera"), nullptr);

    // R10a：失败实例必须收到 INSTANCE_STATUS_ERROR 上报，message 含稳定错误码。
    const auto states = app_server.service().captured();
    ASSERT_EQ(states.size(), 1);
    EXPECT_EQ(states[0].instance_id(), "fail-instance");
    EXPECT_EQ(states[0].status(), aivision::v1::INSTANCE_STATUS_ERROR);
    EXPECT_NE(states[0].message().find("PACKAGE_NOT_FOUND"), std::string::npos);

    server.stop();
    app_server.stop();
    ::unsetenv("AIVISION_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
}

TEST(UdsReconcileTest, RebuildsInstanceWhenAnalysisFpsChanges) {
    const std::string package_dir = "var/fps-rebuild-packages";
    std::filesystem::remove_all(package_dir);
    std::filesystem::create_directories(package_dir + "/mock-detector");
    std::error_code package_copy_error;
    std::filesystem::copy(
        AIVISION_FIXTURE_PACKAGE_DIR,
        package_dir + "/mock-detector/1.0.0",
        std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing,
        package_copy_error);
    ASSERT_FALSE(package_copy_error);
    // 扩写 fps_tiers：1fps→1 units、5fps→60 units、25fps→220 units，
    // 使 FPS 变更带来可观测的资源账本变化。
    const std::filesystem::path manifest_path = package_dir + "/mock-detector/1.0.0/manifest.json";
    nlohmann::json manifest;
    {
        std::ifstream input(manifest_path);
        ASSERT_TRUE(input.is_open());
        input >> manifest;
    }
    manifest["resource_profile"]["fps_tiers"] = nlohmann::json::array({
        {{"fps", 1}, {"units", 1}},
        {{"fps", 5}, {"units", 60}},
        {{"fps", 25}, {"units", 220}},
    });
    {
        std::ofstream output(manifest_path);
        ASSERT_TRUE(output.is_open());
        output << manifest.dump(2);
    }
    ASSERT_EQ(::setenv("AIVISION_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    auto adapter = std::make_shared<aivision::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = aivision::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    aivision::core::ResourceLedger::instance().clear();
    aivision::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    aivision::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    aivision::core::UdsServer server("/tmp/aivision-test-fps.sock", adapter, backend);
    ASSERT_TRUE(server.start());

    // revision 1：同一任务挂两个实例，均 1fps。
    aivision::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("fps-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "fps-instance-1", "fps-camera", "mock-detector", "1.0.0", 1);
    add_instance(&desired, "fps-instance-2", "fps-camera", "mock-detector", "1.0.0", 1);

    aivision::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    ASSERT_EQ(aivision::core::AlgoManager::instance().get("fps-instance-1")->get_target_fps(), 1);
    ASSERT_EQ(aivision::core::AlgoManager::instance().get("fps-instance-2")->get_target_fps(), 1);
    EXPECT_EQ(aivision::core::ResourceLedger::instance().get_used_compute_units(), 2);
    const auto instance_2_before = aivision::core::AlgoManager::instance().get("fps-instance-2");
    ASSERT_NE(instance_2_before, nullptr);

    // revision 2：同算法同版本仅修改 fps-instance-1 的 analysis_fps 1→5。
    aivision::v1::DesiredState updated;
    updated.set_revision(2);
    auto* updated_task = updated.add_tasks();
    updated_task->set_camera_id("fps-camera");
    updated_task->set_rtsp_url("rtsp://unused");
    updated_task->set_enabled(true);
    add_instance(&updated, "fps-instance-1", "fps-camera", "mock-detector", "1.0.0", 5);
    add_instance(&updated, "fps-instance-2", "fps-camera", "mock-detector", "1.0.0", 1);

    aivision::v1::ApplyDesiredStateResponse updated_response;
    ASSERT_TRUE(server.apply_desired_state(updated, &updated_response));
    ASSERT_TRUE(updated_response.code().empty()) << updated_response.error_message();

    // R10b：FPS 变更走重建路径——目标 FPS 生效、账本重算（60+1），
    // 同任务其他实例不被重建（指针与 FPS 均保持原值）。
    const auto instance_1_after = aivision::core::AlgoManager::instance().get("fps-instance-1");
    ASSERT_NE(instance_1_after, nullptr);
    EXPECT_EQ(instance_1_after->get_target_fps(), 5);
    EXPECT_EQ(aivision::core::ResourceLedger::instance().get_used_compute_units(), 61);
    const auto instance_2_after = aivision::core::AlgoManager::instance().get("fps-instance-2");
    ASSERT_NE(instance_2_after, nullptr);
    EXPECT_EQ(instance_2_after, instance_2_before);
    EXPECT_EQ(instance_2_after->get_target_fps(), 1);

    // 清空期望状态，释放全部运行时资源。
    aivision::v1::DesiredState empty;
    empty.set_revision(3);
    aivision::v1::ApplyDesiredStateResponse empty_response;
    ASSERT_TRUE(server.apply_desired_state(empty, &empty_response));
    ASSERT_TRUE(empty_response.code().empty()) << empty_response.error_message();
    EXPECT_EQ(aivision::core::ResourceLedger::instance().get_used_compute_units(), 0);

    server.stop();
    aivision::core::ResourceLedger::instance().clear();
    ::unsetenv("AIVISION_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
}

#endif // !defined(AIVISION_SKIP_IPC_TESTS)
