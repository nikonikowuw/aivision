package model

// Role 角色（表名 roles）。
type Role struct {
	BaseModel
	Name   string `gorm:"column:name;type:varchar(64);not null" json:"name"`
	Code   string `gorm:"column:code;type:varchar(64);not null;uniqueIndex" json:"code"`
	Status int8   `gorm:"column:status;default:1" json:"status"` // 1 启用 / 0 禁用；类型由 gorm 按驱动映射（决策 18）
	Sort   int    `gorm:"column:sort" json:"sort"`
	Remark string `gorm:"column:remark;type:varchar(255)" json:"remark"`
}

// RoleSuperCode 预置超级管理员角色编码。
const RoleSuperCode = "super"

// TableName 显式声明表名。
func (Role) TableName() string { return "roles" }
