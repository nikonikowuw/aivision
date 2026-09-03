# 人脸识别算法包性能、内存与 Core ML 运行时基线分析报告

> 任务编号：`09-03-face-recognition-algorithm-benchmark`  
> 算法包：`algo-packages/macos/arm64/face_recognition`  
> 目标硬件：Apple M4 (10 核心：4 性能核 + 6 能效核), 24 GB Unified Memory  
> 操作系统：macOS 26.5.1 (Darwin Kernel Version 25.5.0: arm64)  
> 编译器：AppleClang 21.0.0 (clang-2100.0.101), C++20, Release (`-O3 -DNDEBUG`)  
> Git Commit HEAD：`1ca263ab`

---

## 1. 执行摘要与关键发现

本次 Benchmark 针对 macOS arm64 架构下的人脸识别算法包（SCRFD 10G 检测 + ByteTracker 跟踪 + 简易质量评估 + GLINTR 100 特征提取）建立了全维度的性能、内存与 Core ML 运行时基线。全过程未触碰生产推理语义、SDK ABI 规范、Engine 核心或 Go 后端代码，保持了纯 C ABI 的强符号隔离与边界纯洁性。

### 核心指标速览
- **基线输入 (466x659 Fixture, best_shot)**：平均延迟 **7.61 ms**，吞吐量 **131.3 FPS**，峰值 RSS **325.5 MB**（稳定运行态 RSS **169.0 MB**）。
- **标准 1080P 输入 (1920x1080 NV12, best_shot)**：平均延迟 **8.84 ms**，吞吐量 **113.2 FPS**，稳定态 RSS **196.8 MB**。
- **超清 4K 输入 (3840x2160 NV12, best_shot)**：平均延迟 **14.96 ms**，吞吐量 **66.8 FPS**，稳定态 RSS **296.5 MB**。
- **1000 帧稳定性测试 (1080P, 60 warmup + 1000 measured)**：平均延迟 **9.81 ms**，首帧预热后 RSS 为 **202.38 MB**，运行 1000 帧后 RSS 为 **202.47 MB**，内存漂移量仅 **+0.09 MB**，**零内存泄漏**。
- **AddressSanitizer (ASan) 验证**：在 Debug+ASan 模式下通过全部 ABI 与算法单元测试，无堆越界、释放后使用或内存泄漏警告。

### 发现的三大核心性能瓶颈
1. **[严重] SCRFD 9-Head MultiArray 拷回开销占比过半 (L1/L3)**：SCRFD 前向推理本身耗时仅 **2.04 ms**，但 9 个输出头通过 `MLMultiArray` 拷贝至 CPU `std::vector<float>` 耗时高达 **4.74 ms**（占基线整帧耗时的 **62.3%**）。通过 `/usr/bin/sample` 堆栈采样证实，瓶颈在于 Core ML `MultiArrayBuffer::loadBuffer()` 的互斥锁与内存同步开销。
2. **[重要] 4K 高分辨率下 CPU 预处理开销陡增 (L1/L2)**：在 4K 输入下，NV12 转换耗时从 0.56 ms 升至 **2.27 ms**，Letterbox 缩放耗时从 0.97 ms 升至 **4.91 ms**。CPU 预处理总耗时达 **7.18 ms**（占整帧 48%），成为 4K 场景的主要拖累。
3. **[重要] 多目标人脸提取的串行 O(N) 尾延迟尖峰 (L1/L2/L4)**：GLINTR 100 单次推理平均耗时 **4.3 ~ 4.8 ms**。在 16 人脸场景下，全量提取模式（`all`）耗时激增至 **79.48 ms**（GLINTR 推理占 **68.45 ms**，吞吐跌至 **12.6 FPS**）；在 `best_shot` 模式下，当多个 track 在同一帧确认时，P99 尾延迟骤升至 **70.08 ms**。

---

## 2. 证据等级分类体系说明

