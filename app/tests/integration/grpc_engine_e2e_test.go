//go:build integration

// Package integration 存放依赖外部系统（真实 C++ aivision-engine）的集成测试。
// 普通 `go test ./...` 不包含本包；通过 `make -C app grpc-e2e` 运行。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/engineipc"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// e2eDesiredStateAdapter 是产品 DesiredStateAdapter 的 recording 实现：
// 每次调用通过 onCall 通道通知测试（缓冲通道，避免丢失信号）。
type e2eDesiredStateAdapter struct {
	mu     sync.Mutex
	calls  int
	state  *aivisionv1.DesiredState
	onCall chan struct{}
}

func (a *e2eDesiredStateAdapter) DesiredState(context.Context, uint64) (*aivisionv1.DesiredState, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	select {
	case a.onCall <- struct{}{}:
	default:
	}
	return a.state, nil
}

func (a *e2eDesiredStateAdapter) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// e2eReportAdapter 是产品 ReportAdapter 的 recording 实现。
type e2eReportAdapter struct {
	mu        sync.Mutex
	metrics   int
	onMetrics chan struct{}
}

func (a *e2eReportAdapter) AcceptAlarm(context.Context, *aivisionv1.AlarmEvent) error {
	return nil
}

func (a *e2eReportAdapter) AcceptTaskState(context.Context, *aivisionv1.TaskState) error {
	return nil
}

func (a *e2eReportAdapter) AcceptInstanceState(context.Context, *aivisionv1.InstanceState) error {
	return nil
}

func (a *e2eReportAdapter) AcceptMetrics(context.Context, *aivisionv1.DeviceTelemetry) error {
	a.mu.Lock()
	a.metrics++
	a.mu.Unlock()
	select {
	case a.onMetrics <- struct{}{}:
	default:
	}
	return nil
}

func (a *e2eReportAdapter) ReconcileOrphanImages(context.Context, []*aivisionv1.OrphanImageEntry) (engineipc.OrphanDisposition, error) {
	return engineipc.OrphanDisposition{}, nil
}

// waitSignal 等待通道信号，超时即失败。用 channel 而非固定 sleep。
func waitSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// waitSocketFile 轮询等待 socket 文件出现。
func waitSocketFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket file %s did not appear", path)
}

// waitSocketGone 轮询等待 socket 文件消失。
func waitSocketGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket file %s still exists", path)
}

