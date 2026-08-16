package repository

import (
	"context"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// AuthRepository 提供认证中间件读取用户有效角色所需的数据访问。
type AuthRepository interface {
	GetActiveRolesByUserID(ctx context.Context, userID uint64) ([]model.Role, error)
}

type authRepository struct {
	db *gorm.DB
}

// NewAuthRepository 创建认证数据访问实例。
func NewAuthRepository(db *gorm.DB) AuthRepository {
	return &authRepository{db: db}
}

// GetActiveRolesByUserID 返回用户绑定的启用角色；用户不存在、被禁用或已软删时
// 通过 JOIN users 过滤直接返回空，调用方据此判定未认证（合并原先的 HasActiveUser 查询）。
func (r *authRepository) GetActiveRolesByUserID(ctx context.Context, userID uint64) ([]model.Role, error) {
	var roles []model.Role
	err := r.db.WithContext(ctx).
		Model(&model.Role{}).
		Select("roles.*").
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Joins("JOIN users ON users.id = user_roles.user_id").
		Where("user_roles.user_id = ? AND users.status = ? AND users.deleted_at IS NULL AND roles.status = ?", userID, model.StatusEnabled, model.StatusEnabled).
		Order("roles.sort asc, roles.id asc").
		Find(&roles).Error
	if err != nil {
		return nil, err
	}
	return roles, nil
}
