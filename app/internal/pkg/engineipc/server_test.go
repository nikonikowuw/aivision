package engineipc

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// startTestRuntime 在临时 UDS 上启动 Runtime 并注册清理；测试用例自行 dial。
func startTestRuntime(t *testing.T, ds DesiredStateAdapter, rep ReportAdapter) *Runtime {
	t.Helper()
	rt := NewRuntime(zap.NewNop(), ds, rep)
	path := testSocketPath(t, "app.sock")
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rt.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return rt
}

// dialReportClients 通过真实 UDS 建立客户端连接，返回 ReportService 与 ControlPlaneService client。
func dialReportClients(t *testing.T, rt *Runtime) (*grpc.ClientConn, aivisionv1.ReportServiceClient, aivisionv1.ControlPlaneServiceClient) {
	t.Helper()
	conn, err := grpc.NewClient("unix://"+rt.owner.Path(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, aivisionv1.NewReportServiceClient(conn), aivisionv1.NewControlPlaneServiceClient(conn)
}

func callCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestControlPlaneGetDesiredStateSuccess(t *testing.T) {
	ds := &recordingDesiredStateAdapter{state: &aivisionv1.DesiredState{
		DeviceId: "dev-1",
		Revision: 7,
		Tasks:    []*aivisionv1.CameraTaskConfig{{CameraId: "cam-1", Enabled: true}},
	}}
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	resp, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{CurrentRevision: 3})
	if err != nil {
		t.Fatalf("GetDesiredState: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	if resp.GetDesiredState().GetRevision() != 7 || resp.GetDesiredState().GetDeviceId() != "dev-1" {
		t.Errorf("desired_state = %+v", resp.GetDesiredState())
	}
	if len(resp.GetDesiredState().GetTasks()) != 1 || resp.GetDesiredState().GetTasks()[0].GetCameraId() != "cam-1" {
		t.Errorf("tasks = %+v", resp.GetDesiredState().GetTasks())
	}
	if ds.revision != 3 {
		t.Errorf("adapter received revision = %d, want 3", ds.revision)
	}
}

func TestControlPlaneGetDesiredStateTypedError(t *testing.T) {
	ds := &recordingDesiredStateAdapter{err: NewAdapterError("STALE_REVISION", "revision moved on")}
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	resp, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if err != nil {
		t.Fatalf("GetDesiredState transport: %v", err)
	}
	if resp.GetCode() != "STALE_REVISION" {
		t.Errorf("code = %q, want STALE_REVISION", resp.GetCode())
	}
	if resp.GetDesiredState() != nil {
		t.Errorf("desired_state should be nil on error, got %+v", resp.GetDesiredState())
	}
}

func TestControlPlaneGetDesiredStateTransportError(t *testing.T) {
	ds := &recordingDesiredStateAdapter{err: status.Error(codes.Unavailable, "engine dependency unavailable")}
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	_, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetDesiredState status = %v, want Unavailable", status.Code(err))
	}
}

// TestControlPlaneGetDesiredStateDeadlinePreserved context deadline 不应降级为业务 ACK。
func TestControlPlaneGetDesiredStateDeadlinePreserved(t *testing.T) {
	ds := &recordingDesiredStateAdapter{err: context.DeadlineExceeded}
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	_, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("GetDesiredState status = %v, want DeadlineExceeded", status.Code(err))
	}
}

func TestControlPlaneGetDesiredStateInternalError(t *testing.T) {
	ds := &recordingDesiredStateAdapter{err: errors.New("storage failed")}
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	resp, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if err != nil {
		t.Fatalf("GetDesiredState transport: %v", err)
	}
	if resp.GetCode() != CodeInternalError {
		t.Errorf("code = %q, want INTERNAL_ERROR", resp.GetCode())
	}
}

