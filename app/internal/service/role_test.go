package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

func newRoleTestService(db *gorm.DB) RoleService {
	return NewRoleService(repository.NewRoleRepository(db), repository.NewMenuRepository(db))
}

// wantErrCode 断言 err 携带指定 errno 业务错误码。
func wantErrCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected errno code %d, got nil", code)
	}
	customErr, ok := err.(*errno.Error)
	if !ok || customErr.Code != code {
		t.Fatalf("error = %v, want errno code %d", err, code)
	}
}

// createTestMenu 直接落库创建一条菜单，返回带自增 ID 的模型。
func createTestMenu(t *testing.T, db *gorm.DB, m model.Menu) model.Menu {
	t.Helper()
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("create menu: %v", err)
	}
	return m
}

// permsEqual 断言权限码切片与期望集合（无序）一致。
func permsEqual(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	for _, g := range got {
		if _, ok := wantSet[g]; !ok {
			return false
		}
	}
	return true
}

func TestRoleServiceCRUDFlow(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	reserved := model.Role{BaseModel: model.BaseModel{ID: model.BuiltinAdminRoleID}, Name: "reserved", Code: "reserved", Status: model.StatusEnabled}
	if err := db.Create(&reserved).Error; err != nil {
		t.Fatalf("create reserved role: %v", err)
	}
	if err := db.Delete(&reserved).Error; err != nil {
		t.Fatalf("delete reserved role: %v", err)
	}

	// 1. 创建两个角色（sort 不同，验证排序）。
	r1, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "editor", Sort: 2, Remark: "a"})
	if err != nil {
		t.Fatalf("CreateRole r1 failed: %v", err)
	}
	if r1.Status != model.StatusEnabled {
		t.Errorf("new role default status = %d, want enabled", r1.Status)
	}
	r2, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "运营", Code: "operator", Sort: 1})
	if err != nil {
		t.Fatalf("CreateRole r2 failed: %v", err)
	}

	// 2. 分页列表：总数与排序（sort asc, id asc）。
	res, err := srv.GetPage(ctx, &RolePageQuery{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("GetPage failed: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("GetPage total = %d, want 2", res.Total)
	}
	if len(res.Items) != 1 || res.Items[0].ID != r2.ID {
		t.Errorf("GetPage items = %+v, want first item r2 (sort asc)", res.Items)
	}
	res2, err := srv.GetPage(ctx, &RolePageQuery{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("GetPage page 2 failed: %v", err)
	}
	if len(res2.Items) != 1 || res2.Items[0].ID != r1.ID {
		t.Errorf("GetPage page 2 items = %+v, want r1", res2.Items)
	}

	// 3. 编辑生效。
	updated, err := srv.UpdateRole(ctx, r1.ID, &SaveRoleInput{Name: "编辑2", Code: "editor", Sort: 3, Remark: "b"})
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
	if updated.Name != "编辑2" || updated.Sort != 3 || updated.Remark != "b" {
		t.Errorf("updated role = %+v, want name/sort/remark updated", updated)
	}

	// 4. 删除后列表不可见。
	if err := srv.DeleteRole(ctx, r1.ID); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}
	res3, err := srv.GetPage(ctx, &RolePageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("GetPage after delete failed: %v", err)
	}
	if res3.Total != 1 || len(res3.Items) != 1 || res3.Items[0].ID != r2.ID {
		t.Errorf("list after delete = %+v, want only r2", res3.Items)
	}

	// 5. 二次删除 → 1011。
	wantErrCode(t, srv.DeleteRole(ctx, r1.ID), errno.CodeNotFound)
}

func TestRoleServicePageFilters(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	if _, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "editor"}); err != nil {
		t.Fatalf("create editor role: %v", err)
	}
	disabled := model.StatusDisabled
	operator, err := srv.CreateRole(ctx, &SaveRoleInput{
		Name: "运营",
		Code: "operator",
	})
	if err != nil {
		t.Fatalf("create operator role: %v", err)
	}
	if _, err := srv.UpdateRole(ctx, operator.ID, &SaveRoleInput{
		Name:   "运营",
		Code:   "operator",
		Status: &disabled,
	}); err != nil {
		t.Fatalf("disable operator role: %v", err)
	}

	byName, err := srv.GetPage(ctx, &RolePageQuery{Name: " 编辑 "})
	if err != nil {
		t.Fatalf("filter by name: %v", err)
	}
	if byName.Total != 1 || len(byName.Items) != 1 || byName.Items[0].Code != "editor" {
		t.Fatalf("filter by name = %+v, want editor only", byName)
	}

	byCode, err := srv.GetPage(ctx, &RolePageQuery{Code: " operator "})
	if err != nil {
		t.Fatalf("filter by code: %v", err)
	}
	if byCode.Total != 1 || len(byCode.Items) != 1 || byCode.Items[0].Name != "运营" {
		t.Fatalf("filter by code = %+v, want operator only", byCode)
	}

	byStatus, err := srv.GetPage(ctx, &RolePageQuery{Status: &disabled})
	if err != nil {
		t.Fatalf("filter by status: %v", err)
	}
	if byStatus.Total != 1 || len(byStatus.Items) != 1 || byStatus.Items[0].Code != "operator" {
		t.Fatalf("filter by status = %+v, want disabled operator only", byStatus)
	}
}

