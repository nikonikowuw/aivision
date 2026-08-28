package service

import (
	"errors"

	"niko-vue-admin/app/internal/model"
)

// DefaultAnalysisFPS analysis_fps 为非正数时的默认采样帧率。
// 必须与 Engine 侧保持一致（engine/src/core/ipc/uds_server.cpp reconcile_instance）。
const DefaultAnalysisFPS = 25

// ErrFPSTierExceeded 请求 FPS 超过算法包声明的最高档位。
// 按契约直接拒绝，不钳位到最高档（manifest-schema §2.3）。
var ErrFPSTierExceeded = errors.New("analysis_fps exceeds highest declared tier")

// ResolveUnits 将请求的采样 FPS 换算为资源账本 units，复刻 Engine 侧权威实现
// （engine/src/core/ipc/uds_server.cpp reconcile_instance 的档位换算），三条不变式：
//  1. analysisFPS <= 0 时按 DefaultAnalysisFPS 处理；
//  2. 取第一个 tier.FPS >= target 的档位 units（向上取档，非最近邻）；
//  3. target 超过最高档返回 ErrFPSTierExceeded，而非钳到最高档。
//
// tiers 必须按 FPS 严格递增——由 manifest-schema §2.3 保证，本函数不重复校验。
// 任一条与 Engine 漂移都会造成「Go 放行 → Engine 拒绝 → 2 秒后 ERROR」的误判。
func ResolveUnits(tiers []model.FPSTier, analysisFPS int32) (uint32, error) {
	target := analysisFPS
	if target <= 0 {
		target = DefaultAnalysisFPS
	}
	for _, tier := range tiers {
		if tier.FPS >= target {
			return tier.Units, nil
		}
	}
	return 0, ErrFPSTierExceeded
}
