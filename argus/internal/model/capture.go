package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/plugin/soft_delete"
)

const (
	CaptureTargetFace     = "face"
	CaptureTargetPerson   = "person"
	CaptureTargetVehicle  = "vehicle"
	CaptureTargetNonMotor = "non_motor"
	CaptureTargetGeneric  = "generic"
)

// FaceAttributes 描述人脸抓拍的可选属性。未提供的字段保持零值。
type FaceAttributes struct {
	Gender  string  `json:"gender,omitempty"`
	Age     int     `json:"age,omitempty"`
	Mask    bool    `json:"mask,omitempty"`
	Glasses bool    `json:"glasses,omitempty"`
	Pitch   float32 `json:"pitch,omitempty"`
	Yaw     float32 `json:"yaw,omitempty"`
	Roll    float32 `json:"roll,omitempty"`
}

// PersonAttributes 描述人体抓拍的可选属性。
type PersonAttributes struct {
	UpperColor string          `json:"upperColor,omitempty"`
	LowerColor string          `json:"lowerColor,omitempty"`
	Hat        bool            `json:"hat,omitempty"`
	Bag        bool            `json:"bag,omitempty"`
	HasFace    bool            `json:"hasFace,omitempty"`
	Face       *FaceAttributes `json:"face,omitempty"`
}

// VehicleAttributes 描述机动车抓拍的可选属性。
type VehicleAttributes struct {
	PlateNumber  string `json:"plateNumber,omitempty"`
	PlateColor   string `json:"plateColor,omitempty"`
	VehicleType  string `json:"vehicleType,omitempty"`
	VehicleColor string `json:"vehicleColor,omitempty"`
	Brand        string `json:"brand,omitempty"`
}

// NonMotorAttributes 描述非机动车抓拍的可选属性。
type NonMotorAttributes struct {
	VehicleType string `json:"vehicleType,omitempty"`
	Color       string `json:"color,omitempty"`
	HasRider    bool   `json:"hasRider,omitempty"`
}

// CaptureRecord 通用抓拍事件记录。
// 每个满足算法端冷却条件的 CaptureEvent 独立插入一行，不做同轨迹聚合。
type CaptureRecord struct {
	// CaptureRecord 自行声明公共字段，以便 deleted_at 参与 event_id 的复合唯一索引；
	// 直接嵌入 BaseModel 会产生重复的 DeletedAt 字段映射。
	ID        uint64                `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time             `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time             `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_captures_event_id" json:"-"`

	EventID          string `gorm:"column:event_id;size:200;not null;uniqueIndex:uk_captures_event_id" json:"eventId"`
	InstanceID       string `gorm:"column:instance_id;size:64;not null;default:'';index:idx_captures_instance_id" json:"instanceId"`
	TargetType       string `gorm:"column:target_type;size:32;not null;default:'generic';index:idx_captures_target_type" json:"targetType"`
	CameraID         string `gorm:"column:camera_id;size:64;not null;index:idx_captures_camera_id" json:"cameraId"`
	CameraName       string `gorm:"column:camera_name;size:128;not null;default:''" json:"cameraName"`
	TaskID           uint64 `gorm:"column:task_id;not null;default:0;index:idx_captures_task_id" json:"taskId"`
	AlgorithmID      string `gorm:"column:algorithm_id;size:64;not null;default:''" json:"algorithmId"`
	AlgorithmVersion string `gorm:"column:algorithm_version;size:32;not null;default:''" json:"algorithmVersion"`
	TrackID          int64  `gorm:"column:track_id;not null;default:0;index:idx_captures_track_id" json:"trackId"`

	Confidence   float32 `gorm:"column:confidence;not null;default:0" json:"confidence"`
	QualityScore float32 `gorm:"column:quality_score;not null;default:0" json:"qualityScore"`
	BBoxJSON     JSONRaw `gorm:"column:bbox_json;type:jsonb;not null;default:'{}'" json:"bboxJson"`
	SubBBoxJSON  JSONRaw `gorm:"column:sub_bbox_json;type:jsonb;not null;default:'{}'" json:"subBboxJson"`
	TimeSynced   bool    `gorm:"column:time_synced;not null;default:false" json:"timeSynced"`

	ImageID             string    `gorm:"column:image_id;size:200;not null;default:'';index:idx_captures_image_id" json:"imageId"`
	ImageRelPath        string    `gorm:"column:image_rel_path;size:255;not null;default:''" json:"imageRelPath"`
	CropImageID         string    `gorm:"column:crop_image_id;size:200;not null;default:'';index:idx_captures_crop_image_id" json:"cropImageId"`
	CropImageRelPath    string    `gorm:"column:crop_image_rel_path;size:255;not null;default:''" json:"cropImageRelPath"`
	SubCropImageID      string    `gorm:"column:sub_crop_image_id;size:200;not null;default:'';index:idx_captures_sub_crop_image_id" json:"subCropImageId"`
	SubCropImageRelPath string    `gorm:"column:sub_crop_image_rel_path;size:255;not null;default:''" json:"subCropImageRelPath"`
	IsRecognized        bool      `gorm:"column:is_recognized;not null;default:false;index:idx_captures_is_recognized" json:"isRecognized"`
	AttributesJSON      JSONRaw   `gorm:"column:attributes_json;type:jsonb;not null;default:'{}'" json:"attributesJson"`
	CapturedAt          time.Time `gorm:"column:captured_at;not null;index:idx_captures_captured_at" json:"capturedAt"`
}

// TableName 显式声明通用抓拍表名。
func (CaptureRecord) TableName() string { return "captures" }

// ParseBBox 解析主目标或附属目标坐标。
func (r *CaptureRecord) ParseBBox(sub bool) ([]float32, error) {
	if r == nil {
		return nil, errors.New("capture record is nil")
	}
	data := r.BBoxJSON
	if sub {
		data = r.SubBBoxJSON
	}
	return parseCaptureBBox(data)
}

// ParseAttributes 解析多态属性 JSON 对象。
func (r *CaptureRecord) ParseAttributes() (map[string]any, error) {
	if r == nil {
		return nil, errors.New("capture record is nil")
	}
	if len(r.AttributesJSON) == 0 {
		return map[string]any{}, nil
	}
	var attributes map[string]any
	if err := json.Unmarshal(r.AttributesJSON, &attributes); err != nil {
		return nil, fmt.Errorf("parse capture attributes: %w", err)
	}
	if attributes == nil {
		return map[string]any{}, nil
	}
	return attributes, nil
}

func parseCaptureBBox(data JSONRaw) ([]float32, error) {
	if len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		return nil, nil
	}
	var values []float32
	if err := json.Unmarshal(data, &values); err == nil {
		if len(values) == 4 {
			return values, nil
		}
		return nil, fmt.Errorf("bbox must contain four values")
	}
	var object struct {
		XMin float32 `json:"x_min"`
		YMin float32 `json:"y_min"`
		XMax float32 `json:"x_max"`
		YMax float32 `json:"y_max"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("parse bbox: %w", err)
	}
	return []float32{object.XMin, object.YMin, object.XMax, object.YMax}, nil
}
