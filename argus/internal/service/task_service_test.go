package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// fakeProfileClient 可编程的 ProfileClient 替身：返回固定 profile 或固定错误。
type fakeProfileClient struct {
	mu      sync.Mutex
	profile *argusv1.QueryProfileResponse
	err     error
}

func (f *fakeProfileClient) set(profile *argusv1.QueryProfileResponse, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.profile = profile
	f.err = err
}

func (f *fakeProfileClient) QueryProfile(_ context.Context, _ *argusv1.QueryProfileRequest, _ ...grpc.CallOption) (*argusv1.QueryProfileResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.profile, nil
}

// profileResp 构造指定 total/reserved 的 QueryProfile 响应。
func profileResp(total, reserved int32) *argusv1.QueryProfileResponse {
	return &argusv1.QueryProfileResponse{
		Profile: &argusv1.PlatformProfileInfo{
			TotalComputeUnits:    total,
			ReservedComputeUnits: reserved,
		},
	}
}

const testTiers = `[{"fps":5,"units":60},{"fps":15,"units":150},{"fps":25,"units":220}]`

const testConfigSchema = `{
	"type": "object",
	"additionalProperties": false,
	"required": ["confidence_threshold"],
	"properties": {
		"confidence_threshold": {"type": "number", "minimum": 0, "maximum": 1}
	}
}`

// newTaskServiceTestEnv 建 TaskService 测试环境：sqlite + 真实仓储 + fake profile client。
// 返回具体 *taskService 以便测试直接操作 quotaManager（waitQuotaReady）。
func newTaskServiceTestEnv(t *testing.T, total, reserved int32) (*taskService, *gorm.DB, *fakeProfileClient, repository.TaskRepository, *ReportAdapter) {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Camera{},
		&model.Algorithm{},
		&model.AlgorithmVersion{},
		&model.AnalysisTask{},
		&model.AlgorithmInstance{},
		&model.DesiredStateRevision{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}

	taskRepo := repository.NewTaskRepository(db)
	report := NewReportAdapter(taskRepo, zap.NewNop())
	profile := &fakeProfileClient{profile: profileResp(total, reserved)}
	svc := NewTaskService(
		taskRepo,
		repository.NewCameraRepository(db),
		repository.NewAlgorithmRepository(db),
		report,
		profile,
		zap.NewNop(),
	)
	return svc.(*taskService), db, profile, taskRepo, report
}

// waitQuotaReady 轮询等待后台配额获取完成（测试确定性）。
func waitQuotaReady(t *testing.T, svc *taskService) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := svc.quota.current(); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("quota limits never became ready")
}

// seedTaskFixture 写入任务配置模块的基础数据：摄像头、任务、算法（激活版本 + 档位 + schema）。
func seedTaskFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Camera{CameraID: "cam-a", Protocol: model.CameraProtocolRTSP, Name: "大门", RtspURL: "rtsp://a/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h-a"})
	mustCreate(&model.Camera{CameraID: "cam-free", Protocol: model.CameraProtocolRTSP, Name: "空闲", RtspURL: "rtsp://free/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h-f"})
	mustCreate(&model.Algorithm{AlgorithmID: "yolov8n", Name: "yolo", AlgorithmType: "object_detection", ActiveVersion: "1.0.0"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		FPSTiers:     model.JSONRaw(testTiers),
		ConfigSchema: model.JSONRaw(testConfigSchema),
		ManifestRaw:  model.JSONRaw("{}"), // 非空避免 sqlite 把 NULL 扫进 json.RawMessage 报错
	})
	mustCreate(&model.AnalysisTask{CameraID: "cam-a", Name: "大门任务", DesiredEnabled: true, ActualStatus: model.TaskStatusRunning})
}

func currentRevision(t *testing.T, taskRepo repository.TaskRepository) uint64 {
	t.Helper()
	rev, err := taskRepo.CurrentRevision(context.Background())
	if err != nil {
		t.Fatalf("current revision: %v", err)
	}
	return rev
}

func instanceCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&model.AlgorithmInstance{}).Count(&count).Error; err != nil {
		t.Fatalf("count instances: %v", err)
	}
	return count
}

