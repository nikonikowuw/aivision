package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// PlateObservationFilter 车牌抓拍过车记录分页组合查询条件。
type PlateObservationFilter struct {
	Page             int
	PageSize         int
	StartTime        *time.Time
	EndTime          *time.Time
	CameraID         string
	PlateText        string
	PlateColor       string
	PlateType        string
	MinConfidence    *float32
	MaxConfidence    *float32
	MinOcrConfidence *float32
}

// PlateObservationRepository 车牌过车记录数据访问接口。
type PlateObservationRepository interface {
	// Create 创建车牌过车记录；若唯一索引冲突（重复 event_id）应返回 ErrDuplicateKey。
	Create(ctx context.Context, record *model.PlateObservation) error
	// GetByID 按主键查询记录。
	GetByID(ctx context.Context, id uint64) (*model.PlateObservation, error)
	// GetByEventID 按 event_id 查询记录。
	GetByEventID(ctx context.Context, eventID string) (*model.PlateObservation, error)
	// ListPage 分页组合查询过车记录。
	ListPage(ctx context.Context, filter *PlateObservationFilter) ([]model.PlateObservation, int64, error)
	// FindExistingImageIDs 批量查询已落库的全景图和车牌特写 image_id 集合（供孤儿图片对账）。
	FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
}
type plateObservationRepository struct {
	db *gorm.DB
}

// NewPlateObservationRepository 创建 PlateObservationRepository 实例。
func NewPlateObservationRepository(db *gorm.DB) PlateObservationRepository {
	return &plateObservationRepository{db: db}
}

func (r *plateObservationRepository) Create(ctx context.Context, record *model.PlateObservation) error {
	if record == nil {
		return errors.New("plate observation record is nil")
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return writeError(err)
	}
	return nil
}

func (r *plateObservationRepository) GetByID(ctx context.Context, id uint64) (*model.PlateObservation, error) {
	var record model.PlateObservation
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *plateObservationRepository) GetByEventID(ctx context.Context, eventID string) (*model.PlateObservation, error) {
	var record model.PlateObservation
	err := r.db.WithContext(ctx).Where("event_id = ?", eventID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *plateObservationRepository) ListPage(ctx context.Context, filter *PlateObservationFilter) ([]model.PlateObservation, int64, error) {
	var f PlateObservationFilter
	if filter != nil {
		f = *filter
	}
	page, pageSize := normalizePage(f.Page, f.PageSize)

	query := r.db.WithContext(ctx).Model(&model.PlateObservation{})

	if f.StartTime != nil {
		query = query.Where("observed_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		query = query.Where("observed_at <= ?", *f.EndTime)
	}
	if f.CameraID != "" {
		query = query.Where("camera_id = ?", f.CameraID)
	}
	if f.PlateText != "" {
		query = query.Where("plate_text LIKE ? OR normalized_text LIKE ?", "%"+f.PlateText+"%", "%"+f.PlateText+"%")
	}
	if f.PlateColor != "" {
		query = query.Where("plate_color = ?", f.PlateColor)
	}
	if f.PlateType != "" {
		query = query.Where("plate_type = ?", f.PlateType)
	}
	if f.MinConfidence != nil {
		query = query.Where("confidence >= ?", *f.MinConfidence)
	}
	if f.MaxConfidence != nil {
		query = query.Where("confidence <= ?", *f.MaxConfidence)
	}
	if f.MinOcrConfidence != nil {
		query = query.Where("ocr_confidence >= ?", *f.MinOcrConfidence)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []model.PlateObservation
	err := query.
		Order("observed_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func (r *plateObservationRepository) FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 {
		return []string{}, nil
	}

	type imageReference struct {
		ImageID      string
		PlateImageID string
	}
	var references []imageReference
	if err := r.db.WithContext(ctx).
		Model(&model.PlateObservation{}).
		Where("image_id IN ? OR plate_image_id IN ?", imageIDs, imageIDs).
		Select("image_id, plate_image_id").
		Find(&references).Error; err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(references)*2)
	for _, reference := range references {
		if reference.ImageID != "" {
			seen[reference.ImageID] = struct{}{}
		}
		if reference.PlateImageID != "" {
			seen[reference.PlateImageID] = struct{}{}
		}
	}
	foundIDs := make([]string, 0, len(seen))
	for imageID := range seen {
		foundIDs = append(foundIDs, imageID)
	}
	return foundIDs, nil
}
