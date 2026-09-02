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
	want := []string{
		"users", "roles", "menus", "departments", "user_roles", "role_menus",
		"refresh_tokens", "operation_logs", "system_configs", "cameras", "persons",
		"algorithms", "algorithm_versions", "analysis_tasks", "algorithm_instances",
		"desired_state_revision", "face_gallery_revision", "alarm_records", "plate_observations", "face_observations",
	}
	for _, name := range want {
		if !gdb.Migrator().HasTable(name) {
			t.Errorf("table %s missing", name)
		}
	}

	for _, column := range []string{
		"algorithm_id", "algorithm_version", "time_synced", "image_id", "image_rel_path",
		"plate_image_id", "plate_image_rel_path",
	} {
		if !gdb.Migrator().HasColumn(&PlateObservation{}, column) {
			t.Errorf("plate_observations column %s missing", column)
		}
	}

	if !gdb.Migrator().HasColumn(&Algorithm{}, "is_builtin") {
		t.Errorf("algorithms column is_builtin missing")
	}
	if !gdb.Migrator().HasColumn(&AlgorithmVersion{}, "is_builtin") {
		t.Errorf("algorithm_versions column is_builtin missing")
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
	if menuCount != 70 {
		t.Errorf("menu rows = %d, want 70", menuCount)
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

	// 权限码契约：全量集合精确匹配
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
		"ops:time", "ops:time:read", "ops:time:edit",
		"ops:storage", "ops:storage:read", "ops:storage:edit",
		"ops:network", "ops:network:edit", "ops:network:confirm", "ops:network:cancel", "ops:network:reset", "ops:network:mode",
		"resource:camera", "resource:camera:add", "resource:camera:edit", "resource:camera:delete", "resource:camera:probe",
		"resource:person", "resource:person:add", "resource:person:edit", "resource:person:delete", "resource:person:face:manage",
		"resource:task", "resource:task:add", "resource:task:edit", "resource:task:delete",
		"ai:algorithm", "ai:algorithm:upload", "ai:algorithm:activate", "ai:algorithm:uninstall",
		"record:alarm", "record:alarm:query", "record:alarm:export",
		"record:plate", "record:plate:query", "record:plate:export",
		"record:face", "record:face:query", "record:face:export",
		"record:capture", "record:capture:query", "record:capture:export",
		"live:preview", "live:preview:stream",
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("permission codes len = %d, want %d. got: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("permission code mismatch: got %v, want %v", got, want)
			break
		}
	}

	// super 角色绑定全部 70 条菜单
	var rmCount int64
	gdb.Model(&RoleMenu{}).Where("role_id = ?", super.ID).Count(&rmCount)
	if rmCount != 70 {
		t.Errorf("role_menus for super = %d, want 70", rmCount)
	}

	// 初始系统配置与 desired_state_revision
	var sysCfg SystemConfig
	if err := gdb.Where("key = ?", ConfigKeyTime).First(&sysCfg).Error; err != nil {
		t.Fatalf("system config time missing: %v", err)
	}
	var rev DesiredStateRevision
	if err := gdb.Where("id = ?", 1).First(&rev).Error; err != nil {
		t.Fatalf("desired_state_revision missing: %v", err)
	}

	// 验证内置算法种子
	var lpr Algorithm
	if err := gdb.Where("algorithm_id = ?", "license_plate_recognition").First(&lpr).Error; err != nil {
		t.Fatalf("license_plate_recognition algorithm missing: %v", err)
	}
	if !lpr.IsBuiltin {
		t.Errorf("license_plate_recognition.is_builtin = false, want true")
	}
	var lprVer AlgorithmVersion
	if err := gdb.Where("algorithm_id = ? AND version = ?", "license_plate_recognition", "1.0.0").First(&lprVer).Error; err != nil {
		t.Fatalf("license_plate_recognition version 1.0.0 missing: %v", err)
	}
	if !lprVer.IsBuiltin {
		t.Errorf("license_plate_recognition version 1.0.0 is_builtin = false, want true")
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

func TestSeedIncrementalSystemSingletonsWhenAdminExists(t *testing.T) {
	gdb := newSmokeDB(t)
	// 首次 seed 初始化全部数据
	if _, err := Seed(gdb); err != nil {
		t.Fatalf("initial seed: %v", err)
	}

	// 模拟旧库：admin 和已有的人脸样本都存在，但新增的单例行缺失。
	legacyFace := PersonFace{
		PersonID:         "legacy-person",
		FaceID:           "legacy-face",
		AlgorithmID:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		Embedding:        []byte{1},
		RawImageKey:      "legacy/raw.jpg",
		RawImageSHA256:   "legacy-sha256",
		RawImageSize:     1,
		RawImageMime:     "image/jpeg",
		AlignedFaceKey:   "legacy/aligned.jpg",
		AlignedFaceSize:  1,
		AlignedFaceMime:  "image/jpeg",
	}
	if err := gdb.Create(&legacyFace).Error; err != nil {
		t.Fatalf("create legacy face: %v", err)
	}
	if err := gdb.Exec("DELETE FROM face_gallery_revision").Error; err != nil {
		t.Fatalf("delete face_gallery_revision: %v", err)
	}
	if err := gdb.Exec("DELETE FROM desired_state_revision").Error; err != nil {
		t.Fatalf("delete desired_state_revision: %v", err)
	}

	// 再次执行 Seed（由于 admin 存在，返回 false，但必须成功补充缺失的系统单例）
	seeded, err := Seed(gdb)
	if err != nil {
		t.Fatalf("seed after singleton deleted: %v", err)
	}
	if seeded {
		t.Fatal("Seed should report seeded=false when admin exists")
	}

	var fgRev FaceGalleryRevision
	if err := gdb.Where("id = ?", 1).First(&fgRev).Error; err != nil {
		t.Fatalf("face_gallery_revision singleton missing after incremental seed: %v", err)
	}
	if fgRev.Revision != 1 {
		t.Errorf("face_gallery_revision.revision = %d, want 1 for existing faces", fgRev.Revision)
	}

	var dsRev DesiredStateRevision
	if err := gdb.Where("id = ?", 1).First(&dsRev).Error; err != nil {
		t.Fatalf("desired_state_revision singleton missing after incremental seed: %v", err)
	}
	if dsRev.Revision != 0 {
		t.Errorf("desired_state_revision.revision = %d, want 0", dsRev.Revision)
	}
}
