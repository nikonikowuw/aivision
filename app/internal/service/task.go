package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
)

// ── 配额上限缓存 ────────────────────────────────────────────────────────
// 设计依据：design §5「配额上限缓存」与 §7 兼容性（total==0 视为未成功获取）。

const (
	// quotaFetchTimeout QueryProfile 单次调用超时。
	quotaFetchTimeout = 5 * time.Second
	// quotaRefreshInterval 距上次成功获取超过该时长时，校验请求触发异步刷新。
	quotaRefreshInterval = 5 * time.Minute
	// quotaInitialBackoff / quotaMaxBackoff 首次获取失败后的指数退避区间。
	quotaInitialBackoff = time.Second
	quotaMaxBackoff     = 30 * time.Second
)

// ProfileClient 定义 TaskService 所需的 Engine 窄接口（便于测试替身）。
type ProfileClient interface {
	QueryProfile(ctx context.Context, req *aivisionv1.QueryProfileRequest, opts ...grpc.CallOption) (*aivisionv1.QueryProfileResponse, error)
}

// quotaLimits 缓存的计算资源上限快照。
type quotaLimits struct {
	total     int32
	reserved  int32
	fetchedAt time.Time
	ok        bool // 是否曾成功获取过（design §7：成功过用上次值，否则拒绝启用）
}

// quotaManager 缓存 Engine 算力上限：构造后后台获取，失败指数退避重试直到首次成功；
// 之后由校验请求触发 5 分钟过期的异步刷新（不阻塞请求）。total<=0 视为未成功获取
// （旧 Engine 未上报算力单位，不能当作「容量为 0」拒绝一切）。
type quotaManager struct {
	client ProfileClient
	log    *zap.Logger

	mu         sync.Mutex
	limits     quotaLimits
	refreshing bool
}

func newQuotaManager(client ProfileClient, log *zap.Logger) *quotaManager {
	return &quotaManager{client: client, log: log}
}

// run 后台循环：指数退避重试直到首次成功获取上限。
func (q *quotaManager) run() {
	backoff := quotaInitialBackoff
	for {
		err := q.fetch()
		if err == nil {
			return
		}
		q.log.Warn("quota limits fetch failed, retrying",
			zap.Duration("backoff", backoff), zap.Error(err))
		time.Sleep(backoff)
		if backoff < quotaMaxBackoff {
			backoff *= 2
			if backoff > quotaMaxBackoff {
				backoff = quotaMaxBackoff
			}
		}
	}
}

