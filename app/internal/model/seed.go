package model

import (
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// seedMenuItem 种子菜单声明（设计 §5 权限码契约表，全项目唯一权限码源）。
type seedMenuItem struct {
	Type       string
	Name       string // 路由标识符，ASCII
	Title      string // i18n key，如 routes.system.user（决策 17）
	Path       string
	Component  string
	Icon       string
	Permission string
	Affix      bool
	KeepAlive  bool
	Children   []seedMenuItem
}

// seedMenuTree 设计 §5 菜单树契约，严禁增删权限码。
var seedMenuTree = []seedMenuItem{
	{
		Type: MenuTypeCatalog, Name: "System", Title: "routes.system.system", Path: "/system", Component: "BasicLayout",
		Icon: "ant-design:setting-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "User", Title: "routes.system.user", Path: "/system/user", Component: "/system/user/index",
				Icon: "ant-design:user-outlined", Permission: "system:user",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "新增用户", Permission: "system:user:add"},
					{Type: MenuTypeButton, Name: "编辑用户", Permission: "system:user:edit"},
					{Type: MenuTypeButton, Name: "删除用户", Permission: "system:user:delete"},
					{Type: MenuTypeButton, Name: "重置密码", Permission: "system:user:reset-password"},
					{Type: MenuTypeButton, Name: "分配角色", Permission: "system:user:assign-role"},
					{Type: MenuTypeButton, Name: "启停用", Permission: "system:user:status"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Role", Title: "routes.system.role", Path: "/system/role", Component: "/system/role/index",
				Icon: "ant-design:team-outlined", Permission: "system:role",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "新增角色", Permission: "system:role:add"},
					{Type: MenuTypeButton, Name: "编辑角色", Permission: "system:role:edit"},
					{Type: MenuTypeButton, Name: "删除角色", Permission: "system:role:delete"},
					{Type: MenuTypeButton, Name: "分配菜单", Permission: "system:role:assign-menu"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Menu", Title: "routes.system.menu", Path: "/system/menu", Component: "/system/menu/index",
				Icon: "ant-design:menu-outlined", Permission: "system:menu",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "新增菜单", Permission: "system:menu:add"},
					{Type: MenuTypeButton, Name: "编辑菜单", Permission: "system:menu:edit"},
					{Type: MenuTypeButton, Name: "删除菜单", Permission: "system:menu:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Dept", Title: "routes.system.dept", Path: "/system/dept", Component: "/system/dept/index",
				Icon: "ant-design:apartment-outlined", Permission: "system:dept",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "新增部门", Permission: "system:dept:add"},
					{Type: MenuTypeButton, Name: "编辑部门", Permission: "system:dept:edit"},
					{Type: MenuTypeButton, Name: "删除部门", Permission: "system:dept:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Log", Title: "routes.system.log", Path: "/system/log", Component: "/system/log/index",
				Icon: "ant-design:file-text-outlined", Permission: "system:log",
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Dashboard", Title: "routes.dashboard.title", Path: "/dashboard", Component: "BasicLayout",
		Icon: "ant-design:home-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "dashboard", Title: "routes.dashboard.analytics", Path: "/dashboard",
				Component: "/dashboard/analytics/index", // vben 自带视图
				Icon:      "ant-design:dashboard-outlined", Affix: true, KeepAlive: true,
			},
		},
	},
}

const (
	seedAdminPassword = "admin123"
	seedDeptName      = "演示部门"
)

// Seed 幂等播种种子数据。
// 每次启动先清理过期 refresh token；users 表已存在 admin 则整体跳过（不覆盖用户对菜单的后续修改）。
// 返回是否执行了播种。
func Seed(db *gorm.DB) (bool, error) {
	// 惰性清理过期 refresh token（父 design.md §2：不做定时任务）。
	if err := db.Where("expires_at < ?", time.Now()).Delete(&RefreshToken{}).Error; err != nil {
		return false, fmt.Errorf("clean expired refresh tokens: %w", err)
	}

	var count int64
	if err := db.Model(&User{}).Where("username = ?", AdminUsername).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check admin exists: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	if err := db.Transaction(seedAll); err != nil {
		return false, err
	}
	return true, nil
}

func seedAll(tx *gorm.DB) error {
	// demo 部门
	dept := Department{Name: seedDeptName, ParentID: 0, Sort: 0, Status: StatusEnabled}
	if err := tx.Where("name = ?", dept.Name).FirstOrCreate(&dept).Error; err != nil {
		return fmt.Errorf("seed department: %w", err)
	}

	// super 角色
	role := Role{Name: "超级管理员", Code: RoleSuperCode, Status: StatusEnabled, Sort: 0}
	if err := tx.Where("code = ?", role.Code).FirstOrCreate(&role).Error; err != nil {
		return fmt.Errorf("seed role: %w", err)
	}

	// admin 用户（bcrypt）
	hash, err := bcrypt.GenerateFromPassword([]byte(seedAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	user := User{
		Username: AdminUsername,
		Password: string(hash),
		Nickname: "管理员",
		Email:    "admin@example.com",
		DeptID:   dept.ID,
		Status:   StatusEnabled,
	}
	if err := tx.Where("username = ?", user.Username).FirstOrCreate(&user).Error; err != nil {
		return fmt.Errorf("seed admin user: %w", err)
	}

	// admin → super 绑定
	ur := UserRole{UserID: user.ID, RoleID: role.ID}
	if err := tx.Where("user_id = ? AND role_id = ?", ur.UserID, ur.RoleID).FirstOrCreate(&ur).Error; err != nil {
		return fmt.Errorf("seed user_role: %w", err)
	}

	// 菜单树 + super 角色全量绑定
	return seedMenuBranch(tx, role.ID, 0, seedMenuTree)
}

func seedMenuBranch(tx *gorm.DB, roleID, parentID uint64, items []seedMenuItem) error {
	for i, item := range items {
		m := Menu{
			ParentID:   parentID,
			Type:       item.Type,
			Name:       item.Name,
			Title:      item.Title,
			Path:       item.Path,
			Component:  item.Component,
			Icon:       item.Icon,
			Sort:       i + 1,
			Status:     StatusEnabled,
			Permission: item.Permission,
			Affix:      item.Affix,
			KeepAlive:  item.KeepAlive,
		}
		if err := tx.Where("parent_id = ? AND name = ?", parentID, item.Name).FirstOrCreate(&m).Error; err != nil {
			return fmt.Errorf("seed menu %s: %w", item.Name, err)
		}
		rm := RoleMenu{RoleID: roleID, MenuID: m.ID}
		if err := tx.Where("role_id = ? AND menu_id = ?", roleID, m.ID).FirstOrCreate(&rm).Error; err != nil {
			return fmt.Errorf("seed role_menu %s: %w", item.Name, err)
		}
		if err := seedMenuBranch(tx, roleID, m.ID, item.Children); err != nil {
			return err
		}
	}
	return nil
}
