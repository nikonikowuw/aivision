package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// StorageCircuitBreakerChecker 熔断器状态检测接口。
type StorageCircuitBreakerChecker interface {
	IsCircuitBreakerActive() bool
}

// ReportAdapter 实现 engineipc.ReportAdapter：
//   - AcceptTaskState / AcceptInstanceState 更新内存缓存，仅状态码变化时落库（D6）；
//   - AcceptAlarm 幂等持久化单目标告警到 alarm_records 表；
//   - AcceptPlateObservation 幂等持久化车牌抓拍过车记录到 plate_observations 表；
//   - AcceptFaceObservation 单调 upsert 人脸抓拍识别记录到 face_observations 表；
//   - ReconcileOrphanImages 针对已落库图片保留（retain），超期未落库图片删除（delete）；
//   - AcceptMetrics 保持 fail-closed。
type ReportAdapter struct {
	repo               repository.TaskRepository
	alarmRepo          repository.AlarmRecordRepository
	plateRepo          repository.PlateObservationRepository
	faceRepo           repository.FaceObservationRepository
	captureRepo        repository.FaceCaptureRepository
	genericCaptureRepo repository.CaptureRepository
	circuitBreaker     StorageCircuitBreakerChecker
	log                *zap.Logger

	mu    sync.RWMutex
	tasks map[string]TaskRuntimeState     // camera_id → 实时状态
	insts map[string]InstanceRuntimeState // instance_id → 实时状态

	// State reports for the same category share a persistence order. The lock
	// covers cache mutation and the corresponding repository write so a newer
	// report cannot be overwritten by an older in-flight write or rollback.
	taskUpdateMu     sync.Mutex
	instanceUpdateMu sync.Mutex
}

func NewReportAdapter(repo repository.TaskRepository, log *zap.Logger) *ReportAdapter {
	return NewReportAdapterWithAlarm(repo, nil, nil, nil, nil, nil, log)
}

// NewReportAdapterWithAlarm 具备告警、车牌、人脸识别和旧版人脸抓拍落库能力。
// 保留该构造函数签名以兼容既有调用方；通用抓拍由 NewReportAdapterWithCaptures 装配。
func NewReportAdapterWithAlarm(
	repo repository.TaskRepository,
	alarmRepo repository.AlarmRecordRepository,
	plateRepo repository.PlateObservationRepository,
	faceRepo repository.FaceObservationRepository,
	captureRepo repository.FaceCaptureRepository,
	circuitBreaker StorageCleanupService,
	log *zap.Logger,
) *ReportAdapter {
	return newReportAdapter(repo, alarmRepo, plateRepo, faceRepo, captureRepo, nil, circuitBreaker, log)
}

// NewReportAdapterWithCaptures 创建包含通用抓拍与旧版人脸抓拍兼容能力的上报适配器。
func NewReportAdapterWithCaptures(
	repo repository.TaskRepository,
	alarmRepo repository.AlarmRecordRepository,
	plateRepo repository.PlateObservationRepository,
	faceRepo repository.FaceObservationRepository,
	captureRepo repository.FaceCaptureRepository,
	genericCaptureRepo repository.CaptureRepository,
	circuitBreaker StorageCleanupService,
	log *zap.Logger,
) *ReportAdapter {
	return newReportAdapter(repo, alarmRepo, plateRepo, faceRepo, captureRepo, genericCaptureRepo, circuitBreaker, log)
}

func newReportAdapter(
	repo repository.TaskRepository,
	alarmRepo repository.AlarmRecordRepository,
	plateRepo repository.PlateObservationRepository,
	faceRepo repository.FaceObservationRepository,
	captureRepo repository.FaceCaptureRepository,
	genericCaptureRepo repository.CaptureRepository,
	circuitBreaker StorageCleanupService,
	log *zap.Logger,
) *ReportAdapter {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReportAdapter{
		repo:               repo,
		alarmRepo:          alarmRepo,
		plateRepo:          plateRepo,
		faceRepo:           faceRepo,
		captureRepo:        captureRepo,
		genericCaptureRepo: genericCaptureRepo,
		circuitBreaker:     circuitBreaker,
		log:                log,
		tasks:              make(map[string]TaskRuntimeState),
		insts:              make(map[string]InstanceRuntimeState),
	}
}

