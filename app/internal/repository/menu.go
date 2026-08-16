package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// ErrNotFound 资源不存在哨兵错误：repository 层把 gorm.ErrRecordNotFound
// 映射为领域错误，避免 service 层依赖 ORM 特定哨兵。
var ErrNotFound = errors.New("repository: not found")

// MenuRepository 菜单数据访问接口。
type MenuRepository interface {
	Create(ctx context.Context, menu *model.Menu) error
	Update(ctx context.Context, menu *model.Menu) error
	Delete(ctx context.Context, id uint64) (bool, error)
	GetByID(ctx context.Context, id uint64) (*model.Menu, error)
	ListAll(ctx context.Context) ([]model.Menu, error)
	GetByIDs(ctx context.Context, ids []uint64) ([]model.Menu, error)
	CountByParentID(ctx context.Context, parentID uint64) (int64, error)
	GetMenuIDsByRoleIDs(ctx context.Context, roleIDs []uint64) ([]uint64, error)
}

type menuRepository struct {
	db *gorm.DB
}

// NewMenuRepository 创建 MenuRepository 实例。
func NewMenuRepository(db *gorm.DB) MenuRepository {
	return &menuRepository{db: db}
}

func (r *menuRepository) Create(ctx context.Context, menu *model.Menu) error {
	return r.db.WithContext(ctx).Create(menu).Error
}

func (r *menuRepository) Update(ctx context.Context, menu *model.Menu) error {
	return r.db.WithContext(ctx).Save(menu).Error
}

// Delete 删除菜单；返回是否实际删除了记录（id 不存在时 RowsAffected=0）。
func (r *menuRepository) Delete(ctx context.Context, id uint64) (bool, error) {
	res := r.db.WithContext(ctx).Delete(&model.Menu{}, id)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *menuRepository) GetByID(ctx context.Context, id uint64) (*model.Menu, error) {
	var menu model.Menu
	if err := r.db.WithContext(ctx).First(&menu, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &menu, nil
}

func (r *menuRepository) ListAll(ctx context.Context) ([]model.Menu, error) {
	var menus []model.Menu
	if err := r.db.WithContext(ctx).Order("sort asc, id asc").Find(&menus).Error; err != nil {
		return nil, err
	}
	return menus, nil
}

func (r *menuRepository) GetByIDs(ctx context.Context, ids []uint64) ([]model.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var menus []model.Menu
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Order("sort asc, id asc").Find(&menus).Error; err != nil {
		return nil, err
	}
	return menus, nil
}

func (r *menuRepository) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Menu{}).Where("parent_id = ?", parentID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *menuRepository) GetMenuIDsByRoleIDs(ctx context.Context, roleIDs []uint64) ([]uint64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var menuIDs []uint64
	if err := r.db.WithContext(ctx).Model(&model.RoleMenu{}).
		Joins("JOIN roles ON roles.id = role_menus.role_id").
		Where("role_menus.role_id IN ? AND roles.status = ? AND roles.deleted_at IS NULL", roleIDs, model.StatusEnabled).
		Pluck("distinct role_menus.menu_id", &menuIDs).Error; err != nil {
		return nil, err
	}
	return menuIDs, nil
}
