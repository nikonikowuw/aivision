package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"argus/app/internal/model"
)

// SystemConfigRepository 通用系统配置仓储接口
type SystemConfigRepository interface {
	GetByKey(ctx context.Context, key string) (*model.SystemConfig, error)
	SetByKey(ctx context.Context, key string, value string, remark string) error
}

type systemConfigRepository struct {
	db *gorm.DB
}

// NewSystemConfigRepository 创建通用系统配置仓储实例
func NewSystemConfigRepository(db *gorm.DB) SystemConfigRepository {
	return &systemConfigRepository{db: db}
}

func (r *systemConfigRepository) GetByKey(ctx context.Context, key string) (*model.SystemConfig, error) {
	var cfg model.SystemConfig
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&cfg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *systemConfigRepository) SetByKey(ctx context.Context, key string, value string, remark string) error {
	cfg := model.SystemConfig{
		Key:    key,
		Value:  value,
		Remark: remark,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "remark", "updated_at"}),
	}).Create(&cfg).Error
}
