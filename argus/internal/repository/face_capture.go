package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// FaceCaptureFilter 人脸抓拍记录分页组合查询条件。
type FaceCaptureFilter struct {
	Page          int
	PageSize      int
	StartTime     *time.Time
	EndTime       *time.Time
	CameraID      string
	TrackID       int64
	PersonID      string
	PersonName    string
	Status        string // "all", "recognized", "stranger"
	MinSimilarity *float32
	MaxSimilarity *float32
}

// FaceCaptureRepository 人脸抓拍记录数据访问接口。
type FaceCaptureRepository interface {
	// Create 创建人脸抓拍记录。
	Create(ctx context.Context, record *model.FaceCapture) error
	// UpsertIncremental 增量单调 upsert 人脸抓拍记录：若不存在则新增，若已存在且快照未达上限则追加快照并单调更新最佳摘要。
	UpsertIncremental(ctx context.Context, capture *model.FaceCapture, snapshot *model.SnapshotItem) error
	// GetByID 按主键查询记录。
	GetByID(ctx context.Context, id uint64) (*model.FaceCapture, error)
	// GetByEventID 按 event_id 查询记录。
	GetByEventID(ctx context.Context, eventID string) (*model.FaceCapture, error)
	// ListPage 分页组合查询人脸抓拍记录。
	ListPage(ctx context.Context, filter *FaceCaptureFilter) ([]model.FaceCapture, int64, error)
	// FindExistingImageIDs 批量查询已落库的抓拍图片 image_id 集合（供孤儿图片对账）。
	FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error)
	// FindExpired 查询早于指定时间的过期抓拍记录（按 first_observed_at 升序，包含已软删除记录）。
	FindExpired(ctx context.Context, before time.Time, limit int) ([]model.FaceCapture, error)
	// FindOldest 查询时间最早的抓拍记录（按 first_observed_at 升序，包含已软删除记录）。
	FindOldest(ctx context.Context, limit int) ([]model.FaceCapture, error)
	// HardDeleteBatch 物理硬删除指定 ID 列表的人脸抓拍记录。
	HardDeleteBatch(ctx context.Context, ids []uint64) error
	// CountTotal 统计有效人脸抓拍记录总数。
	CountTotal(ctx context.Context) (int64, error)
}

type faceCaptureRepository struct {
	db *gorm.DB
}

// NewFaceCaptureRepository 创建 FaceCaptureRepository 实例。
func NewFaceCaptureRepository(db *gorm.DB) FaceCaptureRepository {
	return &faceCaptureRepository{db: db}
}

func (r *faceCaptureRepository) Create(ctx context.Context, record *model.FaceCapture) error {
	if record == nil {
		return errors.New("face capture record is nil")
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return writeError(err)
	}
	return nil
}

func (r *faceCaptureRepository) UpsertIncremental(ctx context.Context, capture *model.FaceCapture, snapshot *model.SnapshotItem) error {
	if capture == nil {
		return errors.New("face capture record is nil")
	}
	if snapshot == nil {
		return errors.New("snapshot item is nil")
	}
	if capture.EventID == "" {
		return errors.New("event_id is required")
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.FaceCapture
		err := tx.Where("event_id = ? AND deleted_at = 0", capture.EventID).First(&existing).Error

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 1. 记录不存在 -> 初始化并创建第一张快照
				snapshot.SnapshotIndex = 1
				snapshotsJSON, mErr := json.Marshal([]model.SnapshotItem{*snapshot})
				if mErr != nil {
					return mErr
				}

				capture.SnapshotCount = 1
				capture.SnapshotsJSON = snapshotsJSON
				capture.FirstObservedAt = snapshot.ObservedAt
				capture.LastObservedAt = snapshot.ObservedAt

				capture.BestSimilarity = snapshot.Similarity
				capture.BestQualityScore = snapshot.QualityScore
				capture.BestPersonID = snapshot.PersonID
				capture.BestPersonName = snapshot.PersonName
				capture.BestImageID = snapshot.ImageID
				capture.BestImageRelPath = snapshot.ImageRelPath
				capture.BestFaceImageID = snapshot.FaceImageID
				capture.BestFaceRelPath = snapshot.FaceImageRelPath
				capture.BestBBoxJSON = snapshot.BBoxJSON
				if candJSON, cErr := json.Marshal(snapshot.Candidates); cErr == nil {
					capture.BestCandidatesJSON = model.JSONRaw(candJSON)
				}

				if cErr := tx.Create(capture).Error; cErr != nil {
					return writeError(cErr)
				}
				return nil
			}
			return err
		}

		// 2. 记录已存在 -> 追加快照（最多 5 组）并更新摘要
		snapshots, pErr := existing.ParseSnapshots()
		if pErr != nil {
			snapshots = []model.SnapshotItem{}
		}

		updates := map[string]any{
			"last_observed_at": snapshot.ObservedAt,
			"updated_at":       time.Now(),
		}

		if len(snapshots) < 5 {
			snapshot.SnapshotIndex = int32(len(snapshots) + 1)
			snapshots = append(snapshots, *snapshot)
			newJSON, mErr := json.Marshal(snapshots)
			if mErr != nil {
				return mErr
			}
			updates["snapshots_json"] = model.JSONRaw(newJSON)
			updates["snapshot_count"] = int32(len(snapshots))
		}

		// 判定是否需要单调更新最佳摘要（相似度更高，或已有相似度为 0 且质量分更高）
		shouldUpdateBest := false
		if snapshot.Similarity > existing.BestSimilarity {
			shouldUpdateBest = true
		} else if existing.BestSimilarity == 0 && snapshot.QualityScore > existing.BestQualityScore {
			shouldUpdateBest = true
		}

		if shouldUpdateBest {
			updates["best_similarity"] = snapshot.Similarity
			updates["best_quality_score"] = snapshot.QualityScore
			updates["best_person_id"] = snapshot.PersonID
			updates["best_person_name"] = snapshot.PersonName
			updates["best_image_id"] = snapshot.ImageID
			updates["best_image_rel_path"] = snapshot.ImageRelPath
			updates["best_face_image_id"] = snapshot.FaceImageID
			updates["best_face_rel_path"] = snapshot.FaceImageRelPath
			updates["best_bbox_json"] = snapshot.BBoxJSON
			if candJSON, cErr := json.Marshal(snapshot.Candidates); cErr == nil {
				updates["best_candidates_json"] = model.JSONRaw(candJSON)
			}
		}

		res := tx.Model(&model.FaceCapture{}).
			Where("id = ? AND deleted_at = 0", existing.ID).
			Updates(updates)
		if res.Error != nil {
			return writeError(res.Error)
		}

		return nil
	})
}

