package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/repository"
)

const (
	defaultBatchSize     = 200
	defaultPaceSleepTime = 50 * time.Millisecond
)

// StorageStatusDTO 磁盘使用状态与统计 DTO
type StorageStatusDTO struct {
	TotalBytes            uint64     `json:"totalBytes"`
	UsedBytes             uint64     `json:"usedBytes"`
	FreeBytes             uint64     `json:"freeBytes"`
	UsagePercent          float64    `json:"usagePercent"`
	AlarmRecordCount      int64      `json:"alarmRecordCount"`
	PlateObservationCount int64      `json:"plateObservationCount"`
	FaceObservationCount  int64      `json:"faceObservationCount"`
	FaceCaptureCount      int64      `json:"faceCaptureCount"`
	OperationLogCount     int64      `json:"operationLogCount"`
	Status                string     `json:"status"` // "normal" | "cleaning" | "degraded"
	CircuitBreakerActive  bool       `json:"circuitBreakerActive"`
	LastCleanupAt         *time.Time `json:"lastCleanupAt,omitempty"`
	LastFreedBytes        uint64     `json:"lastFreedBytes"`
}

// StorageCleanupService 边缘存储清理与防爆盘管理服务接口
type StorageCleanupService interface {
	GetStatus(ctx context.Context) (*StorageStatusDTO, error)
	GetConfig(ctx context.Context) (*model.StorageRetentionConfigValue, error)
	UpdateConfig(ctx context.Context, input *model.StorageRetentionConfigValue) error
	TriggerCleanup(ctx context.Context) error
	IsCircuitBreakerActive() bool
	Start(ctx context.Context)
	Stop()
}

type storageCleanupService struct {
	storageRoot      string
	systemConfigRepo repository.SystemConfigRepository
	alarmRepo        repository.AlarmRecordRepository
	plateRepo        repository.PlateObservationRepository
	faceRepo         repository.FaceObservationRepository
	faceCaptureRepo  repository.FaceCaptureRepository
	opLogRepo        repository.OperationLogRepository
	fileStorage      storage.FileStorage
	sampler          storage.DiskUsageSampler
	log              *zap.Logger

	circuitBreaker atomic.Bool
	cleaningMutex  sync.Mutex
	stateMu        sync.RWMutex
	lastCleanupAt  *time.Time
	lastFreedBytes uint64

	cancelFunc  context.CancelFunc
	wg          sync.WaitGroup
	triggerChan chan struct{}
}

