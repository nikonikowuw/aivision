package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// FaceObservation 人脸通行识别记录模型。
// 对应 face_observations 生产表（000031 迁移），采用 track 级单调 upsert 语义（按 event_id 去重与更新）。
type FaceObservation struct {
	BaseModel
	DeletedAt        soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_face_observations_event_id" json:"-"`
	EventID          string                `gorm:"column:event_id;size:200;not null;uniqueIndex:uk_face_observations_event_id" json:"eventId"`
	InstanceID       string                `gorm:"column:instance_id;size:64;not null;default:''" json:"instanceId"`
	CameraID         string                `gorm:"column:camera_id;size:64;not null;index:idx_face_observations_camera_id" json:"cameraId"`
	CameraName       string                `gorm:"column:camera_name;size:128;not null;default:''" json:"cameraName"`
	AlgorithmID      string                `gorm:"column:algorithm_id;size:64;not null;default:''" json:"algorithmId"`
	AlgorithmVersion string                `gorm:"column:algorithm_version;size:32;not null;default:''" json:"algorithmVersion"`
	TrackID          int64                 `gorm:"column:track_id;not null;default:0" json:"trackId"`
	FaceID           string                `gorm:"column:face_id;size:64;not null;default:''" json:"faceId"`
	PersonID         string                `gorm:"column:person_id;size:64;not null;default:'';index:idx_face_observations_person_id" json:"personId"`
	PersonName       string                `gorm:"column:person_name;size:128;not null;default:''" json:"personName"`
	Similarity       float32               `gorm:"column:similarity;not null;default:0" json:"similarity"`
	BBoxJSON         JSONRaw               `gorm:"column:bbox_json;type:jsonb;not null;default:'{}'" json:"bboxJson"`
	TimeSynced       bool                  `gorm:"column:time_synced;not null;default:false" json:"timeSynced"`
	ImageID          string                `gorm:"column:image_id;size:200;not null;default:'';index:idx_face_observations_image_id" json:"imageId"`
	ImageRelPath     string                `gorm:"column:image_rel_path;size:255;not null;default:''" json:"imageRelPath"`
	FaceImageID      string                `gorm:"column:face_image_id;size:200;not null;default:'';index:idx_face_observations_face_image_id" json:"faceImageId"`
	FaceImageRelPath string                `gorm:"column:face_image_rel_path;size:255;not null;default:''" json:"faceImageRelPath"`
	ObservedAt       time.Time             `gorm:"column:observed_at;not null;index:idx_face_observations_observed_at" json:"observedAt"`
}

// TableName 显式声明表名。
func (FaceObservation) TableName() string {
	return "face_observations"
}
