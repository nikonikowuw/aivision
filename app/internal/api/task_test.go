package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/api"
	"niko-vue-admin/app/internal/middleware"
	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

// fakeProfileClient 实现 service.ProfileClient，供 API 测试注入。
type fakeProfileClient struct {
	profile *aivisionv1.QueryProfileResponse
	err     error
}

func (f *fakeProfileClient) QueryProfile(_ context.Context, _ *aivisionv1.QueryProfileRequest, _ ...grpc.CallOption) (*aivisionv1.QueryProfileResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.profile, nil
}

func taskProfileResp(total, reserved int32) *aivisionv1.QueryProfileResponse {
	return &aivisionv1.QueryProfileResponse{
		Profile: &aivisionv1.PlatformProfileInfo{
			TotalComputeUnits:    total,
			ReservedComputeUnits: reserved,
		},
	}
}

const taskTestTiers = `[{"fps":5,"units":60},{"fps":15,"units":150},{"fps":25,"units":220}]`

const taskTestConfigSchema = `{
	"type": "object",
	"additionalProperties": false,
	"required": ["confidence_threshold"],
	"properties": {
		"confidence_threshold": {"type": "number", "minimum": 0, "maximum": 1}
	}
}`

// setupTaskAPIEngine 装配 task handler 路由（不带认证/权限中间件），
// 返回 engine 与 db（供测试 seed 与断言）。
func setupTaskAPIEngine(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "task")
	// 期望状态版本计数器单行（对齐 000019 迁移初始化；CreateTask/Update 会 bump revision）
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}
	// 基础数据：两个摄像头 + 一个已激活版本算法
	if err := db.Create(&model.Camera{CameraID: "cam-1", Protocol: model.CameraProtocolRTSP, Name: "大门", RtspURL: "rtsp://1/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h1"}).Error; err != nil {
		t.Fatalf("seed camera cam-1: %v", err)
	}
	if err := db.Create(&model.Camera{CameraID: "cam-2", Protocol: model.CameraProtocolRTSP, Name: "侧门", RtspURL: "rtsp://2/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h2"}).Error; err != nil {
		t.Fatalf("seed camera cam-2: %v", err)
	}
	if err := db.Create(&model.Algorithm{AlgorithmID: "yolov8n", Name: "yolo", AlgorithmType: "object_detection", ActiveVersion: "1.0.0"}).Error; err != nil {
		t.Fatalf("seed algorithm: %v", err)
	}
	if err := db.Create(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		PlatformID:   "macos",
		FPSTiers:     json.RawMessage(taskTestTiers),
		ConfigSchema: json.RawMessage(taskTestConfigSchema),
		ManifestRaw:  json.RawMessage("{}"),
	}).Error; err != nil {
		t.Fatalf("seed algorithm version: %v", err)
	}

	taskRepo := repository.NewTaskRepository(db)
	report := service.NewReportAdapter(taskRepo, zap.NewNop())
	profile := &fakeProfileClient{profile: taskProfileResp(1000, 100)}
	svc := service.NewTaskService(
		taskRepo,
		repository.NewCameraRepository(db),
		repository.NewAlgorithmRepository(db),
		report,
		profile,
		zap.NewNop(),
	)
	handler := api.NewTaskHandler(svc)

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	grp := r.Group("/api/task")
	{
		grp.GET("/list", handler.ListTasks)
		grp.GET("/stats", handler.GetTaskStats)
		grp.POST("", handler.CreateTask)
		grp.DELETE("/batch", handler.BatchDeleteTasks)
		grp.PUT("/:cameraId", handler.UpdateTask)
		grp.PUT("/:cameraId/enabled", handler.SetTaskEnabled)
		grp.DELETE("/:cameraId", handler.DeleteTask)
		grp.GET("/available-cameras", handler.ListAvailableCameras)
		grp.GET("/instance/list", handler.ListInstances)
		grp.POST("/instance", handler.CreateInstance)
		grp.PUT("/instance/:instanceId", handler.UpdateInstance)
		grp.PUT("/instance/:instanceId/enabled", handler.SetInstanceEnabled)
		grp.DELETE("/instance/:instanceId", handler.DeleteInstance)
	}
	return r, db
}

func respCode(t *testing.T, body []byte) int {
	t.Helper()
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, body)
	}
	return resp.Code
}

