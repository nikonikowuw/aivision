//go:build linux

package ntp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	timesyncdDropInDir  = "/etc/systemd/timesyncd.conf.d"
	timesyncdDropInFile = "/etc/systemd/timesyncd.conf.d/aivision.conf"
)

type timesyncdExecutor struct{}

// NewTimesyncdExecutor 创建 Linux systemd-timesyncd 执行器
func NewTimesyncdExecutor() Executor {
	return &timesyncdExecutor{}
}

func (ts *timesyncdExecutor) GetStatus(ctx context.Context) (*SyncStatus, error) {
	cmd := exec.CommandContext(ctx, "timedatectl", "show-timesync")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("timedatectl show-timesync failed (%v): %s", err, string(out))
	}

	syncedOut, err := exec.CommandContext(ctx, "timedatectl", "show", "--property=NTPSynchronized", "--value").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("timedatectl show NTPSynchronized failed (%v): %s", err, string(syncedOut))
	}

	status := parseTimesyncdStatus(string(out))
	status.Synced = parseNTPsynchronized(string(syncedOut))
	return status, nil
}

func parseTimesyncdStatus(output string) *SyncStatus {
	status := &SyncStatus{
		Synced: false,
		Source: "",
		Offset: "0.000s",
	}
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, "ServerName="):
			status.Source = strings.TrimPrefix(line, "ServerName=")
		case strings.HasPrefix(line, "Offset="):
			status.Offset = strings.TrimPrefix(line, "Offset=")
		}
	}
	return status
}

func parseNTPsynchronized(output string) bool {
	value := strings.TrimSpace(output)
	if _, parsed, ok := strings.Cut(value, "="); ok {
		value = strings.TrimSpace(parsed)
	}
	return strings.EqualFold(value, "yes")
}

func (ts *timesyncdExecutor) ApplyNTP(ctx context.Context, servers []string) error {
	if err := os.MkdirAll(timesyncdDropInDir, 0755); err != nil {
		return fmt.Errorf("failed to create timesyncd drop-in dir: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("[Time]\n")
	buf.WriteString(fmt.Sprintf("NTP=%s\n", strings.Join(servers, " ")))

	if err := os.WriteFile(timesyncdDropInFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write timesyncd drop-in config: %w", err)
	}

	if out, err := exec.CommandContext(ctx, "timedatectl", "set-ntp", "true").CombinedOutput(); err != nil {
		return fmt.Errorf("timedatectl set-ntp true failed (%v): %s", err, string(out))
	}
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "systemd-timesyncd").CombinedOutput(); err != nil {
		return fmt.Errorf("restart systemd-timesyncd failed (%v): %s", err, string(out))
	}

	return nil
}

func (ts *timesyncdExecutor) DisableNTP(ctx context.Context) error {
	if err := os.Remove(timesyncdDropInFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove timesyncd drop-in config: %w", err)
	}
	if out, err := exec.CommandContext(ctx, "timedatectl", "set-ntp", "false").CombinedOutput(); err != nil {
		return fmt.Errorf("timedatectl set-ntp false failed (%v): %s", err, string(out))
	}
	// 停止已停止的服务属幂等操作，失败不影响停用结果，保持 best-effort
	_ = exec.CommandContext(ctx, "systemctl", "stop", "systemd-timesyncd").Run()

	return nil
}

func (ts *timesyncdExecutor) SyncNow(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "systemd-timesyncd")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restart systemd-timesyncd failed (%v): %s", err, string(out))
	}
	return nil
}

func (ts *timesyncdExecutor) SetSystemTime(ctx context.Context, t time.Time) error {
	// 先禁用 NTP 避免被覆盖
	if err := ts.DisableNTP(ctx); err != nil {
		return fmt.Errorf("disable ntp before set system time: %w", err)
	}
	return setSystemTimeLinux(ctx, t)
}