// TestTaskServiceQuotaRejectedNoWrite 配额拒绝：返回 CodeResourceExceeded、
// 错误信息含已用/申请/上限三个数字，且不写库、不 bump revision（零副作用）。
func TestTaskServiceQuotaRejectedNoWrite(t *testing.T) {
	svc, db, _, taskRepo, _ := newTaskServiceTestEnv(t, 200, 100) // 可分配上限 100
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	// fps=25 → units=220 > 上限 100。
	_, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 25,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Rules:       nil,
		Enabled:     true,
	})
	if !errno.Is(err, errno.CodeResourceExceeded) {
		t.Fatalf("err = %v, want CodeResourceExceeded", err)
	}

	// 三个数字：已用 0 / 申请 220 / 上限 100。
	var e *errno.Error
	if !errors.As(err, &e) || len(e.Args()) != 3 {
		t.Fatalf("error = %v, want 3 message args", err)
	}
	msg := errno.MessageWithArgs(errno.DefaultLang, e.Code, e.Args()...)
	for _, want := range []string{"0", "220", "100"} {
		if !containsNumber(msg, want) {
			t.Errorf("message %q does not contain %s", msg, want)
		}
	}

	// 零副作用：无新实例、revision 未变。
	if count := instanceCount(t, db); count != 0 {
		t.Errorf("instance count = %d, want 0", count)
	}
	if rev := currentRevision(t, taskRepo); rev != 0 {
		t.Errorf("revision = %d, want 0 (no bump)", rev)
	}
}

