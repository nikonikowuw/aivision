// Package main 实现 Stub gRPC 控制面与数据上报服务处理逻辑
package main

import (
	"context"
	"time"

	pb "stub_server/gen/argus/v1"
)


type ControlPlaneServiceImpl struct {
	pb.UnimplementedControlPlaneServiceServer
	state *ServerState
}

func NewControlPlaneService(state *ServerState) *ControlPlaneServiceImpl {
	return &ControlPlaneServiceImpl{state: state}
}

func (s *ControlPlaneServiceImpl) GetDesiredState(ctx context.Context, req *pb.GetDesiredStateRequest) (*pb.GetDesiredStateResponse, error) {
	s.state.mu.RLock()
	delay := s.state.delayedACKMs
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}

	if code != "" {
		return &pb.GetDesiredStateResponse{
			Code:         code,
			ErrorMessage: "injected error code: " + code,
		}, nil
	}

	ds := s.state.GetDesiredState()
	if ds == nil {
		ds = &pb.DesiredState{
			Revision: 0,
		}
	}
	return &pb.GetDesiredStateResponse{
		DesiredState: ds,
		Code:         "",
	}, nil
}

type ReportServiceImpl struct {
	pb.UnimplementedReportServiceServer
	state *ServerState
}

func NewReportService(state *ServerState) *ReportServiceImpl {
	return &ReportServiceImpl{state: state}
}

func (s *ReportServiceImpl) ReportAlarm(ctx context.Context, req *pb.ReportAlarmRequest) (*pb.ReportAlarmResponse, error) {
	s.state.mu.RLock()
	delay := s.state.delayedACKMs
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}

	if code != "" {
		return &pb.ReportAlarmResponse{
			Code:         code,
			ErrorMessage: "injected error: " + code,
		}, nil
	}

	if req.GetAlarm() != nil {
		s.state.AddAlarm(req.GetAlarm())
	}
	return &pb.ReportAlarmResponse{
		Code: "",
	}, nil
}

func (s *ReportServiceImpl) ReportTaskState(ctx context.Context, req *pb.ReportTaskStateRequest) (*pb.ReportTaskStateResponse, error) {
	s.state.mu.RLock()
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if code != "" {
		return &pb.ReportTaskStateResponse{
			Code:         code,
			ErrorMessage: "injected error: " + code,
		}, nil
	}

	if req.GetTaskState() != nil {
		s.state.SetTaskState(req.GetTaskState())
	}
	return &pb.ReportTaskStateResponse{Code: ""}, nil
}

func (s *ReportServiceImpl) ReportInstanceState(ctx context.Context, req *pb.ReportInstanceStateRequest) (*pb.ReportInstanceStateResponse, error) {
	s.state.mu.RLock()
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if code != "" {
		return &pb.ReportInstanceStateResponse{
			Code:         code,
			ErrorMessage: "injected error: " + code,
		}, nil
	}

	if req.GetInstanceState() != nil {
		s.state.SetInstanceState(req.GetInstanceState())
	}
	return &pb.ReportInstanceStateResponse{Code: ""}, nil
}

func (s *ReportServiceImpl) ReportMetrics(ctx context.Context, req *pb.ReportMetricsRequest) (*pb.ReportMetricsResponse, error) {
	s.state.mu.RLock()
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if code != "" {
		return &pb.ReportMetricsResponse{
			Code:         code,
			ErrorMessage: "injected error: " + code,
		}, nil
	}

	if req.GetTelemetry() != nil {
		s.state.AddTelemetry(req.GetTelemetry())
	}
	return &pb.ReportMetricsResponse{Code: ""}, nil
}

func (s *ReportServiceImpl) ReportOrphanImages(ctx context.Context, req *pb.ReportOrphanImagesRequest) (*pb.ReportOrphanImagesResponse, error) {
	s.state.mu.RLock()
	code := s.state.returnCode
	s.state.mu.RUnlock()

	if code != "" {
		return &pb.ReportOrphanImagesResponse{
			Code:         code,
			ErrorMessage: "injected error: " + code,
		}, nil
	}

	s.state.AddOrphanReport(req)
	retain, deleteIDs := s.state.GetOrphanPolicy()
	return &pb.ReportOrphanImagesResponse{
		RetainImageIds: retain,
		DeleteImageIds: deleteIDs,
		Code:           "",
	}, nil
}
