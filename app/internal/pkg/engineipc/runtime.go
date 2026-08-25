package engineipc

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// Runtime 是入站 gRPC server（app.sock）的 one-shot 生命周期对象：
// Start 同步完成安全绑定后才返回，随后后台执行 Serve；Shutdown 在超时窗口内
// 先 GracefulStop，超时强制 Stop，并清理自有 socket。
type Runtime struct {
	log       *zap.Logger
	server    *grpc.Server
	owner     *SocketOwner
	mu        sync.Mutex
	started   bool
	stopped   bool
	serveErr  chan error
	serveDone chan struct{}
}

// NewRuntime 构造入站 gRPC server 并注册 ControlPlane/Report 服务。
// 业务 adapter 由调用方显式注入（生产为 unavailable fail-closed 适配器，测试为 recording fakes）。
func NewRuntime(log *zap.Logger, desiredState DesiredStateAdapter, report ReportAdapter) *Runtime {
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recoveryServerInterceptor(log),
			loggingServerInterceptor(log),
		),
	)
	aivisionv1.RegisterControlPlaneServiceServer(server, &controlPlaneService{
		adapter: desiredState,
		log:     log,
	})
	aivisionv1.RegisterReportServiceServer(server, newReportService(report, log))
	return &Runtime{
		log:       log,
		server:    server,
		serveErr:  make(chan error, 1),
		serveDone: make(chan struct{}),
	}
}

// Start 同步完成 app.sock 安全绑定与权限设置，然后后台执行 gRPC Serve。
// 重复 Start 返回错误；绑定失败时进程不得继续启动。
func (r *Runtime) Start(socketPath string) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return fmt.Errorf("engineipc runtime already started")
	}
	owner, err := BindAppSocket(socketPath)
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("bind app socket %s: %w", socketPath, err)
	}
	r.owner = owner
	r.started = true
	r.mu.Unlock()

	go r.serve(owner)
	r.log.Info("gRPC ipc server listening", zap.String("socket", socketPath))
	return nil
}

func (r *Runtime) serve(owner *SocketOwner) {
	defer close(r.serveDone)
	err := r.server.Serve(owner.Listener())
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		select {
		case r.serveErr <- err:
		default:
		}
	}
}

// Errors 返回 Serve 阶段的非正常退出错误（如 listener 失效）。
func (r *Runtime) Errors() <-chan error {
	return r.serveErr
}

// Shutdown 停止 gRPC server 并清理自有 socket：
//   - 未 Start：确定性的无操作；
//   - 重复 Shutdown：确定性的无操作；
//   - 先 GracefulStop，context 到期时强制 Stop，等待 Serve 退出后按 identity 删除 socket。
func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	if !r.started || r.stopped {
		r.mu.Unlock()
		return nil
	}
	r.stopped = true
	owner := r.owner
	r.mu.Unlock()

	gracefulDone := make(chan struct{})
	go func() {
		r.server.GracefulStop()
		close(gracefulDone)
	}()
	select {
	case <-gracefulDone:
	case <-ctx.Done():
		r.log.Warn("gRPC graceful stop timed out, forcing stop",
			zap.Error(ctx.Err()))
		r.server.Stop()
	}
	<-r.serveDone

	if owner != nil {
		if err := owner.Cleanup(); err != nil {
			return fmt.Errorf("cleanup app socket: %w", err)
		}
	}
	return nil
}

// loggingServerInterceptor 记录 method、duration 与最终 gRPC code。
// 成功调用使用 debug，失败使用 warn/error。禁止记录 request/response、
// RTSP URL、参数 JSON 或凭据。
func loggingServerInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		dur := time.Since(start)
		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.Duration("duration", dur),
			zap.String("code", code.String()),
		}
		if err != nil {
			log.Warn("grpc call failed", fields...)
		} else {
			log.Debug("grpc call", fields...)
		}
		return resp, err
	}
}

// recoveryServerInterceptor 捕获 handler panic，记录内部错误并返回 codes.Internal，
// 防止单个请求崩溃整个 gRPC server。
func recoveryServerInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("grpc handler panic",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
					zap.ByteString("stack", debug.Stack()))
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// UnavailableDesiredStateAdapter 返回生产默认 fail-closed 的 DesiredStateAdapter。
// Wire 装配时显式注入；测试注入 recording fakes。
func UnavailableDesiredStateAdapter() DesiredStateAdapter {
	return unavailableDesiredStateAdapter{}
}

// UnavailableReportAdapter 返回生产默认 fail-closed 的 ReportAdapter。
func UnavailableReportAdapter() ReportAdapter {
	return unavailableReportAdapter{}
}
