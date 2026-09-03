# SCRFD 9-Head 零拷贝与按需解析优化

## Goal

彻底消除 `face_recognition` 算法包中 SCRFD 人脸检测 9 个输出头（Score/Bbox/Landmarks 跨 stride 8/16/32）每帧拷贝及反序列化带来的 4.7 ~ 5.3 ms 开销。将 SCRFD 输出解析阶段的平均耗时降至 0.5 ms 以内，使 1080P 人脸分析总延迟由 8.8 ms 降至 4.5 ms 左右，吞吐量由 113 FPS 提升至 200+ FPS，同时保持检测精度与下游后处理语义 100% 对齐。

## Background & Root Cause

在 Trellis 前序基线任务 `09-03-face-recognition-algorithm-benchmark` 与 `09-03-face-recognition-cross-layer` 中测得：
1. Apple M4 上 SCRFD 10G 前向推理仅需 **2.0 ~ 2.2 ms**，但 9 个 MultiArray 输出头拷贝至 CPU 内存耗时高达 **4.7 ~ 5.3 ms**，占基线总耗时 **62.3%**。
2. 真实系统堆栈采样（`/usr/bin/sample`）捕获到大量 samples 集中在：
   ```text
   instance_process -> run_scrfd_internal -> copy_multiarray_to_float_vector
     -> -[MLMultiArray dataPointer]
     -> CoreML::MultiArrayBuffer::loadBuffer() const
     -> std::__sp_mut::lock() -> pthread_mutex_lock
   ```
3. 代码审查发现关键问题：
   - **问题 1（内存分配与全量搬运）**：每帧为 9 个头分配并全量拷贝 10,080 个 anchor 的 Score、Bbox、KPS 浮点数据（总计 604 KB 浮点数据，9 次 `vector` 堆分配）。
   - **问题 2（潜在的 Strides 错判与逐元素属性访问）**：`copy_multiarray_to_float_vector` 在判断非连续内存时，在元素循环内部每次迭代都调用了 `arr.dataPointer`（Objective-C 动态属性消息），引发海量互斥锁与跨硬件同步。
   - **问题 3（全量解码 vs 极稀疏人脸）**：一帧画面通常仅有 0 ~ 5 个人脸目标，但代码无条件拷贝并解码了 10,080 个锚点的所有 Bbox (40,320 floats) 和 KPS (100,800 floats)。实际上只有 `score >= conf_thresh` 的锚点才需要读取对应 Bbox 和 KPS。

## Requirements

### R1. 纯零拷贝指针借用设计 (Zero-Copy Pointer Borrowing / View)
- 决策确认：采纳方案 1（纯零拷贝指针借用）。`ScrfdOutput` 重构为只读轻量 View 结构，直接借用 Core ML `MLMultiArray` 的内部内存指针（`const float*`）及对应 stride，彻底消除 9 个 `std::vector<float>` 堆内存分配与全量数据拷贝。
- 生命周期约束：`ScrfdOutput` 借用的指针生命周期绑定在当前帧推理与后处理解码的同一同步调用栈内。

### R2. 按需解码 (Lazy/Conditional Bbox & KPS Decoding)
- 仅在 `score >= conf_thresh` 时才访问对应 anchor 索引的 `bbox` 与 `kps` 内存，避免对 99.9% 的背景锚点进行坐标换算与内存访问。

### R3. 消除循环内 `dataPointer` 访问与优化连续性判断
- `arr.dataPointer` 针对每个 `MLMultiArray` 至多访问一次并缓存其指针，严禁在任何循环内访问属性。
- 强化 Core ML 输出张量内存布局识别，支持直接连续指针访问或显式步长跨步计算。

### R4. 保持纯 C ABI 与精度绝对一致
- 保持外部 C ABI 导出与 `av_algo_result_t` JSON 输出格式完全一致。
- 保持 `Postprocessor::decode_scrfd_faces` 的输出坐标与置信度完全对齐（数值误差不超过浮点机精度）。

### R5. 性能与长稳验证
- 重新运行 1080P、Fixture 与 16 人脸 benchmark，验证 `scrfd_copy` 耗时是否降低到 <0.5 ms，1080P FPS 是否突破 200。
- 运行 1000 帧长稳测试与 ASan 内存检测，确保零泄漏、零越界。

## Acceptance Criteria

- [x] `scrfd_copy` 平均耗时由 ~5.0 ms 降至 <= 0.5 ms。（实测降至 0.003 ms）
- [x] 1080P `best_shot` 场景平均延迟降至 <= 5.0 ms，吞吐量提升至 >= 200 FPS。（实测延迟 3.68 ms，吞吐 271.7 FPS）
- [x] `make -C algo-packages/macos/arm64/face_recognition test` 单元测试 100% 通过。
- [x] `make -C algo-packages/macos/arm64/face_recognition asan` 零错误、零内存泄漏。
- [x] `bash engine/scripts/check-boundary.sh` 边界检查 100% 通过。
- [x] 生成优化前后对比的 `analysis.md` 与基准数据。

## Out of Scope

- 不修改 GLINTR 模型或引入动态 Batching（属于方向 B 独立任务）。
- 不引入 GPU/MPS 4K 预降采样（属于 P1 独立任务）。
- 不改变 Engine 核心或 Go 后端代码。