// TestTaskHandlerTaskCRUD 任务 CRUD：创建、重复拒绝、available-cameras 过滤、
// 列表、删除后摄像头重新可用。
func TestTaskHandlerTaskCRUD(t *testing.T) {
	engine, _ := setupTaskAPIEngine(t)

	// 1. 创建任务
	rec := doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-1","name":"大门任务"}`)
	if rec.Code != http.StatusOK || respCode(t, rec.Body.Bytes()) != errno.CodeOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			CameraID       string `json:"cameraId"`
			Name           string `json:"name"`
			DesiredEnabled bool   `json:"desiredEnabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.Data.CameraID != "cam-1" || created.Data.Name != "大门任务" || created.Data.DesiredEnabled {
		t.Fatalf("create data = %+v", created.Data)
	}

	// 2. 重复创建 → CodeTaskAlreadyExists
	rec = doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-1","name":"重复"}`)
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeTaskAlreadyExists {
		t.Fatalf("duplicate create code = %d, want %d", code, errno.CodeTaskAlreadyExists)
	}

	// 3. available-cameras 只含未建任务的 cam-2
	rec = doJSON(t, engine, http.MethodGet, "/api/task/available-cameras", "")
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("available-cameras code = %d", code)
	}
	var available struct {
		Data []struct {
			CameraID string `json:"cameraId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &available); err != nil {
		t.Fatalf("unmarshal available: %v", err)
	}
	if len(available.Data) != 1 || available.Data[0].CameraID != "cam-2" {
		t.Fatalf("available = %+v, want [cam-2]", available.Data)
	}

	// 4. 列表分页
	rec = doJSON(t, engine, http.MethodGet, "/api/task/list?page=1&pageSize=10", "")
	var page struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if page.Data.Total != 1 {
		t.Fatalf("list total = %d, want 1", page.Data.Total)
	}

	// 5. 删除任务
	rec = doJSON(t, engine, http.MethodDelete, "/api/task/cam-1", "")
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("delete code = %d", code)
	}

	// 6. 删除后 cam-1 重新可建任务
	rec = doJSON(t, engine, http.MethodGet, "/api/task/available-cameras", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &available); err != nil {
		t.Fatalf("unmarshal available after delete: %v", err)
	}
	if len(available.Data) != 2 {
		t.Fatalf("available after delete = %+v, want 2", available.Data)
	}

	// 7. 批量删除测试
	doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-1","name":"大门任务"}`)
	doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-2","name":"车库任务"}`)
	rec = doJSON(t, engine, http.MethodDelete, "/api/task/batch", `{"cameraIds":["cam-1","cam-2"]}`)
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("batch delete code = %d body=%s", code, rec.Body.String())
	}
	rec = doJSON(t, engine, http.MethodGet, "/api/task/available-cameras", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &available); err != nil {
		t.Fatalf("unmarshal available after batch delete: %v", err)
	}
	if len(available.Data) != 2 {
		t.Fatalf("available after batch delete = %+v, want 2", available.Data)
	}
}