本报告所有结论均严格标注证据等级：
- **L1（直接测量，Direct Measurement）**：纳秒级单调时钟统计、`/usr/bin/time -l` 系统级硬件计数器、`/usr/bin/sample` 堆栈采样分析、IOSurface 实际分配结果。
- **L2（派生统计，Derived Statistics）**：由测量样本计算出的 P50、P95、P99、均值、FPS、分阶段耗时占比。
- **L3（工具推断，Tool Inference）**：结合 Apple Core ML、Accelerate vImage 内部调用栈（如 `MultiArrayBuffer::loadBuffer()`、`pthread_mutex_lock`）所反推的运行时行为。
- **L4（架构推论，Architectural Speculation）**：基于单张图片合成目标所推断的真实多路视频并发行为、ANE 硬件调度猜测等。

---

## 3. 全矩阵基准测试结果详表

测试矩阵覆盖 19 组完整场景：
- **分辨率**：Fixture (466x659)、1080P (1920x1080)、4K (3840x2160)
- **特征模式**：`best_shot`（跟踪确认帧提取一次）、`all`（每帧每个人脸均提取）、`detection_only`（仅检测跟踪，不提取特征）
- **Surface 类型**：`cvpixelbuffer`（Host 内存）、`cvpixelbuffer_iosurface`（IOSurface 硬件后备）、`host_nv12`（Packed、Aligned64、Padded128 跨距）
- **人脸规模**：0 人脸（纯背景）、1 人脸、4 人脸（2x2 网格）、16 人脸（4x4 网格）
- **样本规模**：开发基准为 30 预热 + 300 次测量；长稳基线为 60 预热 + 1000 次测量。

### 3.1 总体延迟与内存概览表 (L1/L2)

| Scenario | Resolution | Mode | Surface | Faces | Loops | Avg (ms) | P50 (ms) | P95 (ms) | P99 (ms) | FPS | RSS Peak (MB) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **fixture_best_shot** | 466x659 | best_shot | cvpixelbuffer | 1 | 30+300 | **7.61** | 7.36 | 9.08 | 10.78 | **131.3** | 325.5 |
| **fixture_all** | 466x659 | all | cvpixelbuffer | 1 | 30+300 | **13.46** | 13.45 | 16.29 | 18.23 | **74.3** | 325.5 |
| **fixture_detection_only**| 466x659 | detect | cvpixelbuffer | 1 | 30+300 | **7.64** | 7.39 | 9.52 | 10.91 | **131.0** | 325.5 |
| **1080p_best_shot** | 1920x1080 | best_shot | cvpixelbuffer | 1 | 30+300 | **8.84** | 8.44 | 10.71 | 13.19 | **113.2** | 325.5 |
| **1080p_all** | 1920x1080 | all | cvpixelbuffer | 1 | 30+300 | **15.31** | 14.19 | 19.72 | 35.29 | **65.3** | 325.5 |
| **1080p_detection_only** | 1920x1080 | detect | cvpixelbuffer | 1 | 30+300 | **8.99** | 8.61 | 11.58 | 13.35 | **111.2** | 325.5 |
| **4k_best_shot** | 3840x2160 | best_shot | cvpixelbuffer | 1 | 30+300 | **14.96** | 14.22 | 18.93 | 22.84 | **66.8** | 325.5 |
| **4k_all** | 3840x2160 | all | cvpixelbuffer | 1 | 30+300 | **19.31** | 18.82 | 23.57 | 28.20 | **51.8** | 325.5 |
| **4k_detection_only** | 3840x2160 | detect | cvpixelbuffer | 1 | 30+300 | **15.42** | 14.87 | 19.79 | 21.87 | **64.9** | 325.5 |
| **faces_0** | 1920x1080 | best_shot | cvpixelbuffer | 0 | 30+300 | **9.13** | 8.70 | 11.54 | 13.70 | **109.5** | 325.5 |
| **faces_1** | 1920x1080 | best_shot | cvpixelbuffer | 1 | 30+300 | **9.11** | 8.72 | 11.65 | 13.67 | **109.8** | 325.5 |
| **faces_4** | 1920x1080 | best_shot | cvpixelbuffer | 4 | 30+300 | **9.06** | 8.57 | 10.99 | 26.13 | **110.4** | 325.5 |
| **faces_16** | 1920x1080 | best_shot | cvpixelbuffer | 16 | 30+300 | **9.42** | 8.45 | 10.57 | 70.08 | **106.2** | 325.5 |
| **faces_16_all** | 1920x1080 | all | cvpixelbuffer | 16 | 30+300 | **79.48** | 77.69 | 100.61 | 137.55 | **12.6** | 325.5 |
| **stride_packed** | 1920x1080 | best_shot | host_nv12 | 1 | 30+300 | **9.64** | 9.31 | 12.49 | 14.33 | **103.7** | 325.5 |
| **stride_aligned64** | 1920x1080 | best_shot | host_nv12 | 1 | 30+300 | **9.85** | 9.48 | 12.62 | 15.46 | **101.5** | 325.5 |
| **stride_padded128** | 1920x1080 | best_shot | host_nv12 | 1 | 30+300 | **9.19** | 8.77 | 11.56 | 14.63 | **108.8** | 325.5 |
| **surface_iosurface** | 1920x1080 | best_shot | iosurface | 1 | 30+300 | **8.50** | 8.23 | 9.91 | 11.67 | **117.7** | 325.5 |
| **stability_1000** | 1920x1080 | best_shot | cvpixelbuffer | 1 | 60+1000 | **9.81** | 9.12 | 12.77 | 19.38 | **101.9** | 325.5 |

