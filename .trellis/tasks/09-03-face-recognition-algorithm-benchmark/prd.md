# face_recognition 算法包性能基线与 profiling

## Goal

以当前工作区版本为准，为 macOS arm64 `face_recognition` 建立可复现的算法包性能、内存和 Core ML 运行时基线，不修改生产推理语义。

## Requirements

- 补充分阶段 profiling：NV12 conversion、Letterbox、SCRFD、输出拷贝、decode/NMS、tracker/quality、alignment、GLINTR、embedding serialization、total。
- 记录每帧 detected faces、tracks、embedding calls、image requests。
- 覆盖当前 fixture、1080P/4K、synthetic NV12 多 stride、VideoToolbox CVPixelBuffer；覆盖 0/1/4/16 人脸和 detection-only/best-shot/all。
- 统计 p50/p95/p99/max、RSS before/peak/after，并记录 Apple M4/macOS 26.5.1/Release 环境。
- 使用 `os_signpost`、CLI 采样和 Instruments 观察 CPU/GPU/ANE、模型加载及数据搬运；大型 trace 保存元数据而非直接入库。
- 仅允许新增或修改 benchmark/profiling/test 资产，不修改生产算法逻辑。

## Acceptance Criteria

- [ ] 开发基线为 30 warmup + 300 measured，稳定性基线为 60 warmup + 1000 measured。
- [ ] benchmark 输出机器、输入、模式、计时分段、识别次数和内存指标。
- [ ] 至少完成当前 fixture 与 1080P/4K 场景；未能执行的场景标记 BLOCKED 并说明原因。
- [ ] 输出原始数据和分析摘要，结论标注 L1/L2/L3/L4 证据等级。
- [ ] 算法包原有 build/test/asan/benchmark/package 结果不被破坏。

## Out of Scope

- 不做 precision/recall、ROC、TAR/FAR 等精度评测。
- 不直接优化 4K full RGB、模型加载、Core ML 调度或并发模型；这些只形成后续整改建议。
- 不进行 RKNN/MPP/RGA/DMA-BUF 迁移。
