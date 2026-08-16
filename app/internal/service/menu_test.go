package service

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite memory db: %v", err)
	}
	if err := db.AutoMigrate(&model.Menu{}, &model.Role{}, &model.RoleMenu{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func TestMenuServiceCRUDAndTree(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewMenuRepository(db)
	srv := NewMenuService(repo)
	ctx := context.Background()

	statusEnabled := int8(1)
	// 1. Create Catalog
	catInput := &SaveMenuInput{
		ParentID: 0,
		Type:     model.MenuTypeCatalog,
		Name:     "System",
		Title:    "routes.system.system",
		Path:     "/system",
		Sort:     1,
		Status:   &statusEnabled,
	}
	catMenu, err := srv.CreateMenu(ctx, catInput)
	if err != nil {
		t.Fatalf("CreateMenu catalog failed: %v", err)
	}
	if catMenu.Component != model.MenuComponentBasicLayout {
		t.Errorf("catalog component = %q, want %q", catMenu.Component, model.MenuComponentBasicLayout)
	}

	// 2. Create Menu child
	menuInput := &SaveMenuInput{
		ParentID:   catMenu.ID,
		Type:       model.MenuTypeMenu,
		Name:       "User",
		Title:      "routes.system.user",
		Path:       "/system/user",
		Component:  "/system/user/index",
		Sort:       1,
		Status:     &statusEnabled,
		Permission: "system:user",
	}
	userMenu, err := srv.CreateMenu(ctx, menuInput)
	if err != nil {
		t.Fatalf("CreateMenu menu failed: %v", err)
	}

	// 3. Create Button child
	btnInput := &SaveMenuInput{
		ParentID:   userMenu.ID,
		Type:       model.MenuTypeButton,
		Name:       "新增用户",
		Sort:       1,
		Status:     &statusEnabled,
		Permission: "system:user:add",
	}
	_, err = srv.CreateMenu(ctx, btnInput)
	if err != nil {
		t.Fatalf("CreateMenu button failed: %v", err)
	}

	// 4. Test GetMenuTree (All nodes including button)
	allTree, err := srv.GetMenuTree(ctx)
	if err != nil {
		t.Fatalf("GetMenuTree failed: %v", err)
	}
	if len(allTree) != 1 {
		t.Fatalf("allTree len = %d, want 1", len(allTree))
	}
	if len(allTree[0].Children) != 1 {
		t.Fatalf("Catalog children len = %d, want 1", len(allTree[0].Children))
	}
	if len(allTree[0].Children[0].Children) != 1 {
		t.Fatalf("User children len = %d, want 1", len(allTree[0].Children[0].Children))
	}

	// 5. Test GetUserMenuTree for super (should exclude button)
	vbenRoutes, err := srv.GetUserMenuTree(ctx, []string{model.RoleSuperCode}, nil)
	if err != nil {
		t.Fatalf("GetUserMenuTree failed: %v", err)
	}
	if len(vbenRoutes) != 1 {
		t.Fatalf("vbenRoutes len = %d, want 1", len(vbenRoutes))
	}
	if vbenRoutes[0].Name != "System" {
		t.Errorf("vbenRoutes[0].Name = %s, want System", vbenRoutes[0].Name)
	}
	if len(vbenRoutes[0].Children) != 1 {
		t.Fatalf("vbenRoutes catalog children len = %d, want 1", len(vbenRoutes[0].Children))
	}
	// Verify button is excluded
	if len(vbenRoutes[0].Children[0].Children) != 0 {
		t.Errorf("User menu should not contain button children, got len = %d", len(vbenRoutes[0].Children[0].Children))
	}

	// 6. Test DeleteMenu with children -> should return errno 1006
	err = srv.DeleteMenu(ctx, catMenu.ID)
	if err == nil {
		t.Fatal("DeleteMenu for parent with children should fail, but got nil")
	}
	customErr, ok := err.(*errno.Error)
	if !ok || customErr.Code != errno.CodeMenuHasChildren {
		t.Errorf("DeleteMenu error = %v, want errno CodeMenuHasChildren(1006)", err)
	}
	if got := err.Error(); got != errno.Message(errno.DefaultLang, errno.CodeMenuHasChildren) {
		t.Errorf("DeleteMenu error text = %q, want i18n message %q", got, errno.Message(errno.DefaultLang, errno.CodeMenuHasChildren))
	}

	// 7. Delete button, then user menu, then catalog -> should succeed
	btnID := allTree[0].Children[0].Children[0].ID
	if err := srv.DeleteMenu(ctx, btnID); err != nil {
		t.Fatalf("DeleteMenu button failed: %v", err)
	}
	if err := srv.DeleteMenu(ctx, userMenu.ID); err != nil {
		t.Fatalf("DeleteMenu user menu failed: %v", err)
	}
	if err := srv.DeleteMenu(ctx, catMenu.ID); err != nil {
		t.Fatalf("DeleteMenu catalog failed: %v", err)
	}
	if err := srv.DeleteMenu(ctx, catMenu.ID); err == nil {
		t.Fatal("DeleteMenu for missing menu should fail")
	} else if customErr, ok := err.(*errno.Error); !ok || customErr.Code != errno.CodeNotFound {
		t.Errorf("DeleteMenu missing error = %v, want CodeNotFound", err)
	}
}

func TestMenuServiceFiltersNonSuperMenus(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewMenuRepository(db)
	srv := NewMenuService(repo)
	ctx := context.Background()
	statusEnabled := model.StatusEnabled

	allowed, err := srv.CreateMenu(ctx, &SaveMenuInput{Type: model.MenuTypeCatalog, Name: "Allowed", Status: &statusEnabled})
	if err != nil {
		t.Fatalf("create allowed menu: %v", err)
	}
	if _, err := srv.CreateMenu(ctx, &SaveMenuInput{Type: model.MenuTypeCatalog, Name: "Hidden", Status: &statusEnabled}); err != nil {
		t.Fatalf("create hidden menu: %v", err)
	}
	role := model.Role{Name: "Editor", Code: "editor", Status: model.StatusEnabled}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&model.RoleMenu{RoleID: role.ID, MenuID: allowed.ID}).Error; err != nil {
		t.Fatalf("create role menu: %v", err)
	}

	routes, err := srv.GetUserMenuTree(ctx, []string{role.Code}, []uint64{role.ID})
	if err != nil {
		t.Fatalf("get filtered menus: %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "Allowed" {
		t.Fatalf("routes = %+v, want only Allowed", routes)
	}
}

func TestMenuServiceRejectsDescendantParent(t *testing.T) {
	db := setupTestDB(t)
	srv := NewMenuService(repository.NewMenuRepository(db))
	ctx := context.Background()
	statusEnabled := int8(1)

	root, err := srv.CreateMenu(ctx, &SaveMenuInput{
		Type:   model.MenuTypeCatalog,
		Name:   "Root",
		Status: &statusEnabled,
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := srv.CreateMenu(ctx, &SaveMenuInput{
		ParentID: root.ID,
		Type:     model.MenuTypeMenu,
		Name:     "Child",
		Status:   &statusEnabled,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	_, err = srv.UpdateMenu(ctx, root.ID, &SaveMenuInput{
		ParentID: child.ID,
		Type:     model.MenuTypeCatalog,
		Name:     "Root",
	})
	customErr, ok := err.(*errno.Error)
	if !ok || customErr.Code != errno.CodeParentIsDescendant {
		t.Fatalf("UpdateMenu cycle error = %v, want CodeParentIsDescendant", err)
	}
}
