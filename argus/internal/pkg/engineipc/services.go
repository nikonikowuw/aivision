package engineipc

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	argusv1 "argus/app/internal/proto/argus/v1"
)

// controlPlaneService 实现生成的 ControlPlaneServiceServer：Engine 通过 app.sock
// 拉取 DesiredState。私有实现只负责校验、调用 adapter 与错误归一化，不承载业务逻辑。
type controlPlaneService struct {
	argusv1.UnimplementedControlPlaneServiceServer
	adapter DesiredStateAdapter
	log     *zap.Logger
}

// GetDesiredState 返回 Go 权威的期望状态；adapter 错误归一化为稳定响应 code。
func (s *controlPlaneService) GetDesiredState(ctx context.Context, req *argusv1.GetDesiredStateRequest) (*argusv1.GetDesiredStateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ds, err := s.adapter.DesiredState(ctx, req.GetCurrentRevision())
	if err != nil {
		if isTransportError(err) {
			return nil, err
		}
		code, msg := normalizeAdapterError(err)
		if code == CodeInternalError {
			s.log.Warn("desired state adapter failed",
				zap.String("method", "/argus.v1.ControlPlaneService/GetDesiredState"),
				zap.Error(err))
		}
		return &argusv1.GetDesiredStateResponse{Code: code, ErrorMessage: msg}, nil
	}
	if ds == nil {
		// 内部实现错误：adapter 成功但未返回状态，绝不伪装成成功。
		s.log.Error("desired state adapter returned nil state",
			zap.String("method", "/argus.v1.ControlPlaneService/GetDesiredState"))
		return &argusv1.GetDesiredStateResponse{
			Code:         CodeInternalError,
			ErrorMessage: "internal error",
		}, nil
	}
	return &argusv1.GetDesiredStateResponse{DesiredState: ds, Code: CodeOK}, nil
}

// reportService 实现生成的 ReportServiceServer：Engine 通过 app.sock 异步上报
// 告警、任务/实例状态、遥测与孤儿图片。
type reportService struct {
	argusv1.UnimplementedReportServiceServer
	adapter ReportAdapter
	log     *zap.Logger
}

func newReportService(adapter ReportAdapter, log *zap.Logger) *reportService {
	return &reportService{adapter: adapter, log: log}
}

// logAdapterFailure 记录 adapter 内部错误（仅普通 error 触发；typed AdapterError 不记录 cause）。
func (s *reportService) logAdapterFailure(method string, err error) {
	code, _ := normalizeAdapterError(err)
	if code == CodeInternalError {
		s.log.Warn("report adapter failed",
			zap.String("method", method),
			zap.Error(err))
	}
}

// invalidArgument 构造缺少必填 payload 的传输错误（不调用 adapter）。
func invalidArgument(method string) error {
	return status.Error(codes.InvalidArgument, method+": required payload missing")
}

// adapterResult 归一化 adapter 调用结果并返回业务 code/诊断文本：
//   - nil：成功（空 code）；
//   - transport/context error：原样返回，由调用方作为 gRPC status 传播；
//   - 其他 error：记录内部错误日志并按稳定 code 归一化。
func (s *reportService) adapterResult(method string, err error) (code, msg string, transportErr error) {
	if err == nil {
		return CodeOK, "", nil
	}
	if isTransportError(err) {
		return "", "", err
	}
	s.logAdapterFailure(method, err)
	code, msg = normalizeAdapterError(err)
	return code, msg, nil
}

// ReportAlarm 接收告警上报；transport/context 错误保持 gRPC status，业务错误写入响应 code。
func (s *reportService) ReportAlarm(ctx context.Context, req *argusv1.ReportAlarmRequest) (*argusv1.ReportAlarmResponse, error) {
	const method = "/argus.v1.ReportService/ReportAlarm"
	if req == nil || req.Alarm == nil {
		return nil, invalidArgument(method)
	}
	code, msg, transportErr := s.adapterResult(method, s.adapter.AcceptAlarm(ctx, req.Alarm))
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportAlarmResponse{Code: code, ErrorMessage: msg}, nil
}

// ReportPlateObservation 接收车牌抓拍过车记录上报。
func (s *reportService) ReportPlateObservation(ctx context.Context, req *argusv1.ReportPlateObservationRequest) (*argusv1.ReportPlateObservationResponse, error) {
	const method = "/argus.v1.ReportService/ReportPlateObservation"
	if req == nil || req.Observation == nil {
		return nil, invalidArgument(method)
	}
	code, msg, transportErr := s.adapterResult(method, s.adapter.AcceptPlateObservation(ctx, req.Observation))
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportPlateObservationResponse{Code: code, ErrorMessage: msg}, nil
}

// ReportTaskState 接收任务状态上报；只有 adapter 成功接受后才返回空 code。
func (s *reportService) ReportTaskState(ctx context.Context, req *argusv1.ReportTaskStateRequest) (*argusv1.ReportTaskStateResponse, error) {
	const method = "/argus.v1.ReportService/ReportTaskState"
	if req == nil || req.TaskState == nil {
		return nil, invalidArgument(method)
	}
	code, msg, transportErr := s.adapterResult(method, s.adapter.AcceptTaskState(ctx, req.TaskState))
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportTaskStateResponse{Code: code, ErrorMessage: msg}, nil
}

// ReportInstanceState 接收算法实例状态上报；transport 错误不转换为业务响应。
func (s *reportService) ReportInstanceState(ctx context.Context, req *argusv1.ReportInstanceStateRequest) (*argusv1.ReportInstanceStateResponse, error) {
	const method = "/argus.v1.ReportService/ReportInstanceState"
	if req == nil || req.InstanceState == nil {
		return nil, invalidArgument(method)
	}
	code, msg, transportErr := s.adapterResult(method, s.adapter.AcceptInstanceState(ctx, req.InstanceState))
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportInstanceStateResponse{Code: code, ErrorMessage: msg}, nil
}

// ReportMetrics 接收设备遥测上报；普通 adapter 错误只暴露受控 code。
func (s *reportService) ReportMetrics(ctx context.Context, req *argusv1.ReportMetricsRequest) (*argusv1.ReportMetricsResponse, error) {
	const method = "/argus.v1.ReportService/ReportMetrics"
	if req == nil || req.Telemetry == nil {
		return nil, invalidArgument(method)
	}
	code, msg, transportErr := s.adapterResult(method, s.adapter.AcceptMetrics(ctx, req.Telemetry))
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportMetricsResponse{Code: code, ErrorMessage: msg}, nil
}

// ReportOrphanImages 接收孤儿图片对账请求；失败时不得返回空 code 使 Engine 删除图片。
func (s *reportService) ReportOrphanImages(ctx context.Context, req *argusv1.ReportOrphanImagesRequest) (*argusv1.ReportOrphanImagesResponse, error) {
	const method = "/argus.v1.ReportService/ReportOrphanImages"
	if req == nil {
		return nil, invalidArgument(method)
	}
	disposition, err := s.adapter.ReconcileOrphanImages(ctx, req.GetOrphanImages())
	code, msg, transportErr := s.adapterResult(method, err)
	if transportErr != nil {
		return nil, transportErr
	}
	return &argusv1.ReportOrphanImagesResponse{
		RetainImageIds: disposition.RetainImageIDs,
		DeleteImageIds: disposition.DeleteImageIDs,
		Code:           code,
		ErrorMessage:   msg,
	}, nil
}
