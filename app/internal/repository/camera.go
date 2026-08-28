package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// CameraFilter 摄像头分页查询条件。
type CameraFilter struct {
	Page     int
	PageSize int
	Name     string // 名称模糊匹配
}

// CameraRepository 摄像头数据访问接口。
type CameraRepository interface {
	Create(ctx context.Context, camera *model.Camera) error
	Update(ctx context.Context, camera *model.Camera) error
	Delete(ctx context.Context, id uint64) (bool, error)
	BatchDelete(ctx context.Context, ids []uint64) error
	GetByID(ctx context.Context, id uint64) (*model.Camera, error)
	GetByCameraID(ctx context.Context, cameraID string) (*model.Camera, error)
	ListPage(ctx context.Context, filter *CameraFilter) ([]model.Camera, int64, error)
	// ListAll 返回全部未软删摄像头（规模受平台摄像头上限约束，供任务配置下拉
	// 全量过滤「未建任务」摄像头，无分页，design §8）。
	ListAll(ctx context.Context) ([]model.Camera, error)
}

type cameraRepository struct {
	db *gorm.DB
}

// NewCameraRepository 创建 CameraRepository 实例。
func NewCameraRepository(db *gorm.DB) CameraRepository {
	return &cameraRepository{db: db}
}

func (r *cameraRepository) Create(ctx context.Context, camera *model.Camera) error {
	return writeError(r.db.WithContext(ctx).Create(camera).Error)
}

func (r *cameraRepository) Update(ctx context.Context, camera *model.Camera) error {
	return writeError(r.db.WithContext(ctx).Save(camera).Error)
}

// Delete 软删除指定摄像头；不存在或已删除返回 (false, nil)。
func (r *cameraRepository) Delete(ctx context.Context, id uint64) (bool, error) {
	result := r.db.WithContext(ctx).Delete(&model.Camera{}, id)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// BatchDelete 批量软删除。
func (r *cameraRepository) BatchDelete(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.Camera{}).Error
}

func (r *cameraRepository) GetByID(ctx context.Context, id uint64) (*model.Camera, error) {
	var camera model.Camera
	if err := r.db.WithContext(ctx).First(&camera, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &camera, nil
}

func (r *cameraRepository) GetByCameraID(ctx context.Context, cameraID string) (*model.Camera, error) {
	var camera model.Camera
	if err := r.db.WithContext(ctx).Where("camera_id = ?", cameraID).First(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &camera, nil
}

// ListPage 分页查询摄像头；name 非空时按名称模糊匹配，结果按 id 倒序。
func (r *cameraRepository) ListPage(ctx context.Context, filter *CameraFilter) ([]model.Camera, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.Camera{})
	if filter.Name != "" {
		db = db.Where("name LIKE ?", "%"+filter.Name+"%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Camera{}, 0, nil
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var cameras []model.Camera
	offset := (page - 1) * pageSize
	if err := db.Order("id desc").Offset(offset).Limit(pageSize).Find(&cameras).Error; err != nil {
		return nil, 0, err
	}
	return cameras, total, nil
}

func (r *cameraRepository) ListAll(ctx context.Context) ([]model.Camera, error) {
	var cameras []model.Camera
	err := r.db.WithContext(ctx).Order("id desc").Find(&cameras).Error
	return cameras, err
}
