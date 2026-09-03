# PRD: Engine Rockchip (RK3576 / RK3588) 平台适配器

## 1. 背景与目标 (Background & Goal)

### 1.1 背景
Argus 系统的核心定位是边缘 AI 视频分析与流媒体处理网关，其媒体与推理引擎（`argus-engine`）设计了分层解耦的插件化架构：
- 媒体拉流接入层：基于 ZLMediaKit 提供统一的 RTSP/RTMP 解复用与流转发；
- 平台硬件抽象层：通过 `IPlatformAdapter`（`IDecoder` / `IImageProcessor` / `ITelemetry`）屏蔽底层硬件差异；
- 算法推理层：通过标准纯 C ABI 动态加载独立的算法插件包（`.so`），RKNN 算法包内部拥有 `rknpu2` 上下文。

目前引擎已经实现了 `macos` (VideoToolbox + CoreImage) 与 `mock` 适配器，算法包层已完成了 `rk3576-rknn` (YOLOv8n) 的实现与量化。为了让 Argus 能够在国产边缘计算芯片（如 Rockchip RK3576、RK3588、RK3568）上实现全硬件加速与低延迟、低功耗运行，需要为 `argus-engine` 构建原生的 Rockchip 平台适配层。

### 1.2 核心目标 (Goal)
构建原生的 `platform_rockchip` 平台适配器，实现：
1. **硬件视频解码**：对接 Rockchip **MPP (Media Process Platform)**，支持多路 1080p/4K H.264 与 H.265 (HEVC) 硬件解码；
2. **硬件 2D 图像加速**：对接 Rockchip **RGA (librga / im2d API)**，实现零拷贝/硬件加速的 NV12 色彩空间转换、ROI 裁剪、Letterbox 缩放与内存管理；
3. **零拷贝帧生命周期管理**：对接 Linux `dma-buf` 机制与 MPP Buffer，在 C ABI `av_frame_desc` 中以现有 ABI 语义（`memory_type = AV_MEM_PLATFORM_SURFACE` + `opaque_kind = AV_OPAQUE_DMABUF`）安全传递 `dma-buf` 句柄，配合 `av_frame_ops` 保证引用计数安全与零拷贝，**不新增 SDK ABI 枚举**；
4. **芯片级硬件遥测**：对接 Linux sysfs / debugfs，采集真实的 CPU、内存、磁盘以及 **NPU 负载 (`/sys/kernel/debug/rknpu/load`)** 与 **SoC 温度 (`/sys/class/thermal/`)**；
5. **硬件级看门狗与自愈**：实现针对 MPP 硬件解码器挂死（3s 超时）或流异常的 Session 重建与 IDR 重同步机制；
6. **编译与交叉编译工程化**：完善 CMake 配置，支持原生编译与基于 Docker 的 `aarch64-linux-gnu` 交叉编译，生成开箱即用的 Rockchip 便携运行包。

---

## 2. 架构与边界 (Architecture & Boundaries)

```text
+-------------------------------------------------------------------------------+
|                             Argus Engine Core                                 |
|   +-------------------+   +----------------------+   +--------------------+   |
|   |   CameraTask      |   |    AlgoManager       |   | TelemetryCollector |   |
|   +---------+---------+   +----------+-----------+   +---------+----------+   |
+-------------|------------------------|-------------------------|--------------+
              |                        | (C ABI av_image_ops)    |
              v                        v                         v
+-------------------------------------------------------------------------------+
|                      Rockchip Platform Adapter (platform_rockchip)            |
|                                                                               |
|  +---------------------+   +---------------------+   +---------------------+  |
|  |   RockchipDecoder   |   |   RockchipImageOps  |   |  RockchipTelemetry  |  |
|  |     (MPP H264/H265) |   |    (RGA 2.0 / im2d) |   | (NPU/Temp/Sysfs)    |  |
|  +----------+----------+   +----------+----------+   +----------+----------+  |
|             |                         |                         |             |
|             v                         v                         v             |
|     [ librockchip_mpp.so ]      [ librga.so ]          [ Linux Kernel/sysfs ] |
|     (Hardware VPU/Decoder)      (Hardware 2D RGA)      (/sys/kernel/debug/...) |
+-------------------------------------------------------------------------------+
```

