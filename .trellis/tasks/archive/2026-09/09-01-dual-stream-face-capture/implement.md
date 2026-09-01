# 实施计划

## 1. 设计与契约

- [ ] 检查现有 `av_frame_desc`、`av_frame_ops`、decoder 输出和 CMake target，确定 RAII frame handle 接入方式。
- [ ] 新增并校验 `DualStreamConfig`、时间匹配结果、ROI mapper 和 capture request/result 的 Core 头文件。
- [ ] 为 RingBuffer 定义容量、保留时长、FIFO 淘汰和线程安全契约。

## 2. 核心实现

- [ ] 实现 `FrameRingBuffer`，覆盖 retain/release、按时间淘汰、容量上限和 shutdown 清理。
- [ ] 实现 `RoiMapper`，覆盖不同分辨率、Padding、边界和非法浮点输入。
- [ ] 实现双流编排组件，按现有 media/platform 接口创建两路 source/decoder，子流分发、主流入缓冲。
- [ ] 在不破坏 `09-01-on-demand-decoding` 工作区修改的前提下，评估并以最小改动接入 `CameraTask`；若耦合过大保持独立 Core 组件并记录后续接入点。

## 3. 测试

- [ ] 使用 Mock source/decoder 和 fake clock 测试双流生命周期、单流兼容、帧路由与停止顺序。
- [ ] 测试时间匹配：最近帧、容差边界、超窗、空缓冲、offset、异常 PTS。
- [ ] 测试 ROI：4K/720P、1080P/D1、Padding、边界、非法 NaN/越界/空矩形。
- [ ] 测试 RingBuffer FIFO、固定容量、并发 push/match/clear 和 frame token 生命周期。

## 4. 验证与门禁

- [ ] `make -C engine configure`
- [ ] `make -C engine build`
- [ ] `make -C engine test`
- [ ] `make -C engine asan`
- [ ] `make -C engine tsan`
- [ ] `make -C engine lint`

## 风险与回滚点

- 媒体接口当前是单 URL source，双流接入可能触及 CameraTask 生命周期；先保持组件边界，最后再做最小集成。
- 平台 surface 的引用必须通过现有 `frame_ops`，不能复制或释放 `opaque`；若接口无法安全持有，先限制 RingBuffer 到已有 FramePool token 能表达的帧。
- 不修改或回退 `09-01-on-demand-decoding` 的工作区文件；发生冲突时回到设计阶段拆分接入。
