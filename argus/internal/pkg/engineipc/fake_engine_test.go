package engineipc

import (
	"context"
	"sync"

	argusv1 "argus/app/internal/proto/argus/v1"
)

// fakeEngineServiceServer 是 EngineServiceServer 的 recording fake：
// 记录请求并返回可配置的响应（成功或注入的稳定 code）。
type fakeEngineServiceServer struct {
	argusv1.UnimplementedEngineServiceServer

	mu sync.Mutex
	// 注入的业务错误码；空串表示成功。
	code string

	appliedStates    []*argusv1.ApplyDesiredStateRequest
	upsertedTasks    []*argusv1.UpsertTaskRequest
	setInstStates    []*argusv1.SetInstanceStateRequest
	updatedConfigs   []*argusv1.UpdateInstanceConfigRequest
	installed        []*argusv1.InstallPackageRequest
	upgraded         []*argusv1.UpgradePackageRequest
	rolledBack       []*argusv1.RollbackPackageRequest
	uninstalled      []*argusv1.UninstallPackageRequest
	deletedImages    []*argusv1.DeleteImagesRequest
	reconciledImages []*argusv1.ReconcileImagesRequest
	profileQueries   []*argusv1.QueryProfileRequest
	metricsQueries   []*argusv1.QueryMetricsRequest
	probeCameraCalls []*argusv1.ProbeCameraRequest

	// 可配置返回体。
	profile   *argusv1.PlatformProfileInfo
	telemetry *argusv1.DeviceTelemetry
	probe     *argusv1.ProbeCameraResponse
}

func (f *fakeEngineServiceServer) errResp(method string) (code, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.code != "" {
		return f.code, "injected: " + method
	}
	return CodeOK, ""
}

