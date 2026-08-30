/**
 * @file test_uds_reconcile.cpp
 * @brief R9/R10 单测：QueryProfile 算力单位暴露、reconcile 失败 ERROR 异步回流、
 *        FPS 热更新触发实例重建与资源账本重算。
 */

#include <gtest/gtest.h>
#include "argus/core/algo_manager.hpp"
#include "argus/core/resource_ledger.hpp"
#include "argus/core/task_scheduler.hpp"
#include "argus/core/uds_ipc.hpp"
#include "argus/platform/mock_platform.hpp"
#include "argus/platform/macos_platform.hpp"
#include "argus/media/media_api.hpp"

#include <nlohmann/json.hpp>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <string>
#include <vector>

#ifndef ARGUS_FIXTURE_PACKAGE_DIR
#define ARGUS_FIXTURE_PACKAGE_DIR "tests/fixtures/packages/mock_pkg"
#endif

namespace {

class NoopSource final : public argus::media::IMediaSource {
public:
    av_status start(const std::string&, argus::media::PacketCallback,
                    argus::media::StatusCallback) override {
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
};

class NoopBackend final : public argus::media::IMediaBackend {
public:
    std::unique_ptr<argus::media::IMediaSource> create_source(const std::string&) override {
        return std::make_unique<NoopSource>();
    }
};

// CaptureReportService 在 app.sock 上扮演 Go 侧 ReportService，
// 捕获 Engine 主动上报的 InstanceState 供断言（其余方法 fail-closed）。
class CaptureReportService final : public argus::v1::ReportService::Service {
public:
    grpc::Status ReportInstanceState(grpc::ServerContext*,
                                     const argus::v1::ReportInstanceStateRequest* request,
                                     argus::v1::ReportInstanceStateResponse* response) override {
        if (!request || !request->has_instance_state()) {
            response->set_code("INVALID_ARG");
            return grpc::Status::OK;
        }
        std::lock_guard<std::mutex> lock(mutex_);
        states_.push_back(request->instance_state());
        return grpc::Status::OK; // code 留空 = 接受
    }
    grpc::Status ReportAlarm(grpc::ServerContext*, const argus::v1::ReportAlarmRequest*,
                             argus::v1::ReportAlarmResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportTaskState(grpc::ServerContext*, const argus::v1::ReportTaskStateRequest*,
                                 argus::v1::ReportTaskStateResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportMetrics(grpc::ServerContext*, const argus::v1::ReportMetricsRequest*,
                               argus::v1::ReportMetricsResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }
    grpc::Status ReportOrphanImages(grpc::ServerContext*, const argus::v1::ReportOrphanImagesRequest*,
                                    argus::v1::ReportOrphanImagesResponse*) override {
        return grpc::Status(grpc::StatusCode::UNIMPLEMENTED, "not used in this test");
    }

    std::vector<argus::v1::InstanceState> captured() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return states_;
    }

private:
    mutable std::mutex mutex_;
    std::vector<argus::v1::InstanceState> states_;
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

#if !defined(ARGUS_SKIP_IPC_TESTS)
argus::v1::AlgorithmInstanceConfig* add_instance(argus::v1::DesiredState* desired,
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
#endif

} // namespace

#if !defined(ARGUS_SKIP_IPC_TESTS)

TEST(UdsReconcileTest, QueryProfileExposesComputeUnits) {
    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::UdsServer server("/tmp/argus-test-profile.sock", adapter, nullptr);
    ASSERT_TRUE(server.start());

    auto channel = grpc::CreateChannel("unix:///tmp/argus-test-profile.sock",
                                       grpc::InsecureChannelCredentials());
    auto stub = argus::v1::EngineService::NewStub(channel);
    argus::v1::QueryProfileRequest request;
    argus::v1::QueryProfileResponse response;
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
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    // 在 app.sock 上跑捕获服务，接收 Engine 的 InstanceState 主动上报。
    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-report.sock"));

    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::UdsServer server("/tmp/argus-test-reconcile.sock", adapter, backend,
                                     "/tmp/argus-test-app-report.sock");
    ASSERT_TRUE(server.start());

    // 任务项成功、实例项失败（算法包未安装 → PACKAGE_NOT_FOUND），触发整体回滚。
    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("fail-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "fail-instance", "fail-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    EXPECT_EQ(response.code(), "RECONCILE_FAILED");
    // 回滚块将原 OK 的任务项重标记为 RECONCILE_ROLLED_BACK，实例项保留原始失败码。
    ASSERT_GE(response.results_size(), 2);
    EXPECT_EQ(response.results(0).code(), "RECONCILE_ROLLED_BACK");
    EXPECT_EQ(response.results(1).code(), "PACKAGE_NOT_FOUND");
    // 运行时回滚到快照（空状态），失败实例未注册。
    EXPECT_EQ(argus::core::AlgoManager::instance().get("fail-instance"), nullptr);
    EXPECT_EQ(argus::core::TaskScheduler::instance().get_task("fail-camera"), nullptr);

    // R10a：失败实例必须收到 INSTANCE_STATUS_ERROR 上报，message 含稳定错误码。
    const auto states = app_server.service().captured();
    ASSERT_EQ(states.size(), 1);
    EXPECT_EQ(states[0].instance_id(), "fail-instance");
    EXPECT_EQ(states[0].status(), argus::v1::INSTANCE_STATUS_ERROR);
    EXPECT_NE(states[0].message().find("PACKAGE_NOT_FOUND"), std::string::npos);

    server.stop();
    app_server.stop();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
}

TEST(UdsReconcileTest, RebuildsInstanceWhenAnalysisFpsChanges) {
    const std::string package_dir = "var/fps-rebuild-packages";
    std::filesystem::remove_all(package_dir);
    std::filesystem::create_directories(package_dir + "/mock-detector");
    std::error_code package_copy_error;
    std::filesystem::copy(
        ARGUS_FIXTURE_PACKAGE_DIR,
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
    ASSERT_EQ(::setenv("ARGUS_PACKAGE_DIR", package_dir.c_str(), 1), 0);

    auto adapter = std::make_shared<argus::platform::MockPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("mock", adapter);
    registry.set_active_platform("mock");
    argus::core::ResourceLedger::instance().clear();
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::UdsServer server("/tmp/argus-test-fps.sock", adapter, backend);
    ASSERT_TRUE(server.start());

    // revision 1：同一任务挂两个实例，均 1fps。
    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("fps-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "fps-instance-1", "fps-camera", "mock-detector", "1.0.0", 1);
    add_instance(&desired, "fps-instance-2", "fps-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    ASSERT_EQ(argus::core::AlgoManager::instance().get("fps-instance-1")->get_target_fps(), 1);
    ASSERT_EQ(argus::core::AlgoManager::instance().get("fps-instance-2")->get_target_fps(), 1);
    EXPECT_EQ(argus::core::ResourceLedger::instance().get_used_compute_units(), 2);
    const auto instance_2_before = argus::core::AlgoManager::instance().get("fps-instance-2");
    ASSERT_NE(instance_2_before, nullptr);

    // revision 2：同算法同版本仅修改 fps-instance-1 的 analysis_fps 1→5。
    argus::v1::DesiredState updated;
    updated.set_revision(2);
    auto* updated_task = updated.add_tasks();
    updated_task->set_camera_id("fps-camera");
    updated_task->set_rtsp_url("rtsp://unused");
    updated_task->set_enabled(true);
    add_instance(&updated, "fps-instance-1", "fps-camera", "mock-detector", "1.0.0", 5);
    add_instance(&updated, "fps-instance-2", "fps-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse updated_response;
    ASSERT_TRUE(server.apply_desired_state(updated, &updated_response));
    ASSERT_TRUE(updated_response.code().empty()) << updated_response.error_message();

    // R10b：FPS 变更走重建路径——目标 FPS 生效、账本重算（60+1），
    // 同任务其他实例不被重建（指针与 FPS 均保持原值）。
    const auto instance_1_after = argus::core::AlgoManager::instance().get("fps-instance-1");
    ASSERT_NE(instance_1_after, nullptr);
    EXPECT_EQ(instance_1_after->get_target_fps(), 5);
    EXPECT_EQ(argus::core::ResourceLedger::instance().get_used_compute_units(), 61);
    const auto instance_2_after = argus::core::AlgoManager::instance().get("fps-instance-2");
    ASSERT_NE(instance_2_after, nullptr);
    EXPECT_EQ(instance_2_after, instance_2_before);
    EXPECT_EQ(instance_2_after->get_target_fps(), 1);

    // 清空期望状态，释放全部运行时资源。
    argus::v1::DesiredState empty;
    empty.set_revision(3);
    argus::v1::ApplyDesiredStateResponse empty_response;
    ASSERT_TRUE(server.apply_desired_state(empty, &empty_response));
    ASSERT_TRUE(empty_response.code().empty()) << empty_response.error_message();
    EXPECT_EQ(argus::core::ResourceLedger::instance().get_used_compute_units(), 0);

    server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
}

TEST(UdsReconcileTest, ReconcilesExactDetectionRuleFromUI) {
    const std::string pkg_dir = "/tmp/argus-test-pkg-roi";
    std::filesystem::remove_all(pkg_dir);
    std::filesystem::create_directories(pkg_dir + "/yolov8n");
    std::filesystem::copy("/Users/niko/dev/go/argus/engine/var/packages/yolov8n", pkg_dir + "/yolov8n",
                          std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing);
    ::setenv("ARGUS_PACKAGE_DIR", pkg_dir.c_str(), 1);
    auto adapter = std::make_shared<argus::platform::MacosPlatformAdapter>();
    auto backend = std::make_shared<NoopBackend>();
    auto& registry = argus::platform::PlatformRegistry::instance();
    registry.register_adapter("macos-arm64-coreml", adapter);
    registry.set_active_platform("macos-arm64-coreml");
    argus::core::UdsServer server("/tmp/argus-test-roi-reconcile.sock", adapter, backend, "");
    ASSERT_TRUE(server.start());

    // 1. 初始状态：无规则
    argus::v1::DesiredState d1;
    d1.set_revision(1);
    auto* t1 = d1.add_tasks();
    t1->set_camera_id("cam-1");
    t1->set_rtsp_url("rtsp://unused");
    t1->set_enabled(true);

    auto* i1 = d1.add_instances();
    i1->set_instance_id("inst-1");
    i1->set_camera_id("cam-1");
    i1->set_algorithm_id("yolov8n");
    i1->set_algorithm_version("1.0.0");
    i1->set_analysis_fps(25);
    i1->set_params_json("{\"confidence_threshold\":0.5,\"iou_threshold\":0.45}");
    i1->set_enabled(true);

    auto* p1 = d1.add_active_package_versions();
    p1->set_algorithm_id("yolov8n");
    p1->set_version("1.0.0");

    argus::v1::ApplyDesiredStateResponse r1;
    ASSERT_TRUE(server.apply_desired_state(d1, &r1));
    ASSERT_TRUE(r1.code().empty()) << r1.error_message();

    auto inst = argus::core::AlgoManager::instance().get("inst-1");
    ASSERT_NE(inst, nullptr);
    EXPECT_TRUE(inst->is_running());

    // 2. 更新状态：带 ROI 规则
    argus::v1::DesiredState d2;
    d2.set_revision(2);
    auto* t2 = d2.add_tasks();
    t2->CopyFrom(*t1);

    auto* i2 = d2.add_instances();
    i2->CopyFrom(*i1);
    auto* r = i2->add_rules();
    r->set_role(argus::v1::DETECTION_RULE_ROLE_ROI);
    r->set_line_direction(argus::v1::DETECTION_LINE_DIRECTION_BOTH);
    auto* pt0 = r->add_points(); pt0->set_x(0.069785276f); pt0->set_y(0.342886386f);
    auto* pt1 = r->add_points(); pt1->set_x(0.068634969f); pt1->set_y(0.903787103f);
    auto* pt2 = r->add_points(); pt2->set_x(0.891104294f); pt2->set_y(0.825997952f);
    auto* pt3 = r->add_points(); pt3->set_x(0.856595092f); pt3->set_y(0.089048106f);

    auto* p2 = d2.add_active_package_versions();
    p2->CopyFrom(*p1);

    argus::v1::ApplyDesiredStateResponse r2;
    ASSERT_TRUE(server.apply_desired_state(d2, &r2));
    EXPECT_TRUE(r2.code().empty()) << r2.error_message();

    inst = argus::core::AlgoManager::instance().get("inst-1");
    ASSERT_NE(inst, nullptr);
    EXPECT_TRUE(inst->is_running());

    server.stop();
    argus::core::ResourceLedger::instance().clear();
    std::filesystem::remove_all(pkg_dir);
}

#endif // !defined(ARGUS_SKIP_IPC_TESTS)
