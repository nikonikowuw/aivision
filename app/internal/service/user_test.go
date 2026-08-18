package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

func newTestUserService(db *gorm.DB) UserService {
	userRepo := repository.NewUserRepository(db)
	deptRepo := repository.NewDepartmentRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	return NewUserService(userRepo, deptRepo, roleRepo)
}

func TestUserServiceCRUD(t *testing.T) {
	db := setupTestDB(t)
	srv := newTestUserService(db)
	ctx := context.Background()
	admin := model.User{BaseModel: model.BaseModel{ID: model.AdminUserID}, Username: model.AdminUsername, Status: model.StatusEnabled}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create reserved admin: %v", err)
	}

	dept := model.Department{Name: "研发部", Status: model.StatusEnabled}
	if err := db.Create(&dept).Error; err != nil {
		t.Fatalf("create dept failed: %v", err)
	}

	role := model.Role{Name: "管理员", Code: "admin_role", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role failed: %v", err)
	}

	// 1. 创建用户
	statusEnabled := model.StatusEnabled
	u, err := srv.CreateUser(ctx, &SaveUserInput{
		Username: " testuser ",
		Password: "password",
		Nickname: " Test User ",
		DeptID:   dept.ID,
		Status:   &statusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if u.Username != "testuser" {
		t.Errorf("username = %q, want testuser", u.Username)
	}
	if u.Nickname != "Test User" {
		t.Errorf("nickname = %q, want 'Test User'", u.Nickname)
	}

	// 2. 创建重复用户 → 1003
	_, err = srv.CreateUser(ctx, &SaveUserInput{
		Username: "testuser",
	})
	wantErrCode(t, err, errno.CodeUsernameTaken)

	// 3. 创建用户时绑定不存在的部门 → 1011
	_, err = srv.CreateUser(ctx, &SaveUserInput{
		Username: "invalid_dept_user",
		DeptID:   99999,
	})
	wantErrCode(t, err, errno.CodeNotFound)

	// 4. 列表可见（包含部门名称）
	res, err := srv.GetPage(ctx, &UserPageQuery{Username: "testuser"})
	if err != nil {
		t.Fatalf("GetPage failed: %v", err)
	}
	if res.Total != 1 || res.Items[0].Username != "testuser" {
		t.Errorf("GetPage expected 1 item with username testuser, got %d", res.Total)
	}
	if res.Items[0].DeptName != "研发部" {
		t.Errorf("expected deptName '研发部', got %q", res.Items[0].DeptName)
	}

	// 5. 编辑用户
	updated, err := srv.UpdateUser(ctx, u.ID, &SaveUserInput{
		Username: "testuser_updated",
		Nickname: "Test User Updated",
	})
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}
	if updated.Username != "testuser_updated" {
		t.Errorf("updated username = %q, want testuser_updated", updated.Username)
	}
	if updated.Nickname != "Test User Updated" {
		t.Errorf("updated nickname = %q, want Test User Updated", updated.Nickname)
	}

	// 6. 更新为重复用户名 → 1003
	u2, _ := srv.CreateUser(ctx, &SaveUserInput{
		Username: "anotheruser",
	})
	_, err = srv.UpdateUser(ctx, u2.ID, &SaveUserInput{
		Username: "testuser_updated",
	})
	wantErrCode(t, err, errno.CodeUsernameTaken)

	// 7. 分配角色（成功与失败）与查询绑定的角色 ID
	if err := srv.AssignRoles(ctx, u.ID, []uint64{role.ID}); err != nil {
		t.Fatalf("AssignRoles failed: %v", err)
	}
	roleIDs, err := srv.GetRoleIDs(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetRoleIDs failed: %v", err)
	}
	if len(roleIDs) != 1 || roleIDs[0] != role.ID {
		t.Errorf("GetRoleIDs = %v, want [%d]", roleIDs, role.ID)
	}

	err = srv.AssignRoles(ctx, u.ID, []uint64{99999})
	wantErrCode(t, err, errno.CodeNotFound)

	// 8. 重置密码
	if err := srv.ResetPassword(ctx, u.ID, "654321"); err != nil {
		t.Fatalf("ResetPassword failed: %v", err)
	}

	// 9. 更新状态
	if err := srv.UpdateStatus(ctx, u.ID, model.StatusDisabled); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	// 10. 软删用户与重复删除 1011
	if err := srv.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	wantErrCode(t, srv.DeleteUser(ctx, u.ID), errno.CodeNotFound)

	// 11. 删除后不可见
	res2, _ := srv.GetPage(ctx, &UserPageQuery{Username: "testuser_updated"})
	if res2.Total != 0 {
		t.Errorf("expected 0 users after delete, got %d", res2.Total)
	}

	// 12. admin 账号保护（不可删、不可停用、不可改用户名、UpdateUser 时不可禁用）
	err = srv.UpdateStatus(ctx, admin.ID, model.StatusDisabled)
	wantErrCode(t, err, errno.CodeAdminUserProtected)
	err = srv.DeleteUser(ctx, admin.ID)
	wantErrCode(t, err, errno.CodeAdminUserProtected)

	// UpdateUser 改名 admin 失败
	_, err = srv.UpdateUser(ctx, admin.ID, &SaveUserInput{Username: "admin_renamed"})
	wantErrCode(t, err, errno.CodeAdminUserProtected)

	// UpdateUser 禁用 admin 失败
	statusDisabled := model.StatusDisabled
	_, err = srv.UpdateUser(ctx, admin.ID, &SaveUserInput{Username: "admin", Status: &statusDisabled})
	wantErrCode(t, err, errno.CodeAdminUserProtected)
}

