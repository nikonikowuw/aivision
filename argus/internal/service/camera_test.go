package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// fakeProbeClient 可编程的 CameraProbeClient 替身。
type fakeProbeClient struct {
	// onCall 返回响应与错误；nil 时返回空成功响应。
	onCall func(ctx context.Context, req *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error)
	calls  int
}

func (f *fakeProbeClient) ProbeCamera(ctx context.Context, req *argusv1.ProbeCameraRequest, _ ...grpc.CallOption) (*argusv1.ProbeCameraResponse, error) {
	f.calls++
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

func newCameraServiceTestEnv(t *testing.T) (CameraService, *fakeProbeClient, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Camera{}, &model.AnalysisTask{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	fake := &fakeProbeClient{}
	svc := NewCameraService(repository.NewCameraRepository(db), repository.NewTaskRepository(db), fake)
	return svc, fake, db
}

func probeSuccessResp() *argusv1.ProbeCameraResponse {
	return &argusv1.ProbeCameraResponse{
		Status:            model.CameraProbeSuccess,
		SelectedTransport: "tcp",
		Codec:             "H264",
		Width:             1920,
		Height:            1080,
		Fps:               25,
		ElapsedMs:         850,
		Attempts: []*argusv1.ProbeAttempt{
			{Transport: "tcp", ElapsedMs: 850},
		},
	}
}

func probeFailureResp(code string) *argusv1.ProbeCameraResponse {
	return &argusv1.ProbeCameraResponse{
		Status:      model.CameraProbeFailed,
		FailureCode: code,
		ElapsedMs:   5000,
		Attempts: []*argusv1.ProbeAttempt{
			{Transport: "tcp", ElapsedMs: 5000, FailureCode: code},
			{Transport: "udp", ElapsedMs: 100, FailureCode: code},
		},
	}
}

func createFixtureCamera(t *testing.T, svc CameraService, name, url string) *model.Camera {
	t.Helper()
	cam, err := svc.CreateCamera(context.Background(), &SaveCameraInput{Name: name, RtspURL: url})
	if err != nil {
		t.Fatalf("create camera: %v", err)
	}
	return cam
}

func TestCameraServiceURLValidation(t *testing.T) {
	svc, _, _ := newCameraServiceTestEnv(t)
	ctx := context.Background()

	valid := []string{
		"rtsp://192.168.1.10/live",
		"rtsp://user:p%40ss@192.168.1.10/live",
		"RTSP://192.168.1.10:8554/live/stream",
		"rtsp://192.168.1.10/live?param=a%20b",
	}
	for _, u := range valid {
		if _, err := svc.CreateCamera(ctx, &SaveCameraInput{Name: "cam", RtspURL: u}); err != nil {
			t.Errorf("valid url %q rejected: %v", u, err)
		}
	}

	long := make([]byte, 2048)
	for i := range long {
		long[i] = 'a'
	}
	invalid := []string{
		"",
		"  ",
		"http://192.168.1.10/live",            // 非 rtsp scheme
		"rtsp:///live",                        // host 为空
		"rtsp://192.168.1.10/live#frag",       // fragment
		"rtsp://192.168.1.10/live%2",          // 非法百分号编码
		"rtsp://192.168.1.10/live%GG",         // 非十六进制
		"rtsp://192.168.1.10/live\x01",        // 控制字符
		"rtsp://192.168.1.10/" + string(long), // 超长
	}
	for _, u := range invalid {
		if _, err := svc.CreateCamera(ctx, &SaveCameraInput{Name: "cam", RtspURL: u}); err == nil {
			t.Errorf("invalid url %q accepted", u)
		}
	}
}

func TestCameraServiceCreatePersistsFullURLAndFields(t *testing.T) {
	svc, _, _ := newCameraServiceTestEnv(t)

	cam := createFixtureCamera(t, svc, "  门口  ", "  rtsp://user:p%40ss@192.168.1.10/live  ")
	// 首尾空白去除
	if cam.RtspURL != "rtsp://user:p%40ss@192.168.1.10/live" {
		t.Fatalf("rtspUrl = %q", cam.RtspURL)
	}
	if cam.Name != "门口" {
		t.Fatalf("name = %q", cam.Name)
	}
	if cam.Protocol != model.CameraProtocolRTSP || cam.TransportPolicy != model.CameraTransportAuto {
		t.Fatalf("protocol/transport = %q/%q", cam.Protocol, cam.TransportPolicy)
	}
	if cam.LastProbeStatus != model.CameraProbeNever {
		t.Fatalf("lastProbeStatus = %q, want never", cam.LastProbeStatus)
	}
	if cam.ConfigHash == "" {
		t.Fatal("configHash empty")
	}
}

func TestCameraServiceProbePersistsSuccessOnFingerprintMatch(t *testing.T) {
	svc, fake, db := newCameraServiceTestEnv(t)
	ctx := context.Background()
	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")

	fake.onCall = func(_ context.Context, req *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		if req.GetProtocol() != "rtsp" || req.GetUrl() != cam.RtspURL {
			t.Fatalf("engine req = %q/%q", req.GetProtocol(), req.GetUrl())
		}
		return probeSuccessResp(), nil
	}

	result, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{ID: cam.ID, Protocol: "rtsp", RtspURL: cam.RtspURL})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != model.CameraProbeSuccess || !result.Persisted || result.Stale {
		t.Fatalf("result = %+v", result)
	}
	if result.SelectedTransport != "tcp" || result.Codec != "H264" || result.Width != 1920 || result.Height != 1080 || result.FPS != 25 {
		t.Fatalf("result media = %+v", result)
	}

	got, err := repository.NewCameraRepository(db).GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LastProbeStatus != model.CameraProbeSuccess || got.LastSuccessTransport != "tcp" ||
		got.LastCodec != "H264" || got.LastWidth != 1920 || got.LastHeight != 1080 || got.LastFPS != 25 {
		t.Fatalf("persisted metadata = %+v", got)
	}
	if got.LastProbeAt == nil || got.LastSuccessAt == nil {
		t.Fatal("lastProbeAt/lastSuccessAt not set")
	}
}

func TestCameraServiceProbeFingerprintMismatchIsStale(t *testing.T) {
	svc, fake, db := newCameraServiceTestEnv(t)
	ctx := context.Background()
	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")

	// 测活期间配置被修改（URL 变化）→ 指纹不再匹配。
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		if _, err := svc.UpdateCamera(ctx, cam.ID, &SaveCameraInput{Name: cam.Name, RtspURL: "rtsp://192.168.1.11/live"}); err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
		return probeSuccessResp(), nil
	}

	result, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{ID: cam.ID, Protocol: "rtsp", RtspURL: cam.RtspURL})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Persisted {
		t.Fatal("result.persisted = true, want false (fingerprint changed)")
	}
	if !result.Stale {
		t.Fatal("result.stale = false, want true")
	}

	// 数据库未被覆盖：仍为 never（新配置未测活）。
	got, err := repository.NewCameraRepository(db).GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LastProbeStatus != model.CameraProbeNever {
		t.Fatalf("lastProbeStatus = %q, want never (not overwritten)", got.LastProbeStatus)
	}
}

