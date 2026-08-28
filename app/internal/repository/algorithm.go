package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// AlgorithmFilter 算法分页查询过滤条件。
type AlgorithmFilter struct {
	Page          int
	PageSize      int
	AlgorithmType string
	Keyword       string // 模糊匹配 algorithm_id 或 name
}

// AlgorithmRepository 算法及算法版本仓储接口。
type AlgorithmRepository interface {
	// 算法主表操作
	UpsertAlgorithm(ctx context.Context, algo *model.Algorithm) error
	GetAlgorithmByID(ctx context.Context, algorithmID string) (*model.Algorithm, error)
	ListAlgorithms(ctx context.Context, filter *AlgorithmFilter) ([]model.Algorithm, int64, error)
	DeleteAlgorithm(ctx context.Context, algorithmID string) error

	// 算法版本表操作
	UpsertVersion(ctx context.Context, version *model.AlgorithmVersion) error
	GetVersion(ctx context.Context, algorithmID, version string) (*model.AlgorithmVersion, error)
	ListVersions(ctx context.Context, algorithmID string) ([]model.AlgorithmVersion, error)
	ActivateVersion(ctx context.Context, algorithmID, version string) error
	DeleteVersion(ctx context.Context, algorithmID, version string) error
	CountActiveInstances(ctx context.Context, algorithmID, version string) (int64, error)
}

type algorithmRepository struct {
	db *gorm.DB
}

// NewAlgorithmRepository 创建 AlgorithmRepository 实例。
func NewAlgorithmRepository(db *gorm.DB) AlgorithmRepository {
	return &algorithmRepository{db: db}
}

func (r *algorithmRepository) UpsertAlgorithm(ctx context.Context, algo *model.Algorithm) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.Algorithm
		err := tx.Where("algorithm_id = ?", algo.AlgorithmID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(tx.Create(algo).Error)
		} else if err != nil {
			return err
		}

		// 更新字段
		updates := map[string]any{
			"name":           algo.Name,
			"algorithm_type": algo.AlgorithmType,
			"alarm_type_id":  algo.AlarmTypeID,
			"description":    algo.Description,
			"updated_at":     time.Now(),
		}
		if algo.ActiveVersion != "" {
			updates["active_version"] = algo.ActiveVersion
		}
		return tx.Model(&model.Algorithm{}).Where("id = ?", existing.ID).Updates(updates).Error
	})
}

func (r *algorithmRepository) GetAlgorithmByID(ctx context.Context, algorithmID string) (*model.Algorithm, error) {
	var algo model.Algorithm
	err := r.db.WithContext(ctx).
		Preload("Versions", func(db *gorm.DB) *gorm.DB {
			return db.Order("algorithm_versions.created_at DESC")
		}).
		Where("algorithm_id = ?", algorithmID).
		First(&algo).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &algo, err
}

func (r *algorithmRepository) ListAlgorithms(ctx context.Context, filter *AlgorithmFilter) ([]model.Algorithm, int64, error) {
	var items []model.Algorithm
	var total int64

	db := r.db.WithContext(ctx).Model(&model.Algorithm{})
	if filter != nil {
		if filter.AlgorithmType != "" {
			db = db.Where("algorithm_type = ?", filter.AlgorithmType)
		}
		if filter.Keyword != "" {
			pattern := "%" + filter.Keyword + "%"
			db = db.Where("algorithm_id LIKE ? OR name LIKE ?", pattern, pattern)
		}
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := 1
	pageSize := 20
	if filter != nil {
		if filter.Page > 0 {
			page = filter.Page
		}
		if filter.PageSize > 0 {
			pageSize = filter.PageSize
		}
	}

	err := db.Preload("Versions", func(db *gorm.DB) *gorm.DB {
		return db.Order("algorithm_versions.created_at DESC")
	}).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error

	return items, total, err
}

func (r *algorithmRepository) DeleteAlgorithm(ctx context.Context, algorithmID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除所有版本
		if err := tx.Where("algorithm_id = ?", algorithmID).Delete(&model.AlgorithmVersion{}).Error; err != nil {
			return err
		}
		// 删除算法主记录
		result := tx.Where("algorithm_id = ?", algorithmID).Delete(&model.Algorithm{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *algorithmRepository) UpsertVersion(ctx context.Context, version *model.AlgorithmVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.AlgorithmVersion
		err := tx.Where("algorithm_id = ? AND version = ?", version.AlgorithmID, version.Version).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(tx.Create(version).Error)
		} else if err != nil {
			return err
		}

		updates := map[string]any{
			"platform_id":         version.PlatformID,
			"min_adapter_version": version.MinAdapterVersion,
			"package_root":        version.PackageRoot,
			"fps_tiers":           version.FPSTiers,
			"config_schema":       version.ConfigSchema,
			"manifest_raw":        version.ManifestRaw,
			"package_size_bytes":  version.PackageSizeBytes,
			"is_active":           version.IsActive,
			"updated_at":          time.Now(),
		}
		return tx.Model(&model.AlgorithmVersion{}).Where("id = ?", existing.ID).Updates(updates).Error
	})
}

