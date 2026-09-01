package model

import (
	"gorm.io/plugin/soft_delete"
)

// FPSTier 算法包声明的离散 FPS 资源消耗档位。
type FPSTier struct {
	FPS   int32  `json:"fps"`
	Units uint32 `json:"units"`
}

// Algorithm 算法基础信息表。
type Algorithm struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 以加入 algorithm_id 复合唯一索引
	DeletedAt     soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_algorithms_algorithm_id" json:"-"`
	AlgorithmID   string                `gorm:"column:algorithm_id;not null;size:64;uniqueIndex:uk_algorithms_algorithm_id" json:"algorithmId"`
	Name          string                `gorm:"column:name;not null;size:128" json:"name"`
	AlgorithmType string                `gorm:"column:algorithm_type;not null;size:32;index:idx_algorithms_type" json:"algorithmType"`
	AlarmTypeID   string                `gorm:"column:alarm_type_id;not null;size:64;default:''" json:"alarmTypeId"`
	ActiveVersion string                `gorm:"column:active_version;not null;size:32;default:''" json:"activeVersion"`
	Description   string                `gorm:"column:description;not null;default:''" json:"description"`
	IsBuiltin     bool                  `gorm:"column:is_builtin;not null;default:false" json:"isBuiltin"`

	// 关联的版本列表（可按需 Preload，不创建物理外键约束以对齐项目不建物理外键规范）
	Versions []*AlgorithmVersion `gorm:"foreignKey:AlgorithmID;references:AlgorithmID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;-:migration" json:"versions,omitempty"`
}

// TableName 显式声明表名。
func (Algorithm) TableName() string {
	return "algorithms"
}

// AlgorithmVersion 算法包具体版本信息表。
type AlgorithmVersion struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 以加入 algorithm_id + version + platform_id 复合唯一索引
	DeletedAt         soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_algorithm_versions" json:"-"`
	AlgorithmID       string                `gorm:"column:algorithm_id;not null;size:64;uniqueIndex:uk_algorithm_versions;index:idx_algorithm_versions_algorithm_id" json:"algorithmId"`
	Version           string                `gorm:"column:version;not null;size:32;uniqueIndex:uk_algorithm_versions" json:"version"`
	PlatformID        string                `gorm:"column:platform_id;not null;size:64;uniqueIndex:uk_algorithm_versions" json:"platformId"`
	MinAdapterVersion string                `gorm:"column:min_adapter_version;not null;size:32;default:''" json:"minAdapterVersion"`
	PackageRoot       string                `gorm:"column:package_root;not null;size:255;default:''" json:"packageRoot"`
	FPSTiers          JSONRaw               `gorm:"column:fps_tiers;type:jsonb;not null;default:'[]'" json:"fpsTiers"`
	ConfigSchema      JSONRaw               `gorm:"column:config_schema;type:jsonb;not null;default:'{}'" json:"configSchema"`
	ManifestRaw       JSONRaw               `gorm:"column:manifest_raw;type:jsonb;not null;default:'{}'" json:"manifestRaw"`
	PackageSizeBytes  int64                 `gorm:"column:package_size_bytes;not null;default:0" json:"packageSizeBytes"`
	IsActive          bool                  `gorm:"column:is_active;not null;default:false" json:"isActive"`
	IsBuiltin         bool                  `gorm:"column:is_builtin;not null;default:false" json:"isBuiltin"`
}

// TableName 显式声明表名。
func (AlgorithmVersion) TableName() string {
	return "algorithm_versions"
}