func TestRoleServiceCodeUnique(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	editor, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "editor"})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	other, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "运营", Code: "operator"})
	if err != nil {
		t.Fatalf("CreateRole other failed: %v", err)
	}

	// 创建重复 code → 1004。
	_, err = srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑2", Code: "editor"})
	wantErrCode(t, err, errno.CodeRoleCodeTaken)

	// 编辑改成他人 code → 1004。
	_, err = srv.UpdateRole(ctx, other.ID, &SaveRoleInput{Name: "运营", Code: "editor"})
	wantErrCode(t, err, errno.CodeRoleCodeTaken)

	// 编辑保持自身 code → 成功。
	if _, err := srv.UpdateRole(ctx, other.ID, &SaveRoleInput{Name: "运营2", Code: "operator"}); err != nil {
		t.Fatalf("UpdateRole keep own code failed: %v", err)
	}

	// 软删后重建同 code：服务层查重会过滤软删行，由唯一索引兜底 → 1004（而非 500）。
	if err := srv.DeleteRole(ctx, other.ID); err != nil {
		t.Fatalf("DeleteRole other failed: %v", err)
	}
	_, err = srv.CreateRole(ctx, &SaveRoleInput{Name: "运营重生", Code: "operator"})
	wantErrCode(t, err, errno.CodeRoleCodeTaken)

	// 编辑改成已软删角色的 code → 同样 1004。
	_, err = srv.UpdateRole(ctx, editor.ID, &SaveRoleInput{Name: "编辑", Code: "operator"})
	wantErrCode(t, err, errno.CodeRoleCodeTaken)
}

func TestRoleServiceSuperProtection(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	super, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "超级管理员", Code: model.RoleSuperCode})
	if err != nil {
		t.Fatalf("CreateRole super failed: %v", err)
	}

	// 删除 super → 1014。
	wantErrCode(t, srv.DeleteRole(ctx, super.ID), errno.CodeSuperRoleProtected)

	// 停用 super → 1014。
	disabled := model.StatusDisabled
	_, err = srv.UpdateRole(ctx, super.ID, &SaveRoleInput{Name: "超级管理员", Code: model.RoleSuperCode, Status: &disabled})
	wantErrCode(t, err, errno.CodeSuperRoleProtected)

	// 修改 super 的 code → 1014。
	_, err = srv.UpdateRole(ctx, super.ID, &SaveRoleInput{Name: "超级管理员", Code: "editor"})
	wantErrCode(t, err, errno.CodeSuperRoleProtected)

	// 改其他字段 → 成功且状态保持启用。
	updated, err := srv.UpdateRole(ctx, super.ID, &SaveRoleInput{Name: "超级管理员2", Code: model.RoleSuperCode, Sort: 9})
	if err != nil {
		t.Fatalf("UpdateRole super other fields failed: %v", err)
	}
	if updated.Name != "超级管理员2" || updated.Status != model.StatusEnabled {
		t.Errorf("updated super = %+v, want name changed and status enabled", updated)
	}
}