// TestTaskServiceUpdateAtomicity 整份更新：schema/几何/配额任一失败均零写入
// （实例字段与 revision 均不变）；三项全过才整体生效（design §4.2）。
func TestTaskServiceUpdateAtomicity(t *testing.T) {
	svc, db, profile, taskRepo, _ := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	created, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 5,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Rules:       nil,
		Enabled:     true, // 启用态：整份更新才触发配额复校（停用实例不占资源，跳过配额，见 design §7）
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	baseRevision := currentRevision(t, taskRepo)

	validInput := func() *UpdateInstanceInput {
		fps := int32(15)
		return &UpdateInstanceInput{
			AnalysisFPS: &fps,
			ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.8}`),
			Rules:       nil,
		}
	}

	// 1. schema 失败：confidence_threshold 超出 maximum。
	bad := validInput()
	bad.ParamsJSON = json.RawMessage(`{"confidence_threshold":2}`)
	if err := svc.UpdateInstance(ctx, created.InstanceID, bad); !errno.Is(err, errno.CodeInvalidParam) {
		t.Fatalf("schema-invalid update err = %v, want CodeInvalidParam", err)
	}

	// 2. 几何失败：ROI 坐标越界。
	bad = validInput()
	bad.Rules = []model.DetectionRule{
		{Role: model.DetectionRuleRoleROI, Points: []model.DetectionPoint{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1.5, Y: 1}}},
	}
	if err := svc.UpdateInstance(ctx, created.InstanceID, bad); !errno.Is(err, errno.CodeRuleOutOfBounds) {
		t.Fatalf("geom-invalid update err = %v, want CodeRuleOutOfBounds", err)
	}

	// 3. 配额失败：把上限压到 100，fps=25 → 220 > 100。
	profile.set(profileResp(200, 100), nil)
	svc.quota.mu.Lock()
	svc.quota.limits = quotaLimits{total: 200, reserved: 100, fetchedAt: time.Now(), ok: true}
	svc.quota.mu.Unlock()
	bad = validInput()
	badFPS := int32(25)
	bad.AnalysisFPS = &badFPS
	if err := svc.UpdateInstance(ctx, created.InstanceID, bad); !errno.Is(err, errno.CodeResourceExceeded) {
		t.Fatalf("quota-exceeded update err = %v, want CodeResourceExceeded", err)
	}
	// 恢复初始上限（total=1000/reserved=100），供后续合法更新使用。
	svc.quota.mu.Lock()
	svc.quota.limits = quotaLimits{total: 1000, reserved: 100, fetchedAt: time.Now(), ok: true}
	svc.quota.mu.Unlock()

	// 三次失败后实例字段与 revision 必须完全未变（零写入）。
	inst, err := taskRepo.GetInstance(ctx, created.InstanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if inst.AnalysisFPS != 5 || string(inst.ParamsJSON) != `{"confidence_threshold":0.5}` {
		t.Fatalf("instance mutated after rejected updates: fps=%d params=%s", inst.AnalysisFPS, inst.ParamsJSON)
	}
	if rev := currentRevision(t, taskRepo); rev != baseRevision {
		t.Errorf("revision = %d, want %d (no bump)", rev, baseRevision)
	}

	// 4. 合法整份提交：整体生效，revision +1。
	if err := svc.UpdateInstance(ctx, created.InstanceID, validInput()); err != nil {
		t.Fatalf("valid update: %v", err)
	}
	inst, err = taskRepo.GetInstance(ctx, created.InstanceID)
	if err != nil {
		t.Fatalf("get instance after valid update: %v", err)
	}
	if inst.AnalysisFPS != 15 || string(inst.ParamsJSON) != `{"confidence_threshold":0.8}` {
		t.Fatalf("instance = fps %d params %s", inst.AnalysisFPS, inst.ParamsJSON)
	}
	if rev := currentRevision(t, taskRepo); rev != baseRevision+1 {
		t.Errorf("revision = %d, want %d", rev, baseRevision+1)
	}
}

// TestTaskServiceStatusMerge 状态合并：库中 status 为底，内存实时字段命中时填充、
// 未命中（Engine 尚未上报）时返回 null（「等待上报」语义，D6）。
func TestTaskServiceStatusMerge(t *testing.T) {
	svc, db, _, _, report := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	inst, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 15,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	// 内存未命中：实时字段 null。
	items, err := svc.ListInstances(ctx, "cam-a")
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("instances = %d, want 1", len(items))
	}
	if items[0].CurrentFps != nil || items[0].ReportedAt != nil {
		t.Fatalf("unreported instance should have null realtime fields: %+v", items[0])
	}
	tasks, err := svc.ListTasks(ctx, &TaskListQuery{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if tasks.Items[0].LastFrameAt != nil || tasks.Items[0].ReportedAt != nil {
		t.Fatalf("unreported task should have null realtime fields: %+v", tasks.Items[0])
	}

	// Engine 上报：内存命中，实时字段填充，status 落库。
	if err := report.AcceptInstanceState(ctx, &argusv1.InstanceState{
		InstanceId: inst.InstanceID,
		Status:     argusv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING,
		Message:    "running",
		CurrentFps: 14.8,
	}); err != nil {
		t.Fatalf("accept instance state: %v", err)
	}
	if err := report.AcceptTaskState(ctx, &argusv1.TaskState{
		CameraId:            "cam-a",
		Status:              argusv1.TaskStatusCode_TASK_STATUS_RUNNING,
		Message:             "running",
		LastFrameWallTimeNs: 1234567890,
	}); err != nil {
		t.Fatalf("accept task state: %v", err)
	}

	items, err = svc.ListInstances(ctx, "cam-a")
	if err != nil {
		t.Fatalf("list instances after report: %v", err)
	}
	if items[0].CurrentFps == nil || *items[0].CurrentFps != 14.8 {
		t.Fatalf("currentFps = %v, want 14.8", items[0].CurrentFps)
	}
	if items[0].ReportedAt == nil {
		t.Fatal("reportedAt should be set after report")
	}
	if items[0].ActualStatus != model.InstanceStatusRunning {
		t.Fatalf("status = %d, want RUNNING from db", items[0].ActualStatus)
	}

	tasks, err = svc.ListTasks(ctx, &TaskListQuery{})
	if err != nil {
		t.Fatalf("list tasks after report: %v", err)
	}
	if tasks.Items[0].LastFrameAt == nil || tasks.Items[0].LastFrameAt.UnixNano() != 1234567890 {
		t.Fatalf("lastFrameAt = %+v", tasks.Items[0].LastFrameAt)
	}
	if tasks.Items[0].ActualStatus != model.TaskStatusRunning {
		t.Fatalf("task status = %d, want RUNNING from db", tasks.Items[0].ActualStatus)
	}
}

// TestTaskServiceCRUD 任务/实例基础 CRUD：重复建任务、级联删除、available-cameras 过滤。
func TestTaskServiceCRUD(t *testing.T) {
	svc, db, _, taskRepo, _ := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	// 重复建任务 → CodeTaskAlreadyExists。
	if _, err := svc.CreateTask(ctx, &CreateTaskInput{CameraID: "cam-a", Name: "重复"}); !errno.Is(err, errno.CodeTaskAlreadyExists) {
		t.Fatalf("duplicate task err = %v, want CodeTaskAlreadyExists", err)
	}
	// 摄像头不存在 → CodeNotFound。
	if _, err := svc.CreateTask(ctx, &CreateTaskInput{CameraID: "ghost", Name: "x"}); !errno.Is(err, errno.CodeNotFound) {
		t.Fatalf("ghost camera task err = %v, want CodeNotFound", err)
	}

	// available-cameras：cam-a 已建任务被过滤，cam-free 保留。
	available, err := svc.ListAvailableCameras(ctx)
	if err != nil {
		t.Fatalf("list available cameras: %v", err)
	}
	if len(available) != 1 || available[0].CameraID != "cam-free" {
		t.Fatalf("available cameras = %+v, want only cam-free", available)
	}

	// 挂实例 → 启停 → 级联删除。
	inst, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 15,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if inst.ActualStatus != model.InstanceStatusStopped {
		t.Fatalf("disabled instance status = %d, want STOPPED", inst.ActualStatus)
	}
	if err := svc.SetInstanceEnabled(ctx, inst.InstanceID, true); err != nil {
		t.Fatalf("enable instance: %v", err)
	}
	enabled, err := taskRepo.GetInstance(ctx, inst.InstanceID)
	if err != nil {
		t.Fatalf("get enabled instance: %v", err)
	}
	if !enabled.Enabled || enabled.ActualStatus != model.InstanceStatusStarting {
		t.Fatalf("enabled instance = %+v", enabled)
	}
	// 幂等启停：同值不 bump。
	rev := currentRevision(t, taskRepo)
	if err := svc.SetInstanceEnabled(ctx, inst.InstanceID, true); err != nil {
		t.Fatalf("idempotent enable: %v", err)
	}
	if got := currentRevision(t, taskRepo); got != rev {
		t.Errorf("idempotent enable bumped revision %d -> %d", rev, got)
	}

	// 删除任务级联删除实例（D9）。
	if err := svc.DeleteTask(ctx, "cam-a"); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if count := instanceCount(t, db); count != 0 {
		t.Errorf("instances after cascade delete = %d, want 0", count)
	}
	if _, err := taskRepo.GetTaskByCameraID(ctx, "cam-a"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("task still exists: %v", err)
	}
	// 任务删除后摄像头重新可用。
	available, err = svc.ListAvailableCameras(ctx)
	if err != nil {
		t.Fatalf("list available cameras after delete: %v", err)
	}
	if len(available) != 2 {
		t.Fatalf("available cameras = %+v, want both", available)
	}
}

// TestTaskServiceEngineUnavailableRejectsEnable 从未成功获取配额上限时拒绝启用实例
// 并返回 CodeEngineUnavailable（design §7）；已有实例创建（含 FPS 档位校验）不受影响。
func TestTaskServiceEngineUnavailableRejectsEnable(t *testing.T) {
	svc, db, profile, _, _ := newTaskServiceTestEnv(t, 1000, 100)
	profile.set(nil, errors.New("engine socket missing"))
	seedTaskFixture(t, db)
	// 不 waitQuotaReady：配额永不成功。
	ctx := context.Background()

	_, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 15,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     true,
	})
	if !errno.Is(err, errno.CodeEngineUnavailable) {
		t.Fatalf("enable with engine down err = %v, want CodeEngineUnavailable", err)
	}
}

// TestTaskServiceEngineUnavailableAllowsDisabledWrite Engine 离线（配额从未获取）时：
// 停用实例的创建与配置更新仍被允许（离线编排），仅启用操作被拒（design §7）。
func TestTaskServiceEngineUnavailableAllowsDisabledWrite(t *testing.T) {
	svc, db, profile, taskRepo, _ := newTaskServiceTestEnv(t, 1000, 100)
	profile.set(nil, errors.New("engine socket missing"))
	seedTaskFixture(t, db)
	// 不 waitQuotaReady：配额永不成功。
	ctx := context.Background()

	// 停用实例创建成功（不触发配额校验）。
	inst, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 15,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("create disabled instance with engine down: %v", err)
	}
	if inst.Enabled || inst.ActualStatus != model.InstanceStatusStopped {
		t.Fatalf("disabled instance = %+v", inst)
	}

	// 停用实例整份更新成功（不触发配额校验）。
	fps := int32(25)
	if err := svc.UpdateInstance(ctx, inst.InstanceID, &UpdateInstanceInput{
		AnalysisFPS: &fps,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.8}`),
		Rules:       nil,
	}); err != nil {
		t.Fatalf("update disabled instance with engine down: %v", err)
	}

	// 启用被拒（CodeEngineUnavailable），且不写库不 bump（零副作用）。
	rev := currentRevision(t, taskRepo)
	if err := svc.SetInstanceEnabled(ctx, inst.InstanceID, true); !errno.Is(err, errno.CodeEngineUnavailable) {
		t.Fatalf("enable with engine down err = %v, want CodeEngineUnavailable", err)
	}
	if got := currentRevision(t, taskRepo); got != rev {
		t.Errorf("rejected enable bumped revision %d -> %d", rev, got)
	}
}

