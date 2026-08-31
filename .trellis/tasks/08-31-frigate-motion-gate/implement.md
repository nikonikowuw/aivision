# 实现计划

## 顺序

1. 定位现有解码帧到实例 worker 的分发与配置入口，确认最小改动点；验证不改变媒体回调、队列和帧所有权契约。
2. 实现独立 `MotionGate` 及配置校验、低分辨率亮度采样、背景差分、mask 和 keepalive 判定；验证纯 C++ 单元测试。
3. 在每个摄像头/实例的 worker 分发路径接入 gate；验证 active、keepalive、skip 三条路径和释放行为。
4. 接入配置 revision 更新与 reset/shutdown；验证尺寸/格式变化、重连和配置更新不会复用过期背景。
5. 接入 telemetry 计数和日志字段；验证统计不会记录帧内容且多实例互不污染。
6. 运行质量门禁并整理结果。

## 测试与命令

- 固定 NV12 fixture + fake monotonic clock：首帧、静止、运动、mask、阈值/面积边界、keepalive 边界、reset、配置错误。
- `make -C engine configure`
- `make -C engine build`
- `make -C engine test`
- `make -C engine lint`
- `make -C engine asan`（环境支持时）
- `make -C engine tsan`（环境支持时）
- `bash algo-packages/scripts/check-consistency.sh`（若 SDK/ABI 有变更时）

## 风险与回滚点

- 现有帧描述符可能只允许平台句柄访问：优先使用已有平台图像读取能力；若读取成本会触发昂贵转换，需暂停并调整设计。
- 运动判断若接入位置错误，可能在解码线程阻塞或破坏队列背压；保留分发路径测试作为回滚依据。
- 配置若跨进程传递，先确认已有 schema/revision 所有者，再决定是否扩展契约。
- gate 默认应保持兼容：未启用时行为与现有持续推理一致。

## 开始实现前检查

- [ ] 用户确认 PRD 范围与默认参数策略。
- [ ] 已读取 engine 平台、调度、错误和质量规范。
- [ ] 已确认当前源码中的实际 worker/配置入口。
- [ ] 设计不将 zone 或对象 mask 引入本 task。
