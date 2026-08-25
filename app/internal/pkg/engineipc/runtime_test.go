package engineipc

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

func TestRuntimeLifecycle(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Start 后 socket 文件必须存在且可连接。
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("socket file missing after Start: %v", err)
	}

	conn, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cp := aivisionv1.NewControlPlaneServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cp.GetDesiredState(ctx, &aivisionv1.GetDesiredStateRequest{}); err != nil {
		t.Fatalf("GetDesiredState over live runtime: %v", err)
	}
	_ = conn.Close()

	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	if err := rt.Shutdown(shCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// Shutdown 后 socket 文件按 identity 被删除。
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket file should be removed after Shutdown, err=%v", err)
	}
}

func TestRuntimeRepeatedStartFails(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = rt.Shutdown(ctx)
	}()
	if err := rt.Start(path); err == nil {
		t.Fatal("second Start should fail")
	}
}

func TestRuntimeShutdownBeforeStart(t *testing.T) {
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start should be no-op, got %v", err)
	}
}

func TestRuntimeRepeatedShutdown(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := rt.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Shutdown #%d: %v", i+1, err)
		}
	}
}

// blockingDesiredStateAdapter 让 GetDesiredState 阻塞直到释放，用于强制停机路径。
type blockingDesiredStateAdapter struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func newBlockingDesiredStateAdapter() *blockingDesiredStateAdapter {
	return &blockingDesiredStateAdapter{release: make(chan struct{}), entered: make(chan struct{}, 1)}
}

func (a *blockingDesiredStateAdapter) DesiredState(ctx context.Context, _ uint64) (*aivisionv1.DesiredState, error) {
	select {
	case a.entered <- struct{}{}:
	default:
	}
	select {
	case <-a.release:
		return &aivisionv1.DesiredState{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *blockingDesiredStateAdapter) free() {
	a.once.Do(func() { close(a.release) })
}

// TestRuntimeForceStop 超时后强制 Stop：阻塞中的 RPC 被取消，Shutdown 返回且 socket 清理。
func TestRuntimeForceStop(t *testing.T) {
	adapter := newBlockingDesiredStateAdapter()
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), adapter, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}

	conn, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cp := aivisionv1.NewControlPlaneServiceClient(conn)

	// 发起阻塞中的 RPC。
	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer rpcCancel()
	rpcErr := make(chan error, 1)
	go func() {
		_, err := cp.GetDesiredState(rpcCtx, &aivisionv1.GetDesiredStateRequest{})
		rpcErr <- err
	}()
	select {
	case <-adapter.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("RPC did not reach adapter")
	}

	// 短 deadline 触发强制 Stop。
	shCtx, shCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = rt.Shutdown(shCtx)
	shCancel()
	if err != nil {
		t.Fatalf("Shutdown with force stop: %v", err)
	}
	adapter.free()
	// 阻塞中的 RPC 应因连接关闭而失败（Canceled 或 Unavailable）。
	if err := <-rpcErr; err == nil {
		t.Fatal("in-flight RPC should fail after force stop")
	}
	_ = conn.Close()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed after force stop, err=%v", err)
	}
}

// TestRuntimeServeError 监听器异常时 Errors() 上报错误，随后 Shutdown 仍清理 socket。
func TestRuntimeServeError(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 关闭底层 listener 使 Serve 返回非正常错误。
	rt.mu.Lock()
	owner := rt.owner
	rt.mu.Unlock()
	if err := owner.Listener().Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	select {
	case err := <-rt.Errors():
		if err == nil {
			t.Fatal("Errors() should report non-nil serve error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no serve error reported")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown after serve error: %v", err)
	}
}

// TestRuntimeShutdownStopsNewCalls 停机后新调用失败且 socket 消失。
func TestRuntimeShutdownStopsNewCalls(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), &recordingDesiredStateAdapter{}, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	conn, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient after shutdown: %v", err)
	}
	defer conn.Close()
	cp := aivisionv1.NewControlPlaneServiceClient(conn)
	callCtx, callCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer callCancel()
	if _, err := cp.GetDesiredState(callCtx, &aivisionv1.GetDesiredStateRequest{}); err == nil {
		t.Fatal("call after shutdown should fail")
	}
}

// TestRuntimeConcurrentShutdown 并发 RPC 与 Shutdown 无竞态（配合 -race 运行）。
func TestRuntimeConcurrentShutdown(t *testing.T) {
	adapter := &recordingDesiredStateAdapter{state: &aivisionv1.DesiredState{Revision: 1}}
	path := testSocketPath(t, "app.sock")
	rt := NewRuntime(zap.NewNop(), adapter, &recordingReportAdapter{})
	if err := rt.Start(path); err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cp := aivisionv1.NewControlPlaneServiceClient(conn)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				_, _ = cp.GetDesiredState(ctx, &aivisionv1.GetDesiredStateRequest{})
				cancel()
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	shCtx, shCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = rt.Shutdown(shCtx)
	shCancel()
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	close(stop)
	wg.Wait()
	_ = conn.Close()
}
