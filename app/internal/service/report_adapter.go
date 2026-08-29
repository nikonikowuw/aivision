package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"niko-vue-admin/app/internal/pkg/engineipc"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
)

// TaskRuntimeState 任务实时运行状态（驻内存，供列表状态合并读取，design D6）。
type TaskRuntimeState struct {
	Status      int8
	Message     string
	LastFrameAt *time.Time
	ReportedAt  time.Time
}

// InstanceRuntimeState 实例实时运行状态。
type InstanceRuntimeState struct {
	Status     int8
	Message    string
	CurrentFps float32
	ReportedAt time.Time
}

// ReportAdapter 实现 engineipc.ReportAdapter：
//   - AcceptTaskState / AcceptInstanceState 更新内存缓存，仅状态码变化时落库（D6）；
//   - AcceptAlarm / AcceptMetrics / ReconcileOrphanImages 保持 fail-closed。
//
// 说明：engineipc.unavailableReportAdapter 类型未导出，无法嵌入复用其默认实现，
// 因此三个未实现方法显式返回 IPC_UNAVAILABLE，与 unavailable 语义完全一致
// （PRD Non-Goals：告警落库/遥测/孤儿图片对账属后续任务）。
type ReportAdapter struct {
	repo repository.TaskRepository
	log  *zap.Logger

	mu    sync.RWMutex
	tasks map[string]TaskRuntimeState     // camera_id → 实时状态
	insts map[string]InstanceRuntimeState // instance_id → 实时状态

	// State reports for the same category share a persistence order. The lock
	// covers cache mutation and the corresponding repository write so a newer
	// report cannot be overwritten by an older in-flight write or rollback.
	taskUpdateMu     sync.Mutex
	instanceUpdateMu sync.Mutex
}

// NewReportAdapter 创建 ReportAdapter。
func NewReportAdapter(repo repository.TaskRepository, log *zap.Logger) *ReportAdapter {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReportAdapter{
		repo:  repo,
		log:   log,
		tasks: make(map[string]TaskRuntimeState),
		insts: make(map[string]InstanceRuntimeState),
	}
}

// AcceptTaskState 缓存任务实时状态；仅状态码或状态消息变化时写库（D6：16 路 × 每 2 秒全量
// 上报下，FPS/最后帧/上报时间等高频字段不逐条落库）。
// 落库失败时把内存状态码回退为上一值并返回错误（非空 code，Engine 下轮重试），
// 保证「变化」在下一次上报时仍被判定为变化而再次尝试落库。
func (a *ReportAdapter) AcceptTaskState(ctx context.Context, state *aivisionv1.TaskState) error {
	if state == nil {
		return errors.New("task state is nil")
	}
	cameraID := state.GetCameraId()
	if cameraID == "" {
		return errors.New("task state camera_id is empty")
	}

	a.taskUpdateMu.Lock()
	defer a.taskUpdateMu.Unlock()

	status := int8(state.GetStatus())
	now := time.Now()

	a.mu.Lock()
	prev, existed := a.tasks[cameraID]
	changed := !existed || prev.Status != status || prev.Message != state.GetMessage()
	a.tasks[cameraID] = TaskRuntimeState{
		Status:      status,
		Message:     state.GetMessage(),
		LastFrameAt: timeFromWallNs(state.GetLastFrameWallTimeNs()),
		ReportedAt:  now,
	}
	a.mu.Unlock()

	if !changed {
		return nil
	}
	if err := a.repo.UpdateTaskStatus(ctx, cameraID, status, state.GetMessage()); err != nil {
		a.log.Warn("update task status failed", zap.String("camera_id", cameraID), zap.Error(err))
		a.mu.Lock()
		if rt, ok := a.tasks[cameraID]; ok {
			if existed {
				rt.Status = prev.Status
				rt.Message = prev.Message
				a.tasks[cameraID] = rt
			} else {
				delete(a.tasks, cameraID)
			}
		}
		a.mu.Unlock()
		return err
	}
	return nil
}

// AcceptInstanceState 缓存实例实时状态；落库规则同 AcceptTaskState。
func (a *ReportAdapter) AcceptInstanceState(ctx context.Context, state *aivisionv1.InstanceState) error {
	if state == nil {
		return errors.New("instance state is nil")
	}
	instanceID := state.GetInstanceId()
	if instanceID == "" {
		return errors.New("instance state instance_id is empty")
	}

	a.instanceUpdateMu.Lock()
	defer a.instanceUpdateMu.Unlock()

	status := int8(state.GetStatus())
	now := time.Now()

	a.mu.Lock()
	prev, existed := a.insts[instanceID]
	changed := !existed || prev.Status != status || prev.Message != state.GetMessage()
	a.insts[instanceID] = InstanceRuntimeState{
		Status:     status,
		Message:    state.GetMessage(),
		CurrentFps: state.GetCurrentFps(),
		ReportedAt: now,
	}
	a.mu.Unlock()

	if !changed {
		return nil
	}
	if err := a.repo.UpdateInstanceStatus(ctx, instanceID, status, state.GetMessage()); err != nil {
		a.log.Warn("update instance status failed", zap.String("instance_id", instanceID), zap.Error(err))
		a.mu.Lock()
		if rt, ok := a.insts[instanceID]; ok {
			if existed {
				rt.Status = prev.Status
				rt.Message = prev.Message
				a.insts[instanceID] = rt
			} else {
				delete(a.insts, instanceID)
			}
		}
		a.mu.Unlock()
		return err
	}
	return nil
}

// AcceptAlarm 告警落库属后续任务（PRD Non-Goals），保持 fail-closed，
// 禁止对未持久化的告警返回成功 ACK（engineipc 稳定错误契约）。
func (a *ReportAdapter) AcceptAlarm(context.Context, *aivisionv1.AlarmEvent) error {
	return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "report service unavailable")
}

// AcceptMetrics 设备遥测时序落库属后续任务，语义同 AcceptAlarm。
func (a *ReportAdapter) AcceptMetrics(context.Context, *aivisionv1.DeviceTelemetry) error {
	return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "report service unavailable")
}

// ReconcileOrphanImages 孤儿图片对账属后续任务，语义同 AcceptAlarm。
// 失败时不得返回空 code，否则 Engine 会删除图片（quality-guidelines）。
func (a *ReportAdapter) ReconcileOrphanImages(context.Context, []*aivisionv1.OrphanImageEntry) (engineipc.OrphanDisposition, error) {
	return engineipc.OrphanDisposition{}, engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "report service unavailable")
}

// TaskRuntime 返回 camera_id 对应的内存实时状态；未上报过返回 ok=false。
func (a *ReportAdapter) TaskRuntime(cameraID string) (TaskRuntimeState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state, ok := a.tasks[cameraID]
	return state, ok
}

// InstanceRuntime 返回 instance_id 对应的内存实时状态；未上报过返回 ok=false。
func (a *ReportAdapter) InstanceRuntime(instanceID string) (InstanceRuntimeState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state, ok := a.insts[instanceID]
	return state, ok
}

// timeFromWallNs 把 Engine 的 wall-clock 纳秒转成时间指针；非正值视为未上报（nil）。
func timeFromWallNs(ns int64) *time.Time {
	if ns <= 0 {
		return nil
	}
	t := time.Unix(0, ns)
	return &t
}