// SetCircuitBreakerChecker 设置熔断状态检测器（供单测与动态依赖注入）。
func (a *ReportAdapter) SetCircuitBreakerChecker(checker StorageCircuitBreakerChecker) {
	a.circuitBreaker = checker
}

func isPathSafe(p string) bool {
	if p == "" {
		return true
	}
	clean := filepath.Clean(p)
	return !strings.HasPrefix(clean, "..") && !filepath.IsAbs(clean)
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
	lastFrameAt := timeFromWallNs(state.GetLastFrameWallTimeNs())
	now := time.Now()

	a.mu.Lock()
	prev, existed := a.tasks[cameraID]
	changed := !existed || prev.Status != status || prev.Message != state.GetMessage()
	a.tasks[cameraID] = TaskRuntimeState{
		Status:      status,
		Message:     state.GetMessage(),
		LastFrameAt: lastFrameAt,
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

// AcceptInstanceState 缓存实例实时状态；仅状态码或状态消息变化时写库（D6）。
// 落库失败时把内存状态码回退为上一值并返回错误（非空 code，Engine 下轮重试）。
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

	// 检查图片相对路径安全性（防止极端注入）及 95% 极危防爆盘熔断处理
	imgID := event.GetImageId()
	imgRelPath := event.GetImageRelPath()
	if a.circuitBreaker != nil && a.circuitBreaker.IsCircuitBreakerActive() {
		a.log.Warn("storage circuit breaker active (disk usage >= 95%): dropping alarm image persistence",
			zap.String("event_id", eventID),
			zap.String("dropped_image", imgRelPath))
		imgID, imgRelPath = "", ""
	} else if !isPathSafe(imgRelPath) {
		return errors.New("alarm image_rel_path is invalid")
	}

	// 告警事件已经由 Engine fan-out 为单目标事件；Go 端拒绝多目标载荷，避免静默丢弃后续目标。
	objects := event.GetObjects()
	if len(objects) != 1 || objects[0] == nil {
		return errors.New("alarm event must contain exactly one object")
	}

	// 提取单个检测目标信息（1 Target = 1 Record）
	var targetLabel string
	var confidence float32
	var trackID int64
	var bbox []float32

	firstObj := objects[0]
	targetLabel = firstObj.GetLabel()
	confidence = firstObj.GetConfidence()
	trackID = firstObj.GetTrackId()
	if pbBBox := firstObj.GetBbox(); pbBBox != nil {
		bbox = []float32{pbBBox.GetXMin(), pbBBox.GetYMin(), pbBBox.GetXMax(), pbBBox.GetYMax()}
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
		ImageID:          imgID,
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

// AcceptPlateObservation 车牌抓拍过车记录持久化：根据 event_id 幂等落库。
func (a *ReportAdapter) AcceptPlateObservation(ctx context.Context, obs *argusv1.PlateObservation) error {
	if a.plateRepo == nil {
		return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "plate observation report service unavailable")
	}
	if obs == nil {
		return errors.New("plate observation is nil")
	}
	eventID := strings.TrimSpace(obs.GetEventId())
	if eventID == "" {
		return errors.New("plate observation event_id is empty")
	}

	// 校验图片路径安全性及 95% 极危防爆盘熔断处理
	imgID := obs.GetImageId()
	imgRelPath := obs.GetImageRelPath()
	plateImgID := obs.GetPlateImageId()
	plateImgRelPath := obs.GetPlateImageRelPath()

	if a.circuitBreaker != nil && a.circuitBreaker.IsCircuitBreakerActive() {
		a.log.Warn("storage circuit breaker active (disk usage >= 95%): dropping plate observation image persistence",
			zap.String("event_id", eventID),
			zap.String("dropped_image", imgRelPath),
			zap.String("dropped_plate_image", plateImgRelPath))
		imgID, imgRelPath = "", ""
		plateImgID, plateImgRelPath = "", ""
	} else if !isPathSafe(imgRelPath) || !isPathSafe(plateImgRelPath) {
		return errors.New("plate observation image path is invalid")
	}

	var bbox []float32
	if pbBBox := obs.GetPlateBbox(); pbBBox != nil {
		bbox = []float32{pbBBox.GetXMin(), pbBBox.GetYMin(), pbBBox.GetXMax(), pbBBox.GetYMax()}
	}
	bboxJSON, err := json.Marshal(bbox)
	if err != nil {
		return fmt.Errorf("marshal plate bbox: %w", err)
	}

	var vehicleBBox []float32
	if pbVBBox := obs.GetVehicleBbox(); pbVBBox != nil {
		vehicleBBox = []float32{pbVBBox.GetXMin(), pbVBBox.GetYMin(), pbVBBox.GetXMax(), pbVBBox.GetYMax()}
	}
	vehicleBBoxJSON, err := json.Marshal(vehicleBBox)
	if err != nil {
		return fmt.Errorf("marshal vehicle bbox: %w", err)
	}

	observedAt := time.Now()
	if obs.GetWallTimeNs() > 0 {
		observedAt = time.Unix(0, obs.GetWallTimeNs())
	}

	record := &model.PlateObservation{
		EventID:           eventID,
		InstanceID:        obs.GetInstanceId(),
		AlgorithmID:       obs.GetAlgorithmId(),
		AlgorithmVersion:  obs.GetAlgorithmVersion(),
		CameraID:          obs.GetCameraId(),
		TimeSynced:        obs.GetTimeSynced(),
		PlateText:         obs.GetPlateText(),
		NormalizedText:    obs.GetNormalizedText(),
		PlateColor:        obs.GetPlateColor(),
		PlateType:         obs.GetPlateType(),
		Confidence:        obs.GetConfidence(),
		OcrConfidence:     obs.GetOcrConfidence(),
		TrackID:           obs.GetTrackId(),
		BBoxJSON:          bboxJSON,
		VehicleBBoxJSON:   vehicleBBoxJSON,
		PanoramaImage:     imgRelPath,
		PlateImage:        plateImgRelPath,
		ImageID:           imgID,
		ImageRelPath:      imgRelPath,
		PlateImageID:      plateImgID,
		PlateImageRelPath: plateImgRelPath,
		ObservedAt:        observedAt,
	}

	if err := a.plateRepo.Create(ctx, record); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			a.log.Info("duplicate plate observation received, treated as idempotent success", zap.String("event_id", eventID))
			return nil
		}
		a.log.Error("persist plate observation failed", zap.String("event_id", eventID), zap.Error(err))
		return err
	}

	return nil
}

