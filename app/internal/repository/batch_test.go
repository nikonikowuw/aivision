package repository

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

func newBatchRepositoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func TestUserRepositoryBatchOperations(t *testing.T) {
	db := newBatchRepositoryDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	users := []model.User{
		{Username: "user-1", Status: model.StatusEnabled},
		{Username: "user-2", Status: model.StatusEnabled},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	role := model.Role{Name: "role", Code: "role", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&[]model.UserRole{
		{UserID: users[0].ID, RoleID: role.ID},
		{UserID: users[1].ID, RoleID: role.ID},
	}).Error; err != nil {
		t.Fatalf("create user roles: %v", err)
	}

	if err := repo.BatchUpdateStatus(ctx, []uint64{users[0].ID, users[1].ID}, model.StatusDisabled); err != nil {
		t.Fatalf("batch update status: %v", err)
	}
	var updated model.User
	if err := db.First(&updated, users[0].ID).Error; err != nil {
		t.Fatalf("find updated user: %v", err)
	}
	if updated.Status != model.StatusDisabled {
		t.Fatalf("status = %d, want %d", updated.Status, model.StatusDisabled)
	}

	if err := repo.BatchDelete(ctx, []uint64{users[0].ID, users[1].ID}); err != nil {
		t.Fatalf("batch delete users: %v", err)
	}
	var deleted model.User
	if err := db.Unscoped().First(&deleted, users[0].ID).Error; err != nil {
		t.Fatalf("find deleted user: %v", err)
	}
	if deleted.DeletedAt == 0 {
		t.Fatal("user was not soft-deleted")
	}
	var relationCount int64
	if err := db.Model(&model.UserRole{}).Where("user_id IN ?", []uint64{users[0].ID, users[1].ID}).Count(&relationCount).Error; err != nil {
		t.Fatalf("count user roles: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("batch delete left %d user roles", relationCount)
	}
}

func TestRoleRepositoryBatchDelete(t *testing.T) {
	db := newBatchRepositoryDB(t)
	repo := NewRoleRepository(db)
	ctx := context.Background()

	role := model.Role{Name: "role", Code: "role", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	user := model.User{Username: "user", Status: model.StatusEnabled}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	menu := model.Menu{Type: model.MenuTypeMenu, Name: "menu", Status: model.StatusEnabled}
	if err := db.Create(&menu).Error; err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if err := db.Create(&model.RoleMenu{RoleID: role.ID, MenuID: menu.ID}).Error; err != nil {
		t.Fatalf("create role menu: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}

	if err := repo.BatchDelete(ctx, []uint64{role.ID}); err != nil {
		t.Fatalf("batch delete role: %v", err)
	}
	var deleted model.Role
	if err := db.Unscoped().First(&deleted, role.ID).Error; err != nil {
		t.Fatalf("find deleted role: %v", err)
	}
	if deleted.DeletedAt == 0 {
		t.Fatal("role was not soft-deleted")
	}
	var roleMenuCount, userRoleCount int64
	if err := db.Model(&model.RoleMenu{}).Where("role_id = ?", role.ID).Count(&roleMenuCount).Error; err != nil {
		t.Fatalf("count role menus: %v", err)
	}
	if err := db.Model(&model.UserRole{}).Where("role_id = ?", role.ID).Count(&userRoleCount).Error; err != nil {
		t.Fatalf("count user roles: %v", err)
	}
	if roleMenuCount != 0 || userRoleCount != 0 {
		t.Fatalf("batch delete left relations: roleMenu=%d userRole=%d", roleMenuCount, userRoleCount)
	}
}