func (f *fakeEngineServiceServer) ApplyDesiredState(_ context.Context, req *argusv1.ApplyDesiredStateRequest) (*argusv1.ApplyDesiredStateResponse, error) {
	f.mu.Lock()
	f.appliedStates = append(f.appliedStates, req)
	f.mu.Unlock()
	code, msg := f.errResp("ApplyDesiredState")
	return &argusv1.ApplyDesiredStateResponse{AppliedRevision: 42, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UpsertTask(_ context.Context, req *argusv1.UpsertTaskRequest) (*argusv1.UpsertTaskResponse, error) {
	f.mu.Lock()
	f.upsertedTasks = append(f.upsertedTasks, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpsertTask")
	return &argusv1.UpsertTaskResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) SetInstanceState(_ context.Context, req *argusv1.SetInstanceStateRequest) (*argusv1.SetInstanceStateResponse, error) {
	f.mu.Lock()
	f.setInstStates = append(f.setInstStates, req)
	f.mu.Unlock()
	code, msg := f.errResp("SetInstanceState")
	return &argusv1.SetInstanceStateResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UpdateInstanceConfig(_ context.Context, req *argusv1.UpdateInstanceConfigRequest) (*argusv1.UpdateInstanceConfigResponse, error) {
	f.mu.Lock()
	f.updatedConfigs = append(f.updatedConfigs, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpdateInstanceConfig")
	return &argusv1.UpdateInstanceConfigResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) InstallPackage(_ context.Context, req *argusv1.InstallPackageRequest) (*argusv1.InstallPackageResponse, error) {
	f.mu.Lock()
	f.installed = append(f.installed, req)
	f.mu.Unlock()
	code, msg := f.errResp("InstallPackage")
	return &argusv1.InstallPackageResponse{Code: code, ErrorMessage: msg, AlgorithmId: "yolov8n", Version: "1.0.0"}, nil
}

func (f *fakeEngineServiceServer) UpgradePackage(_ context.Context, req *argusv1.UpgradePackageRequest) (*argusv1.UpgradePackageResponse, error) {
	f.mu.Lock()
	f.upgraded = append(f.upgraded, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpgradePackage")
	return &argusv1.UpgradePackageResponse{Code: code, ErrorMessage: msg, AlgorithmId: "yolov8n", Version: "1.1.0"}, nil
}

func (f *fakeEngineServiceServer) RollbackPackage(_ context.Context, req *argusv1.RollbackPackageRequest) (*argusv1.RollbackPackageResponse, error) {
	f.mu.Lock()
	f.rolledBack = append(f.rolledBack, req)
	f.mu.Unlock()
	code, msg := f.errResp("RollbackPackage")
	return &argusv1.RollbackPackageResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UninstallPackage(_ context.Context, req *argusv1.UninstallPackageRequest) (*argusv1.UninstallPackageResponse, error) {
	f.mu.Lock()
	f.uninstalled = append(f.uninstalled, req)
	f.mu.Unlock()
	code, msg := f.errResp("UninstallPackage")
	return &argusv1.UninstallPackageResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) DeleteImages(_ context.Context, req *argusv1.DeleteImagesRequest) (*argusv1.DeleteImagesResponse, error) {
	f.mu.Lock()
	f.deletedImages = append(f.deletedImages, req)
	f.mu.Unlock()
	code, msg := f.errResp("DeleteImages")
	results := make([]*argusv1.DeleteImageResult, 0, len(req.GetImageIds()))
	for _, id := range req.GetImageIds() {
		results = append(results, &argusv1.DeleteImageResult{
			ImageId: id,
			Status:  argusv1.ImageDeleteStatus_IMAGE_DELETE_STATUS_DELETED,
		})
	}
	return &argusv1.DeleteImagesResponse{Results: results, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) ReconcileImages(_ context.Context, req *argusv1.ReconcileImagesRequest) (*argusv1.ReconcileImagesResponse, error) {
	f.mu.Lock()
	f.reconciledImages = append(f.reconciledImages, req)
	f.mu.Unlock()
	code, msg := f.errResp("ReconcileImages")
	return &argusv1.ReconcileImagesResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) QueryProfile(_ context.Context, req *argusv1.QueryProfileRequest) (*argusv1.QueryProfileResponse, error) {
	f.mu.Lock()
	f.profileQueries = append(f.profileQueries, req)
	profile := f.profile
	f.mu.Unlock()
	code, msg := f.errResp("QueryProfile")
	if profile == nil {
		profile = &argusv1.PlatformProfileInfo{PlatformId: "fake", Arch: "arm64", InferenceRuntime: "mock"}
	}
	return &argusv1.QueryProfileResponse{Profile: profile, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) QueryMetrics(_ context.Context, req *argusv1.QueryMetricsRequest) (*argusv1.QueryMetricsResponse, error) {
	f.mu.Lock()
	f.metricsQueries = append(f.metricsQueries, req)
	tel := f.telemetry
	f.mu.Unlock()
	code, msg := f.errResp("QueryMetrics")
	if tel == nil {
		tel = &argusv1.DeviceTelemetry{UptimeSeconds: 7}
	}
	return &argusv1.QueryMetricsResponse{Telemetry: tel, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) ProbeCamera(_ context.Context, req *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
	f.mu.Lock()
	f.probeCameraCalls = append(f.probeCameraCalls, req)
	probe := f.probe
	f.mu.Unlock()
	code, msg := f.errResp("ProbeCamera")
	if probe == nil {
		probe = &argusv1.ProbeCameraResponse{Status: "failed", FailureCode: "RTSP_MEDIA_ERROR"}
	}
	// 新建响应并填充业务码/诊断，避免复制 proto 消息（含内部锁）。
	response := &argusv1.ProbeCameraResponse{
		Status:            probe.GetStatus(),
		FailureCode:       probe.GetFailureCode(),
		SelectedTransport: probe.GetSelectedTransport(),
		Codec:             probe.GetCodec(),
		Width:             probe.GetWidth(),
		Height:            probe.GetHeight(),
		Fps:               probe.GetFps(),
		ElapsedMs:         probe.GetElapsedMs(),
		Code:              code,
		ErrorMessage:      msg,
	}
	for _, attempt := range probe.GetAttempts() {
		response.Attempts = append(response.Attempts, &argusv1.ProbeAttempt{
			Transport:   attempt.GetTransport(),
			FailureCode: attempt.GetFailureCode(),
			ElapsedMs:   attempt.GetElapsedMs(),
		})
	}
	return response, nil
}
