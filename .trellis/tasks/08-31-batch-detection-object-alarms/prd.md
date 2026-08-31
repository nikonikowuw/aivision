# Batch detection callbacks with per-object alarms

## Goal

将目标检测结果从“每个目标一次 ABI 回调”调整为“每帧一次 ABI 批次回调”，同时保持目标级告警语义：一帧检测到 N 个可告警目标，Engine 生成 N 条独立 `AlarmEvent`。减少回调、JSON 序列化和同帧重复抓拍，同时修复 standalone runner 只保留最后一次回调的问题。

## Requirements

- 算法在完成预处理、Core ML 推理、后处理、Tracker、规则和目标级冷却后，一次 `instance_process` 最多回调一次正常 `AV_RESULT_ALARM`。
- 正常结果 JSON 的 `objects` 数组承载同一帧所有通过告警条件的目标；无目标时不产生正常告警回调。
- 每个目标仍然对应一条独立业务告警和独立全局 `event_id`；不得使用同一个批次 ID 作为多个告警的幂等键。
- Engine 必须在同步结果回调内复制并校验批次数据，再把每个目标拆为只含一个对象的 `AlarmEvent`。
- Engine 对同一批次的全景抓拍最多编码一次，并让拆出的告警共享同一图片引用；不得为同一帧的每个目标重复编码相同全景图。
- 目标级 5 秒冷却继续按 `track_id` 工作；批次只包含当前允许触发告警的目标。
- 事件 ID 必须在 `instance_run_id` 内唯一，并且同一目标在不同批次触发时不能仅依赖 `track_id`。
- standalone runner 必须能打印完整批次结果并绘制所有目标，`TARGET_CLASSES` 只影响 runner 展示过滤，不改变算法检测集合。
- 同步更新 SDK/Engine 结果契约、相关中文注释、测试和规范文档；不破坏 ABI 结构布局。

## Acceptance Criteria

- [ ] macOS YOLOv8n 对固定测试图只触发一次正常 ABI 回调，JSON 的 `objects` 包含 3 个 `person` 和 1 个 `bus`。
- [ ] Engine 对包含 4 个目标的一个批次向上游报告 4 个独立 `AlarmEvent`，四个事件 ID 唯一，每个事件只有一个对象。
- [ ] 同一批次的 4 个事件共享一个抓拍图片引用，图片编码调用最多一次；图片失败、队列丢弃和停止路径不泄漏帧引用。
- [ ] 重复处理相同批次/事件时仍按 event ID 幂等去重，不会生成重复业务记录。
- [ ] 目标冷却、空批次、规则过滤和多实例并发行为保持正确。
- [ ] standalone runner 输出完整检测结果，结果图片包含所有配置允许展示的目标。
- [ ] 包、Engine 单测/契约测试、静态检查和相关 sanitizer 通过。

## Notes

- 现有 `objects[]` 是目标数组，不是事件数组；本改造明确把 ABI 回调定义为检测批次边界，把 `AlarmEvent` 定义为业务事件边界。
- 现有 `av_algo_result` 结构和 C ABI 数值保持不变；批次标识与目标事件 ID 的职责在结果协议和 Engine 中明确区分。
- 当前旧规范和归档 PRD 中关于“每个 `AV_RESULT_ALARM` 即一条完整单目标告警”的描述需要同步修订，避免实现与文档继续分叉。
