# Argus 人脸识别系统性能与跨层架构分析报告

> 面向维护者的综合技术报告  
> 适用版本：`v1.0.0`（macOS arm64 Apple Silicon 架构）  
> 评测日期：2026-09-03  

---

## 1. 架构总览

Argus 人脸识别由四个协同层组成：
1. **算法包层 (`algo-packages/macos/arm64/face_recognition`)**：
   - 模型架构：SCRFD 10G 人脸关键点检测 + ByteTracker 轨迹关联 + 简易质量评估 + 五点相似变换对齐 + GLINTR 100 512维特征提取。
   - 特征模式：`best_shot`（轨迹确认并稳定后提取一次，兼顾精度与性能）与 `all`（每帧全量提取）。
2. **SDK C ABI 契约层 (`sdk/c_abi`)**：
   - 纯 C ABI 导出（`av_algo_library_open`, `av_algo_instance_create`, `av_algo_instance_process` 等）。
   - 强符号隔离，零 C++ 符号导出。
3. **推理与抓拍引擎层 (`engine`)**：
   - 帧缓冲池 (`FramePool`) 管理帧引用计数与生命周期。
   - UDS IPC 服务 (`uds_server.cpp`) 负责报文白名单校验、`FaceGallery` 1:N 向量检索与 Top-5 候选排序。
   - 异步抓拍工作线程负责全景图与特写图 JPEG 编码及落盘。
4. **Go 后端业务层 (`argus`)**：
   - 接收 UDS gRPC 上报（`ReportFaceObservation` / `ReportFaceCapture`）。
   - 单调 Upsert 落库与时序快照追加；95% 磁盘极危熔断保护与孤儿图片周期性清理。

---

## 2. 性能基线数据 (Apple M4, 24 GB)

实测环境：Apple M4 (10-core, 4P+6E), 24GB Unified Memory, macOS 26.5.1, Release (`-O3 -DNDEBUG`)。

### 2.1 分辨率与人脸规模优化演进表

| 输入场景 | 分辨率 | 初始基准 (FPS / ms) | SCRFD 零拷贝后 (FPS / ms) | 彻底废除全图 RGB 后 (FPS / ms) | 累计提速幅度 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **基线单图** | 466x659 | 131.3 FPS (7.61 ms) | 405.2 FPS (2.47 ms) | **435.1 FPS (2.30 ms)** | **+231.4% (3.3x)** |
| **标准 1080P** | 1920x1080 | 113.2 FPS (8.84 ms) | 271.7 FPS (3.68 ms) | **425.9 FPS (2.35 ms)** | **+276.2% (3.8x)** |
| **超清 4K** | 3840x2160 | 66.8 FPS (14.96 ms) | 107.3 FPS (9.32 ms) | **291.8 FPS (3.43 ms)** | **+336.8% (4.4x)** |
| **1080P 16 人脸** | 1920x1080 | 106.2 FPS (9.42 ms) | 193.4 FPS (5.17 ms) | **322.9 FPS (3.10 ms)** | **+204.0% (3.0x)** |

*长稳与内存安全：1000 帧连续压测前后内存零漂移；单帧堆内存临时分配（Heap Churn）从 58.1 MB 彻底归零（0 MB）；AddressSanitizer (ASan) 运行时检测零内存泄漏、零越界。*

### 2.2 阶段耗时演进对比 (以 1080P best_shot 为例)

- **NV12 转码 + Letterbox**：初始 1.54 ms → 优化后 **0.466 ms**（降采样像素减少 36 倍，废除全图 RGB）
- **SCRFD 9-Head 输出拷贝**：初始 4.70 ms → 优化后 **0.002 ms**（零拷贝指针借用）
- **SCRFD 10G 前向推理**：稳定在 1.7 ~ 1.8 ms
- **人脸五点仿射截脸对齐**：初始 0.15 ms → 优化后 **0.001 ms**（直接在 NV12 原图双平面点采样）
- **总帧耗时**：从 **8.84 ms** 骤降至 **2.35 ms**（吞吐达 **425.9 FPS**）

