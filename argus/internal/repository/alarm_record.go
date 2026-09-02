package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// AlarmRecordFilter 告警记录分页组合查询条件。
type AlarmRecordFilter struct {
	Page          int
	PageSize      int
	StartTime     *time.Time
	EndTime       *time.Time
	CameraID      string
	AlgorithmID   string
	AlarmTypeID   string
	MinConfidence *float32
	MaxConfidence *float32
}

// AlarmRecordRepository 告警记录数据访问接口。
type AlarmRecordRepository interface {
	// Create 创建告警记录；若唯一索引冲突（重复 event_id）应返回 ErrDuplicateKey。
	Create(ctx context.Context, record *model.AlarmRecord) error
	// GetByID 按主键查询告警记录。
	GetByID(ctx context.Context, id uint64) (*model.AlarmRecord, error)
	// GetByEventID 按 event_id 查询告警记录。
	GetByEventID(ctx context.Context, eventID string) (*model.AlarmRecord, error)
	// GetByImageID 按 image_id 查询告警记录。
	GetByImageID(ctx context.Context, imageID string) (*model.AlarmRecord, error)
	// ListPage 分页组合查询告警记录。
	ListPage(ctx context.Context, filter *AlarmRecordFilter) ([]model.AlarmRecord, int64, error)
	// FindExistingImageIDs 批量查询已落库的 image_id 集合（供孤儿图片对账）。
	FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
	// FindExpired 查询早于指定时间的过期告警记录（按 occurred_at 升序，包含已软删除记录）。
	FindExpired(ctx context.Context, before time.Time, limit int) ([]model.AlarmRecord, error)
	// FindOldest 查询时间最早的告警记录（按 occurred_at 升序，包含已软删除记录）。
	FindOldest(ctx context.Context, limit int) ([]model.AlarmRecord, error)
	// HardDeleteBatch 物理硬删除指定 ID 列表的告警记录。
	HardDeleteBatch(ctx context.Context, ids []uint64) error
	// CountTotal 统计有效告警记录总数。
	CountTotal(ctx context.Context) (int64, error)
}

type alarmRecordRepository struct {
	db *gorm.DB
}

// NewAlarmRecordRepository 创建 AlarmRecordRepository 实例。
func NewAlarmRecordRepository(db *gorm.DB) AlarmRecordRepository {
	return &alarmRecordRepository{db: db}
}

func (r *alarmRecordRepository) Create(ctx context.Context, record *model.AlarmRecord) error {
	if record == nil {
		return errors.New("alarm record is nil")
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return writeError(err)
	}
	return nil
}

func (r *alarmRecordRepository) GetByID(ctx context.Context, id uint64) (*model.AlarmRecord, error) {
	var record model.AlarmRecord
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *alarmRecordRepository) GetByEventID(ctx context.Context, eventID string) (*model.AlarmRecord, error) {
	var record model.AlarmRecord
	err := r.db.WithContext(ctx).Where("event_id = ?", eventID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *alarmRecordRepository) GetByImageID(ctx context.Context, imageID string) (*model.AlarmRecord, error) {
	var record model.AlarmRecord
	err := r.db.WithContext(ctx).Where("image_id = ?", imageID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *alarmRecordRepository) ListPage(ctx context.Context, filter *AlarmRecordFilter) ([]model.AlarmRecord, int64, error) {
	var f AlarmRecordFilter
	if filter != nil {
		f = *filter
	}
	page, pageSize := normalizePage(f.Page, f.PageSize)

	query := r.db.WithContext(ctx).Model(&model.AlarmRecord{})

	if f.StartTime != nil {
		query = query.Where("occurred_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		query = query.Where("occurred_at <= ?", *f.EndTime)
	}
	if f.CameraID != "" {
		query = query.Where("camera_id = ?", f.CameraID)
	}
	if f.AlgorithmID != "" {
		query = query.Where("algorithm_id = ?", f.AlgorithmID)
	}
	if f.AlarmTypeID != "" {
		query = query.Where("alarm_type_id = ?", f.AlarmTypeID)
	}
	if f.MinConfidence != nil {
		query = query.Where("confidence >= ?", *f.MinConfidence)
	}
	if f.MaxConfidence != nil {
		query = query.Where("confidence <= ?", *f.MaxConfidence)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []model.AlarmRecord
	err := query.
		Order("occurred_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func (r *alarmRecordRepository) FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 {
		return []string{}, nil
	}
	var foundIDs []string
	err := r.db.WithContext(ctx).
		Model(&model.AlarmRecord{}).
		Where("image_id IN ?", imageIDs).
		Pluck("image_id", &foundIDs).Error
	if err != nil {
		return nil, err
	}
	return foundIDs, nil
}

func (r *alarmRecordRepository) FindExpired(ctx context.Context, before time.Time, limit int) ([]model.AlarmRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.AlarmRecord
	err := r.db.WithContext(ctx).
		Unscoped().
		Where("occurred_at < ?", before).
		Order("occurred_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (r *alarmRecordRepository) FindOldest(ctx context.Context, limit int) ([]model.AlarmRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.AlarmRecord
	err := r.db.WithContext(ctx).
		Unscoped().
		Order("occurred_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (r *alarmRecordRepository) HardDeleteBatch(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Delete(&model.AlarmRecord{}).Error
}

func (r *alarmRecordRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.AlarmRecord{}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}