---

### 3.2 阶段耗时平均值拆解表 (Avg ms, L1/L2)

| Scenario | NV12 Conv | Letterbox | SCRFD Infer | SCRFD Copy | Decode/NMS | Tracker | Align | GLINTR Infer | GLINTR Copy | JSON |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **fixture_best_shot** | 0.119 | 0.237 | 2.041 | **4.742** | 0.005 | 0.004 | 0.001 | 0.042 | 0.0000 | 0.012 |
| **fixture_all** | 0.137 | 0.313 | 2.209 | **5.324** | 0.006 | 0.035 | 0.103 | **4.858** | 0.0031 | 0.017 |
| **fixture_detection_only**| 0.125 | 0.232 | 2.034 | **4.759** | 0.005 | 0.004 | 0.001 | 0.048 | 0.0001 | 0.012 |
| **1080p_best_shot** | 0.558 | 0.975 | 2.058 | **4.702** | 0.005 | 0.004 | 0.001 | 0.047 | 0.0000 | 0.012 |
| **1080p_all** | 0.649 | 1.281 | 2.238 | **5.659** | 0.006 | 0.046 | 0.123 | **4.758** | 0.0030 | 0.018 |
| **1080p_detection_only** | 0.578 | 0.961 | 2.091 | **4.813** | 0.005 | 0.004 | 0.001 | 0.039 | 0.0000 | 0.013 |
| **4k_best_shot** | **2.273** | **4.906** | 2.116 | **4.884** | 0.005 | 0.004 | 0.003 | 0.048 | 0.0000 | 0.013 |
| **4k_all** | **2.222** | **4.781** | 2.079 | **4.777** | 0.006 | 0.028 | 0.267 | **4.436** | 0.0024 | 0.015 |
| **4k_detection_only** | **2.271** | **5.164** | 2.151 | **5.042** | 0.006 | 0.004 | 0.002 | 0.040 | 0.0000 | 0.014 |
| **faces_0** | 0.606 | 1.026 | 2.128 | **4.864** | 0.004 | 0.002 | 0.000 | 0.000 | 0.0000 | 0.000 |
| **faces_1** | 0.594 | 0.999 | 2.093 | **4.863** | 0.005 | 0.004 | 0.001 | 0.046 | 0.0000 | 0.013 |
| **faces_4** | 0.568 | 0.952 | 2.091 | **4.735** | 0.009 | 0.006 | 0.004 | 0.190 | 0.0001 | 0.024 |
| **faces_16** | 0.577 | 0.947 | 2.066 | **4.590** | 0.018 | 0.011 | 0.013 | 0.659 | 0.0003 | 0.064 |
| **faces_16_all** | 0.588 | 1.057 | 2.100 | **4.833** | 0.020 | 0.264 | **1.419** | **68.452**| 0.0340 | 0.088 |
| **stride_packed** | 0.632 | 1.145 | 2.198 | **5.102** | 0.006 | 0.005 | 0.001 | 0.042 | 0.0000 | 0.014 |
| **stride_aligned64** | 0.623 | 1.198 | 2.202 | **5.237** | 0.006 | 0.005 | 0.001 | 0.051 | 0.0000 | 0.015 |
| **stride_padded128** | 0.615 | 1.045 | 2.101 | **4.859** | 0.005 | 0.004 | 0.001 | 0.050 | 0.0000 | 0.013 |
| **surface_iosurface** | 0.538 | 0.880 | 2.019 | **4.534** | 0.005 | 0.004 | 0.001 | 0.037 | 0.0000 | 0.011 |
| **stability_1000** | 0.637 | 1.144 | 2.272 | **5.204** | 0.007 | 0.005 | 0.000 | 0.012 | 0.0000 | 0.014 |

