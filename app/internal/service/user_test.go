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
	admin, err := srv.CreateUser(ctx, &SaveUserInput{Username: "admin"})
	if err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}
	err = srv.UpdateStatus(ctx, admin.ID, model.StatusDisabled)
	wantErrCode(t, err, errno.CodeInvalidParam)
	err = srv.DeleteUser(ctx, admin.ID)
	wantErrCode(t, err, errno.CodeInvalidParam)

	// UpdateUser 改名 admin 失败
	_, err = srv.UpdateUser(ctx, admin.ID, &SaveUserInput{Username: "admin_renamed"})
	wantErrCode(t, err, errno.CodeInvalidParam)

	// UpdateUser 禁用 admin 失败
	statusDisabled := model.StatusDisabled
	_, err = srv.UpdateUser(ctx, admin.ID, &SaveUserInput{Username: "admin", Status: &statusDisabled})
	wantErrCode(t, err, errno.CodeInvalidParam)
}
