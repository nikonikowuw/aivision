package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// CameraProbeTimeout 测活整体硬 deadline：C++ 每种传输方式 5 秒（TCP→UDP 最多约 10 秒），
// 这里留出传输与调度余量。
const CameraProbeTimeout = 12 * time.Second

// SaveCameraInput 新增/修改摄像头入参。
type SaveCameraInput struct {
	Name       string `json:"name" binding:"required,max=128"`
	RtspURL    string `json:"rtspUrl" binding:"required,max=2048"`
	SubRtspURL string `json:"subRtspUrl" binding:"omitempty,max=2048"`
	Remark     string `json:"remark" binding:"omitempty,max=255"`
}

// CameraPageQuery 摄像头分页查询参数。
type CameraPageQuery struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"pageSize" binding:"omitempty,min=1,max=100"`
	Name     string `form:"name"`
}

// CameraPageResult 摄像头分页查询结果。
type CameraPageResult struct {
	Items []*model.Camera `json:"items"`
	Total int64           `json:"total"`
}

// ProbeCameraRequest 测活请求（API handler 解析后的 DTO）。
type ProbeCameraRequest struct {
	ID       uint64 // 可选：已保存摄像头的数值 id；0 表示未保存表单
	Protocol string // 当前固定 "rtsp"
	RtspURL  string
}

// ProbeAttempt 单次传输方式尝试结果。
type ProbeAttempt struct {
	Transport   string `json:"transport"`   // tcp | udp
	ElapsedMS   uint64 `json:"elapsedMs"`   // 该次尝试耗时
	FailureCode string `json:"failureCode"` // 空串表示该次成功
}

// ProbeCameraResult 测活结构化结果（无论测活成功失败均 code=0）。
type ProbeCameraResult struct {
	Status            string         `json:"status"` // success | failed
	FailureCode       string         `json:"failureCode"`
	FailureMessage    string         `json:"failureMessage"`
	Attempts          []ProbeAttempt `json:"attempts"`
	SelectedTransport string         `json:"selectedTransport"` // 实际成功传输方式（成功时有值）
	Codec             string         `json:"codec"`
	Width             uint32         `json:"width"`
	Height            uint32         `json:"height"`
	FPS               float64        `json:"fps"`
	ElapsedMS         uint64         `json:"elapsedMs"`
	Persisted         bool           `json:"persisted"` // 是否已写入测活元数据
	Stale             bool           `json:"stale"`     // 配置指纹不一致，结果不适用于当前配置
}

// CameraLiveStreamResult 实时取流结果
type CameraLiveStreamResult struct {
	StreamPath string `json:"streamPath"`
	HTTPPort   int32  `json:"httpPort"`
	WSPort     int32  `json:"wsPort"`
	HTTPURL    string `json:"httpUrl"`
	WSURL      string `json:"wsUrl"`
	StreamType string `json:"streamType"`
}

// CameraProbeClient 摄像头测活与预览所需的 Engine 客户端窄接口（便于测试注入替身）。
type CameraProbeClient interface {
	ProbeCamera(ctx context.Context, req *argusv1.ProbeCameraRequest, opts ...grpc.CallOption) (*argusv1.ProbeCameraResponse, error)
	StartCameraPreview(ctx context.Context, req *argusv1.StartCameraPreviewRequest, opts ...grpc.CallOption) (*argusv1.StartCameraPreviewResponse, error)
	StopCameraPreview(ctx context.Context, req *argusv1.StopCameraPreviewRequest, opts ...grpc.CallOption) (*argusv1.StopCameraPreviewResponse, error)
}

// CameraService 摄像头视频源管理业务接口。
type CameraService interface {
	GetPage(ctx context.Context, query *CameraPageQuery) (*CameraPageResult, error)
	CreateCamera(ctx context.Context, input *SaveCameraInput) (*model.Camera, error)
	UpdateCamera(ctx context.Context, id uint64, input *SaveCameraInput) (*model.Camera, error)
	DeleteCamera(ctx context.Context, id uint64) error
	BatchDeleteCamera(ctx context.Context, ids []uint64) error
	ProbeCamera(ctx context.Context, req *ProbeCameraRequest) (*ProbeCameraResult, error)
	StartLivePreview(ctx context.Context, id uint64, streamType string) (*CameraLiveStreamResult, error)
	StopLivePreview(ctx context.Context, id uint64, streamType string) error
}

type cameraService struct {
	repo     repository.CameraRepository
	taskRepo repository.TaskRepository
	registry *ProtocolRegistry
	engine   CameraProbeClient
}

// NewCameraService 创建 CameraService 实例。
func NewCameraService(repo repository.CameraRepository, taskRepo repository.TaskRepository, engine CameraProbeClient) CameraService {
	return &cameraService{
		repo:     repo,
		taskRepo: taskRepo,
		registry: NewProtocolRegistry(),
		engine:   engine,
	}
}

func (s *cameraService) GetPage(ctx context.Context, query *CameraPageQuery) (*CameraPageResult, error) {
	items, total, err := s.repo.ListPage(ctx, &repository.CameraFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		Name:     strings.TrimSpace(query.Name),
	})
	if err != nil {
		return nil, err
	}
	pointers := make([]*model.Camera, 0, len(items))
	for i := range items {
		pointers = append(pointers, &items[i])
	}
	return &CameraPageResult{Items: pointers, Total: total}, nil
}

func (s *cameraService) CreateCamera(ctx context.Context, input *SaveCameraInput) (*model.Camera, error) {
	if err := s.normalizeAndValidate(input); err != nil {
		return nil, err
	}
	camera := &model.Camera{
		CameraID:        uuid.NewString(),
		Protocol:        model.CameraProtocolRTSP,
		Name:            input.Name,
		RtspURL:         input.RtspURL,
		SubRtspURL:      input.SubRtspURL,
		Remark:          input.Remark,
		TransportPolicy: model.CameraTransportAuto,
		LastProbeStatus: model.CameraProbeNever,
	}
	camera.ConfigHash = cameraConfigHash(camera.Protocol, camera.RtspURL, camera.TransportPolicy)
	if err := s.repo.Create(ctx, camera); err != nil {
		return nil, mapRepoError(err)
	}
	return camera, nil
}

func (s *cameraService) UpdateCamera(ctx context.Context, id uint64, input *SaveCameraInput) (*model.Camera, error) {
	camera, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	if err := s.normalizeAndValidate(input); err != nil {
		return nil, err
	}
	camera.Name = input.Name
	camera.RtspURL = input.RtspURL
	camera.SubRtspURL = input.SubRtspURL
	camera.Remark = input.Remark
	// 配置变更后重新计算指纹；旧测活元数据保留但通过指纹比对视为不适用于当前配置。
	camera.ConfigHash = cameraConfigHash(camera.Protocol, camera.RtspURL, camera.TransportPolicy)
	if err := s.repo.Update(ctx, camera); err != nil {
		return nil, mapRepoError(err)
	}
	return camera, nil
}

func (s *cameraService) DeleteCamera(ctx context.Context, id uint64) error {
	// 删除保护（D9）：摄像头存在未软删分析任务时拒绝删除。
	if err := s.ensureNoTaskRef(ctx, id); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return errno.NewError(errno.CodeNotFound)
	}
	return nil
}

func (s *cameraService) BatchDeleteCamera(ctx context.Context, ids []uint64) error {
	// 删除保护（D9）：任一摄像头存在未软删任务即整批拒绝——
	// 部分成功会让用户误判操作结果；16 路规模下全量检查代价可忽略。
	for _, id := range ids {
		if err := s.ensureNoTaskRef(ctx, id); err != nil {
			return err
		}
	}
	return s.repo.BatchDelete(ctx, ids)
}

// ensureNoTaskRef 校验指定摄像头（按主键 id 取 camera_id 业务键）无未软删分析任务，
// 存在则返回 CodeCameraInUse（D9）。
func (s *cameraService) ensureNoTaskRef(ctx context.Context, id uint64) error {
	camera, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}
	count, err := s.taskRepo.CountTasksByCameraID(ctx, camera.CameraID)
	if err != nil {
		return err
	}
	if count > 0 {
		return errno.NewError(errno.CodeCameraInUse)
	}
	return nil
}

// ProbeCamera 测活编排：协议/URL 校验 → 读取 DB → 调 Engine → 按配置指纹规则落库。
// 测活失败作为正常结构化结果返回（status=failed），不产生业务错误码。
func (s *cameraService) ProbeCamera(ctx context.Context, req *ProbeCameraRequest) (*ProbeCameraResult, error) {
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	url := strings.TrimSpace(req.RtspURL)
	adapter, ok := s.registry.Lookup(protocol)
	if !ok {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if err := adapter.ValidateURL(url); err != nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if req.ID != 0 {
		if _, err := s.repo.GetByID(ctx, req.ID); err != nil {
			return nil, mapRepoError(err)
		}
	}

	// 配置指纹：以本次请求的 protocol+canonical_url+transport_policy 计算。
	hash := cameraConfigHash(protocol, url, model.CameraTransportAuto)

	probeCtx, cancel := context.WithTimeout(ctx, CameraProbeTimeout)
	defer cancel()
	resp, err := s.engine.ProbeCamera(probeCtx, &argusv1.ProbeCameraRequest{
		Protocol: protocol,
		Url:      url,
	})
	if err != nil {
		return nil, mapProbeEngineError(err)
	}
	result := buildProbeResult(resp)

	// 落库规则：有 id 且完成后当前配置指纹仍匹配才持久化；否则 stale 不覆盖。
	if req.ID != 0 {
		current, err := s.repo.GetByID(ctx, req.ID)
		if err != nil {
			// 测活期间摄像头被删除：不落库，结果标记 stale。
			result.Stale = true
		} else if current.ConfigHash != hash {
			result.Stale = true
		} else {
			if err := s.persistProbeResult(ctx, current, result); err != nil {
				return nil, mapRepoError(err)
			}
			result.Persisted = true
		}
	}
	return result, nil
}

// persistProbeResult 按测活结果更新摄像头测活元数据（指纹已由调用方校验一致）。
// 失败测活更新当前失败状态但保留最后成功媒体信息。
func (s *cameraService) persistProbeResult(ctx context.Context, camera *model.Camera, result *ProbeCameraResult) error {
	now := time.Now()
	camera.LastProbeAt = &now
	if result.Status == model.CameraProbeSuccess {
		camera.LastProbeStatus = model.CameraProbeSuccess
		camera.LastProbeErrorCode = ""
		camera.LastProbeErrorMessage = ""
		camera.LastSuccessAt = &now
		camera.LastSuccessTransport = result.SelectedTransport
		camera.LastCodec = result.Codec
		camera.LastWidth = int(result.Width)
		camera.LastHeight = int(result.Height)
		camera.LastFPS = result.FPS
	} else {
		camera.LastProbeStatus = model.CameraProbeFailed
		camera.LastProbeErrorCode = result.FailureCode
		camera.LastProbeErrorMessage = result.FailureMessage
		// 保留最后成功媒体信息。
	}
	return s.repo.Update(ctx, camera)
}

// StartLivePreview 请求开启实时预览拉流并返回播放流地址
func (s *cameraService) StartLivePreview(ctx context.Context, id uint64, reqStreamType string) (*CameraLiveStreamResult, error) {
	camera, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}

	st := argusv1.StreamType_STREAM_TYPE_MAIN
	urlToPlay := camera.RtspURL
	finalTypeStr := "main"

	if strings.ToLower(reqStreamType) == "sub" {
		if strings.TrimSpace(camera.SubRtspURL) != "" {
			st = argusv1.StreamType_STREAM_TYPE_SUB
			urlToPlay = camera.SubRtspURL
			finalTypeStr = "sub"
		}
	}

	req := &argusv1.StartCameraPreviewRequest{
		CameraId:   camera.CameraID,
		StreamType: st,
		Url:        urlToPlay,
	}

	resp, err := s.engine.StartCameraPreview(ctx, req)
	if err != nil {
		return nil, mapProbeEngineError(err)
	}
	if resp.GetCode() != "" {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}

	return &CameraLiveStreamResult{
		StreamPath: resp.GetStreamPath(),
		HTTPPort:   resp.GetHttpPort(),
		WSPort:     resp.GetWsPort(),
		StreamType: finalTypeStr,
	}, nil
}

// StopLivePreview 停止指定摄像头的预览拉流
func (s *cameraService) StopLivePreview(ctx context.Context, id uint64, reqStreamType string) error {
	camera, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}

	st := argusv1.StreamType_STREAM_TYPE_MAIN
	if strings.ToLower(reqStreamType) == "sub" {
		st = argusv1.StreamType_STREAM_TYPE_SUB
	}

	req := &argusv1.StopCameraPreviewRequest{
		CameraId:   camera.CameraID,
		StreamType: st,
	}

	_, err = s.engine.StopCameraPreview(ctx, req)
	if err != nil {
		return mapProbeEngineError(err)
	}
	return nil
}

// buildProbeResult 将 Engine 测活响应转换为结构化结果。
func buildProbeResult(resp *argusv1.ProbeCameraResponse) *ProbeCameraResult {
	result := &ProbeCameraResult{
		Status:            resp.GetStatus(),
		FailureCode:       resp.GetFailureCode(),
		SelectedTransport: resp.GetSelectedTransport(),
		Codec:             resp.GetCodec(),
		Width:             resp.GetWidth(),
		Height:            resp.GetHeight(),
		FPS:               resp.GetFps(),
		ElapsedMS:         resp.GetElapsedMs(),
	}
	for _, attempt := range resp.GetAttempts() {
		result.Attempts = append(result.Attempts, ProbeAttempt{
			Transport:   attempt.GetTransport(),
			ElapsedMS:   attempt.GetElapsedMs(),
			FailureCode: attempt.GetFailureCode(),
		})
	}
	// 失败时补充诊断文案（稳定码本地化在 API 层完成，这里保留原始码）。
	if result.Status != model.CameraProbeSuccess {
		result.FailureMessage = "camera probe failed: " + result.FailureCode
	}
	return result
}

// normalizeAndValidate 规范化并校验保存入参（名称/备注/RTSP URL）。
func (s *cameraService) normalizeAndValidate(input *SaveCameraInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.RtspURL = strings.TrimSpace(input.RtspURL)
	input.Remark = strings.TrimSpace(input.Remark)
	if input.Name == "" || len(input.Name) > 128 {
		return errno.NewError(errno.CodeInvalidParam)
	}
	if len(input.Remark) > 255 {
		return errno.NewError(errno.CodeInvalidParam)
	}
	adapter, ok := s.registry.Lookup(model.CameraProtocolRTSP)
	if !ok {
		return errno.NewError(errno.CodeInvalidParam)
	}
	if err := adapter.ValidateURL(input.RtspURL); err != nil {
		return errno.NewError(errno.CodeInvalidParam)
	}
	return nil
}

// cameraConfigHash 计算配置指纹：sha256(protocol + \x00 + canonical_url + \x00 + transport_policy)。
// canonical_url 为去首尾空白后的持久化 URL 原文（不再次改写，避免改变语义）。
func cameraConfigHash(protocol, url, transportPolicy string) string {
	h := sha256.New()
	h.Write([]byte(protocol))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(url)))
	h.Write([]byte{0})
	h.Write([]byte(transportPolicy))
	return hex.EncodeToString(h.Sum(nil))
}

// mapProbeEngineError 将 Engine RPC 处理错误映射为业务错误码。
// 注意：测活失败（status=failed）不走这里；这里只处理参数/平台/传输层错误。
func mapProbeEngineError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errno.NewError(errno.CodeInternal)
	}
	var remote *engineipc.RemoteError
	if errors.As(err, &remote) {
		switch remote.Code {
		case "INVALID_ARG":
			return errno.NewError(errno.CodeInvalidParam)
		default:
			return errno.NewError(errno.CodeInternal)
		}
	}
	return errno.NewError(errno.CodeInternal)
}
