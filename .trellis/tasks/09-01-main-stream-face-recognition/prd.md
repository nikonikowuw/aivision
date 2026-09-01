# 主码流同帧人脸检测与识别

## Goal

先在 macOS 上验证已有 `algo-packages/macos/arm64/face_recognition` 初版的算法正确性、同帧坐标契约和 C ABI 生命周期，再接入 Engine 主码流。最终运行时以主码流作为人脸检测、高清裁剪、质量评估与识别的统一时序来源，面向 RK3568 等边缘设备通过低频检测、同帧缩放/裁剪、轨迹级识别限流和有界硬件 Surface 控制资源占用。

## Requirements

- R1. macOS 算法包必须完成真实 `CVPixelBuffer -> av_frame_desc -> SCRFD -> landmarks -> 原图同帧对齐 -> GLINTR embedding -> C ABI result` 闭环验证。
- R2. 主码流是人脸检测、关键点、高清裁剪图和证据帧的唯一权威来源；检测输入可以是同一主流帧的缩放派生图，但不得使用 Snapshot、子流坐标或其他时间点的帧。
- R3. Engine 负责主流解码、`target_fps` 抽帧、实例队列和资源准入；算法包处理收到的帧，不重复实现 Engine 的分析 FPS 抽样。
- R4. 人脸算法默认使用 SCRFD + tracker + quality + alignment + GLINTR 主路径；人体 YOLO 为可选能力，不得默认与 SCRFD 每帧并行执行。
- R5. 支持最佳帧选择、质量门槛、每个 `track_id` 的最大识别次数和 cooldown；重度 embedding 不得对连续每帧默认执行。
- R6. 结果必须保留输入 `frame_id`、`pts_ns`、人脸 bbox、5 点 landmarks 和 embedding 的关联；Engine 与算法结果必须能验证属于同一主流帧。
- R7. 主流高清裁剪使用原始主流帧及合法 frame reference，处理完成后及时释放完整高清 Surface，不维护多帧 4K 解码 RingBuffer。
- R8. 算法包与 Engine 的公共结果、后处理和生命周期契约应保持平台无关，为后续 RKNN 迁移提供清晰边界；本任务不实现 RKNN、MPP、RGA 或 DMA-BUF 适配。

## Constraints

- 当前 macOS 包的 `.mlpackage` 仅用于 macOS 验证，不能直接部署到 RK3568。
- 本任务验收环境为 macOS；不以缺少 RKNN Toolkit、RK3568 板卡或 Linux BSP 为阻塞条件。
- 本任务不实现子码流检测、子流唤醒主流、异步 Snapshot、跨流 PTS 对齐、主流 GOP 回溯、直接跨流 ROI 映射、RKNN 模型转换、MPP 解码、RGA/DMA-BUF 适配或 RK3568 板端性能验证。

## Acceptance Criteria

- [ ] AC1. macOS standalone runner 使用真实 `CVPixelBuffer` 或等价真实平台帧，完成 SCRFD 检测、landmark、同帧原图对齐和 GLINTR embedding。
- [ ] AC2. macOS 结果携带正确的 `frame_id/pts_ns`，检测坐标映射、5 点顺序、112x112 对齐和可视化结果通过固定 fixture 验证。
- [ ] AC3. 默认路径不执行 YOLO；分析 FPS 由 Engine 控制，best-shot、质量门槛、track cooldown 和识别次数上限测试通过。
- [ ] AC4. Engine 主流接入后，检测图和高清裁剪图来自同一主流帧，stop/remove/shutdown 不发生 frame token 泄漏或 use-after-free。
- [ ] AC5. 1080P/4K 主流 ROI 裁剪正确处理边界、stride、像素格式和 surface 生命周期，完整高清帧及时释放。
- [ ] AC6. macOS 包的 build/test/asan/benchmark/package 和 Engine 的 test/asan/tsan/lint 通过。

## Out of Scope

- RKNN 模型转换、RKNN Runtime、MPP、RGA、DMA-BUF 和 RK3568 板端部署验证；这些内容另建独立后续任务。
- 子码流检测、子流唤醒主流和主子流坐标映射。
- ONVIF/ISAPI Snapshot 和异步 JPEG 证据图。
- 告警落库、管理端页面、云端向量检索和多摄像头分布式调度。
