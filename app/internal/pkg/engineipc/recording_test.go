package engineipc

import (
	"context"
	"sync"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// recordingDesiredStateAdapter 是 DesiredStateAdapter 的 recording fake：
// 记录调用参数，按配置返回状态或错误。
type recordingDesiredStateAdapter struct {
	mu       sync.Mutex
	state    *aivisionv1.DesiredState
	err      error
	panic    bool
	revision uint64
	calls    int
}

func (a *recordingDesiredStateAdapter) DesiredState(_ context.Context, rev uint64) (*aivisionv1.DesiredState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revision = rev
	a.calls++
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return nil, a.err
	}
	return a.state, nil
}

func (a *recordingDesiredStateAdapter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// recordingReportAdapter 是 ReportAdapter 的 recording fake。
type recordingReportAdapter struct {
	mu            sync.Mutex
	alarms        []*aivisionv1.AlarmEvent
	taskStates    []*aivisionv1.TaskState
	instStates    []*aivisionv1.InstanceState
	metrics       []*aivisionv1.DeviceTelemetry
	orphanReports [][]*aivisionv1.OrphanImageEntry
	err           error
	panic         bool
	disposition   OrphanDisposition
}

func (a *recordingReportAdapter) AcceptAlarm(_ context.Context, alarm *aivisionv1.AlarmEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return a.err
	}
	a.alarms = append(a.alarms, alarm)
	return nil
}

func (a *recordingReportAdapter) AcceptTaskState(_ context.Context, state *aivisionv1.TaskState) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return a.err
	}
	a.taskStates = append(a.taskStates, state)
	return nil
}

func (a *recordingReportAdapter) AcceptInstanceState(_ context.Context, state *aivisionv1.InstanceState) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return a.err
	}
	a.instStates = append(a.instStates, state)
	return nil
}

func (a *recordingReportAdapter) AcceptMetrics(_ context.Context, telemetry *aivisionv1.DeviceTelemetry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return a.err
	}
	a.metrics = append(a.metrics, telemetry)
	return nil
}

func (a *recordingReportAdapter) ReconcileOrphanImages(_ context.Context, orphans []*aivisionv1.OrphanImageEntry) (OrphanDisposition, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panic {
		panic("boom")
	}
	if a.err != nil {
		return OrphanDisposition{}, a.err
	}
	a.orphanReports = append(a.orphanReports, orphans)
	return a.disposition, nil
}

func (a *recordingReportAdapter) alarmCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.alarms)
}

func (a *recordingReportAdapter) lastAlarm() *aivisionv1.AlarmEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.alarms) == 0 {
		return nil
	}
	return a.alarms[len(a.alarms)-1]
}

func (a *recordingReportAdapter) lastTaskState() *aivisionv1.TaskState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.taskStates) == 0 {
		return nil
	}
	return a.taskStates[len(a.taskStates)-1]
}

func (a *recordingReportAdapter) lastInstanceState() *aivisionv1.InstanceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.instStates) == 0 {
		return nil
	}
	return a.instStates[len(a.instStates)-1]
}

func (a *recordingReportAdapter) lastMetrics() *aivisionv1.DeviceTelemetry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.metrics) == 0 {
		return nil
	}
	return a.metrics[len(a.metrics)-1]
}

func (a *recordingReportAdapter) orphanReportCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.orphanReports)
}