---

## 4. 关键阶段深入剖析

### 4.1 核心瓶颈：SCRFD 9-Head MultiArray 拷回开销 (L1/L3)

#### 数据事实 (L1/L2)
在所有测试场景中，`SCRFD Inference` 均在 **2.0 ~ 2.2 ms** 之间，而 `SCRFD Copy (9H)` 始终稳定在 **4.5 ~ 5.6 ms**。
在 `fixture_best_shot` 中，整帧耗时 7.61 ms，其中：
- SCRFD 输出拷回占 **62.3%**；
- SCRFD 推理占 **26.8%**；
- 剩余所有阶段合计仅占 **10.9%**。

#### 运行时机理与采样证据 (L1/L3)
我们使用 `/usr/bin/sample` 在 2000 循环压测期间对进行采样的堆栈结果显示：
```text
Call graph:
    269 Thread_4153007: Main Thread   DispatchQueue_<multiple>
    + 269 start  (in dyld)
    +   269 main  (in face_recognition_runner)
    +     269 run_single_benchmark
    +       252 instance_process
    +       ! 83 run_scrfd_internal
    +       ! : 83 copy_multiarray_to_float_vector
    +       ! :   32 -[MLMultiArray dataPointer]
    +       ! :   | 19 CoreML::MultiArrayBuffer::loadBuffer() const
    +       ! :   | + 16 <deduplicated_symbol>
    +       ! :   | + ! 5 std::__sp_mut::lock()
    +       ! :   | + ! : 3 pthread_mutex_lock
    +       ! :   | + ! 5 std::__sp_mut::unlock()
    +       ! :   | + ! : 4 pthread_mutex_unlock
```
**机理解释**：
- SCRFD 10G 网络为 3 尺度多任务头设计，分别输出 3 组检测框（stride 8/16/32）、3 组置信度、3 组五关键点，共计 **9 个独立的 MLMultiArray**。
- Apple Core ML（尤其是基于 MLE5Engine 执行后端）将输出保留在 E5/Neural Engine 硬件缓冲区中。当代码调用 `[MLMultiArray dataPointer]` 时，Core ML 内部触发了 `MultiArrayBuffer::loadBuffer()`，执行跨进程/硬件的线程锁同步与 Host 虚拟地址映射。
- 随后，`copy_multiarray_to_float_vector` 逐元素遍历或做类型转换拷贝。
- **每帧执行 9 次独立的加锁同步与内存复制**，累积消耗了 4.7 ~ 5.3 ms 的 CPU 时间！

---

### 4.2 图像预处理与分辨率扩展性 (L1/L2)

