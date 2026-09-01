package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// ErrLimitExceeded 表示实体关联数量超过上限。
var ErrLimitExceeded = errors.New("repository: limit exceeded")

// PersonFaceRepository 人员人脸样本数据访问接口。
type PersonFaceRepository interface {
	Create(ctx context.Context, face *model.PersonFace) error
	Delete(ctx context.Context, personID, faceID string) (*model.PersonFace, error)
	GetByFaceID(ctx context.Context, personID, faceID string) (*model.PersonFace, error)
	ListByPersonID(ctx context.Context, personID string) ([]model.PersonFace, error)
	CountByPersonID(ctx context.Context, personID string) (int64, error)
	CountByPersonIDs(ctx context.Context, personIDs []string) (map[string]int64, error)
	GetActiveFaceBySHA256(ctx context.Context, sha256 string) (*model.PersonFace, error)
	DeleteAllByPersonID(ctx context.Context, personID string) ([]model.PersonFace, error)
}

type personFaceRepository struct {
	db *gorm.DB
}

// NewPersonFaceRepository 创建 PersonFaceRepository 实例。
func NewPersonFaceRepository(db *gorm.DB) PersonFaceRepository {
	return &personFaceRepository{db: db}
}

// Create 在事务内保存人脸样本（同时检查未软删除样本数量是否小于 10）。
func (r *personFaceRepository) Create(ctx context.Context, face *model.PersonFace) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.PersonFace{}).Where("person_id = ?", face.PersonID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 10 {
			return ErrLimitExceeded
		}
		if err := tx.Create(face).Error; err != nil {
			return writeError(err)
		}
		return nil
	})
}

// Delete 软删除指定人员的单个人脸样本，并返回被删除的记录（供上层清理文件对象）。
func (r *personFaceRepository) Delete(ctx context.Context, personID, faceID string) (*model.PersonFace, error) {
	var face model.PersonFace
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("person_id = ? AND face_id = ?", personID, faceID).First(&face).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		res := tx.Where("id = ?", face.ID).Delete(&model.PersonFace{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, writeError(err)
	}
	return &face, nil
}

// GetByFaceID 查询指定人员的单个人脸样本。
func (r *personFaceRepository) GetByFaceID(ctx context.Context, personID, faceID string) (*model.PersonFace, error) {
	var face model.PersonFace
	if err := r.db.WithContext(ctx).Where("person_id = ? AND face_id = ?", personID, faceID).First(&face).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &face, nil
}

// ListByPersonID 查询指定人员的所有有效人脸样本，按创建时间降序。
func (r *personFaceRepository) ListByPersonID(ctx context.Context, personID string) ([]model.PersonFace, error) {
	var faces []model.PersonFace
	if err := r.db.WithContext(ctx).Where("person_id = ?", personID).Order("id DESC").Find(&faces).Error; err != nil {
		return nil, err
	}
	return faces, nil
}

// CountByPersonID 统计指定人员的有效人脸样本数。
func (r *personFaceRepository) CountByPersonID(ctx context.Context, personID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.PersonFace{}).Where("person_id = ?", personID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountByPersonIDs 批量统计多个人员的有效人脸样本数。
func (r *personFaceRepository) CountByPersonIDs(ctx context.Context, personIDs []string) (map[string]int64, error) {
	result := make(map[string]int64, len(personIDs))
	if len(personIDs) == 0 {
		return result, nil
	}
	type row struct {
		PersonID string `gorm:"column:person_id"`
		Total    int64  `gorm:"column:total"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&model.PersonFace{}).
		Select("person_id, count(*) as total").
		Where("person_id IN ?", personIDs).
		Group("person_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.PersonID] = r.Total
	}
	return result, nil
}

// GetActiveFaceBySHA256 查询指定图片哈希在活动样本中的记录（用于全局精确查重）。
func (r *personFaceRepository) GetActiveFaceBySHA256(ctx context.Context, sha256 string) (*model.PersonFace, error) {
	var face model.PersonFace
	if err := r.db.WithContext(ctx).Where("raw_image_sha256 = ?", sha256).First(&face).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &face, nil
}

// DeleteAllByPersonID 删除人员名下的所有人脸样本，并返回被删除的记录集合。
func (r *personFaceRepository) DeleteAllByPersonID(ctx context.Context, personID string) ([]model.PersonFace, error) {
	var faces []model.PersonFace
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("person_id = ?", personID).Find(&faces).Error; err != nil {
			return err
		}
		if len(faces) == 0 {
			return nil
		}
		return tx.Where("person_id = ?", personID).Delete(&model.PersonFace{}).Error
	})
	if err != nil {
		return nil, writeError(err)
	}
	return faces, nil
}
