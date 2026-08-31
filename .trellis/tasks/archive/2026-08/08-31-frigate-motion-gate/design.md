# MotionGate 技术设计

## 架构边界

`MotionGate` 位于 engine core 的解码帧分发与算法实例 worker 之间，属于通用调度机制，不依赖具体推理运行时、ZLMediaKit 或算法包实现。每个摄像头/算法实例拥有独立状态；若现有架构以实例为采样单元，则 gate 随实例创建并由该实例 worker 独占访问。

## 数据流

```text
encoded queue -> decoder -> FrameHandle
                         -> MotionGate::evaluate(frame, monotonic_now)
                              | active / keepalive
                              v
                    existing latest-frame queue -> algorithm worker
                              |
                         skip: release frame
```

运动输入优先使用 NV12 的 Y 平面或平台可读的等价亮度视图。将亮度帧按配置高度缩小到固定尺寸，应用 mask 后与运行背景做差分，提取变化轮廓并以阈值/面积判定。首帧只建立背景；后续帧更新背景时遵循 `frame_alpha` 等价策略，避免静态物体永久触发。

## 配置与状态

配置至少包含：`enabled`、`threshold`、`contour_area`、`frame_height`、`keepalive_interval`、`mask`。配置在实例启动或 revision 更新时完成边界校验和状态重置。时间使用 monotonic clock，测试注入 fake clock。

状态至少包含：是否已初始化背景、上一/当前低分辨率亮度帧、上次放行推理时间、配置尺寸/格式签名和统计计数。`evaluate` 在实例 worker 内串行调用，不增加跨线程共享状态。

## 语义

- disabled 时保持现有“每个采样帧均可推理”行为。
- enabled 且首帧/重置帧只建立背景，不触发伪运动。
- active motion 立即放行。
- no motion 且 `now - last_inference >= keepalive_interval` 时放行 keepalive。
- 其他 no-motion 帧跳过并释放引用。
- mask 只影响运动统计，不影响算法输入；空 mask 表示不排除区域，全屏 mask 表示不产生 mask 外运动。

## 兼容性与错误处理

优先使用已有内部 C++ 配置/调度契约，不修改 SDK C ABI。无法读取或不支持的帧格式应返回可观测的 gate error 或按现有策略退化为放行推理，不能静默丢帧；具体策略需与既有错误规范一致。无效 mask、越界点和极端参数在配置校验阶段拒绝。

## 资源与回滚

运动检测缓冲区按配置尺寸分配并限制最大尺寸；不持有算法帧的额外长期引用。关闭 gate 或配置无效时保留旧配置/旧调度状态，不能破坏已有推理链路。新增指标沿用现有 telemetry 机制。

## 验证重点

覆盖纯静止帧、超过阈值的变化、mask 内外变化、首帧/格式变化、keepalive 时间边界、配置更新、队列丢帧和 shutdown。使用固定 NV12 fixture 与 fake clock；再运行 engine 的构建、单测、lint 及适用 sanitizer。
