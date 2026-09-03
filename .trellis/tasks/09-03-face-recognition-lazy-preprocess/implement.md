# Implementation Plan: 彻底废除全图 RGB 零冗余预处理架构

## Checklist

- [x] 1. 结构重构：在 `preprocessor.hpp` 中重构 `PreprocessResult`，彻底删除 `original_rgb`，增加原图宽高及 NV12 指针与跨距视图。
- [x] 2. 直接分级 Letterbox 降采样：在 `preprocessor.mm` 中实现基于常驻 scratch buffer 的 Y 平面与 UV 平面直接降采样及小尺寸色彩转换。
- [x] 3. NV12 双平面直接 112x112 仿射对齐截脸：在 `preprocessor.mm` 中实现 NV12 上的双线性直接插值与 BT.709 点级色彩转换，并在 `algo_entry.cpp` 中适配调用。
- [x] 4. 单元测试与回归校验：更新 `preprocess_tests.mm` 验证 NV12 直接对齐截脸，运行 `make -C algo-packages/macos/arm64/face_recognition test` 确保 100% 通过。
- [x] 5. 性能 Benchmark 实测：使用 `standalone_runner` 测量 1080P、4K、Fixture 及 16 人脸场景，对比优化前后的预处理耗时、总延迟及吞吐量。
- [x] 6. 内存安全检测：运行 `make -C algo-packages/macos/arm64/face_recognition asan` 和 `check-boundary.sh`，确保零泄漏、零越界、无符号污染。
- [x] 7. 交付总结：编写 `analysis.md` 记录优化前后的基准数据与收益对比，完成任务交付。
