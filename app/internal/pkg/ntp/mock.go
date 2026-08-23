package ntp

import (
	"context"
	"sync"
	"time"
)

// MockExecutor 纯内存测试用执行器
type MockExecutor struct {
	mu           sync.RWMutex
	synced       bool
	source       string
	offset       string
	lastSyncTime *string
	servers      []string
	ntpEnabled   bool
	manualTime   *time.Time

	// 可注入错误
	ApplyErr   error
	DisableErr error
	SyncErr    error
	SetTimeErr error
	StatusErr  error
}

// NewMockExecutor 创建测试用 Mock 执行器
func NewMockExecutor() *MockExecutor {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	return &MockExecutor{
		synced:       true,
		source:       "pool.ntp.org",
		offset:       "+0.001s",
		lastSyncTime: &nowStr,
		servers:      []string{"pool.ntp.org", "ntp.aliyun.com"},
		ntpEnabled:   true,
	}
}

func (m *MockExecutor) GetStatus(ctx context.Context) (*SyncStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.StatusErr != nil {
		return nil, m.StatusErr
	}
	return &SyncStatus{
		Synced:       m.synced,
		Source:       m.source,
		Offset:       m.offset,
		LastSyncTime: m.lastSyncTime,
	}, nil
}

func (m *MockExecutor) ApplyNTP(ctx context.Context, servers []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ApplyErr != nil {
		return m.ApplyErr
	}
	m.servers = append([]string(nil), servers...)
	m.ntpEnabled = true
	m.synced = true
	if len(servers) > 0 {
		m.source = servers[0]
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	m.lastSyncTime = &nowStr
	return nil
}

func (m *MockExecutor) DisableNTP(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DisableErr != nil {
		return m.DisableErr
	}
	m.ntpEnabled = false
	m.synced = false
	m.source = modeManual
	m.offset = "0.000s"
	return nil
}

func (m *MockExecutor) SyncNow(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SyncErr != nil {
		return m.SyncErr
	}
	m.synced = true
	nowStr := time.Now().UTC().Format(time.RFC3339)
	m.lastSyncTime = &nowStr
	return nil
}

func (m *MockExecutor) SetSystemTime(ctx context.Context, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SetTimeErr != nil {
		return m.SetTimeErr
	}
	m.manualTime = &t
	m.ntpEnabled = false
	m.synced = false
	m.source = modeManual
	m.offset = "0.000s"
	nowStr := t.UTC().Format(time.RFC3339)
	m.lastSyncTime = &nowStr
	return nil
}