// TestTaskHandlerInstanceCRUD 实例 CRUD：停用创建（无需配额）、列表、启用
// （轮询等待配额缓存就绪）、整份更新、删除。
func TestTaskHandlerInstanceCRUD(t *testing.T) {
	engine, _ := setupTaskAPIEngine(t)

	if rec := doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-1","name":"大门任务"}`); respCode(t, rec.Body.Bytes()) != errno.CodeOK {
		t.Fatalf("create task failed: %s", rec.Body.String())
	}

	// 1. 创建停用实例（不触发配额校验，无需等待配额就绪）
	rec := doJSON(t, engine, http.MethodPost, "/api/task/instance",
		`{"cameraId":"cam-1","algorithmId":"yolov8n","analysisFps":15,"paramsJson":{"confidence_threshold":0.5},"rules":[],"enabled":false}`)
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("create instance code = %d body=%s", code, rec.Body.String())
	}
	var created struct {
		Data struct {
			InstanceID string `json:"instanceId"`
			Enabled    bool   `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create instance: %v", err)
	}
	if created.Data.InstanceID == "" || created.Data.Enabled {
		t.Fatalf("create instance data = %+v", created.Data)
	}
	instanceID := created.Data.InstanceID

	// 2. 实例列表
	rec = doJSON(t, engine, http.MethodGet, "/api/task/instance/list?cameraId=cam-1", "")
	var list struct {
		Data []struct {
			InstanceID string `json:"instanceId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal instance list: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].InstanceID != instanceID {
		t.Fatalf("instance list = %+v, want [%s]", list.Data, instanceID)
	}

	// 3. 启用实例（轮询直到后台配额缓存就绪；就绪前返回 CodeEngineUnavailable）
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec = doJSON(t, engine, http.MethodPut, "/api/task/instance/"+instanceID+"/enabled", `{"enabled":true}`)
		code := respCode(t, rec.Body.Bytes())
		if code == errno.CodeOK {
			break
		}
		if code != errno.CodeEngineUnavailable {
			t.Fatalf("enable instance code = %d body=%s", code, rec.Body.String())
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota never became ready: %s", rec.Body.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 4. 整份更新
	rec = doJSON(t, engine, http.MethodPut, "/api/task/instance/"+instanceID,
		`{"analysisFps":5,"paramsJson":{"confidence_threshold":0.8},"rules":[]}`)
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("update instance code = %d body=%s", code, rec.Body.String())
	}

	// 5. 删除实例
	rec = doJSON(t, engine, http.MethodDelete, "/api/task/instance/"+instanceID, "")
	if code := respCode(t, rec.Body.Bytes()); code != errno.CodeOK {
		t.Fatalf("delete instance code = %d body=%s", code, rec.Body.String())
	}

	// 6. 删除后列表为空
	rec = doJSON(t, engine, http.MethodGet, "/api/task/instance/list?cameraId=cam-1", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal instance list after delete: %v", err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("instance list after delete = %+v, want empty", list.Data)
	}
}

// TestTaskHandlerTaskStats 概览统计接口：先等 quota 就绪，再建任务与启用实例，
// 校验计数与算力负载字段（used/total/reserved/available）。
func TestTaskHandlerTaskStats(t *testing.T) {
	engine, _ := setupTaskAPIEngine(t)

	// 轮询 /stats 直到 quota 就绪（totalUnits>0），避免后续启用实例被配额预检拒绝
	var stats struct {
		Data struct {
			TotalTasks       int64 `json:"totalTasks"`
			RunningTasks     int64 `json:"runningTasks"`
			TotalInstances   int64 `json:"totalInstances"`
			EnabledInstances int64 `json:"enabledInstances"`
			UsedUnits        int   `json:"usedUnits"`
			TotalUnits       int   `json:"totalUnits"`
			ReservedUnits    int   `json:"reservedUnits"`
			AvailableUnits   int   `json:"availableUnits"`
		} `json:"data"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := doJSON(t, engine, http.MethodGet, "/api/task/stats", "")
		if respCode(t, rec.Body.Bytes()) != errno.CodeOK {
			t.Fatalf("stats code not ok: %s", rec.Body.String())
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &stats)
		if stats.Data.TotalUnits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stats totalUnits never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 建任务 cam-1 + 启用实例 25fps（→220 units）
	rec := doJSON(t, engine, http.MethodPost, "/api/task", `{"cameraId":"cam-1","name":"大门任务"}`)
	if respCode(t, rec.Body.Bytes()) != errno.CodeOK {
		t.Fatalf("create task failed: %s", rec.Body.String())
	}
	rec = doJSON(t, engine, http.MethodPost, "/api/task/instance",
		`{"cameraId":"cam-1","algorithmId":"yolov8n","analysisFps":25,"paramsJson":{"confidence_threshold":0.5},"enabled":true}`)
	if respCode(t, rec.Body.Bytes()) != errno.CodeOK {
		t.Fatalf("create enabled instance failed: %s", rec.Body.String())
	}

	rec = doJSON(t, engine, http.MethodGet, "/api/task/stats", "")
	if respCode(t, rec.Body.Bytes()) != errno.CodeOK {
		t.Fatalf("stats after write code not ok: %s", rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if stats.Data.TotalTasks != 1 || stats.Data.TotalInstances != 1 || stats.Data.EnabledInstances != 1 {
		t.Fatalf("stats counts = %+v, want total/inst/enabled 1/1/1", stats.Data)
	}
	if stats.Data.UsedUnits != 220 {
		t.Fatalf("used units = %d, want 220", stats.Data.UsedUnits)
	}
	if stats.Data.TotalUnits != 1000 || stats.Data.ReservedUnits != 100 || stats.Data.AvailableUnits != 900 {
		t.Fatalf("units = %+v, want 1000/100/900", stats.Data)
	}
}
