package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// PlateObservation 车牌抓拍过车记录数据模型。
// 映射 PostgreSQL plate_observations 表。
type PlateObservation struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 复合唯一索引 uk_plate_observations_event_id
	DeletedAt       soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_plate_observations_event_id" json:"-"`
	EventID         string                `gorm:"column:event_id;size:200;not null;uniqueIndex:uk_plate_observations_event_id" json:"eventId"`
	TaskID          string                `gorm:"column:task_id;size:64;not null;default:''" json:"taskId"`
	InstanceID      string                `gorm:"column:instance_id;size:64;not null;default:''" json:"instanceId"`
	CameraID        string                `gorm:"column:camera_id;size:64;not null;index:idx_plate_observations_camera_id" json:"cameraId"`
	CameraName      string                `gorm:"column:camera_name;size:128;not null;default:''" json:"cameraName"`
	PlateText       string                `gorm:"column:plate_text;size:32;not null;index:idx_plate_observations_plate_text" json:"plateText"`
	NormalizedText  string                `gorm:"column:normalized_text;size:32;not null;index:idx_plate_observations_normalized_text" json:"normalizedText"`
	PlateColor      string                `gorm:"column:plate_color;size:16;not null;default:'';index:idx_plate_observations_plate_color" json:"plateColor"`
	PlateType       string                `gorm:"column:plate_type;size:32;not null;default:''" json:"plateType"`
	Confidence      float32               `gorm:"column:confidence;not null;default:0" json:"confidence"`
	OcrConfidence   float32               `gorm:"column:ocr_confidence;not null;default:0" json:"ocrConfidence"`
	TrackID         int64                 `gorm:"column:track_id;not null;default:0" json:"trackId"`
	BBoxJSON        []byte                `gorm:"column:bbox_json;type:jsonb;not null;default:'{}'" json:"bboxJson"`
	VehicleBBoxJSON []byte                `gorm:"column:vehicle_bbox_json;type:jsonb;not null;default:'{}'" json:"vehicleBBoxJson"`
	PanoramaImage   string                `gorm:"column:panorama_image;size:255;not null;default:''" json:"panoramaImage"`
	PlateImage      string                `gorm:"column:plate_image;size:255;not null;default:''" json:"plateImage"`
	ObservedAt      time.Time             `gorm:"column:observed_at;not null;index:idx_plate_observations_observed_at,sort:desc" json:"observedAt"`
}

// TableName 显式声明表名。
func (PlateObservation) TableName() string {
	return "plate_observations"
}
