/**
 * @file test_uds_reconcile.cpp
 * @brief R9/R10 单测：QueryProfile 算力单位暴露、reconcile 失败 ERROR 异步回流、
 *        FPS 热更新触发实例重建与资源账本重算。
 */

#include <gtest/gtest.h>
#include "argus/core/algo_manager.hpp"
#include "argus/core/frame_pool.hpp"
#include "argus/core/image_manager.hpp"
#include "argus/core/resource_ledger.hpp"
#include "argus/core/task_scheduler.hpp"
#include "argus/core/uds_ipc.hpp"
#include "argus/platform/mock_platform.hpp"
#include "argus/platform/macos_platform.hpp"
#include "argus/media/media_api.hpp"

#include <nlohmann/json.hpp>
#include <chrono>
#include <condition_variable>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#ifndef ARGUS_FIXTURE_PACKAGE_DIR
#define ARGUS_FIXTURE_PACKAGE_DIR "tests/fixtures/packages/mock_pkg"
#endif

#ifndef ARGUS_LPR_FIXTURE_PACKAGE_DIR
#define ARGUS_LPR_FIXTURE_PACKAGE_DIR "tests/fixtures/packages/mock_lpr_pkg"
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
// 捕获 Engine 主动上报的 InstanceState 和 AlarmEvent 供断言（其余方法 fail-closed）。
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
    grpc::Status ReportAlarm(grpc::ServerContext*, const argus::v1::ReportAlarmRequest* request,
                             argus::v1::ReportAlarmResponse* response) override {
        if (!request || !request->has_alarm()) {
            response->set_code("INVALID_ARG");
            return grpc::Status::OK;
        }
        std::unique_lock<std::mutex> lock(mutex_);
        alarms_.push_back(request->alarm());
        alarm_started_ = true;
        alarm_cv_.notify_all();
        alarm_cv_.wait(lock, [this] { return !block_alarm_ || release_alarm_; });
        return grpc::Status::OK; // code 留空 = 接受
    }
    grpc::Status ReportPlateObservation(grpc::ServerContext*,
                                        const argus::v1::ReportPlateObservationRequest* request,
                                        argus::v1::ReportPlateObservationResponse* response) override {
        if (!request || !request->has_observation()) {
            response->set_code("INVALID_ARG");
            return grpc::Status::OK;
        }
        std::unique_lock<std::mutex> lock(mutex_);
        ++plate_report_count_;
        if (fail_next_plate_) {
            fail_next_plate_ = false;
            response->set_code("TEMPORARY_FAILURE");
            return grpc::Status::OK;
        }
        observations_.push_back(request->observation());
        plate_cv_.notify_all();
        return grpc::Status::OK; // code 留空 = 接受
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

    void block_alarm() {
        std::lock_guard<std::mutex> lock(mutex_);
        block_alarm_ = true;
        release_alarm_ = false;
        alarm_started_ = false;
    }

    bool wait_for_alarm(std::chrono::milliseconds timeout) {
        std::unique_lock<std::mutex> lock(mutex_);
        return alarm_cv_.wait_for(lock, timeout, [this] { return alarm_started_; });
    }

    void release_alarm() {
        std::lock_guard<std::mutex> lock(mutex_);
        release_alarm_ = true;
        block_alarm_ = false;
        alarm_cv_.notify_all();
    }

    std::vector<argus::v1::AlarmEvent> alarms() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return alarms_;
    }

    std::vector<argus::v1::PlateObservation> observations() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return observations_;
    }

    void fail_next_plate_observation() {
        std::lock_guard<std::mutex> lock(mutex_);
        fail_next_plate_ = true;
    }

    size_t plate_report_count() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return plate_report_count_;
    }

    bool wait_for_observation(std::chrono::milliseconds timeout) {
        std::unique_lock<std::mutex> lock(mutex_);
        return plate_cv_.wait_for(lock, timeout, [this] { return !observations_.empty(); });
    }

    std::vector<argus::v1::InstanceState> captured() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return states_;
    }

