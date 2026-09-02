package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"argus/app/internal/model"
	argusv1 "argus/app/internal/proto/argus/v1"
)

// ErrLimitExceeded 表示实体关联数量超过上限。
var ErrLimitExceeded = errors.New("repository: limit exceeded")

// ErrFaceGalleryFull 表示人脸底库全局容量已满（达到 5000 条目上限）。
var ErrFaceGalleryFull = errors.New("repository: face gallery full")

// ErrFaceGalleryRevisionMissing 人脸底库版本计数器单行缺失（迁移未初始化或数据被破坏）。
var ErrFaceGalleryRevisionMissing = errors.New("repository: face_gallery_revision singleton row missing")

// MaxFaceGalleryEntries 人脸底库全局条目数硬上限（MVP）。
const MaxFaceGalleryEntries = 5000

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
	CurrentGalleryRevision(ctx context.Context) (uint64, error)
	LoadFaceGallery(ctx context.Context, currentGalleryRevision uint64) (uint64, bool, []*argusv1.FaceGalleryEntry, error)
}

type personFaceRepository struct {
	db *gorm.DB
}

// NewPersonFaceRepository 创建 PersonFaceRepository 实例。
func NewPersonFaceRepository(db *gorm.DB) PersonFaceRepository {
	return &personFaceRepository{db: db}
}

// Create 在事务内保存人脸样本（同时检查全局 5000 上限与单人 10 张上限，并同事务递增底库版本）。
func (r *personFaceRepository) Create(ctx context.Context, face *model.PersonFace) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var globalCount int64
		if err := tx.Model(&model.PersonFace{}).Count(&globalCount).Error; err != nil {
			return err
		}
		if globalCount >= MaxFaceGalleryEntries {
			return ErrFaceGalleryFull
		}

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
		if _, err := BumpFaceGalleryRevisionTx(ctx, tx); err != nil {
			return err
		}
		return nil
	})
}

// Delete 软删除指定人员的单个人脸样本，并同事务递增底库版本（返回被删除的记录供上层清理文件对象）。
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
		if _, err := BumpFaceGalleryRevisionTx(ctx, tx); err != nil {
			return err
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

// DeleteAllByPersonID 删除人员名下的所有人脸样本，并同事务递增底库版本（返回被删除的记录集合）。
func (r *personFaceRepository) DeleteAllByPersonID(ctx context.Context, personID string) ([]model.PersonFace, error) {
	var faces []model.PersonFace
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("person_id = ?", personID).Find(&faces).Error; err != nil {
			return err
		}
		if len(faces) == 0 {
			return nil
		}
		if err := tx.Where("person_id = ?", personID).Delete(&model.PersonFace{}).Error; err != nil {
			return err
		}
		if _, err := BumpFaceGalleryRevisionTx(ctx, tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, writeError(err)
	}
	return faces, nil
}

// BumpFaceGalleryRevisionTx 在指定事务连接上执行单行计数器 face_gallery_revision.revision+1 并返回新值。
// 保证人脸底库变更（新增、删除、级联删除）与版本递增在同一事务内原子提交。
func BumpFaceGalleryRevisionTx(ctx context.Context, tx *gorm.DB) (uint64, error) {
	var rev uint64
	err := tx.WithContext(ctx).Raw(
		"UPDATE face_gallery_revision SET revision = revision + 1 WHERE id = 1 RETURNING revision",
	).Scan(&rev).Error
	if err != nil {
		return 0, err
	}
	if rev == 0 {
		return 0, ErrFaceGalleryRevisionMissing
	}
	return rev, nil
}

// CurrentGalleryRevision 查询当前人脸底库版本号。
func (r *personFaceRepository) CurrentGalleryRevision(ctx context.Context) (uint64, error) {
	var row model.FaceGalleryRevision
	err := r.db.WithContext(ctx).Where("id = ?", 1).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrFaceGalleryRevisionMissing
	}
	if err != nil {
		return 0, err
	}
	return uint64(row.Revision), nil
}

// LoadFaceGallery 在同一只读事务内原子读取 revision 与快照条目。
// 若请求 revision 与库内一致，返回 changed=false 且 entries 为空。
func (r *personFaceRepository) LoadFaceGallery(ctx context.Context, currentGalleryRevision uint64) (uint64, bool, []*argusv1.FaceGalleryEntry, error) {
	var (
		rev     uint64
		changed bool
		entries []*argusv1.FaceGalleryEntry
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.FaceGalleryRevision
		if err := tx.Where("id = ?", 1).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrFaceGalleryRevisionMissing
			}
			return err
		}
		rev = uint64(row.Revision)
		if rev == currentGalleryRevision {
			changed = false
			return nil
		}
		changed = true

		var galleryCount int64
		if err := tx.Model(&model.PersonFace{}).Count(&galleryCount).Error; err != nil {
			return err
		}
		if galleryCount > MaxFaceGalleryEntries {
			return ErrFaceGalleryFull
		}

		type entryRow struct {
			FaceID    string `gorm:"column:face_id"`
			PersonID  string `gorm:"column:person_id"`
			Name      string `gorm:"column:name"`
			Embedding []byte `gorm:"column:embedding"`
		}
		var rows []entryRow
		if err := tx.Table("person_faces").
			Select("person_faces.face_id, person_faces.person_id, coalesce(persons.name, '') as name, person_faces.embedding").
			Joins("LEFT JOIN persons ON persons.person_id = person_faces.person_id AND persons.deleted_at = 0").
			Where("person_faces.deleted_at = 0").
			Order("person_faces.id ASC").
			Scan(&rows).Error; err != nil {
			return err
		}

		entries = make([]*argusv1.FaceGalleryEntry, len(rows))
		for i, row := range rows {
			entries[i] = &argusv1.FaceGalleryEntry{
				FaceId:     row.FaceID,
				PersonId:   row.PersonID,
				PersonName: row.Name,
				Embedding:  row.Embedding,
			}
		}
		return nil
	})
	if err != nil {
		return 0, false, nil, err
	}
	return rev, changed, entries, nil
}