// TestTaskServiceEnableRejectsCorruptedRules 存储 rules_json 损坏时启用实例必须
// fail closed（CodeInternal），不得静默降级为空规则后放行——与快照组装语义一致。
func TestTaskServiceEnableRejectsCorruptedRules(t *testing.T) {
	svc, db, _, taskRepo, _ := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	inst, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 5,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	// 直接写坏 rules_json（模拟存储损坏）。
	if err := db.Model(&model.AlgorithmInstance{}).
		Where("instance_id = ?", inst.InstanceID).
		Update("rules_json", []byte(`{"not":"an array"}`)).Error; err != nil {
		t.Fatalf("corrupt rules_json: %v", err)
	}

	// 启用被拒：CodeInternal，不写库不 bump（零副作用）。
	rev := currentRevision(t, taskRepo)
	if err := svc.SetInstanceEnabled(ctx, inst.InstanceID, true); !errno.Is(err, errno.CodeInternal) {
		t.Fatalf("enable with corrupted rules err = %v, want CodeInternal", err)
	}
	if got := currentRevision(t, taskRepo); got != rev {
		t.Errorf("rejected enable bumped revision %d -> %d", rev, got)
	}
	got, err := taskRepo.GetInstance(ctx, inst.InstanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.Enabled {
		t.Fatal("instance should stay disabled after rejected enable")
	}
}

// TestTaskServiceFPSTierExceeded 超过最高档位直接拒绝（CodeFPSTierExceeded），不钳位。
func TestTaskServiceFPSTierExceeded(t *testing.T) {
	svc, db, _, _, _ := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)

	_, err := svc.CreateInstance(context.Background(), &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 26, // 最高档 25
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	})
	if !errno.Is(err, errno.CodeFPSTierExceeded) {
		t.Fatalf("err = %v, want CodeFPSTierExceeded", err)
	}
}

