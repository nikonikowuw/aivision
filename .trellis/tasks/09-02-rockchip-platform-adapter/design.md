# Technical Design: Engine Rockchip Platform Adapter

## 1. 系统架构与模块划分 (Architecture Overview)

```text
engine/src/platform/rockchip/
├── rockchip_platform.hpp / .cpp     # IPlatformAdapter 实现与 PlatformProfile 组装
├── rockchip_decoder.hpp / .cpp      # 基于 Rockchip MPP 的硬件解码器 (IDecoder)
├── rockchip_image_ops.hpp / .cpp    # 基于 RGA 2.0 (im2d) 的 2D 硬件图像处理 (IImageProcessor + c_image_ops)
│                                    # 含 encode_jpeg / encode_thumbnail_jpeg 实现（见 2.4）
├── rockchip_telemetry.hpp / .cpp    # 基于 Linux procfs/sysfs/debugfs 的系统与 NPU/温度遥测 (ITelemetry)
└── rockchip_mpp_utils.hpp / .cpp    # MPP Buffer、DMA-BUF 封装与内存管理工具类
```

> **模块边界说明**：`encode_jpeg` / `encode_thumbnail_jpeg` 是 `IImageProcessor`
> （`engine/include/argus/platform/platform_api.hpp`）的纯虚/虚成员，不是独立接口。
> 因此 PRD FR-3 不产生独立的 `RockchipJpegEncoder` 适配器类，只在
> `rockchip_image_ops.cpp` 内以私有编码器类（MPP MJPEG 编码上下文封装）承载，
> 由 `RockchipImageOps` 委托调用。

---

## 2. 详细设计 (Detailed Component Design)

### 2.1 MPP 硬件解码器 (`RockchipDecoder`)
- **API 接口**：`mpp_create`, `mpp_init(ctx, MPP_CTX_DEC, MPP_VIDEO_CodingAVC / MPP_VIDEO_CodingHEVC)`, `mpp_decode_put_packet`, `mpp_decode_get_frame`, `mpp_destroy`；
- **Access Unit 送包机制**：
  - 将输入的 H.264/H.265 NAL 数据封装为 `MppPacket`（设置 `mpp_packet_set_pts` 与 `mpp_packet_set_is_intra`）；
  - 调用 `mpi->decode_put_packet(ctx, packet)` 送入解码管线；
- **Frame 接收与生命周期控制**：
  - 调用 `mpi->decode_get_frame(ctx, &frame)`；
  - 提取 `MppBuffer` 句柄与 `mpp_frame_get_err_info(frame)`；
  - 若为有效 YUV 帧，调用 `mpp_buffer_get_fd(buf)` 获取底层的 `dma_buf` fd 或直接获取物理/虚拟地址；
  - 组装为 `av_frame_desc`：
    - `pixel_format = AV_PIX_NV12`，`layout = AV_LAYOUT_PLATFORM_NATIVE`；
    - `memory_type = AV_MEM_PLATFORM_SURFACE`，`opaque_kind = AV_OPAQUE_DMABUF`（`0x2001`，已在 ABI 中预留）；
    - `opaque` = 只读平台句柄（DMA-BUF fd 的持有者 wrapper）；`frame_token` = 只供 `av_frame_ops` 使用的私有 wrapper 指针；二者按 spec 边界 5 严禁混用；
  - 在 `av_frame_ops.retain` 与 `release` 中调用 `mpp_frame_deinit` / `mpp_buffer_inc_ref` / `mpp_buffer_dec_ref`，确保算法包在完成推理后安全释放 MPP 缓冲池。

> **不新增 ABI 枚举**：`sdk/include/argus/types.h` 的 `av_memory_type` 只有
> `AV_MEM_UNKNOWN / AV_MEM_HOST / AV_MEM_PLATFORM_SURFACE`，**不存在 `AV_MEM_DMA_BUF`**。
> DMA-BUF 语义通过 `AV_MEM_PLATFORM_SURFACE` + `AV_OPAQUE_DMABUF` 组合表达，与 macOS 的
> `AV_OPAQUE_CVPIXELBUFFER` 同构。本任务**不修改 SDK ABI**：新增枚举值会牵动 152B 布局断言、
> 双编译器契约测试与已发布的 `algo-packages/rknn/rk3576/yolov8n` vendored SDK，属范围外破坏性变更。


### 2.2 RGA 2.0 图像加速与 C ABI (`RockchipImageOps`)
- **RGA 上下文**：使用 Rockchip 官方推荐的 `im2d` API（头文件 `<rga/im2d.h>`）；
- **色彩转换与缩放 (`convert`)**：
  - 输入：`av_frame_desc`（从 `dma_buf` fd 或内存指针构建 `rga_buffer_t`）；
  - 输出：目标 `av_image_view`（如 640x640 RGB24 / BGRA）；
  - 核心调用：`imcrop` + `imcvtcolor` + `imresize` / `improcess`；
- **Padding (`pad`)**：
  - 支持硬件黑边填充（Letterbox padding），调用 `imfill` 或 `imcopy` 边界扩展；
- **对齐与 Fallback**：
  - RGA 硬件对宽、高与步长（stride）有 2/4/16 字节对齐约束；
  - 对于不符合 RGA 硬件约束的异常输入，自动调用内置的 NEON/CPU 高效算法，保证 100% 鲁棒性。

