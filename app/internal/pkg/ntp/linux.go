//go:build linux

package ntp

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// setSystemTimeLinux 手动修改系统时钟：设置系统时间并同步到硬件时钟。
func setSystemTimeLinux(ctx context.Context, t time.Time) error {
	timeStr := t.UTC().Format("2006-01-02 15:04:05")
	cmd := exec.CommandContext(ctx, "date", "-u", "-s", timeStr)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("date -s failed (%v): %s", err, string(out))
	}
	if out, err := exec.CommandContext(ctx, "hwclock", "--systohc").CombinedOutput(); err != nil {
		return fmt.Errorf("hwclock --systohc failed (%v): %s", err, string(out))
	}
	return nil
}
