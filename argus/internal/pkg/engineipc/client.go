package engineipc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"argus/app/internal/pkg/config"
	argusv1 "argus/app/internal/proto/argus/v1"
)

// RemoteError 是 Engine 返回的业务错误（非空响应 code）。调用方只判断 Code，
// 不解析 ErrorMessage 文本；ErrorMessage 仅供诊断。
type RemoteError struct {
	Code         string
	ErrorMessage string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("engine ipc error: %s", e.Code)
}

// codedResponse 是所有 EngineService 响应共同的最小接口（稳定 code + 诊断文本）。
type codedResponse interface {
	GetCode() string
	GetErrorMessage() string
}

// responseError 检查响应 code：空串表示成功返回 nil；非空返回 typed *RemoteError。
func responseError(resp codedResponse) error {
	if resp.GetCode() != "" {
		return &RemoteError{Code: resp.GetCode(), ErrorMessage: resp.GetErrorMessage()}
	}
	return nil
}

// call 执行一次 EngineService RPC：transport error 原样返回；非空响应 code 转成
// 可 errors.As 判断的 *RemoteError，同时保留响应供诊断。12 个包装方法共用。
func call[Req any, R codedResponse](
	fn func(ctx context.Context, req *Req, opts ...grpc.CallOption) (R, error),
	ctx context.Context, req *Req, opts ...grpc.CallOption,
) (resp R, err error) {
	resp, err = fn(ctx, req, opts...)
	if err != nil {
		return resp, err
	}
	return resp, responseError(resp)
}

// EngineClient 是 EngineService 的薄客户端包装：持有长期 grpc.ClientConn，
// 提供 12 个与 generated client 同签名的包装方法。transport error 原样返回；
// 非空响应 code 转成可 errors.As 判断的 *RemoteError，同时保留响应供诊断。
type EngineClient struct {
	raw       argusv1.EngineServiceClient
	personRaw argusv1.PersonServiceClient
	conn      *grpc.ClientConn
}

// NewEngineClient 构造 EngineClient。使用 unix://<absolute-path> + insecure 凭据，
// 安全边界来自本地目录、socket owner/group 与 mode，不启用 TCP fallback。
//
// 不使用 WithBlock：Engine 尚未启动时 Go 仍能正常启动；ClientConn 自行维护连接状态，
// 后续调用可在 Engine 恢复后成功。Engine socket 不存在不属于构造失败。
func NewEngineClient(cfg *config.Config) (*EngineClient, error) {
	socketPath := cfg.IPC.EngineSocket
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create engine grpc client for %s: %w", socketPath, err)
	}
	return &EngineClient{
		raw:       argusv1.NewEngineServiceClient(conn),
		personRaw: argusv1.NewPersonServiceClient(conn),
		conn:      conn,
	}, nil
}

// Close 关闭底层连接。
func (c *EngineClient) Close() error {
	return c.conn.Close()
}

// Conn 返回底层 ClientConn（供需要直接访问传输层的场景使用）。
func (c *EngineClient) Conn() *grpc.ClientConn {
	return c.conn
}

// ApplyDesiredState 下发全量期望状态；deadline 由调用方 context 决定。
func (c *EngineClient) ApplyDesiredState(ctx context.Context, req *argusv1.ApplyDesiredStateRequest, opts ...grpc.CallOption) (*argusv1.ApplyDesiredStateResponse, error) {
	return call(c.raw.ApplyDesiredState, ctx, req, opts...)
}

// UpsertTask 新增或更新单摄像头任务。
func (c *EngineClient) UpsertTask(ctx context.Context, req *argusv1.UpsertTaskRequest, opts ...grpc.CallOption) (*argusv1.UpsertTaskResponse, error) {
	return call(c.raw.UpsertTask, ctx, req, opts...)
}

// SetInstanceState 设置算法实例启用状态。
func (c *EngineClient) SetInstanceState(ctx context.Context, req *argusv1.SetInstanceStateRequest, opts ...grpc.CallOption) (*argusv1.SetInstanceStateResponse, error) {
	return call(c.raw.SetInstanceState, ctx, req, opts...)
}

