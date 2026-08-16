package model

// Department 部门（表名 departments）。
type Department struct {
	BaseModel
	ParentID uint64 `gorm:"column:parent_id;not null;default:0" json:"parentId"` // 0=根
	Name     string `gorm:"column:name;type:varchar(64);not null" json:"name"`
	Sort     int    `gorm:"column:sort;default:0" json:"sort"`
	Leader   string `gorm:"column:leader;type:varchar(64)" json:"leader"`
	Phone    string `gorm:"column:phone;type:varchar(32)" json:"phone"`
	Status   int8   `gorm:"column:status;default:1" json:"status"` // 1 启用 / 0 禁用；类型由 gorm 按驱动映射（决策 18）
}

// TableName 显式声明表名。
func (Department) TableName() string { return "departments" }
