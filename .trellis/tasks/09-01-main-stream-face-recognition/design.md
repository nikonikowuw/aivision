# 技术设计

## 分阶段边界

```text
Phase 1: macOS face_recognition 算法基线
    真实 CVPixelBuffer -> Core ML -> 同帧结果

Phase 2: Engine 主流接入
    CameraTask 主流 -> VideoToolbox -> target_fps -> C ABI instance_process
```

本任务只包含上述两个阶段。RKNN/MPP/RGA/DMA-BUF 和 RK3568 板端部署另建后续任务，不作为本任务的实现项或验收项。

## Phase 1：macOS 算法包

当前 `algo-packages/macos/arm64/face_recognition` 已包含 SCRFD、YOLO、GLINTR、tracker、质量评分、五点对齐和 standalone runner。先将默认生产路径收敛为 SCRFD + tracker + quality + alignment + GLINTR；YOLO 通过明确配置可选，不作为人脸识别必需模型。

`Preprocessor::process_frame` 生成检测用 Letterbox 图和同一输入帧的原图 RGB。SCRFD 输出经过反 Letterbox 变换回原图坐标，landmarks 只允许用于同一原图的对齐。结果增加/确认 `frame_id` 与 `pts_ns`，以便 Engine 验证同帧关联。

## Phase 2：Engine

Engine 使用已有 `AlgorithmInstance.target_fps` 负责抽帧，算法包不再二次抽帧。主流解码帧经平台图像原语产生检测输入；检测结果反变换到原始主流帧，高清 ROI 和证据请求只能引用该帧。识别调度按固定队列容量、全局并发上限、质量门槛和 `track_id` 次数/cooldown 限制。

## 后续迁移边界

未来 RKNN 任务应保持本任务的结果、后处理和生命周期契约，另行替换 `model_inference.mm`、平台预处理和 buffer 管理。该迁移计划不在本任务内实现，也不在本任务验收中承诺任何 RK3568 路数、FPS 或内存峰值。

## 内存边界

本任务在 macOS 上验证不持有多帧 4K 解码 RingBuffer，并验证 Engine 的有界队列和帧释放；不做 RK3568 CMA、DMA-BUF、MPP Surface 或 RKNN Runtime 的实测。

## 错误与回滚

macOS 模型加载、真实帧格式、landmark、对齐、embedding 或 ABI 失败必须在算法包内返回明确错误。Engine 资源不足、frame retain 失败或识别队列满时拒绝/丢弃当前请求，不阻塞媒体回调。保留原有单流 CameraTask 路径作为回滚路径。
