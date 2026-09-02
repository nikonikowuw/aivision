package model

const (
	// ConfigKeyTime 对时服务配置键。
	ConfigKeyTime = "system:time"
	// ConfigKeyStorageRetention 存储清理与保留策略配置键。
	ConfigKeyStorageRetention = "system:storage:retention"
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

// StorageRetentionConfigValue 存储清理策略配置 DTO
type StorageRetentionConfigValue struct {
	RetentionDays        int  `json:"retentionDays"`        // 常规保留天数 (1~365, 默认 30)
	HighWatermarkPercent int  `json:"highWatermarkPercent"` // 高水位触发阈值 (50~95, 默认 85)
	LowWatermarkPercent  int  `json:"lowWatermarkPercent"`  // 低水位目标阈值 (30~90, 默认 70)
	CheckIntervalSeconds int  `json:"checkIntervalSeconds"` // 巡检周期秒数 (30~86400, 默认 600)
	AutoCleanupEnabled   bool `json:"autoCleanupEnabled"`   // 是否启用自动清理 (默认 true)
}

// DefaultStorageRetentionConfig 返回存储清理策略的默认配置。
func DefaultStorageRetentionConfig() StorageRetentionConfigValue {
	return StorageRetentionConfigValue{
		RetentionDays:        30,
		HighWatermarkPercent: 85,
		LowWatermarkPercent:  70,
		CheckIntervalSeconds: 600,
		AutoCleanupEnabled:   true,
	}
}
