#pragma once

#include <string>
#include <memory>
#include <vector>
#include <grpcpp/grpcpp.h>
#include "aivision/v1/engine.grpc.pb.h"
#include "aivision/v1/app.grpc.pb.h"
#include "aivision/v1/person.grpc.pb.h"
#include "aivision/platform/platform_api.hpp"
#include "aivision/media/media_api.hpp"

namespace aivision::core {

class UdsClient {
public:
    explicit UdsClient(const std::string& app_sock_path);
    ~UdsClient() = default;

    bool report_alarm(const aivision::v1::AlarmEvent& alarm);
    bool report_telemetry(const aivision::v1::DeviceTelemetry& telemetry);
    bool report_task_state(const aivision::v1::TaskState& task_state);
    bool report_instance_state(const aivision::v1::InstanceState& instance_state);
    bool report_orphan_images(const std::vector<aivision::v1::OrphanImageEntry>& orphan_images,
                              aivision::v1::ReportOrphanImagesResponse* out_response);
    bool get_desired_state(uint64_t current_revision, aivision::v1::DesiredState* out_state);

private:
    std::shared_ptr<grpc::Channel> channel_;
    std::unique_ptr<aivision::v1::ReportService::Stub> report_stub_;
    std::unique_ptr<aivision::v1::ControlPlaneService::Stub> control_plane_stub_;
};

class UdsServer {
public:
    explicit UdsServer(
        const std::string& engine_sock_path,
        std::shared_ptr<platform::IPlatformAdapter> platform_adapter = nullptr,
        std::shared_ptr<media::IMediaBackend> media_backend = nullptr);
    ~UdsServer();

    bool start();
    bool apply_desired_state(const aivision::v1::DesiredState& desired_state,
                             aivision::v1::ApplyDesiredStateResponse* response);

    void stop();

private:
    std::string sock_path_;
    std::shared_ptr<platform::IPlatformAdapter> platform_adapter_;
    std::shared_ptr<media::IMediaBackend> media_backend_;
    std::unique_ptr<grpc::Server> server_;
    std::unique_ptr<grpc::Service> engine_service_;
    std::unique_ptr<grpc::Service> person_service_;
    bool owns_socket_ = false;
};

} // namespace aivision::core
