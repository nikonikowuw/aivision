package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// ErrDuplicateKey 唯一约束冲突哨兵：把 gorm 跨驱动翻译的重复键错误
// （gorm.ErrDuplicatedKey）映射为领域错误，供 service 层映射为业务错误码。
var ErrDuplicateKey = errors.New("repository: duplicate key")

// writeError 映射写操作的 ORM 错误：唯一约束冲突 → ErrDuplicateKey，其余原样返回。
func writeError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicateKey
	}
	return err
}

// RoleRepository 角色数据访问接口。
type RoleRepository interface {
	Create(ctx context.Context, role *model.Role) error
	Update(ctx context.Context, role *model.Role) error
	Delete(ctx context.Context, id uint64) error
	GetByID(ctx context.Context, id uint64) (*model.Role, error)
	ListPage(ctx context.Context, page, pageSize int) ([]model.Role, int64, error)
	GetMenuIDs(ctx context.Context, roleID uint64) ([]uint64, error)
	ReplaceMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error
}

type roleRepository struct {
	db *gorm.DB
}

// NewRoleRepository 创建 RoleRepository 实例。
func NewRoleRepository(db *gorm.DB) RoleRepository {
	return &roleRepository{db: db}
}

func (r *roleRepository) Create(ctx context.Context, role *model.Role) error {
	return writeError(r.db.WithContext(ctx).Create(role).Error)
}

// Update 保存角色全字段，与 menu repo 同风格。
func (r *roleRepository) Update(ctx context.Context, role *model.Role) error {
	return writeError(r.db.WithContext(ctx).Save(role).Error)
}

// Delete 软删除角色。存在性由 service 层先行校验（GetByID），此处只执行删除。
func (r *roleRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.Role{}, id).Error
}

func (r *roleRepository) GetByID(ctx context.Context, id uint64) (*model.Role, error) {
	var role model.Role
	if err := r.db.WithContext(ctx).First(&role, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &role, nil
}

// ListPage 分页查询角色；排序 sort asc, id asc。
// 分页参数归一（page<1→1；pageSize<1→20；>100→100）由 normalizePage 统一处理。
func (r *roleRepository) ListPage(ctx context.Context, page, pageSize int) ([]model.Role, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.Role{})

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)

	var roles []model.Role
	offset := (page - 1) * pageSize
	if err := db.Order("sort asc, id asc").Offset(offset).Limit(pageSize).Find(&roles).Error; err != nil {
		return nil, 0, err
	}
	return roles, total, nil
}

// GetMenuIDs 返回角色绑定的全部菜单 id：不过滤角色状态（编辑弹窗需要展示禁用角色的
// 既有勾选），但排除已软删菜单（与 menu repo 的 GetMenuIDsByRoleIDs 语义不同）。
func (r *roleRepository) GetMenuIDs(ctx context.Context, roleID uint64) ([]uint64, error) {
	var menuIDs []uint64
	if err := r.db.WithContext(ctx).Model(&model.RoleMenu{}).
		Joins("JOIN menus ON menus.id = role_menus.menu_id").
		Where("role_menus.role_id = ? AND menus.deleted_at IS NULL", roleID).
		Pluck("distinct role_menus.menu_id", &menuIDs).Error; err != nil {
		return nil, err
	}
	return menuIDs, nil
}

// ReplaceMenus 事务内覆盖式写入角色-菜单关联：先删该角色全部 role_menus 再插入新集
// （空集仅删），原子提交。
func (r *roleRepository) ReplaceMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		if len(menuIDs) == 0 {
			return nil
		}
		rows := make([]model.RoleMenu, 0, len(menuIDs))
		for _, menuID := range menuIDs {
			rows = append(rows, model.RoleMenu{RoleID: roleID, MenuID: menuID})
		}
		return tx.Create(&rows).Error
	})
}
