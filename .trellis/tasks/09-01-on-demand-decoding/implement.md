# 实现计划

## 实施步骤

1. **CameraTask 统一消费者引用模型与状态定义**：
   - 在 `engine/include/argus/core/camera_task.hpp` 中新增 `preview_leases_`、`acquire_preview_lease()`、`release_preview_lease()`、`active_consumers_count()`、`active_instances_count()`、`is_probing_mode()` 以及探测保活时间戳与丢包计数；
   - 在 `CameraTask::start()` 初始化与 `stop()` 资源重置处同步重置新增状态。

2. **decode_loop 实现按需降频探测与平滑同步**：
   - 在 `engine/src/core/task/camera_task.cpp` 的 `decode_loop` 中：
     - 判定 `active_consumers_count()`；
     - 当活跃消费者为 0 时，启用 1 FPS IDR 门控，跳过普通 P/B 帧；
     - 在消费者计数从 0 跃升为 >0 时重置关键帧同步标志，保证全速切回时首帧必为 IDR 关键帧。

3. **可观测性输出与指标上报**：
   - 在 `log_debug_metrics` 中加入 `probing_mode`、`active_consumers`、`preview_leases` 与 `probing_drops` 统计输出。

4. **单元测试与全量回归**：
   - 在 `engine/tests/unit/test_camera_task.cpp` 中编写针对无消费者探测（`ZeroConsumerProbingModeThrottlesTo1FPS`）、算法实例与预览租约增删切换（`PreviewLeaseAndInstanceTransitionResyncIDR`）的单元测试用例；
   - 运行质量门禁：
     - `make -C engine configure`：通过
     - `make -C engine build`：通过
     - `make -C engine test`：通过（77/77 tests passed）
     - `make -C engine lint`：通过
     - `make -C engine asan`：通过（57/57 tests passed，0 leaks）
     - `cd argus && go test ./... && go vet ./...`：通过
     - `bash algo-packages/scripts/sync-sdk.sh && bash algo-packages/scripts/check-consistency.sh`：通过

## 验证结论

- 0 消费者场景下，CameraTask 正常进入 1 FPS 探测模式，P/B 帧被门控拦截丢弃，仅放行 1 帧/秒的 IDR 关键帧，看门狗保活正常运作。
- 动态添加算法实例或获取预览租约时，CameraTask 自动退出探测模式，重置 IDR 状态机并在收到下一完整 SPS/PPS + IDR 帧后无缝切入 25 FPS 全速解码分发，无花屏无撕裂。

## 开始实现前检查

- [x] 用户确认 PRD 范围、1 FPS 保活策略以及实时预览统一纳入消费者引用计数。
- [x] 已确认媒体接入层、解码线程、实例扇出与预览租约接入点。
- [x] 设计保证无消费者时 1 FPS 维持 `last_frame_wall_time_ns` 更新与看门狗正常工作。
- [x] 设计保证任何消费者接入切回全速时等待 IDR 关键帧以杜绝花屏。