1. **NV12 转 RGB 与 Stride 鲁棒性**：
   - 在 1080P 下，Packed NV12、Aligned64（`align64(width)`）与 Padded128（`width+128`）的转换耗时分别为 **0.632 ms**、**0.623 ms**、**0.615 ms**。
   - `vImageConvert_Planar8toARGB8888` 配合显式 `rowBytes` 步长完美吸收了硬件对齐与填充，无额外损耗。
2. **4K 场景的内存墙效应**：
   - 1080P NV12 双平面的内存占用为 3.11 MB，RGB 中间缓冲为 6.22 MB，预处理总耗时约 **1.53 ms**。
   - 4K NV12 双平面内存占用为 12.44 MB，RGB 中间缓冲为 24.88 MB，NV12 转换升至 **2.27 ms**，Letterbox 双线性缩放（从 3840x2160 到 640x384）升至 **4.91 ms**，预处理总耗时达 **7.18 ms**。
   - 这表明在无硬件加速（如 GPU/MPS 或专用硬缩放器）介入时，纯 CPU 4K 图像缩放对单流分析已形成严重阻碍（上限 ~66 FPS）。

---

### 4.3 多人脸并发与特征提取扩展性 (L1/L2/L4)

对比 1080P 下人脸数变化实验：
- **0 人脸 (`faces_0`)**：无任何后续处理，耗时 **9.13 ms**（SCRFD 推理 2.13 ms + 拷贝 4.86 ms + 预处理 1.63 ms）。
- **1 人脸 (`faces_1`)**：耗时 **9.11 ms**。
- **4 人脸 (`faces_4`)**：`best_shot` 均值 **9.06 ms**，但 P99 尾延迟升至 **26.13 ms**（4 个人脸在同一帧确认并提取特征，GLINTR 推理耗时 16.77 ms）。
- **16 人脸 (`faces_16`)**：`best_shot` 均值 **9.42 ms**，P99 尾延迟骤升至 **70.08 ms**（Max **87.79 ms**，16 个人脸同时确认，GLINTR 推理耗时 59.71 ms）。
- **16 人脸全量提取 (`faces_16_all`)**：每帧提取 16 个人脸特征，GLINTR 推理耗时平均 **68.45 ms**（P99 **99.45 ms**），人脸对齐 **1.42 ms**，整帧耗时 **79.48 ms**，吞吐跌至 **12.6 FPS**。

**关键推论 (L4)**：
GLINTR 100 是单样本（Batch=1）前向网络，单次推理耗时约 4.3 ms。在人群密集或频繁切换新人的场景下，串行推理导致整帧分析出现显著的周期性卡顿与尾延迟跳跃。

---

## 5. 内存与稳定性评估

### 5.1 内存生命周期剖析 (L1/L2)
通过 `/usr/bin/time -l` 与 `mach_task_basic_info` 采集到的物理驻留内存（Resident Set Size, RSS）：
- **加载前基础 RSS**：约 **148 MB**（包含动态链接器、Foundation、CoreML 等系统共享库基线）。
- **模型加载完成态**：
  - 466x659 Fixture：**168.9 MB**（增量 ~20.9 MB）
  - 1080P：**196.8 MB**（增量 ~48.8 MB，包含 1080P 输入 CVPixelBuffer 与临时缓冲）
  - 4K：**296.5 MB**（增量 ~148.5 MB，包含 4K 输入 Surface 与 25MB RGB Canvas）
- **Core ML 预热峰值 (Peak RSS)**：**325.5 MB**。
  - `/usr/bin/time -l` 测量 `maximum resident set size` 为 **342,474,752 字节**（326.6 MB）。
  - 该峰值发生在 `library_open` 中对 SCRFD 和 GLINTR 执行 `compile_and_load_model` 及首轮 Dummy 前向传播期间。Core ML 编译器与 JIT 引擎在此阶段分配编译图与工作空间，随后释放临时工作内存。
- **实例释放态**：调用 `library_close` 后，RSS 维持在稳定态水平（约 169 ~ 202 MB），无不可逆常驻增长。