func TestRoleServiceAssignMenus(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	menuA := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "A", Status: model.StatusEnabled, Permission: "system:user"})
	menuB := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "B", Status: model.StatusEnabled, Permission: "system:dept"})

	role, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "editor"})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	// 1. 首次分配 + 去重（重复 id 只写一条）。
	if err := srv.AssignMenus(ctx, role.ID, []uint64{menuA.ID, menuA.ID, menuB.ID}); err != nil {
		t.Fatalf("AssignMenus failed: %v", err)
	}
	ids, err := srv.GetMenuIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("GetMenuIDs failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("menu ids after assign = %v, want 2 unique ids", ids)
	}

	// 2. 覆盖语义：二次分配替换旧集。
	if err := srv.AssignMenus(ctx, role.ID, []uint64{menuB.ID}); err != nil {
		t.Fatalf("AssignMenus overwrite failed: %v", err)
	}
	ids, err = srv.GetMenuIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("GetMenuIDs after overwrite failed: %v", err)
	}
	if len(ids) != 1 || ids[0] != menuB.ID {
		t.Errorf("menu ids after overwrite = %v, want [%d]", ids, menuB.ID)
	}

	// 3. 非法菜单 id → 1009，且不产生部分写入。
	if err := srv.AssignMenus(ctx, role.ID, []uint64{menuA.ID, 99999}); err != nil {
		wantErrCode(t, err, errno.CodeInvalidParam)
	} else {
		t.Fatal("AssignMenus with invalid menu id should fail")
	}
	ids, _ = srv.GetMenuIDs(ctx, role.ID)
	if len(ids) != 1 || ids[0] != menuB.ID {
		t.Errorf("menu ids after failed assign = %v, want unchanged [%d]", ids, menuB.ID)
	}

	// 4. 角色不存在 → 1011。
	wantErrCode(t, srv.AssignMenus(ctx, 99999, []uint64{menuA.ID}), errno.CodeNotFound)

	// 5. 清空分配。
	if err := srv.AssignMenus(ctx, role.ID, []uint64{}); err != nil {
		t.Fatalf("AssignMenus clear failed: %v", err)
	}
	ids, err = srv.GetMenuIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("GetMenuIDs after clear failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("menu ids after clear = %v, want empty", ids)
	}

	// 6. super 角色不可分配菜单 → 1014（super 绕过 role_menus，避免覆盖 seed 全量绑定）。
	super, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "超级管理员", Code: model.RoleSuperCode})
	if err != nil {
		t.Fatalf("CreateRole super failed: %v", err)
	}
	wantErrCode(t, srv.AssignMenus(ctx, super.ID, []uint64{menuA.ID}), errno.CodeSuperRoleProtected)
}

func TestRoleServiceGetMenuIDs(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	menuA := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "A", Status: model.StatusEnabled})

	role, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "editor"})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if err := srv.AssignMenus(ctx, role.ID, []uint64{menuA.ID}); err != nil {
		t.Fatalf("AssignMenus failed: %v", err)
	}

	// 停用角色后仍可查询其已分配菜单（编辑弹窗需展示既有勾选）。
	disabled := model.StatusDisabled
	if _, err := srv.UpdateRole(ctx, role.ID, &SaveRoleInput{Name: "编辑", Code: "editor", Status: &disabled}); err != nil {
		t.Fatalf("disable role failed: %v", err)
	}
	ids, err := srv.GetMenuIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("GetMenuIDs disabled role failed: %v", err)
	}
	if len(ids) != 1 || ids[0] != menuA.ID {
		t.Errorf("menu ids of disabled role = %v, want [%d]", ids, menuA.ID)
	}

	// 角色不存在 → 1011。
	_, err = srv.GetMenuIDs(ctx, 99999)
	wantErrCode(t, err, errno.CodeNotFound)

	// 菜单软删后不再出现在已分配列表。
	if err := db.Delete(&model.Menu{}, menuA.ID).Error; err != nil {
		t.Fatalf("delete menuA: %v", err)
	}
	ids, err = srv.GetMenuIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("GetMenuIDs after menu soft delete failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("menu ids after menu soft delete = %v, want empty", ids)
	}
}