func TestCameraServiceProbeNoIDDoesNotPersist(t *testing.T) {
	svc, fake, _ := newCameraServiceTestEnv(t)
	ctx := context.Background()
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return probeSuccessResp(), nil
	}

	result, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{Protocol: "rtsp", RtspURL: "rtsp://192.168.1.10/live"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != model.CameraProbeSuccess || result.Persisted || result.Stale {
		t.Fatalf("result = %+v", result)
	}
}

func TestCameraServiceProbeFailedPersistsFailureKeepsSuccessMetadata(t *testing.T) {
	svc, fake, db := newCameraServiceTestEnv(t)
	ctx := context.Background()
	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")

	// 先成功一次
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return probeSuccessResp(), nil
	}
	if _, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{ID: cam.ID, Protocol: "rtsp", RtspURL: cam.RtspURL}); err != nil {
		t.Fatalf("success probe: %v", err)
	}

	// 再失败一次：更新失败状态但保留成功媒体信息
	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return probeFailureResp("RTSP_CONNECT_FAILED"), nil
	}
	result, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{ID: cam.ID, Protocol: "rtsp", RtspURL: cam.RtspURL})
	if err != nil {
		t.Fatalf("failed probe: %v", err)
	}
	if result.Status != model.CameraProbeFailed || result.FailureCode != "RTSP_CONNECT_FAILED" || !result.Persisted {
		t.Fatalf("result = %+v", result)
	}

	got, err := repository.NewCameraRepository(db).GetByID(ctx, cam.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LastProbeStatus != model.CameraProbeFailed || got.LastProbeErrorCode != "RTSP_CONNECT_FAILED" {
		t.Fatalf("probe failure metadata = %+v", got)
	}
	// 成功媒体信息保留
	if got.LastSuccessTransport != "tcp" || got.LastCodec != "H264" || got.LastWidth != 1920 {
		t.Fatalf("success metadata lost: %+v", got)
	}
}

func TestCameraServiceProbeEngineErrorMapping(t *testing.T) {
	svc, fake, _ := newCameraServiceTestEnv(t)
	ctx := context.Background()

	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return nil, &engineipc.RemoteError{Code: "INVALID_ARG", ErrorMessage: "protocol and url are required"}
	}
	_, err := svc.ProbeCamera(ctx, &ProbeCameraRequest{Protocol: "rtsp", RtspURL: "rtsp://192.168.1.10/live"})
	if !errno.Is(err, errno.CodeInvalidParam) {
		t.Fatalf("INVALID_ARG error = %v, want CodeInvalidParam", err)
	}

	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return nil, &engineipc.RemoteError{Code: "PLATFORM_UNAVAILABLE", ErrorMessage: "no media backend"}
	}
	_, err = svc.ProbeCamera(ctx, &ProbeCameraRequest{Protocol: "rtsp", RtspURL: "rtsp://192.168.1.10/live"})
	if !errno.Is(err, errno.CodeInternal) {
		t.Fatalf("PLATFORM_UNAVAILABLE error = %v, want CodeInternal", err)
	}

	fake.onCall = func(_ context.Context, _ *argusv1.ProbeCameraRequest) (*argusv1.ProbeCameraResponse, error) {
		return nil, errors.New("transport down")
	}
	_, err = svc.ProbeCamera(ctx, &ProbeCameraRequest{Protocol: "rtsp", RtspURL: "rtsp://192.168.1.10/live"})
	if !errno.Is(err, errno.CodeInternal) {
		t.Fatalf("transport error = %v, want CodeInternal", err)
	}
}

