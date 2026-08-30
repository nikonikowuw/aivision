//go:build darwin

package ntp

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type darwinExecutor struct {
	mu           sync.RWMutex
	mode         string
	lastSyncTime *string
	servers      []string
}

// NewDarwinExecutor 创建 macOS 执行器
func NewDarwinExecutor() Executor {
	return &darwinExecutor{
		mode:    modeNTP,
		servers: []string{"pool.ntp.org"},
	}
}

func (d *darwinExecutor) GetStatus(ctx context.Context) (*SyncStatus, error) {
	d.mu.RLock()
	currentMode := d.mode
	lastSync := d.lastSyncTime
	srvList := d.servers
	d.mu.RUnlock()

	// 手动模式下返回手动状态
	if currentMode == modeManual {
		return &SyncStatus{
			Synced:       false,
			Source:       "manual",
			Offset:       "0.000s",
			LastSyncTime: lastSync,
		}, nil
	}

	currentServer := "pool.ntp.org"
	if len(srvList) > 0 && srvList[0] != "" {
		currentServer = srvList[0]
	}

	status := &SyncStatus{
		Synced:       false,
		Source:       currentServer,
		Offset:       "0.000s",
		LastSyncTime: lastSync,
	}

	// 使用 sntp 探测实际同步状态与 offset；探测失败时不能伪报已同步。
	sntpCmd := exec.CommandContext(ctx, "sntp", "-d", currentServer)
	sntpOut, err := sntpCmd.CombinedOutput()
	if err == nil {
		status.Synced = true
		offsetRegex := regexp.MustCompile(`([+-]?[0-9.]+)\s*\+/-`)
		if matches := offsetRegex.FindStringSubmatch(string(sntpOut)); len(matches) > 1 {
			status.Offset = strings.TrimSpace(matches[1]) + "s"
		}
	}

	return status, nil
}

func (d *darwinExecutor) ApplyNTP(ctx context.Context, servers []string) error {
	if len(servers) > 0 && servers[0] != "" {
		if out, err := exec.CommandContext(ctx, "systemsetup", "-setnetworktimeserver", servers[0]).CombinedOutput(); err != nil {
			return fmt.Errorf("systemsetup -setnetworktimeserver failed (%v): %s", err, string(out))
		}
	}
	if out, err := exec.CommandContext(ctx, "systemsetup", "-setusingnetworktime", "on").CombinedOutput(); err != nil {
		return fmt.Errorf("systemsetup -setusingnetworktime on failed (%v): %s", err, string(out))
	}

	d.mu.Lock()
	d.mode = modeNTP
	d.servers = append([]string(nil), servers...)
	d.mu.Unlock()
	return nil
}

func (d *darwinExecutor) DisableNTP(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "systemsetup", "-setusingnetworktime", "off").CombinedOutput(); err != nil {
		return fmt.Errorf("systemsetup -setusingnetworktime off failed (%v): %s", err, string(out))
	}

	d.mu.Lock()
	d.mode = modeManual
	d.lastSyncTime = nil
	d.mu.Unlock()
	return nil
}

func (d *darwinExecutor) SyncNow(ctx context.Context) error {
	d.mu.RLock()
	server := "pool.ntp.org"
	if len(d.servers) > 0 && d.servers[0] != "" {
		server = d.servers[0]
	}
	d.mu.RUnlock()

	if out, err := exec.CommandContext(ctx, "sntp", "-sS", server).CombinedOutput(); err != nil {
		return fmt.Errorf("sntp -sS failed (%v): %s", err, string(out))
	}
	// 同步后的探测为 best-effort，失败不影响同步结果
	_ = exec.CommandContext(ctx, "sntp", server).Run()

	d.mu.Lock()
	d.mode = modeNTP
	nowStr := time.Now().UTC().Format(time.RFC3339)
	d.lastSyncTime = &nowStr
	d.mu.Unlock()
	return nil
}

func (d *darwinExecutor) SetSystemTime(ctx context.Context, t time.Time) error {
	// 先禁用 NTP 避免被覆盖
	if err := d.DisableNTP(ctx); err != nil {
		return fmt.Errorf("disable ntp before set system time: %w", err)
	}

	// date 的设时参数按系统本地时区解释，先将 RFC3339 瞬时转换为本地墙上时间。
	timeStr := t.Local().Format("010215042006.05")
	cmd := exec.CommandContext(ctx, "date", timeStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("设置系统时间失败 (需要 root/管理员权限运行后端服务): %s (exit: %v)", strings.TrimSpace(string(out)), err)
	}

	d.mu.Lock()
	d.mode = modeManual
	d.mu.Unlock()

	return nil
}