// AcceptFaceObservation 人脸抓拍识别记录持久化：根据 event_id 单调 upsert。
func (a *ReportAdapter) AcceptFaceObservation(ctx context.Context, obs *argusv1.FaceObservation) error {
	if a.faceRepo == nil {
		return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "face observation report service unavailable")
	}
	if obs == nil {
		return errors.New("face observation is nil")
	}
	eventID := strings.TrimSpace(obs.GetEventId())
	if eventID == "" {
		return errors.New("face observation event_id is empty")
	}

	// 校验图片路径安全性及 95% 极危防爆盘熔断处理
	imgID := obs.GetImageId()
	imgRelPath := obs.GetImageRelPath()
	faceImgID := obs.GetFaceImageId()
	faceImgRelPath := obs.GetFaceImageRelPath()

	if a.circuitBreaker != nil && a.circuitBreaker.IsCircuitBreakerActive() {
		a.log.Warn("storage circuit breaker active (disk usage >= 95%): dropping face observation image persistence",
			zap.String("event_id", eventID),
			zap.String("dropped_image", imgRelPath),
			zap.String("dropped_face_image", faceImgRelPath))
		imgID, imgRelPath = "", ""
		faceImgID, faceImgRelPath = "", ""
	} else if !isPathSafe(imgRelPath) || !isPathSafe(faceImgRelPath) {
		return errors.New("face observation image path is invalid")
	}

	var bbox []float32
	if pbBBox := obs.GetFaceBbox(); pbBBox != nil {
		bbox = []float32{pbBBox.GetXMin(), pbBBox.GetYMin(), pbBBox.GetXMax(), pbBBox.GetYMax()}
	}
	bboxJSON, err := json.Marshal(bbox)
	if err != nil {
		return fmt.Errorf("marshal face bbox: %w", err)
	}

	observedAt := time.Now()
	if obs.GetWallTimeNs() > 0 {
		observedAt = time.Unix(0, obs.GetWallTimeNs())
	}

	candidates := make([]model.FaceCandidateItem, 0, len(obs.GetCandidates()))
	for _, c := range obs.GetCandidates() {
		if c != nil {
			candidates = append(candidates, model.FaceCandidateItem{
				FaceID:     c.GetFaceId(),
				PersonID:   c.GetPersonId(),
				PersonName: c.GetPersonName(),
				Similarity: c.GetSimilarity(),
			})
		}
	}
	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return fmt.Errorf("marshal face candidates: %w", err)
	}

	record := &model.FaceObservation{
		EventID:          eventID,
		InstanceID:       obs.GetInstanceId(),
		CameraID:         obs.GetCameraId(),
		CameraName:       obs.GetCameraName(),
		AlgorithmID:      obs.GetAlgorithmId(),
		AlgorithmVersion: obs.GetAlgorithmVersion(),
		TrackID:          obs.GetTrackId(),
		FaceID:           obs.GetFaceId(),
		PersonID:         obs.GetPersonId(),
		PersonName:       obs.GetPersonName(),
		Similarity:       obs.GetSimilarity(),
		CandidatesJSON:   candidatesJSON,
		BBoxJSON:         bboxJSON,
		TimeSynced:       obs.GetTimeSynced(),
		ImageID:          imgID,
		ImageRelPath:     imgRelPath,
		FaceImageID:      faceImgID,
		FaceImageRelPath: faceImgRelPath,
		ObservedAt:       observedAt,
	}

	if err := a.faceRepo.UpsertMonotonic(ctx, record); err != nil {
		a.log.Error("persist face observation failed", zap.String("event_id", eventID), zap.Error(err))
		return err
	}

	return nil
}

