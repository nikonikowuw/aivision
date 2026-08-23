//go:build linux

package netconfig

import (
	"context"
	"slices"
	"testing"
)

// TestLinuxPlatformCapabilities 断言 LinuxPlatform 只声明 multi-address（M2 / AC1 / D6）。
func TestLinuxPlatformCapabilities(t *testing.T) {
	p, err := NewLinuxPlatform("", false)
	if err != nil {
		t.Fatalf("NewLinuxPlatform failed: %v", err)
	}
	caps := p.Capabilities(context.Background())
	wantModes := []NetworkMode{NetworkModeMultiAddress}
	if !slices.Equal(caps.SupportedModes, wantModes) {
		t.Errorf("SupportedModes = %v, want %v", caps.SupportedModes, wantModes)
	}
	if slices.Contains(caps.SupportedModes, NetworkModeActiveBackup) {
		t.Errorf("LinuxPlatform must not declare active-backup before platform realization (D6)")
	}
}
