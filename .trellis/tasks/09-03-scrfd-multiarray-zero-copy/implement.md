# Implementation Plan: SCRFD 9-Head 零拷贝与按需解析优化

## Checklist

- [x] 1. 结构重构：在 `model_inference.hpp` 中定义 `ScrfdHeadView` 并重构 `ScrfdOutput` 为零拷贝视图与生命周期 Token。
- [x] 2. 映射实现：在 `model_inference.mm` 中实现 `map_head_view`，仅提取一次指针基地址与步长，彻底废弃 9 个头的 `vector` 拷贝。
- [x] 3. 按需解码：在 `postprocessor.cpp` 的 `decode_scrfd_faces` 中采用条件延迟读取，仅对 `score >= conf_thresh` 读取 bbox 和 kps。
- [x] 4. 单元与回归测试：运行 `make -C algo-packages/macos/arm64/face_recognition test`，确保数值完全一致、测试 100% 通过。
- [x] 5. 性能 Benchmark 实测：使用 `standalone_runner` 测量 1080P、Fixture、4K 与 16 人脸场景，对比优化前后的 `scrfd_copy` 耗时与 FPS。
- [x] 6. 内存安全检测：运行 `make -C algo-packages/macos/arm64/face_recognition asan` 和 `check-boundary.sh`，确保零泄漏、零越界、无符号污染。
- [x] 7. 交付总结：编写 `analysis.md` 记录优化前后的基准数据与收益对比，完成任务交付。