// TestTaskServiceListTasksWithInstances 验证 ListTasks 一次查询聚合返回挂载的实例摘要（1:N 管道槽位数据）。
func TestTaskServiceListTasksWithInstances(t *testing.T) {
	svc, db, _, _, report := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db)
	waitQuotaReady(t, svc)
	ctx := context.Background()

	// cam-a 创建一个停用实例
	inst1, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 10,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("create inst1: %v", err)
	}

	// 模拟上报 runtime 实时数据
	report.AcceptInstanceState(context.Background(), &argusv1.InstanceState{
		InstanceId: inst1.InstanceID,
		Status:     argusv1.InstanceStatusCode_INSTANCE_STATUS_RUNNING,
		CurrentFps: 9.8,
	})

	res, err := svc.ListTasks(ctx, &TaskListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("expected at least 1 task")
	}

	// 找到 cam-a 的任务
	var target *TaskItem
	for _, item := range res.Items {
		if item.CameraID == "cam-a" {
			target = item
			break
		}
	}
	if target == nil {
		t.Fatal("cam-a task not found")
	}
	if target.InstanceCount != 1 || len(target.Instances) != 1 {
		t.Fatalf("instanceCount=%d, len(instances)=%d, want 1", target.InstanceCount, len(target.Instances))
	}
	brief := target.Instances[0]
	if brief.InstanceID != inst1.InstanceID || brief.AlgorithmID != "yolov8n" {
		t.Fatalf("unexpected brief: %+v", brief)
	}
	if brief.CurrentFPS == nil || *brief.CurrentFPS < 9.0 {
		t.Fatalf("expected currentFps to be merged, got %+v", brief.CurrentFPS)
	}
}

