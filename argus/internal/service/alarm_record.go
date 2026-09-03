package service

import (
	"context"
	"encoding/json"
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

// AlarmRecordQuery 告警记录分页查询入参。
type AlarmRecordQuery struct {
	Page          int        `form:"page"`
	PageSize      int        `form:"pageSize"`
	StartTime     *time.Time `form:"startTime"`
	EndTime       *time.Time `form:"endTime"`
	CameraID      string     `form:"cameraId"`
	AlgorithmID   string     `form:"algorithmId"`
	AlarmTypeID   string     `form:"alarmTypeId"`
	TargetLabel   string     `form:"targetLabel"`
	MinConfidence *float32   `form:"minConfidence"`
	MaxConfidence *float32   `form:"maxConfidence"`
}

// AlarmRecordItem 告警记录前端返回视图（单目标单记录 1 Target = 1 Record）。
type AlarmRecordItem struct {
	ID               uint64    `json:"id"`
	EventID          string    `json:"eventId"`
	InstanceID       string    `json:"instanceId"`
	CameraID         string    `json:"cameraId"`
	CameraName       string    `json:"cameraName"`
	AlgorithmID      string    `json:"algorithmId"`
	AlgorithmName    string    `json:"algorithmName"`
	AlgorithmVersion string    `json:"algorithmVersion"`
	AlarmTypeID      string    `json:"alarmTypeId"`
	OccurredAt       time.Time `json:"occurredAt"`
	TimeSynced       bool      `json:"timeSynced"`
	TargetLabel      string    `json:"targetLabel"`
	Confidence       float32   `json:"confidence"`
	TrackID          int64     `json:"trackId"`
	BBox             []float32 `json:"bbox"` // [x1, y1, x2, y2]
	ImageID          string    `json:"imageId"`
	ImageURL         string    `json:"imageUrl"`
	CreatedAt        time.Time `json:"createdAt"`
}

// AlarmRecordDetail 告警记录详情视图（含检测规则）。
type AlarmRecordDetail struct {
	AlarmRecordItem
	Rules []model.DetectionRule `json:"rules"`
}

// AlarmRecordPageResult 告警记录分页查询结果。
type AlarmRecordPageResult struct {
	Items []AlarmRecordItem `json:"items"`
	Total int64             `json:"total"`
}

// AlarmRecordService 告警记录业务接口。
type AlarmRecordService interface {
	// ListPage 分页组合查询告警记录。
	ListPage(ctx context.Context, q *AlarmRecordQuery) (*AlarmRecordPageResult, error)
	// GetDetail 查询告警记录详情（包含所属实例规则快照）。
	GetDetail(ctx context.Context, id uint64) (*AlarmRecordDetail, error)
	// ReadImageStream 安全读取受控告警图片（基于 image_id 校验防穿越，支持缩略图与原图切换）。
	ReadImageStream(ctx context.Context, imageID string, isThumbnail bool) (io.ReadCloser, int64, string, error)
}

type alarmRecordService struct {
	alarmRepo repository.AlarmRecordRepository
	camRepo   repository.CameraRepository
	algoRepo  repository.AlgorithmRepository
	taskRepo  repository.TaskRepository
	imageDir  string
}

// NewAlarmRecordService 创建 AlarmRecordService 实例。
func NewAlarmRecordService(
	alarmRepo repository.AlarmRecordRepository,
	camRepo repository.CameraRepository,
	algoRepo repository.AlgorithmRepository,
	taskRepo repository.TaskRepository,
	cfg *config.Config,
) AlarmRecordService {
	imageDir := "var/images"
	if envDir := os.Getenv("AIVISION_IMAGE_DIR"); envDir != "" {
		imageDir = envDir
	} else if _, err := os.Stat("var/images"); os.IsNotExist(err) {
		// 在根目录或 app/ 目录下启动时的自适应探测
		if _, err := os.Stat("engine/var/images"); err == nil {
			imageDir = "engine/var/images"
		} else if _, err := os.Stat("../engine/var/images"); err == nil {
			imageDir = "../engine/var/images"
		}
	}
	return &alarmRecordService{
		alarmRepo: alarmRepo,
		camRepo:   camRepo,
		algoRepo:  algoRepo,
		taskRepo:  taskRepo,
		imageDir:  imageDir,
	}
}

func (s *alarmRecordService) ListPage(ctx context.Context, q *AlarmRecordQuery) (*AlarmRecordPageResult, error) {
	var query AlarmRecordQuery
	if q != nil {
		query = *q
	}
	filter := &repository.AlarmRecordFilter{
		Page:          query.Page,
		PageSize:      query.PageSize,
		StartTime:     query.StartTime,
		EndTime:       query.EndTime,
		CameraID:      query.CameraID,
		AlgorithmID:   query.AlgorithmID,
		AlarmTypeID:   query.AlarmTypeID,
		TargetLabel:   strings.TrimSpace(query.TargetLabel),
		MinConfidence: query.MinConfidence,
		MaxConfidence: query.MaxConfidence,
	}

	records, total, err := s.alarmRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 预加载摄像头与算法字典（避免 N+1 慢查询）
	camNames := make(map[string]string)
	if s.camRepo != nil {
		if cams, err := s.camRepo.ListAll(ctx); err == nil {
			for _, cam := range cams {
				camNames[cam.CameraID] = cam.Name
			}
		}
	}

	algoNames := make(map[string]string)
	if s.algoRepo != nil {
		if algos, _, err := s.algoRepo.ListAlgorithms(ctx, nil); err == nil {
			for _, algo := range algos {
				algoNames[algo.AlgorithmID] = algo.Name
			}
		}
	}

	items := make([]AlarmRecordItem, 0, len(records))
	for _, rec := range records {
		camName := rec.CameraID
		if name, ok := camNames[rec.CameraID]; ok && name != "" {
			camName = name
		}

		algoName := rec.AlgorithmID
		if name, ok := algoNames[rec.AlgorithmID]; ok && name != "" {
			algoName = name
		}

		var bbox []float32
		if len(rec.BBoxJSON) > 0 {
			_ = json.Unmarshal(rec.BBoxJSON, &bbox)
		}

		imageURL := ""
		if rec.ImageID != "" {
			imageURL = fmt.Sprintf("/api/record/images/%s", rec.ImageID)
		}

		items = append(items, AlarmRecordItem{
			ID:               rec.ID,
			EventID:          rec.EventID,
			InstanceID:       rec.InstanceID,
			CameraID:         rec.CameraID,
			CameraName:       camName,
			AlgorithmID:      rec.AlgorithmID,
			AlgorithmName:    algoName,
			AlgorithmVersion: rec.AlgorithmVersion,
			AlarmTypeID:      rec.AlarmTypeID,
			OccurredAt:       rec.OccurredAt,
			TimeSynced:       rec.TimeSynced,
			TargetLabel:      rec.TargetLabel,
			Confidence:       rec.Confidence,
			TrackID:          rec.TrackID,
			BBox:             bbox,
			ImageID:          rec.ImageID,
			ImageURL:         imageURL,
			CreatedAt:        rec.CreatedAt,
		})
	}

	return &AlarmRecordPageResult{
		Items: items,
		Total: total,
	}, nil
}

func (s *alarmRecordService) GetDetail(ctx context.Context, id uint64) (*AlarmRecordDetail, error) {
	rec, err := s.alarmRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}

	cameraName := rec.CameraID
	if s.camRepo != nil {
		if cam, err := s.camRepo.GetByCameraID(ctx, rec.CameraID); err == nil && cam != nil {
			cameraName = cam.Name
		}
	}

	algoName := rec.AlgorithmID
	if s.algoRepo != nil {
		if algo, err := s.algoRepo.GetAlgorithmByID(ctx, rec.AlgorithmID); err == nil && algo != nil {
			algoName = algo.Name
		}
	}

	var bbox []float32
	if len(rec.BBoxJSON) > 0 {
		_ = json.Unmarshal(rec.BBoxJSON, &bbox)
	}

	var rules []model.DetectionRule
	if s.taskRepo != nil && rec.InstanceID != "" {
		if inst, err := s.taskRepo.GetInstance(ctx, rec.InstanceID); err == nil && inst != nil {
			if len(inst.RulesJSON) > 0 {
				_ = json.Unmarshal(inst.RulesJSON, &rules)
			}
		}
	}

	imageURL := ""
	if rec.ImageID != "" {
		imageURL = fmt.Sprintf("/api/record/images/%s", rec.ImageID)
	}

	return &AlarmRecordDetail{
		AlarmRecordItem: AlarmRecordItem{
			ID:               rec.ID,
			EventID:          rec.EventID,
			InstanceID:       rec.InstanceID,
			CameraID:         rec.CameraID,
			CameraName:       cameraName,
			AlgorithmID:      rec.AlgorithmID,
			AlgorithmName:    algoName,
			AlgorithmVersion: rec.AlgorithmVersion,
			AlarmTypeID:      rec.AlarmTypeID,
			OccurredAt:       rec.OccurredAt,
			TimeSynced:       rec.TimeSynced,
			TargetLabel:      rec.TargetLabel,
			Confidence:       rec.Confidence,
			TrackID:          rec.TrackID,
			BBox:             bbox,
			ImageID:          rec.ImageID,
			ImageURL:         imageURL,
			CreatedAt:        rec.CreatedAt,
		},
		Rules: rules,
	}, nil
}

func (s *alarmRecordService) ReadImageStream(ctx context.Context, imageID string, isThumbnail bool) (io.ReadCloser, int64, string, error) {
	if strings.TrimSpace(imageID) == "" {
		return nil, 0, "", errno.NewError(errno.CodeInvalidParam)
	}

	rec, err := s.alarmRepo.GetByImageID(ctx, imageID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", err
	}

	if rec.ImageRelPath == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	candidates := []string{s.imageDir, "var/images", "engine/var/images", "../engine/var/images"}
	return openImageFileWithFallback(candidates, rec.ImageRelPath, isThumbnail)
}
