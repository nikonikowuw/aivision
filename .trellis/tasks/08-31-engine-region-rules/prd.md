# Engine 推理后区域规则与业务过滤

## Goal

将算法推理后的 ROI、Mask、Zone 统一收归 Engine，支持多个多边形区域和业务规则过滤；不承担 MotionGate 的推理前运动判断。

## Dependencies

- 依赖 `08-31-engine-tracker-line-crossing` 提供稳定目标轨迹和锚点。
- 依赖 `08-31-algo-package-rule-migration` 明确算法包规则职责迁移方式。
- 可复用 `08-31-frigate-motion-gate` 的区域坐标校验，但不依赖其运行时状态。

## Requirements

- Engine 在算法结果后处理 ROI、Mask、Zone。
- 支持多个区域、多目标类型和 required/excluded 语义。
- 统一使用归一化坐标和明确的目标锚点规则。
- 区域过滤不得破坏目标轨迹生命周期。
- 区域规则更新具备原子性和可观测错误。

## Acceptance Criteria

- [ ] 多个 ROI、Mask、Zone 的组合行为有固定测试。
- [ ] 区域外目标过滤、区域内目标保留和边界点行为正确。
- [ ] 过滤结果与目标跟踪状态相互独立。
- [ ] 配置错误不会替换当前生效规则。

## Out of Scope

- MotionGate 实现。
- 模型输入裁剪和多 ROI 多次推理。
- 后端和前端配置页面。
