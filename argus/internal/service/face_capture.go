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

// FaceCaptureQuery 人脸抓拍记录分页查询入参。
type FaceCaptureQuery struct {
	Page          int        `form:"page"`
	PageSize      int        `form:"pageSize"`
	StartTime     *time.Time `form:"startTime"`
	EndTime       *time.Time `form:"endTime"`
	CameraID      string     `form:"cameraId"`
	TrackID       int64      `form:"trackId"`
	PersonID      string     `form:"personId"`
	PersonName    string     `form:"personName"`
	Status        string     `form:"status"` // "all", "recognized", "stranger"
	MinSimilarity *float32   `form:"minSimilarity"`
	MaxSimilarity *float32   `form:"maxSimilarity"`
}

// SnapshotItemView 单张抓拍快照前端展示结构。
type SnapshotItemView struct {
	SnapshotIndex    int32     `json:"snapshotIndex"`
	WallTimeNs       int64     `json:"wallTimeNs"`
	TimeSynced       bool      `json:"timeSynced"`
	ObservedAt       time.Time `json:"observedAt"`
	CapturedAt       string    `json:"capturedAt"`
	ImageID          string    `json:"imageId"`
	ImageRelPath     string    `json:"imageRelPath"`
	FaceImageID      string    `json:"faceImageId"`
	FaceImageRelPath string    `json:"faceImageRelPath"`
	PanoramaImageURL string    `json:"panoramaImageUrl"`
	FaceImageURL     string    `json:"faceImageUrl"`
	BBox             []float32 `json:"bbox"`
	FaceBBox         []float32 `json:"faceBbox"`
	QualityScore     float32   `json:"qualityScore"`
	Similarity       float32   `json:"similarity"`
	PersonID         string    `json:"personId"`
	PersonName       string    `json:"personName"`
}

// FaceCaptureItem 人脸抓拍记录前端返回视图。
type FaceCaptureItem struct {
	ID               uint64             `json:"id"`
	EventID          string             `json:"eventId"`
	InstanceID       string             `json:"instanceId"`
	CameraID         string             `json:"cameraId"`
	CameraName       string             `json:"cameraName"`
	AlgorithmID      string             `json:"algorithmId"`
	AlgorithmVersion string             `json:"algorithmVersion"`
	TrackID          int64              `json:"trackId"`
	BestSimilarity   float32            `json:"bestSimilarity"`
	BestQualityScore float32            `json:"bestQualityScore"`
	BestPersonID     string             `json:"bestPersonId"`
	BestPersonName   string             `json:"bestPersonName"`
	BestImageID      string             `json:"bestImageId"`
	BestImageRelPath string             `json:"bestImageRelPath"`
	BestFaceImageID  string             `json:"bestFaceImageId"`
	BestFaceRelPath  string             `json:"bestFaceRelPath"`
	BestBBox         []float32          `json:"bestBbox"`
	PanoramaImageURL string             `json:"panoramaImageUrl"`
	FaceImageURL     string             `json:"faceImageUrl"`
	BestPanoramaURL  string             `json:"bestPanoramaUrl"`
	BestFaceCropURL  string             `json:"bestFaceCropUrl"`
	IsStranger       bool               `json:"isStranger"`
	SnapshotCount    int32              `json:"snapshotCount"`
	Snapshots        []SnapshotItemView `json:"snapshots,omitempty"`
	FirstObservedAt  time.Time          `json:"firstObservedAt"`
	LastObservedAt   time.Time          `json:"lastObservedAt"`
	CreatedAt        time.Time          `json:"createdAt"`
}

// FaceCapturePageResult 人脸抓拍记录分页结果。
type FaceCapturePageResult struct {
	Items []FaceCaptureItem `json:"items"`
	Total int64             `json:"total"`
}

// FaceCaptureService 人脸抓拍业务接口。
type FaceCaptureService interface {
	ListPage(ctx context.Context, q *FaceCaptureQuery) (*FaceCapturePageResult, error)
	GetDetail(ctx context.Context, id uint64) (*FaceCaptureItem, error)
	ReadImageStream(ctx context.Context, id uint64, kind string, snapshotIndex int, isThumbnail bool) (io.ReadCloser, int64, string, error)
}

type faceCaptureService struct {
	captureRepo repository.FaceCaptureRepository
	camRepo     repository.CameraRepository
	imageDir    string
}

// NewFaceCaptureService 创建 FaceCaptureService 实例。
func NewFaceCaptureService(
	captureRepo repository.FaceCaptureRepository,
	camRepo repository.CameraRepository,
	cfg *config.Config,
) FaceCaptureService {
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

	return &faceCaptureService{captureRepo: captureRepo, camRepo: camRepo, imageDir: imageDir}
}

func (s *faceCaptureService) ListPage(ctx context.Context, q *FaceCaptureQuery) (*FaceCapturePageResult, error) {
	if q == nil {
		q = &FaceCaptureQuery{}
	}

	filter := &repository.FaceCaptureFilter{
		Page:          q.Page,
		PageSize:      q.PageSize,
		StartTime:     q.StartTime,
		EndTime:       q.EndTime,
		CameraID:      strings.TrimSpace(q.CameraID),
		TrackID:       q.TrackID,
		PersonID:      strings.TrimSpace(q.PersonID),
		PersonName:    strings.TrimSpace(q.PersonName),
		Status:        strings.TrimSpace(q.Status),
		MinSimilarity: q.MinSimilarity,
		MaxSimilarity: q.MaxSimilarity,
	}

	records, total, err := s.captureRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list face captures: %w", err)
	}

	items := make([]FaceCaptureItem, 0, len(records))
	for _, r := range records {
		item := s.toItem(&r, false)
		items = append(items, item)
	}

	return &FaceCapturePageResult{
		Items: items,
		Total: total,
	}, nil
}