// TestRoleServiceInputNormalization 锁定 name/code 去空白与纯空白拒绝。
func TestRoleServiceInputNormalization(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	// 纯空白 name/code → 1009。
	_, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "   ", Code: "editor"})
	wantErrCode(t, err, errno.CodeInvalidParam)
	_, err = srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑", Code: "  "})
	wantErrCode(t, err, errno.CodeInvalidParam)

	// 首尾空白被去除后入库；去重基于去除后的值。
	role, err := srv.CreateRole(ctx, &SaveRoleInput{Name: " 编辑 ", Code: " editor "})
	if err != nil {
		t.Fatalf("CreateRole with spaces failed: %v", err)
	}
	if role.Name != "编辑" || role.Code != "editor" {
		t.Errorf("role name/code = %q/%q, want trimmed", role.Name, role.Code)
	}
	_, err = srv.CreateRole(ctx, &SaveRoleInput{Name: "编辑2", Code: "editor"})
	wantErrCode(t, err, errno.CodeRoleCodeTaken)
}

// TestRoleServicePermissionUnion 锁定权限码并集计算四契约：
// 含 button 码 / 多角色重叠去重 / 禁用角色与禁用菜单排除 / 清空后为空。
func TestRoleServicePermissionUnion(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	menuRepo := repository.NewMenuRepository(db)
	ctx := context.Background()

	userMenu := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "User", Status: model.StatusEnabled, Permission: "system:user"})
	userAddBtn := createTestMenu(t, db, model.Menu{Type: model.MenuTypeButton, Name: "新增用户", Status: model.StatusEnabled, Permission: "system:user:add"})
	deptMenu := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "Dept", Status: model.StatusEnabled, Permission: "system:dept"})
	hiddenMenu := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "Hidden", Status: model.StatusEnabled, Permission: "system:hidden"})
	// gorm 的 default:1 会把零值 status 写回默认启用，需用 Update 显式禁用。
	if err := db.Model(&model.Menu{}).Where("id = ?", hiddenMenu.ID).Update("status", model.StatusDisabled).Error; err != nil {
		t.Fatalf("disable hidden menu: %v", err)
	}

	r1, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "角色一", Code: "role1"})
	if err != nil {
		t.Fatalf("CreateRole r1 failed: %v", err)
	}
	r2, err := srv.CreateRole(ctx, &SaveRoleInput{Name: "角色二", Code: "role2"})
	if err != nil {
		t.Fatalf("CreateRole r2 failed: %v", err)
	}

	// 契约 1：分配含 button 节点的菜单集后，并集含按钮码。
	if err := srv.AssignMenus(ctx, r1.ID, []uint64{userMenu.ID, userAddBtn.ID}); err != nil {
		t.Fatalf("AssignMenus r1 failed: %v", err)
	}
	perms, err := menuRepo.GetPermissionsByRoleIDs(ctx, []uint64{r1.ID})
	if err != nil {
		t.Fatalf("GetPermissionsByRoleIDs failed: %v", err)
	}
	if !permsEqual(perms, "system:user", "system:user:add") {
		t.Errorf("perms = %v, want [system:user system:user:add]", perms)
	}

	// 契约 2：多角色重叠菜单去重。
	if err := srv.AssignMenus(ctx, r2.ID, []uint64{userMenu.ID, deptMenu.ID, hiddenMenu.ID}); err != nil {
		t.Fatalf("AssignMenus r2 failed: %v", err)
	}
	perms, err = menuRepo.GetPermissionsByRoleIDs(ctx, []uint64{r1.ID, r2.ID})
	if err != nil {
		t.Fatalf("GetPermissionsByRoleIDs multi-role failed: %v", err)
	}
	// hiddenMenu 为禁用菜单，契约 3 一并验证其被排除。
	if !permsEqual(perms, "system:user", "system:user:add", "system:dept") {
		t.Errorf("perms = %v, want dedup union [system:user system:user:add system:dept]", perms)
	}

	// 契约 3：禁用角色被排除（停用 r2 后并集只剩 r1 的码）。
	disabled := model.StatusDisabled
	if _, err := srv.UpdateRole(ctx, r2.ID, &SaveRoleInput{Name: "角色二", Code: "role2", Status: &disabled}); err != nil {
		t.Fatalf("disable r2 failed: %v", err)
	}
	perms, err = menuRepo.GetPermissionsByRoleIDs(ctx, []uint64{r1.ID, r2.ID})
	if err != nil {
		t.Fatalf("GetPermissionsByRoleIDs disabled-role failed: %v", err)
	}
	if !permsEqual(perms, "system:user", "system:user:add") {
		t.Errorf("perms = %v, want [system:user system:user:add] (r2 excluded)", perms)
	}

	// 契约 4：清空分配后并集为空。
	if err := srv.AssignMenus(ctx, r1.ID, []uint64{}); err != nil {
		t.Fatalf("AssignMenus clear failed: %v", err)
	}
	perms, err = menuRepo.GetPermissionsByRoleIDs(ctx, []uint64{r1.ID})
	if err != nil {
		t.Fatalf("GetPermissionsByRoleIDs after clear failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("perms after clear = %v, want empty", perms)
	}
}