// fetch 同步拉取一次上限；total>0 才算成功并更新缓存。
func (q *quotaManager) fetch() error {
	if q.client == nil {
		return errors.New("quota profile client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), quotaFetchTimeout)
	defer cancel()
	resp, err := q.client.QueryProfile(ctx, &aivisionv1.QueryProfileRequest{})
	if err != nil {
		return fmt.Errorf("query engine profile: %w", err)
	}
	profile := resp.GetProfile()
	if profile == nil {
		return errors.New("query engine profile: empty profile")
	}
	total := profile.GetTotalComputeUnits()
	if total <= 0 {
		// design §7：旧 Engine 未上报算力单位 → 等同于未成功获取，进重试循环。
		return errors.New("query engine profile: total compute units not reported")
	}
	reserved := profile.GetReservedComputeUnits()
	if reserved < 0 {
		reserved = 0
	}
	q.mu.Lock()
	q.limits = quotaLimits{total: total, reserved: reserved, fetchedAt: time.Now(), ok: true}
	q.mu.Unlock()
	q.log.Info("quota limits loaded",
		zap.Int32("total", total), zap.Int32("reserved", reserved))
	return nil
}

// current 返回缓存快照与是否曾成功获取。
func (q *quotaManager) current() (quotaLimits, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.limits, q.limits.ok
}

// ensureFresh 距上次获取超过 quotaRefreshInterval 时触发一次异步刷新（不阻塞当前请求）。
// 刷新失败保留旧值（ok 不回退），由下次校验再次触发。
func (q *quotaManager) ensureFresh() {
	q.mu.Lock()
	if !q.limits.ok || q.refreshing || time.Since(q.limits.fetchedAt) <= quotaRefreshInterval {
		q.mu.Unlock()
		return
	}
	q.refreshing = true
	q.mu.Unlock()

	go func() {
		if err := q.fetch(); err != nil {
			q.log.Warn("quota limits refresh failed", zap.Error(err))
		}
		q.mu.Lock()
		q.refreshing = false
		q.mu.Unlock()
	}()
}

// ── DTO ─────────────────────────────────────────────────────────────────

// TaskListQuery 任务分页查询参数。
type TaskListQuery struct {
	Page       int    `form:"page" binding:"omitempty,min=1"`
	PageSize   int    `form:"pageSize" binding:"omitempty,min=1,max=100"`
	CameraID   string `form:"cameraId"`
	Name       string `form:"name"`
	Configured *bool  `form:"configured"` // nil=全部；true=已有实例；false=无实例
}

// TaskItem 任务列表项：库中配置与状态码为底，内存实时字段合并（design D6）。
type TaskItem struct {
	CameraID       string     `json:"cameraId"`
	Name           string     `json:"name"`
	DesiredEnabled bool       `json:"desiredEnabled"`
	ActualStatus   int8       `json:"actualStatus"`
	StatusMessage  string     `json:"statusMessage"`
	LastFrameAt    *time.Time `json:"lastFrameAt"` // 实时字段：无上报时为 null（等待上报）
	ReportedAt     *time.Time `json:"reportedAt"`  // 实时字段：无上报时为 null
}

// TaskPageResult 任务分页查询结果。
type TaskPageResult struct {
	Items []*TaskItem `json:"items"`
	Total int64       `json:"total"`
}

// CreateTaskInput 创建分析任务入参（D8：任务绑定一个尚未建任务的摄像头）。
type CreateTaskInput struct {
	CameraID string `json:"cameraId" binding:"required"`
	Name     string `json:"name" binding:"required,max=128"`
}

// UpdateTaskInput 更新任务入参（当前仅任务名）。
type UpdateTaskInput struct {
	Name string `json:"name" binding:"required,max=128"`
}

// SetEnabledInput 启停入参。
type SetEnabledInput struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// AvailableCameraItem 未建任务摄像头轻量项（D8 下拉数据契约：无分页，value 用 camera_id）。
type AvailableCameraItem struct {
	CameraID string `json:"cameraId"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

// InstanceItem 实例列表项（状态合并同 TaskItem）。
type InstanceItem struct {
	InstanceID    string                `json:"instanceId"`
	CameraID      string                `json:"cameraId"`
	AlgorithmID   string                `json:"algorithmId"`
	AnalysisFPS   int32                 `json:"analysisFps"`
	ParamsJSON    json.RawMessage       `json:"paramsJson"`
	Rules         []model.DetectionRule `json:"rules"`
	Enabled       bool                  `json:"enabled"`
	ActualStatus  int8                  `json:"actualStatus"`
	StatusMessage string                `json:"statusMessage"`
	CurrentFps    *float32              `json:"currentFps"` // 实时字段：无上报时为 null
	ReportedAt    *time.Time            `json:"reportedAt"` // 实时字段：无上报时为 null
}

// CreateInstanceInput 创建实例入参。AnalysisFPS<=0 按默认 25 处理（D12）；
// ParamsJSON 缺省为 {}；Rules 缺省为空；Enabled=true 时创建即启用。
type CreateInstanceInput struct {
	CameraID    string                `json:"cameraId" binding:"required"`
	AlgorithmID string                `json:"algorithmId" binding:"required"`
	AnalysisFPS int32                 `json:"analysisFps"`
	ParamsJSON  json.RawMessage       `json:"paramsJson"`
	Rules       []model.DetectionRule `json:"rules"`
	Enabled     bool                  `json:"enabled"`
}

// UpdateInstanceInput 整份提交实例配置（analysisFps + paramsJson + rules，design §4.2）。
// 三个字段都必须出现在请求体中（required），拒绝部分更新。
type UpdateInstanceInput struct {
	AnalysisFPS *int32                `json:"analysisFps" binding:"required"`
	ParamsJSON  json.RawMessage       `json:"paramsJson" binding:"required"`
	Rules       []model.DetectionRule `json:"rules" binding:"required"`
}

// ── TaskService ─────────────────────────────────────────────────────────

// TaskService 任务配置模块业务接口。
// 改变 DesiredState 内容的写路径全部经 repo.InTx 并同事务 BumpRevision；
// 校验顺序固定为 schema → 几何 → 配额，任一失败零副作用（design §4.1）。
type TaskService interface {
	ListTasks(ctx context.Context, query *TaskListQuery) (*TaskPageResult, error)
	CreateTask(ctx context.Context, input *CreateTaskInput) (*TaskItem, error)
	UpdateTask(ctx context.Context, cameraID string, input *UpdateTaskInput) error
	SetTaskEnabled(ctx context.Context, cameraID string, enabled bool) error
	DeleteTask(ctx context.Context, cameraID string) error
	ListAvailableCameras(ctx context.Context) ([]AvailableCameraItem, error)

	ListInstances(ctx context.Context, cameraID string) ([]*InstanceItem, error)
	CreateInstance(ctx context.Context, input *CreateInstanceInput) (*InstanceItem, error)
	UpdateInstance(ctx context.Context, instanceID string, input *UpdateInstanceInput) error
	SetInstanceEnabled(ctx context.Context, instanceID string, enabled bool) error
	DeleteInstance(ctx context.Context, instanceID string) error
}

type taskService struct {
	repo       repository.TaskRepository
	cameraRepo repository.CameraRepository
	algoRepo   repository.AlgorithmRepository
	report     *ReportAdapter
	log        *zap.Logger
	quota      *quotaManager
}

// NewTaskService 创建 TaskService 并启动配额上限的后台获取循环。
func NewTaskService(
	repo repository.TaskRepository,
	cameraRepo repository.CameraRepository,
	algoRepo repository.AlgorithmRepository,
	report *ReportAdapter,
	profileClient ProfileClient,
	log *zap.Logger,
) TaskService {
	if log == nil {
		log = zap.NewNop()
	}
	s := &taskService{
		repo:       repo,
		cameraRepo: cameraRepo,
		algoRepo:   algoRepo,
		report:     report,
		log:        log,
	}
	s.quota = newQuotaManager(profileClient, log)
	go s.quota.run()
	return s
}

// ── 任务 ────────────────────────────────────────────────────────────────

func (s *taskService) ListTasks(ctx context.Context, query *TaskListQuery) (*TaskPageResult, error) {
	items, total, err := s.repo.ListTaskPage(ctx, &repository.TaskFilter{
		Page:       query.Page,
		PageSize:   query.PageSize,
		CameraID:   strings.TrimSpace(query.CameraID),
		Name:       strings.TrimSpace(query.Name),
		Configured: query.Configured,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*TaskItem, 0, len(items))
	for i := range items {
		out = append(out, s.mergeTask(&items[i]))
	}
	return &TaskPageResult{Items: out, Total: total}, nil
}

// mergeTask 库中配置/状态码为底，合并内存实时字段（D6）。
func (s *taskService) mergeTask(task *model.AnalysisTask) *TaskItem {
	item := &TaskItem{
		CameraID:       task.CameraID,
		Name:           task.Name,
		DesiredEnabled: task.DesiredEnabled,
		ActualStatus:   task.ActualStatus,
		StatusMessage:  task.StatusMessage,
	}
	if rt, ok := s.report.TaskRuntime(task.CameraID); ok {
		item.LastFrameAt = rt.LastFrameAt
		item.ReportedAt = timePtr(rt.ReportedAt)
	}
	return item
}

func (s *taskService) CreateTask(ctx context.Context, input *CreateTaskInput) (*TaskItem, error) {
	cameraID := strings.TrimSpace(input.CameraID)
	name := strings.TrimSpace(input.Name)
	if cameraID == "" || name == "" || len(name) > 128 {
		return nil, errno.New(errno.CodeInvalidParam)
	}
	// 摄像头必须存在，避免产生永不进入快照的幽灵任务（LoadDesiredSnapshot JOIN 过滤）。
	if _, err := s.cameraRepo.GetByCameraID(ctx, cameraID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.New(errno.CodeNotFound)
		}
		return nil, err
	}
	if _, err := s.repo.GetTaskByCameraID(ctx, cameraID); err == nil {
		return nil, errno.New(errno.CodeTaskAlreadyExists)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	task := &model.AnalysisTask{
		CameraID:       cameraID,
		Name:           name,
		DesiredEnabled: false, // 新建任务默认停用（D8），启停走 SetTaskEnabled
		ActualStatus:   model.TaskStatusStopped,
	}
	if err := s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.CreateTask(ctx, task); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	}); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			// 并发创建同一摄像头任务：唯一索引兜底。
			return nil, errno.New(errno.CodeTaskAlreadyExists)
		}
		return nil, err
	}
	s.log.Info("task created", zap.String("camera_id", cameraID), zap.String("name", name))
	return s.mergeTask(task), nil
}

func (s *taskService) UpdateTask(ctx context.Context, cameraID string, input *UpdateTaskInput) error {
	cameraID = strings.TrimSpace(cameraID)
	name := strings.TrimSpace(input.Name)
	if cameraID == "" || name == "" || len(name) > 128 {
		return errno.New(errno.CodeInvalidParam)
	}
	task, err := s.repo.GetTaskByCameraID(ctx, cameraID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeTaskNotFound)
		}
		return err
	}
	task.Name = name
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.UpdateTask(ctx, task); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *taskService) SetTaskEnabled(ctx context.Context, cameraID string, enabled bool) error {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return errno.New(errno.CodeInvalidParam)
	}
	task, err := s.repo.GetTaskByCameraID(ctx, cameraID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeTaskNotFound)
		}
		return err
	}
	if task.DesiredEnabled == enabled {
		return nil // 幂等：无状态变化不写库、不 bump
	}
	task.DesiredEnabled = enabled
	task.StatusMessage = ""
	if enabled {
		task.ActualStatus = model.TaskStatusStarting
	} else {
		task.ActualStatus = model.TaskStatusStopped
	}
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.UpdateTask(ctx, task); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *taskService) DeleteTask(ctx context.Context, cameraID string) error {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return errno.New(errno.CodeInvalidParam)
	}
	// 任务软删 + 实例级联软删 + revision bump 必须同事务（D9）。
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		deleted, err := r.DeleteTaskCascade(ctx, cameraID)
		if err != nil {
			return err
		}
		if !deleted {
			return errno.New(errno.CodeTaskNotFound)
		}
		_, err = r.BumpRevision(ctx)
		return err
	})
}

func (s *taskService) ListAvailableCameras(ctx context.Context) ([]AvailableCameraItem, error) {
	cameras, err := s.cameraRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	taskCameraIDs, err := s.repo.ListTaskCameraIDs(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]struct{}, len(taskCameraIDs))
	for _, id := range taskCameraIDs {
		taken[id] = struct{}{}
	}
	out := make([]AvailableCameraItem, 0, len(cameras))
	for _, cam := range cameras {
		if _, ok := taken[cam.CameraID]; ok {
			continue
		}
		out = append(out, AvailableCameraItem{
			CameraID: cam.CameraID,
			Name:     cam.Name,
			Protocol: cam.Protocol,
		})
	}
	return out, nil
}

// ── 实例 ────────────────────────────────────────────────────────────────

func (s *taskService) ListInstances(ctx context.Context, cameraID string) ([]*InstanceItem, error) {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return nil, errno.New(errno.CodeInvalidParam)
	}
	if _, err := s.repo.GetTaskByCameraID(ctx, cameraID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.New(errno.CodeTaskNotFound)
		}
		return nil, err
	}
	items, err := s.repo.ListInstancesByCameraID(ctx, cameraID)
	if err != nil {
		return nil, err
	}
	out := make([]*InstanceItem, 0, len(items))
	for i := range items {
		out = append(out, s.mergeInstance(&items[i]))
	}
	return out, nil
}

// mergeInstance 库中配置/状态码为底，合并内存实时字段（D6）。
func (s *taskService) mergeInstance(inst *model.AlgorithmInstance) *InstanceItem {
	item := &InstanceItem{
		InstanceID:    inst.InstanceID,
		CameraID:      inst.CameraID,
		AlgorithmID:   inst.AlgorithmID,
		AnalysisFPS:   inst.AnalysisFPS,
		ParamsJSON:    inst.ParamsJSON,
		Rules:         s.parseStoredRules(inst),
		Enabled:       inst.Enabled,
		ActualStatus:  inst.ActualStatus,
		StatusMessage: inst.StatusMessage,
	}
	if rt, ok := s.report.InstanceRuntime(inst.InstanceID); ok {
		fps := rt.CurrentFps
		item.CurrentFps = &fps
		item.ReportedAt = timePtr(rt.ReportedAt)
	}
	return item
}

// parseStoredRules 解析实例规则 JSON；损坏时记 warn 并返回空列表（列表展示不 500）。
func (s *taskService) parseStoredRules(inst *model.AlgorithmInstance) []model.DetectionRule {
	if len(inst.RulesJSON) == 0 {
		return nil
	}
	var rules []model.DetectionRule
	if err := json.Unmarshal(inst.RulesJSON, &rules); err != nil {
		s.log.Warn("stored rules_json is corrupted",
			zap.String("instance_id", inst.InstanceID), zap.Error(err))
		return nil
	}
	return rules
}

func (s *taskService) CreateInstance(ctx context.Context, input *CreateInstanceInput) (*InstanceItem, error) {
	cameraID := strings.TrimSpace(input.CameraID)
	algorithmID := strings.TrimSpace(input.AlgorithmID)
	if cameraID == "" || algorithmID == "" {
		return nil, errno.New(errno.CodeInvalidParam)
	}
	// 1. camera_id 对应任务必须存在。
	if _, err := s.repo.GetTaskByCameraID(ctx, cameraID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.New(errno.CodeTaskNotFound)
		}
		return nil, err
	}
	// 2. algorithm_id 存在且 active_version 非空（D11：版本在快照组装时动态填充）。
	algo, err := s.algoRepo.GetAlgorithmByID(ctx, algorithmID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.New(errno.CodeNotFound)
		}
		return nil, err
	}
	if algo.ActiveVersion == "" {
		s.log.Warn("algorithm has no active version",
			zap.String("algorithm_id", algorithmID))
		return nil, errno.New(errno.CodeInvalidParam)
	}
	// 3-5. 校验顺序固定：schema → 几何 → 配额；任一失败零副作用。
	if err := s.validateInstanceConfig(ctx, algorithmID, algo.ActiveVersion,
		input.AnalysisFPS, input.ParamsJSON, input.Rules, ""); err != nil {
		return nil, err
	}

	paramsJSON, err := normalizeParamsJSON(input.ParamsJSON)
	if err != nil {
		return nil, err
	}
	rulesJSON, err := marshalRules(input.Rules)
	if err != nil {
		return nil, err
	}
	inst := &model.AlgorithmInstance{
		InstanceID:   uuid.NewString(),
		CameraID:     cameraID,
		AlgorithmID:  algorithmID,
		AnalysisFPS:  input.AnalysisFPS,
		ParamsJSON:   paramsJSON,
		RulesJSON:    rulesJSON,
		Enabled:      input.Enabled,
		ActualStatus: model.InstanceStatusStopped,
	}
	if input.Enabled {
		// 乐观提交（D3）：Go 预校验通过即写库，Engine ≤2s 内应用并回报真实状态；
		// 写入瞬间先展示 STARTING 中间态。
		inst.ActualStatus = model.InstanceStatusStarting
	}
	if err := s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.CreateInstance(ctx, inst); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	s.log.Info("instance created",
		zap.String("instance_id", inst.InstanceID),
		zap.String("camera_id", cameraID),
		zap.String("algorithm_id", algorithmID),
		zap.Int32("analysis_fps", inst.AnalysisFPS),
		zap.Bool("enabled", inst.Enabled))
	return s.mergeInstance(inst), nil
}

func (s *taskService) UpdateInstance(ctx context.Context, instanceID string, input *UpdateInstanceInput) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errno.New(errno.CodeInvalidParam)
	}
	inst, err := s.repo.GetInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeInstanceNotFound)
		}
		return err
	}
	// 算法可能已被卸载（Phase 4 引用保护落地前），更新时复校存在性与激活版本。
	algo, err := s.algoRepo.GetAlgorithmByID(ctx, inst.AlgorithmID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}
	if algo.ActiveVersion == "" {
		s.log.Warn("algorithm has no active version",
			zap.String("algorithm_id", inst.AlgorithmID))
		return errno.New(errno.CodeInvalidParam)
	}
	fps := *input.AnalysisFPS
	// 整份提交：schema → 几何 → 配额全量复校；配额排除自身旧占用，避免重复计数。
	if err := s.validateInstanceConfig(ctx, inst.AlgorithmID, algo.ActiveVersion,
		fps, input.ParamsJSON, input.Rules, inst.InstanceID); err != nil {
		return err
	}

	paramsJSON, err := normalizeParamsJSON(input.ParamsJSON)
	if err != nil {
		return err
	}
	rulesJSON, err := marshalRules(input.Rules)
	if err != nil {
		return err
	}
	inst.AnalysisFPS = fps
	inst.ParamsJSON = paramsJSON
	inst.RulesJSON = rulesJSON
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *taskService) SetInstanceEnabled(ctx context.Context, instanceID string, enabled bool) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errno.New(errno.CodeInvalidParam)
	}
	inst, err := s.repo.GetInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeInstanceNotFound)
		}
		return err
	}
	if inst.Enabled == enabled {
		return nil // 幂等：无状态变化不写库、不 bump
	}
	if enabled {
		// 启用前完整复校（design §4.1）：schema → 几何 → 配额。
		algo, err := s.algoRepo.GetAlgorithmByID(ctx, inst.AlgorithmID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return errno.New(errno.CodeNotFound)
			}
			return err
		}
		if algo.ActiveVersion == "" {
			s.log.Warn("algorithm has no active version",
				zap.String("algorithm_id", inst.AlgorithmID))
			return errno.New(errno.CodeInvalidParam)
		}
		if err := s.validateInstanceConfig(ctx, inst.AlgorithmID, algo.ActiveVersion,
			inst.AnalysisFPS, inst.ParamsJSON, s.parseStoredRules(inst), inst.InstanceID); err != nil {
			return err
		}
		inst.Enabled = true
		inst.ActualStatus = model.InstanceStatusStarting
		inst.StatusMessage = ""
	} else {
		inst.Enabled = false
		inst.ActualStatus = model.InstanceStatusStopped
		inst.StatusMessage = ""
	}
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if err := r.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *taskService) DeleteInstance(ctx context.Context, instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errno.New(errno.CodeInvalidParam)
	}
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		deleted, err := r.DeleteInstance(ctx, instanceID)
		if err != nil {
			return err
		}
		if !deleted {
			return errno.New(errno.CodeInstanceNotFound)
		}
		_, err = r.BumpRevision(ctx)
		return err
	})
}

// ── 校验与配额 ──────────────────────────────────────────────────────────

// validateInstanceConfig 按固定顺序执行实例配置的三级校验：
// schema（active version 的 config_schema）→ 几何（rulegeom）→ 配额。
// exceptInstanceID 非空时配额累计排除该实例自身的旧占用（更新/启用语义）。
// 任一失败返回 4xx 业务错误，调用方保证零副作用（不写库、不 bump）。
func (s *taskService) validateInstanceConfig(
	ctx context.Context,
	algorithmID, activeVersion string,
	analysisFPS int32,
	paramsRaw json.RawMessage,
	rules []model.DetectionRule,
	exceptInstanceID string,
) error {
	version, err := s.algoRepo.GetVersion(ctx, algorithmID, activeVersion)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			s.log.Warn("active version row missing",
				zap.String("algorithm_id", algorithmID),
				zap.String("version", activeVersion))
			return errno.New(errno.CodeInvalidParam)
		}
		return err
	}

	// 1. schema 校验（服务端复校，不信任前端）。
	schema, err := CompileSchema(version.ConfigSchema)
	if err != nil {
		// 已安装算法包的 schema 非法属数据问题，按内部错误处理并记 warn。
		s.log.Warn("stored config schema invalid",
			zap.String("algorithm_id", algorithmID), zap.Error(err))
		return fmt.Errorf("compile config schema of %s@%s: %w", algorithmID, activeVersion, err)
	}
	if err := schema.Validate(paramsRaw); err != nil {
		s.log.Warn("params failed schema validation",
			zap.String("algorithm_id", algorithmID), zap.Error(err))
		return errno.New(errno.CodeInvalidParam)
	}

	// 2. 几何校验。
	if err := ValidateRules(rules); err != nil {
		return err
	}

	// 3. 配额校验。
	var tiers []model.FPSTier
	if err := json.Unmarshal(version.FPSTiers, &tiers); err != nil {
		s.log.Warn("stored fps tiers invalid",
			zap.String("algorithm_id", algorithmID), zap.Error(err))
		return fmt.Errorf("parse fps tiers of %s@%s: %w", algorithmID, activeVersion, err)
	}
	requested, err := ResolveUnits(tiers, analysisFPS)
	if err != nil {
		return errno.New(errno.CodeFPSTierExceeded)
	}
	return s.checkQuota(ctx, exceptInstanceID, requested)
}

// checkQuota 校验 Σ units(已启用实例，排除 exceptInstanceID) + requested ≤ total - reserved。
// 超出返回 CodeResourceExceeded，错误信息携带已用/申请/可用上限三个数字（PRD R5）。
func (s *taskService) checkQuota(ctx context.Context, exceptInstanceID string, requested uint32) error {
	s.quota.ensureFresh()
	limits, ok := s.quota.current()
	if !ok {
		// design §7：从未成功获取过上限（Engine 不可用或版本过旧）→ 拒绝启用，
		// 返回明确原因；已有实例不受影响。
		return errno.New(errno.CodeEngineUnavailable)
	}
	used, err := s.sumUsedUnits(ctx, exceptInstanceID)
	if err != nil {
		return err
	}
	available := int64(limits.total) - int64(limits.reserved)
	if available < 0 {
		available = 0
	}
	if int64(used)+int64(requested) > available {
		return errno.NewErrorArgs(errno.CodeResourceExceeded, used, requested, available)
	}
	return nil
}

// sumUsedUnits 累加已启用实例占用的资源单位（排除 exceptInstanceID 自身）。
// 算法缺失/未激活/档位不可解析的实例记 warn 并按不占资源处理：
// 该类实例本身不会进入 DesiredState、不会被 Engine 运行。
func (s *taskService) sumUsedUnits(ctx context.Context, exceptInstanceID string) (uint32, error) {
	instances, err := s.repo.ListEnabledInstances(ctx)
	if err != nil {
		return 0, err
	}
	var used uint32
	for i := range instances {
		inst := &instances[i]
		if exceptInstanceID != "" && inst.InstanceID == exceptInstanceID {
			continue
		}
		units, ok := s.instanceUnits(ctx, inst)
		if !ok {
			continue
		}
		used += units
	}
	return used, nil
}

// instanceUnits 计算单个已启用实例的 units：经 algorithm.active_version 取档位
// 并 ResolveUnits（D12）。返回 ok=false 表示该实例当前不可计价。
func (s *taskService) instanceUnits(ctx context.Context, inst *model.AlgorithmInstance) (uint32, bool) {
	algo, err := s.algoRepo.GetAlgorithmByID(ctx, inst.AlgorithmID)
	if err != nil {
		s.log.Warn("quota sum: algorithm missing",
			zap.String("algorithm_id", inst.AlgorithmID),
			zap.String("instance_id", inst.InstanceID), zap.Error(err))
		return 0, false
	}
	if algo.ActiveVersion == "" {
		s.log.Warn("quota sum: algorithm has no active version",
			zap.String("algorithm_id", inst.AlgorithmID))
		return 0, false
	}
	version, err := s.algoRepo.GetVersion(ctx, inst.AlgorithmID, algo.ActiveVersion)
	if err != nil {
		s.log.Warn("quota sum: active version row missing",
			zap.String("algorithm_id", inst.AlgorithmID),
			zap.String("version", algo.ActiveVersion), zap.Error(err))
		return 0, false
	}
	var tiers []model.FPSTier
	if err := json.Unmarshal(version.FPSTiers, &tiers); err != nil {
		s.log.Warn("quota sum: stored fps tiers invalid",
			zap.String("algorithm_id", inst.AlgorithmID), zap.Error(err))
		return 0, false
	}
	units, err := ResolveUnits(tiers, inst.AnalysisFPS)
	if err != nil {
		s.log.Warn("quota sum: instance fps exceeds tiers",
			zap.String("instance_id", inst.InstanceID), zap.Error(err))
		return 0, false
	}
	return units, true
}

// ── 辅助 ────────────────────────────────────────────────────────────────

// normalizeParamsJSON 空入参归一为 "{}"（列 default 对齐）。
func normalizeParamsJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errno.New(errno.CodeInvalidParam)
	}
	return raw, nil
}

// marshalRules 序列化规则 JSON；nil 归一为空数组。
func marshalRules(rules []model.DetectionRule) (json.RawMessage, error) {
	if rules == nil {
		rules = []model.DetectionRule{}
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("marshal detection rules: %w", err)
	}
	return raw, nil
}

// timePtr 拷贝时间值返回指针（列表合并用，避免共享 reportAdapter 内部值）。
func timePtr(t time.Time) *time.Time {
	cp := t
	return &cp
}
