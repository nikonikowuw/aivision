package model

// UserRole 用户-角色关联（表名 user_roles，复合唯一）。
type UserRole struct {
	ID     uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID uint64 `gorm:"column:user_id;not null;uniqueIndex:uk_user_role" json:"userId"`
	RoleID uint64 `gorm:"column:role_id;not null;uniqueIndex:uk_user_role" json:"roleId"`
}

// TableName 显式声明表名。
func (UserRole) TableName() string { return "user_roles" }

// RoleMenu 角色-菜单关联（表名 role_menus，复合唯一）。
type RoleMenu struct {
	ID     uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RoleID uint64 `gorm:"column:role_id;not null;uniqueIndex:uk_role_menu" json:"roleId"`
	MenuID uint64 `gorm:"column:menu_id;not null;uniqueIndex:uk_role_menu" json:"menuId"`
}

// TableName 显式声明表名。
func (RoleMenu) TableName() string { return "role_menus" }
