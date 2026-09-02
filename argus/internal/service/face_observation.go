package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// FaceObservationQuery 人脸识别记录分页查询入参。
type FaceObservationQuery struct {
	Page          int        `form:"page"`
	PageSize      int        `form:"pageSize"`
	StartTime     *time.Time `form:"startTime"`
	EndTime       *time.Time `form:"endTime"`
	CameraID      string     `form:"cameraId"`
	PersonID      string     `form:"personId"`
	PersonName    string     `form:"personName"`
	MinSimilarity *float32   `form:"minSimilarity"`
	MaxSimilarity *float32   `form:"maxSimilarity"`
}

// FaceObservationItem 人脸识别记录前端返回视图。
// 不包含 embedding 等敏感生物特征数据。
type FaceObservationItem struct {
	ID               uint64    `json:"id"`
	EventID          string    `json:"eventId"`
	InstanceID       string    `json:"instanceId"`
	CameraID         string    `json:"cameraId"`
	CameraName       string    `json:"cameraName"`
	AlgorithmID      string    `json:"algorithmId"`
	AlgorithmVersion string    `json:"algorithmVersion"`
	TimeSynced       bool      `json:"timeSynced"`
	TrackID          int64     `json:"trackId"`
	FaceID           string    `json:"faceId"`
	PersonID         string    `json:"personId"`
	PersonName       string    `json:"personName"`
	Similarity       float32   `json:"similarity"`
	BBox             []float32 `json:"bbox"`
	PanoramaImageURL string    `json:"panoramaImageUrl"`
	FaceImageURL     string    `json:"faceImageUrl"`
	ImageID          string    `json:"imageId"`
	ImageRelPath     string    `json:"imageRelPath"`
	FaceImageID      string    `json:"faceImageId"`
	FaceImageRelPath string    `json:"faceImageRelPath"`
	ObservedAt       time.Time `json:"observedAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

// FaceObservationPageResult 人脸识别记录分页查询结果。
type FaceObservationPageResult struct {
	Items []FaceObservationItem `json:"items"`
	Total int64                 `json:"total"`
}

// FaceObservationService 人脸识别记录业务接口。
type FaceObservationService interface {
	ListPage(ctx context.Context, q *FaceObservationQuery) (*FaceObservationPageResult, error)
	GetDetail(ctx context.Context, id uint64) (*FaceObservationItem, error)
	ReadImageStream(ctx context.Context, id uint64, kind string) (io.ReadCloser, int64, string, error)
}

type faceObservationService struct {
	faceRepo repository.FaceObservationRepository
	camRepo  repository.CameraRepository
	imageDir string
}

// NewFaceObservationService 创建 FaceObservationService 实例。
func NewFaceObservationService(
	faceRepo repository.FaceObservationRepository,
	camRepo repository.CameraRepository,
	cfg *config.Config,
) FaceObservationService {
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

	return &faceObservationService{faceRepo: faceRepo, camRepo: camRepo, imageDir: imageDir}
}

func (s *faceObservationService) ListPage(ctx context.Context, q *FaceObservationQuery) (*FaceObservationPageResult, error) {
	if q == nil {
		q = &FaceObservationQuery{}
	}

	filter := &repository.FaceObservationFilter{
		Page:          q.Page,
		PageSize:      q.PageSize,
		StartTime:     q.StartTime,
		EndTime:       q.EndTime,
		CameraID:      strings.TrimSpace(q.CameraID),
		PersonID:      strings.TrimSpace(q.PersonID),
		PersonName:    strings.TrimSpace(q.PersonName),
		MinSimilarity: q.MinSimilarity,
		MaxSimilarity: q.MaxSimilarity,
	}

	records, total, err := s.faceRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list face observations: %w", err)
	}

	cameraMap := make(map[string]string)
	if len(records) > 0 && s.camRepo != nil {
		if cams, err := s.camRepo.ListAll(ctx); err == nil {
			for _, c := range cams {
				cameraMap[c.CameraID] = c.Name
			}
		}
	}

	items := make([]FaceObservationItem, 0, len(records))
	for _, record := range records {
		items = append(items, s.toItem(record, cameraMap[record.CameraID]))
	}
	return &FaceObservationPageResult{Items: items, Total: total}, nil
}

func (s *faceObservationService) GetDetail(ctx context.Context, id uint64) (*FaceObservationItem, error) {
	record, err := s.faceRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}

	cameraName := ""
	if s.camRepo != nil && record.CameraID != "" {
		if camera, err := s.camRepo.GetByCameraID(ctx, record.CameraID); err == nil && camera != nil {
			cameraName = camera.Name
		}
	}
	item := s.toItem(*record, cameraName)
	return &item, nil
}

func (s *faceObservationService) toItem(record model.FaceObservation, currentCameraName string) FaceObservationItem {
	cameraName := record.CameraName
	if cameraName == "" {
		cameraName = currentCameraName
	}

	var bbox []float32
	if len(record.BBoxJSON) > 0 {
		_ = json.Unmarshal(record.BBoxJSON, &bbox)
	}

	panoramaURL := ""
	if record.ImageRelPath != "" {
		panoramaURL = fmt.Sprintf("/api/record/faces/%d/panorama", record.ID)
	}
	faceURL := ""
	if record.FaceImageRelPath != "" {
		faceURL = fmt.Sprintf("/api/record/faces/%d/face", record.ID)
	}

	return FaceObservationItem{
		ID:               record.ID,
		EventID:          record.EventID,
		InstanceID:       record.InstanceID,
		CameraID:         record.CameraID,
		CameraName:       cameraName,
		AlgorithmID:      record.AlgorithmID,
		AlgorithmVersion: record.AlgorithmVersion,
		TimeSynced:       record.TimeSynced,
		TrackID:          record.TrackID,
		FaceID:           record.FaceID,
		PersonID:         record.PersonID,
		PersonName:       record.PersonName,
		Similarity:       record.Similarity,
		BBox:             bbox,
		PanoramaImageURL: panoramaURL,
		FaceImageURL:     faceURL,
		ImageID:          record.ImageID,
		ImageRelPath:     record.ImageRelPath,
		FaceImageID:      record.FaceImageID,
		FaceImageRelPath: record.FaceImageRelPath,
		ObservedAt:       record.ObservedAt,
		CreatedAt:        record.CreatedAt,
	}
}

func (s *faceObservationService) ReadImageStream(ctx context.Context, id uint64, kind string) (io.ReadCloser, int64, string, error) {
	record, err := s.faceRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", err
	}

	var relPath string
	switch kind {
	case "panorama":
		relPath = record.ImageRelPath
	case "face":
		relPath = record.FaceImageRelPath
	default:
		return nil, 0, "", errno.NewError(errno.CodeInvalidParam)
	}
	if strings.TrimSpace(relPath) == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	cleanRel := filepath.Clean(relPath)
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}
	absBase, err := filepath.Abs(s.imageDir)
	if err != nil {
		return nil, 0, "", fmt.Errorf("resolve image dir: %w", err)
	}
	targetPath := filepath.Join(absBase, cleanRel)
	cleanTarget := filepath.Clean(targetPath)
	if !strings.HasPrefix(cleanTarget, absBase+string(filepath.Separator)) && cleanTarget != absBase {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	fi, err := os.Stat(cleanTarget)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", err
	}
	file, err := os.Open(cleanTarget)
	if err != nil {
		return nil, 0, "", err
	}
	return file, fi.Size(), "image/jpeg", nil
}
