package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// PersonFilter 人员分页查询条件。
type PersonFilter struct {
	Page     int
	PageSize int
	PersonID string // 精确匹配
	Name     string // 模糊匹配
}

// PersonRepository 人员数据访问接口。
type PersonRepository interface {
	Create(ctx context.Context, person *model.Person) error
	UpdateName(ctx context.Context, personID, name string) (*model.Person, error)
	Delete(ctx context.Context, personID string) (bool, error)
	BatchDelete(ctx context.Context, personIDs []string) error
	GetByPersonID(ctx context.Context, personID string) (*model.Person, error)
	GetByPersonIDWithDeleted(ctx context.Context, personID string) (*model.Person, error)
	ListPage(ctx context.Context, filter *PersonFilter) ([]model.Person, int64, error)
	RestoreAndUpdate(ctx context.Context, id uint64, name string) (*model.Person, error)
}

type personRepository struct {
	db *gorm.DB
}

// NewPersonRepository 创建 PersonRepository 实例。
func NewPersonRepository(db *gorm.DB) PersonRepository {
	return &personRepository{db: db}
}

// Create 持久化人员记录，并将数据库唯一键冲突映射为仓储错误。
func (r *personRepository) Create(ctx context.Context, person *model.Person) error {
	return writeError(r.db.WithContext(ctx).Create(person).Error)
}

// UpdateName 根据对外唯一 person_id 更新活动人员姓名，未找到返回 ErrNotFound。
func (r *personRepository) UpdateName(ctx context.Context, personID, name string) (*model.Person, error) {
	var person model.Person
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("person_id = ?", personID).First(&person).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		// 使用显式 UPDATE，避免 Save 在目标被并发软删除后退化为 INSERT/UPSERT。
		result := tx.Model(&model.Person{}).
			Where("id = ?", person.ID).
			Updates(map[string]any{
				"name":       name,
				"updated_at": time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("id = ?", person.ID).First(&person).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, writeError(err)
	}
	return &person, nil
}

// Delete 根据 person_id 软删除活动人员及关联人脸样本；不存在或已删除返回 (false, nil)。
func (r *personRepository) Delete(ctx context.Context, personID string) (bool, error) {
	var affected bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("person_id = ?", personID).Delete(&model.Person{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		affected = true
		if err := tx.Where("person_id = ?", personID).Delete(&model.PersonFace{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, writeError(err)
	}
	return affected, nil
}

// BatchDelete 批量软删除活动人员及关联人脸样本；不存在或已删除项会被忽略。
func (r *personRepository) BatchDelete(ctx context.Context, personIDs []string) error {
	if len(personIDs) == 0 {
		return nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("person_id IN ?", personIDs).Delete(&model.Person{}).Error; err != nil {
			return err
		}
		return tx.Where("person_id IN ?", personIDs).Delete(&model.PersonFace{}).Error
	})
	if err != nil {
		return writeError(err)
	}
	return nil
}

// GetByPersonID 查询活动状态人员。
func (r *personRepository) GetByPersonID(ctx context.Context, personID string) (*model.Person, error) {
	var person model.Person
	if err := r.db.WithContext(ctx).Where("person_id = ?", personID).First(&person).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &person, nil
}

// GetByPersonIDWithDeleted 查询人员（包含软删除记录）。
func (r *personRepository) GetByPersonIDWithDeleted(ctx context.Context, personID string) (*model.Person, error) {
	var person model.Person
	if err := r.db.WithContext(ctx).Unscoped().Where("person_id = ?", personID).First(&person).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &person, nil
}

// ListPage 分页查询活动人员；按 created_at desc, id desc 排序。
func (r *personRepository) ListPage(ctx context.Context, filter *PersonFilter) ([]model.Person, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.Person{})
	if filter.PersonID != "" {
		db = db.Where("person_id = ?", filter.PersonID)
	}
	if filter.Name != "" {
		db = db.Where("name LIKE ?", "%"+filter.Name+"%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Person{}, 0, nil
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var persons []model.Person
	offset := (page - 1) * pageSize
	if err := db.Order("created_at desc, id desc").Offset(offset).Limit(pageSize).Find(&persons).Error; err != nil {
		return nil, 0, err
	}
	return persons, total, nil
}

// RestoreAndUpdate 恢复已软删除人员记录并更新姓名，保持内部 id 与原创建时间不变。
func (r *personRepository) RestoreAndUpdate(ctx context.Context, id uint64, name string) (*model.Person, error) {
	var person model.Person
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Unscoped().Model(&model.Person{}).Where("id = ?", id).Updates(map[string]any{
			"deleted_at": 0,
			"name":       name,
			"updated_at": now,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("id = ?", id).First(&person).Error
	})
	if err != nil {
		return nil, writeError(err)
	}
	return &person, nil
}