### 规范与抽象隔离要求：
1. **公共头纯洁性**：`include/argus/platform/` 与 SDK ABI 头中禁止出现 MPP (`rk_mpi.h`, `mpp_buffer.h`) 或 RGA (`rga.h`, `im2d.h`) 专有头文件和数据类型，私有结构必须封装在 `.cpp` 或 internal 目录中；
2. **所有权契约**：MPP 解码产出的内存缓冲区由 `RockchipDecoder` 封装为 `av_frame_desc`，其 `opaque` 存储私有 `MppBuffer` 或 `dma-buf` 句柄，并通过 `av_frame_ops`（retain/release）严格控制生命周期；
3. **降级与容错**：当 RGA 硬件不支持某些极端尺寸或不可用时，必须支持安全降级至 NEON/CPU 软件路径，且降级行为在能力档案（`PlatformProfile`）中可被观测。

---

## 3. 功能需求 (Functional Requirements)

### FR-1: 硬件视频解码器 (`RockchipDecoder` via MPP)
- [ ] 支持 H.264 (AVC) 与 H.265 (HEVC) 码流硬解；
- [ ] 封装 MPP `MppCtx` 和 `MppApi`，按 Access Unit 送入 Annex-B / NAL 编码包（`send_packet`）；
- [ ] 解码输出 `AV_PIX_NV12` (YUV420SP) 格式，正确解析 width, height, stride（对齐要求通常为 16 或 64 字节对齐）；
- [ ] 支持丢弃前导非关键帧，严格校验 SPS/PPS/VPS 及 IDR 关键帧，防止花屏；
- [ ] 支持 `flush()` 清空内部解码队列与缓存，支持 `reset()` 彻底重建 MPP 上下文。

### FR-2: 硬件 2D 图像加速与色彩转换 (`RockchipImageOps` via RGA)
- [ ] **色彩空间转换**：通过 RGA `imcvtcolor` 实现硬件加速的 NV12 转 RGB24 / BGRA；
- [ ] **Letterbox 与缩放**：通过 RGA `imcrop` / `imresize` / `imcopy` 实现带黑边填充（Pad）的等比例缩放；
- [ ] **ROI 区域裁剪与提取**：支持根据归一化坐标 `av_rect` 从 1080p/4K 底图硬件裁剪目标区域；
- [ ] **C ABI 函数表导出**：导出纯 C 的 `av_image_ops` 接口供算法包动态库直接调用硬件加速；
- [ ] **CPU/NEON Fallback**：若输入尺寸不满足 RGA 硬件对齐限制（如奇数像素或极小尺寸），自动回退至 CPU/NEON 软件算法，避免硬件报错。

### FR-3: 抓拍与 JPEG 编码
- [ ] 实现 `IImageProcessor::encode_jpeg` / `encode_thumbnail_jpeg`，支持对 `av_frame_desc` 或指定 ROI 区域进行高质量 JPEG 编码（抓拍图保存与预览缩略图生成）；
- [ ] 优先对接 MPP 硬件 JPEG 编码器（`MPP_VIDEO_CodingMJPEG`），不可用时回退 libjpeg-turbo；
- [ ] 注：JPEG 编码是 `IImageProcessor` 的成员而非独立接口，实现落在 `RockchipImageOps` 内部私有类，不产生独立适配器组件。

### FR-4: Rockchip 芯片与系统遥测 (`RockchipTelemetry`)
- [ ] **基础指标**：读取 `/proc/stat`, `/proc/meminfo`, `/proc/uptime`, `statvfs` 采集 CPU、内存、系统运行时间与磁盘水位；
- [ ] **NPU 负载**：读取 Rockchip NPU 驱动节点 `/sys/kernel/debug/rknpu/load`（支持单核/多核 RK3576 2-Core / RK3588 3-Core NPU 利用率解析）；
- [ ] **芯片温度**：遍历 `/sys/class/thermal/thermal_zone*/temp` 采集 SoC/NPU 核心温度（摄氏度）。

### FR-5: 平台档案与配置 (`PlatformProfile`)
- [ ] `platform_id` 声明为 `rk3576-linux` / `rk3588-linux`；
- [ ] `platform_tag` 标识为 `0x524B4E4E` ("RKNN") 或平台专属四字节 Tag；
- [ ] `hardware_decode`, `vector_image_ops`, `telemetry_metrics` 状态明确标记为 `AVAILABLE`。

---

## 4. 非功能性需求 (Non-Functional Requirements)

1. **性能与延迟**：
   - 1080p H.264/H.265 解码单帧耗时 `<= 5ms`；
   - 1080p NV12 -> 640x640 RGB Letterbox RGA 硬件耗时 `<= 3ms`；
   - 解码与图像预处理零拷贝，内存带宽消耗低于纯 CPU 方案 70% 以上。
