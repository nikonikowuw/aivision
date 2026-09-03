# face_recognition SDK Engine Go 跨层分析

## Goal

以当前工作区版本为准，验证 `face_recognition` 从 SDK/C ABI、Engine 到 Go 后端记录与底库链路的数据契约、帧关联、并发边界和资源生命周期。

## Requirements

- 绘制 `frame_desc -> instance_process -> AV_RESULT_RECOGNITION -> Engine parser -> gallery matching -> ReportFaceObservation/FaceCapture -> Go repository` 数据流。
- 核对 frame_id、pts_ns、instance_run_id、track_id、event_id、bbox、landmarks、embedding 512 维 little-endian、similarity、gallery revision、图片 key 的跨层一致性。
- 检查 frame retain/release、image request、callback 生命周期、flush/destroy、底库原子替换、报告重试和 Go 幂等落库。
- 执行已有 SDK/Engine/Go 相关测试；对关键缺口补充最小契约测试，不改变公共 ABI 和业务模型。
- 将结论按 L1/L2/L3/L4 标注，验证结果使用 PASS/FAIL/BLOCKED/NOT APPLICABLE。

## Acceptance Criteria

- [ ] 完成算法包、SDK、Engine、Go 后端四层数据流图和所有权表。
- [ ] 已有相关测试执行结果和命令完整记录。
- [ ] 关键字段和错误路径均有代码锚点，识别丢失、重复、迟到覆盖和图片孤儿风险有明确结论。
- [ ] 形成最小跨层契约测试清单；无法运行项说明环境阻塞原因。
- [ ] 不扩展到 Vue 前端和正式识别精度评测。

## Out of Scope

- 不修改 SDK 公共 ABI、Engine 生产调度和 Go 数据模型。
- 不进行前端 UI 审计。
- 不进行 RKNN/MPP/RGA/DMA-BUF 适配。