private:
    mutable std::mutex mutex_;
    std::condition_variable alarm_cv_;
    std::condition_variable plate_cv_;
    std::vector<argus::v1::InstanceState> states_;
    std::vector<argus::v1::AlarmEvent> alarms_;
    std::vector<argus::v1::PlateObservation> observations_;
    size_t plate_report_count_ = 0;
    bool fail_next_plate_ = false;
    bool block_alarm_ = false;
    bool release_alarm_ = false;
    bool alarm_started_ = false;
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

TEST(UdsReconcileTest, AlarmCaptureRunsOffAlgorithmWorker) {
    const std::string package_dir = "var/async-alarm-packages";
    const std::string image_dir = "build/async-alarm-images";
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
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
    argus::core::ResourceLedger::instance().clear();
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::ImageManager::instance().init(
        image_dir, std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-alarm.sock"));
    app_server.service().block_alarm();
    argus::core::UdsServer server("/tmp/argus-test-alarm.sock", adapter, backend,
                                  "/tmp/argus-test-app-alarm.sock");
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("alarm-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "alarm-instance", "alarm-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    const auto instance = argus::core::AlgoManager::instance().get("alarm-instance");
    ASSERT_NE(instance, nullptr);

    auto& pool = argus::core::FramePool::instance();
    for (uint64_t frame_id = 1; frame_id <= 2; ++frame_id) {
        const uint64_t source_frame_id = frame_id == 1 ? 999 : 2;
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = source_frame_id;
        frame->wall_time_ns = static_cast<int64_t>(source_frame_id) * 1'000'000'000;
        frame->pts_ns = static_cast<int64_t>(source_frame_id) * 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
        if (frame_id == 1) {
            ASSERT_TRUE(app_server.service().wait_for_alarm(std::chrono::seconds(2)));
        }
    }

    {
        // 相同批次 ID 的重复回调不应再次生成四条目标告警。
        av_frame_desc* duplicate = pool.acquire_frame();
        ASSERT_NE(duplicate, nullptr);
        duplicate->frame_id = 999;
        duplicate->wall_time_ns = 99'000'000'000;
        duplicate->pts_ns = 99'000'000'000;
        duplicate->memory_type = AV_MEM_HOST;
        duplicate->pixel_format = AV_PIX_NV12;
        duplicate->width = 1920;
        duplicate->height = 1080;
        instance->push_frame(*duplicate);
        EXPECT_EQ(pool.release_frame(duplicate->frame_token), AV_OK);
    }

    const auto alarms = app_server.service().alarms();
    // 批次的第一条目标事件已进入阻塞 RPC，其余三个目标事件仍在同一 pending 中。
    ASSERT_EQ(alarms.size(), 1);
    ASSERT_EQ(alarms[0].objects_size(), 1);
    EXPECT_TRUE(alarms[0].event_id().ends_with("/mock-event-999-1"));
    EXPECT_FALSE(alarms[0].image_id().empty());
    EXPECT_TRUE(std::filesystem::exists(image_dir + "/" + alarms[0].image_rel_path()));
    EXPECT_GE(pool.active_frame_count(), 1U);

    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < 2; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    // ReportAlarm 仍被阻塞时，第二帧也应已完成 instance_process。
    EXPECT_GE(instance->get_processed_frames(), 2U);

    app_server.service().release_alarm();
    for (int attempt = 0; attempt < 400 && (pool.active_frame_count() != 0 || app_server.service().alarms().size() < 5U); ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(pool.active_frame_count(), 0U);
    const auto completed_alarms = app_server.service().alarms();
    ASSERT_EQ(completed_alarms.size(), 5U);
    ASSERT_EQ(completed_alarms[0].objects_size(), 1);
    const std::string shared_image_id = completed_alarms[0].image_id();
    ASSERT_FALSE(shared_image_id.empty());
    for (int index = 0; index < 4; ++index) {
        ASSERT_EQ(completed_alarms[static_cast<size_t>(index)].objects_size(), 1);
        EXPECT_EQ(completed_alarms[static_cast<size_t>(index)].image_id(), shared_image_id);
        EXPECT_EQ(completed_alarms[static_cast<size_t>(index)].image_rel_path(), completed_alarms[0].image_rel_path());
        EXPECT_TRUE(completed_alarms[static_cast<size_t>(index)].event_id().ends_with(
            "/mock-event-999-" + std::to_string(index + 1)));
        for (int other = index + 1; other < 4; ++other) {
            EXPECT_NE(completed_alarms[static_cast<size_t>(index)].event_id(),
                      completed_alarms[static_cast<size_t>(other)].event_id());
        }
    }
    EXPECT_TRUE(completed_alarms[4].event_id().ends_with("/mock-event-2-1"));

    server.stop();
    app_server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
}

TEST(UdsReconcileTest, AlarmQueueDropsOldestWhenFullAndReleasesFrames) {
    const std::string package_dir = "var/async-alarm-drop-packages";
    const std::string image_dir = "build/async-alarm-drop-images";
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
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
    argus::core::ResourceLedger::instance().clear();
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::ImageManager::instance().init(
        image_dir, std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-drop.sock"));
    app_server.service().block_alarm();
    argus::core::UdsServer server("/tmp/argus-test-drop.sock", adapter, backend,
                                  "/tmp/argus-test-app-drop.sock");
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("drop-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "drop-instance", "drop-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    const auto instance = argus::core::AlgoManager::instance().get("drop-instance");
    ASSERT_NE(instance, nullptr);

    auto& pool = argus::core::FramePool::instance();

    // 1. 发送第 1 帧，让 worker 进入 ReportAlarm 并被阻塞
    {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = 1;
        frame->wall_time_ns = 1'000'000'000;
        frame->pts_ns = 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
        ASSERT_TRUE(app_server.service().wait_for_alarm(std::chrono::seconds(2)));
    }

    // 2. 推入 257 帧（frame_id 2 ~ 258），队列上限为 256，必定触发最旧帧丢弃与释放
    constexpr size_t kTotalFrames = 258;
    for (uint64_t frame_id = 2; frame_id <= kTotalFrames; ++frame_id) {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = frame_id;
        frame->wall_time_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->pts_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
        // 等待算法实例消费该帧，避免塞满长度为 5 的算法内部输入队列
        for (int attempt = 0; attempt < 200 && instance->get_processed_frames() < frame_id; ++attempt) {
            std::this_thread::sleep_for(std::chrono::microseconds(100));
        }
    }

    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < kTotalFrames; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(instance->get_processed_frames(), kTotalFrames);

    // 释放阻塞的 ReportAlarm，让队列消费完毕
    app_server.service().release_alarm();
    for (int attempt = 0; attempt < 400 && pool.active_frame_count() != 0; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(10));
    }
    EXPECT_EQ(pool.active_frame_count(), 0U);

    const auto alarms = app_server.service().alarms();
    // 总共收到 1（第1帧） + 256（第3~258帧） = 257 条告警，第 2 帧被 drop
    EXPECT_EQ(alarms.size(), 257U);
    for (const auto& alarm : alarms) {
        EXPECT_FALSE(alarm.event_id().ends_with("/mock-event-2-1"));
    }

    server.stop();
    app_server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
}

TEST(UdsReconcileTest, AlarmQueueDiscardsOnStopReleasesFrames) {
    const std::string package_dir = "var/async-alarm-stop-packages";
    const std::string image_dir = "build/async-alarm-stop-images";
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
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
    argus::core::ResourceLedger::instance().clear();
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::ImageManager::instance().init(
        image_dir, std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-stop.sock"));
    app_server.service().block_alarm();
    argus::core::UdsServer server("/tmp/argus-test-stop.sock", adapter, backend,
                                  "/tmp/argus-test-app-stop.sock");
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("stop-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "stop-instance", "stop-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    const auto instance = argus::core::AlgoManager::instance().get("stop-instance");
    ASSERT_NE(instance, nullptr);

    auto& pool = argus::core::FramePool::instance();

    // 1. 发送第 1 帧阻塞 worker
    {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = 1;
        frame->wall_time_ns = 1'000'000'000;
        frame->pts_ns = 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
        ASSERT_TRUE(app_server.service().wait_for_alarm(std::chrono::seconds(2)));
    }

    // 2. 再推入 5 帧，排队在 capture_queue_ 中
    for (uint64_t frame_id = 2; frame_id <= 6; ++frame_id) {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = frame_id;
        frame->wall_time_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->pts_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
    }

    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < 6; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    EXPECT_EQ(instance->get_processed_frames(), 6U);
    EXPECT_GE(pool.active_frame_count(), 5U);

    // 3. 解除阻塞，同时调用 server.stop() 触发停机清理
    app_server.service().release_alarm();
    server.stop();

    // 停机完成后，残留队列中的帧引用必须被全部释放
    EXPECT_EQ(pool.active_frame_count(), 0U);

    app_server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
}

TEST(UdsReconcileTest, AlarmReportedEvenWhenFrameRetainFails) {
    const std::string package_dir = "var/async-alarm-retainfail-packages";
    const std::string image_dir = "build/async-alarm-retainfail-images";
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
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
    argus::core::ResourceLedger::instance().clear();
    argus::core::ResourceLedger::instance().set_limits(1000, 100, 0);
    argus::core::ResourceLedger::instance().set_free_memory_provider([] {
        return uint64_t{2} * 1024 * 1024 * 1024;
    });
    argus::core::ImageManager::instance().init(
        image_dir, std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-retainfail.sock"));
    argus::core::UdsServer server("/tmp/argus-test-retainfail.sock", adapter, backend,
                                  "/tmp/argus-test-app-retainfail.sock");
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("retainfail-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "retainfail-instance", "retainfail-camera", "mock-detector", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    const auto instance = argus::core::AlgoManager::instance().get("retainfail-instance");
    ASSERT_NE(instance, nullptr);

    // 从 FramePool 申请合法帧，mock_algo 会在 frame_id == 42 时提前释放 token，
    // 导致 on_result_bridge 处 retain 失败，测试告警元数据降级递送
    auto& pool = argus::core::FramePool::instance();
    av_frame_desc* frame = pool.acquire_frame();
    ASSERT_NE(frame, nullptr);
    frame->frame_id = 42;
    frame->wall_time_ns = 1'000'000'000;
    frame->pts_ns = 1'000'000'000;
    frame->memory_type = AV_MEM_HOST;
    frame->pixel_format = AV_PIX_NV12;
    frame->width = 1920;
    frame->height = 1080;

    instance->push_frame(*frame);
    EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);

    // 等待告警上报到 StubAppServer
    ASSERT_TRUE(app_server.service().wait_for_alarm(std::chrono::seconds(2)));

    const auto alarms = app_server.service().alarms();
    ASSERT_EQ(alarms.size(), 1U);
    // 告警元数据正常上报，但因为 retain 失败，抓拍图像为空
    EXPECT_TRUE(alarms[0].image_id().empty());
    EXPECT_TRUE(alarms[0].image_rel_path().empty());
    EXPECT_TRUE(alarms[0].event_id().ends_with("/mock-event-42-1"));

    server.stop();
    app_server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
}

TEST(UdsReconcileTest, ReconcilesExactDetectionRuleFromUI) {
    const std::string pkg_dir = "/tmp/argus-test-pkg-roi";
    std::filesystem::remove_all(pkg_dir);
    std::filesystem::create_directories(pkg_dir + "/mock-detector");
    std::error_code package_copy_error;
    std::filesystem::copy(
        ARGUS_FIXTURE_PACKAGE_DIR,
        pkg_dir + "/mock-detector/1.0.0",
        std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing,
        package_copy_error);
    ASSERT_FALSE(package_copy_error);
    const std::filesystem::path manifest_path = pkg_dir + "/mock-detector/1.0.0/manifest.json";
    nlohmann::json manifest;
    {
        std::ifstream input(manifest_path);
        ASSERT_TRUE(input.is_open());
        input >> manifest;
    }
    manifest["resource_profile"]["fps_tiers"] = nlohmann::json::array({
        {{"fps", 25}, {"units", 220}},
    });
    {
        std::ofstream output(manifest_path);
        ASSERT_TRUE(output.is_open());
        output << manifest.dump(2);
    }
    ::setenv("ARGUS_PACKAGE_DIR", pkg_dir.c_str(), 1);
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
    i1->set_algorithm_id("mock-detector");
    i1->set_algorithm_version("1.0.0");
    i1->set_analysis_fps(25);
    i1->set_params_json("{\"confidence_threshold\":0.5,\"iou_threshold\":0.45}");
    i1->set_enabled(true);

    auto* p1 = d1.add_active_package_versions();
    p1->set_algorithm_id("mock-detector");
    p1->set_version("1.0.0");

    argus::v1::ApplyDesiredStateResponse r1;
    ASSERT_TRUE(server.apply_desired_state(d1, &r1));
    for (int i = 0; i < r1.results_size(); ++i) {
        const auto& item = r1.results(i);
        EXPECT_EQ(item.status(), argus::v1::RECONCILE_ITEM_STATUS_OK)
            << item.id() << " failed: " << item.code() << " (" << item.error_message() << ")";
    }
    ASSERT_TRUE(r1.code().empty()) << r1.code() << ": " << r1.error_message();

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

TEST(UdsReconcileTest, LicensePlateObservationCaptureValidationAndRetry) {
    const std::string package_dir = "var/lpr-observation-packages";
    const std::string image_dir = "build/lpr-observation-images";
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
    std::filesystem::create_directories(package_dir + "/mock-lpr");
    std::error_code package_copy_error;
    std::filesystem::copy(
        ARGUS_LPR_FIXTURE_PACKAGE_DIR,
        package_dir + "/mock-lpr/1.0.0",
        std::filesystem::copy_options::recursive | std::filesystem::copy_options::overwrite_existing,
        package_copy_error);
    ASSERT_FALSE(package_copy_error);
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
    argus::core::ImageManager::instance().init(
        image_dir, std::shared_ptr<argus::platform::IImageProcessor>(adapter, adapter->get_image_processor()));

    StubAppServer app_server;
    ASSERT_TRUE(app_server.start("/tmp/argus-test-app-lpr.sock"));
    argus::core::UdsServer server("/tmp/argus-test-lpr.sock", adapter, backend,
                                  "/tmp/argus-test-app-lpr.sock");
    ASSERT_TRUE(server.start());

    argus::v1::DesiredState desired;
    desired.set_revision(1);
    auto* task = desired.add_tasks();
    task->set_camera_id("lpr-camera");
    task->set_rtsp_url("rtsp://unused");
    task->set_enabled(true);
    add_instance(&desired, "lpr-instance", "lpr-camera", "mock-lpr", "1.0.0", 1);

    argus::v1::ApplyDesiredStateResponse response;
    ASSERT_TRUE(server.apply_desired_state(desired, &response));
    ASSERT_TRUE(response.code().empty()) << response.error_message();
    const auto instance = argus::core::AlgoManager::instance().get("lpr-instance");
    ASSERT_NE(instance, nullptr);

    app_server.service().fail_next_plate_observation();
    auto& pool = argus::core::FramePool::instance();
    auto push_frame = [&](uint64_t frame_id) {
        av_frame_desc* frame = pool.acquire_frame();
        ASSERT_NE(frame, nullptr);
        frame->frame_id = frame_id;
        frame->opaque = reinterpret_cast<void*>(0x1);
        frame->wall_time_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->pts_ns = static_cast<int64_t>(frame_id) * 1'000'000'000;
        frame->memory_type = AV_MEM_HOST;
        frame->pixel_format = AV_PIX_NV12;
        frame->width = 1920;
        frame->height = 1080;
        instance->push_frame(*frame);
        EXPECT_EQ(pool.release_frame(frame->frame_token), AV_OK);
    };

    // 首次上报失败后，worker 应复用已落盘的图片引用完成一次重试。
    push_frame(1);
    ASSERT_TRUE(app_server.service().wait_for_observation(std::chrono::seconds(3)));
    const auto observations = app_server.service().observations();
    ASSERT_EQ(observations.size(), 1U);
    ASSERT_EQ(app_server.service().plate_report_count(), 2U);
    EXPECT_EQ(observations[0].plate_text(), "A12345");
    EXPECT_EQ(observations[0].algorithm_id(), "mock-lpr");
    EXPECT_FALSE(observations[0].image_id().empty());
    EXPECT_FALSE(observations[0].plate_image_id().empty());
    EXPECT_TRUE(std::filesystem::exists(image_dir + "/" + observations[0].image_rel_path()));
    EXPECT_TRUE(std::filesystem::exists(image_dir + "/" + observations[0].plate_image_rel_path()));

    // 同一 event_id 再次回调时只允许第一次进入上报队列。
    push_frame(2);
    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < 2; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
    EXPECT_EQ(app_server.service().observations().size(), 1U);
    EXPECT_EQ(app_server.service().plate_report_count(), 2U);

    const auto unreported_before_invalid = argus::core::ImageManager::instance().list_unreported_images();
    EXPECT_TRUE(unreported_before_invalid.empty());

    // 算法提交缺失 images 的识别结果时，Engine 必须在抓拍和 IPC 之前拒绝。
    push_frame(99);
    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < 3; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
    EXPECT_EQ(app_server.service().observations().size(), 1U);
    EXPECT_EQ(app_server.service().plate_report_count(), 2U);
    EXPECT_TRUE(argus::core::ImageManager::instance().list_unreported_images().empty());

    // 超过 Go plate_observations VARCHAR(32) 约束的文本也必须在副作用前拒绝。
    push_frame(100);
    for (int attempt = 0; attempt < 400 && instance->get_processed_frames() < 4; ++attempt) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
    EXPECT_EQ(app_server.service().observations().size(), 1U);
    EXPECT_EQ(app_server.service().plate_report_count(), 2U);
    EXPECT_TRUE(argus::core::ImageManager::instance().list_unreported_images().empty());

    server.stop();
    app_server.stop();
    argus::core::ResourceLedger::instance().clear();
    ::unsetenv("ARGUS_PACKAGE_DIR");
    std::filesystem::remove_all(package_dir);
    std::filesystem::remove_all(image_dir);
}

TEST(UdsReconcileTest, PlateObservationReportAndClient) {
    const std::string app_sock = "/tmp/argus-test-plate-client-app.sock";
    StubAppServer app_server;
    ASSERT_TRUE(app_server.start(app_sock));

    argus::core::UdsClient client(app_sock);

    argus::v1::PlateObservation obs;
    obs.set_event_id("inst-run-1/1001-1");
    obs.set_instance_id("inst-1");
    obs.set_camera_id("cam-1");
    obs.set_algorithm_id("license_plate_recognition");
    obs.set_algorithm_version("1.0.0");
    obs.set_wall_time_ns(1788185888187000000LL);
    obs.set_time_synced(true);
    obs.set_track_id(101);
    obs.set_plate_text("粤B12345");
    obs.set_normalized_text("粤B12345");
    obs.set_plate_color("blue");
    obs.set_plate_type("standard");
    obs.set_confidence(0.96f);
    obs.set_ocr_confidence(0.94f);
    obs.mutable_plate_bbox()->set_x_min(0.35f);
    obs.mutable_plate_bbox()->set_y_min(0.42f);
    obs.mutable_plate_bbox()->set_x_max(0.53f);
    obs.mutable_plate_bbox()->set_y_max(0.51f);
    obs.mutable_vehicle_bbox()->set_x_min(0.18f);
    obs.mutable_vehicle_bbox()->set_y_min(0.20f);
    obs.mutable_vehicle_bbox()->set_x_max(0.72f);
    obs.mutable_vehicle_bbox()->set_y_max(0.85f);
    obs.set_image_id("img_pano_1001");
    obs.set_image_rel_path("2026-08-31/img_pano_1001.jpg");
    obs.set_plate_image_id("img_plate_1001");
    obs.set_plate_image_rel_path("2026-08-31/img_plate_1001.jpg");

    EXPECT_TRUE(client.report_plate_observation(obs));
    ASSERT_TRUE(app_server.service().wait_for_observation(std::chrono::seconds(2)));

    const auto observations = app_server.service().observations();
    ASSERT_EQ(observations.size(), 1U);
    EXPECT_EQ(observations[0].event_id(), "inst-run-1/1001-1");
    EXPECT_EQ(observations[0].plate_text(), "粤B12345");
    EXPECT_EQ(observations[0].normalized_text(), "粤B12345");
    EXPECT_EQ(observations[0].plate_color(), "blue");
    EXPECT_EQ(observations[0].plate_type(), "standard");
    EXPECT_FLOAT_EQ(observations[0].confidence(), 0.96f);
    EXPECT_FLOAT_EQ(observations[0].ocr_confidence(), 0.94f);
    EXPECT_EQ(observations[0].image_id(), "img_pano_1001");
    EXPECT_EQ(observations[0].plate_image_id(), "img_plate_1001");

    app_server.stop();
}

#endif // !defined(ARGUS_SKIP_IPC_TESTS)
