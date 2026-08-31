# 引入 Frigate 风格运动检测推理门控

## Goal

在 Argus 引擎中引入每路摄像头的低成本运动检测门控：视频继续解码，但当画面没有足够变化时跳过图像预处理和算法插件推理，以降低 NPU/GPU/CPU 推理资源消耗；静止期间按可配置间隔执行一次保活推理，避免无限期漏检。

## Background

当前视频链路为编码包 -> 解码帧 -> 算法实例 worker。运动判断应位于解码之后、算法图像转换和插件调用之前，并复用解码帧的 NV12/Y 平面。Frigate 的 motion mask 只用于运动判断阶段排除干扰区域；zone 和对象 mask 属于检测结果后的业务过滤，不纳入本 task。

## Requirements

- R1. 为每路摄像头提供独立的 `MotionGate` 状态，不同算法实例不得互相共享背景模型或保活计时。
- R2. 使用解码帧的亮度数据或等价低成本输入生成固定低分辨率 motion frame，避免为运动判断执行模型输入级图像转换。
- R3. 支持可配置运动阈值 `threshold`、最小变化轮廓/区域 `contour_area`、motion frame 高度或等价采样尺寸，以及静止期间的保活推理间隔 `keepalive_interval`。
- R4. 支持可选的 motion mask 多边形；mask 内的像素变化不得触发运动门控。mask 不得被解释为目标检测 ROI、zone 或目标类型过滤器。
- R5. 运动状态为 active 时立即放行当前帧到算法推理；无运动且未到保活时间时释放/跳过当前帧，不执行后续预处理和插件调用。
- R6. 无运动达到 `keepalive_interval` 时放行一次保活推理，并重置保活计时；默认值必须避免无限期不推理。
- R7. 首帧、尺寸/像素格式变化、解码重置或配置更新时正确初始化/重建背景状态，不因未初始化背景误报运动。
- R8. 保持现有有界队列、丢旧留新、单实例 worker 串行和帧 token 生命周期契约；运动判断不得阻塞媒体回调或解码线程。
- R9. 记录可观测计数：收到帧、运动放行、保活放行、无运动跳过、mask 后变化结果；不记录帧内容。

## Acceptance Criteria

- [ ] AC1. 静止固定帧序列在未达到保活间隔时不调用算法实例 process，达到间隔后恰好调用一次。
- [ ] AC2. 变化超过 threshold 且变化区域超过 contour_area 时，当前帧被放行推理。
- [ ] AC3. 仅 mask 区域发生变化时，帧不因该变化被放行；mask 外变化仍可放行。
- [ ] AC4. 首帧建立背景而不产生伪运动；尺寸/格式变化后重新初始化且无越界访问。
- [ ] AC5. 配置边界值、空 mask、全屏 mask、多个 mask 和无效多边形均有确定行为并有单元测试。
- [ ] AC6. 运动门控测试使用固定 fixture/fake clock，不依赖真实 sleep，并覆盖 reset/shutdown 生命周期。
- [ ] AC7. `make -C engine configure`、`build`、`test`、`lint` 通过；适用时运行 ASan/TSan 并报告结果。

## Out of Scope

- 后端运动事件持久化、API 和前端配置页面。
- Frigate zone、objects mask、required_zones、目标跟踪和告警语义。
- 录像保留、预录/后录、MQTT 运动事件和通知。
- 以神经网络或光流模型实现运动检测。

## Technical Notes

- motion mask 是运动检测输入上的排除多边形，不裁剪算法模型输入。
- 保活推理间隔是降低漏检风险的必要机制，必须使用 monotonic clock/fake clock。
- 公共 C ABI 若不需要暴露配置则优先保持不变；跨进程配置字段若必须新增，应同步版本、校验和测试。
