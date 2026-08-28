package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/engineipc"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
)

// countingTaskRepo 包装真实 TaskRepository，统计状态落库调用次数，
// 用于断言 D6「状态码未变零数据库写入」。
type countingTaskRepo struct {
	repository.TaskRepository

	mu               sync.Mutex
	taskStatusWrites int
	instStatusWrites int
}

func (c *countingTaskRepo) UpdateTaskStatus(ctx context.Context, cameraID string, status int8, msg string) error {
	c.mu.Lock()
	c.taskStatusWrites++
	c.mu.Unlock()
	return c.TaskRepository.UpdateTaskStatus(ctx, cameraID, status, msg)
}

func (c *countingTaskRepo) UpdateInstanceStatus(ctx context.Context, instanceID string, status int8, msg string) error {
	c.mu.Lock()
	c.instStatusWrites++
	c.mu.Unlock()
	return c.TaskRepository.UpdateInstanceStatus(ctx, instanceID, status, msg)
}

func (c *countingTaskRepo) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.taskStatusWrites, c.instStatusWrites
}

// TestReportAdapterWritesOnlyOnStatusChange 状态码变化落库、未变零写入（D6），
// 内存实时字段（current_fps/last_frame_at/reported_at）每次上报都刷新。
func TestReportAdapterWritesOnlyOnStatusChange(t *testing.T) {
	db := newDesiredStateTestDB(t)
	counting := &countingTaskRepo{TaskRepository: repository.NewTaskRepository(db)}
	adapter := NewReportAdapter(counting, zap.NewNop())
	ctx := context.Background()

	// 实例状态：UNSPECIFIED → RUNNING → RUNNING（同码）→ STOPPED。
	if err := adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
		InstanceId: "i1", Status: aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING,
		Message: "ok", CurrentFps: 12.5,
	}); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if err := adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
		InstanceId: "i1", Status: aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING,
		Message: "ok", CurrentFps: 13.5,
	}); err != nil {
		t.Fatalf("second accept: %v", err)
	}
	if err := adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
		InstanceId: "i1", Status: aivisionv1.InstanceStatusCode_INSTANCE_STATUS_STOPPED,
		Message: "stopped", CurrentFps: 0,
	}); err != nil {
		t.Fatalf("third accept: %v", err)
	}
	// 任务状态：STARTING → STARTING（同码）。
	if err := adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{
		CameraId: "cam-a", Status: aivisionv1.TaskStatusCode_TASK_STATUS_STARTING,
		Message: "starting", LastFrameWallTimeNs: 0,
	}); err != nil {
		t.Fatalf("accept task state: %v", err)
	}
	if err := adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{
		CameraId: "cam-a", Status: aivisionv1.TaskStatusCode_TASK_STATUS_STARTING,
		Message: "starting", LastFrameWallTimeNs: 1000,
	}); err != nil {
		t.Fatalf("accept task state again: %v", err)
	}

	taskWrites, instWrites := counting.counts()
	if instWrites != 2 {
		t.Errorf("instance status writes = %d, want 2 (RUNNING, STOPPED)", instWrites)
	}
	if taskWrites != 1 {
		t.Errorf("task status writes = %d, want 1 (STARTING)", taskWrites)
	}

	// 内存访问器：实时字段为最近一次上报值。
	rt, ok := adapter.InstanceRuntime("i1")
	if !ok {
		t.Fatal("instance runtime state missing")
	}
	if rt.Status != int8(aivisionv1.InstanceStatusCode_INSTANCE_STATUS_STOPPED) || rt.CurrentFps != 0 {
		t.Fatalf("instance runtime = %+v", rt)
	}
	if time.Since(rt.ReportedAt) > time.Second {
		t.Fatalf("reportedAt stale: %v", rt.ReportedAt)
	}
	tt, ok := adapter.TaskRuntime("cam-a")
	if !ok {
		t.Fatal("task runtime state missing")
	}
	if tt.LastFrameAt == nil || tt.LastFrameAt.UnixNano() != 1000 {
		t.Fatalf("task lastFrameAt = %+v", tt.LastFrameAt)
	}
	if _, ok := adapter.InstanceRuntime("unknown"); ok {
		t.Fatal("unknown instance runtime should be absent")
	}
}

