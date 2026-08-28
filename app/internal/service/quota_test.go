package service

import (
	"errors"
	"testing"

	"niko-vue-admin/app/internal/model"
)

// TestResolveUnits 钉死与 Engine（uds_server.cpp reconcile_instance）逐条一致的
// 三条不变式：非正数默认 25、向上取第一个 >= target 的档位、超最高档拒绝而非钳位。
func TestResolveUnits(t *testing.T) {
	tiers := []model.FPSTier{
		{FPS: 5, Units: 60},
		{FPS: 15, Units: 150},
		{FPS: 25, Units: 220},
	}
	tests := []struct {
		name      string
		fps       int32
		wantUnits uint32
		wantErr   error
	}{
		{"低于最低档向上取档", 3, 60, nil},
		{"精确命中最低档", 5, 60, nil},
		{"落在档位之间向上取档", 6, 150, nil},
		{"精确命中最高档", 25, 220, nil},
		{"超过最高档拒绝而非钳位", 26, 0, ErrFPSTierExceeded},
		{"零值默认按 25 处理", 0, 220, nil},
		{"负值默认按 25 处理", -1, 220, nil},
		{"空档位表直接拒绝", 25, 0, ErrFPSTierExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tiers
			if tt.name == "空档位表直接拒绝" {
				input = nil
			}
			got, err := ResolveUnits(input, tt.fps)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveUnits(%v, %d) error = %v, want %v", input, tt.fps, err, tt.wantErr)
			}
			if got != tt.wantUnits {
				t.Fatalf("ResolveUnits(%v, %d) = %d units, want %d", input, tt.fps, got, tt.wantUnits)
			}
		})
	}
}
