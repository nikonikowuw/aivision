# face_recognition 算法多层面分析与验证

## Goal

对当前项目中的 `face_recognition` 算法建立可复现、可验证的多层面分析结论，重点评估实时性能、内存占用、Core ML 利用率、数据流与生命周期，并识别从算法包到 Engine/SDK/业务层的真实瓶颈和风险。分析完成后形成按优先级排序的整改方案；在证据不足前不直接进行投机性优化。

## Background

当前 macOS arm64 算法包位于 `algo-packages/macos/arm64/face_recognition`，实时路径为：

```text
NV12/CVPixelBuffer -> 原图 RGB -> 640x384 Letterbox -> SCRFD
-> tracker/quality -> 112x112 五点对齐 -> GLINTR100 512 维 embedding
-> AV_RESULT_RECOGNITION
```

注册路径通过 `av_algo_extract_face` 使用 640x640 SCRFD 和 112x112 embedding。

当前开发机基线已实测：Apple M4、24 GB RAM、macOS 26.5.1、Release；现有 runner benchmark 使用 `testimage.jpg`（466x659），结果为 Avg 7.4367 ms、P50 7.13483 ms、P99 12.5955 ms、134.468 FPS。该结果尚未拆分阶段，也未明确记录 embedding 调用次数，不能作为 1080P/4K 或多路性能承诺。

当前代码事实包括：

- `instance_process()` 默认走 RGB 缓冲推理重载，`CVPixelBuffer` 推理重载虽已存在但尚未成为实时主路径；
- 4K 处理会产生完整 RGB、完整 ARGB、Letterbox 和缩放临时缓冲；
- `ModelInferenceManager` 及其 cached pixel buffer 位于 `LibraryContext`，可能被多个实例共享；
- 默认关闭人体 YOLO，但 `library_open()` 仍尝试加载 YOLO；
- runner benchmark 仅统计 `instance_process()` 总耗时；
- 算法包测试已覆盖 ABI、预处理和后处理基础行为，但尚未形成完整的分辨率、stride、多人脸、长时间和 Core ML 设备利用率基线。

## Requirements

### R1. 算法包性能基线

建立分阶段 latency、吞吐和识别调用次数基线，至少覆盖预处理、SCRFD、输出拷贝、后处理、tracker、alignment、GLINTR、序列化和总耗时。

### R2. 分辨率与内存分析

验证 466x659、1080P、4K 输入下的 CPU 时间、临时 buffer、RSS 峰值和长期内存稳定性，明确完整 RGB/ARGB 转换的成本。

### R3. 输入与 Surface 路径分析

分别验证 Host NV12、带 stride 的 NV12、CVPixelBuffer NV12 和 VideoToolbox 真实解码 Surface，确认拷贝、锁、释放和输入格式路径。

### R4. 人脸数量与调度分析

覆盖 0/1/4/16 人脸，以及 `detection-only`、`best_shot`、`all` 模式，记录 tracker、quality、GLINTR 次数和多人场景尾延迟。

### R5. Core ML 利用率分析

通过结构化 profiling 和 macOS Instruments/signpost 验证 SCRFD、GLINTR、YOLO 实际 CPU/GPU/ANE 利用情况、输入输出拷贝和模型加载行为；不得将 `MLComputeUnitsAll` 直接等同于 ANE 执行。

### R6. 跨层数据流与生命周期分析

审查 SDK C ABI、算法包、Engine 主流接入、图片请求、结果回调、底库比对和记录落库之间的数据流、帧关联、并发边界和资源释放。

### R7. 问题整改优先级

基于可复现实验证据，将问题分为正确性、并发/生命周期、性能、可观测性和后续 RKNN 迁移边界，并给出最小整改方案、验证方法和回滚点。

## Benchmark Plan

- 开发基线：30 warmup + 300 measured；
- 稳定性测试：60 warmup + 1000 measured；
- 输入：当前固定图片、生成的 1080P/4K NV12、synthetic NV12 多 stride、VideoToolbox 真实视频；
- 指标：p50/p95/p99/max、RSS before/peak/after、检测数、track 数、embedding 数、图片请求数；
- profiling：算法内部结构化 profiling + `os_signpost` + Instruments；默认生产构建不启用详细 profiling，不改变公共 C ABI。

## Acceptance Criteria

- [ ] 输出覆盖算法包、SDK/ABI、Engine、后端记录链路和部署迁移边界的分层分析报告。
- [ ] 建立可重复 benchmark 矩阵和机器环境记录，至少完成当前 fixture、1080P/4K 和 0/1/4/16 人脸场景设计或实测。
- [ ] 阶段 profiling 能区分 SCRFD 与 GLINTR，且记录 embedding 调用次数及 p50/p95/p99。
- [ ] 给出 4K 内存峰值、输入拷贝、cached buffer 并发安全和 YOLO 按需加载的证据与优先级。
- [ ] 对所有关键结论提供代码、测试、配置、日志或 profiling 证据锚点；不把未实测的 ANE、路数和 FPS 写成承诺。
- [ ] 形成后续实现任务的拆分建议；本分析任务不默认包含未经确认的生产代码重构。

## Out of Scope

- 不在本任务内直接完成 RKNN/MPP/RGA/DMA-BUF 迁移或 RK3568 板端性能承诺。
- 不在没有 profiling 证据时进行大规模算法模型替换或参数调优。
- 不改变公共 ABI、业务数据模型或前端交互，除非后续明确创建实现子任务。

## Open Questions

- 已确认允许补充 benchmark/profiling/test 代码，但不直接修改生产算法、Engine、SDK ABI 或 Go 业务模型；生产修复在证据确认后另建实现子任务。
