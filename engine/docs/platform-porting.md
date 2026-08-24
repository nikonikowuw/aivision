# 平台适配开发指南 (Platform Porting Guide)

## 1. 适配接口体系

运行平台适配层需要实现 `engine/include/aivision/platform/platform_api.hpp` 中定义的接口集合：

1. **`IPlatformAdapter`**：平台能力入口，声明 `PlatformProfile`。
2. **`IDecoder`**：硬件解码器会话（如 macOS 的 VideoToolbox，RKNN 平台的 MPP）。
3. **`IImageProcessor`**：硬件加速图像处理原语与 JPEG 编码。
4. **`ITelemetry`**：系统负载、内存、算力芯片指标采集。

## 2. 通用帧描述符与内存所有权

- 跨平台帧描述符统一使用 152 字节固定布局的 `av_frame_desc`。
- 平台原生内存句柄放入 `opaque`，并通过 `opaque_kind` 标识类型（如 `AV_OPAQUE_CVPIXELBUFFER` 或 `AV_OPAQUE_DMA_BUF_FD`）。
- 帧生命周期严格由 `av_frame_ops`（`retain_frame` / `release_frame`）管理，算法层只能持有只读引用，不得释放原生平台句柄。

## 3. 看门狗与自愈契约

- 针对硬件驱动死锁，实现双层看门狗保护：
  - 媒体拉流 5 秒无数据超时；
  - 硬件解码队列有包但连续 3 秒未吐帧超时。
- 触发超时后，平台层必须提供安全的 Reset 机制，销毁旧 Decoder 会话原地重建，并请求上游补发 IDR 关键帧。
