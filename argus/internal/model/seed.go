package model

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// seedMenuItem 种子菜单声明（设计 §5 权限码契约表，全项目唯一权限码源）。
type seedMenuItem struct {
	Type       string
	Name       string // catalog/menu 为 ASCII 路由标识符；button 为 i18n key（如 system.user.addUser）
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
// 顺序调整：实时预览(1) -> 智能记录(2) -> 资源管理(3) -> AI算法(4) -> 运维管理(5) -> 系统管理(6)
var seedMenuTree = []seedMenuItem{
	{
		Type: MenuTypeMenu, Name: "LivePreview", Title: "routes.live.live", Path: "/live", Component: "/live/index",
		Icon: "ant-design:video-camera-outlined", Permission: "live:preview", Affix: true,
		Children: []seedMenuItem{
			{Type: MenuTypeButton, Name: "live.preview.stream", Permission: "live:preview:stream"},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Record", Title: "routes.record.record", Path: "/record", Component: "BasicLayout",
		Icon: "ant-design:history-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "RecordAlarm", Title: "routes.record.alarm", Path: "/record/alarm", Component: "/record/alarm/index",
				Icon: "ant-design:alert-outlined", Permission: "record:alarm", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "record.alarm.query", Permission: "record:alarm:query"},
					{Type: MenuTypeButton, Name: "record.alarm.export", Permission: "record:alarm:export"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Resource", Title: "routes.resource.resource", Path: "/resource", Component: "BasicLayout",
		Icon: "ant-design:database-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "Camera", Title: "routes.resource.camera", Path: "/resource/camera", Component: "/resource/camera/index",
				Icon: "ant-design:video-camera-outlined", Permission: "resource:camera",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.camera.add", Permission: "resource:camera:add"},
					{Type: MenuTypeButton, Name: "resource.camera.edit", Permission: "resource:camera:edit"},
					{Type: MenuTypeButton, Name: "resource.camera.delete", Permission: "resource:camera:delete"},
					{Type: MenuTypeButton, Name: "resource.camera.probe", Permission: "resource:camera:probe"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "ResourcePerson", Title: "routes.resource.person", Path: "/resource/person", Component: "/resource/person/index",
				Icon: "ant-design:idcard-outlined", Permission: "resource:person", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.person.add", Permission: "resource:person:add"},
					{Type: MenuTypeButton, Name: "resource.person.edit", Permission: "resource:person:edit"},
					{Type: MenuTypeButton, Name: "resource.person.delete", Permission: "resource:person:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "ResourceTask", Title: "routes.resource.task", Path: "/resource/task", Component: "/resource/task/index",
				Icon: "ant-design:profile-outlined", Permission: "resource:task", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.task.add", Permission: "resource:task:add"},
					{Type: MenuTypeButton, Name: "resource.task.edit", Permission: "resource:task:edit"},
					{Type: MenuTypeButton, Name: "resource.task.delete", Permission: "resource:task:delete"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "AI", Title: "routes.ai.ai", Path: "/ai", Component: "BasicLayout",
		Icon: "ant-design:robot-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "AiAlgorithm", Title: "routes.ai.algorithm", Path: "/ai/algorithm", Component: "/ai/algorithm/index",
				Icon: "ant-design:appstore-outlined", Permission: "ai:algorithm", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "ai.algorithm.upload", Permission: "ai:algorithm:upload"},
					{Type: MenuTypeButton, Name: "ai.algorithm.activate", Permission: "ai:algorithm:activate"},
					{Type: MenuTypeButton, Name: "ai.algorithm.uninstall", Permission: "ai:algorithm:uninstall"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Ops", Title: "routes.ops.ops", Path: "/ops", Component: "BasicLayout",
		Icon: "ant-design:tool-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "Time", Title: "routes.ops.time", Path: "/ops/time", Component: "/ops/time/index",
				Icon: "ant-design:field-time-outlined", Permission: "ops:time",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "ops.time.read", Permission: "ops:time:read"},
					{Type: MenuTypeButton, Name: "ops.time.edit", Permission: "ops:time:edit"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Network", Title: "routes.ops.network", Path: "/ops/network", Component: "/ops/network/index",
				Icon: "ant-design:global-outlined", Permission: "ops:network",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.common.edit", Permission: "ops:network:edit"},
					{Type: MenuTypeButton, Name: "ops.network.confirm", Permission: "ops:network:confirm"},
					{Type: MenuTypeButton, Name: "ops.network.cancel", Permission: "ops:network:cancel"},
					{Type: MenuTypeButton, Name: "ops.network.reset", Permission: "ops:network:reset"},
					{Type: MenuTypeButton, Name: "ops.network.mode", Permission: "ops:network:mode"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "System", Title: "routes.system.system", Path: "/system", Component: "BasicLayout",
		Icon: "ant-design:setting-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "User", Title: "routes.system.user", Path: "/system/user", Component: "/system/user/index",
				Icon: "ant-design:user-outlined", Permission: "system:user",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.user.addUser", Permission: "system:user:add"},
					{Type: MenuTypeButton, Name: "system.user.editUser", Permission: "system:user:edit"},
					{Type: MenuTypeButton, Name: "system.user.deleteUser", Permission: "system:user:delete"},
					{Type: MenuTypeButton, Name: "system.user.resetPassword", Permission: "system:user:reset-password"},
					{Type: MenuTypeButton, Name: "system.user.assignRole", Permission: "system:user:assign-role"},
					{Type: MenuTypeButton, Name: "system.user.status", Permission: "system:user:status"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Role", Title: "routes.system.role", Path: "/system/role", Component: "/system/role/index",
				Icon: "ant-design:team-outlined", Permission: "system:role",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.role.addRole", Permission: "system:role:add"},
					{Type: MenuTypeButton, Name: "system.role.editRole", Permission: "system:role:edit"},
					{Type: MenuTypeButton, Name: "system.role.deleteRole", Permission: "system:role:delete"},
					{Type: MenuTypeButton, Name: "system.role.assignMenu", Permission: "system:role:assign-menu"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Menu", Title: "routes.system.menu", Path: "/system/menu", Component: "/system/menu/index",
				Icon: "ant-design:menu-outlined", Permission: "system:menu",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.menu.addMenu", Permission: "system:menu:add"},
					{Type: MenuTypeButton, Name: "system.menu.editMenu", Permission: "system:menu:edit"},
					{Type: MenuTypeButton, Name: "system.menu.deleteMenu", Permission: "system:menu:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Dept", Title: "routes.system.dept", Path: "/system/dept", Component: "/system/dept/index",
				Icon: "ant-design:apartment-outlined", Permission: "system:dept",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.dept.addDept", Permission: "system:dept:add"},
					{Type: MenuTypeButton, Name: "system.dept.editDept", Permission: "system:dept:edit"},
					{Type: MenuTypeButton, Name: "system.dept.deleteDept", Permission: "system:dept:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Log", Title: "routes.system.log", Path: "/system/log", Component: "/system/log/index",
				Icon: "ant-design:file-text-outlined", Permission: "system:log",
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

	// 菜单树 + super 角色全量绑定
	if err := seedMenuBranch(tx, role.ID, 0, seedMenuTree); err != nil {
		return err
	}

	// admin 用户（bcrypt，仅在指定密码或非空时创建，若环境未设则通过 cmd/bootstrap 手动初始化）
	adminPassword := os.Getenv("APP_BOOTSTRAP_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = seedAdminPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
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
	if err := seedMenuBranch(tx, role.ID, 0, seedMenuTree); err != nil {
		return err
	}

	// 基础系统配置 (system:time)
	timeCfg := SystemConfig{
		Key:    ConfigKeyTime,
		Value:  `{"mode":"ntp","servers":["pool.ntp.org","ntp.aliyun.com"]}`,
		Remark: "系统对时配置",
	}
	if err := tx.Where("key = ?", timeCfg.Key).FirstOrCreate(&timeCfg).Error; err != nil {
		return fmt.Errorf("seed system config (%s): %w", timeCfg.Key, err)
	}

	// 任务版本计数器单行初始化 (id=1, revision=0)
	rev := DesiredStateRevision{ID: 1, Revision: 0}
	if err := tx.Where("id = ?", 1).FirstOrCreate(&rev).Error; err != nil {
		return fmt.Errorf("seed desired_state_revision: %w", err)
	}

	return nil
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
