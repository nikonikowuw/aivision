# 实施计划

## 1. macOS 算法包基线

- [x] 检查并修正 `face_recognition` 的真实 CVPixelBuffer 输入、SCRFD 预处理/反 Letterbox、landmark 顺序和原图同帧对齐。
- [x] 默认关闭 YOLO 人体检测，保留显式可选开关；确认算法包不重复执行 Engine 的分析 FPS 抽样。
- [x] 固化结果 JSON/C ABI：保留 `frame_id`、`pts_ns`、bbox、landmarks 和 embedding 关联。
- [x] 完善最佳帧、质量门槛、track cooldown 和最大识别次数测试。
- [x] 执行算法包 `build`、`test`、`asan`、`run`、`benchmark`、`package`，形成 macOS 基线报告。

## 2. Engine 主流接入

- [x] 检查 `CameraTask`、`AlgorithmInstance`、`FramePool` 和 `IImageProcessor`，确定同帧引用的最小改动。
- [x] 接入主流解码输出和 `target_fps` 抽帧，禁止算法包二次抽帧。
- [x] 实现同帧缩放坐标还原、原始帧 ROI 校验和有界识别调度。
- [x] 验证 frame token、stop/remove/shutdown 和多实例资源释放。

## 3. 质量门禁

- [x] 算法包：`make -C algo-packages/macos/arm64/face_recognition build test asan benchmark package`。
- [x] Engine：`make -C engine test asan lint`。
- [x] macOS 验收通过后记录后续 RKNN 迁移边界，但不在本任务中转换模型或适配 RKNN。

## 风险与回滚点

- `face_recognition` 当前是 macOS Core ML 初版；本任务先完成 macOS 正确性和 Engine 契约，不承诺 Linux/RKNN 可部署。
- 当前工作区包含 `09-01-on-demand-decoding` 的未提交 `CameraTask` 修改，Engine 集成只能做必要改动，不回退已有变更。
- 若识别模型异步生命周期无法持有源帧，则同步裁剪到有界 112x112 buffer 后再提交识别。
- RKNN/MPP/RGA/DMA-BUF 迁移、模型转换证据和 RK3568 资源实测另建任务，作为本任务的后续依赖，而非本任务的回滚范围。
