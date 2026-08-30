package model

import (
	"encoding/json"
)

// FPSTier 算法包声明的离散 FPS 资源消耗档位。
type FPSTier struct {
	FPS   int32  `json:"fps"`
	Units uint32 `json:"units"`
}

// Algorithm 算法基础信息表。
type Algorithm struct {
	BaseModel
	AlgorithmID   string `gorm:"column:algorithm_id;not null;size:64" json:"algorithmId"`
	Name          string `gorm:"column:name;not null;size:128" json:"name"`
	AlgorithmType string `gorm:"column:algorithm_type;not null;size:32" json:"algorithmType"`
	AlarmTypeID   string `gorm:"column:alarm_type_id;not null;size:64;default:''" json:"alarmTypeId"`
	ActiveVersion string `gorm:"column:active_version;not null;size:32;default:''" json:"activeVersion"`
	Description   string `gorm:"column:description;not null;default:''" json:"description"`

	// 关联的版本列表（可按需 Preload）
	Versions []*AlgorithmVersion `gorm:"foreignKey:AlgorithmID;references:AlgorithmID" json:"versions,omitempty"`
}

// TableName 显式声明表名。
func (Algorithm) TableName() string {
	return "algorithms"
}

// AlgorithmVersion 算法包具体版本信息表。
type AlgorithmVersion struct {
	BaseModel
	AlgorithmID       string          `gorm:"column:algorithm_id;not null;size:64" json:"algorithmId"`
	Version           string          `gorm:"column:version;not null;size:32" json:"version"`
	PlatformID        string          `gorm:"column:platform_id;not null;size:64" json:"platformId"`
	MinAdapterVersion string          `gorm:"column:min_adapter_version;not null;size:32;default:''" json:"minAdapterVersion"`
	PackageRoot       string          `gorm:"column:package_root;not null;size:255;default:''" json:"packageRoot"`
	FPSTiers          json.RawMessage `gorm:"column:fps_tiers;type:jsonb;not null;default:'[]'" json:"fpsTiers"`
	ConfigSchema      json.RawMessage `gorm:"column:config_schema;type:jsonb;not null;default:'{}'" json:"configSchema"`
	ManifestRaw       json.RawMessage `gorm:"column:manifest_raw;type:jsonb;not null;default:'{}'" json:"manifestRaw"`
	PackageSizeBytes  int64           `gorm:"column:package_size_bytes;not null;default:0" json:"packageSizeBytes"`
	IsActive          bool            `gorm:"column:is_active;not null;default:false" json:"isActive"`
}

// TableName 显式声明表名。
func (AlgorithmVersion) TableName() string {
	return "algorithm_versions"
}
