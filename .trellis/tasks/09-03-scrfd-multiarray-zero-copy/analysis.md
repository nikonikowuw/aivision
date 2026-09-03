# SCRFD 9-Head 零拷贝与按需解析优化验证报告

> 任务编号：`09-03-scrfd-multiarray-zero-copy`  
> 评测环境：Apple M4 (10 核心：4P+6E), 24 GB Unified Memory, macOS 26.5.1  
> 编译器：AppleClang 21.0.0 (C++20), Release (`-O3 -DNDEBUG`)  
> 实施方案：方案 1（纯零拷贝指针借用与按需延迟解码）  
> 验证日期：2026-09-03  

---

## 1. 优化前后全场景性能对比 (L1/L2 实测)

| 测试场景 | 指标类型 | 优化前基线 (09-03 Baseline) | 零拷贝优化后 (09-03 Optimized) | 收益与加速比 |
| :--- | :--- | :--- | :--- | :--- |
| **Fixture (466x659)**<br>`best_shot` | **SCRFD Copy (9H)**<br>**总平均延迟**<br>P50 延迟<br>P99 延迟<br>**吞吐量 (FPS)** | 4.702 ms<br>**7.61 ms**<br>7.36 ms<br>10.78 ms<br>**131.3 FPS** | **0.0029 ms (2.9 µs)**<br>**2.47 ms**<br>2.34 ms<br>5.60 ms<br>**405.2 FPS** | **拷贝消除 99.9% (1,600x)**<br>**延迟降低 67.5%**<br>**FPS 飙升 3.1x** |
| **标准 1080P**<br>`best_shot` | **SCRFD Copy (9H)**<br>**总平均延迟**<br>P50 延迟<br>P99 延迟<br>**吞吐量 (FPS)** | 4.702 ms<br>**8.84 ms**<br>8.44 ms<br>13.19 ms<br>**113.2 FPS** | **0.0031 ms (3.1 µs)**<br>**3.68 ms**<br>3.48 ms<br>8.90 ms<br>**271.7 FPS** | **拷贝消除 99.9% (1,500x)**<br>**延迟降低 58.4%**<br>**FPS 飙升 2.4x (271 FPS)** |
| **超清 4K**<br>`best_shot` | **SCRFD Copy (9H)**<br>**总平均延迟**<br>P50 延迟<br>P99 延迟<br>**吞吐量 (FPS)** | 4.884 ms<br>**14.96 ms**<br>14.22 ms<br>22.84 ms<br>**66.8 FPS** | **0.0034 ms (3.4 µs)**<br>**9.32 ms**<br>9.01 ms<br>13.69 ms<br>**107.3 FPS** | **拷贝消除 99.9%**<br>**延迟降低 37.7%**<br>**突破 100 FPS 大关** |
| **1080P 16 人脸**<br>`best_shot` | **SCRFD Copy (9H)**<br>**总平均延迟**<br>P50 延迟<br>P99 延迟<br>**吞吐量 (FPS)** | 4.833 ms<br>**9.42 ms**<br>8.45 ms<br>70.08 ms<br>**106.2 FPS** | **0.0125 ms (12.5 µs)**<br>**5.17 ms**<br>3.73 ms<br>65.43 ms<br>**193.4 FPS** | **拷贝消除 99.7%**<br>**延迟降低 45.1%**<br>**FPS 提升至 193 FPS** |

---

## 2. 优化机理与技术突破

1. **彻底消除 9 次堆内存分配与数据搬运**：
   - 弃用 9 个 `std::vector<float>`，重构为轻量只读视图 `ScrfdHeadView` 与 `ScrfdOutput`。
   - 每帧不再进行 151,200 个浮点数（604.8 KB）的堆分配与全量 `memcpy`。
2. **消灭循环内 `dataPointer` 属性获取与互斥锁争用**：
   - 现仅在模型推理完成后对 9 个头各执行一次 `dataPointer` 提取基地址；
   - 彻底消除了旧代码在非连续 strides 布局下逐元素触发的 `[arr dataPointer]` Objective-C 消息发送、`MultiArrayBuffer::loadBuffer()` 和 `pthread_mutex_lock` 调用栈开销。
3. **按需稀疏解码 (Lazy Coordinate Decoding)**：
   - 在 `Postprocessor::decode_scrfd_faces` 中，仅先读取 `score_head.get(a_idx, 0)`；
   - 占 99.9% 以上的背景锚点（`score < conf_thresh`）直接 `continue`，完全跳过对应 Bbox 与 KPS 的计算与内存访问；
   - 仅对真实人脸候选位置延迟读取 Bbox (4 floats) 与 Landmarks (10 floats)。
4. **生命周期安全闭环**：
   - `ScrfdOutput` 内嵌 `std::shared_ptr<void> buffer_holder`，持有 `output_provider`（Objective-C 引用保持），确保底层 Core ML 张量内存跨越栈帧直到解码完成析构，零悬挂、零泄漏。

---

## 3. 质量门禁与测试回归

| 验证项 | 测试命令 / 检查方式 | 结果 | 关键观测 |
| :--- | :--- | :--- | :--- |
| **算法包单元与 ABI 测试** | `make -C algo-packages/macos/arm64/face_recognition test` | **100% PASS** (3/3) | `face_recognition_tests`, `preprocess_tests`, `abi_tests` 全绿 |
| **数值精度对齐** | 单图推理人脸检测结果与 Landmarks 坐标校验 | **100% PASS** | 检测框坐标 `[0.439005, 0.170817, ...]` 及 5 关键点与优化前完全一致 |
| **AddressSanitizer 内存安全** | `make -C algo-packages/macos/arm64/face_recognition asan` | **100% PASS** | 零内存泄漏、零堆越界、零野指针 |
| **纯 C ABI 隔离检查** | `bash engine/scripts/check-boundary.sh` | **100% PASS** | 零 C++ 符号导出污染，保持 `av_algo_get_abi` 绝对纯洁 |
| **Engine 全链路回归测试** | `make -C engine test` | **100% PASS** (101/101) | 包含 `FaceRecognitionObservationUsesRealUdsCallbackAndRetry` 等用例 |
| **Go 后端全量测试** | `cd argus && go test ./...` | **100% PASS** | 数据流与持久化回归正常 |

---

## 4. 结论与交付确认

本次优化成功解决了 `face_recognition` 算法包中最大的性能瓶颈（P0）：
- **目标指标全部超额达成**：
  - `scrfd_copy` 从 4.70 ms 降至 **0.003 ms**（目标 <= 0.5 ms，大幅超越目标）；
  - 1080P 延迟从 8.84 ms 降至 **3.68 ms**（目标 <= 5.0 ms）；
  - 1080P 吞吐从 113 FPS 提升至 **271.7 FPS**（目标 >= 200 FPS）。
- 验收准则全部核准，可顺利归档。
