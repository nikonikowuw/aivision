package repository

import (
	"context"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// AuthRole 是认证查询返回的最小角色信息。
type AuthRole struct {
	ID   uint64
	Code string
}

// AuthIdentity 是认证中间件所需的启用用户身份。
type AuthIdentity struct {
	Username string
	Roles    []AuthRole
}

// AuthRepository 提供认证中间件读取有效用户身份所需的数据访问。
type AuthRepository interface {
	GetActiveIdentity(ctx context.Context, userID uint64) (*AuthIdentity, error)
}

type authRepository struct {
	db *gorm.DB
}

// NewAuthRepository 创建认证数据访问实例。
func NewAuthRepository(db *gorm.DB) AuthRepository {
	return &authRepository{db: db}
}

type authIdentityRow struct {
	Username string `gorm:"column:username"`
	RoleID   uint64 `gorm:"column:role_id"`
	RoleCode string `gorm:"column:role_code"`
}

// GetActiveIdentity 返回启用用户的账号和有效角色；用户不存在、被禁用、软删或没有启用角色时返回 nil。
func (r *authRepository) GetActiveIdentity(ctx context.Context, userID uint64) (*AuthIdentity, error) {
	var rows []authIdentityRow
	if err := r.db.WithContext(ctx).
		Model(&model.User{}).
		Select("users.username, roles.id AS role_id, roles.code AS role_code").
		Joins("JOIN user_roles ON user_roles.user_id = users.id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("users.id = ? AND users.status = ? AND users.deleted_at IS NULL AND roles.status = ? AND roles.deleted_at IS NULL", userID, model.StatusEnabled, model.StatusEnabled).
		Order("roles.sort asc, roles.id asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	identity := &AuthIdentity{
		Username: rows[0].Username,
		Roles:    make([]AuthRole, 0, len(rows)),
	}
	for _, row := range rows {
		identity.Roles = append(identity.Roles, AuthRole{ID: row.RoleID, Code: row.RoleCode})
	}
	return identity, nil
}