func TestRoleServiceBatchDelete(t *testing.T) {
	db := setupTestDB(t)
	srv := newRoleTestService(db)
	ctx := context.Background()

	protected := model.Role{BaseModel: model.BaseModel{ID: model.BuiltinAdminRoleID}, Name: "内置管理员", Code: model.RoleAdminCode, Status: model.StatusEnabled}
	regular := model.Role{Name: "批量角色", Code: "batch-role", Status: model.StatusEnabled}
	if err := db.Create(&protected).Error; err != nil {
		t.Fatalf("create protected role: %v", err)
	}
	if err := db.Create(&regular).Error; err != nil {
		t.Fatalf("create regular role: %v", err)
	}
	user := model.User{Username: "role-user", Status: model.StatusEnabled}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	menu := createTestMenu(t, db, model.Menu{Type: model.MenuTypeMenu, Name: "批量菜单", Status: model.StatusEnabled})
	if err := db.Create(&model.RoleMenu{RoleID: regular.ID, MenuID: menu.ID}).Error; err != nil {
		t.Fatalf("create role-menu: %v", err)
	}
	if err := db.Create(&model.UserRole{UserID: user.ID, RoleID: regular.ID}).Error; err != nil {
		t.Fatalf("create user-role: %v", err)
	}

	wantErrCode(t, srv.DeleteRole(ctx, protected.ID), errno.CodeSuperRoleProtected)
	wantErrCode(t, srv.BatchDelete(ctx, []uint64{0}), errno.CodeInvalidParam)
	wantErrCode(t, srv.BatchDelete(ctx, []uint64{regular.ID, protected.ID}), errno.CodeSuperRoleProtected)

	var untouched model.Role
	if err := db.First(&untouched, regular.ID).Error; err != nil {
		t.Fatalf("protected batch must not delete regular role: %v", err)
	}

	if err := srv.BatchDelete(ctx, []uint64{regular.ID, regular.ID}); err != nil {
		t.Fatalf("batch delete regular role: %v", err)
	}
	var deleted model.Role
	if err := db.Unscoped().First(&deleted, regular.ID).Error; err != nil {
		t.Fatalf("find soft-deleted role: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Fatal("batch delete did not soft-delete role")
	}
	var roleMenuCount, userRoleCount int64
	if err := db.Model(&model.RoleMenu{}).Where("role_id = ?", regular.ID).Count(&roleMenuCount).Error; err != nil {
		t.Fatalf("count role-menu: %v", err)
	}
	if err := db.Model(&model.UserRole{}).Where("role_id = ?", regular.ID).Count(&userRoleCount).Error; err != nil {
		t.Fatalf("count user-role: %v", err)
	}
	if roleMenuCount != 0 || userRoleCount != 0 {
		t.Fatalf("batch delete left relations: roleMenu=%d userRole=%d", roleMenuCount, userRoleCount)
	}
}
