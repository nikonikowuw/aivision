// Package main 是产品后端启动入口：装配 Gin HTTP server 与 gRPC over UDS
// （app.sock 入站 / engine.sock 出站）的联合生命周期。
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"

	"argus/app/internal/pkg/config"
)

const (
	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
)

// serverLifecycle 封装 HTTP TCP 与 gRPC UDS 的联合启动/停机，便于测试注入退出触发与超时。
type serverLifecycle struct {
	cfg     *config.Config
	app     *App
	quit    <-chan os.Signal
	timeout time.Duration
}

// run 执行 HTTP + gRPC 联合生命周期：
//   - 任一 listener 启动失败都会回收已获得资源并返回错误（进程启动失败）；
//   - Engine client 目标 socket 不存在不属于启动失败；
//   - HTTP/gRPC serve error 与 OS signal 走同一关闭路径，返回退出原因。
func (l *serverLifecycle) run() error {
	log := l.app.Logger

	// 1. 预绑定 HTTP TCP listener（先于 app.sock，保证 gRPC 绑定失败时可回收）。
	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", l.cfg.Server.Port))
	if err != nil {
		l.cleanupStartupResources()
		return fmt.Errorf("bind http listener: %w", err)
	}

	// 2. 绑定 gRPC app.sock。
	if l.app.IPCRuntime == nil {
		_ = httpLn.Close()
		l.cleanupStartupResources()
		return errors.New("ipc runtime not wired")
	}
	if err := l.app.IPCRuntime.Start(l.cfg.IPC.AppSocket); err != nil {
		_ = httpLn.Close()
		l.cleanupStartupResources()
		return fmt.Errorf("start ipc runtime: %w", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", l.cfg.Server.Port),
		Handler:           l.app.Engine,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	// 3. 后台执行 HTTP Serve；serve error 上报到统一退出通道。
	httpErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	}()
	log.Info("server listening",
		zap.Int("port", l.cfg.Server.Port),
		zap.String("app_socket", l.cfg.IPC.AppSocket))

	// 4. 等待退出触发：OS signal、HTTP serve error 或 gRPC serve error。
	var exitErr error
	select {
	case sig := <-l.quit:
		log.Info("received signal", zap.String("signal", sig.String()))
	case err := <-httpErr:
		log.Error("http server failed", zap.Error(err))
		exitErr = err
	case err := <-l.app.IPCRuntime.Errors():
		log.Error("grpc ipc server failed", zap.Error(err))
		exitErr = err
	}

	// 5. 统一关闭路径（同一个 deadline 窗口）。
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	l.shutdown(ctx, srv)
	return exitErr
}

// cleanupStartupResources 回收 server listener 尚未全部启动时已经获得的依赖。
// main 在进入 run 前已经启动 Network 并创建 EngineClient，因此 bind 失败也必须走这里。
func (l *serverLifecycle) cleanupStartupResources() {
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	if l.app == nil {
		return
	}
	if l.app.IPCRuntime != nil {
		if err := l.app.IPCRuntime.Shutdown(ctx); err != nil {
			l.app.Logger.Error("grpc ipc startup cleanup failed", zap.Error(err))
		}
	}
	l.closeDependencies(ctx)
}

// closeDependencies 关闭非 listener 依赖；每项失败只记录，不阻断后续清理。
func (l *serverLifecycle) closeDependencies(ctx context.Context) {
	if l.app == nil {
		return
	}
	// 停止后台配额获取循环，避免 goroutine 泄漏
	if l.app.TaskService != nil {
		l.app.TaskService.Shutdown()
	}
	if l.app.EngineClient != nil {
		if err := l.app.EngineClient.Close(); err != nil {
			l.app.Logger.Error("engine client close failed", zap.Error(err))
		}
	}
	if l.app.Network != nil {
		if err := l.app.Network.Close(ctx); err != nil {
			l.app.Logger.Error("network service close failed", zap.Error(err))
		}
	}
}

// shutdown 在同一个 context 内并发停止 HTTP 与 gRPC admission，随后关闭
// EngineClient，再用剩余时间关闭 Network。任何单项失败只记录，不阻断其余资源关闭。
func (l *serverLifecycle) shutdown(ctx context.Context, srv *http.Server) {
	log := l.app.Logger
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("http graceful shutdown failed", zap.Error(err))
		}
	}()
	go func() {
		defer wg.Done()
		if err := l.app.IPCRuntime.Shutdown(ctx); err != nil {
			log.Error("grpc ipc shutdown failed", zap.Error(err))
		}
	}()
	wg.Wait()

	l.closeDependencies(ctx)
}