// AcceptCapture 通用抓拍事件流持久化：每个唯一 event_id 只插入一条记录。
func (a *ReportAdapter) AcceptCapture(ctx context.Context, event *argusv1.CaptureEvent) error {
	if a.genericCaptureRepo == nil {
		return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "capture report service unavailable")
	}
	if event == nil {
		return errors.New("capture event is nil")
	}
	eventID := strings.TrimSpace(event.GetEventId())
	if eventID == "" {
		return errors.New("capture event_id is empty")
	}
	if strings.TrimSpace(event.GetCameraId()) == "" {
		return errors.New("capture camera_id is empty")
	}

	targetType := strings.TrimSpace(event.GetTargetType())
	if targetType == "" {
		targetType = model.CaptureTargetGeneric
	}
	switch targetType {
	case model.CaptureTargetFace, model.CaptureTargetPerson, model.CaptureTargetVehicle,
		model.CaptureTargetNonMotor, model.CaptureTargetGeneric:
	default:
		return errors.New("capture target_type is invalid")
	}

	if event.GetBbox() == nil {
		return errors.New("capture bbox is required")
	}
	bboxJSON, err := captureBBoxJSON(event.GetBbox())
	if err != nil {
		return err
	}
	var subBBoxJSON []byte
	if event.GetSubBbox() != nil {
		subBBoxJSON, err = captureBBoxJSON(event.GetSubBbox())
		if err != nil {
			return err
		}
	} else {
		subBBoxJSON = []byte("{}")
	}

	if !finiteRatio(event.GetConfidence()) || !finiteRatio(event.GetQualityScore()) {
		return errors.New("capture confidence or quality_score is invalid")
	}
	attributesJSON := []byte(strings.TrimSpace(event.GetAttributesJson()))
	if len(attributesJSON) == 0 {
		attributesJSON = []byte("{}")
	}
	var attributes map[string]any
	if !json.Valid(attributesJSON) || json.Unmarshal(attributesJSON, &attributes) != nil || attributes == nil {
		return errors.New("capture attributes_json must be a JSON object")
	}

	imageID := event.GetImageId()
	imageRelPath := event.GetImageRelPath()
	cropImageID := event.GetCropImageId()
	cropImageRelPath := event.GetCropImageRelPath()
	subCropImageID := event.GetSubCropImageId()
	subCropImageRelPath := event.GetSubCropImageRelPath()
	if a.circuitBreaker != nil && a.circuitBreaker.IsCircuitBreakerActive() {
		a.log.Warn("storage circuit breaker active: dropping capture image persistence",
			zap.String("event_id", eventID), zap.String("target_type", targetType))
		imageID, imageRelPath = "", ""
		cropImageID, cropImageRelPath = "", ""
		subCropImageID, subCropImageRelPath = "", ""
	} else if !isPathSafe(imageRelPath) || !isPathSafe(cropImageRelPath) || !isPathSafe(subCropImageRelPath) {
		return errors.New("capture image path is invalid")
	}

	capturedAt := time.Now()
	if event.GetWallTimeNs() > 0 {
		capturedAt = time.Unix(0, event.GetWallTimeNs())
	}
	record := &model.CaptureRecord{
		EventID:             eventID,
		InstanceID:          event.GetInstanceId(),
		TargetType:          targetType,
		CameraID:            event.GetCameraId(),
		CameraName:          event.GetCameraName(),
		AlgorithmID:         event.GetAlgorithmId(),
		AlgorithmVersion:    event.GetAlgorithmVersion(),
		TrackID:             event.GetTrackId(),
		Confidence:          event.GetConfidence(),
		QualityScore:        event.GetQualityScore(),
		BBoxJSON:            model.JSONRaw(bboxJSON),
		SubBBoxJSON:         model.JSONRaw(subBBoxJSON),
		TimeSynced:          event.GetTimeSynced(),
		ImageID:             imageID,
		ImageRelPath:        imageRelPath,
		CropImageID:         cropImageID,
		CropImageRelPath:    cropImageRelPath,
		SubCropImageID:      subCropImageID,
		SubCropImageRelPath: subCropImageRelPath,
		IsRecognized:        event.GetIsRecognized(),
		AttributesJSON:      model.JSONRaw(attributesJSON),
		CapturedAt:          capturedAt,
	}
	if err := a.genericCaptureRepo.Create(ctx, record); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			a.log.Info("duplicate capture event received, treated as idempotent success", zap.String("event_id", eventID))
			return nil
		}
		a.log.Error("persist capture event failed", zap.String("event_id", eventID), zap.Error(err))
		return err
	}
	return nil
}

