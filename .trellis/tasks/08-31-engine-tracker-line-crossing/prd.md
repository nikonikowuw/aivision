# Engine 目标跟踪与越界线方向判断

## Goal

在 Engine 建立统一的目标轨迹和越界线规则能力，为区域过滤和业务事件提供稳定基础。

## Dependencies

- 依赖当前算法结果中的检测框和 `track_id` 现状分析。
- 为 `08-31-engine-region-rules` 和 `08-31-algo-package-rule-migration` 提供轨迹契约。

## Requirements

- 定义目标轨迹创建、关联、短暂丢失、过期和 reset 语义。
- 使用目标锚点和归一化坐标判断轨迹是否穿过线段/折线。
- 支持 BOTH、A_TO_B、B_TO_A 方向以及 epsilon、冷却和最大轨迹间隔。
- 越界线生成独立事件，不因当前帧未越线而删除目标。
- 使用 fake clock 测试跨线、抖动、丢帧、重连和重复触发。

## Acceptance Criteria

- [ ] 同一目标跨越单线和折线时产生一次方向正确的事件。
- [ ] 线附近抖动、贴线和轨迹间隔过大时不会误报。
- [ ] 多实例 track_id 不冲突，reset 后旧轨迹不会复用。
- [ ] 轨迹状态与检测结果过滤解耦。

## Out of Scope

- MotionGate。
- 具体模型推理和目标检测算法。
- 前端绘制和业务告警编排。
