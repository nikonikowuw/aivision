# 跨码流主子流协同抓拍与高精识别

## Goal

在 Engine Core 中提供可独立测试的主/子双码流协同抓拍基础能力：子码流用于连续分析，主码流只保留最近一段有界高清帧；分析侧提交带时间戳和归一化 ROI 的抓拍请求后，核心模块按时间容差选择主流帧，完成坐标映射、Margin Padding 和 ROI JPEG 编码所需的数据输出。

本阶段目标是建立稳定的 Engine Core 契约和基础流水线，为后续接入具体人脸/车牌算法、高精识别和告警业务提供基础，不承诺本阶段完成端到端业务闭环。

## Confirmed Facts

- 当前 `IMediaBackend::create_source(source_id)` 可以创建多个独立媒体源，`IMediaSource::start` 接收单个 RTSP URL；双流编排由 Core 持有两个 source，具体 ZLMediaKit 类型不进入 Core。
- `EncodedPacket` 已提供微秒级 `pts_us`、`dts_us`、关键帧标志和可拥有的 packet 副本能力。
- `av_frame_desc` 已携带帧的时间戳和分辨率信息；平台 `IImageProcessor::encode_jpeg` 已支持 `av_rect` ROI。
- 当前工作区对 `CameraTask` 的未提交修改属于 `09-01-on-demand-decoding`，本任务不覆盖或回退这些改动。

## Requirements

- R1. 增加双流配置模型，至少包含主流 URL、子流 URL、主流 RingBuffer 容量/保留时长和 PTS 匹配容差；子流 URL 为空时保留单流兼容路径。
- R2. Core 为主流和子流分别创建、启动、停止媒体源与 decoder；子流输出可交给既有分析分发路径，主流输出进入有界 RingBuffer，不自动送入分析实例。
- R3. RingBuffer 必须有明确上限，按 PTS/插入顺序淘汰最旧帧；帧引用和平台 opaque/frame token 生命周期在淘汰、停止和析构时正确释放，禁止阻塞媒体回调线程。
- R4. 提供时间轴匹配：给定子流抓拍 PTS，在主流缓冲中选择绝对时间差最小且不超过容差的帧；无候选时返回可区分的未命中结果，不使用超窗帧。
- R5. 提供归一化 ROI 到主流像素 ROI 的映射，支持不同主/子分辨率，执行边界裁剪和可配置 Margin Padding；非法 NaN、越界、反向或空 ROI 返回明确错误。
- R6. 提供抓拍结果契约，至少包含匹配主流帧的 PTS、映射后的 ROI 和编码输入引用；平台 JPEG 原语保持在 Core/已有图片管理边界内，不新增算法包图片文件所有权。
- R7. 明确并发契约：媒体回调可并发到达，RingBuffer 读写线程安全；停止过程先阻止新回调，再停止 source/decoder，最后释放缓冲帧。
- R8. 使用 fake clock、Mock source 和 Mock decoder 编写 C++ 单元测试，覆盖时间匹配、分辨率映射、Padding、FIFO 淘汰、空缓冲、超窗、并发停止和帧生命周期；支持 ASan 验证。

## Acceptance Criteria

- [ ] AC1. 双流配置可创建两个独立媒体源；双流启动/停止和单流兼容路径均有测试。
- [ ] AC2. 子流帧可被分析路径消费，主流帧不进入分析分发，仅进入有界缓冲；缓冲容量和保留策略不会无限增长。
- [ ] AC3. 主 4K + 子 720P、主 1080P + 子 D1 等组合下，归一化 ROI 映射无比例偏移；Padding 后严格限制在边界内。
- [ ] AC4. 时间差在容差内选择最近主流帧，超窗、空缓冲和时间戳异常均返回明确结果。
- [ ] AC5. 同一抓拍请求最多产生一个匹配结果；本阶段不实现轨迹去重，但结果契约保留请求/目标标识。
- [ ] AC6. 单元测试、ASan 和 lint 通过；停止、淘汰和析构路径无泄漏、死锁或 use-after-free。

## Out of Scope

- ArcFace、LPRNet、SCRFD、YOLO、ByteTrack 等具体算法包和高精识别调用。
- 告警去重、告警落库、全景/特写证据链业务字段和管理端展示。
- IPC HTTP/ONVIF Snapshot 兜底。
- 云台控制、跨设备时钟同步、网络层 RTSP 厂商兼容策略优化。
- 本阶段不要求主流完全不解码；允许按关键帧或低频解码进入 RingBuffer，具体平台优化由后续任务决定。

## Technical Notes

- Core 内部统一使用 `pts_us`；匹配默认容差建议 100 ms，配置值必须为非负且有合理上限。
- RingBuffer 保存平台帧的 RAII 引用或 FramePool token，而非裸指针；媒体回调仅转移所有权或快速入队。
- ROI 映射公式为 `floor(normalized * main_dimension)`，Padding 在主流像素坐标执行，最终矩形使用半开区间并转换为现有 `av_rect` 契约。
- 主/子流 PTS 可能有固定偏差；本阶段支持显式 offset 配置或为匹配器预留 offset，不实现在线漂移估计。
