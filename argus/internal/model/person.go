package model

import "gorm.io/plugin/soft_delete"

// Person 人员基础信息模型。
// 内部主键自增 uint64；person_id 为对外唯一标识（创建后不可修改，且在所有记录含软删除中全局唯一）。
type Person struct {
	BaseModel
	PersonID      string `gorm:"column:person_id;size:64;not null;uniqueIndex:uk_persons_person_id" json:"personId"`
	Name          string `gorm:"column:name;size:64;not null" json:"name"`
	PrimaryFaceID string `gorm:"column:primary_face_id;size:64;not null;default:''" json:"primaryFaceId"`
}

// TableName 返回人员表名。
func (Person) TableName() string {
	return "persons"
}

// PersonFace 人员人脸样本模型。
type PersonFace struct {
	BaseModel
	DeletedAt        soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_person_faces_face_id;uniqueIndex:uk_person_faces_raw_sha256" json:"-"`
	PersonID         string                `gorm:"column:person_id;size:64;not null;index:idx_person_faces_person_id" json:"personId"`
	FaceID           string                `gorm:"column:face_id;size:64;not null;uniqueIndex:uk_person_faces_face_id" json:"faceId"`
	AlgorithmID      string                `gorm:"column:algorithm_id;size:64;not null;default:''" json:"algorithmId"`
	AlgorithmVersion string                `gorm:"column:algorithm_version;size:64;not null;default:''" json:"algorithmVersion"`
	Embedding        []byte                `gorm:"column:embedding;type:blob;not null" json:"-"`
	QualityScore     float32               `gorm:"column:quality_score;not null;default:0" json:"qualityScore"`
	DetectionScore   float32               `gorm:"column:detection_score;not null;default:0" json:"detectionScore"`
	BoundingBox      string                `gorm:"column:bounding_box;size:255;not null;default:''" json:"boundingBox"`
	RawImageKey      string                `gorm:"column:raw_image_key;size:255;not null" json:"rawImageKey"`
	RawImageSHA256   string                `gorm:"column:raw_image_sha256;size:64;not null;uniqueIndex:uk_person_faces_raw_sha256" json:"rawImageSha256"`
	RawImageSize     int64                 `gorm:"column:raw_image_size;not null" json:"rawImageSize"`
	RawImageMime     string                `gorm:"column:raw_image_mime;size:32;not null" json:"rawImageMime"`
	AlignedFaceKey   string                `gorm:"column:aligned_face_key;size:255;not null" json:"alignedFaceKey"`
	AlignedFaceSize  int64                 `gorm:"column:aligned_face_size;not null" json:"alignedFaceSize"`
	AlignedFaceMime  string                `gorm:"column:aligned_face_mime;size:32;not null" json:"alignedFaceMime"`
}

// TableName 返回人员人脸样本表名。
func (PersonFace) TableName() string {
	return "person_faces"
}
