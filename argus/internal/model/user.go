package model

import (
	"gorm.io/plugin/soft_delete"
)

// AdminUserID 系统内置管理员用户 ID。
const AdminUserID uint64 = 1

// AdminUsername 系统内置管理员用户名。
const AdminUsername = "admin"

// User 用户（表名 users）。
type User struct {
	BaseModel
	DeletedAt soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_users_username" json:"-"`
	Username  string                `gorm:"column:username;type:varchar(64);not null;uniqueIndex:uk_users_username" json:"username"`
	Password  string                `gorm:"column:password;type:varchar(255)" json:"-"`
	Nickname  string                `gorm:"column:nickname;type:varchar(64)" json:"nickname"`
	Email     string                `gorm:"column:email;type:varchar(128)" json:"email"`
	Phone     string                `gorm:"column:phone;type:varchar(32)" json:"phone"`
	Avatar    string                `gorm:"column:avatar;type:varchar(255)" json:"avatar"`
	DeptID    uint64                `gorm:"column:dept_id;index" json:"deptId"`
	Status    int8                  `gorm:"column:status;default:1" json:"status"` // 1 启用 / 0 禁用；类型由 gorm 按驱动映射（决策 18）
	Remark    string                `gorm:"column:remark;type:varchar(255)" json:"remark"`
}

// TableName 显式声明表名。
func (User) TableName() string { return "users" }
