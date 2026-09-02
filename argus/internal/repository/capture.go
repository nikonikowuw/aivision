package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// CaptureFilter 通用抓拍记录分页查询条件。
type CaptureFilter struct {
	Page          int
	PageSize      int
	StartTime     *time.Time
	EndTime       *time.Time
	TargetType    string
	CameraID      string
	TrackID       int64
	Keyword       string
	IsRecognized  *bool
	MinQuality    *float32
	MaxQuality    *float32
	MinConfidence *float32
	MaxConfidence *float32
}

// CaptureRepository 通用抓拍记录数据访问接口。
type CaptureRepository interface {
	// Create 插入一条抓拍事件。重复 event_id 返回 ErrDuplicateKey。
	Create(ctx context.Context, record *model.CaptureRecord) error
	// GetByID 按主键查询有效抓拍记录。
	GetByID(ctx context.Context, id uint64) (*model.CaptureRecord, error)
	// GetByEventID 按事件 ID 查询有效抓拍记录。
	GetByEventID(ctx context.Context, eventID string) (*model.CaptureRecord, error)
	// ListPage 分页查询有效抓拍记录。
	ListPage(ctx context.Context, filter *CaptureFilter) ([]model.CaptureRecord, int64, error)
	// FindExistingImageIDs 查询已被抓拍记录引用的图片 ID。
	FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
	// FindExpired 查询过期抓拍，包含软删除记录供清理器物理回收。
	FindExpired(ctx context.Context, before time.Time, limit int) ([]model.CaptureRecord, error)
	// FindOldestUnrecognized 查询最早的未识别抓拍，供高水位优先削峰。
	FindOldestUnrecognized(ctx context.Context, limit int) ([]model.CaptureRecord, error)
	// FindOldest 查询时间最早的抓拍，作为削峰兜底。
	FindOldest(ctx context.Context, limit int) ([]model.CaptureRecord, error)
	// HardDeleteBatch 物理删除指定抓拍记录。
	HardDeleteBatch(ctx context.Context, ids []uint64) error
	// CountTotal 统计有效抓拍数量。
	CountTotal(ctx context.Context) (int64, error)
}

type captureRepository struct {
	db *gorm.DB
}

// NewCaptureRepository 创建通用抓拍记录仓储。
func NewCaptureRepository(db *gorm.DB) CaptureRepository {
	return &captureRepository{db: db}
}

func (r *captureRepository) Create(ctx context.Context, record *model.CaptureRecord) error {
	if record == nil {
		return errors.New("capture record is nil")
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return writeError(err)
	}
	return nil
}

func (r *captureRepository) GetByID(ctx context.Context, id uint64) (*model.CaptureRecord, error) {
	var record model.CaptureRecord
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *captureRepository) GetByEventID(ctx context.Context, eventID string) (*model.CaptureRecord, error) {
	var record model.CaptureRecord
	if err := r.db.WithContext(ctx).Where("event_id = ?", eventID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *captureRepository) ListPage(ctx context.Context, filter *CaptureFilter) ([]model.CaptureRecord, int64, error) {
	var f CaptureFilter
	if filter != nil {
		f = *filter
	}
	page, pageSize := normalizePage(f.Page, f.PageSize)

	query := r.db.WithContext(ctx).Model(&model.CaptureRecord{})
	if f.StartTime != nil {
		query = query.Where("captured_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		query = query.Where("captured_at <= ?", *f.EndTime)
	}
	if f.TargetType != "" && f.TargetType != "all" {
		query = query.Where("target_type = ?", f.TargetType)
	}
	if f.CameraID != "" {
		query = query.Where("camera_id = ?", f.CameraID)
	}
	if f.TrackID > 0 {
		query = query.Where("track_id = ?", f.TrackID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		query = query.Where("event_id LIKE ? OR camera_name LIKE ? OR attributes_json LIKE ?", like, like, like)
	}
	if f.IsRecognized != nil {
		query = query.Where("is_recognized = ?", *f.IsRecognized)
	}
	if f.MinQuality != nil {
		query = query.Where("quality_score >= ?", *f.MinQuality)
	}
	if f.MaxQuality != nil {
		query = query.Where("quality_score <= ?", *f.MaxQuality)
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

	var records []model.CaptureRecord
	if err := query.Order("captured_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *captureRepository) FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 {
		return []string{}, nil
	}
	var records []struct {
		ImageID        string `gorm:"column:image_id"`
		CropImageID    string `gorm:"column:crop_image_id"`
		SubCropImageID string `gorm:"column:sub_crop_image_id"`
	}
	if err := r.db.WithContext(ctx).Model(&model.CaptureRecord{}).
		Select("image_id, crop_image_id, sub_crop_image_id").
		Where("image_id IN ? OR crop_image_id IN ? OR sub_crop_image_id IN ?", imageIDs, imageIDs, imageIDs).
		Find(&records).Error; err != nil {
		return nil, err
	}

	wanted := make(map[string]struct{}, len(imageIDs))
	for _, id := range imageIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	found := make([]string, 0, len(wanted))
	seen := make(map[string]struct{}, len(wanted))
	for _, record := range records {
		for _, id := range []string{record.ImageID, record.CropImageID, record.SubCropImageID} {
			if _, ok := wanted[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			found = append(found, id)
		}
	}
	return found, nil
}

func (r *captureRepository) FindExpired(ctx context.Context, before time.Time, limit int) ([]model.CaptureRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.CaptureRecord
	err := r.db.WithContext(ctx).Unscoped().
		Where("captured_at < ?", before).
		Order("captured_at ASC, id ASC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func (r *captureRepository) FindOldestUnrecognized(ctx context.Context, limit int) ([]model.CaptureRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.CaptureRecord
	err := r.db.WithContext(ctx).Unscoped().
		Where("is_recognized = ?", false).
		Order("captured_at ASC, id ASC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func (r *captureRepository) FindOldest(ctx context.Context, limit int) ([]model.CaptureRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.CaptureRecord
	err := r.db.WithContext(ctx).Unscoped().
		Order("captured_at ASC, id ASC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func (r *captureRepository) HardDeleteBatch(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.CaptureRecord{}).Error
}

func (r *captureRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.CaptureRecord{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
