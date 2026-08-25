// Package main 维护 Stub 服务的内存状态与测试钩子
package main

import (
	"sync"

	pb "stub_server/gen/aivision/v1"
)


type ServerState struct {
	mu sync.RWMutex

	// DesiredState to return to Engine on GetDesiredState
	desiredState *pb.DesiredState

	// Received reports
	alarms        []*pb.AlarmEvent
	taskStates    map[string]*pb.TaskState
	instStates    map[string]*pb.InstanceState
	telemetryList []*pb.DeviceTelemetry
	orphanReports []*pb.ReportOrphanImagesRequest

	// Orphan response policy
	retainImageIDs []string
	deleteImageIDs []string

	// Hooks / flags for testing
	delayedACKMs int
	returnCode   string // If non-empty, returned as error code in responses
}

func NewServerState() *ServerState {
	return &ServerState{
		taskStates:     make(map[string]*pb.TaskState),
		instStates:     make(map[string]*pb.InstanceState),
		desiredState:   &pb.DesiredState{Revision: 0},
		alarms:         make([]*pb.AlarmEvent, 0),
		telemetryList:  make([]*pb.DeviceTelemetry, 0),
		orphanReports:  make([]*pb.ReportOrphanImagesRequest, 0),
		retainImageIDs: make([]string, 0),
		deleteImageIDs: make([]string, 0),
	}
}

func (s *ServerState) SetDesiredState(ds *pb.DesiredState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.desiredState = ds
}

func (s *ServerState) GetDesiredState() *pb.DesiredState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.desiredState
}

func (s *ServerState) AddAlarm(alarm *pb.AlarmEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alarms = append(s.alarms, alarm)
}

func (s *ServerState) GetAlarms() []*pb.AlarmEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]*pb.AlarmEvent, len(s.alarms))
	copy(res, s.alarms)
	return res
}

func (s *ServerState) SetTaskState(state *pb.TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskStates[state.CameraId] = state
}

func (s *ServerState) GetTaskState(cameraID string) *pb.TaskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.taskStates[cameraID]
}

func (s *ServerState) SetInstanceState(state *pb.InstanceState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instStates[state.InstanceId] = state
}

func (s *ServerState) GetInstanceState(instanceID string) *pb.InstanceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instStates[instanceID]
}

func (s *ServerState) AddTelemetry(telemetry *pb.DeviceTelemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.telemetryList = append(s.telemetryList, telemetry)
}

func (s *ServerState) GetTelemetryList() []*pb.DeviceTelemetry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]*pb.DeviceTelemetry, len(s.telemetryList))
	copy(res, s.telemetryList)
	return res
}

func (s *ServerState) AddOrphanReport(req *pb.ReportOrphanImagesRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orphanReports = append(s.orphanReports, req)
}

func (s *ServerState) SetOrphanPolicy(retain []string, delete []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retainImageIDs = retain
	s.deleteImageIDs = delete
}

func (s *ServerState) GetOrphanPolicy() ([]string, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retainImageIDs, s.deleteImageIDs
}

func (s *ServerState) ResetReports() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alarms = make([]*pb.AlarmEvent, 0)
	s.taskStates = make(map[string]*pb.TaskState)
	s.instStates = make(map[string]*pb.InstanceState)
	s.telemetryList = make([]*pb.DeviceTelemetry, 0)
	s.orphanReports = make([]*pb.ReportOrphanImagesRequest, 0)
}
