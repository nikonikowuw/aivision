# Technical Design

## 1. Domain Model

- **Detection batch**：一次 `instance_process` 对一帧产生的检测集合，包含 `objects[]` 和批次级关联信息。
- **Target alarm**：一个通过目标级冷却/规则的检测目标，对外持久化为一条 `AlarmEvent`，只包含一个对象。
- **Batch callback**：一次 `AV_RESULT_ALARM` 回调，传递一个检测批次；它不再是业务告警的边界。
- **Shared capture**：同一批次的一张图片，拆出的多个目标告警共享其 `image_id`/路径。

## 2. Result Contract

保留 `av_algo_result` 结构布局和 `AV_RESULT_ALARM` 数值。正常 JSON 仍使用顶层 `event_id`、`alarm_type_id`、`objects[]`，但将顶层 ID 解释为唯一批次 ID；每个 `objects[]` 元素必须有 `track_id`（目标级 fan-out 的身份组成部分）。规范说明：Engine 不把顶层批次 ID直接作为多个告警 ID。

算法生成一个唯一批次 ID。Engine 在收到批次后为每个目标生成唯一目标事件 ID，建议格式为：

```text
<instance_run_id>/<batch_event_id>-<target_sequence>
```

`target_sequence` 是当前回调内从 1 开始的稳定序号；同批次内对象按算法输出顺序处理。目标事件 ID 只在 Engine 内部作为全局事件键使用，批次 ID 仍保留在内部处理上下文/日志中。若后续需要跨重试复现同一目标事件，批次 ID 必须来自稳定的算法序列，而不是仅使用时间戳。

不修改公共 C ABI；SDK JSON parser 继续解析一个批次的 `objects[]`。

## 3. Algorithm Package

在 `yolo_instance_process_impl` 中：

1. `run_pipeline` 返回完整目标列表；
2. 遍历目标，仅更新每个 `track_id` 的冷却并收集当前可触发目标；
3. 若收集为空，返回 `AV_OK` 且不回调；
4. 生成一次批次 ID，调用 `serialize_alarm_json` 传入完整目标 vector；
5. 构造一次全景图片请求并调用一次 `on_result`。

这样模型/NMS/Tracker 行为不变，只有结果分发粒度变化。

## 4. Engine Fan-out and Capture

`handle_result` 解析并验证完整批次后：

1. 校验批次 ID、告警类型、目标数和每个 bbox/置信度/track_id；
2. 为每个目标创建只含一个 `DetectedObject` 的 `AlarmEvent`；
3. 为每个目标生成唯一全局事件 ID；在同一批次内拒绝 ID 冲突；
4. 对批次图片请求只 retain 一份 frame token；
5. 建立批次级 pending capture，先编码一次图片，再将同一图片引用复制到多个待上报事件；
6. 保持事件队列有界、去重原子性和停止时 frame token 释放。

图片模型需要新增一个 Engine 内部的共享 capture 对象/批次 pending 结构，不扩展 Go `AlarmEvent` protobuf：最终每条 `AlarmEvent` 仍携带同一个 `image_id` 和 `image_rel_path`。ImageManager catalog 需要允许一个图片引用多个事件；最小实现可增加内部共享保存接口，让一次编码返回一个 `ImageRecord`，再为每个事件建立引用元数据，不能用一次 `save_detection_image(event_id)` 重复编码。

由于当前 `ImageRecord` 只有单个 `event_id`，设计实现应增加共享图片的关联字段或在 Engine/Go 协议中明确图片为可复用引用；清理/孤儿对账必须按 image ID 而非单个事件判断。

## 5. Standalone Runner

runner 回调直接保存一个批次 JSON 和解析后的完整目标数组，不再依赖多回调聚合。打印完整 JSON；绘图继续对完整对象数组应用 `TARGET_CLASSES`。

## 6. Compatibility and Rollout

这是协议语义变更，不改变 C ABI 内存布局。同步更新：

- `.trellis/spec/engine/algo-package-spec.md` 和 `manifest-schema.md`；
- vendored SDK parser/comment；
- Engine result/capture implementation；
- 算法包实现和测试；
- 相关 Engine fixture/test expectations。

旧的单目标多回调算法仍可被 Engine 接收会导致每个回调各自 fan-out；为避免混合语义，Engine 可暂时兼容单对象结果，但本包和新规范只产生批次回调。兼容行为必须不重复拆分一个已含多个对象的批次。

## 7. Risks

- 图片共享会影响 catalog 引用计数、孤儿图片对账和删除时机；实现必须先固定引用所有权。
- 目标 ID 若仅使用 track_id，会在 tracker reset/restart 后冲突；必须包含批次序列。
- 旧 mock 只产生空对象告警，需增加多对象 fixture 验证 fan-out 与共享图片。
- 单帧结果 JSON 上限仍需限制目标数，避免大量对象导致超过 `AV_MAX_RESULT_JSON_BYTES`。
