# CameraTask 按需降频解码与边缘算力优化

## Goal

当摄像头任务（`CameraTask`）处于已启动状态但其关联的消费者引用数（算法实例与实时预览等）为 0 时，`CameraTask` 自动从全速解码（25 FPS）降级为“低频探测保活模式”（Probing Standby Mode，固定 1 FPS 抽解 IDR 关键帧），以消除边缘端 95% 以上的空转硬解/软解计算负载、内存总线带宽占用与发热；当有算法实例启用或有实时视频预览接入时，在首个有效 IDR 关键帧处平滑无缝恢复全速流水线。

## Background

当前媒体链路为：RTSP 编码包 -> `encoded_queue_` -> `decode_loop` 全速解码 -> `FramePool` -> 扇出给算法实例。
当任务已建立但没有活跃消费者（算法实例未开启且无预览查看）时，`CameraTask` 仍然每秒执行 25 次硬件/软件解码，随后因消费者列表为空直接归还 `FramePool`。在多路视频流并发场景下，这导致 VPU/CPU 算力与内存带宽被严重浪费，在低功耗边缘芯片（如 RK3588/RK3576）或软解回退场景下尤为明显。

## Core Decisions & Invariants

1. **固定 1 FPS 关键帧保活**：在无活跃消费者时，丢弃非关键帧（P/B 帧），对 IDR 关键帧按 1 秒间隔进行频率限制，既消除 95% 以上无效解码，又保证 `last_frame_wall_time_ns` 每秒平稳刷新和画面连通性监控。
2. **消费者引用计数统一判定（Subscriber Ref-Count）**：
   - 视频消费者的定义包括：**活跃算法实例（Active Algorithm Instances）** 和 **实时视频预览/推流订阅（Live Preview Consumers）**；
   - 只要 `total_active_consumers > 0`，`CameraTask` 必须保持 **全速 25 FPS**；
   - 仅当 `active_instances == 0 && live_preview_subscribers == 0` 时，才触发降频退避至 1 FPS 探测。
3. **分层正交性**：
   - 媒体管道层的降频流控作用于流自身通道（无论当前配置为主码流还是子码流）；
   - 上层算法的分辨率诉求（如普通目标检测使用子码流、人脸识别使用 Detector-Cropper 分级抓拍与局部高精抠图流水线）与底层无实例降频机制正交解耦。
4. **平滑切回防花屏（Smooth Resync）**：从 Probing 切回 Active 时，必须在集齐参数集（SPS/PPS）并在下一个有效 IDR 关键帧到达后开始连续全速送解，确保下游消费者接收的首帧数据完整无花屏。

## Requirements

- R1. **消费者引用计数模型（Subscriber Model）**：
  - `CameraTask` 维护统一的消费者引用计数（`active_instances_count` 与 `preview_subscriber_count`）；
  - 支持实例增删（`add_instance`/`remove_instance`）以及预览订阅监听（`acquire_preview_lease`/`release_preview_lease`）。
- R2. **固定 1 FPS 降频探测保活模式（Probing Standby Mode）**：
  - 当 `total_active_consumers == 0` 时，自动进入探测保活模式；
  - 维持 RTSP 网络连接与数据包接收，丢弃非关键帧（P/B 帧）不送入硬件/软件解码器；
  - 对关键帧（IDR 帧）按单调时钟进行频率限制（固定 1 FPS，即每秒最多解码 1 个关键帧），维持 `last_frame_wall_time_ns` 正常每秒刷新、维持画面缩略图与看门狗心跳。
- R3. **全速分析/预览模式（Active Full-Speed Mode）**：
  - 当 `total_active_consumers > 0`（有算法实例运行 OR 有实时预览请求）时，恢复满帧率（25 FPS）全速送解并分发。
- R4. **双向平滑切换（Smooth Transition）**：
  - `Probing -> Active`：当消费者从 0 增加为 > 0 时，必须确保从下一组参数集（SPS/PPS）和 IDR 关键帧开始连续送解，防止因参考帧缺失导致花屏或解码器报错；
  - `Active -> Probing`：当最后一个活跃消费者退出（0 实例 & 0 预览）时，立即在下一帧停止全速送解，退避至 1 FPS 探测。
- R5. **看门狗自适应（Watchdog Adaptive Check）**：
  - 看门狗（Watchdog）在探测模式下自适应调整解码卡顿判定阈值（允许 1 FPS 的低频帧间隔），不误报断流重连或 `CameraState::ERROR`。
- R6. **可观测性指标（Metrics）**：
  - `log_debug_metrics` 输出增加当前工作模式（`PROBING` vs `ACTIVE`）、消费者计数（`instances_count`, `preview_count`）以及探测过滤丢弃包计数 `probing_gate_drops_`。

## Acceptance Criteria

- [ ] AC1. 0 活跃实例且 0 预览订阅时，网络拉流正常，但送入解码器的包数与解码帧数严格被限制在约 1 FPS，VPU/CPU 占用大幅下降。
- [ ] AC2. 0 消费者时，`last_frame_wall_time_ns` 仍持续每秒更新，看门狗不发生误判重连或状态异常。
- [ ] AC3. 动态添加/启用算法实例或触发预览请求时，解码器在收到下一个有效 IDR 帧后平滑无缝恢复全速解码，消费者正常收到完整无花屏视频。
- [ ] AC4. 动态移除/停用所有算法实例且无预览时，解码流水线立即回退到 1 FPS 探测保活模式。
- [ ] AC5. 编写单元测试（`test_camera_task`），覆盖 0 消费者低频探测、算法实例切换、预览租约切换、看门狗判定等场景。
- [ ] AC6. `make -C engine test`、`make -C engine asan`、`make -C engine lint` 全量通过。

## Out of Scope

- RTSP 网络连接级断开（保持 TCP/UDP 媒体链路，以确保秒级唤醒和网络连通性监控）。
- 前后端 gRPC / HTTP API 契约变更（完全在 C++ 媒体层自适应处理，对上层控制面透明）。

## Technical Notes

- 探测模式下的关键帧抽样必须依赖单调时钟（`std::chrono::steady_clock`）。
- H.264/H.265 的 SPS/PPS 参数集需要保持缓存，以便在切回全速模式时随时具备初始化参数。
