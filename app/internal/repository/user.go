package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// UserFilter 用户分页查询条件。
type UserFilter struct {
	Page     int
	PageSize int
	Username string
	Nickname string
	Status   *int8
	DeptID   *uint64
}

// UserRepository 用户数据访问接口。
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User, fields map[string]interface{}) error
	Delete(ctx context.Context, id uint64) error
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	ListPage(ctx context.Context, filter *UserFilter) ([]model.User, int64, error)
	ReplaceRoles(ctx context.Context, userID uint64, roleIDs []uint64) error
	GetRoleIDs(ctx context.Context, userID uint64) ([]uint64, error)
	BatchDelete(ctx context.Context, ids []uint64) error
	BatchUpdateStatus(ctx context.Context, ids []uint64, status int8) error
	ChangePasswordAndRevokeSessions(ctx context.Context, userID uint64, passwordHash string) error
}

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建 UserRepository 实例。
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	return writeError(r.db.WithContext(ctx).Create(user).Error)
}

func (r *userRepository) Update(ctx context.Context, user *model.User, fields map[string]interface{}) error {
	return writeError(r.db.WithContext(ctx).Model(user).Updates(fields).Error)
}

func (r *userRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.User{}, id).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *userRepository) BatchDelete(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&model.User{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id IN ?", ids).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *userRepository) BatchUpdateStatus(ctx context.Context, ids []uint64, status int8) error {
	if len(ids) == 0 {
		return nil
	}
	return writeError(r.db.WithContext(ctx).Model(&model.User{}).Where("id IN ?", ids).Update("status", status).Error)
}

func (r *userRepository) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) ListPage(ctx context.Context, filter *UserFilter) ([]model.User, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.User{})

	if filter.Username != "" {
		db = db.Where("username LIKE ?", "%"+filter.Username+"%")
	}
	if filter.Nickname != "" {
		db = db.Where("nickname LIKE ?", "%"+filter.Nickname+"%")
	}
	if filter.Status != nil {
		db = db.Where("status = ?", *filter.Status)
	}
	if filter.DeptID != nil && *filter.DeptID > 0 {
		db = db.Where("dept_id = ?", *filter.DeptID)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.User{}, 0, nil
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)

	var users []model.User
	offset := (page - 1) * pageSize
	if err := db.Omit("password").Order("id desc").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

func (r *userRepository) ReplaceRoles(ctx context.Context, userID uint64, roleIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		if len(roleIDs) == 0 {
			return nil
		}
		rows := make([]model.UserRole, 0, len(roleIDs))
		for _, roleID := range roleIDs {
			rows = append(rows, model.UserRole{UserID: userID, RoleID: roleID})
		}
		return tx.Create(&rows).Error
	})
}

func (r *userRepository) GetRoleIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	var roleIDs []uint64
	if err := r.db.WithContext(ctx).Model(&model.UserRole{}).
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ? AND roles.deleted_at = 0", userID).
		Pluck("distinct user_roles.role_id", &roleIDs).Error; err != nil {
		return nil, err
	}
	return roleIDs, nil
}

func (r *userRepository) ChangePasswordAndRevokeSessions(ctx context.Context, userID uint64, passwordHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.User{}).Where("id = ? AND deleted_at = 0", userID).Update("password", passwordHash)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}

		return tx.Model(&model.RefreshToken{}).
			Where("user_id = ? AND revoked = ?", userID, false).
			Update("revoked", true).Error
	})
}
