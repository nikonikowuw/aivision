package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// CaptureQuery 通用抓拍记录分页查询入参。
type CaptureQuery struct {
	Page          int        `form:"page"`
	PageSize      int        `form:"pageSize"`
	StartTime     *time.Time `form:"startTime"`
	EndTime       *time.Time `form:"endTime"`
	TargetType    string     `form:"targetType"`
	CameraID      string     `form:"cameraId"`
	TrackID       int64      `form:"trackId"`
	Keyword       string     `form:"keyword"`
	IsRecognized  *bool      `form:"isRecognized"`
	MinQuality    *float32   `form:"minQuality"`
	MaxQuality    *float32   `form:"maxQuality"`
	MinConfidence *float32   `form:"minConfidence"`
	MaxConfidence *float32   `form:"maxConfidence"`
}

// CaptureItem 通用抓拍记录前端返回视图。
type CaptureItem struct {
	ID                  uint64         `json:"id"`
	EventID             string         `json:"eventId"`
	InstanceID          string         `json:"instanceId"`
	TargetType          string         `json:"targetType"`
	CameraID            string         `json:"cameraId"`
	CameraName          string         `json:"cameraName"`
	TaskID              uint64         `json:"taskId"`
	AlgorithmID         string         `json:"algorithmId"`
	AlgorithmVersion    string         `json:"algorithmVersion"`
	TrackID             int64          `json:"trackId"`
	Confidence          float32        `json:"confidence"`
	QualityScore        float32        `json:"qualityScore"`
	BBox                []float32      `json:"bbox"`
	SubBBox             []float32      `json:"subBbox,omitempty"`
	ImageID             string         `json:"imageId"`
	ImageRelPath        string         `json:"imageRelPath"`
	CropImageID         string         `json:"cropImageId"`
	CropImageRelPath    string         `json:"cropImageRelPath"`
	SubCropImageID      string         `json:"subCropImageId"`
	SubCropImageRelPath string         `json:"subCropImageRelPath"`
	ImageURL            string         `json:"imageUrl,omitempty"`
	CropImageURL        string         `json:"cropImageUrl,omitempty"`
	SubCropImageURL     string         `json:"subCropImageUrl,omitempty"`
	IsRecognized        bool           `json:"isRecognized"`
	Attributes          map[string]any `json:"attributes"`
	CapturedAt          time.Time      `json:"capturedAt"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

// CapturePageResult 通用抓拍记录分页结果。
type CapturePageResult struct {
	Items []CaptureItem `json:"items"`
	Total int64         `json:"total"`
}

// CaptureService 通用抓拍记录业务接口。
type CaptureService interface {
	ListPage(ctx context.Context, q *CaptureQuery) (*CapturePageResult, error)
	GetDetail(ctx context.Context, id uint64) (*CaptureItem, error)
	ReadImageStream(ctx context.Context, id uint64, kind string, isThumbnail bool) (io.ReadCloser, int64, string, error)
}

type captureService struct {
	captureRepo repository.CaptureRepository
	imageDir    string
}

// NewCaptureService 创建通用抓拍记录服务。
func NewCaptureService(captureRepo repository.CaptureRepository, _ *config.Config) CaptureService {
	imageDir := "var/images"
	if envDir := os.Getenv("AIVISION_IMAGE_DIR"); envDir != "" {
		imageDir = envDir
	} else if _, err := os.Stat("var/images"); os.IsNotExist(err) {
		if _, err := os.Stat("engine/var/images"); err == nil {
			imageDir = "engine/var/images"
		} else if _, err := os.Stat("../engine/var/images"); err == nil {
			imageDir = "../engine/var/images"
		}
	}
	return &captureService{captureRepo: captureRepo, imageDir: imageDir}
}

func (s *captureService) ListPage(ctx context.Context, q *CaptureQuery) (*CapturePageResult, error) {
	var query CaptureQuery
	if q != nil {
		query = *q
	}
	filter := &repository.CaptureFilter{
		Page:          query.Page,
		PageSize:      query.PageSize,
		StartTime:     query.StartTime,
		EndTime:       query.EndTime,
		TargetType:    strings.TrimSpace(query.TargetType),
		CameraID:      strings.TrimSpace(query.CameraID),
		TrackID:       query.TrackID,
		Keyword:       strings.TrimSpace(query.Keyword),
		IsRecognized:  query.IsRecognized,
		MinQuality:    query.MinQuality,
		MaxQuality:    query.MaxQuality,
		MinConfidence: query.MinConfidence,
		MaxConfidence: query.MaxConfidence,
	}

	records, total, err := s.captureRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list captures: %w", err)
	}
	items := make([]CaptureItem, 0, len(records))
	for i := range records {
		items = append(items, s.toItem(&records[i]))
	}
	return &CapturePageResult{Items: items, Total: total}, nil
}

func (s *captureService) GetDetail(ctx context.Context, id uint64) (*CaptureItem, error) {
	record, err := s.captureRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, fmt.Errorf("get capture: %w", err)
	}
	item := s.toItem(record)
	return &item, nil
}

func (s *captureService) ReadImageStream(ctx context.Context, id uint64, kind string, isThumbnail bool) (io.ReadCloser, int64, string, error) {
	record, err := s.captureRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", fmt.Errorf("get capture: %w", err)
	}

	var relPath string
	switch kind {
	case "panorama":
		relPath = record.ImageRelPath
	case "crop":
		relPath = record.CropImageRelPath
	case "sub_crop":
		relPath = record.SubCropImageRelPath
	default:
		return nil, 0, "", errno.NewError(errno.CodeInvalidParam)
	}
	if strings.TrimSpace(relPath) == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}
	return openImageFileWithFallback(
		[]string{s.imageDir, "var/images", "engine/var/images", "../engine/var/images"},
		relPath,
		isThumbnail,
	)
}

func (s *captureService) toItem(record *model.CaptureRecord) CaptureItem {
	attributes, err := record.ParseAttributes()
	if err != nil || attributes == nil {
		attributes = map[string]any{}
	}
	bbox, _ := record.ParseBBox(false)
	subBBox, _ := record.ParseBBox(true)

	item := CaptureItem{
		ID:                  record.ID,
		EventID:             record.EventID,
		InstanceID:          record.InstanceID,
		TargetType:          record.TargetType,
		CameraID:            record.CameraID,
		CameraName:          record.CameraName,
		TaskID:              record.TaskID,
		AlgorithmID:         record.AlgorithmID,
		AlgorithmVersion:    record.AlgorithmVersion,
		TrackID:             record.TrackID,
		Confidence:          record.Confidence,
		QualityScore:        record.QualityScore,
		BBox:                bbox,
		SubBBox:             subBBox,
		ImageID:             record.ImageID,
		ImageRelPath:        record.ImageRelPath,
		CropImageID:         record.CropImageID,
		CropImageRelPath:    record.CropImageRelPath,
		SubCropImageID:      record.SubCropImageID,
		SubCropImageRelPath: record.SubCropImageRelPath,
		IsRecognized:        record.IsRecognized,
		Attributes:          attributes,
		CapturedAt:          record.CapturedAt,
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
	if record.ImageRelPath != "" {
		item.ImageURL = fmt.Sprintf("/api/record/captures/%d/image?kind=panorama", record.ID)
	}
	if record.CropImageRelPath != "" {
		item.CropImageURL = fmt.Sprintf("/api/record/captures/%d/image?kind=crop", record.ID)
	}
	if record.SubCropImageRelPath != "" {
		item.SubCropImageURL = fmt.Sprintf("/api/record/captures/%d/image?kind=sub_crop", record.ID)
	}
	return item
}
