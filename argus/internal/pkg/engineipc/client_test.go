package engineipc

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"argus/app/internal/pkg/config"
	argusv1 "argus/app/internal/proto/argus/v1"
)

// startFakeEngine 在真实临时 UDS 上启动 fake EngineService server，返回 socket 路径与 fake。
func startFakeEngine(t *testing.T, fake *fakeEngineServiceServer) string {
	t.Helper()
	path := testSocketPath(t, "engine.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen engine socket: %v", err)
	}
	srv := grpc.NewServer()
	argusv1.RegisterEngineServiceServer(srv, fake)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = ln.Close()
	})
	return path
}

func newTestEngineClient(t *testing.T, path string) *EngineClient {
	t.Helper()
	c, err := NewEngineClient(&config.Config{IPC: config.IPC{EngineSocket: path}})
	if err != nil {
		t.Fatalf("NewEngineClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func clientCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestEngineClientAllRPCs 通过真实 UDS 覆盖全部 13 个 EngineService RPC 的请求映射与响应。
func TestEngineClientAllRPCs(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	path := startFakeEngine(t, fake)
	client := newTestEngineClient(t, path)
	ctx := clientCtx(t)

	// 1. ApplyDesiredState
	resp1, err := client.ApplyDesiredState(ctx, &argusv1.ApplyDesiredStateRequest{
		DesiredState: &argusv1.DesiredState{DeviceId: "dev-1", Revision: 9},
	})
	if err != nil {
		t.Fatalf("ApplyDesiredState: %v", err)
	}
	if resp1.GetAppliedRevision() != 42 || resp1.GetCode() != CodeOK {
		t.Errorf("ApplyDesiredState resp = %+v", resp1)
	}

	// 2. UpsertTask
	resp2, err := client.UpsertTask(ctx, &argusv1.UpsertTaskRequest{
		Task: &argusv1.CameraTaskConfig{CameraId: "cam-1", RtspUrl: "rtsp://x", Enabled: true},
	})
	if err != nil || resp2.GetCode() != CodeOK {
		t.Fatalf("UpsertTask: %v resp=%+v", err, resp2)
	}

	// 3. SetInstanceState
	resp3, err := client.SetInstanceState(ctx, &argusv1.SetInstanceStateRequest{InstanceId: "inst-1", Enabled: false})
	if err != nil || resp3.GetCode() != CodeOK {
		t.Fatalf("SetInstanceState: %v resp=%+v", err, resp3)
	}

	// 4. UpdateInstanceConfig
	resp4, err := client.UpdateInstanceConfig(ctx, &argusv1.UpdateInstanceConfigRequest{InstanceId: "inst-1", ParamsJson: `{"x":1}`})
	if err != nil || resp4.GetCode() != CodeOK {
		t.Fatalf("UpdateInstanceConfig: %v resp=%+v", err, resp4)
	}

	// 5. InstallPackage
	resp5, err := client.InstallPackage(ctx, &argusv1.InstallPackageRequest{PackagePath: "/tmp/pkg.tar.gz"})
	if err != nil || resp5.GetCode() != CodeOK || resp5.GetAlgorithmId() != "yolov8n" {
		t.Fatalf("InstallPackage: %v resp=%+v", err, resp5)
	}

	// 6. UpgradePackage
	resp6, err := client.UpgradePackage(ctx, &argusv1.UpgradePackageRequest{PackagePath: "/tmp/pkg2.tar.gz"})
	if err != nil || resp6.GetCode() != CodeOK || resp6.GetVersion() != "1.1.0" {
		t.Fatalf("UpgradePackage: %v resp=%+v", err, resp6)
	}

	// 7. RollbackPackage
	resp7, err := client.RollbackPackage(ctx, &argusv1.RollbackPackageRequest{AlgorithmId: "yolov8n", TargetVersion: "1.0.0"})
	if err != nil || resp7.GetCode() != CodeOK {
		t.Fatalf("RollbackPackage: %v resp=%+v", err, resp7)
	}

	// 8. UninstallPackage
	resp8, err := client.UninstallPackage(ctx, &argusv1.UninstallPackageRequest{AlgorithmId: "yolov8n", Version: "1.0.0"})
	if err != nil || resp8.GetCode() != CodeOK {
		t.Fatalf("UninstallPackage: %v resp=%+v", err, resp8)
	}

	// 9. DeleteImages
	resp9, err := client.DeleteImages(ctx, &argusv1.DeleteImagesRequest{ImageIds: []string{"a", "b"}})
	if err != nil || resp9.GetCode() != CodeOK {
		t.Fatalf("DeleteImages: %v resp=%+v", err, resp9)
	}
	if len(resp9.GetResults()) != 2 || resp9.GetResults()[1].GetImageId() != "b" {
		t.Errorf("DeleteImages results = %+v", resp9.GetResults())
	}

	// 10. ReconcileImages
	resp10, err := client.ReconcileImages(ctx, &argusv1.ReconcileImagesRequest{RetainImageIds: []string{"a"}})
	if err != nil || resp10.GetCode() != CodeOK {
		t.Fatalf("ReconcileImages: %v resp=%+v", err, resp10)
	}

	// 11. QueryProfile
	resp11, err := client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
	if err != nil || resp11.GetCode() != CodeOK || resp11.GetProfile().GetPlatformId() != "fake" {
		t.Fatalf("QueryProfile: %v resp=%+v", err, resp11)
	}

	// 12. QueryMetrics
	resp12, err := client.QueryMetrics(ctx, &argusv1.QueryMetricsRequest{})
	if err != nil || resp12.GetCode() != CodeOK || resp12.GetTelemetry().GetUptimeSeconds() != 7 {
		t.Fatalf("QueryMetrics: %v resp=%+v", err, resp12)
	}

	// 13. ProbeCamera
	fake.probe = &argusv1.ProbeCameraResponse{
		Status:            "success",
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
	resp13, err := client.ProbeCamera(ctx, &argusv1.ProbeCameraRequest{Protocol: "rtsp", Url: "rtsp://192.168.1.10/live"})
	if err != nil || resp13.GetCode() != CodeOK || resp13.GetStatus() != "success" || resp13.GetSelectedTransport() != "tcp" {
		t.Fatalf("ProbeCamera: %v resp=%+v", err, resp13)
	}

	// 请求映射断言（通过 fake 记录）。
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.appliedStates) != 1 || fake.appliedStates[0].GetDesiredState().GetRevision() != 9 {
		t.Errorf("appliedStates = %+v", fake.appliedStates)
	}
	if len(fake.upsertedTasks) != 1 || fake.upsertedTasks[0].GetTask().GetCameraId() != "cam-1" {
		t.Errorf("upsertedTasks = %+v", fake.upsertedTasks)
	}
	if len(fake.setInstStates) != 1 || fake.setInstStates[0].GetEnabled() {
		t.Errorf("setInstStates = %+v", fake.setInstStates)
	}
	if len(fake.updatedConfigs) != 1 || fake.updatedConfigs[0].GetParamsJson() != `{"x":1}` {
		t.Errorf("updatedConfigs = %+v", fake.updatedConfigs)
	}
	if len(fake.installed) != 1 || fake.installed[0].GetPackagePath() != "/tmp/pkg.tar.gz" {
		t.Errorf("installed = %+v", fake.installed)
	}
	if len(fake.upgraded) != 1 {
		t.Errorf("upgraded = %+v", fake.upgraded)
	}
	if len(fake.rolledBack) != 1 || fake.rolledBack[0].GetTargetVersion() != "1.0.0" {
		t.Errorf("rolledBack = %+v", fake.rolledBack)
	}
	if len(fake.uninstalled) != 1 {
		t.Errorf("uninstalled = %+v", fake.uninstalled)
	}
	if len(fake.deletedImages) != 1 || len(fake.deletedImages[0].GetImageIds()) != 2 {
		t.Errorf("deletedImages = %+v", fake.deletedImages)
	}
	if len(fake.reconciledImages) != 1 || len(fake.reconciledImages[0].GetRetainImageIds()) != 1 {
		t.Errorf("reconciledImages = %+v", fake.reconciledImages)
	}
	if len(fake.profileQueries) != 1 {
		t.Errorf("profileQueries = %+v", fake.profileQueries)
	}
	if len(fake.metricsQueries) != 1 {
		t.Errorf("metricsQueries = %+v", fake.metricsQueries)
	}
	if len(fake.probeCameraCalls) != 1 || fake.probeCameraCalls[0].GetProtocol() != "rtsp" ||
		fake.probeCameraCalls[0].GetUrl() != "rtsp://192.168.1.10/live" {
		t.Errorf("probeCameraCalls = %+v", fake.probeCameraCalls)
	}
}

// TestEngineClientRemoteError 非空响应 code 转成可 errors.As 判断的 *RemoteError。
func TestEngineClientRemoteError(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	path := startFakeEngine(t, fake)
	client := newTestEngineClient(t, path)

	fake.mu.Lock()
	fake.code = "STALE_REVISION"
	fake.mu.Unlock()

	resp, err := client.ApplyDesiredState(clientCtx(t), &argusv1.ApplyDesiredStateRequest{})
	if err == nil {
		t.Fatal("expected RemoteError for non-empty code")
	}
	var re *RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *RemoteError", err)
	}
	if re.Code != "STALE_REVISION" {
		t.Errorf("RemoteError.Code = %q, want STALE_REVISION", re.Code)
	}
	// 响应仍保留供诊断。
	if resp == nil || resp.GetCode() != "STALE_REVISION" {
		t.Errorf("response should be retained for diagnostics, got %+v", resp)
	}
}

// TestEngineClientProbeFailureIsNotRemoteError 测活失败（status=failed、code 为空）不是错误：
// RPC code 仅表示处理成功，失败细节放在结构化 status/failure_code 中。
func TestEngineClientProbeFailureIsNotRemoteError(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	fake.probe = &argusv1.ProbeCameraResponse{
		Status:      "failed",
		FailureCode: "RTSP_CONNECT_FAILED",
		ElapsedMs:   5000,
		Attempts: []*argusv1.ProbeAttempt{
			{Transport: "tcp", ElapsedMs: 5000, FailureCode: "RTSP_CONNECT_FAILED"},
			{Transport: "udp", ElapsedMs: 100, FailureCode: "RTSP_CONNECT_FAILED"},
		},
	}
	path := startFakeEngine(t, fake)
	client := newTestEngineClient(t, path)

	resp, err := client.ProbeCamera(clientCtx(t), &argusv1.ProbeCameraRequest{Protocol: "rtsp", Url: "rtsp://x/live"})
	if err != nil {
		t.Fatalf("probe failure should not be an error, got %v", err)
	}
	if resp.GetStatus() != "failed" || resp.GetFailureCode() != "RTSP_CONNECT_FAILED" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.GetCode() != CodeOK {
		t.Errorf("RPC code = %q, want empty (OK)", resp.GetCode())
	}
}

// TestEngineClientProbeRemoteError RPC 处理失败（非空 code）转成 *RemoteError。
func TestEngineClientProbeRemoteError(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	path := startFakeEngine(t, fake)
	client := newTestEngineClient(t, path)

	fake.mu.Lock()
	fake.code = "INVALID_ARG"
	fake.mu.Unlock()

	_, err := client.ProbeCamera(clientCtx(t), &argusv1.ProbeCameraRequest{Protocol: "", Url: ""})
	if err == nil {
		t.Fatal("expected RemoteError for INVALID_ARG")
	}
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "INVALID_ARG" {
		t.Fatalf("err = %v, want *RemoteError{INVALID_ARG}", err)
	}
}

// TestEngineClientTransportError 业务失败不伪装成 transport status：code 非空时
// gRPC transport 仍为 OK，错误通过 *RemoteError 表达。
func TestEngineClientTransportOKForBusinessError(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	path := startFakeEngine(t, fake)
	client := newTestEngineClient(t, path)

	fake.mu.Lock()
	fake.code = "RESOURCE_LIMIT_EXCEEDED"
	fake.mu.Unlock()

	_, err := client.InstallPackage(clientCtx(t), &argusv1.InstallPackageRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	// 不应是 gRPC status（业务失败不是传输失败）。
	if _, ok := status.FromError(err); ok {
		t.Errorf("business error should not be a gRPC status: %v", err)
	}
}

// TestEngineClientEngineAbsent 目标 socket 不存在时调用在 deadline 内失败。
func TestEngineClientEngineAbsent(t *testing.T) {
	path := testSocketPath(t, "absent-engine.sock")
	client := newTestEngineClient(t, path)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
	if err == nil {
		t.Fatal("call to absent engine should fail")
	}
}

// TestEngineClientReconnectAfterServerAppears 同一 ClientConn 在 server 后启动后恢复。
func TestEngineClientReconnectAfterServerAppears(t *testing.T) {
	path := testSocketPath(t, "late-engine.sock")
	client := newTestEngineClient(t, path)

	// 先失败。
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, err := client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
	cancel()
	if err == nil {
		t.Fatal("expected failure before server starts")
	}

	// server 随后启动。
	fake := &fakeEngineServiceServer{}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	argusv1.RegisterEngineServiceServer(srv, fake)
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop()

	// 同一 ClientConn 应恢复（gRPC 自动重连）。
	var resp *argusv1.QueryProfileResponse
	var lastErr error
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		resp, lastErr = client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
		cancel()
		if lastErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("client did not recover after server start: %v", lastErr)
	}
	if resp.GetProfile().GetPlatformId() != "fake" {
		t.Errorf("profile = %+v", resp.GetProfile())
	}
}

// TestEngineClientServerRestart 同一 ClientConn 在 server 重启后恢复。
func TestEngineClientServerRestart(t *testing.T) {
	path := testSocketPath(t, "restart-engine.sock")
	fake := &fakeEngineServiceServer{}

	startSrv := func() *grpc.Server {
		ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		srv := grpc.NewServer()
		argusv1.RegisterEngineServiceServer(srv, fake)
		go func() { _ = srv.Serve(ln) }()
		return srv
	}

	srv := startSrv()
	client := newTestEngineClient(t, path)
	if _, err := client.QueryProfile(clientCtx(t), &argusv1.QueryProfileRequest{}); err != nil {
		t.Fatalf("initial call: %v", err)
	}

	// 停止 server（模拟 Engine 重启）。
	srv.Stop()

	// 等待连接失效后重新启动 server 于同一路径。
	time.Sleep(200 * time.Millisecond)
	_ = os.Remove(path)
	srv2 := startSrv()
	defer srv2.Stop()

	// 同一 ClientConn 应自动重连并恢复。
	var lastErr error
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, lastErr = client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
		cancel()
		if lastErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("client did not recover after server restart: %v", lastErr)
	}
}

// TestEngineClientClose 关闭后调用失败。
func TestEngineClientClose(t *testing.T) {
	fake := &fakeEngineServiceServer{}
	path := startFakeEngine(t, fake)
	client, err := NewEngineClient(&config.Config{IPC: config.IPC{EngineSocket: path}})
	if err != nil {
		t.Fatalf("NewEngineClient: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := client.QueryProfile(ctx, &argusv1.QueryProfileRequest{}); err == nil {
		t.Fatal("call after Close should fail")
	}
}