### 5.2 1000 帧长稳测试 (L1/L2)
在 `stability_1000`（60 warmup + 1000 循环）中：
- 预热完毕后 RSS：**202.375 MB**
- 1000 帧处理完毕后 RSS：**202.469 MB**
- **1000 帧内存净增量仅 +0.094 MB**，属于系统内存池自然碎片范围。
- ASan 运行时检测无内存越界和无泄漏。证明 C++ 处理链路、CVPixelBuffer 引用计数管理及 JSON 序列化具有极高稳定性。

---

## 6. 性能观测工具可用性报告 (Tooling Availability)

| 工具 / 观测方式 | 可用性状态 | 证据与限制说明 |
| :--- | :--- | :--- |
| **`xctrace / Instruments`** | **BLOCKED** | 系统未安装完整 Xcode.app（仅安装了 Command Line Tools）。执行 `xctrace record` 报错：`xctrace requires Xcode to be installed. Run xcode-select to select it if it's already installed.`。在此环境下无法直接采集 ANE 硬件 Counters trace。 |
| **`/usr/bin/time -l`** | **AVAILABLE** | 完全可用。测量得到真实物理峰值内存（342.5 MB）、用户态/内核态 CPU 时间、指令数（3.04×10¹⁰ instructions / 100 loops）、周期数（7.38×10⁹ cycles）以及缺页次数（9,358 page faults）。 |
| **`/usr/bin/sample`** | **AVAILABLE** | 完全可用。在运行循环中以 10 ms 间隔采样 3 秒，成功定位到 `CoreML::MultiArrayBuffer::loadBuffer()` 和 `pthread_mutex_lock` 的瓶颈调用链。 |
| **`os_signpost`** | **AVAILABLE** | 代码内已完整实现并测试验证。在 `ENABLE_PROFILING=ON` 时向 `OS_LOG_CATEGORY_POINTS_OF_INTEREST` 发送带有阶段 tag 的 signpost 标记，后续在拥有完整 Xcode 的机器上可直接无缝可视化。 |

---

## 7. 后续优化建议（交付父任务参考）

本子任务遵循 Trellis 规范，仅做基线测定与瓶颈定位，不修改生产推理逻辑。针对发现的明确瓶颈，建议后续父任务优先安排以下优化：

1. **[优先级 P0] SCRFD 9-Head MultiArray 零拷贝或跨步指针访问**
   - **现状**：9 个 head 每次分别 `dataPointer` 并加锁拷贝，耗时 ~5 ms。
   - **优化方案**：在 Core ML 导出或加载配置中探索零拷贝 Surface 输出；或直接读取底层的连续 float 指针进行跨步索引，避免分配 `std::vector<float>`；或在模型导出时将 3 个尺度的 score、bbox、kps 分别在网络末端 Concat，将 9 个 head 减少至 1~3 个 head，减少 70% 的 `loadBuffer()` 锁同步次数。
   - **预期收益**：整帧延迟从 8.8 ms 降至 **4.5 ms 左右**，吞吐量突破 **220 FPS**。
2. **[优先级 P0] GLINTR 人脸特征提取批量化 (Batching) 或并发化**
   - **现状**：多目标时串行调用，16 目标耗时 68 ms。
   - **优化方案**：为 GLINTR Core ML 模型增加 Dynamic Batch（支持 Batch 1/4/8/16），或者引入异步推理队列，将人脸对齐和特征提取任务放入工作线程池中，避免阻塞主视频分析管道。
   - **预期收益**：16 目标全量提取耗时由 80 ms 降低至 **20 ms 以内**。
3. **[优先级 P1] 4K 分辨率下的硬件预降采样**
   - **现状**：CPU vImage 缩放 4K 耗时 ~5 ms。
   - **优化方案**：在输入到达算法前，由 Engine 或 VideoToolbox 解码器直接输出 1080P 或 640 宽度的中间帧；或使用 Metal Performance Shaders (MPS) 在 GPU 上完成 NV12 到 RGB 的 Letterbox。
   - **预期收益**：4K 分析帧率从 66 FPS 提升至 **100+ FPS**。
