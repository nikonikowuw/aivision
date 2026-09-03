# Technical Design: SCRFD 9-Head 零拷贝与按需解析优化

> 任务编号：`09-03-scrfd-multiarray-zero-copy`  
> 目标模块：`algo-packages/macos/arm64/face_recognition`  
> 评审目标：消除 SCRFD 9 个输出头 4.7 ~ 5.3 ms 的 MultiArray 拷贝与锁开销，实现微秒级零拷贝只读借用与按需解析  

---

## 1. 现状与性能根因分析

### 1.1 当前数据路径瓶颈
1. **每帧堆内存反复分配**：
   `ScrfdOutput` 包含 9 个 `std::vector<float>`（3 个 Score、3 个 Bbox、3 个 Kps）。每帧在堆上进行 9 次内存分配与重新调整大小（总计 151,200 个 float，约 604.8 KB）。
2. **`arr.dataPointer` 属性频繁访问与互斥锁争用**：
   在现有的 `copy_multiarray_to_float_vector` 中，如果 Core ML 输出张量的 strides 布局因 Batch=1 维度出现细微跨度偏离（例如 `strides[0] != expected`），会回退到非连续通用循环。而在该非连续循环内部，每一轮迭代都在执行 `arr.dataPointer`（Objective-C 动态属性消息）。在压测采样中捕获到 2,169 次 `CoreML::MultiArrayBuffer::loadBuffer()` 和 `pthread_mutex_lock` 调用。
3. **全量搬移与极度稀疏人脸的矛盾**：
   视频流中 10,080 个锚点中 99.9% 属于背景（Score < `conf_thresh`）。原代码无条件全量拷贝并解包所有 Bbox (40,320 个 float) 与 Landmarks (100,800 个 float)，造成了严重的无效数据搬运。

---

## 2. 核心架构设计：零拷贝 View 与按需解析

### 2.1 数据结构重构 (`ScrfdHeadView` & `ScrfdOutput`)

定义只读轻量跨距视图 `ScrfdHeadView`：

```cpp
namespace face_recognition {

/**
 * @brief 零拷贝只读输出头视图
 */
struct ScrfdHeadView {
    const float* data = nullptr;           // 连续或基地址指针 (Float32)
    const _Float16* data_fp16 = nullptr;   // 若 Core ML 原生输出为 Float16 则指向此指针
    bool is_fp16 = false;                  // 是否为 FP16 数据类型
    uint32_t num_anchors = 0;              // 锚点数量 (7680, 1920, 480 等)
    uint32_t dim1 = 1;                     // 通道维度 (Score=1, Bbox=4, Kps=10)
    int64_t stride_anchor = 0;             // 相邻 anchor 之间的元素步长
    int64_t stride_channel = 1;            // 相同 anchor 内相邻 channel 的元素步长

    // 内联快速读取单一浮点数
    inline float get(uint32_t a_idx, uint32_t c_idx = 0) const noexcept {
        const int64_t offset = a_idx * stride_anchor + c_idx * stride_channel;
        if (__builtin_expect(!is_fp16, 1)) {
            return data[offset];
        } else {
            return static_cast<float>(data_fp16[offset]);
        }
    }
};

/**
 * @brief SCRFD 9-head 零拷贝输出张量包
 */
struct ScrfdOutput {
    ScrfdHeadView score_8;
    ScrfdHeadView score_16;
    ScrfdHeadView score_32;
    ScrfdHeadView bbox_8;
    ScrfdHeadView bbox_16;
    ScrfdHeadView bbox_32;
    ScrfdHeadView kps_8;
    ScrfdHeadView kps_16;
    ScrfdHeadView kps_32;

    // 声明周期保持 Token：持有 Core ML MLFeatureProvider，
    // 确保底层 MLMultiArray 内存指针在当前帧后处理完成前始终有效且不被回收
    std::shared_ptr<void> buffer_holder;
};

} // namespace face_recognition
```

### 2.2 推理输出映射 (`run_scrfd_internal`)

在 `model_inference.mm` 中：
1. 仅对每个 `MLMultiArray` 调用一次 `dataPointer` 提取指针基地址；
2. 提取 `arr.strides` 与 `arr.shape` 计算 `stride_anchor` 与 `stride_channel`；
3. 将 `output_provider` 封装至 `out.buffer_holder`，通过 `std::shared_ptr` 的 custom deleter 维系生命周期：
   ```objc
   id<MLFeatureProvider> output_provider = [model predictionFromFeatures:input_provider error:&ns_error];
   out.buffer_holder = std::shared_ptr<void>((__bridge_retained void*)output_provider, [](void* ptr) {
       if (ptr) {
           CFRelease(ptr);
       }
   });
   ```
4. 彻底删除 9 次 `vector` 分配与 `memcpy` 拷贝。

### 2.3 后处理按需解码 (`decode_scrfd_faces`)

在 `postprocessor.cpp` 中重构 `decode_scrfd_faces`：
1. 遍历 10,080 个锚点时，**仅读取** `score_view.get(a_idx, 0)`；
2. 若 `score < conf_thresh`，直接 `continue`（**0 内存读取开销**）；
3. 仅当 `score >= conf_thresh` 时（通常每帧 0 ~ 5 次），通过 `bbox_view.get(a_idx, 0..3)` 与 `kps_view.get(a_idx, 0..9)` 延迟读取目标坐标并还原至原图。

---

## 3. 性能预期与验证指标

- **`scrfd_copy` 阶段耗时**：
  由 **4.7 ~ 5.3 ms** 骤降至 **< 0.05 ms**（仅做 9 次指针与 stride 赋值）。
- **`decode_nms` 阶段耗时**：
  由于无需遍历非命中目标的 bbox 和 landmarks，解码耗时稳定在 **0.05 ~ 0.15 ms**。
- **1080P 吞吐量与总耗时**：
  总耗时由 **8.84 ms** 降至 **4.2 ~ 4.6 ms**，吞吐量由 **113 FPS 翻倍至 220+ FPS**。
- **内存堆分配与 RSS**：
  每帧临时堆分配减少 604.8 KB，GC/堆震荡彻底消除。
