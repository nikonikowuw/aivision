package model

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newSmokeDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每个测试独立的内存库（t.Name() 保证唯一）
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(gdb); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return gdb
}

func TestAutoMigrateCreatesAllTables(t *testing.T) {
	gdb := newSmokeDB(t)
	want := []string{"users", "roles", "menus", "departments", "user_roles", "role_menus", "refresh_tokens", "operation_logs"}
	for _, name := range want {
		if !gdb.Migrator().HasTable(name) {
			t.Errorf("table %s missing", name)
		}
	}
}

func TestSeedIdempotentAndStructure(t *testing.T) {
	gdb := newSmokeDB(t)

	seeded, err := Seed(gdb)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !seeded {
		t.Fatal("first Seed should report seeded=true")
	}

	// 二次执行：admin 已存在 → 整体跳过
	again, err := Seed(gdb)
	if err != nil {
		t.Fatalf("seed again: %v", err)
	}
	if again {
		t.Fatal("second Seed should report seeded=false")
	}

	// 数量不重复
	var menuCount, userCount, roleCount int64
	gdb.Model(&Menu{}).Count(&menuCount)
	gdb.Model(&User{}).Count(&userCount)
	gdb.Model(&Role{}).Count(&roleCount)
	if menuCount != 24 {
		t.Errorf("menu rows = %d, want 24", menuCount)
	}
	if userCount != 1 || roleCount != 1 {
		t.Errorf("users=%d roles=%d, want 1/1", userCount, roleCount)
	}

	// admin + bcrypt
	var admin User
	if err := gdb.Where("username = ?", AdminUsername).First(&admin).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(seedAdminPassword)); err != nil {
		t.Errorf("admin password is not bcrypt of admin123: %v", err)
	}
	if admin.Status != 1 {
		t.Errorf("admin status = %d, want 1", admin.Status)
	}

	// super 角色 + 绑定
	var super Role
	if err := gdb.Where("code = ?", RoleSuperCode).First(&super).Error; err != nil {
		t.Fatalf("find super role: %v", err)
	}
	var ur UserRole
	if err := gdb.Where("user_id = ? AND role_id = ?", admin.ID, super.ID).First(&ur).Error; err != nil {
		t.Errorf("admin-super binding missing: %v", err)
	}

	// demo 部门且 admin 挂上
	var dept Department
	if err := gdb.Where("name = ?", seedDeptName).First(&dept).Error; err != nil {
		t.Fatalf("find demo dept: %v", err)
	}
	if admin.DeptID != dept.ID {
		t.Errorf("admin.dept_id = %d, want %d", admin.DeptID, dept.ID)
	}

	// 权限码契约：全量集合精确匹配设计 §5
	var menus []Menu
	gdb.Find(&menus)
	got := make([]string, 0, len(menus))
	for _, m := range menus {
		if m.Permission != "" {
			got = append(got, m.Permission)
		}
	}
	want := []string{
		"system:user", "system:user:add", "system:user:edit", "system:user:delete",
		"system:user:reset-password", "system:user:assign-role", "system:user:status",
		"system:role", "system:role:add", "system:role:edit", "system:role:delete", "system:role:assign-menu",
		"system:menu", "system:menu:add", "system:menu:edit", "system:menu:delete",
		"system:dept", "system:dept:add", "system:dept:edit", "system:dept:delete",
		"system:log",
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("permission codes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("permission code mismatch: got %v, want %v", got, want)
			break
		}
	}

	// 按钮级数量：user 6 / role 4 / menu 3 / dept 3 / log 0
	var buttonCounts []struct {
		Parent string
		Count  int64
	}
	gdb.Model(&Menu{}).
		Select("menus.name as parent, count(child.id) as count").
		Joins("left join menus child on child.parent_id = menus.id and child.type = ?", MenuTypeButton).
		Where("menus.type = ?", MenuTypeMenu).
		Group("menus.id").
		Scan(&buttonCounts)
	expect := map[string]int64{"User": 6, "Role": 4, "Menu": 3, "Dept": 3, "Log": 0, "dashboard": 0}
	for _, bc := range buttonCounts {
		if expect[bc.Parent] != bc.Count {
			t.Errorf("menu %s buttons = %d, want %d", bc.Parent, bc.Count, expect[bc.Parent])
		}
	}
	for _, name := range []string{"User", "Role", "Menu", "Dept", "Log", "dashboard"} {
		found := false
		for _, bc := range buttonCounts {
			if bc.Parent == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("menu %s missing from button count result", name)
		}
	}

	// title key 契约：仅 catalog/menu 有 title（决策 17，design.md §5）
	titleWant := map[string]string{
		"System": "routes.system.system", "User": "routes.system.user",
		"Role": "routes.system.role", "Menu": "routes.system.menu",
		"Dept": "routes.system.dept", "Log": "routes.system.log",
		"Dashboard": "routes.dashboard.title", "dashboard": "routes.dashboard.analytics",
	}
	var titled []Menu
	gdb.Where("title <> ''").Find(&titled)
	if len(titled) != len(titleWant) {
		t.Errorf("menus with title = %d, want %d", len(titled), len(titleWant))
	}
	for _, m := range titled {
		if want, ok := titleWant[m.Name]; !ok || m.Title != want {
			t.Errorf("menu %s title = %q, want %q", m.Name, m.Title, want)
		}
	}
	// button 无 title
	var untitledButton int64
	gdb.Model(&Menu{}).Where("type = ? AND title <> ''", MenuTypeButton).Count(&untitledButton)
	if untitledButton != 0 {
		t.Errorf("buttons with title = %d, want 0", untitledButton)
	}

	// super 角色绑定全部 24 条菜单
	var rmCount int64
	gdb.Model(&RoleMenu{}).Where("role_id = ?", super.ID).Count(&rmCount)
	if rmCount != 24 {
		t.Errorf("role_menus for super = %d, want 24", rmCount)
	}

	// 树结构：2 个根（System 在前，Dashboard 在后），System 下 5 个子节点
	roots := BuildMenuTree(menus)
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	if roots[0].Name != "System" || roots[1].Name != "Dashboard" {
		t.Errorf("root order = [%s, %s], want [System, Dashboard]", roots[0].Name, roots[1].Name)
	}
	if len(roots[0].Children) != 5 {
		t.Errorf("System children = %d, want 5", len(roots[0].Children))
	}
}

func TestSeedCleansExpiredRefreshTokens(t *testing.T) {
	gdb := newSmokeDB(t)
	if _, err := Seed(gdb); err != nil {
		t.Fatalf("seed: %v", err)
	}
	expired := RefreshToken{
		UserID: 1, Token: "expired-token", ExpiresAt: time.Now().Add(-time.Hour),
	}
	valid := RefreshToken{
		UserID: 1, Token: "valid-token", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := gdb.Create(&expired).Error; err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	if err := gdb.Create(&valid).Error; err != nil {
		t.Fatalf("create valid token: %v", err)
	}

	if _, err := Seed(gdb); err != nil {
		t.Fatalf("seed again: %v", err)
	}
	var remaining []RefreshToken
	gdb.Find(&remaining)
	if len(remaining) != 1 || remaining[0].Token != "valid-token" {
		t.Errorf("after cleanup remaining tokens = %+v, want only valid-token", remaining)
	}
}