// containsNumber 检查消息里是否包含数字 token（避免子串误判，按空白/标点切分）。
func containsNumber(msg, number string) bool {
	fields := splitMessageFields(msg)
	for _, f := range fields {
		if f == number {
			return true
		}
	}
	return false
}

func splitMessageFields(msg string) []string {
	return strings.FieldsFunc(msg, func(r rune) bool {
		return r < '0' || r > '9'
	})
}

// TestTaskServiceTaskStats 概览统计：任务/实例计数 + 算力负载。
// 数据源：仓储计数 + sumUsedUnits（配额计价行）+ quotaManager 缓存上限。
func TestTaskServiceTaskStats(t *testing.T) {
	svc, db, _, taskRepo, _ := newTaskServiceTestEnv(t, 1000, 100)
	seedTaskFixture(t, db) // cam-a RUNNING + yolov8n@1.0.0（testTiers: 5→60/15→150/25→220）
	waitQuotaReady(t, svc)
	ctx := context.Background()

	// 再补两个任务：cam-b RUNNING、cam-c STOPPED（直接写库，绕过摄像头存在性校验）
	for i, camID := range []string{"cam-b", "cam-c"} {
		task := &model.AnalysisTask{CameraID: camID, Name: "任务" + camID}
		if i == 0 {
			task.ActualStatus = model.TaskStatusRunning
		} else {
			task.ActualStatus = model.TaskStatusStopped
		}
		if err := taskRepo.CreateTask(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", camID, err)
		}
	}
	// 实例：i-1 启用 25fps（→220 units）、i-2 停用 10fps（不计）
	if _, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 25,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     true,
	}); err != nil {
		t.Fatalf("create enabled instance: %v", err)
	}
	if _, err := svc.CreateInstance(ctx, &CreateInstanceInput{
		CameraID:    "cam-b",
		AlgorithmID: "yolov8n",
		AnalysisFPS: 10,
		ParamsJSON:  json.RawMessage(`{"confidence_threshold":0.5}`),
		Enabled:     false,
	}); err != nil {
		t.Fatalf("create disabled instance: %v", err)
	}

	stats, err := svc.TaskStats(ctx)
	if err != nil {
		t.Fatalf("task stats: %v", err)
	}
	if stats.TotalTasks != 3 || stats.RunningTasks != 2 {
		t.Fatalf("tasks = total %d running %d, want 3/2", stats.TotalTasks, stats.RunningTasks)
	}
	if stats.TotalInstances != 2 || stats.EnabledInstances != 1 {
		t.Fatalf("instances = total %d enabled %d, want 2/1", stats.TotalInstances, stats.EnabledInstances)
	}
	if stats.UsedUnits != 220 {
		t.Fatalf("used units = %d, want 220", stats.UsedUnits)
	}
	if stats.TotalUnits != 1000 || stats.ReservedUnits != 100 || stats.AvailableUnits != 900 {
		t.Fatalf("units = total %d reserved %d available %d, want 1000/100/900",
			stats.TotalUnits, stats.ReservedUnits, stats.AvailableUnits)
	}
}

// TestTaskServiceTaskStatsNoQuota 引擎未上报算力（quota 未就绪）时：TotalUnits=0，
// 任务/实例计数仍返回，前端应据此展示负载为不可用而非报错。
func TestTaskServiceTaskStatsNoQuota(t *testing.T) {
	svc, db, _, _, _ := newTaskServiceTestEnv(t, 0, 0) // total=0 视为未成功获取
	seedTaskFixture(t, db)
	ctx := context.Background()

	// 等待 quota 后台循环放弃（total=0 不会置 ok），随后直接调用 TaskStats。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := svc.quota.current(); ok {
			t.Fatal("quota unexpectedly ready with total=0")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stats, err := svc.TaskStats(ctx)
	if err != nil {
		t.Fatalf("task stats: %v", err)
	}
	if stats.TotalTasks != 1 || stats.TotalUnits != 0 {
		t.Fatalf("stats = total %d units %d, want 1/0", stats.TotalTasks, stats.TotalUnits)
	}
	if stats.AvailableUnits != 0 {
		t.Fatalf("available units = %d, want 0 when quota not ready", stats.AvailableUnits)
	}
}
