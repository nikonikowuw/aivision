#pragma once

/**
 * @file uds_ipc.hpp
 * @brief Unix Domain Socket (UDS) gRPC 进程间通信客户端与服务端
 * 
 * 实现 Engine 与 App (Go 后端) 的全双工通信契约：
 * 1. UdsClient：Engine 向 App 主动上报告警事件、设备遥测、任务状态及拉取期望状态（DesiredState）；
 * 2. UdsServer：Engine 监听 engine.sock，处理来自 App 的 ApplyDesiredState、算法包安装/升级/回滚、图片清理等 RPC。
 */

#include <cstdint>
#include <string>
#include <memory>
#include <vector>
#include <grpcpp/grpcpp.h>
#include "argus/v1/engine.grpc.pb.h"
#include "argus/v1/app.grpc.pb.h"
#include "argus/v1/person.grpc.pb.h"
#include "argus/platform/platform_api.hpp"
#include "argus/media/media_api.hpp"

namespace argus::core {

/**
 * @brief UDS gRPC 客户端（Engine -> App）
 */
class UdsClient {
public:
    /**
     * @brief 构造客户端并建立到 app.sock 的通道
     * @param app_sock_path App 监听的 UDS 路径
     */
    explicit UdsClient(const std::string& app_sock_path);
    ~UdsClient() = default;

    /**
     * @brief 上报检测告警事件
     */
    bool report_alarm(const argus::v1::AlarmEvent& alarm);

    /**
     * @brief 上报车牌通行观测记录
     */
    bool report_plate_observation(const argus::v1::PlateObservation& observation);

    /**
     * @brief 上报人脸识别观测记录
     */
    bool report_face_observation(const argus::v1::FaceObservation& observation);

    /**
     * @brief 上报设备性能与健康遥测数据
     */
    bool report_telemetry(const argus::v1::DeviceTelemetry& telemetry);

    /**
     * @brief 上报摄像头任务状态变更
     */
    bool report_task_state(const argus::v1::TaskState& task_state);

    /**
     * @brief 上报算法实例状态变更
     */
    bool report_instance_state(const argus::v1::InstanceState& instance_state);

    /**
     * @brief 主动上报本地扫描出的孤儿图片记录
     */
    bool report_orphan_images(const std::vector<argus::v1::OrphanImageEntry>& orphan_images,
                              argus::v1::ReportOrphanImagesResponse* out_response);

    /**
     * @brief 向 App 拉取指定 revision 的期望配置（DesiredState）
     */
    bool get_desired_state(uint64_t current_revision, argus::v1::DesiredState* out_state);

    /**
     * @brief 向 App 拉取指定 revision 的全量人脸底库快照
     */
    bool get_face_gallery(uint64_t current_gallery_revision,
                          argus::v1::GetFaceGalleryResponse* out_response);

private:
    std::shared_ptr<grpc::Channel> channel_;
    std::unique_ptr<argus::v1::ReportService::Stub> report_stub_;
    std::unique_ptr<argus::v1::ControlPlaneService::Stub> control_plane_stub_;
};

/**
 * @brief UDS gRPC 服务端（App -> Engine）
 */
class UdsServer {
public:
    /**
     * @brief 构造 UdsServer 实例
     * @param engine_sock_path Engine 监听的 UDS socket 路径
     * @param platform_adapter 平台适配器
     * @param media_backend 媒体后端
     * @param app_sock_path App 监听的 UDS socket 路径；为空时使用开发环境变量
     */
    explicit UdsServer(
        const std::string& engine_sock_path,
        std::shared_ptr<platform::IPlatformAdapter> platform_adapter = nullptr,
        std::shared_ptr<media::IMediaBackend> media_backend = nullptr,
        std::string app_sock_path = {});
    ~UdsServer();

    /**
     * @brief 启动 gRPC 服务端并绑定 UDS socket
     */
    bool start();

    /**
     * @brief 应用全量期望配置状态（增量比对、任务与实例对账调整）
     */
    bool apply_desired_state(const argus::v1::DesiredState& desired_state,
                             argus::v1::ApplyDesiredStateResponse* response);

    /**
     * @brief 停止服务端并解除 socket 绑定
     */
    void stop();

private:
    std::string sock_path_;
    std::string app_sock_path_;
    std::shared_ptr<platform::IPlatformAdapter> platform_adapter_;
    std::shared_ptr<media::IMediaBackend> media_backend_;
    std::unique_ptr<grpc::Server> server_;
    std::unique_ptr<grpc::Service> engine_service_;
    std::unique_ptr<grpc::Service> person_service_;
    bool owns_socket_ = false;
    uint64_t socket_device_ = 0;
    uint64_t socket_inode_ = 0;
    bool socket_identity_valid_ = false;
};

} // namespace argus::core

