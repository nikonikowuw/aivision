package ntp

import (
	"context"
	"time"
)

// 对时模式取值（与 model.TimeModeNTP / model.TimeModeManual 保持一致，避免字面量散落）。
const (
	modeNTP    = "ntp"
	modeManual = "manual"
)

// SyncStatus 实时时钟同步状态
type SyncStatus struct {
	Synced       bool    `json:"synced"`       // 系统时钟是否已完成同步
	Source       string  `json:"source"`       // 当前有效同步源
	Offset       string  `json:"offset"`       // 时钟偏移量（如 "+0.003s"）
	LastSyncTime *string `json:"lastSyncTime"` // 最近一次同步时间 (RFC3339 字符串)
}

// Executor NTP 底层执行器接口
type Executor interface {
	// GetStatus 查询实时同步状态
	GetStatus(ctx context.Context) (*SyncStatus, error)

	// ApplyNTP 应用 NTP 服务器列表并启用/重载 NTP 守护进程
	ApplyNTP(ctx context.Context, servers []string) error

	// DisableNTP 停用 NTP 服务（切到手动模式或执行手动设时前）
	DisableNTP(ctx context.Context) error

	// SyncNow 触发 NTP 守护进程立即对时
	SyncNow(ctx context.Context) error

	// SetSystemTime 手动修改系统时钟
	SetSystemTime(ctx context.Context, t time.Time) error
}
