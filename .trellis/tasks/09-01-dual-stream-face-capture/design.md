# 技术设计

## 1. 边界与模块

新增 Engine Core 的双流协同组件，不把 ZLMediaKit、VideoToolbox 或算法类型放入公共媒体/平台头。建议拆为：

- `DualStreamConfig`：URL、RingBuffer 容量/保留时长、PTS offset 与 tolerance 的值对象和校验。
- `FrameRingBuffer`：保存主流 `av_frame_desc` 的 RAII 引用，提供线程安全 `push`、按 PTS 最近匹配和清空。
- `RoiMapper`：纯函数完成归一化 ROI 校验、主流像素坐标映射、Padding 和边界裁剪。
- `DualStreamCapture`：拥有主/子 `IMediaSource`、decoder 和两个队列/线程；子流帧沿现有实例分发，主流解码帧进入 RingBuffer；接收抓拍请求并返回匹配帧与 ROI。

若现有 `CameraTask` 改造不会影响按需解码任务，则通过最小扩展接入；否则先让组件可独立使用，并在后续任务中合并到 CameraTask。不得覆盖工作区中另一个任务的未提交修改。

## 2. 数据流与生命周期

```text
sub source -> bounded sub queue -> sub decoder -> existing analysis dispatch
main source -> bounded main queue -> main decoder -> FrameRingBuffer
capture request(pts, normalized roi) -> match main frame -> RoiMapper -> CaptureResult
```

媒体回调只 clone/转移需要的 packet 并入有界队列，不解码、不编码、不等待。停止时设置 accepting=false，停止 source 并移除回调，再唤醒并 join worker，最后清理 decoder 和 RingBuffer。

FrameRingBuffer 不复制平台像素。插入前通过 `frame_token` 对应的 `av_frame_ops.retain` 获得引用，淘汰、清空和析构时成对 release；借用的 `av_frame_desc` 不能脱离该引用返回。匹配结果应持有 RAII frame handle，确保调用方编码期间帧仍有效。

## 3. 时间与 ROI 契约

所有内部时间使用微秒。有效匹配时间为 `abs(main_pts_us - (capture_pts_us + pts_offset_us)) <= tolerance_us`，候选取绝对差最小，平局取 PTS 更早者。无候选返回 `NoFrameInTolerance`。

归一化 ROI 要求 finite、坐标范围 `[0,1]`、width/height 大于 0。先映射到主流宽高，再按比例扩展 margin，向下/向上取整后裁剪到 `[0,width) x [0,height)`；结果为空返回错误。主流分辨率从匹配帧读取，因而支持任意主/子组合。

## 4. 兼容性与取舍

- 单流模式继续使用现有 CameraTask 行为。
- `sub_rtsp_url` 为空不创建第二 source。
- 不扩展 C ABI；Core 内部使用 C++ 类型，跨算法边界仍只传既有 `av_frame_desc`。
- 本阶段不在线估计时钟漂移，不承诺原始主流完全零解码。

## 5. 错误与可观测性

配置非法、source 创建/启动失败、decoder 创建失败、无时间匹配、ROI 非法和 frame retain/release 失败必须返回现有 `av_status` 或稳定的 Core 结果枚举，并记录流标识、PTS、缓冲深度和丢弃计数，日志不得包含 URL 密码。

## 6. 回滚

新组件以独立 target/文件加入；若真实媒体后端接入导致回归，可保留组件和 Mock 测试，暂时关闭 CameraTask 双流入口，不改变单流路径。
