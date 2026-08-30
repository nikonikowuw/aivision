package service

import (
	"context"

	"go.uber.org/zap"

	"argus/app/internal/pkg/engineipc"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// deviceIDPlaceholder 期望状态中的占位设备标识。
// Engine 侧仅把 device_id 用于日志上下文（多设备定位），本平台当前单机部署，
// 设备身份体系未定义；平台集成任务（多设备/集中管理）落地时替换为真实标识。
const deviceIDPlaceholder = "edge-node-01"

// desiredStateAdapter 实现 engineipc.DesiredStateAdapter：
// 以 repository 的 revision 与全量快照应答 Engine 的 GetDesiredState（design §3.3）。
type desiredStateAdapter struct {
	repo repository.TaskRepository
	log  *zap.Logger
}

// NewDesiredStateAdapter 创建 desiredStateAdapter。
func NewDesiredStateAdapter(repo repository.TaskRepository, log *zap.Logger) engineipc.DesiredStateAdapter {
	if log == nil {
		log = zap.NewNop()
	}
	return &desiredStateAdapter{repo: repo, log: log}
}

// DesiredState 返回当前 revision 下的完整期望状态。
// 即使 currentRevision 未变也返回完整快照：revision 是否变大的判断在 Engine 侧
// （main.cpp control_plane_thread），返回空快照会让 Engine 无法区分
// 「没变化」与「配置被清空」（design §3.3）。
// 任何错误直接返回，由 engineipc 归一化为 INTERNAL_ERROR fail closed，
// 不让 Engine 拿到 revision=0 的「配置被清空」快照。
func (a *desiredStateAdapter) DesiredState(ctx context.Context, _ uint64) (*argusv1.DesiredState, error) {
	state, err := a.repo.LoadDesiredState(ctx)
	if err != nil {
		return nil, err
	}
	state.DeviceId = deviceIDPlaceholder
	return state, nil
}
