package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/proto/argus/v1"
	"argus/app/internal/service"
	"argus/app/internal/testutil"
)

// testSocketPath delegates to the shared short-path helper used by app tests.
func testSocketPath(t *testing.T, name string) string {
	return testutil.SocketPath(t, name)
}

// freePort 返回一个当前空闲的 TCP 端口。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// waitHTTP 轮询等待 HTTP server 开始接受连接。
func waitHTTP(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("http server did not come up")
}

func newTestRuntime() *engineipc.Runtime {
	return engineipc.NewRuntime(zap.NewNop(), engineipc.UnavailableDesiredStateAdapter(), engineipc.UnavailableReportAdapter())
}

// fakeIPCRuntime 是 ipcRuntime 的测试替身：可注入 Start 失败与 serve error。
type fakeIPCRuntime struct {
	startErr   error
	serveErrCh chan error
	shutdownCh chan struct{}
}

func (f *fakeIPCRuntime) Start(string) error { return f.startErr }

func (f *fakeIPCRuntime) Errors() <-chan error { return f.serveErrCh }

func (f *fakeIPCRuntime) Shutdown(context.Context) error {
	select {
	case f.shutdownCh <- struct{}{}:
	default:
	}
	return nil
}

// fakeNetworkService embeds the production interface so lifecycle tests only override Close.
type fakeNetworkService struct {
	service.NetworkService
	closed chan struct{}
}

func (f *fakeNetworkService) Close(context.Context) error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

// TestLifecycleStartupFailureClosesDependencies 启动 listener 失败时回收 EngineClient 与 Network。
func TestLifecycleStartupFailureClosesDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	port := freePort(t)
	appSocket := testSocketPath(t, "app.sock")
	occupier, err := net.ListenUnix("unix", &net.UnixAddr{Name: appSocket, Net: "unix"})
	if err != nil {
		t.Fatalf("occupy app.sock: %v", err)
	}
	defer occupier.Close()

	client, err := engineipc.NewEngineClient(&config.Config{IPC: config.IPC{EngineSocket: testSocketPath(t, "engine.sock")}})
	if err != nil {
		t.Fatalf("NewEngineClient: %v", err)
	}
	network := &fakeNetworkService{closed: make(chan struct{})}
	app := &App{
		Logger:       zap.NewNop(),
		Engine:       gin.New(),
		Network:      network,
		IPCRuntime:   newTestRuntime(),
		EngineClient: client,
	}
	cfg := &config.Config{Server: config.Server{Port: port}, IPC: config.IPC{AppSocket: appSocket}}
	lc := &serverLifecycle{cfg: cfg, app: app, quit: make(chan os.Signal, 1), timeout: time.Second}

	if err := lc.run(); err == nil {
		t.Fatal("run should fail when app.sock is occupied")
	}
	select {
	case <-network.closed:
	case <-time.After(time.Second):
		t.Fatal("Network was not closed after startup failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := client.QueryProfile(ctx, &argusv1.QueryProfileRequest{}); err == nil {
		t.Fatal("EngineClient should be closed after startup failure")
	}
}

// TestLifecycleHTTPBindFailure 端口被占用时启动失败，且不创建 app.sock。
func TestLifecycleHTTPBindFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	appSocket := testSocketPath(t, "app.sock")
	app := &App{Logger: zap.NewNop(), Engine: gin.New(), IPCRuntime: newTestRuntime()}
	cfg := &config.Config{Server: config.Server{Port: port}, IPC: config.IPC{AppSocket: appSocket}}
	lc := &serverLifecycle{cfg: cfg, app: app, quit: make(chan os.Signal, 1), timeout: 2 * time.Second}

	if err := lc.run(); err == nil {
		t.Fatal("run should fail when HTTP port is occupied")
	}
	// gRPC 不应启动：app.sock 必须不存在。
	if _, err := os.Lstat(appSocket); !os.IsNotExist(err) {
		t.Fatalf("app.sock should not be created when HTTP bind fails, err=%v", err)
	}
}

// TestLifecycleGRPCBindFailure app.sock 被活跃进程占用时启动失败，HTTP listener 被回收。
func TestLifecycleGRPCBindFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	port := freePort(t)
	appSocket := testSocketPath(t, "app.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: appSocket, Net: "unix"})
	if err != nil {
		t.Fatalf("occupy app.sock: %v", err)
	}
	defer ln.Close()

	app := &App{Logger: zap.NewNop(), Engine: gin.New(), IPCRuntime: newTestRuntime()}
	cfg := &config.Config{Server: config.Server{Port: port}, IPC: config.IPC{AppSocket: appSocket}}
	lc := &serverLifecycle{cfg: cfg, app: app, quit: make(chan os.Signal, 1), timeout: 2 * time.Second}

	if err := lc.run(); err == nil {
		t.Fatal("run should fail when app.sock is occupied by an active listener")
	}
	// 活跃 socket 不能被 unlink。
	if _, err := os.Lstat(appSocket); err != nil {
		t.Fatalf("active app.sock was removed: %v", err)
	}
	// HTTP listener 必须已回收。
	ln2, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("http listener not released after gRPC bind failure: %v", err)
	}
	_ = ln2.Close()
}