// UpdateInstanceConfig 热更新实例配置。
func (c *EngineClient) UpdateInstanceConfig(ctx context.Context, req *argusv1.UpdateInstanceConfigRequest, opts ...grpc.CallOption) (*argusv1.UpdateInstanceConfigResponse, error) {
	return call(c.raw.UpdateInstanceConfig, ctx, req, opts...)
}

// InstallPackage 安装算法包。
func (c *EngineClient) InstallPackage(ctx context.Context, req *argusv1.InstallPackageRequest, opts ...grpc.CallOption) (*argusv1.InstallPackageResponse, error) {
	return call(c.raw.InstallPackage, ctx, req, opts...)
}

// UpgradePackage 升级算法包。
func (c *EngineClient) UpgradePackage(ctx context.Context, req *argusv1.UpgradePackageRequest, opts ...grpc.CallOption) (*argusv1.UpgradePackageResponse, error) {
	return call(c.raw.UpgradePackage, ctx, req, opts...)
}

// RollbackPackage 回滚算法包版本。
func (c *EngineClient) RollbackPackage(ctx context.Context, req *argusv1.RollbackPackageRequest, opts ...grpc.CallOption) (*argusv1.RollbackPackageResponse, error) {
	return call(c.raw.RollbackPackage, ctx, req, opts...)
}

// UninstallPackage 卸载算法包。
func (c *EngineClient) UninstallPackage(ctx context.Context, req *argusv1.UninstallPackageRequest, opts ...grpc.CallOption) (*argusv1.UninstallPackageResponse, error) {
	return call(c.raw.UninstallPackage, ctx, req, opts...)
}

// DeleteImages 请求 Engine 删除图片。
func (c *EngineClient) DeleteImages(ctx context.Context, req *argusv1.DeleteImagesRequest, opts ...grpc.CallOption) (*argusv1.DeleteImagesResponse, error) {
	return call(c.raw.DeleteImages, ctx, req, opts...)
}

// ReconcileImages 推送 Go 权威保留的图片 ID 集合供 Engine 对账。
func (c *EngineClient) ReconcileImages(ctx context.Context, req *argusv1.ReconcileImagesRequest, opts ...grpc.CallOption) (*argusv1.ReconcileImagesResponse, error) {
	return call(c.raw.ReconcileImages, ctx, req, opts...)
}

// QueryProfile 查询平台能力档案。
func (c *EngineClient) QueryProfile(ctx context.Context, req *argusv1.QueryProfileRequest, opts ...grpc.CallOption) (*argusv1.QueryProfileResponse, error) {
	return call(c.raw.QueryProfile, ctx, req, opts...)
}

// QueryMetrics 查询设备遥测。
func (c *EngineClient) QueryMetrics(ctx context.Context, req *argusv1.QueryMetricsRequest, opts ...grpc.CallOption) (*argusv1.QueryMetricsResponse, error) {
	return call(c.raw.QueryMetrics, ctx, req, opts...)
}

// ProbeCamera 摄像头测活；RPC code 仅表示处理成功，测活结果在 status/failure_code 中。
func (c *EngineClient) ProbeCamera(ctx context.Context, req *argusv1.ProbeCameraRequest, opts ...grpc.CallOption) (*argusv1.ProbeCameraResponse, error) {
	return call(c.raw.ProbeCamera, ctx, req, opts...)
}

// StartCameraPreview 请求开启摄像头预览拉流。
func (c *EngineClient) StartCameraPreview(ctx context.Context, req *argusv1.StartCameraPreviewRequest, opts ...grpc.CallOption) (*argusv1.StartCameraPreviewResponse, error) {
	return call(c.raw.StartCameraPreview, ctx, req, opts...)
}

// StopCameraPreview 停止摄像头预览拉流。
func (c *EngineClient) StopCameraPreview(ctx context.Context, req *argusv1.StopCameraPreviewRequest, opts ...grpc.CallOption) (*argusv1.StopCameraPreviewResponse, error) {
	return call(c.raw.StopCameraPreview, ctx, req, opts...)
}

// ExtractFaceFeature 提取人脸特征与对齐标准化人脸图。
func (c *EngineClient) ExtractFaceFeature(ctx context.Context, req *argusv1.ExtractFaceFeatureRequest, opts ...grpc.CallOption) (*argusv1.ExtractFaceFeatureResponse, error) {
	return call(c.personRaw.ExtractFaceFeature, ctx, req, opts...)
}