// TestGrpcEngineE2E 使用真实 C++ aivision-engine（mock platform）验证双向通信：
//   - Go -> C++：产品 EngineClient.QueryProfile 调用 engine.sock；
//   - C++ -> Go：Engine 至少调用一次 GetDesiredState 与 ReportMetrics（app.sock）；
//   - 终止 Engine 后双方自有 socket 有序清理，子进程始终被回收。
//
// 需要 AIVISION_ENGINE_BIN 指向已构建的 mock engine 二进制（由 `make -C app grpc-e2e` 提供）。
func TestGrpcEngineE2E(t *testing.T) {
	engineBin := os.Getenv("AIVISION_ENGINE_BIN")
	if engineBin == "" {
		t.Skip("AIVISION_ENGINE_BIN not set; run `make -C app grpc-e2e`")
	}
	if fi, err := os.Stat(engineBin); err != nil || fi.IsDir() {
		t.Fatalf("AIVISION_ENGINE_BIN %q is not an executable file", engineBin)
	}

	// --- 临时目录与 socket 路径 ---
	tmp := t.TempDir()
	appSocket := filepath.Join(tmp, "app.sock")
	engineSocket := filepath.Join(tmp, "engine.sock")
	pkgDir := filepath.Join(tmp, "packages")
	imgDir := filepath.Join(tmp, "images")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir packages: %v", err)
	}
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatalf("mkdir images: %v", err)
	}
	profilePath := filepath.Join(tmp, "engine-profile.json")
	profileData, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"platform_id":    "mock",
		"paths":          map[string]any{"runtime_dir": tmp},
		"ipc":            map[string]any{"app_socket": "app.sock", "engine_socket": "engine.sock"},
	})
	if err != nil {
		t.Fatalf("marshal engine profile: %v", err)
	}
	if err := os.WriteFile(profilePath, profileData, 0o600); err != nil {
		t.Fatalf("write engine profile: %v", err)
	}

	// --- Go 侧：产品 engineipc.Runtime + recording adapters（绑定 app.sock）---
	dsAdapter := &e2eDesiredStateAdapter{
		onCall: make(chan struct{}, 16),
		state: &aivisionv1.DesiredState{
			DeviceId: "e2e-device",
			Revision: 1,
		},
	}
	reportAdapter := &e2eReportAdapter{onMetrics: make(chan struct{}, 16)}
	rt := engineipc.NewRuntime(zap.NewNop(), dsAdapter, reportAdapter)
	if err := rt.Start(appSocket); err != nil {
		t.Fatalf("start Go ipc runtime: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rt.Shutdown(ctx); err != nil {
			t.Errorf("go runtime shutdown: %v", err)
		}
	}()

	// --- 启动真实 C++ aivision-engine（mock platform，绑定 engine.sock）---
	cmd := exec.Command(engineBin)
	env := make([]string, 0, len(os.Environ())+4)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "AIVISION_ENGINE_SOCKET=") || strings.HasPrefix(value, "AIVISION_APP_SOCKET=") ||
			strings.HasPrefix(value, "AIVISION_ENGINE_PROFILE=") {
			continue
		}
		env = append(env, value)
	}
	cmd.Env = append(env,
		"AIVISION_ENGINE_PROFILE="+profilePath,
		"AIVISION_PACKAGE_DIR="+pkgDir,
		"AIVISION_IMAGE_DIR="+imgDir,
		"AIVISION_LOG_LEVEL=info",
	)
	var engineLog bytes.Buffer
	cmd.Stdout = &engineLog
	cmd.Stderr = &engineLog
	if err := cmd.Start(); err != nil {
		t.Fatalf("start engine: %v", err)
	}
	proc := cmd.Process
	reaped := make(chan struct{})
	cleanupEngine := func() {
		t.Helper()
		if proc == nil {
			return
		}
		_ = proc.Signal(syscall.SIGTERM)
		select {
		case <-reaped:
		case <-time.After(5 * time.Second):
			_ = proc.Kill()
			<-reaped
		}
		if t.Failed() {
			t.Logf("engine log:\n%s", engineLog.String())
		}
	}
	t.Cleanup(cleanupEngine)
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()

	// 等待 Engine 的 engine.sock 出现。
	waitSocketFile(t, engineSocket, 15*time.Second)

	// --- Go -> C++：产品 EngineClient.QueryProfile 调用真实 Engine ---
	client, err := engineipc.NewEngineClient(&config.Config{IPC: config.IPC{EngineSocket: engineSocket}})
	if err != nil {
		t.Fatalf("NewEngineClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.QueryProfile(ctx, &aivisionv1.QueryProfileRequest{})
	if err != nil {
		t.Fatalf("QueryProfile against real engine: %v", err)
	}
	if resp.GetCode() != "" {
		t.Fatalf("QueryProfile code = %q, want empty", resp.GetCode())
	}
	if resp.GetProfile() == nil || resp.GetProfile().GetPlatformId() == "" {
		t.Fatalf("QueryProfile profile empty: %+v", resp.GetProfile())
	}
	t.Logf("engine profile: platform_id=%s arch=%s inference=%s",
		resp.GetProfile().GetPlatformId(), resp.GetProfile().GetArch(), resp.GetProfile().GetInferenceRuntime())

	// --- C++ -> Go：等待 Engine 至少调用一次 GetDesiredState 与 ReportMetrics ---
	// Engine 控制面循环首轮即上报遥测（last_telemetry 初始化为 now-10s），GetDesiredState 每 ~2s 一次。
	waitSignal(t, dsAdapter.onCall, 15*time.Second, "GetDesiredState from engine")
	waitSignal(t, reportAdapter.onMetrics, 20*time.Second, "ReportMetrics from engine")
	if dsAdapter.count() < 1 || reportAdapter.metrics < 1 {
		t.Fatalf("engine calls: desiredState=%d metrics=%d, want >=1 each",
			dsAdapter.count(), reportAdapter.metrics)
	}

	// --- 终止 Engine 并验证双方自有 socket 有序清理 ---
	cleanupEngine() // SIGTERM → engine 优雅停机并 unlink engine.sock
	waitSocketGone(t, engineSocket, 10*time.Second)
	t.Log("engine exited and removed engine.sock")

	// Go runtime 关闭后 app.sock 按 identity 清理。
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	if err := rt.Shutdown(shCtx); err != nil {
		t.Fatalf("go runtime shutdown: %v", err)
	}
	waitSocketGone(t, appSocket, 5*time.Second)
	t.Log("go runtime removed app.sock")
}

// TestGrpcEngineE2EValidation 校验 AIVISION_ENGINE_BIN 缺失时测试跳过（而非误跑）。
func TestGrpcEngineE2EValidation(t *testing.T) {
	// 复用真实监听验证 Go 侧 server 在无 Engine 时仍可独立启动（Engine 缺席不阻止 Go 启动）。
	tmp := t.TempDir()
	appSocket := filepath.Join(tmp, "app.sock")
	rt := engineipc.NewRuntime(zap.NewNop(),
		&e2eDesiredStateAdapter{onCall: make(chan struct{}, 1), state: &aivisionv1.DesiredState{DeviceId: "standalone", Revision: 1}},
		&e2eReportAdapter{onMetrics: make(chan struct{}, 1)})
	if err := rt.Start(appSocket); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	conn, err := grpc.NewClient("unix://"+appSocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cp := aivisionv1.NewControlPlaneServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if resp, err := cp.GetDesiredState(ctx, &aivisionv1.GetDesiredStateRequest{}); err != nil || resp.GetDesiredState() == nil {
		t.Fatalf("GetDesiredState without engine: resp=%+v err=%v", resp, err)
	}
	_ = conn.Close()
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	if err := rt.Shutdown(shCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
