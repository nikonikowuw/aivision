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

// blockingReportRepo makes the first status write wait until the test releases it.
// It exposes the second call so the tests can assert that same-entity reports do
// not enter persistence concurrently.
type blockingReportRepo struct {
	repository.TaskRepository

	taskMu            sync.Mutex
	taskCalls         int
	taskCompleted     []int8
	taskFirstErr      error
	taskFirstStarted  chan struct{}
	taskSecondStarted chan struct{}
	taskReleaseFirst  chan struct{}

	instMu            sync.Mutex
	instCalls         int
	instCompleted     []int8
	instFirstErr      error
	instFirstStarted  chan struct{}
	instSecondStarted chan struct{}
	instReleaseFirst  chan struct{}
}

func newBlockingReportRepo(firstErr error) *blockingReportRepo {
	return &blockingReportRepo{
		taskFirstErr:      firstErr,
		taskFirstStarted:  make(chan struct{}),
		taskSecondStarted: make(chan struct{}),
		taskReleaseFirst:  make(chan struct{}),
		instFirstErr:      firstErr,
		instFirstStarted:  make(chan struct{}),
		instSecondStarted: make(chan struct{}),
		instReleaseFirst:  make(chan struct{}),
	}
}

func (r *blockingReportRepo) UpdateTaskStatus(_ context.Context, _ string, status int8, _ string) error {
	r.taskMu.Lock()
	r.taskCalls++
	call := r.taskCalls
	if call == 1 {
		close(r.taskFirstStarted)
	}
	if call == 2 {
		close(r.taskSecondStarted)
	}
	r.taskMu.Unlock()

	if call == 1 {
		<-r.taskReleaseFirst
	}

	r.taskMu.Lock()
	r.taskCompleted = append(r.taskCompleted, status)
	r.taskMu.Unlock()
	if call == 1 {
		return r.taskFirstErr
	}
	return nil
}

func (r *blockingReportRepo) UpdateInstanceStatus(_ context.Context, _ string, status int8, _ string) error {
	r.instMu.Lock()
	r.instCalls++
	call := r.instCalls
	if call == 1 {
		close(r.instFirstStarted)
	}
	if call == 2 {
		close(r.instSecondStarted)
	}
	r.instMu.Unlock()

	if call == 1 {
		<-r.instReleaseFirst
	}

	r.instMu.Lock()
	r.instCompleted = append(r.instCompleted, status)
	r.instMu.Unlock()
	if call == 1 {
		return r.instFirstErr
	}
	return nil
}

func (r *blockingReportRepo) taskCompletedStatuses() []int8 {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	return append([]int8(nil), r.taskCompleted...)
}

func (r *blockingReportRepo) instanceCompletedStatuses() []int8 {
	r.instMu.Lock()
	defer r.instMu.Unlock()
	return append([]int8(nil), r.instCompleted...)
}

// waitReportResult waits with a bound so a broken serialization implementation
// cannot leave the test process blocked forever.
func waitReportResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("report state update did not complete")
		return nil
	}
}

// TestReportAdapterSerializesTaskUpdates ensures an older failed write cannot
// roll back a newer task state or leave the database write order reversed.
func TestReportAdapterSerializesTaskUpdates(t *testing.T) {
	firstErr := errors.New("first task write failed")
	repo := newBlockingReportRepo(firstErr)
	adapter := NewReportAdapter(repo, zap.NewNop())
	ctx := context.Background()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{
			CameraId: "cam-concurrent",
			Status:   aivisionv1.TaskStatusCode_TASK_STATUS_RUNNING,
		})
	}()
	<-repo.taskFirstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- adapter.AcceptTaskState(ctx, &aivisionv1.TaskState{
			CameraId: "cam-concurrent",
			Status:   aivisionv1.TaskStatusCode_TASK_STATUS_STOPPED,
		})
	}()

	select {
	case <-repo.taskSecondStarted:
		t.Errorf("second task status entered repository before first completed")
	case <-time.After(250 * time.Millisecond):
	}
	close(repo.taskReleaseFirst)

	if err := waitReportResult(t, firstDone); !errors.Is(err, firstErr) {
		t.Errorf("first task update error = %v, want %v", err, firstErr)
	}
	if err := waitReportResult(t, secondDone); err != nil {
		t.Fatalf("second task update: %v", err)
	}

	statuses := repo.taskCompletedStatuses()
	if len(statuses) != 2 || statuses[0] != int8(aivisionv1.TaskStatusCode_TASK_STATUS_RUNNING) || statuses[1] != int8(aivisionv1.TaskStatusCode_TASK_STATUS_STOPPED) {
		t.Fatalf("task persistence order = %v, want [RUNNING STOPPED]", statuses)
	}
	rt, ok := adapter.TaskRuntime("cam-concurrent")
	if !ok || rt.Status != int8(aivisionv1.TaskStatusCode_TASK_STATUS_STOPPED) {
		t.Fatalf("task runtime = %+v, present=%v, want final STOPPED", rt, ok)
	}
}

// TestReportAdapterSerializesInstanceUpdates is the instance equivalent of the
// task test; both paths must protect their own entity update order.
func TestReportAdapterSerializesInstanceUpdates(t *testing.T) {
	firstErr := errors.New("first instance write failed")
	repo := newBlockingReportRepo(firstErr)
	adapter := NewReportAdapter(repo, zap.NewNop())
	ctx := context.Background()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
			InstanceId: "inst-concurrent",
			Status:     aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING,
		})
	}()
	<-repo.instFirstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- adapter.AcceptInstanceState(ctx, &aivisionv1.InstanceState{
			InstanceId: "inst-concurrent",
			Status:     aivisionv1.InstanceStatusCode_INSTANCE_STATUS_STOPPED,
		})
	}()

	select {
	case <-repo.instSecondStarted:
		t.Errorf("second instance status entered repository before first completed")
	case <-time.After(250 * time.Millisecond):
	}
	close(repo.instReleaseFirst)

	if err := waitReportResult(t, firstDone); !errors.Is(err, firstErr) {
		t.Errorf("first instance update error = %v, want %v", err, firstErr)
	}
	if err := waitReportResult(t, secondDone); err != nil {
		t.Fatalf("second instance update: %v", err)
	}

	statuses := repo.instanceCompletedStatuses()
	if len(statuses) != 2 || statuses[0] != int8(aivisionv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING) || statuses[1] != int8(aivisionv1.InstanceStatusCode_INSTANCE_STATUS_STOPPED) {
		t.Fatalf("instance persistence order = %v, want [RUNNING STOPPED]", statuses)
	}
	rt, ok := adapter.InstanceRuntime("inst-concurrent")
	if !ok || rt.Status != int8(aivisionv1.InstanceStatusCode_INSTANCE_STATUS_STOPPED) {
		t.Fatalf("instance runtime = %+v, present=%v, want final STOPPED", rt, ok)
	}
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
