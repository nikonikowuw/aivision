package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/ntp"
	"argus/app/internal/pkg/storage"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/router"
	"argus/app/internal/service"
)

// fakeProbeClient 实现 service.CameraProbeClient，供 API 测试注入。
type fakeProbeClient struct {
	onCall func(ctx context.Context, req *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error)
}

func (f *fakeProbeClient) ProbeCamera(ctx context.Context, req *argusv1.ProbeCameraRequest, _ ...grpc.CallOption) (*argusv1.ProbeCameraResponse, error) {
	if f.onCall != nil {
		return f.onCall(ctx, req)
	}
	return &argusv1.ProbeCameraResponse{}, nil
}

func (f *fakeProbeClient) StartCameraPreview(ctx context.Context, req *argusv1.StartCameraPreviewRequest, _ ...grpc.CallOption) (*argusv1.StartCameraPreviewResponse, error) {
	return &argusv1.StartCameraPreviewResponse{
		StreamPath: "/live/" + req.CameraId + "_main.live.flv",
		HttpPort:   8080,
		WsPort:     8080,
	}, nil
}

func (f *fakeProbeClient) StopCameraPreview(ctx context.Context, req *argusv1.StopCameraPreviewRequest, _ ...grpc.CallOption) (*argusv1.StopCameraPreviewResponse, error) {
	return &argusv1.StopCameraPreviewResponse{}, nil
}

type cameraAPIResp struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

// setupCameraAPIEngine 装配 camera handler 路由（不带认证/权限中间件），返回可注入 fake probe 的 engine。
func setupCameraAPIEngine(t *testing.T) (*gin.Engine, *fakeProbeClient) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "camera")
	repo := repository.NewCameraRepository(db)
	fake := &fakeProbeClient{}
	srv := service.NewCameraService(repo, repository.NewTaskRepository(db), fake)
	handler := api.NewCameraHandler(srv)

	r := gin.New()
	r.Use(middleware.ErrorHandler())

	grp := r.Group("/api/camera")
	{
		grp.GET("/page", handler.GetPage)
		grp.POST("", handler.CreateCamera)
		grp.DELETE("/batch", handler.BatchDeleteCamera)
		grp.PUT("/:id", handler.UpdateCamera)
		grp.DELETE("/:id", handler.DeleteCamera)
		grp.POST("/probe", handler.ProbeCamera)
	}
	return r, fake
}

