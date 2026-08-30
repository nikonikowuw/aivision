package model

const (
	// ConfigKeyTime 对时服务配置键。
	ConfigKeyTime = "system:time"
)

// 对时模式枚举（TimeConfigValue.Mode 的取值契约）。
const (
	TimeModeNTP    = "ntp"
	TimeModeManual = "manual"
)

// SystemConfig 通用系统配置表模型（无软删除，直接持久化配置 JSONB）
type SystemConfig struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TimeFields
	Key    string `gorm:"column:key;size:64;not null;uniqueIndex:uk_system_configs_key" json:"key"`
	Value  string `gorm:"column:value;type:jsonb;not null;default:'{}'" json:"value"`
	Remark string `gorm:"column:remark;size:255" json:"remark"`
}

func (SystemConfig) TableName() string {
	return "system_configs"
}

// TimeConfigValue 对时业务配置 DTO
type TimeConfigValue struct {
	Mode    string   `json:"mode"`    // "ntp" | "manual"
	Servers []string `json:"servers"` // NTP 服务器列表
}