---

## 3. 核心瓶颈机理解析

### 3.1 SCRFD 9-Head MultiArray 拷回开销
- **现象**：SCRFD 推理仅需 2.0 ms，但 9 个 MultiArray 输出头拷贝至 CPU 内存耗时高达 4.7 ~ 5.3 ms。
- **调用栈证据 (`/usr/bin/sample`)**：
  ```text
  copy_multiarray_to_float_vector -> -[MLMultiArray dataPointer]
    -> CoreML::MultiArrayBuffer::loadBuffer() const
    -> std::__sp_mut::lock() -> pthread_mutex_lock
  ```
- **机理**：Core ML MLE5Engine 在输出 MultiArray 被访问时触发同步锁与物理地址刷新。9 个独立的输出头导致每帧执行 9 次跨硬件同步与加锁。

### 3.2 4K 图像 CPU 预处理瓶颈
- 4K 输入时，纯 CPU vImage 预处理（NV12 转换 2.27 ms + Letterbox 4.91 ms = 7.18 ms）占整帧耗时的 48%，导致 4K 分析帧率受限于 66 FPS。

### 3.3 多人脸特征提取串行 O(N) 尾延迟
- GLINTR 100 仅支持 Batch=1 串行推理。在 16 人脸全量提取场景下，GLINTR 推理耗时高达 68.5 ms，吞吐跌至 12.6 FPS。在 `best_shot` 模式下，多目标同帧确认时 P99 出现 70 ms 尖峰。

---

## 4. 跨层契约与安全性保证

### 4.1 资源生命周期
- **视频帧引用计数**：解码线程分配（`ref=1`），算法只读借用不增减；抓拍决策时 Engine 显式 `retain`（`ref=2`）；异步编码落盘后 `release`。队列满丢弃或异常时确保配对释放，测试断言 `pool.active_frame_count() == 0`。
- **回调内存可见性**：算法包向 Engine 回传的裸指针仅在回调函数执行期有效；Engine 内部同步完成反序列化与深拷贝，严禁向异步队列传递裸指针。
- **FaceGallery RCU 无锁热换**：采用 `std::atomic_load/store` 替换内部不可变快照，1:N 比对完全无锁，换库与推理并发完全解耦。

### 4.2 业务数据一致性
- **单调 Upsert 防迟到覆写**：Go 后端在数据库事务中使用 `Where("event_id = ? AND similarity < ?", newSim)`，只有更高相似度才允许更新，杜绝网络重试引发的迟到降级。
- **防爆盘熔断与孤儿清理**：磁盘使用率 $\ge 95\%$ 时自动熔断图片持久化；Go 后端定期扫描磁盘，通过 `FindExistingImageIDs` 批量对账并物理硬删除孤儿图片。

---

## 5. 优化路线图与演进状态 (Roadmap)

1. **[已完成] SCRFD 9-Head MultiArray 零拷贝/跨步访问 (P0)**：
   - 采用只读借用基地址与跨步缓存，拷贝耗时从 4.70 ms 压缩至 0.002 ms（99.9% 消除）。
2. **[已完成] 彻底废除全图 RGB 与 NV12 直接采样降采样 (P0)**：
   - 消除全尺寸 14.5 ~ 58.1 MB 堆分配（Heap Churn 归零）；
   - NV12 直接降采样至 640x384，4K 预处理耗时从 6.85 ms 降至 1.48 ms，4K 吞吐跃升至 291.8 FPS；1080P 吞吐跃升至 425.9 FPS。
3. **[进行中/候选] GLINTR 动态 Batching 或异步 Worker 线程池 (P0)**：
   - 为 GLINTR 模型增加 Dynamic Batch 支持，或将特征提取移出主视频流线程，消除密集人群场景的尾延迟尖峰。
4. **[后续储备] 人脸五关键点 (Landmarks) 跨层透传 (P1)**：
   - 在 `FaceObservation` / `FaceCapture` Protobuf 中增加 landmarks 字段，支持前端高精人脸五官标定渲染。
