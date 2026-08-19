package repository

import (
	"context"
	"errors"
	"time"

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

// AuthRepository 提供认证与令牌管理所需的数据访问。
type AuthRepository interface {
	GetActiveIdentity(ctx context.Context, userID uint64) (*AuthIdentity, error)
	CreateRefreshToken(ctx context.Context, token *model.RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenStr string) (*model.RefreshToken, error)
	// RotateRefreshToken 原子地吊销仍有效的旧令牌并创建新令牌。
	RotateRefreshToken(ctx context.Context, oldTokenStr string, newToken *model.RefreshToken) (bool, error)
	RevokeRefreshToken(ctx context.Context, tokenStr string) error
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
		Where("users.id = ? AND users.status = ? AND users.deleted_at = 0 AND roles.status = ? AND roles.deleted_at = 0", userID, model.StatusEnabled, model.StatusEnabled).
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

func (r *authRepository) CreateRefreshToken(ctx context.Context, token *model.RefreshToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *authRepository) GetRefreshToken(ctx context.Context, tokenStr string) (*model.RefreshToken, error) {
	var token model.RefreshToken
	if err := r.db.WithContext(ctx).Where("token = ?", tokenStr).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &token, nil
}

func (r *authRepository) RotateRefreshToken(ctx context.Context, oldTokenStr string, newToken *model.RefreshToken) (bool, error) {
	consumed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.RefreshToken{}).
			Where("token = ? AND revoked = ? AND expires_at > ?", oldTokenStr, false, time.Now()).
			Update("revoked", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}

		if err := tx.Create(newToken).Error; err != nil {
			return err
		}
		consumed = true
		return nil
	})
	return consumed, err
}

func (r *authRepository) RevokeRefreshToken(ctx context.Context, tokenStr string) error {
	return r.db.WithContext(ctx).Model(&model.RefreshToken{}).
		Where("token = ?", tokenStr).
		Update("revoked", true).Error
}
