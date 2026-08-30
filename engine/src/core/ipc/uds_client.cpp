/**
 * @file uds_client.cpp
 * @brief UDS gRPC 客户端实现（Engine 主动向 Go 后端上报数据）
 */

#include "argus/core/uds_ipc.hpp"

#include <chrono>


namespace argus::core {

UdsClient::UdsClient(const std::string& app_sock_path) {
    // 创建 Unix Domain Socket (UDS) gRPC 传输通道并实例化客户端 Stub
    channel_ = grpc::CreateChannel("unix://" + app_sock_path, grpc::InsecureChannelCredentials());
    report_stub_ = argus::v1::ReportService::NewStub(channel_);
    control_plane_stub_ = argus::v1::ControlPlaneService::NewStub(channel_);
}

bool UdsClient::report_alarm(const argus::v1::AlarmEvent& alarm) {
    if (!report_stub_) return false;
    argus::v1::ReportAlarmRequest req;
    *req.mutable_alarm() = alarm;
    argus::v1::ReportAlarmResponse resp;
    grpc::ClientContext ctx;
    // 设置 2 秒硬超时，防止网络或进程阻塞影响推理主流程
    ctx.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));

    grpc::Status status = report_stub_->ReportAlarm(&ctx, req, &resp);
    return status.ok() && resp.code().empty();
}

bool UdsClient::report_telemetry(const argus::v1::DeviceTelemetry& telemetry) {
    if (!report_stub_) return false;
    argus::v1::ReportMetricsRequest req;
    *req.mutable_telemetry() = telemetry;
    argus::v1::ReportMetricsResponse resp;
    grpc::ClientContext ctx;
    ctx.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));

    grpc::Status status = report_stub_->ReportMetrics(&ctx, req, &resp);
    return status.ok() && resp.code().empty();
}

bool UdsClient::report_task_state(const argus::v1::TaskState& task_state) {
    if (!report_stub_) return false;
    argus::v1::ReportTaskStateRequest request;
    *request.mutable_task_state() = task_state;
    argus::v1::ReportTaskStateResponse response;
    grpc::ClientContext context;
    context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));
    const grpc::Status status = report_stub_->ReportTaskState(&context, request, &response);
    return status.ok() && response.code().empty();
}

bool UdsClient::report_instance_state(const argus::v1::InstanceState& instance_state) {
    if (!report_stub_) return false;
    argus::v1::ReportInstanceStateRequest request;
    *request.mutable_instance_state() = instance_state;
    argus::v1::ReportInstanceStateResponse response;
    grpc::ClientContext context;
    context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));
    const grpc::Status status = report_stub_->ReportInstanceState(&context, request, &response);
    return status.ok() && response.code().empty();
}

bool UdsClient::report_orphan_images(const std::vector<argus::v1::OrphanImageEntry>& orphan_images,
                                     argus::v1::ReportOrphanImagesResponse* out_response) {
    if (!report_stub_ || !out_response) return false;
    argus::v1::ReportOrphanImagesRequest request;
    for (const auto& orphan : orphan_images) *request.add_orphan_images() = orphan;
    grpc::ClientContext context;
    context.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));
    const grpc::Status status = report_stub_->ReportOrphanImages(&context, request, out_response);
    return status.ok() && out_response->code().empty();
}
bool UdsClient::get_desired_state(uint64_t current_revision, argus::v1::DesiredState* out_state) {
    if (!control_plane_stub_ || !out_state) return false;
    argus::v1::GetDesiredStateRequest req;
    req.set_current_revision(current_revision);
    argus::v1::GetDesiredStateResponse resp;
    grpc::ClientContext ctx;
    ctx.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds(2));

    grpc::Status status = control_plane_stub_->GetDesiredState(&ctx, req, &resp);
    if (status.ok() && resp.code().empty() && resp.has_desired_state()) {
        *out_state = resp.desired_state();
        return true;
    }
    return false;
}

} // namespace argus::core
