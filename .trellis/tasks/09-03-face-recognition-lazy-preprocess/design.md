# Technical Design: 彻底废除全图 RGB 零冗余预处理架构

> 任务编号：`09-03-face-recognition-lazy-preprocess`  
> 目标模块：`algo-packages/macos/arm64/face_recognition/src/preprocess/`  
> 优化目标：彻底消除全分辨率 RGB/ARGB 内存分配与转码开销，直出 640x384 与 112x112  

---

## 1. 架构瓶颈根因回顾

在消除了 SCRFD 9-Head 5 ms 拷贝瓶颈后，预处理耗时成为了系统的主要开销：
- **4K 输入**：单帧无脑转换 830 万像素，消耗 33.2 MB ARGB + 24.9 MB RGB（总计 58.1 MB 堆内存），耗时 **6.85 ms**（占 4K 总延迟 73.5%）。
- **1080P 输入**：单帧无脑转换 207 万像素，消耗 14.5 MB 堆内存，耗时 **1.48 ms**（占 1080P 总延迟 40.2%）。
- **根本冗余**：
  - SCRFD 检测仅需 640x384 Letterbox RGB（23 万有效像素）；
  - GLINTR 特征提取仅在低频确认帧需要 112x112 特写（1.2 万像素）；
  - 生产链路完全不需要全分辨率 RGB。

---

## 2. 零冗余预处理架构方案

### 2.1 数据结构精简 (`PreprocessResult`)

从 `PreprocessResult` 中彻底剔除 `original_rgb`（25 MB 内存），改为直接借用输入帧的 NV12 平面只读指针与跨距：

```cpp
namespace face_recognition {

struct PreprocessResult {
    uint32_t orig_width = 0;
    uint32_t orig_height = 0;
    ImageBuffer letterbox_rgb;                  // 目标固定 640x384 (737 KB)
    argus::cv::LetterboxInfo letterbox_info;     // 几何变换映射信息

    // 只读借用当前帧的 NV12 裸指针与跨距 (零内存拷贝)
    const uint8_t* y_plane = nullptr;
    const uint8_t* uv_plane = nullptr;
    int32_t y_stride = 0;
    int32_t uv_stride = 0;
};

} // namespace face_recognition
```

### 2.2 直接分级 Letterbox 降采样 (`process_frame`)

不再把 830 万原图像素先转 ARGB/RGB，而是直接对 Y/UV 平面执行分级硬件降采样：
1. **Y 平面直接降采样**：
   使用 `vImageScale_Planar8` 将 $W \times H$ 的单通道 Y 数据直接缩放为 $nw \times nh$（16:9 下为 $640 \times 360$）。
2. **UV 平面直接降采样**：
   UV 平面为 2 通道交织数据（U8V8），使用 `vImageScale_Planar16U` 将 $(W/2) \times (H/2)$ 的 16 位象元直接缩放为 $(nw/2) \times (nh/2)$（即 $320 \times 180$）。
3. **小分辨率色彩转换 (36x 像素量降低)**：
   仅对缩放后的 $640 \times 360$ NV12 数据（仅 23 万像素）执行 `vImageConvert_420Yp8_CbCr8ToARGB8888` 与 `vImageConvert_ARGB8888toRGB888`，直接写入 `letterbox_rgb` 内部对应 ROI（外围保持中性灰 114 padding）。
4. **常驻复用 Scratch Buffer (0 Heap Churn)**：
   $640 \times 360$ 缩放所需的临时工作区（`scaled_y` 230 KB，`scaled_uv` 115 KB，`small_argb` 921 KB，总计 < 1.3 MB）全部使用常驻缓冲复用，单帧堆分配次数彻底为 **0**。

### 2.3 NV12 双平面直接五点仿射对齐截脸 (`align_face_112x112`)

当且仅当某帧确认需要提取 GLINTR 特征向量时（`need_extract == true`）：
1. 基于 5 关键点与 `kArcFaceSrc` 通过 Umeyama 算法求得 $2 \times 3$ 仿射变换矩阵 $M$ 并求逆矩阵 $M^{-1}$；
2. 遍历 $112 \times 112 = 12,544$ 个像素坐标 $(x, y)$，通过 $M^{-1}$ 映射至原图浮点坐标 $(\text{src}_x, \text{src}_y)$；
3. **Y 平面双线性插值**：在 `y_plane`（步长 `y_stride`）上双线性插值采样得到浮点 $Y$；
4. **UV 平面双线性插值**：在 `uv_plane`（步长 `uv_stride`）对应 $(\text{src}_x / 2.0, \text{src}_y / 2.0)$ 位置，分别对交织的 U 与 V 进行双线性插值采样得到浮点 $U, V$；
5. **BT.709 色彩转换**：
   在 CPU 寄存器内完成公式转换，直接写入输出 `out_face_112`：
   $$\begin{aligned}
   Y' &= (Y - 16.0) \times 1.164383 \\
   C_b &= U - 128.0, \quad C_r = V - 128.0 \\
   R &= \text{clamp}(Y' + 1.792741 \times C_r, 0, 255) \\
   G &= \text{clamp}(Y' - 0.213249 \times C_b - 0.532909 \times C_r, 0, 255) \\
   B &= \text{clamp}(Y' + 2.112402 \times C_b, 0, 255)
   \end{aligned}$$
6. 12,544 像素整体计算耗时 **< 0.04 ms**，且完全无需分配任何全图大内存。

---

## 3. 预期收益与指标

- **全分辨率内存占用**：由每帧 14.5 ~ 58.1 MB 降至 **0 MB**（彻底归零）。
- **1080P 预处理耗时**：由 **1.48 ms** 降至 **0.3 ~ 0.5 ms**；总 FPS 跃升至 **330+ FPS**。
- **4K 预处理耗时**：由 **6.85 ms** 降至 **1.0 ~ 1.5 ms**；总延迟由 9.3 ms 降至 **~4.5 ms**，总 FPS 翻倍突破 **200+ FPS**。
