# 区域规则后端前端配置

## Goal

为 Engine 区域规则提供后端 DesiredState/API 和前端可视化绘制、编辑、校验能力。

## Dependencies

- 依赖 `08-31-engine-region-rules` 的区域语义。
- 依赖 `08-31-engine-tracker-line-crossing` 的越界线和方向契约。
- 依赖 `08-31-algo-package-rule-migration` 的最终配置/ABI 边界。

## Requirements

- 后端持久化和下发区域、规则、方向、算法实例绑定关系。
- 遵循 DesiredState/applied revision 和原子配置更新约定。
- 前端支持多边形、屏蔽区域和越界线绘制与编辑。
- UI 明确区分 MotionGate、输入裁剪、结果过滤和业务 Zone。
- 对非法多边形、重叠/冲突配置和输入裁剪的推理成本提供校验或提示。

## Acceptance Criteria

- [ ] 区域配置能从前端保存并经后端下发到 Engine。
- [ ] revision、失败回滚和错误提示行为完整。
- [ ] 多区域编辑、坐标归一化和方向配置可重复加载。
- [ ] 前端术语与 Engine 规则语义一致。

## Out of Scope

- MotionGate 算法和低成本帧差分。
- Engine Tracker 实现。
- 模型输入裁剪算法本身。
