package model

import (
	"database/sql/driver"
	"fmt"
	"time"

	"gorm.io/plugin/soft_delete"
)

// Scan 和 Value 实现 sql.Scanner 与 driver.Valuer，确保在 SQLite 单测和 PG 中均可直接读写 json/jsonb 字段。
type JSONRaw []byte

func (j JSONRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "{}", nil
	}
	return string(j), nil
}

func (j *JSONRaw) Scan(value any) error {
	if value == nil {
		*j = []byte("{}")
		return nil
	}
	switch s := value.(type) {
	case []byte:
		*j = append((*j)[0:0], s...)
	case string:
		*j = append((*j)[0:0], s...)
	default:
		return fmt.Errorf("unsupported Scan, storing %T into type *JSONRaw", value)
	}
	return nil
}

func (j JSONRaw) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("{}"), nil
	}
	return j, nil
}

func (j *JSONRaw) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("JSONRaw: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[0:0], data...)
	return nil
}

func (j JSONRaw) GormDataType() string {
	return "jsonb"
}

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