func finiteRatio(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) && value >= 0 && value <= 1
}

func captureBBoxJSON(bbox *argusv1.BoundingBox) ([]byte, error) {
	values := []float32{bbox.GetXMin(), bbox.GetYMin(), bbox.GetXMax(), bbox.GetYMax()}
	for _, value := range values {
		if !finiteRatio(value) {
			return nil, errors.New("capture bbox contains invalid coordinate")
		}
	}
	if values[2] <= values[0] || values[3] <= values[1] {
		return nil, errors.New("capture bbox has invalid bounds")
	}
	return json.Marshal(map[string]float32{
		"x_min": values[0], "y_min": values[1], "x_max": values[2], "y_max": values[3],
	})
}

// AcceptFaceCapture 人脸抓拍全量事件记录持久化：增量追加快照至 face_captures 表。
func (a *ReportAdapter) AcceptFaceCapture(ctx context.Context, capture *argusv1.FaceCapture) error {
	if a.captureRepo == nil {
		return engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "face capture report service unavailable")
	}
	if capture == nil {
		return errors.New("face capture is nil")
	}
	eventID := strings.TrimSpace(capture.GetEventId())
	if eventID == "" {
		return errors.New("face capture event_id is empty")
	}

	snap := capture.GetSnapshot()
	if snap == nil {
		return errors.New("face capture snapshot is nil")
	}

	// 校验图片路径安全性及 95% 极危防爆盘熔断处理
	snapImgID := snap.GetImageId()
	snapImgRelPath := snap.GetImageRelPath()
	snapFaceImgID := snap.GetFaceImageId()
	snapFaceImgRelPath := snap.GetFaceImageRelPath()

	if a.circuitBreaker != nil && a.circuitBreaker.IsCircuitBreakerActive() {
		a.log.Warn("storage circuit breaker active (disk usage >= 95%): dropping face capture image persistence",
			zap.String("event_id", eventID),
			zap.String("dropped_image", snapImgRelPath),
			zap.String("dropped_face_image", snapFaceImgRelPath))
		snapImgID, snapImgRelPath = "", ""
		snapFaceImgID, snapFaceImgRelPath = "", ""
	} else if !isPathSafe(snapImgRelPath) || !isPathSafe(snapFaceImgRelPath) {
		return errors.New("face capture image path is invalid")
	}

	var bboxMap map[string]float32
	if pbBBox := snap.GetFaceBbox(); pbBBox != nil {
		bboxMap = map[string]float32{
			"x_min": pbBBox.GetXMin(),
			"y_min": pbBBox.GetYMin(),
			"x_max": pbBBox.GetXMax(),
			"y_max": pbBBox.GetYMax(),
		}
	}
	bboxJSON, err := json.Marshal(bboxMap)
	if err != nil {
		return fmt.Errorf("marshal face capture bbox: %w", err)
	}

	observedAt := time.Now()
	if snap.GetWallTimeNs() > 0 {
		observedAt = time.Unix(0, snap.GetWallTimeNs())
	}

	modelCapture := &model.FaceCapture{
		EventID:          eventID,
		InstanceID:       capture.GetInstanceId(),
		CameraID:         capture.GetCameraId(),
		CameraName:       capture.GetCameraName(),
		AlgorithmID:      capture.GetAlgorithmId(),
		AlgorithmVersion: capture.GetAlgorithmVersion(),
		TrackID:          capture.GetTrackId(),
	}

	snapCandidates := make([]model.FaceCandidateItem, 0, len(snap.GetCandidates()))
	for _, c := range snap.GetCandidates() {
		if c != nil {
			snapCandidates = append(snapCandidates, model.FaceCandidateItem{
				FaceID:     c.GetFaceId(),
				PersonID:   c.GetPersonId(),
				PersonName: c.GetPersonName(),
				Similarity: c.GetSimilarity(),
			})
		}
	}

	snapshotItem := &model.SnapshotItem{
		SnapshotIndex:    snap.GetSnapshotIndex(),
		WallTimeNs:       snap.GetWallTimeNs(),
		TimeSynced:       snap.GetTimeSynced(),
		ObservedAt:       observedAt,
		ImageID:          snapImgID,
		ImageRelPath:     snapImgRelPath,
		FaceImageID:      snapFaceImgID,
		FaceImageRelPath: snapFaceImgRelPath,
		BBoxJSON:         bboxJSON,
		QualityScore:     snap.GetQualityScore(),
		Similarity:       snap.GetSimilarity(),
		FaceID:           snap.GetFaceId(),
		PersonID:         snap.GetPersonId(),
		PersonName:       snap.GetPersonName(),
		Candidates:       snapCandidates,
	}

	if err := a.captureRepo.UpsertIncremental(ctx, modelCapture, snapshotItem); err != nil {
		a.log.Error("persist face capture failed", zap.String("event_id", eventID), zap.Error(err))
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
	if a.alarmRepo == nil && a.plateRepo == nil && a.faceRepo == nil && a.captureRepo == nil && a.genericCaptureRepo == nil {
		return engineipc.OrphanDisposition{}, engineipc.NewAdapterError(engineipc.CodeIPCUNAVAILABLE, "image reference repositories unavailable")
	}

	if len(entries) == 0 {
		return engineipc.OrphanDisposition{}, nil
	}

	allImageIDs := make([]string, 0, len(entries))
	seenImageIDs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.GetImageId() == "" {
			continue
		}
		if _, ok := seenImageIDs[entry.GetImageId()]; ok {
			continue
		}
		seenImageIDs[entry.GetImageId()] = struct{}{}
		allImageIDs = append(allImageIDs, entry.GetImageId())
	}

	existingMap := make(map[string]struct{}, len(allImageIDs))
	type imageIDFinder interface {
		FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
	}
	finders := []struct {
		name   string
		finder imageIDFinder
	}{
		{"alarm", a.alarmRepo},
		{"plate", a.plateRepo},
		{"face", a.faceRepo},
		{"face_capture", a.captureRepo},
		{"capture", a.genericCaptureRepo},
	}
	for _, f := range finders {
		if f.finder == nil {
			continue
		}
		existingIDs, err := f.finder.FindExistingImageIDs(ctx, allImageIDs)
		if err != nil {
			a.log.Error(fmt.Sprintf("find %s image ids failed during orphan reconciliation", f.name), zap.Error(err))
			return engineipc.OrphanDisposition{}, err
		}
		for _, id := range existingIDs {
			existingMap[id] = struct{}{}
		}
	}

	now := time.Now()
	var retainIDs []string
	var deleteIDs []string
	seenRetainIDs := make(map[string]struct{}, len(entries))
	seenDeleteIDs := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if entry == nil || entry.GetImageId() == "" {
			continue
		}
		imageID := entry.GetImageId()
		if _, ok := existingMap[imageID]; ok {
			if _, seen := seenRetainIDs[imageID]; !seen {
				seenRetainIDs[imageID] = struct{}{}
				retainIDs = append(retainIDs, imageID)
			}
			continue
		}

		// 未落库：检查是否超过保护期。
		createdAt := time.Unix(0, entry.GetCreatedAtNs())
		if entry.GetCreatedAtNs() > 0 && now.Sub(createdAt) > orphanRetentionGracePeriod {
			if _, seen := seenDeleteIDs[imageID]; !seen {
				seenDeleteIDs[imageID] = struct{}{}
				deleteIDs = append(deleteIDs, imageID)
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

// SetTaskRuntimeStatus 手动更新/重置任务的内存实时状态（如启停控制）。
func (a *ReportAdapter) SetTaskRuntimeStatus(cameraID string, status int8, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.tasks[cameraID]
	state.Status = status
	state.Message = message
	state.ReportedAt = time.Now()
	if status == model.TaskStatusStopped {
		state.LastFrameAt = nil
	}
	a.tasks[cameraID] = state
}

// RemoveTaskRuntime 移除任务的内存实时状态（如删除任务）。
func (a *ReportAdapter) RemoveTaskRuntime(cameraID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tasks, cameraID)
}

// InstanceRuntime 返回 instance_id 对应的内存实时状态；未上报过返回 ok=false。
func (a *ReportAdapter) InstanceRuntime(instanceID string) (InstanceRuntimeState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state, ok := a.insts[instanceID]
	return state, ok
}

// SetInstanceRuntimeStatus 手动更新/重置实例的内存实时状态（如启停控制）。
func (a *ReportAdapter) SetInstanceRuntimeStatus(instanceID string, status int8, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.insts[instanceID]
	state.Status = status
	state.Message = message
	state.ReportedAt = time.Now()
	if status == model.InstanceStatusStopped {
		state.CurrentFps = 0
	}
	a.insts[instanceID] = state
}

// RemoveInstanceRuntime 移除实例的内存实时状态（如删除实例）。
func (a *ReportAdapter) RemoveInstanceRuntime(instanceID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.insts, instanceID)
}

// timeFromWallNs 把 Engine 的 wall-clock 纳秒转成时间指针；非正值视为未上报（nil）。
func timeFromWallNs(ns int64) *time.Time {
	if ns <= 0 {
		return nil
	}
	t := time.Unix(0, ns)
	return &t
}