### 2.3 硬件与系统遥测 (`RockchipTelemetry`)
- **NPU 负载**：
  - 读取 `/sys/kernel/debug/rknpu/load`；
  - 解析输出文本（例如 `NPU load: Core0: 35%, Core1: 42%` 或单核数值），计算多核平均利用率；
- **芯片温度**：
  - 扫描 `/sys/class/thermal/thermal_zone*/`；
  - 匹配 `type` 为 `soc-thermal`, `npu-thermal` 或 `cpu-thermal` 的传感器，读取 `temp` 并除以 1000 转换为摄氏度；
- **系统基础负载**：
  - 读取 `/proc/stat` 计算 CPU 使用率；
  - 读取 `/proc/meminfo`（`MemTotal`, `MemAvailable`）计算内存使用率；
  - 调用 `statvfs` 获取工作目录磁盘使用率。
- **不可用语义**：`SystemMetrics.accelerator_usage_percent` 与 `temperature_celsius` 默认已是
  `quiet_NaN()`，节点缺失/无权限时**保持 NaN 且不写入伪造值**，同时把 `PlatformProfile.telemetry_metrics`
  标记为 `DEGRADED` 并在 `reason` 写明缺失的节点路径（对应 AC-5）。

### 2.4 JPEG 抓拍编码（`RockchipImageOps` 内部）
- 实现 `IImageProcessor::encode_jpeg`：先经 RGA 完成 ROI 裁剪与（必要的）色彩转换，再送入编码器；
- 编码器优先级：MPP 硬件 MJPEG（`MPP_VIDEO_CodingMJPEG`，`MPP_CTX_ENC`）→ libjpeg-turbo 软编回退；
  两条路径的可用性在构造期探测一次并记入 `PlatformProfile.vector_image_ops.reason`；
- `encode_thumbnail_jpeg` 覆写基类默认实现：用 RGA 硬件缩放到 `max_width` 后再编码，避免全尺寸软编开销。

---

## 3. 构建与交叉编译设计 (Build & Packaging)

- **CMake 构建目标**：
  - 在 `engine/src/platform/CMakeLists.txt` 中增加 `platform_rockchip` 目标；
  - 当 `PLATFORM_TARGET STREQUAL "rockchip"` 时编译该模块并链接 `rockchip_mpp` 与 `rga`；
  - 同时应用 `argus_enable_strict_warnings()`，保持 `-Werror` 一致。
- **`PLATFORM_TARGET` 条件判断需要小范围整理（而非单纯加分支）**：
  现有代码把平台判断写成 `if(APPLE AND PLATFORM_TARGET STREQUAL "macos")`，共三处语义不同的用途，
  加入第三个平台后必须分别处理，不能复制粘贴：
  | 位置 | 现状 | 本任务处理 |
  | --- | --- | --- |
  | `engine/CMakeLists.txt:4` | 缓存描述 `"Target platform (macos or mock)"` | 补充 `rockchip`，并校验取值合法性 |
  | `engine/CMakeLists.txt:6` | `enable_language(OBJCXX)` | 仅 macos 需要，保持 `APPLE AND` 前缀不变 |
  | `engine/CMakeLists.txt:67` | `set(ARGUS_USE_ZLM ON)` 仅 macos | **本任务不动**，见下方媒体后端边界 |
  | `engine/CMakeLists.txt:68` 起 | ZLM 子项目与链接 | 本任务不动 |
- **媒体后端边界（重要范围声明）**：
  `engine/src/app/main.cpp:206` 在非 `ARGUS_PLATFORM_MACOS` 下一律 `create_mock_backend()`，
  且 `ARGUS_USE_ZLM` 只在 macOS 打开。因此 **platform_rockchip 完成后，RK 板上的拉流仍是 mock**。
  aarch64 下启用 ZLMediaKit 是独立且体量不小的工程（third_party 交叉编译、依赖裁剪），
  已拆分为子任务 `09-02-engine-zlm-aarch64-e2e`，本任务只负责 platform 适配层，
  `main.cpp` 侧仅新增 `ARGUS_PLATFORM_ROCKCHIP` 分支来激活 `RockchipPlatformAdapter`，不改媒体后端选择逻辑。
- **Docker 镜像与 SDK 准备**：
  - 扩展 `deploy/docker/Dockerfile.cross-rknn`，安装/编译 `librga` 与 `mpp` 的 aarch64 开发库；
  - 更新 `deploy/scripts/build-rknn-bundle.sh`，把 `-DPLATFORM_TARGET=mock` 改为 `rockchip`，并额外打包
    `librockchip_mpp.so` / `librga.so`。

---

## 4. 验证策略（无硬件环境）

当前开发机为 macOS/arm64，MPP 与 RGA 的**运行时正确性无法在本地验证**。本任务按「可离线验证」与
「需硬件验收」两层切分，硬件层归子任务：

| 层次 | 内容 | 本地可验证 |
| --- | --- | --- |
| 编译 | aarch64 交叉编译 + `-Werror` + 边界纯洁性 | 是（Docker） |
| 纯逻辑单测 | telemetry 文本解析（含畸形输入）、RGA 对齐判定、letterbox 缩放/pad 数值计算、fallback 触发条件 | 是（GTest，注入 fixture 文本与 fake 文件系统根） |
| 硬件运行时 | MPP 解码像素正确性、RGA PSNR、NPU/温度真实读数、端到端 FPS | 否 → 子任务 |

