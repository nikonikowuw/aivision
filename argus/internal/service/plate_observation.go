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

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// PlateObservationQuery 车牌过车记录分页查询入参。
type PlateObservationQuery struct {
	Page             int        `form:"page"`
	PageSize         int        `form:"pageSize"`
	StartTime        *time.Time `form:"startTime"`
	EndTime          *time.Time `form:"endTime"`
	CameraID         string     `form:"cameraId"`
	PlateText        string     `form:"plateText"`
	PlateColor       string     `form:"plateColor"`
	PlateType        string     `form:"plateType"`
	MinConfidence    *float32   `form:"minConfidence"`
	MaxConfidence    *float32   `form:"maxConfidence"`
	MinOcrConfidence *float32   `form:"minOcrConfidence"`
}

// PlateObservationItem 车牌抓拍过车记录前端返回视图。
type PlateObservationItem struct {
	ID               uint64    `json:"id"`
	EventID          string    `json:"eventId"`
	TaskID           string    `json:"taskId"`
	InstanceID       string    `json:"instanceId"`
	CameraID         string    `json:"cameraId"`
	CameraName       string    `json:"cameraName"`
	PlateText        string    `json:"plateText"`
	NormalizedText   string    `json:"normalizedText"`
	PlateColor       string    `json:"plateColor"`
	PlateType        string    `json:"plateType"`
	Confidence       float32   `json:"confidence"`
	OcrConfidence    float32   `json:"ocrConfidence"`
	TrackID          int64     `json:"trackId"`
	BBox             []float32 `json:"bbox"`
	VehicleBBox      []float32 `json:"vehicleBBox"`
	PanoramaImageURL string    `json:"panoramaImageUrl"`
	PlateImageURL    string    `json:"plateImageUrl"`
	ObservedAt       time.Time `json:"observedAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

// PlateObservationPageResult 分页查询结果。
type PlateObservationPageResult struct {
	Items []PlateObservationItem `json:"items"`
	Total int64                  `json:"total"`
}

// PlateObservationService 车牌抓拍过车业务接口。
type PlateObservationService interface {
	// ListPage 分页组合查询车牌过车记录。
	ListPage(ctx context.Context, q *PlateObservationQuery) (*PlateObservationPageResult, error)
	// GetDetail 查询单条车牌过车详情。
	GetDetail(ctx context.Context, id uint64) (*PlateObservationItem, error)
	// ReadImageStream 安全读取受控车牌图片（panorama 或 plate）。
	ReadImageStream(ctx context.Context, id uint64, kind string) (io.ReadCloser, int64, string, error)
}

type plateObservationService struct {
	plateRepo repository.PlateObservationRepository
	camRepo   repository.CameraRepository
	imageDir  string
}

// NewPlateObservationService 创建 PlateObservationService 实例。
func NewPlateObservationService(
	plateRepo repository.PlateObservationRepository,
	camRepo repository.CameraRepository,
	cfg *config.Config,
) PlateObservationService {
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

	return &plateObservationService{
		plateRepo: plateRepo,
		camRepo:   camRepo,
		imageDir:  imageDir,
	}
}

func (s *plateObservationService) ListPage(ctx context.Context, q *PlateObservationQuery) (*PlateObservationPageResult, error) {
	if q == nil {
		q = &PlateObservationQuery{}
	}

	filter := &repository.PlateObservationFilter{
		Page:             q.Page,
		PageSize:         q.PageSize,
		StartTime:        q.StartTime,
		EndTime:          q.EndTime,
		CameraID:         strings.TrimSpace(q.CameraID),
		PlateText:        strings.TrimSpace(q.PlateText),
		PlateColor:       strings.TrimSpace(q.PlateColor),
		PlateType:        strings.TrimSpace(q.PlateType),
		MinConfidence:    q.MinConfidence,
		MaxConfidence:    q.MaxConfidence,
		MinOcrConfidence: q.MinOcrConfidence,
	}

	records, total, err := s.plateRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list plate observations: %w", err)
	}

	// 补充摄像头名称
	cameraMap := make(map[string]string)
	if len(records) > 0 && s.camRepo != nil {
		if cams, err := s.camRepo.ListAll(ctx); err == nil {
			for _, c := range cams {
				cameraMap[c.CameraID] = c.Name
			}
		}
	}

	items := make([]PlateObservationItem, 0, len(records))
	for _, r := range records {
		camName := r.CameraName
		if name, ok := cameraMap[r.CameraID]; ok && name != "" {
			camName = name
		}

		var bbox []float32
		if len(r.BBoxJSON) > 0 {
			_ = json.Unmarshal(r.BBoxJSON, &bbox)
		}
		var vehicleBBox []float32
		if len(r.VehicleBBoxJSON) > 0 {
			_ = json.Unmarshal(r.VehicleBBoxJSON, &vehicleBBox)
		}

		panoURL := ""
		if r.PanoramaImage != "" {
			panoURL = fmt.Sprintf("/api/v1/plate-observations/%d/panorama", r.ID)
		}
		plateURL := ""
		if r.PlateImage != "" {
			plateURL = fmt.Sprintf("/api/v1/plate-observations/%d/plate", r.ID)
		}

		items = append(items, PlateObservationItem{
			ID:               r.ID,
			EventID:          r.EventID,
			TaskID:           r.TaskID,
			InstanceID:       r.InstanceID,
			CameraID:         r.CameraID,
			CameraName:       camName,
			PlateText:        r.PlateText,
			NormalizedText:   r.NormalizedText,
			PlateColor:       r.PlateColor,
			PlateType:        r.PlateType,
			Confidence:       r.Confidence,
			OcrConfidence:    r.OcrConfidence,
			TrackID:          r.TrackID,
			BBox:             bbox,
			VehicleBBox:      vehicleBBox,
			PanoramaImageURL: panoURL,
			PlateImageURL:    plateURL,
			ObservedAt:       r.ObservedAt,
			CreatedAt:        r.CreatedAt,
		})
	}

	return &PlateObservationPageResult{
		Items: items,
		Total: total,
	}, nil
}

func (s *plateObservationService) GetDetail(ctx context.Context, id uint64) (*PlateObservationItem, error) {
	record, err := s.plateRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}

	camName := record.CameraName
	if record.CameraID != "" && s.camRepo != nil {
		if cam, err := s.camRepo.GetByCameraID(ctx, record.CameraID); err == nil && cam != nil {
			camName = cam.Name
		}
	}

	var bbox []float32
	if len(record.BBoxJSON) > 0 {
		_ = json.Unmarshal(record.BBoxJSON, &bbox)
	}
	var vehicleBBox []float32
	if len(record.VehicleBBoxJSON) > 0 {
		_ = json.Unmarshal(record.VehicleBBoxJSON, &vehicleBBox)
	}

	panoURL := ""
	if record.PanoramaImage != "" {
		panoURL = fmt.Sprintf("/api/v1/plate-observations/%d/panorama", record.ID)
	}
	plateURL := ""
	if record.PlateImage != "" {
		plateURL = fmt.Sprintf("/api/v1/plate-observations/%d/plate", record.ID)
	}

	return &PlateObservationItem{
		ID:               record.ID,
		EventID:          record.EventID,
		TaskID:           record.TaskID,
		InstanceID:       record.InstanceID,
		CameraID:         record.CameraID,
		CameraName:       camName,
		PlateText:        record.PlateText,
		NormalizedText:   record.NormalizedText,
		PlateColor:       record.PlateColor,
		PlateType:        record.PlateType,
		Confidence:       record.Confidence,
		OcrConfidence:    record.OcrConfidence,
		TrackID:          record.TrackID,
		BBox:             bbox,
		VehicleBBox:      vehicleBBox,
		PanoramaImageURL: panoURL,
		PlateImageURL:    plateURL,
		ObservedAt:       record.ObservedAt,
		CreatedAt:        record.CreatedAt,
	}, nil
}

func (s *plateObservationService) ReadImageStream(ctx context.Context, id uint64, kind string) (io.ReadCloser, int64, string, error) {
	record, err := s.plateRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", err
	}

	var relPath string
	if kind == "plate" {
		relPath = record.PlateImage
	} else {
		relPath = record.PanoramaImage
	}

	if strings.TrimSpace(relPath) == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	// 路径安全校验
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

	f, err := os.Open(cleanTarget)
	if err != nil {
		return nil, 0, "", err
	}

	return f, fi.Size(), "image/jpeg", nil
}