func (r *algorithmRepository) GetVersion(ctx context.Context, algorithmID, version string) (*model.AlgorithmVersion, error) {
	var ver model.AlgorithmVersion
	err := r.db.WithContext(ctx).
		Where("algorithm_id = ? AND version = ?", algorithmID, version).
		First(&ver).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &ver, err
}

func (r *algorithmRepository) ListVersions(ctx context.Context, algorithmID string) ([]model.AlgorithmVersion, error) {
	var items []model.AlgorithmVersion
	err := r.db.WithContext(ctx).
		Where("algorithm_id = ?", algorithmID).
		Order("created_at DESC").
		Find(&items).Error
	return items, err
}

func (r *algorithmRepository) ActivateVersion(ctx context.Context, algorithmID, version string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 确认该版本存在
		var target model.AlgorithmVersion
		if err := tx.Where("algorithm_id = ? AND version = ?", algorithmID, version).First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		// 2. 将该算法所有版本置为非激活
		if err := tx.Model(&model.AlgorithmVersion{}).
			Where("algorithm_id = ?", algorithmID).
			Update("is_active", false).Error; err != nil {
			return err
		}

		// 3. 激活指定版本
		if err := tx.Model(&model.AlgorithmVersion{}).
			Where("id = ?", target.ID).
			Update("is_active", true).Error; err != nil {
			return err
		}

		// 4. 同步更新 algorithms 主表的 active_version
		return tx.Model(&model.Algorithm{}).
			Where("algorithm_id = ?", algorithmID).
			Update("active_version", version).Error
	})
}

func (r *algorithmRepository) DeleteVersion(ctx context.Context, algorithmID, version string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target model.AlgorithmVersion
		if err := tx.Where("algorithm_id = ? AND version = ?", algorithmID, version).First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		// 删除该版本
		if err := tx.Delete(&target).Error; err != nil {
			return err
		}

		// 如果删除的是当前激活版本，且还有其他版本，将最新的一个设为激活；若无其他版本则清空主表或删除主表
		var remaining []model.AlgorithmVersion
		if err := tx.Where("algorithm_id = ?", algorithmID).Order("created_at DESC").Find(&remaining).Error; err != nil {
			return err
		}

		if len(remaining) == 0 {
			// 所有版本都删除了，级联软删除主表
			return tx.Where("algorithm_id = ?", algorithmID).Delete(&model.Algorithm{}).Error
		}

		if target.IsActive {
			newActive := remaining[0]
			if err := tx.Model(&model.AlgorithmVersion{}).Where("id = ?", newActive.ID).Update("is_active", true).Error; err != nil {
				return err
			}
			return tx.Model(&model.Algorithm{}).Where("algorithm_id = ?", algorithmID).Update("active_version", newActive.Version).Error
		}

		return nil
	})
}

func (r *algorithmRepository) CountActiveInstances(ctx context.Context, algorithmID, version string) (int64, error) {
	// 在未来的 analysis_tasks 表建立前返回 0；后续在此补充查询活跃任务数
	return 0, nil
}