func doJSON(t *testing.T, engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestCameraHandlerCRUD(t *testing.T) {
	engine, _ := setupCameraAPIEngine(t)

	// 1. 创建
	createBody := `{"name":"门口","rtspUrl":"rtsp://user:p%40ss@192.168.1.10/live","remark":"正门"}`
	rec := doJSON(t, engine, http.MethodPost, "/api/camera", createBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Code int `json:"code"`
		Data struct {
			ID              uint64 `json:"id"`
			CameraID        string `json:"cameraId"`
			Name            string `json:"name"`
			RtspURL         string `json:"rtspUrl"`
			Protocol        string `json:"protocol"`
			LastProbeStatus string `json:"lastProbeStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.Code != errno.CodeOK || created.Data.ID == 0 || created.Data.CameraID == "" {
		t.Fatalf("create resp = %+v", created)
	}
	if created.Data.RtspURL != "rtsp://user:p%40ss@192.168.1.10/live" || created.Data.LastProbeStatus != "never" {
		t.Fatalf("create data = %+v", created.Data)
	}

	// 2. 分页查询（含 name 模糊）
	rec = doJSON(t, engine, http.MethodGet, "/api/camera/page?page=1&pageSize=10&name=门口", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
			Total int64             `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	if page.Code != errno.CodeOK || page.Data.Total != 1 || len(page.Data.Items) != 1 {
		t.Fatalf("page resp = %+v", page)
	}

	// 3. 更新
	id := created.Data.ID
	updateBody := `{"name":"东门","rtspUrl":"rtsp://192.168.1.11/live"}`
	rec = doJSON(t, engine, http.MethodPut, fmt.Sprintf("/api/camera/%d", id), updateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Code int `json:"code"`
		Data struct {
			Name    string `json:"name"`
			RtspURL string `json:"rtspUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	if updated.Data.Name != "东门" || updated.Data.RtspURL != "rtsp://192.168.1.11/live" {
		t.Fatalf("update data = %+v", updated.Data)
	}

	// 4. 删除
	rec = doJSON(t, engine, http.MethodDelete, fmt.Sprintf("/api/camera/%d", id), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var deleted cameraAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil {
		t.Fatalf("unmarshal delete: %v", err)
	}
	if deleted.Code != errno.CodeOK {
		t.Fatalf("delete code = %d", deleted.Code)
	}
	// 删除后分页为空
	rec = doJSON(t, engine, http.MethodGet, "/api/camera/page", "")
	var after cameraAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("unmarshal page after delete: %v", err)
	}
	if !bytes.Contains(after.Data, []byte(`"total":0`)) {
		t.Fatalf("page after delete = %s", rec.Body.String())
	}
}

func TestCameraHandlerValidation(t *testing.T) {
	engine, _ := setupCameraAPIEngine(t)

	invalid := []string{
		`{"name":"","rtspUrl":"rtsp://192.168.1.10/live"}`,
		`{"name":"cam","rtspUrl":""}`,
		`{"name":"cam","rtspUrl":"http://192.168.1.10/live"}`,
		`{"name":"cam","rtspUrl":"rtsp://192.168.1.10/live#frag"}`,
		`{"name":"cam","rtspUrl":"rtsp://192.168.1.10/live%2"}`,
	}
	for _, body := range invalid {
		rec := doJSON(t, engine, http.MethodPost, "/api/camera", body)
		var resp cameraAPIResp
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
		}
		if resp.Code != errno.CodeInvalidParam {
			t.Errorf("body %s → code %d, want %d (resp=%s)", body, resp.Code, errno.CodeInvalidParam, rec.Body.String())
		}
	}
}

func TestCameraHandlerNotFound(t *testing.T) {
	engine, _ := setupCameraAPIEngine(t)

	rec := doJSON(t, engine, http.MethodPut, "/api/camera/9999", `{"name":"x","rtspUrl":"rtsp://1.2.3.4/live"}`)
	var resp cameraAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	if resp.Code != errno.CodeNotFound {
		t.Fatalf("update missing id code = %d, want %d", resp.Code, errno.CodeNotFound)
	}

	rec = doJSON(t, engine, http.MethodDelete, "/api/camera/9999", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal delete: %v", err)
	}
	if resp.Code != errno.CodeNotFound {
		t.Fatalf("delete missing id code = %d, want %d", resp.Code, errno.CodeNotFound)
	}

	rec = doJSON(t, engine, http.MethodPost, "/api/camera/probe", `{"id":9999,"protocol":"rtsp","rtspUrl":"rtsp://1.2.3.4/live"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal probe: %v", err)
	}
	if resp.Code != errno.CodeNotFound {
		t.Fatalf("probe missing id code = %d, want %d", resp.Code, errno.CodeNotFound)
	}
}

func TestCameraHandlerProbe(t *testing.T) {
	engine, fake := setupCameraAPIEngine(t)

	// 1. 无 id 测活成功 → code=0，persisted=false
	fake.onCall = func(_ context.Context, req *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		if req.GetProtocol() != "rtsp" || req.GetUrl() != "rtsp://192.168.1.10/live" {
			t.Fatalf("engine req = %q/%q", req.GetProtocol(), req.GetUrl())
		}
		return &argusv1.ProbeCameraResponse{
			Status:            "success",
			SelectedTransport: "tcp",
			Codec:             "H264",
			Width:             1920,
			Height:            1080,
			Fps:               25,
			ElapsedMs:         850,
		}, nil
	}
	rec := doJSON(t, engine, http.MethodPost, "/api/camera/probe", `{"protocol":"rtsp","rtspUrl":"rtsp://192.168.1.10/live"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var probe struct {
		Code int `json:"code"`
		Data struct {
			Status            string `json:"status"`
			SelectedTransport string `json:"selectedTransport"`
			Codec             string `json:"codec"`
			Width             uint32 `json:"width"`
			Height            uint32 `json:"height"`
			Persisted         bool   `json:"persisted"`
			Stale             bool   `json:"stale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &probe); err != nil {
		t.Fatalf("unmarshal probe: %v", err)
	}
	if probe.Code != errno.CodeOK || probe.Data.Status != "success" || probe.Data.Persisted || probe.Data.Stale {
		t.Fatalf("probe resp = %+v", probe)
	}

	// 2. 测活失败 → code=0 + status=failed + 稳定码
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return &argusv1.ProbeCameraResponse{
			Status:      "failed",
			FailureCode: "RTSP_CONNECT_FAILED",
			ElapsedMs:   5000,
			Attempts: []*argusv1.ProbeAttempt{
				{Transport: "tcp", ElapsedMs: 5000, FailureCode: "RTSP_CONNECT_FAILED"},
			},
		}, nil
	}
	rec = doJSON(t, engine, http.MethodPost, "/api/camera/probe", `{"protocol":"rtsp","rtspUrl":"rtsp://192.168.1.10/live"}`)
	var probeFail struct {
		Code int `json:"code"`
		Data struct {
			Status      string `json:"status"`
			FailureCode string `json:"failureCode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &probeFail); err != nil {
		t.Fatalf("unmarshal probe fail: %v", err)
	}
	if probeFail.Code != errno.CodeOK || probeFail.Data.Status != "failed" || probeFail.Data.FailureCode != "RTSP_CONNECT_FAILED" {
		t.Fatalf("probe fail resp = %+v (body=%s)", probeFail, rec.Body.String())
	}

	// 3. 已保存 id + 指纹一致 → persisted=true
	createRec := doJSON(t, engine, http.MethodPost, "/api/camera", `{"name":"门口","rtspUrl":"rtsp://192.168.1.10/live"}`)
	var created struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return &argusv1.ProbeCameraResponse{Status: "success", SelectedTransport: "udp", Codec: "H265"}, nil
	}
	rec = doJSON(t, engine, http.MethodPost, "/api/camera/probe",
		fmt.Sprintf(`{"id":%d,"protocol":"rtsp","rtspUrl":"rtsp://192.168.1.10/live"}`, created.Data.ID))
	var probePersist struct {
		Code int `json:"code"`
		Data struct {
			Persisted bool `json:"persisted"`
			Stale     bool `json:"stale"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &probePersist); err != nil {
		t.Fatalf("unmarshal probe persist: %v", err)
	}
	if probePersist.Code != errno.CodeOK || !probePersist.Data.Persisted || probePersist.Data.Stale {
		t.Fatalf("probe persist resp = %+v (body=%s)", probePersist, rec.Body.String())
	}
}

func TestCameraHandlerBatchDelete(t *testing.T) {
	engine, _ := setupCameraAPIEngine(t)

	ids := make([]uint64, 0, 2)
	for _, name := range []string{"A", "B"} {
		rec := doJSON(t, engine, http.MethodPost, "/api/camera",
			fmt.Sprintf(`{"name":%q,"rtspUrl":"rtsp://1.2.3.%s/live"}`, name, map[string]string{"A": "1", "B": "2"}[name]))
		var created struct {
			Data struct {
				ID uint64 `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		ids = append(ids, created.Data.ID)
	}

	body := fmt.Sprintf(`{"ids":[%d,%d]}`, ids[0], ids[1])
	rec := doJSON(t, engine, http.MethodDelete, "/api/camera/batch", body)
	var resp cameraAPIResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal batch: %v", err)
	}
	if resp.Code != errno.CodeOK {
		t.Fatalf("batch delete code = %d (body=%s)", resp.Code, rec.Body.String())
	}

	// 列表为空
	rec = doJSON(t, engine, http.MethodGet, "/api/camera/page", "")
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"total":0`)) {
		t.Fatalf("page after batch = %s", rec.Body.String())
	}
}

// TestCameraAuthAndPermission 验证摄像头接口的认证与权限码（super 放行、无权限用户 403）。
func TestCameraAuthAndPermission(t *testing.T) {
	engine, db := setupCameraFullApp(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	admin := model.User{Username: "admin", Password: string(hash), Nickname: "管理员", Status: model.StatusEnabled}
	db.Create(&admin)
	superRole := model.Role{Name: "超级管理员", Code: model.RoleSuperCode, Status: model.StatusEnabled}
	db.Create(&superRole)
	db.Create(&model.UserRole{UserID: admin.ID, RoleID: superRole.ID})

	normalUser := model.User{Username: "normal", Password: string(hash), Nickname: "普通用户", Status: model.StatusEnabled}
	db.Create(&normalUser)
	normalRole := model.Role{Name: "普通角色", Code: "normal", Status: model.StatusEnabled}
	db.Create(&normalRole)
	db.Create(&model.UserRole{UserID: normalUser.ID, RoleID: normalRole.ID})

	login := func(username string) string {
		t.Helper()
		body := fmt.Sprintf(`{"username":%q,"password":"admin123"}`, username)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login %s status = %d, body = %s", username, rec.Code, rec.Body.String())
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				AccessToken string `json:"accessToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal login: %v", err)
		}
		return resp.Data.AccessToken
	}

	adminToken := login("admin")
	normalToken := login("normal")

	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// 1. 无 token → 401
	if rec := do(http.MethodGet, "/api/camera/page", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/camera/page no token status = %d, want 401", rec.Code)
	}

	// 2. super 用户全放行
	if rec := do(http.MethodGet, "/api/camera/page", adminToken, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/camera/page (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/camera", adminToken, `{"name":"x","rtspUrl":"rtsp://1.2.3.4/live"}`); rec.Code != http.StatusOK {
		t.Errorf("POST /api/camera (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/camera/probe", adminToken, `{"protocol":"rtsp","rtspUrl":"rtsp://1.2.3.4/live"}`); rec.Code != http.StatusOK {
		t.Errorf("POST /api/camera/probe (admin) status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// 3. 无权限用户 → 403
	if rec := do(http.MethodGet, "/api/camera/page", normalToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/camera/page (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/camera", normalToken, `{"name":"x","rtspUrl":"rtsp://1.2.3.4/live"}`); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/camera (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/camera/probe", normalToken, `{"protocol":"rtsp","rtspUrl":"rtsp://1.2.3.4/live"}`); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/camera/probe (normal) status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

// setupCameraFullApp 装配完整路由（含认证/权限），供摄像头权限测试使用。
func setupCameraFullApp(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "camera-full")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	cfg := &config.Config{
		JWT: config.JWT{
			Secret:       "api-test-jwt-secret-123456",
			AccessTTL:    time.Hour,
			RefreshTTL:   7 * 24 * time.Hour,
			SecureCookie: true,
		},
		Log:     config.Log{Level: "release"},
		Storage: config.Storage{MaxSize: 10 * 1024 * 1024},
	}

	authRepo := repository.NewAuthRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	deptRepo := repository.NewDepartmentRepository(db)
	userRepo := repository.NewUserRepository(db)
	oplogRepo := repository.NewOperationLogRepository(db)
	sysCfgRepo := repository.NewSystemConfigRepository(db)
	cameraRepo := repository.NewCameraRepository(db)

	authSvc := service.NewAuthService(authRepo, userRepo, menuRepo, cfg)
	menuSvc := service.NewMenuService(menuRepo)
	roleSvc := service.NewRoleService(roleRepo, menuRepo)
	deptSvc := service.NewDeptService(deptRepo)
	userSvc := service.NewUserService(userRepo, deptRepo, roleRepo)
	oplogSvc := service.NewOperationLogService(oplogRepo)
	ntpSvc := service.NewNTPService(sysCfgRepo, ntp.NewMockExecutor(), zap.NewNop())
	cameraSvc := service.NewCameraService(cameraRepo, repository.NewTaskRepository(db), &fakeProbeClient{})

	authMid := middleware.NewAuthMiddleware(authRepo, cfg)
	permMid := middleware.NewPermMiddleware(menuRepo)
	oplogMid := middleware.NewOplogMiddleware(oplogSvc, zap.NewNop())

	deps := router.Deps{
		ErrorHandler:        middleware.ErrorHandler(),
		AuthMiddleware:      authMid,
		PermMiddleware:      permMid,
		OplogMiddleware:     oplogMid,
		MenuHandler:         api.NewMenuHandler(menuSvc),
		RoleHandler:         api.NewRoleHandler(roleSvc),
		DepartmentHandler:   api.NewDepartmentHandler(deptSvc),
		OperationLogHandler: api.NewOperationLogHandler(oplogSvc),
		UserHandler:         api.NewUserHandler(userSvc),
		AuthHandler:         api.NewAuthHandler(authSvc, authMid, cfg),
		FileHandler:         api.NewFileHandler(service.NewFileService(storage.NopStorage(), cfg), cfg),
		NTPHandler:          api.NewNTPHandler(ntpSvc),
		CameraHandler:       api.NewCameraHandler(cameraSvc),
	}

	return router.New(cfg, deps), db
}
