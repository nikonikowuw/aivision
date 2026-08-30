package ntp

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// NewExecutor 根据当前运行平台与系统安装的工具自动探测并创建合适的 Executor
func NewExecutor() Executor {
	switch runtime.GOOS {
	case "darwin":
		return newDarwinExecutor()
	case "linux":
		return newLinuxExecutor()
	default:
		return newUnavailableExecutor("unsupported operating system")
	}
}

type unavailableExecutor struct {
	reason string
}

func newUnavailableExecutor(reason string) Executor {
	return &unavailableExecutor{reason: reason}
}

func (e *unavailableExecutor) err() error {
	return fmt.Errorf("ntp executor unavailable: %s", e.reason)
}

func (e *unavailableExecutor) GetStatus(context.Context) (*SyncStatus, error) {
	return nil, e.err()
}

func (e *unavailableExecutor) ApplyNTP(context.Context, []string) error {
	return e.err()
}

func (e *unavailableExecutor) DisableNTP(context.Context) error {
	return e.err()
}

func (e *unavailableExecutor) SyncNow(context.Context) error {
	return e.err()
}

func (e *unavailableExecutor) SetSystemTime(context.Context, time.Time) error {
	return e.err()
}
