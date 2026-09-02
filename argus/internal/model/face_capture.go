package model

import (
	"encoding/json"
	"time"

	"gorm.io/plugin/soft_delete"
)

// SnapshotItem 存储在 snapshots_json 中的单张时序快照结构。
type SnapshotItem struct {
	SnapshotIndex    int32               `json:"snapshotIndex"`
	WallTimeNs       int64               `json:"wallTimeNs"`
	TimeSynced       bool                `json:"timeSynced"`
	ObservedAt       time.Time           `json:"observedAt"`
	ImageID          string              `json:"imageId"`
	ImageRelPath     string              `json:"imageRelPath"`
	FaceImageID      string              `json:"faceImageId"`
	FaceImageRelPath string              `json:"faceImageRelPath"`
	BBoxJSON         JSONRaw             `json:"bboxJson"`
	QualityScore     float32             `json:"qualityScore"`
	Similarity       float32             `json:"similarity"`
	FaceID           string              `json:"faceId,omitempty"`
	PersonID         string              `json:"personId,omitempty"`
	PersonName       string              `json:"personName,omitempty"`
	Candidates       []FaceCandidateItem `json:"candidates,omitempty"`
}

// FaceCapture 人脸抓拍全量事件记录模型。
// 对应 face_captures 生产表（000034 迁移），采用 track 级增量单调 upsert 语义（按 event_id 去重并追加 1~5 组快照）。
type FaceCapture struct {
	BaseModel
	DeletedAt        soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_face_captures_event_id" json:"-"`
	EventID          string                `gorm:"column:event_id;size:200;not null;uniqueIndex:uk_face_captures_event_id" json:"eventId"`
	InstanceID       string                `gorm:"column:instance_id;size:64;not null;default:''" json:"instanceId"`
	CameraID         string                `gorm:"column:camera_id;size:64;not null;index:idx_face_captures_camera_id" json:"cameraId"`
	CameraName       string                `gorm:"column:camera_name;size:128;not null;default:''" json:"cameraName"`
	AlgorithmID      string                `gorm:"column:algorithm_id;size:64;not null;default:''" json:"algorithmId"`
	AlgorithmVersion string                `gorm:"column:algorithm_version;size:32;not null;default:''" json:"algorithmVersion"`
	TrackID          int64                 `gorm:"column:track_id;not null;default:0" json:"trackId"`

	// 最佳识别/快照摘要（原生列以保证高性能列表查询）
	BestSimilarity     float32 `gorm:"column:best_similarity;not null;default:0" json:"bestSimilarity"`
	BestQualityScore   float32 `gorm:"column:best_quality_score;not null;default:0" json:"bestQualityScore"`
	BestPersonID       string  `gorm:"column:best_person_id;size:64;not null;default:'';index:idx_face_captures_best_person_id" json:"bestPersonId"`
	BestPersonName     string  `gorm:"column:best_person_name;size:128;not null;default:''" json:"bestPersonName"`
	BestImageID        string  `gorm:"column:best_image_id;size:200;not null;default:''" json:"bestImageId"`
	BestImageRelPath   string  `gorm:"column:best_image_rel_path;size:255;not null;default:''" json:"bestImageRelPath"`
	BestFaceImageID    string  `gorm:"column:best_face_image_id;size:200;not null;default:''" json:"bestFaceImageId"`
	BestFaceRelPath    string  `gorm:"column:best_face_rel_path;size:255;not null;default:''" json:"bestFaceRelPath"`
	BestBBoxJSON       JSONRaw `gorm:"column:best_bbox_json;type:jsonb;not null;default:'{}'" json:"bestBboxJson"`
	BestCandidatesJSON JSONRaw `gorm:"column:best_candidates_json;type:jsonb;not null;default:'[]'" json:"bestCandidatesJson"`

	// 1~5 组时序快照序列
	SnapshotCount int32   `gorm:"column:snapshot_count;not null;default:1" json:"snapshotCount"`
	SnapshotsJSON JSONRaw `gorm:"column:snapshots_json;type:jsonb;not null;default:'[]'" json:"snapshotsJson"`

	FirstObservedAt time.Time `gorm:"column:first_observed_at;not null;index:idx_face_captures_first_observed_at" json:"firstObservedAt"`
	LastObservedAt  time.Time `gorm:"column:last_observed_at;not null" json:"lastObservedAt"`
}

// TableName 显式声明表名。
func (FaceCapture) TableName() string {
	return "face_captures"
}

// ParseSnapshots 解析 snapshots_json 数组。
func (f *FaceCapture) ParseSnapshots() ([]SnapshotItem, error) {
	if len(f.SnapshotsJSON) == 0 {
		return []SnapshotItem{}, nil
	}
	var items []SnapshotItem
	if err := json.Unmarshal(f.SnapshotsJSON, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// ParseBestCandidates 解析 best_candidates_json 数组。
func (f *FaceCapture) ParseBestCandidates() ([]FaceCandidateItem, error) {
	if len(f.BestCandidatesJSON) == 0 {
		return []FaceCandidateItem{}, nil
	}
	var items []FaceCandidateItem
	if err := json.Unmarshal(f.BestCandidatesJSON, &items); err != nil {
		return nil, err
	}
	return items, nil
}
