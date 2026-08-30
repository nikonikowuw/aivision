package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

const (
	// orphanRetentionGracePeriod 孤儿图片保留保护期（5分钟），防止刚生成的图片被误删。
	orphanRetentionGracePeriod = 5 * time.Minute
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
//   - AcceptAlarm 幂等持久化到 alarm_records 表；
//   - ReconcileOrphanImages 针对已落库图片保留（retain），超期未落库图片删除（delete）；
//   - AcceptMetrics 保持 fail-closed。
type ReportAdapter struct {
	repo      repository.TaskRepository
	alarmRepo repository.AlarmRecordRepository
	log       *zap.Logger

	mu    sync.RWMutex
	tasks map[string]TaskRuntimeState     // camera_id → 实时状态
	insts map[string]InstanceRuntimeState // instance_id → 实时状态

	// State reports for the same category share a persistence order. The lock
	// covers cache mutation and the corresponding repository write so a newer
	// report cannot be overwritten by an older in-flight write or rollback.
	taskUpdateMu     sync.Mutex
	instanceUpdateMu sync.Mutex
}

// NewReportAdapter 创建 ReportAdapter（保持原有两参数构造函数签名，兼容既有单测与调用者）。
func NewReportAdapter(repo repository.TaskRepository, log *zap.Logger) *ReportAdapter {
	return NewReportAdapterWithAlarm(repo, nil, log)
}

// NewReportAdapterWithAlarm 具备告警落库与孤儿图片对账能力的完整构造函数。
func NewReportAdapterWithAlarm(
	repo repository.TaskRepository,
	alarmRepo repository.AlarmRecordRepository,
	log *zap.Logger,
) *ReportAdapter {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReportAdapter{
		repo:      repo,
		alarmRepo: alarmRepo,
		log:       log,
		tasks:     make(map[string]TaskRuntimeState),
		insts:     make(map[string]InstanceRuntimeState),
	}
}

// AcceptTaskState 缓存任务实时状态；仅状态码或状态消息变化时写库（D6：16 路 × 每 2 秒全量
// 上报下，FPS/最后帧/上报时间等高频字段不逐条落库）。
// 落库失败时把内存状态码回退为上一值并返回错误（非空 code，Engine 下轮重试），
// 保证「变化」在下一次上报时仍被判定为变化而再次尝试落库。
func (a *ReportAdapter) AcceptTaskState(ctx context.Context, state *argusv1.TaskState) error {
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
func (a *ReportAdapter) AcceptInstanceState(ctx context.Context, state *argusv1.InstanceState) error {
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

// AcceptAlarm 告警事件持久化：根据 event_id 幂等落库。
func (a *ReportAdapter) AcceptAlarm(ctx context.Context, event *argusv1.AlarmEvent) error {
	if a.alarmRepo == nil {
		return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "alarm report service unavailable")
	}
	if event == nil {
		return errors.New("alarm event is nil")
	}
	eventID := strings.TrimSpace(event.GetEventId())
	if eventID == "" {
		return errors.New("alarm event_id is empty")
	}

	// 检查图片相对路径安全性（防止极端注入）
	imgRelPath := event.GetImageRelPath()
	if imgRelPath != "" {
		cleanPath := filepath.Clean(imgRelPath)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			return errors.New("alarm image_rel_path is invalid")
		}
	}

	// 提取单个检测目标信息（1 Target = 1 Record）
	var targetLabel string
	var confidence float32
	var trackID int64
	var bbox []float32

	if len(event.GetObjects()) > 0 && event.GetObjects()[0] != nil {
		firstObj := event.GetObjects()[0]
		targetLabel = firstObj.GetLabel()
		confidence = firstObj.GetConfidence()
		trackID = firstObj.GetTrackId()
		if pbBBox := firstObj.GetBbox(); pbBBox != nil {
			bbox = []float32{pbBBox.GetXMin(), pbBBox.GetYMin(), pbBBox.GetXMax(), pbBBox.GetYMax()}
		}
	}

	bboxJSON, err := json.Marshal(bbox)
	if err != nil {
		return fmt.Errorf("marshal alarm bbox: %w", err)
	}

	occurredAt := time.Now()
	if event.GetWallTimeNs() > 0 {
		occurredAt = time.Unix(0, event.GetWallTimeNs())
	}

	record := &model.AlarmRecord{
		EventID:          eventID,
		InstanceID:       event.GetInstanceId(),
		CameraID:         event.GetCameraId(),
		AlgorithmID:      event.GetAlgorithmId(),
		AlgorithmVersion: event.GetAlgorithmVersion(),
		AlarmTypeID:      event.GetAlarmTypeId(),
		OccurredAt:       occurredAt,
		TimeSynced:       event.GetTimeSynced(),
		TargetLabel:      targetLabel,
		Confidence:       confidence,
		TrackID:          trackID,
		BBoxJSON:         bboxJSON,
		ImageID:          event.GetImageId(),
		ImageRelPath:     imgRelPath,
	}

	if err := a.alarmRepo.Create(ctx, record); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			// 幂等处理：若 event_id 已存在，视为成功处理
			a.log.Info("duplicate alarm event received, treated as idempotent success", zap.String("event_id", eventID))
			return nil
		}
		a.log.Error("persist alarm record failed", zap.String("event_id", eventID), zap.Error(err))
		return err
	}

	return nil
}

// AcceptMetrics 设备遥测时序落库属后续任务，语义保持 fail-closed。
func (a *ReportAdapter) AcceptMetrics(context.Context, *argusv1.DeviceTelemetry) error {
	return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "metrics report service unavailable")
}

// ReconcileOrphanImages 孤儿图片对账：
// 1. 批量反查数据库中的 image_id；
// 2. 命中者放入 RetainImageIDs；
// 3. 未命中且生成时间超过保护期（5分钟）放入 DeleteImageIDs；
// 4. 未命中但在保护期内的图片不做处理（等待下轮对账或落库）。
func (a *ReportAdapter) ReconcileOrphanImages(ctx context.Context, entries []*argusv1.OrphanImageEntry) (engineipc.OrphanDisposition, error) {
	if a.alarmRepo == nil {
		return engineipc.OrphanDisposition{}, engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "reconcile orphan images service unavailable")
	}

	if len(entries) == 0 {
		return engineipc.OrphanDisposition{}, nil
	}

	allImageIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry != nil && entry.GetImageId() != "" {
			allImageIDs = append(allImageIDs, entry.GetImageId())
		}
	}

	existingIDs, err := a.alarmRepo.FindExistingImageIDs(ctx, allImageIDs)
	if err != nil {
		a.log.Error("find existing image ids failed during orphan reconciliation", zap.Error(err))
		return engineipc.OrphanDisposition{}, err
	}

	existingMap := make(map[string]struct{}, len(existingIDs))
	for _, id := range existingIDs {
		existingMap[id] = struct{}{}
	}

	now := time.Now()
	var retainIDs []string
	var deleteIDs []string

	for _, entry := range entries {
		if entry == nil || entry.GetImageId() == "" {
			continue
		}
		imgID := entry.GetImageId()
		if _, ok := existingMap[imgID]; ok {
			retainIDs = append(retainIDs, imgID)
		} else {
			// 未落库：检查是否超过保护期
			createdAt := time.Unix(0, entry.GetCreatedAtNs())
			if entry.GetCreatedAtNs() > 0 && now.Sub(createdAt) > orphanRetentionGracePeriod {
				deleteIDs = append(deleteIDs, imgID)
			}
		}
	}

	return engineipc.OrphanDisposition{
		RetainImageIDs: retainIDs,
		DeleteImageIDs: deleteIDs,
	}, nil
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