func TestUserServiceBatchOperations(t *testing.T) {
	db := setupTestDB(t)
	srv := newTestUserService(db)
	ctx := context.Background()

	protected := model.User{BaseModel: model.BaseModel{ID: model.AdminUserID}, Username: "renamed-admin", Status: model.StatusEnabled}
	user1 := model.User{Username: "batch-user-1", Status: model.StatusEnabled}
	user2 := model.User{Username: "batch-user-2", Status: model.StatusEnabled}
	for _, user := range []*model.User{&protected, &user1, &user2} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user %q: %v", user.Username, err)
		}
	}
	role := model.Role{Name: "批量角色", Code: "batch-role", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&[]model.UserRole{
		{UserID: user1.ID, RoleID: role.ID},
		{UserID: user2.ID, RoleID: role.ID},
	}).Error; err != nil {
		t.Fatalf("create user roles: %v", err)
	}

	wantErrCode(t, srv.BatchDelete(ctx, []uint64{0, user1.ID}), errno.CodeInvalidParam)
	wantErrCode(t, srv.BatchUpdateStatus(ctx, []uint64{}, model.StatusEnabled), errno.CodeInvalidParam)
	wantErrCode(t, srv.BatchUpdateStatus(ctx, []uint64{user1.ID}, 2), errno.CodeInvalidParam)

	// ID=1 受保护，即使用户名不是 admin 也不能禁用、删除或改名。
	wantErrCode(t, srv.UpdateStatus(ctx, protected.ID, model.StatusDisabled), errno.CodeAdminUserProtected)
	wantErrCode(t, srv.DeleteUser(ctx, protected.ID), errno.CodeAdminUserProtected)
	_, err := srv.UpdateUser(ctx, protected.ID, &SaveUserInput{Username: protected.Username})
	wantErrCode(t, err, errno.CodeAdminUserProtected)
	wantErrCode(t, srv.BatchUpdateStatus(ctx, []uint64{user1.ID, protected.ID}, model.StatusDisabled), errno.CodeAdminUserProtected)
	wantErrCode(t, srv.BatchDelete(ctx, []uint64{user1.ID, protected.ID}), errno.CodeAdminUserProtected)

	var untouched model.User
	if err := db.First(&untouched, user1.ID).Error; err != nil {
		t.Fatalf("protected batch must not delete user1: %v", err)
	}
	if untouched.Status != model.StatusEnabled {
		t.Fatalf("protected batch must not update user1 status: got %d", untouched.Status)
	}

	if err := srv.BatchUpdateStatus(ctx, []uint64{user1.ID, user2.ID}, model.StatusDisabled); err != nil {
		t.Fatalf("batch update status: %v", err)
	}
	var disabled model.User
	if err := db.First(&disabled, user2.ID).Error; err != nil {
		t.Fatalf("find disabled user: %v", err)
	}
	if disabled.Status != model.StatusDisabled {
		t.Fatalf("batch status = %d, want %d", disabled.Status, model.StatusDisabled)
	}

	if err := srv.BatchDelete(ctx, []uint64{user1.ID, user2.ID, user1.ID}); err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	var deleted model.User
	if err := db.Unscoped().First(&deleted, user1.ID).Error; err != nil {
		t.Fatalf("find soft-deleted user: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Fatal("batch delete did not soft-delete user1")
	}
	var relationCount int64
	if err := db.Model(&model.UserRole{}).Where("user_id IN ?", []uint64{user1.ID, user2.ID}).Count(&relationCount).Error; err != nil {
		t.Fatalf("count deleted user roles: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("batch delete left %d user-role relations", relationCount)
	}
}