2. **多路并发与稳定性**：
   - 支持至少 4 ~ 8 路 1080p@25fps 视频流稳定并发硬解与推理，7x24 小时无内存泄漏与死锁；
   - 面对 RTSP 丢包、花屏、网络断开等异常，看门狗 3 秒触发解码器自愈，不影响其他正常通道。
3. **工程构建与兼容性**：
   - CMake 支持通过 `-DPLATFORM_TARGET=rockchip` 开启编译；
   - Docker 交叉编译环境自动化拉取并配置 `librknnrt.so`、`librockchip_mpp.so`、`librga.so`；
   - 输出统一的便携部署包，支持一键在开发板解压运行。

---

## 5. 验收标准 (Acceptance Criteria)

> 分为「本任务门禁」与「子任务硬件验收」两层。开发机为 macOS/arm64，MPP/RGA 运行时正确性无法本地验证，
> 凡需真实 RK 板卡的验收项统一归入子任务 `09-02-engine-zlm-aarch64-e2e`，本任务不以其为完成条件。

### 5.1 本任务门禁（必须全部通过才可收尾）

- [ ] **AC-1 (构建与测试)**：`PLATFORM_TARGET=rockchip` 可以在 aarch64 交叉编译环境下无警告（`-Werror`）编译通过。
- [ ] **AC-2 (ABI 与边界纯洁性)**：`bash engine/scripts/check-boundary.sh` 与 `make -C engine lint` 通过；
      MPP/RGA 私有头与类型不出现在 `engine/include/argus/platform/` 与 SDK ABI 头中；SDK ABI 头零改动。
- [ ] **AC-3 (纯逻辑单测)**：以下逻辑有 GTest 覆盖且不依赖真实硬件与真实 sleep：
      NPU load 文本解析（含单核/多核/畸形/空文件/节点缺失）、thermal zone 选择、`/proc/stat` 与
      `/proc/meminfo` 解析、RGA 对齐约束判定与 fallback 触发条件、letterbox 的 scale/pad_w/pad_h 数值计算。
- [ ] **AC-4 (遥测不可用语义)**：节点缺失或无权限时 `accelerator_usage_percent` / `temperature_celsius`
      保持 NaN，`telemetry_metrics` 标记为 `DEGRADED` 且 `reason` 含缺失节点路径，不伪造数据。
- [ ] **AC-5 (打包产物)**：`bash deploy/scripts/build-rknn-bundle.sh` 产出的便携包内包含
      `argus-engine` 与 `librknnrt.so` / `librockchip_mpp.so` / `librga.so`，`readelf -d` 无未满足的动态依赖。

### 5.2 硬件验收（移交子任务 `09-02-engine-zlm-aarch64-e2e`）

- [ ] **HW-1 (MPP 硬件解码)**：1080p H.264/H.265 测试码流产出合法 `AV_PIX_NV12` 帧，像素与时间戳正确无花屏。
- [ ] **HW-2 (RGA 精度)**：`c_image_ops` 的 convert/pad 输出 640x640 RGB，与 CPU 参考实现 PSNR >= 38dB。
- [ ] **HW-3 (内存安全)**：设备上 ASan/Valgrind 下 retain/release 严格配对，无泄漏与野指针。
- [ ] **HW-4 (真实遥测读数)**：RK3576/RK3588 上正确读出 NPU 利用率与芯片温度。
- [ ] **HW-5 (端到端)**：加载 `algo-packages/rknn/rk3576/yolov8n`，单路 RTSP 实时分析稳定 >= 25fps。
      **前置依赖**：aarch64 下 ZLMediaKit 可用（当前 `main.cpp:206` 非 macOS 一律 mock backend）。


---

## 6. 范围外说明 (Out of Scope)

- 本任务聚焦在 Engine 的 Platform 适配层（MPP 解码、RGA 图像处理、JPEG 编码、Linux 遥测）；
- **不修改 SDK C ABI**（`sdk/include/argus/*.h` 零改动），DMA-BUF 走既有 `AV_MEM_PLATFORM_SURFACE` + `AV_OPAQUE_DMABUF`；
- **不负责 aarch64 下的 ZLMediaKit 交叉编译与媒体后端切换**，该部分与端到端硬件验收一并归子任务
  `09-02-engine-zlm-aarch64-e2e`；本任务不改动 `engine/CMakeLists.txt` 的 `ARGUS_USE_ZLM` 逻辑与
  `main.cpp` 的媒体后端选择分支；
- 不修改已经完成的 `algo-packages/rknn/rk3576/yolov8n` 模型权重和推理结构；
- 不涉及前端 UI 界面的重构。
