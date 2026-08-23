package ntp_test

import (
	"context"
	"testing"
	"time"

	"niko-vue-admin/app/internal/pkg/ntp"
)

func TestMockExecutor(t *testing.T) {
	ctx := context.Background()
	exec := ntp.NewMockExecutor()

	// 1. 获取初始状态
	status, err := exec.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Synced {
		t.Error("synced = false, want true")
	}
	if status.Source != "pool.ntp.org" {
		t.Errorf("source = %q, want pool.ntp.org", status.Source)
	}

	// 2. 应用 NTP 配置
	if err := exec.ApplyNTP(ctx, []string{"ntp.aliyun.com", "time.google.com"}); err != nil {
		t.Fatalf("ApplyNTP: %v", err)
	}
	status, err = exec.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Synced {
		t.Error("synced = false, want true")
	}
	if status.Source != "ntp.aliyun.com" {
		t.Errorf("source = %q, want ntp.aliyun.com", status.Source)
	}

	// 3. 立即同步
	if err := exec.SyncNow(ctx); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	// 4. 手动设时
	targetTime := time.Date(2025, 8, 22, 10, 0, 0, 0, time.UTC)
	if err := exec.SetSystemTime(ctx, targetTime); err != nil {
		t.Fatalf("SetSystemTime: %v", err)
	}
	status, err = exec.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Synced {
		t.Error("synced = true, want false")
	}
	if status.Source != "manual" {
		t.Errorf("source = %q, want manual", status.Source)
	}

	// 5. 禁用 NTP
	if err := exec.DisableNTP(ctx); err != nil {
		t.Fatalf("DisableNTP: %v", err)
	}
}
