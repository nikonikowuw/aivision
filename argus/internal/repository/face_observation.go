package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// FaceObservationFilter 人脸抓拍识别记录分页组合查询条件。
type FaceObservationFilter struct {
	Page          int
	PageSize      int
	StartTime     *time.Time
	EndTime       *time.Time
	CameraID      string
	PersonID      string
	PersonName    string
	MinSimilarity *float32
	MaxSimilarity *float32
}

// FaceObservationRepository 人脸抓拍识别记录数据访问接口。
type FaceObservationRepository interface {
	// Create 创建人脸识别记录；若唯一索引冲突（重复 event_id）应返回 ErrDuplicateKey。
	Create(ctx context.Context, record *model.FaceObservation) error
	// UpsertMonotonic 单调 upsert 人脸识别记录：仅当新记录相似度更高时更新，否则幂等忽略。
	UpsertMonotonic(ctx context.Context, record *model.FaceObservation) error
	// GetByID 按主键查询记录。
	GetByID(ctx context.Context, id uint64) (*model.FaceObservation, error)
	// GetByEventID 按 event_id 查询记录。
	GetByEventID(ctx context.Context, eventID string) (*model.FaceObservation, error)
	// ListPage 分页组合查询人脸识别记录。
	ListPage(ctx context.Context, filter *FaceObservationFilter) ([]model.FaceObservation, int64, error)
	// FindExistingImageIDs 批量查询已落库的全景图和人脸特写 image_id 集合（供孤儿图片对账）。
	FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
}

type faceObservationRepository struct {
	db *gorm.DB
}

// NewFaceObservationRepository 创建 FaceObservationRepository 实例。
func NewFaceObservationRepository(db *gorm.DB) FaceObservationRepository {
	return &faceObservationRepository{db: db}
}

func (r *faceObservationRepository) Create(ctx context.Context, record *model.FaceObservation) error {
	if record == nil {
		return errors.New("face observation record is nil")
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return writeError(err)
	}
	return nil
}

func (r *faceObservationRepository) UpsertMonotonic(ctx context.Context, record *model.FaceObservation) error {
	if record == nil {
		return errors.New("face observation record is nil")
	}
	if record.EventID == "" {
		return errors.New("event_id is required")
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 尝试直接插入新记录
		err := tx.Create(record).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return writeError(err)
		}

		// 2. 唯一冲突 -> 单调 upsert（仅当新相似度更高时覆盖）
		res := tx.Model(&model.FaceObservation{}).
			Where("event_id = ? AND deleted_at = 0 AND similarity < ?", record.EventID, record.Similarity).
			Updates(map[string]any{
				"instance_id":         record.InstanceID,
				"camera_id":           record.CameraID,
				"camera_name":         record.CameraName,
				"algorithm_id":        record.AlgorithmID,
				"algorithm_version":   record.AlgorithmVersion,
				"track_id":            record.TrackID,
				"face_id":             record.FaceID,
				"person_id":           record.PersonID,
				"person_name":         record.PersonName,
				"similarity":          record.Similarity,
				"bbox_json":           record.BBoxJSON,
				"time_synced":         record.TimeSynced,
				"image_id":            record.ImageID,
				"image_rel_path":      record.ImageRelPath,
				"face_image_id":       record.FaceImageID,
				"face_image_rel_path": record.FaceImageRelPath,
				"observed_at":         record.ObservedAt,
				"updated_at":          time.Now(),
			})
		if res.Error != nil {
			return writeError(res.Error)
		}
		// RowsAffected == 0 表示已有记录相似度不低于本次上报（含重试队列乱序），视为幂等成功
		return nil
	})
}

func (r *faceObservationRepository) GetByID(ctx context.Context, id uint64) (*model.FaceObservation, error) {
	var record model.FaceObservation
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *faceObservationRepository) GetByEventID(ctx context.Context, eventID string) (*model.FaceObservation, error) {
	var record model.FaceObservation
	err := r.db.WithContext(ctx).Where("event_id = ?", eventID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *faceObservationRepository) ListPage(ctx context.Context, filter *FaceObservationFilter) ([]model.FaceObservation, int64, error) {
	var f FaceObservationFilter
	if filter != nil {
		f = *filter
	}
	page, pageSize := normalizePage(f.Page, f.PageSize)

	query := r.db.WithContext(ctx).Model(&model.FaceObservation{})

	if f.StartTime != nil {
		query = query.Where("observed_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		query = query.Where("observed_at <= ?", *f.EndTime)
	}
	if f.CameraID != "" {
		query = query.Where("camera_id = ?", f.CameraID)
	}
	if f.PersonID != "" {
		query = query.Where("person_id = ?", f.PersonID)
	}
	if f.PersonName != "" {
		query = query.Where("person_name LIKE ?", "%"+f.PersonName+"%")
	}
	if f.MinSimilarity != nil {
		query = query.Where("similarity >= ?", *f.MinSimilarity)
	}
	if f.MaxSimilarity != nil {
		query = query.Where("similarity <= ?", *f.MaxSimilarity)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.FaceObservation{}, 0, nil
	}

	var records []model.FaceObservation
	offset := (page - 1) * pageSize
	if err := query.Order("observed_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *faceObservationRepository) FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 {
		return nil, nil
	}
	var existingPanorama []string
	if err := r.db.WithContext(ctx).Model(&model.FaceObservation{}).
		Where("image_id IN ?", imageIDs).
		Pluck("image_id", &existingPanorama).Error; err != nil {
		return nil, err
	}

	var existingFace []string
	if err := r.db.WithContext(ctx).Model(&model.FaceObservation{}).
		Where("face_image_id IN ?", imageIDs).
		Pluck("face_image_id", &existingFace).Error; err != nil {
		return nil, err
	}

	merged := make(map[string]struct{}, len(existingPanorama)+len(existingFace))
	for _, id := range existingPanorama {
		if id != "" {
			merged[id] = struct{}{}
		}
	}
	for _, id := range existingFace {
		if id != "" {
			merged[id] = struct{}{}
		}
	}

	result := make([]string, 0, len(merged))
	for id := range merged {
		result = append(result, id)
	}
	return result, nil
}