// TestReportAdapterStatusPersisted 状态码变化时确实落库（库中 status/message 更新）。
func TestReportAdapterStatusPersisted(t *testing.T) {
	db := newDesiredStateTestDB(t)
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	if err := db.Create(&model.AnalysisTask{CameraID: "cam-a", Name: "t", DesiredEnabled: true, ActualStatus: model.TaskStatusStarting}).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := db.Create(&model.AlgorithmInstance{InstanceID: "i1", CameraID: "cam-a", AlgorithmID: "a", AnalysisFPS: 5, ParamsJSON: []byte(`{}`), RulesJSON: []byte(`[]`), Enabled: true, ActualStatus: model.InstanceStatusStarting}).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}

	adapter := NewReportAdapter(repo, zap.NewNop())
	if err := adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{
		CameraId: "cam-a", Status: aivisionv1.TaskStatusCode_TASK_STATUS_RUNNING, Message: "running",
	}); err != nil {
		t.Fatalf("accept task state: %v", err)
	}
	if err := adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
		InstanceId: "i1", Status: aivisionv1.InstanceStatusCode_INSTANCE_STATUS_ERROR, Message: "oom",
	}); err != nil {
		t.Fatalf("accept instance state: %v", err)
	}

	task, err := repo.GetTaskByCameraID(ctx, "cam-a")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.ActualStatus != model.TaskStatusRunning || task.StatusMessage != "running" {
		t.Fatalf("task status = %d/%q", task.ActualStatus, task.StatusMessage)
	}
	inst, err := repo.GetInstance(ctx, "i1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if inst.ActualStatus != model.InstanceStatusError || inst.StatusMessage != "oom" {
		t.Fatalf("instance status = %d/%q", inst.ActualStatus, inst.StatusMessage)
	}
}

// TestReportAdapterUnimplementedFailClosed 三个未实现方法必须返回 IPC_UNAVAILABLE，
// 与 engineipc unavailable 语义一致（engineipc.unavailableReportAdapter 未导出，无法嵌入）。
func TestReportAdapterUnimplementedFailClosed(t *testing.T) {
	db := newDesiredStateTestDB(t)
	adapter := NewReportAdapter(repository.NewTaskRepository(db), zap.NewNop())

	if err := adapter.AcceptAlarm(context.Background(), &aivisionv1.AlarmEvent{}); !isAdapterUnavailable(err) {
		t.Errorf("AcceptAlarm err = %v, want IPC_UNAVAILABLE", err)
	}
	if err := adapter.AcceptMetrics(context.Background(), &aivisionv1.DeviceTelemetry{}); !isAdapterUnavailable(err) {
		t.Errorf("AcceptMetrics err = %v, want IPC_UNAVAILABLE", err)
	}
	if _, err := adapter.ReconcileOrphanImages(context.Background(), nil); !isAdapterUnavailable(err) {
		t.Errorf("ReconcileOrphanImages err = %v, want IPC_UNAVAILABLE", err)
	}
}

func isAdapterUnavailable(err error) bool {
	var ae *engineipc.AdapterError
	return errors.As(err, &ae) && ae != nil && ae.Code == engineipc.CodeIPCUNAVAILABLE
}

// TestReportAdapterRejectsEmptyIDs 空标识的状态上报按内部错误处理，不得返回成功 ACK。
func TestReportAdapterRejectsEmptyIDs(t *testing.T) {
	db := newDesiredStateTestDB(t)
	adapter := NewReportAdapter(repository.NewTaskRepository(db), zap.NewNop())
	ctx := context.Background()

	if err := adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{}); err == nil {
		t.Error("empty camera_id task state unexpectedly accepted")
	}
	if err := adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{}); err == nil {
		t.Error("empty instance_id instance state unexpectedly accepted")
	}
}