// NewStorageCleanupService 构造存储管理服务实例
func NewStorageCleanupService(
	cfg *config.Config,
	systemConfigRepo repository.SystemConfigRepository,
	alarmRepo repository.AlarmRecordRepository,
	plateRepo repository.PlateObservationRepository,
	faceRepo repository.FaceObservationRepository,
	faceCaptureRepo repository.FaceCaptureRepository,
	opLogRepo repository.OperationLogRepository,
	fileStorage storage.FileStorage,
	sampler storage.DiskUsageSampler,
	logger *zap.Logger,
) StorageCleanupService {
	storageRoot := "."
	if cfg != nil && cfg.Storage.Local.Root != "" {
		storageRoot = cfg.Storage.Local.Root
	}

	if sampler == nil {
		sampler = storage.NewDiskUsageSampler()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &storageCleanupService{
		storageRoot:      storageRoot,
		systemConfigRepo: systemConfigRepo,
		alarmRepo:        alarmRepo,
		plateRepo:        plateRepo,
		faceRepo:         faceRepo,
		faceCaptureRepo:  faceCaptureRepo,
		opLogRepo:        opLogRepo,
		fileStorage:      fileStorage,
		sampler:          sampler,
		log:              logger,
		triggerChan:      make(chan struct{}, 1),
	}
}

// IsCircuitBreakerActive 查询 95% 极危抓拍熔断状态
func (s *storageCleanupService) IsCircuitBreakerActive() bool {
	return s.circuitBreaker.Load()
}

// GetConfig 获取当前存储保留与高低水位策略配置
func (s *storageCleanupService) GetConfig(ctx context.Context) (*model.StorageRetentionConfigValue, error) {
	record, err := s.systemConfigRepo.GetByKey(ctx, model.ConfigKeyStorageRetention)
	if err != nil {
		s.log.Error("failed to get storage retention config", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	if record == nil || record.Value == "" || record.Value == "{}" {
		defaultCfg := model.DefaultStorageRetentionConfig()
		return &defaultCfg, nil
	}

	var val model.StorageRetentionConfigValue
	if err := json.Unmarshal([]byte(record.Value), &val); err != nil {
		s.log.Error("failed to unmarshal storage retention config", zap.Error(err))
		defaultCfg := model.DefaultStorageRetentionConfig()
		return &defaultCfg, nil
	}
	return &val, nil
}

// UpdateConfig 更新存储保留策略配置（含边界校验与持久化）
func (s *storageCleanupService) UpdateConfig(ctx context.Context, input *model.StorageRetentionConfigValue) error {
	if input == nil {
		return errno.NewError(errno.CodeStorageInvalidConfig)
	}

	if err := validateStorageRetentionConfig(input); err != nil {
		s.log.Warn("storage retention config validation failed", zap.Error(err))
		return errno.NewError(errno.CodeStorageInvalidConfig)
	}

	rawJSON, err := json.Marshal(input)
	if err != nil {
		s.log.Error("failed to marshal storage retention config", zap.Error(err))
		return errno.NewError(errno.CodeInternal)
	}

	if err := s.systemConfigRepo.SetByKey(ctx, model.ConfigKeyStorageRetention, string(rawJSON), "存储保留与清理策略配置"); err != nil {
		s.log.Error("failed to persist storage retention config", zap.Error(err))
		return errno.NewError(errno.CodeInternal)
	}

	s.log.Info("storage retention config updated successfully",
		zap.Int("retentionDays", input.RetentionDays),
		zap.Int("highWatermarkPercent", input.HighWatermarkPercent),
		zap.Int("lowWatermarkPercent", input.LowWatermarkPercent),
		zap.Int("checkIntervalSeconds", input.CheckIntervalSeconds),
		zap.Bool("autoCleanupEnabled", input.AutoCleanupEnabled),
	)

	// 尝试通知可能等待的 worker 重新调度
	select {
	case s.triggerChan <- struct{}{}:
	default:
	}

	return nil
}

func validateStorageRetentionConfig(c *model.StorageRetentionConfigValue) error {
	if c.RetentionDays < 1 || c.RetentionDays > 365 {
		return errors.New("retentionDays must be between 1 and 365")
	}
	if c.HighWatermarkPercent < 50 || c.HighWatermarkPercent > 95 {
		return errors.New("highWatermarkPercent must be between 50 and 95")
	}
	if c.LowWatermarkPercent < 30 || c.LowWatermarkPercent > 90 {
		return errors.New("lowWatermarkPercent must be between 30 and 90")
	}
	if c.LowWatermarkPercent >= c.HighWatermarkPercent {
		return errors.New("lowWatermarkPercent must be strictly less than highWatermarkPercent")
	}
	if c.CheckIntervalSeconds < 30 || c.CheckIntervalSeconds > 86400 {
		return errors.New("checkIntervalSeconds must be between 30 and 86400")
	}
	return nil
}

// GetStatus 查询当前磁盘使用容量、各业务表记录数统计及清理状态
func (s *storageCleanupService) GetStatus(ctx context.Context) (*StorageStatusDTO, error) {
	usage, err := s.sampler.GetDiskUsage(s.storageRoot)
	if err != nil {
		s.log.Error("failed to sample disk usage", zap.Error(err), zap.String("root", s.storageRoot))
		return nil, errno.NewError(errno.CodeInternal)
	}

	alarmCount, err := s.alarmRepo.CountTotal(ctx)
	if err != nil {
		s.log.Error("failed to count alarm records", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	plateCount, err := s.plateRepo.CountTotal(ctx)
	if err != nil {
		s.log.Error("failed to count plate observations", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	faceCount, err := s.faceRepo.CountTotal(ctx)
	if err != nil {
		s.log.Error("failed to count face observations", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	faceCaptureCount, err := s.faceCaptureRepo.CountTotal(ctx)
	if err != nil {
		s.log.Error("failed to count face captures", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	opLogCount, err := s.opLogRepo.CountTotal(ctx)
	if err != nil {
		s.log.Error("failed to count operation logs", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	circuitBreakerActive := s.circuitBreaker.Load()

	status := "normal"
	if circuitBreakerActive {
		status = "degraded"
	} else if !s.cleaningMutex.TryLock() {
		status = "cleaning"
	} else {
		s.cleaningMutex.Unlock()
	}

	s.stateMu.RLock()
	lastAt := s.lastCleanupAt
	lastFreed := s.lastFreedBytes
	s.stateMu.RUnlock()

	return &StorageStatusDTO{
		TotalBytes:            usage.TotalBytes,
		UsedBytes:             usage.UsedBytes,
		FreeBytes:             usage.FreeBytes,
		UsagePercent:          usage.UsagePercent,
		AlarmRecordCount:      alarmCount,
		PlateObservationCount: plateCount,
		FaceObservationCount:  faceCount,
		FaceCaptureCount:      faceCaptureCount,
		OperationLogCount:     opLogCount,
		Status:                status,
		CircuitBreakerActive:  circuitBreakerActive,
		LastCleanupAt:         lastAt,
		LastFreedBytes:        lastFreed,
	}, nil
}

// TriggerCleanup 手动触发单次清理巡检
func (s *storageCleanupService) TriggerCleanup(ctx context.Context) error {
	select {
	case s.triggerChan <- struct{}{}:
		return nil
	default:
		return nil
	}
}

// Start 启动后台守护协程
func (s *storageCleanupService) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	s.cancelFunc = cancel

	s.wg.Add(1)
	go s.workerLoop(ctx)
	s.log.Info("storage cleanup worker started", zap.String("storageRoot", s.storageRoot))
}

// Stop 优雅关闭后台守护协程
func (s *storageCleanupService) Stop() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.wg.Wait()
	s.log.Info("storage cleanup worker stopped gracefully")
}

func (s *storageCleanupService) workerLoop(ctx context.Context) {
	defer s.wg.Done()

	// 启动时先执行一次健康检测与清理检查
	_ = s.executeCleanupCycle(ctx)

	for {
		cfg, err := s.GetConfig(ctx)
		interval := 600 * time.Second
		if err == nil && cfg.CheckIntervalSeconds >= 30 {
			interval = time.Duration(cfg.CheckIntervalSeconds) * time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-s.triggerChan:
			_ = s.executeCleanupCycle(ctx)
		case <-time.After(interval):
			_ = s.executeCleanupCycle(ctx)
		}
	}
}

func (s *storageCleanupService) updateCircuitBreaker(usagePercent float64) {
	if usagePercent >= 95.0 {
		if !s.circuitBreaker.Swap(true) {
			s.log.Warn("CRITICAL: disk usage reached >= 95%, circuit breaker ACTIVATED (dropping image writes)",
				zap.Float64("usagePercent", usagePercent))
		}
	} else if usagePercent < 85.0 {
		if s.circuitBreaker.Swap(false) {
			s.log.Info("disk usage dropped < 85%, circuit breaker RESET to normal",
				zap.Float64("usagePercent", usagePercent))
		}
	}
}

// executeCleanupCycle 执行单次存储巡检决策与清理
func (s *storageCleanupService) executeCleanupCycle(ctx context.Context) error {
	if !s.cleaningMutex.TryLock() {
		// 已有清理任务在进行中
		return nil
	}
	defer s.cleaningMutex.Unlock()

	usage, err := s.sampler.GetDiskUsage(s.storageRoot)
	if err != nil {
		s.log.Error("failed to sample disk usage during cleanup cycle", zap.Error(err))
		return err
	}

	// 1. 95% 极危水位熔断检测与状态机更新
	s.updateCircuitBreaker(usage.UsagePercent)

	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}

	if !cfg.AutoCleanupEnabled {
		return nil
	}

	initialFree := usage.FreeBytes
	totalCleanedRecords := 0

	// 2. 高低水位紧急削峰优先判断
	if usage.UsagePercent >= float64(cfg.HighWatermarkPercent) {
		s.log.Warn("disk usage exceeded high watermark, starting emergency FIFO purge",
			zap.Float64("current", usage.UsagePercent),
			zap.Int("highWatermark", cfg.HighWatermarkPercent),
			zap.Int("lowWatermark", cfg.LowWatermarkPercent))

		cleaned, purgeErr := s.purgeOldestRecords(ctx, float64(cfg.LowWatermarkPercent))
		totalCleanedRecords += cleaned
		if purgeErr != nil {
			s.log.Error("emergency purge failed", zap.Error(purgeErr))
		}
	} else if cfg.RetentionDays > 0 {
		// 3. 常规 TTL 周期巡检
		cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays)
		cleaned, ttlErr := s.purgeExpiredRecords(ctx, cutoff)
		totalCleanedRecords += cleaned
		if ttlErr != nil {
			s.log.Error("routine TTL cleanup failed", zap.Error(ttlErr))
		}
	}

	// 4. 清理后重新采样，更新释放统计与熔断状态
	newUsage, sErr := s.sampler.GetDiskUsage(s.storageRoot)
	if sErr == nil {
		if newUsage.FreeBytes > initialFree {
			freed := newUsage.FreeBytes - initialFree
			s.stateMu.Lock()
			now := time.Now()
			s.lastCleanupAt = &now
			s.lastFreedBytes = freed
			s.stateMu.Unlock()

			s.log.Info("cleanup cycle finished successfully",
				zap.Int("cleanedRecords", totalCleanedRecords),
				zap.Uint64("freedBytes", freed),
				zap.Float64("newUsagePercent", newUsage.UsagePercent))
		}
		s.updateCircuitBreaker(newUsage.UsagePercent)
	}

	return nil
}

func (s *storageCleanupService) cleanAllMediaBatches(ctx context.Context, isOldest bool, cutoff time.Time) int {
	batchCleaned := 0

	if n, err := s.cleanAlarmBatch(ctx, isOldest, cutoff); err != nil {
		s.log.Error("error cleaning alarm batch", zap.Error(err))
	} else {
		batchCleaned += n
	}

	if n, err := s.cleanPlateBatch(ctx, isOldest, cutoff); err != nil {
		s.log.Error("error cleaning plate batch", zap.Error(err))
	} else {
		batchCleaned += n
	}

	if n, err := s.cleanFaceBatch(ctx, isOldest, cutoff); err != nil {
		s.log.Error("error cleaning face batch", zap.Error(err))
	} else {
		batchCleaned += n
	}

	if n, err := s.cleanFaceCaptureBatch(ctx, isOldest, cutoff); err != nil {
		s.log.Error("error cleaning face capture batch", zap.Error(err))
	} else {
		batchCleaned += n
	}

	return batchCleaned
}

// purgeOldestRecords 紧急削峰模式：按 FIFO 逐批清理直至使用率低于 targetLowPercent
func (s *storageCleanupService) purgeOldestRecords(ctx context.Context, targetLowPercent float64) (int, error) {
	totalCleaned := 0

	for {
		if ctx.Err() != nil {
			return totalCleaned, ctx.Err()
		}

		usage, err := s.sampler.GetDiskUsage(s.storageRoot)
		if err != nil {
			return totalCleaned, err
		}

		if usage.UsagePercent <= targetLowPercent {
			s.log.Info("emergency cleanup reached target low watermark, early exiting",
				zap.Float64("currentUsage", usage.UsagePercent),
				zap.Float64("targetLow", targetLowPercent))
			break
		}

		batchCleaned := s.cleanAllMediaBatches(ctx, true, time.Time{})
		totalCleaned += batchCleaned

		if batchCleaned == 0 {
			// 数据库中已无可清理业务记录
			s.log.Warn("no more deletable records available, disk usage remains above target",
				zap.Float64("currentUsage", usage.UsagePercent))
			break
		}

		// I/O 让步休眠，避免磁盘 IOPS 打满影响视频解码与推理
		select {
		case <-ctx.Done():
			return totalCleaned, ctx.Err()
		case <-time.After(defaultPaceSleepTime):
		}
	}

	return totalCleaned, nil
}

// purgeExpiredRecords 常规 TTL 清理模式：分批删除早于 cutoff 的过期数据
func (s *storageCleanupService) purgeExpiredRecords(ctx context.Context, cutoff time.Time) (int, error) {
	totalCleaned := 0

	for {
		if ctx.Err() != nil {
			return totalCleaned, ctx.Err()
		}

		batchCleaned := s.cleanAllMediaBatches(ctx, false, cutoff)

		if n, err := s.opLogRepo.DeleteExpired(ctx, cutoff, defaultBatchSize); err != nil {
			s.log.Error("error cleaning expired operation logs", zap.Error(err))
		} else {
			batchCleaned += int(n)
		}

		totalCleaned += batchCleaned

		if batchCleaned == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return totalCleaned, ctx.Err()
		case <-time.After(defaultPaceSleepTime):
		}
	}

	return totalCleaned, nil
}

// deleteImages 物理批量删除给定的相对图片路径，幂等忽略空路径或已不存在的文件
func (s *storageCleanupService) deleteImages(ctx context.Context, paths ...string) {
	if s.fileStorage == nil {
		return
	}
	for _, p := range paths {
		if p != "" {
			if err := s.fileStorage.Delete(ctx, p); err != nil {
				s.log.Warn("failed to delete image file during cleanup", zap.String("path", p), zap.Error(err))
			}
		}
	}
}

// cleanAlarmBatch 执行单批告警记录防孤儿删除（先物理删图片 -> 后物理删 DB）
func (s *storageCleanupService) cleanAlarmBatch(ctx context.Context, isOldest bool, cutoff time.Time) (int, error) {
	var records []model.AlarmRecord
	var err error

	if isOldest {
		records, err = s.alarmRepo.FindOldest(ctx, defaultBatchSize)
	} else {
		records, err = s.alarmRepo.FindExpired(ctx, cutoff, defaultBatchSize)
	}

	if err != nil || len(records) == 0 {
		return 0, err
	}

	ids := make([]uint64, 0, len(records))
	for _, rec := range records {
		s.deleteImages(ctx, rec.ImageRelPath)
		ids = append(ids, rec.ID)
	}

	if err := s.alarmRepo.HardDeleteBatch(ctx, ids); err != nil {
		return 0, fmt.Errorf("hard delete alarm records batch: %w", err)
	}

	return len(records), nil
}

// cleanPlateBatch 执行单批车牌记录防孤儿删除
func (s *storageCleanupService) cleanPlateBatch(ctx context.Context, isOldest bool, cutoff time.Time) (int, error) {
	var records []model.PlateObservation
	var err error

	if isOldest {
		records, err = s.plateRepo.FindOldest(ctx, defaultBatchSize)
	} else {
		records, err = s.plateRepo.FindExpired(ctx, cutoff, defaultBatchSize)
	}

	if err != nil || len(records) == 0 {
		return 0, err
	}

	ids := make([]uint64, 0, len(records))
	for _, rec := range records {
		s.deleteImages(ctx, rec.ImageRelPath, rec.PlateImageRelPath)
		ids = append(ids, rec.ID)
	}

	if err := s.plateRepo.HardDeleteBatch(ctx, ids); err != nil {
		return 0, fmt.Errorf("hard delete plate observations batch: %w", err)
	}

	return len(records), nil
}

// cleanFaceBatch 执行单批人脸识别记录防孤儿删除
func (s *storageCleanupService) cleanFaceBatch(ctx context.Context, isOldest bool, cutoff time.Time) (int, error) {
	var records []model.FaceObservation
	var err error

	if isOldest {
		records, err = s.faceRepo.FindOldest(ctx, defaultBatchSize)
	} else {
		records, err = s.faceRepo.FindExpired(ctx, cutoff, defaultBatchSize)
	}

	if err != nil || len(records) == 0 {
		return 0, err
	}

	ids := make([]uint64, 0, len(records))
	for _, rec := range records {
		s.deleteImages(ctx, rec.ImageRelPath, rec.FaceImageRelPath)
		ids = append(ids, rec.ID)
	}

	if err := s.faceRepo.HardDeleteBatch(ctx, ids); err != nil {
		return 0, fmt.Errorf("hard delete face observations batch: %w", err)
	}

	return len(records), nil
}

// cleanFaceCaptureBatch 执行单批人脸抓拍记录防孤儿删除（包含全景图、人脸特写图以及所有时序快照图片）
func (s *storageCleanupService) cleanFaceCaptureBatch(ctx context.Context, isOldest bool, cutoff time.Time) (int, error) {
	var records []model.FaceCapture
	var err error

	if isOldest {
		records, err = s.faceCaptureRepo.FindOldest(ctx, defaultBatchSize)
	} else {
		records, err = s.faceCaptureRepo.FindExpired(ctx, cutoff, defaultBatchSize)
	}

	if err != nil || len(records) == 0 {
		return 0, err
	}

	ids := make([]uint64, 0, len(records))
	for _, rec := range records {
		s.deleteImages(ctx, rec.BestImageRelPath, rec.BestFaceRelPath)
		// 删除时序快照列表中的所有关联物理图片
		if snapshots, pErr := rec.ParseSnapshots(); pErr == nil {
			for _, snap := range snapshots {
				s.deleteImages(ctx, snap.ImageRelPath, snap.FaceImageRelPath)
			}
		}
		ids = append(ids, rec.ID)
	}

	if err := s.faceCaptureRepo.HardDeleteBatch(ctx, ids); err != nil {
		return 0, fmt.Errorf("hard delete face captures batch: %w", err)
	}

	return len(records), nil
}
