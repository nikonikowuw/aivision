package service

import (
	"testing"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
)

// points 便捷构造归一化检测点列表。
func points(xy ...float64) []model.DetectionPoint {
	out := make([]model.DetectionPoint, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, model.DetectionPoint{X: xy[i], Y: xy[i+1]})
	}
	return out
}

// TestValidateRules 覆盖 design §6 五项几何校验的正反例。
func TestValidateRules(t *testing.T) {
	tests := []struct {
		name     string
		rules    []model.DetectionRule
		wantErr  bool
		wantCode int
	}{
		{
			name:    "空规则列表合法",
			rules:   nil,
			wantErr: false,
		},
		{
			name: "合法 ROI 矩形与 LINE 折线",
			rules: []model.DetectionRule{
				{Role: model.DetectionRuleRoleROI, Points: points(0.1, 0.1, 0.9, 0.1, 0.9, 0.9, 0.1, 0.9)},
				{Role: model.DetectionRuleRoleLine, LineDirection: model.DetectionLineDirectionAToB,
					Points: points(0.2, 0.5, 0.8, 0.5)},
			},
			wantErr: false,
		},
		{
			name:     "非法角色",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleUnspecified, Points: points(0.1, 0.1, 0.2, 0.2, 0.3, 0.3)}},
			wantErr:  true,
			wantCode: errno.CodeInvalidParam,
		},
		{
			name:     "坐标越界",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleROI, Points: points(0.1, 0.1, 0.2, 1.5, 0.3, 0.3)}},
			wantErr:  true,
			wantCode: errno.CodeRuleOutOfBounds,
		},
		{
			name:     "ROI 顶点不足",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleROI, Points: points(0.1, 0.1, 0.2, 0.2)}},
			wantErr:  true,
			wantCode: errno.CodeRuleTooFewPoints,
		},
		{
			name:     "LINE 顶点不足",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleLine, Points: points(0.1, 0.1)}},
			wantErr:  true,
			wantCode: errno.CodeRuleTooFewPoints,
		},
		{
			name:     "MASK 顶点不足",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleMask, Points: points(0.1, 0.1, 0.2, 0.2)}},
			wantErr:  true,
			wantCode: errno.CodeRuleTooFewPoints,
		},
		{
			// 蝴蝶结多边形：边 0-1 与边 2-3 交叉于中心。
			name:     "ROI 自交",
			rules:    []model.DetectionRule{{Role: model.DetectionRuleRoleROI, Points: points(0, 0, 1, 1, 0, 1, 1, 0)}},
			wantErr:  true,
			wantCode: errno.CodeRuleSelfIntersect,
		},
		{
			name: "ROI 携带方向字段",
			rules: []model.DetectionRule{{
				Role: model.DetectionRuleRoleROI, LineDirection: model.DetectionLineDirectionAToB,
				Points: points(0.1, 0.1, 0.9, 0.1, 0.9, 0.9),
			}},
			wantErr:  true,
			wantCode: errno.CodeInvalidParam,
		},
		{
			name: "MASK 携带方向字段",
			rules: []model.DetectionRule{{
				Role: model.DetectionRuleRoleMask, LineDirection: model.DetectionLineDirectionBToA,
				Points: points(0.1, 0.1, 0.9, 0.1, 0.9, 0.9),
			}},
			wantErr:  true,
			wantCode: errno.CodeInvalidParam,
		},
		{
			// 凹多边形（非自交）应通过。
			name:    "凹多边形合法",
			rules:   []model.DetectionRule{{Role: model.DetectionRuleRoleROI, Points: points(0, 0, 1, 0, 0.5, 0.5, 1, 1, 0, 1)}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRules(tt.rules)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateRules(%v) unexpectedly passed", tt.rules)
				}
				if !errno.Is(err, tt.wantCode) {
					t.Fatalf("ValidateRules(%v) error code = %v, want %d", tt.rules, err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateRules(%v) = %v, want nil", tt.rules, err)
			}
		})
	}
}