func (s *faceCaptureService) GetDetail(ctx context.Context, id uint64) (*FaceCaptureItem, error) {
	record, err := s.captureRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, fmt.Errorf("get face capture: %w", err)
	}

	item := s.toItem(record, true)
	return &item, nil
}

func (s *faceCaptureService) ReadImageStream(
	ctx context.Context,
	id uint64,
	kind string,
	snapshotIndex int,
	isThumbnail bool,
) (io.ReadCloser, int64, string, error) {
	record, err := s.captureRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		return nil, 0, "", fmt.Errorf("get face capture: %w", err)
	}

	var relPath string
	if snapshotIndex > 0 {
		snapshots, pErr := record.ParseSnapshots()
		if pErr != nil {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		var found *model.SnapshotItem
		for i := range snapshots {
			if snapshots[i].SnapshotIndex == int32(snapshotIndex) {
				found = &snapshots[i]
				break
			}
		}
		if found == nil {
			return nil, 0, "", errno.NewError(errno.CodeNotFound)
		}
		if kind == "panorama" {
			relPath = found.ImageRelPath
		} else {
			relPath = found.FaceImageRelPath
		}
	} else {
		if kind == "panorama" {
			relPath = record.BestImageRelPath
		} else {
			relPath = record.BestFaceRelPath
		}
	}

	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	cleanRel := filepath.Clean(relPath)
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	// 候选基础目录：优先使用配置路径，其次检查各工作目录下的图片存储路径
	candidates := []string{s.imageDir, "var/images", "engine/var/images", "../engine/var/images"}
	return openImageFileWithFallback(candidates, relPath, isThumbnail)
}

func parseBBoxFromJSON(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	var sliceBBox []float32
	if err := json.Unmarshal(data, &sliceBBox); err == nil && len(sliceBBox) == 4 {
		return sliceBBox
	}
	var bboxMap map[string]float32
	if err := json.Unmarshal(data, &bboxMap); err == nil && len(bboxMap) > 0 {
		return []float32{
			bboxMap["x_min"],
			bboxMap["y_min"],
			bboxMap["x_max"],
			bboxMap["y_max"],
		}
	}
	return nil
}

func (s *faceCaptureService) toItem(r *model.FaceCapture, includeSnapshots bool) FaceCaptureItem {
	item := FaceCaptureItem{
		ID:               r.ID,
		EventID:          r.EventID,
		InstanceID:       r.InstanceID,
		CameraID:         r.CameraID,
		CameraName:       r.CameraName,
		AlgorithmID:      r.AlgorithmID,
		AlgorithmVersion: r.AlgorithmVersion,
		TrackID:          r.TrackID,
		BestSimilarity:   r.BestSimilarity,
		BestQualityScore: r.BestQualityScore,
		BestPersonID:     r.BestPersonID,
		BestPersonName:   r.BestPersonName,
		BestImageID:      r.BestImageID,
		BestImageRelPath: r.BestImageRelPath,
		BestFaceImageID:  r.BestFaceImageID,
		BestFaceRelPath:  r.BestFaceRelPath,
		BestBBox:         parseBBoxFromJSON(r.BestBBoxJSON),
		PanoramaImageURL: fmt.Sprintf("/api/record/captures/%d/panorama", r.ID),
		FaceImageURL:     fmt.Sprintf("/api/record/captures/%d/face", r.ID),
		BestPanoramaURL:  fmt.Sprintf("/api/record/captures/%d/panorama", r.ID),
		BestFaceCropURL:  fmt.Sprintf("/api/record/captures/%d/face", r.ID),
		IsStranger:       r.BestPersonID == "",
		SnapshotCount:    r.SnapshotCount,
		FirstObservedAt:  r.FirstObservedAt,
		LastObservedAt:   r.LastObservedAt,
		CreatedAt:        r.CreatedAt,
	}

	if includeSnapshots {
		rawSnapshots, err := r.ParseSnapshots()
		if err == nil {
			views := make([]SnapshotItemView, 0, len(rawSnapshots))
			for _, snap := range rawSnapshots {
				bbox := parseBBoxFromJSON(snap.BBoxJSON)
				views = append(views, SnapshotItemView{
					SnapshotIndex:    snap.SnapshotIndex,
					WallTimeNs:       snap.WallTimeNs,
					TimeSynced:       snap.TimeSynced,
					ObservedAt:       snap.ObservedAt,
					CapturedAt:       snap.ObservedAt.Format("2006-01-02 15:04:05"),
					ImageID:          snap.ImageID,
					ImageRelPath:     snap.ImageRelPath,
					FaceImageID:      snap.FaceImageID,
					FaceImageRelPath: snap.FaceImageRelPath,
					PanoramaImageURL: fmt.Sprintf("/api/record/captures/%d/snapshots/%d/panorama", r.ID, snap.SnapshotIndex),
					FaceImageURL:     fmt.Sprintf("/api/record/captures/%d/snapshots/%d/face", r.ID, snap.SnapshotIndex),
					BBox:             bbox,
					FaceBBox:         bbox,
					QualityScore:     snap.QualityScore,
					Similarity:       snap.Similarity,
					PersonID:         snap.PersonID,
					PersonName:       snap.PersonName,
				})
			}
			item.Snapshots = views
		}
	}

	return item
}
