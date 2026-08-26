package engineipc

import (
	"context"
	"sync"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// fakeEngineServiceServer 是 EngineServiceServer 的 recording fake：
// 记录请求并返回可配置的响应（成功或注入的稳定 code）。
type fakeEngineServiceServer struct {
	aivisionv1.UnimplementedEngineServiceServer

	mu sync.Mutex
	// 注入的业务错误码；空串表示成功。
	code string

	appliedStates    []*aivisionv1.ApplyDesiredStateRequest
	upsertedTasks    []*aivisionv1.UpsertTaskRequest
	setInstStates    []*aivisionv1.SetInstanceStateRequest
	updatedConfigs   []*aivisionv1.UpdateInstanceConfigRequest
	installed        []*aivisionv1.InstallPackageRequest
	upgraded         []*aivisionv1.UpgradePackageRequest
	rolledBack       []*aivisionv1.RollbackPackageRequest
	uninstalled      []*aivisionv1.UninstallPackageRequest
	deletedImages    []*aivisionv1.DeleteImagesRequest
	reconciledImages []*aivisionv1.ReconcileImagesRequest
	profileQueries   []*aivisionv1.QueryProfileRequest
	metricsQueries   []*aivisionv1.QueryMetricsRequest
	probeCameraCalls []*aivisionv1.ProbeCameraRequest

	// 可配置返回体。
	profile   *aivisionv1.PlatformProfileInfo
	telemetry *aivisionv1.DeviceTelemetry
	probe     *aivisionv1.ProbeCameraResponse
}

func (f *fakeEngineServiceServer) errResp(method string) (code, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.code != "" {
		return f.code, "injected: " + method
	}
	return CodeOK, ""
}

func (f *fakeEngineServiceServer) ApplyDesiredState(_ context.Context, req *aivisionv1.ApplyDesiredStateRequest) (*aivisionv1.ApplyDesiredStateResponse, error) {
	f.mu.Lock()
	f.appliedStates = append(f.appliedStates, req)
	f.mu.Unlock()
	code, msg := f.errResp("ApplyDesiredState")
	return &aivisionv1.ApplyDesiredStateResponse{AppliedRevision: 42, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UpsertTask(_ context.Context, req *aivisionv1.UpsertTaskRequest) (*aivisionv1.UpsertTaskResponse, error) {
	f.mu.Lock()
	f.upsertedTasks = append(f.upsertedTasks, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpsertTask")
	return &aivisionv1.UpsertTaskResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) SetInstanceState(_ context.Context, req *aivisionv1.SetInstanceStateRequest) (*aivisionv1.SetInstanceStateResponse, error) {
	f.mu.Lock()
	f.setInstStates = append(f.setInstStates, req)
	f.mu.Unlock()
	code, msg := f.errResp("SetInstanceState")
	return &aivisionv1.SetInstanceStateResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UpdateInstanceConfig(_ context.Context, req *aivisionv1.UpdateInstanceConfigRequest) (*aivisionv1.UpdateInstanceConfigResponse, error) {
	f.mu.Lock()
	f.updatedConfigs = append(f.updatedConfigs, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpdateInstanceConfig")
	return &aivisionv1.UpdateInstanceConfigResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) InstallPackage(_ context.Context, req *aivisionv1.InstallPackageRequest) (*aivisionv1.InstallPackageResponse, error) {
	f.mu.Lock()
	f.installed = append(f.installed, req)
	f.mu.Unlock()
	code, msg := f.errResp("InstallPackage")
	return &aivisionv1.InstallPackageResponse{Code: code, ErrorMessage: msg, AlgorithmId: "yolov8n", Version: "1.0.0"}, nil
}

func (f *fakeEngineServiceServer) UpgradePackage(_ context.Context, req *aivisionv1.UpgradePackageRequest) (*aivisionv1.UpgradePackageResponse, error) {
	f.mu.Lock()
	f.upgraded = append(f.upgraded, req)
	f.mu.Unlock()
	code, msg := f.errResp("UpgradePackage")
	return &aivisionv1.UpgradePackageResponse{Code: code, ErrorMessage: msg, AlgorithmId: "yolov8n", Version: "1.1.0"}, nil
}

func (f *fakeEngineServiceServer) RollbackPackage(_ context.Context, req *aivisionv1.RollbackPackageRequest) (*aivisionv1.RollbackPackageResponse, error) {
	f.mu.Lock()
	f.rolledBack = append(f.rolledBack, req)
	f.mu.Unlock()
	code, msg := f.errResp("RollbackPackage")
	return &aivisionv1.RollbackPackageResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) UninstallPackage(_ context.Context, req *aivisionv1.UninstallPackageRequest) (*aivisionv1.UninstallPackageResponse, error) {
	f.mu.Lock()
	f.uninstalled = append(f.uninstalled, req)
	f.mu.Unlock()
	code, msg := f.errResp("UninstallPackage")
	return &aivisionv1.UninstallPackageResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) DeleteImages(_ context.Context, req *aivisionv1.DeleteImagesRequest) (*aivisionv1.DeleteImagesResponse, error) {
	f.mu.Lock()
	f.deletedImages = append(f.deletedImages, req)
	f.mu.Unlock()
	code, msg := f.errResp("DeleteImages")
	results := make([]*aivisionv1.DeleteImageResult, 0, len(req.GetImageIds()))
	for _, id := range req.GetImageIds() {
		results = append(results, &aivisionv1.DeleteImageResult{
			ImageId: id,
			Status:  aivisionv1.ImageDeleteStatus_IMAGE_DELETE_STATUS_DELETED,
		})
	}
	return &aivisionv1.DeleteImagesResponse{Results: results, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) ReconcileImages(_ context.Context, req *aivisionv1.ReconcileImagesRequest) (*aivisionv1.ReconcileImagesResponse, error) {
	f.mu.Lock()
	f.reconciledImages = append(f.reconciledImages, req)
	f.mu.Unlock()
	code, msg := f.errResp("ReconcileImages")
	return &aivisionv1.ReconcileImagesResponse{Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) QueryProfile(_ context.Context, req *aivisionv1.QueryProfileRequest) (*aivisionv1.QueryProfileResponse, error) {
	f.mu.Lock()
	f.profileQueries = append(f.profileQueries, req)
	profile := f.profile
	f.mu.Unlock()
	code, msg := f.errResp("QueryProfile")
	if profile == nil {
		profile = &aivisionv1.PlatformProfileInfo{PlatformId: "fake", Arch: "arm64", InferenceRuntime: "mock"}
	}
	return &aivisionv1.QueryProfileResponse{Profile: profile, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) QueryMetrics(_ context.Context, req *aivisionv1.QueryMetricsRequest) (*aivisionv1.QueryMetricsResponse, error) {
	f.mu.Lock()
	f.metricsQueries = append(f.metricsQueries, req)
	tel := f.telemetry
	f.mu.Unlock()
	code, msg := f.errResp("QueryMetrics")
	if tel == nil {
		tel = &aivisionv1.DeviceTelemetry{UptimeSeconds: 7}
	}
	return &aivisionv1.QueryMetricsResponse{Telemetry: tel, Code: code, ErrorMessage: msg}, nil
}

func (f *fakeEngineServiceServer) ProbeCamera(_ context.Context, req *aivisionv1.ProbeCameraRequest) (*aivisionv1.ProbeCameraResponse, error) {
	f.mu.Lock()
	f.probeCameraCalls = append(f.probeCameraCalls, req)
	probe := f.probe
	f.mu.Unlock()
	code, msg := f.errResp("ProbeCamera")
	if probe == nil {
		probe = &aivisionv1.ProbeCameraResponse{Status: "failed", FailureCode: "RTSP_MEDIA_ERROR"}
	}
	// 新建响应并填充业务码/诊断，避免复制 proto 消息（含内部锁）。
	response := &aivisionv1.ProbeCameraResponse{
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
		response.Attempts = append(response.Attempts, &aivisionv1.ProbeAttempt{
			Transport:   attempt.GetTransport(),
			FailureCode: attempt.GetFailureCode(),
			ElapsedMs:   attempt.GetElapsedMs(),
		})
	}
	return response, nil
}