func TestCameraServiceLivePreview(t *testing.T) {
	svc, _, _ := newCameraServiceTestEnv(t)
	ctx := context.Background()

	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")
	res, err := svc.StartLivePreview(ctx, cam.ID, "main")
	if err != nil {
		t.Fatalf("start live preview: %v", err)
	}
	if res.StreamPath == "" || res.HTTPPort != 8080 {
		t.Fatalf("unexpected res: %+v", res)
	}

	if err := svc.StopLivePreview(ctx, cam.ID, "main"); err != nil {
		t.Fatalf("stop live preview: %v", err)
	}
}

func TestCameraServiceDeleteAndBatch(t *testing.T) {
	svc, _, _ := newCameraServiceTestEnv(t)
	ctx := context.Background()

	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")
	if err := svc.DeleteCamera(ctx, cam.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.DeleteCamera(ctx, cam.ID); !errno.Is(err, errno.CodeNotFound) {
		t.Fatalf("delete missing = %v, want CodeNotFound", err)
	}

	c1 := createFixtureCamera(t, svc, "A", "rtsp://192.168.1.1/live")
	c2 := createFixtureCamera(t, svc, "B", "rtsp://192.168.1.2/live")
	if err := svc.BatchDeleteCamera(ctx, []uint64{c1.ID, c2.ID}); err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	page, err := svc.GetPage(ctx, &CameraPageQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("total after batch delete = %d, want 0", page.Total)
	}
}

func TestCameraServiceProbeUnknownIDReturnsNotFound(t *testing.T) {
	svc, _, _ := newCameraServiceTestEnv(t)
	_, err := svc.ProbeCamera(context.Background(), &ProbeCameraRequest{ID: 9999, Protocol: "rtsp", RtspURL: "rtsp://192.168.1.10/live"})
	if !errno.Is(err, errno.CodeNotFound) {
		t.Fatalf("unknown id error = %v, want CodeNotFound", err)
	}
}

// TestCameraServiceDeleteBlockedByActiveTask 摄像头存在关联分析任务时拒绝删除（D9）。
func TestCameraServiceDeleteBlockedByActiveTask(t *testing.T) {
	svc, _, db := newCameraServiceTestEnv(t)
	ctx := context.Background()
	cam := createFixtureCamera(t, svc, "门口", "rtsp://192.168.1.10/live")

	// 存在未软删分析任务 → 拒绝删除并返回 CodeCameraInUse
	if err := db.Create(&model.AnalysisTask{CameraID: cam.CameraID, Name: "task-a"}).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := svc.DeleteCamera(ctx, cam.ID); !errno.Is(err, errno.CodeCameraInUse) {
		t.Fatalf("delete with active task = %v, want CodeCameraInUse", err)
	}
	if _, err := repository.NewCameraRepository(db).GetByID(ctx, cam.ID); err != nil {
		t.Fatalf("camera must remain after rejected delete: %v", err)
	}

	// 任务软删后 → 允许删除
	if err := db.Delete(&model.AnalysisTask{}, "camera_id = ?", cam.CameraID).Error; err != nil {
		t.Fatalf("soft delete task: %v", err)
	}
	if err := svc.DeleteCamera(ctx, cam.ID); err != nil {
		t.Fatalf("delete after task removed failed: %v", err)
	}
}

// TestCameraServiceBatchDeleteBlockedWhenAnyCameraHasTask
// 批量删除时任一摄像头有关联任务即整批拒绝。
func TestCameraServiceBatchDeleteBlockedWhenAnyCameraHasTask(t *testing.T) {
	svc, _, db := newCameraServiceTestEnv(t)
	ctx := context.Background()
	c1 := createFixtureCamera(t, svc, "A", "rtsp://192.168.1.1/live")
	c2 := createFixtureCamera(t, svc, "B", "rtsp://192.168.1.2/live")
	if err := db.Create(&model.AnalysisTask{CameraID: c2.CameraID, Name: "task-b"}).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// 任一摄像头有任务即整批拒绝，c1 也不应被删除
	if err := svc.BatchDeleteCamera(ctx, []uint64{c1.ID, c2.ID}); !errno.Is(err, errno.CodeCameraInUse) {
		t.Fatalf("batch delete = %v, want CodeCameraInUse", err)
	}
	page, err := svc.GetPage(ctx, &CameraPageQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want 2 (nothing deleted)", page.Total)
	}
}
