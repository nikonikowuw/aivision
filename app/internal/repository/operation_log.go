package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
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
		db = db.Where("created_at <= ?", *filter.EndTime)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	var items []model.OperationLog
	offset := (page - 1) * pageSize
	if err := db.Order("created_at desc, id desc").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}
