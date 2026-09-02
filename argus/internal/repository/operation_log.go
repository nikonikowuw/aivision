package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"argus/app/internal/model"
)

// OperationLogFilter 查询操作日志的筛选参数。
type OperationLogFilter struct {
	Page       int
	PageSize   int
	Username   string
	Module     string
	StatusCode int
	StartTime  *time.Time
	EndTime    *time.Time
}

// OperationLogRepository 操作日志数据访问接口。
type OperationLogRepository interface {
	Create(ctx context.Context, log *model.OperationLog) error
	GetByID(ctx context.Context, id uint64) (*model.OperationLog, error)
	ListPage(ctx context.Context, filter *OperationLogFilter) ([]model.OperationLog, int64, error)
	// DeleteExpired 物理删除早于指定时间的操作日志（分批限额）。
	DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error)
	// CountTotal 统计操作日志总数。
	CountTotal(ctx context.Context) (int64, error)
}

type operationLogRepository struct {
	db *gorm.DB
}

// NewOperationLogRepository 创建 OperationLogRepository 实例。
func NewOperationLogRepository(db *gorm.DB) OperationLogRepository {
	return &operationLogRepository{db: db}
}

func (r *operationLogRepository) Create(ctx context.Context, log *model.OperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *operationLogRepository) GetByID(ctx context.Context, id uint64) (*model.OperationLog, error) {
	var item model.OperationLog
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *operationLogRepository) ListPage(ctx context.Context, filter *OperationLogFilter) ([]model.OperationLog, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.OperationLog{})

	if filter.Username != "" {
		db = db.Where("username LIKE ?", "%"+filter.Username+"%")
	}
	if filter.Module != "" {
		db = db.Where("module = ?", filter.Module)
	}
	if filter.StatusCode > 0 {
		db = db.Where("status_code = ?", filter.StatusCode)
	}
	if filter.StartTime != nil {
		db = db.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		db = db.Where("created_at < ?", *filter.EndTime)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)

	var items []model.OperationLog
	offset := (page - 1) * pageSize
	if err := db.Order("created_at desc, id desc").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (r *operationLogRepository) DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 200
	}
	var ids []uint64
	err := r.db.WithContext(ctx).
		Model(&model.OperationLog{}).
		Where("created_at < ?", before).
		Order("created_at ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&model.OperationLog{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *operationLogRepository) CountTotal(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.OperationLog{}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}