func TestControlPlaneGetDesiredStateNilState(t *testing.T) {
	ds := &recordingDesiredStateAdapter{} // (nil, nil)
	rt := startTestRuntime(t, ds, &recordingReportAdapter{})
	_, _, cp := dialReportClients(t, rt)

	resp, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if err != nil {
		t.Fatalf("GetDesiredState transport: %v", err)
	}
	if resp.GetCode() != CodeInternalError {
		t.Errorf("code = %q, want INTERNAL_ERROR for nil state", resp.GetCode())
	}
}

func TestControlPlaneGetDesiredStateUnavailable(t *testing.T) {
	rt := startTestRuntime(t, UnavailableDesiredStateAdapter(), UnavailableReportAdapter())
	_, _, cp := dialReportClients(t, rt)

	resp, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if err != nil {
		t.Fatalf("GetDesiredState transport: %v", err)
	}
	if resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("code = %q, want IPC_UNAVAILABLE", resp.GetCode())
	}
	if resp.GetDesiredState() != nil {
		t.Errorf("desired_state must be nil on IPC_UNAVAILABLE")
	}
}

func TestReportAlarmSuccess(t *testing.T) {
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	alarm := &aivisionv1.AlarmEvent{
		EventId: "run-1/evt-1", InstanceId: "inst-1", CameraId: "cam-1",
		AlgorithmId: "yolov8n", AlarmTypeId: "person",
	}
	resp, err := rc.ReportAlarm(callCtx(t), &aivisionv1.ReportAlarmRequest{Alarm: alarm})
	if err != nil {
		t.Fatalf("ReportAlarm transport: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	got := rep.lastAlarm()
	if got == nil || got.GetEventId() != "run-1/evt-1" || got.GetCameraId() != "cam-1" {
		t.Errorf("adapter received alarm = %+v", got)
	}
}

func TestReportAlarmTypedError(t *testing.T) {
	rep := &recordingReportAdapter{err: NewAdapterError("DUP_EVENT", "duplicate")}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	resp, err := rc.ReportAlarm(callCtx(t), &aivisionv1.ReportAlarmRequest{Alarm: &aivisionv1.AlarmEvent{EventId: "x"}})
	if err != nil {
		t.Fatalf("ReportAlarm transport: %v", err)
	}
	if resp.GetCode() != "DUP_EVENT" {
		t.Errorf("code = %q, want DUP_EVENT", resp.GetCode())
	}
}

func TestReportAlarmInternalError(t *testing.T) {
	rep := &recordingReportAdapter{err: errors.New("database failed")}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	resp, err := rc.ReportAlarm(callCtx(t), &aivisionv1.ReportAlarmRequest{Alarm: &aivisionv1.AlarmEvent{}})
	if err != nil {
		t.Fatalf("ReportAlarm transport: %v", err)
	}
	if resp.GetCode() != CodeInternalError {
		t.Errorf("code = %q, want INTERNAL_ERROR", resp.GetCode())
	}
	if rep.alarmCount() != 0 {
		t.Errorf("adapter should not have stored the alarm on error")
	}
}

// TestReportAlarmTransportError transport status 保持原样，不能写成响应内 INTERNAL_ERROR。
func TestReportAlarmTransportError(t *testing.T) {
	rep := &recordingReportAdapter{err: status.Error(codes.Unavailable, "downstream unavailable")}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	_, err := rc.ReportAlarm(callCtx(t), &aivisionv1.ReportAlarmRequest{Alarm: &aivisionv1.AlarmEvent{}})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ReportAlarm status = %v, want Unavailable", status.Code(err))
	}
}

// TestReportAlarmEmptyCodeFailsClosed typed adapter error 的空 code 不能形成成功 ACK。
func TestReportAlarmEmptyCodeFailsClosed(t *testing.T) {
	rep := &recordingReportAdapter{err: &AdapterError{ErrorMessage: "missing business code"}}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	resp, err := rc.ReportAlarm(callCtx(t), &aivisionv1.ReportAlarmRequest{Alarm: &aivisionv1.AlarmEvent{}})
	if err != nil {
		t.Fatalf("ReportAlarm transport: %v", err)
	}
	if resp.GetCode() != CodeInternalError {
		t.Fatalf("code = %q, want INTERNAL_ERROR", resp.GetCode())
	}
}

func TestReportTaskStateSuccess(t *testing.T) {
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	state := &aivisionv1.TaskState{CameraId: "cam-2", Status: aivisionv1.TaskStatusCode_TASK_STATUS_RUNNING}
	resp, err := rc.ReportTaskState(callCtx(t), &aivisionv1.ReportTaskStateRequest{TaskState: state})
	if err != nil {
		t.Fatalf("ReportTaskState: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	if got := rep.lastTaskState(); got == nil || got.GetCameraId() != "cam-2" ||
		got.GetStatus() != aivisionv1.TaskStatusCode_TASK_STATUS_RUNNING {
		t.Errorf("adapter received task_state = %+v", got)
	}
}

func TestReportInstanceStateSuccess(t *testing.T) {
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	state := &aivisionv1.InstanceState{InstanceId: "inst-9", Status: aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING, CurrentFps: 25.5}
	resp, err := rc.ReportInstanceState(callCtx(t), &aivisionv1.ReportInstanceStateRequest{InstanceState: state})
	if err != nil {
		t.Fatalf("ReportInstanceState: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	if got := rep.lastInstanceState(); got == nil || got.GetInstanceId() != "inst-9" ||
		got.GetStatus() != aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING {
		t.Errorf("adapter received instance_state = %+v", got)
	}
}

func TestReportMetricsSuccess(t *testing.T) {
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	tel := &aivisionv1.DeviceTelemetry{UptimeSeconds: 123, CpuUsagePercent: 10.5}
	resp, err := rc.ReportMetrics(callCtx(t), &aivisionv1.ReportMetricsRequest{Telemetry: tel})
	if err != nil {
		t.Fatalf("ReportMetrics: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	if got := rep.lastMetrics(); got == nil || got.GetUptimeSeconds() != 123 || got.GetCpuUsagePercent() != 10.5 {
		t.Errorf("adapter received telemetry = %+v", got)
	}
}

func TestReportOrphanImagesSuccess(t *testing.T) {
	rep := &recordingReportAdapter{disposition: OrphanDisposition{
		RetainImageIDs: []string{"keep-1"},
		DeleteImageIDs: []string{"del-1", "del-2"},
	}}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	orphans := []*aivisionv1.OrphanImageEntry{{EventId: "r/e1", ImageId: "img-1"}}
	resp, err := rc.ReportOrphanImages(callCtx(t), &aivisionv1.ReportOrphanImagesRequest{OrphanImages: orphans})
	if err != nil {
		t.Fatalf("ReportOrphanImages: %v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty", resp.GetCode())
	}
	if len(resp.GetRetainImageIds()) != 1 || resp.GetRetainImageIds()[0] != "keep-1" {
		t.Errorf("retain = %v", resp.GetRetainImageIds())
	}
	if len(resp.GetDeleteImageIds()) != 2 || resp.GetDeleteImageIds()[1] != "del-2" {
		t.Errorf("delete = %v", resp.GetDeleteImageIds())
	}
	if rep.orphanReportCount() != 1 {
		t.Errorf("adapter orphan report count = %d, want 1", rep.orphanReportCount())
	}
}

// TestReportMissingPayload 缺少必填嵌套 payload 时返回 InvalidArgument，且不调用 adapter。
func TestReportMissingPayload(t *testing.T) {
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, &recordingDesiredStateAdapter{}, rep)
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	ctx := callCtx(t)
	// ReportAlarm 缺 alarm
	_, err := rc.ReportAlarm(ctx, &aivisionv1.ReportAlarmRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ReportAlarm missing payload: status = %v, want InvalidArgument", err)
	}
	// ReportTaskState 缺 task_state
	_, err = rc.ReportTaskState(ctx, &aivisionv1.ReportTaskStateRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ReportTaskState missing payload: status = %v, want InvalidArgument", err)
	}
	// ReportInstanceState 缺 instance_state
	_, err = rc.ReportInstanceState(ctx, &aivisionv1.ReportInstanceStateRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ReportInstanceState missing payload: status = %v, want InvalidArgument", err)
	}
	// ReportMetrics 缺 telemetry
	_, err = rc.ReportMetrics(ctx, &aivisionv1.ReportMetricsRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ReportMetrics missing payload: status = %v, want InvalidArgument", err)
	}
	if rep.alarmCount() != 0 || rep.orphanReportCount() != 0 {
		t.Errorf("adapter should not be called for missing payload")
	}
}

// TestReportUnavailableFailClosed 未注入 adapter 时全部上报 RPC fail closed 返回 IPC_UNAVAILABLE。
func TestReportUnavailableFailClosed(t *testing.T) {
	rt := startTestRuntime(t, UnavailableDesiredStateAdapter(), UnavailableReportAdapter())
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	ctx := callCtx(t)
	if resp, err := rc.ReportAlarm(ctx, &aivisionv1.ReportAlarmRequest{Alarm: &aivisionv1.AlarmEvent{}}); err != nil || resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("ReportAlarm unavailable: resp=%+v err=%v", resp, err)
	}
	if resp, err := rc.ReportTaskState(ctx, &aivisionv1.ReportTaskStateRequest{TaskState: &aivisionv1.TaskState{}}); err != nil || resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("ReportTaskState unavailable: resp=%+v err=%v", resp, err)
	}
	if resp, err := rc.ReportInstanceState(ctx, &aivisionv1.ReportInstanceStateRequest{InstanceState: &aivisionv1.InstanceState{}}); err != nil || resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("ReportInstanceState unavailable: resp=%+v err=%v", resp, err)
	}
	if resp, err := rc.ReportMetrics(ctx, &aivisionv1.ReportMetricsRequest{Telemetry: &aivisionv1.DeviceTelemetry{}}); err != nil || resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("ReportMetrics unavailable: resp=%+v err=%v", resp, err)
	}
	if resp, err := rc.ReportOrphanImages(ctx, &aivisionv1.ReportOrphanImagesRequest{}); err != nil || resp.GetCode() != CodeIPCUNAVAILABLE {
		t.Errorf("ReportOrphanImages unavailable: resp=%+v err=%v", resp, err)
	}
}

// TestReportOrphanImagesUnavailable 孤儿图片对账失败时不得返回空 code（防止 Engine 误删图片）。
func TestReportOrphanImagesUnavailable(t *testing.T) {
	rt := startTestRuntime(t, UnavailableDesiredStateAdapter(), UnavailableReportAdapter())
	conn, rc, _ := dialReportClients(t, rt)
	defer conn.Close()

	resp, err := rc.ReportOrphanImages(callCtx(t), &aivisionv1.ReportOrphanImagesRequest{})
	if err != nil {
		t.Fatalf("ReportOrphanImages transport: %v", err)
	}
	if resp.GetCode() == CodeOK {
		t.Fatal("orphan images must not ACK when adapter unavailable")
	}
}

// TestPanicRecovery adapter panic 被 recovery interceptor 转成 codes.Internal，server 继续存活。
func TestPanicRecovery(t *testing.T) {
	ds := &recordingDesiredStateAdapter{panic: true}
	rep := &recordingReportAdapter{}
	rt := startTestRuntime(t, ds, rep)
	conn, rc, cp := dialReportClients(t, rt)
	defer conn.Close()

	// panic adapter：GetDesiredState 返回 Internal（由 recovery 归一化，而非 transport 断开）。
	_, err := cp.GetDesiredState(callCtx(t), &aivisionv1.GetDesiredStateRequest{})
	if status.Code(err) != codes.Internal {
		t.Errorf("panic status = %v, want Internal", err)
	}

	// 同一连接继续可用：Report 路径（不 panic 的 adapter）必须成功。
	resp, err := rc.ReportMetrics(callCtx(t), &aivisionv1.ReportMetricsRequest{Telemetry: &aivisionv1.DeviceTelemetry{}})
	if err != nil {
		t.Fatalf("server should stay alive after panic, transport err=%v", err)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("code = %q, want empty after panic recovery", resp.GetCode())
	}
}