func (r *faceCaptureRepository) GetByID(ctx context.Context, id uint64) (*model.FaceCapture, error) {
	var record model.FaceCapture
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *faceCaptureRepository) GetByEventID(ctx context.Context, eventID string) (*model.FaceCapture, error) {
	var record model.FaceCapture
	err := r.db.WithContext(ctx).Where("event_id = ? AND deleted_at = 0", eventID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (r *faceCaptureRepository) ListPage(ctx context.Context, filter *FaceCaptureFilter) ([]model.FaceCapture, int64, error) {
	var f FaceCaptureFilter
	if filter != nil {
		f = *filter
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	query := r.db.WithContext(ctx).Model(&model.FaceCapture{})

	if f.StartTime != nil {
		query = query.Where("first_observed_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		query = query.Where("first_observed_at <= ?", *f.EndTime)
	}
	if f.CameraID != "" {
		query = query.Where("camera_id = ?", f.CameraID)
	}
	if f.TrackID > 0 {
		query = query.Where("track_id = ?", f.TrackID)
	}
	if f.PersonID != "" {
		query = query.Where("best_person_id = ?", f.PersonID)
	}
	if f.PersonName != "" {
		query = query.Where("best_person_name LIKE ?", "%"+f.PersonName+"%")
	}
	if f.MinSimilarity != nil {
		query = query.Where("best_similarity >= ?", *f.MinSimilarity)
	}
	if f.MaxSimilarity != nil {
		query = query.Where("best_similarity <= ?", *f.MaxSimilarity)
	}

	if f.Status == "recognized" {
		query = query.Where("best_person_id != ''")
	} else if f.Status == "stranger" {
		query = query.Where("(best_person_id = '' OR best_person_id IS NULL)")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.FaceCapture{}, 0, nil
	}

	var records []model.FaceCapture
	offset := (page - 1) * pageSize
	err := query.Order("first_observed_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *faceCaptureRepository) FindExistingImageIDs(ctx context.Context, imageIDs []string) ([]string, error) {
	if len(imageIDs) == 0 {
		return nil, nil
	}

	var bestPanoIDs []string
	if err := r.db.WithContext(ctx).Model(&model.FaceCapture{}).
		Where("best_image_id IN ?", imageIDs).
		Pluck("best_image_id", &bestPanoIDs).Error; err != nil {
		return nil, err
	}

	var bestFaceIDs []string
	if err := r.db.WithContext(ctx).Model(&model.FaceCapture{}).
		Where("best_face_image_id IN ?", imageIDs).
		Pluck("best_face_image_id", &bestFaceIDs).Error; err != nil {
		return nil, err
	}

	idMap := make(map[string]struct{}, len(bestPanoIDs)+len(bestFaceIDs))
	for _, id := range bestPanoIDs {
		if id != "" {
			idMap[id] = struct{}{}
		}
	}
	for _, id := range bestFaceIDs {
		if id != "" {
			idMap[id] = struct{}{}
		}
	}

	// 针对未直接命中 best 列的 imageIDs，检查 snapshots_json 中是否存在
	var remaining []string
	for _, id := range imageIDs {
		if _, ok := idMap[id]; !ok && id != "" {
			remaining = append(remaining, id)
		}
	}
	if len(remaining) > 0 {
		for _, id := range remaining {
			var count int64
			if err := r.db.WithContext(ctx).Model(&model.FaceCapture{}).
				Where("snapshots_json LIKE ?", "%"+id+"%").
				Count(&count).Error; err == nil && count > 0 {
				idMap[id] = struct{}{}
			}
		}
	}

	res := make([]string, 0, len(idMap))
	for id := range idMap {
		res = append(res, id)
	}
	return res, nil
}

func (r *faceCaptureRepository) FindExpired(ctx context.Context, before time.Time, limit int) ([]model.FaceCapture, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.FaceCapture
	err := r.db.WithContext(ctx).
		Unscoped().
		Where("first_observed_at < ?", before).
		Order("first_observed_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (r *faceCaptureRepository) FindOldest(ctx context.Context, limit int) ([]model.FaceCapture, error) {
	if limit <= 0 {
		limit = 200
	}
	var records []model.FaceCapture
	err := r.db.WithContext(ctx).
		Unscoped().
		Order("first_observed_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (r *faceCaptureRepository) HardDeleteBatch(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Delete(&model.FaceCapture{}).Error
}

func (r *faceCaptureRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.FaceCapture{}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}
