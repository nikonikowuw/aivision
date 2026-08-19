package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// BaseModel 业务表公共字段：ID 自增 + 时间戳 + 软删除（gorm 需带索引列）。
type BaseModel struct {
	ID        uint64                `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time             `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time             `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0" json:"-"`
}

// TimeFields 无软删除表的公共时间字段（refresh_tokens / operation_logs）。
type TimeFields struct {
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}
