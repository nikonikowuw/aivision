# 算法包通用区域规则迁移

## Goal

将当前算法包内部的 ROI、Mask、Line 规则处理迁移到 Engine，重新划分 SDK/C ABI 职责并保持算法包可验证、可兼容。

## Dependencies

- 依赖 `08-31-engine-region-rules` 和 `08-31-engine-tracker-line-crossing`。
- 以当前 `av_rule`、`instance_set_rules`、yolo26n/RKNN 实现为迁移基线。

## Requirements

- 明确算法包只负责模型推理、输出解析及必要的模型专属后处理。
- Engine 统一执行通用 ROI、Mask、Zone、Line 规则。
- 设计旧 ABI 的兼容、废弃或版本演进策略。
- 同步 vendored SDK、Proto、算法包测试和 consistency 检查。
- 删除重复的包内轨迹和几何规则实现。

## Acceptance Criteria

- [ ] macOS 和 RKNN 算法包不再独立实现通用区域规则。
- [ ] 迁移后检测结果、目标生命周期和越界事件语义一致。
- [ ] ABI/版本/offset/一致性测试通过。
- [ ] 旧配置的迁移或拒绝行为明确且可观测。

## Out of Scope

- MotionGate 算法。
- 后端/前端区域配置体验。
- 具体模型精度优化。
