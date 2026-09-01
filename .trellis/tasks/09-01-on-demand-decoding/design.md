# CameraTask 按需降频解码技术设计

## 1. 架构定位与数据流

`CameraTask` 位于 C++ Engine 媒体接入与解码层。本优化属于 Engine 内部纯调度与流控优化，对 Go 控制面（gRPC DesiredState / ReportStatus）及 SDK C ABI 零破坏、全透明。

```text
[RTSP Network Packet]
        │
        ▼
   [encoded_queue_]
        │
        ▼
  [decode_loop] ────► 计算 total_consumers = active_instances + preview_leases
        │
        ├── total_consumers == 0 (PROBING 模式)
        │      ├─ 非关键帧 (P/B 帧) ────────► 直接丢弃 (probing_gate_drops_++)
        │      └─ 关键帧 (IDR 帧)
        │            ├─ 距离上次探测 < 1s ───► 丢弃 (probing_gate_drops_++)
        │            └─ 距离上次探测 ≥ 1s ───► 送解 1 帧 ──► 刷新 last_frame_wall_time_ns ──► 释放 FramePool
        │
        └── total_consumers > 0 (ACTIVE 模式)
               └─ 全速送解 (25 FPS) ──────────► 扇出给各 AlgorithmInstance & 预览管道 ──► 推理
```

## 2. 状态定义与变量设计

在 `CameraTask` 类中新增/扩展以下状态：

```cpp
// 消费者引用计数
std::atomic<uint32_t> preview_leases_{0};

// 探测模式抽帧控制（单调时钟）
std::chrono::steady_clock::time_point last_probe_decode_time_{};

// 探测模式丢包指标计数
std::atomic<uint64_t> probing_gate_drops_{0};

// 当前是否处于探测保活模式快照
std::atomic<bool> is_probing_mode_{false};
```

并提供预览租约管理接口：

```cpp
void acquire_preview_lease();
void release_preview_lease();
[[nodiscard]] uint32_t active_consumers_count() const;
```

## 3. 流水线与模式切换规则

### 3.1 PROBING 探测保活模式

- **触发条件**：`active_consumers_count() == 0`（无算法实例且无预览租约）。
- **执行动作**：
  1. `is_param_set`（SPS/PPS/VPS）包：正常解析并记录参数集标志，送入解码器以建立/维持解码上下文；
  2. 非参数集包：
     - 若 `!packet.is_keyframe`：直接跳过，`probing_gate_drops_++`，不调用 `decoder_->send_packet`；
     - 若 `packet.is_keyframe`：计算 `now - last_probe_decode_time_`，若不足 1000ms 则跳过；若达到 1000ms 则送入解码器输出 1 帧并更新 `last_frame_wall_time_ns_`，随后归还 `FramePool`，更新 `last_probe_decode_time_ = now`。

### 3.2 ACTIVE 全速分析/预览模式

- **触发条件**：`active_consumers_count() > 0`。
- **执行动作**：维持原有全速解码逻辑，满帧率送入解码器并分发给所有绑定的消费者。

### 3.3 模式切换防花屏（Smooth Resync）

- 当消费者计数从 0 增加为 1 时（无论是添加首个算法实例还是申请首个预览租约）：
  - 将 `saw_idr_keyframe_.store(false, std::memory_order_release)`；
  - `decode_loop` 自动在等待集齐参数集并遇到下一个有效 IDR 帧时才开始全速送解，确保下游消费者接收的第一帧即为完整的独立关键帧画面，彻底杜绝 P 帧先到的花屏与解码器参考帧缺失错误。

## 4. 看门狗（Watchdog）适配

- `last_packet_time_ms_` 继续由 RTSP 网络包回调更新，网络断流判定（> 5000ms）保持不变；
- 解码器在探测模式下每秒仍有 1 帧输出，输出完成后 `decoder_waiting_for_output_` 置为 `false`，解码卡顿判定（> 3000ms）不会被误触发。

## 5. 质量与回归验证

- 单元测试扩展：
  - 构造 `test_camera_task` 场景：在没有消费者时喂入 25 FPS 连续包，断言解码帧数严格限制在 1 FPS 左右；
  - 动态调用 `add_instance` 或 `acquire_preview_lease`，断言解码立即恢复全速；
  - 动态调用 `remove_instance` 与 `release_preview_lease`，断言解码立即回退至 1 FPS；
  - 验证看门狗与最后帧时间戳在整个过程中平稳更新。