// fakeEngineService 只实现 QueryProfile 的最小 Engine 替身（供关闭验证）。
type fakeEngineService struct {
	argusv1.UnimplementedEngineServiceServer
}

func (f *fakeEngineService) QueryProfile(context.Context, *argusv1.QueryProfileRequest) (*argusv1.QueryProfileResponse, error) {
	return &argusv1.QueryProfileResponse{Profile: &argusv1.PlatformProfileInfo{PlatformId: "fake"}}, nil
}

// TestLifecycleGracefulShutdown 触发退出信号后 HTTP/gRPC 优雅停止、app.sock 清理、EngineClient 关闭。
func TestLifecycleGracefulShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	port := freePort(t)
	appSocket := testSocketPath(t, "app.sock")
	engineSocket := testSocketPath(t, "engine.sock")

	// fake Engine 服务端（验证 EngineClient 关闭前可调用）。
	eln, err := net.ListenUnix("unix", &net.UnixAddr{Name: engineSocket, Net: "unix"})
	if err != nil {
		t.Fatalf("listen engine socket: %v", err)
	}
	esrv := grpc.NewServer()
	argusv1.RegisterEngineServiceServer(esrv, &fakeEngineService{})
	go func() { _ = esrv.Serve(eln) }()
	defer esrv.Stop()

	client, err := engineipc.NewEngineClient(&config.Config{IPC: config.IPC{EngineSocket: engineSocket}})
	if err != nil {
		t.Fatalf("NewEngineClient: %v", err)
	}
	// 关闭前可调用。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
	cancel()
	if err != nil {
		t.Fatalf("QueryProfile before shutdown: %v", err)
	}

	app := &App{Logger: zap.NewNop(), Engine: gin.New(), IPCRuntime: newTestRuntime(), EngineClient: client}
	cfg := &config.Config{
		Server: config.Server{Port: port},
		IPC:    config.IPC{AppSocket: appSocket, EngineSocket: engineSocket},
	}
	quit := make(chan os.Signal, 1)
	lc := &serverLifecycle{cfg: cfg, app: app, quit: quit, timeout: 3 * time.Second}

	done := make(chan error, 1)
	go func() { done <- lc.run() }()
	waitHTTP(t, port)
	waitSocketFile(t, appSocket)

	quit <- syscall.SIGTERM
	if err := <-done; err != nil {
		t.Fatalf("run after graceful shutdown: %v", err)
	}
	// app.sock 必须按 identity 清理。
	if _, err := os.Lstat(appSocket); !os.IsNotExist(err) {
		t.Fatalf("app.sock should be removed after shutdown, err=%v", err)
	}
	// EngineClient 必须已关闭：调用应失败。
	ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, err = client.QueryProfile(ctx, &argusv1.QueryProfileRequest{})
	cancel()
	if err == nil {
		t.Fatal("EngineClient should be closed after shutdown")
	}
}

// waitSocketFile 轮询等待 socket 文件出现。
func waitSocketFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket file %s did not appear", path)
}

// TestLifecycleSharedTimeout 阻塞中的 HTTP 请求使停机超时，但 run 在超时窗口内返回。
func TestLifecycleSharedTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	port := freePort(t)
	appSocket := testSocketPath(t, "app.sock")

	engine := gin.New()
	release := make(chan struct{})
	entered := make(chan struct{})
	engine.GET("/block", func(c *gin.Context) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		c.Status(http.StatusOK)
	})

	app := &App{Logger: zap.NewNop(), Engine: engine, IPCRuntime: newTestRuntime()}
	cfg := &config.Config{Server: config.Server{Port: port}, IPC: config.IPC{AppSocket: appSocket}}
	quit := make(chan os.Signal, 1)
	lc := &serverLifecycle{cfg: cfg, app: app, quit: quit, timeout: 200 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- lc.run() }()
	waitHTTP(t, port)

	// 发起阻塞中的 HTTP 请求。
	go func() { _, _ = http.Get(fmt.Sprintf("http://127.0.0.1:%d/block", port)) }()
	<-entered

	start := time.Now()
	quit <- syscall.SIGTERM
	if err := <-done; err != nil {
		t.Fatalf("run with forced timeout: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("shutdown took too long: %v", elapsed)
	}
	close(release)
}

// TestLifecycleServeError gRPC serve error 走统一关闭路径并返回退出原因。
func TestLifecycleServeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	port := freePort(t)
	rt := &fakeIPCRuntime{serveErrCh: make(chan error, 1), shutdownCh: make(chan struct{}, 1)}
	app := &App{Logger: zap.NewNop(), Engine: gin.New(), IPCRuntime: rt}
	cfg := &config.Config{Server: config.Server{Port: port}, IPC: config.IPC{AppSocket: testSocketPath(t, "app.sock")}}
	quit := make(chan os.Signal, 1)
	lc := &serverLifecycle{cfg: cfg, app: app, quit: quit, timeout: 3 * time.Second}

	done := make(chan error, 1)
	go func() { done <- lc.run() }()
	waitHTTP(t, port)

	rt.serveErrCh <- errors.New("listener failed")
	if err := <-done; err == nil {
		t.Fatal("run should return the serve error")
	}
	select {
	case <-rt.shutdownCh:
	default:
		t.Fatal("shutdown should be called on serve error")
	}
}
