//go:build darwin && cgo

package netconfig

import (
	"context"
	"slices"
	"testing"
)

// TestDarwinPlatformCapabilities 断言 DarwinPlatform 只声明 multi-address（M2 / AC1 / parent D2）。
func TestDarwinPlatformCapabilities(t *testing.T) {
	p, err := NewDarwinPlatform(false)
	if err != nil {
		t.Fatalf("NewDarwinPlatform failed: %v", err)
	}
	caps := p.Capabilities(context.Background())
	wantModes := []NetworkMode{NetworkModeMultiAddress}
	if !slices.Equal(caps.SupportedModes, wantModes) {
		t.Errorf("SupportedModes = %v, want %v", caps.SupportedModes, wantModes)
	}
	if slices.Contains(caps.SupportedModes, NetworkModeActiveBackup) {
		t.Errorf("DarwinPlatform must not declare active-backup (parent D2)")
	}
}
