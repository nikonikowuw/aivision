package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// AlarmRecord 告警记录数据模型（单目标单记录 1 Target = 1 Record）。
// 映射 PostgreSQL alarm_records 表。
type AlarmRecord struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 复合唯一索引 uk_alarm_records_event_id
	DeletedAt        soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_alarm_records_event_id" json:"-"`
	EventID          string                `gorm:"column:event_id;size:200;not null;uniqueIndex:uk_alarm_records_event_id" json:"eventId"`
	InstanceID       string                `gorm:"column:instance_id;size:36;not null;index:idx_alarm_records_instance_id" json:"instanceId"`
	CameraID         string                `gorm:"column:camera_id;size:36;not null;index:idx_alarm_records_camera_id" json:"cameraId"`
	AlgorithmID      string                `gorm:"column:algorithm_id;size:64;not null;index:idx_alarm_records_algorithm_id" json:"algorithmId"`
	AlgorithmVersion string                `gorm:"column:algorithm_version;size:32;not null" json:"algorithmVersion"`
	AlarmTypeID      string                `gorm:"column:alarm_type_id;size:128;not null" json:"alarmTypeId"`
	OccurredAt       time.Time             `gorm:"column:occurred_at;not null;index:idx_alarm_records_occurred_at,sort:desc" json:"occurredAt"`
	TimeSynced       bool                  `gorm:"column:time_synced;not null;default:true" json:"timeSynced"`
	TargetLabel      string                `gorm:"column:target_label;size:64;not null;default:''" json:"targetLabel"`
	Confidence       float32               `gorm:"column:confidence;not null;default:0;index:idx_alarm_records_confidence" json:"confidence"`
	TrackID          int64                 `gorm:"column:track_id;not null;default:0" json:"trackId"`
	BBoxJSON         []byte                `gorm:"column:bbox_json;type:jsonb;not null;default:'[]'" json:"bboxJson"` // [x1, y1, x2, y2]
	ImageID          string                `gorm:"column:image_id;size:200;not null;default:'';index:idx_alarm_records_image_id" json:"imageId"`
	ImageRelPath     string                `gorm:"column:image_rel_path;size:255;not null;default:''" json:"imageRelPath"`
}

// TableName 显式声明表名。
func (AlarmRecord) TableName() string {
	return "alarm_records"
}
