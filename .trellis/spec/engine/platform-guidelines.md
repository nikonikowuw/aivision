# 平台适配、媒体与调度规范

> 本规范定义平台能力接口、媒体后端边界、单摄像头共享源、多实例调度、看门狗、资源账本和设备指标。

## 1. Scope / Trigger

新增平台、解码器、媒体后端、图像原语、调度策略、资源档位或监控指标时必须读取本规范。

媒体实现与硬件适配是两个独立边界：ZLMediaKit 实现 `media_api`；VideoToolbox/RKNN 等实现 `platform_api`。两者由 `engine_app` 装配，不能互相泄漏私有类型。

## 2. 平台接口签名

`IPlatform` 暴露六类服务，不包含模型推理接口：

```cpp
class IPlatform {
 public:
  virtual ~IPlatform() = default;
  virtual std::string_view id() const noexcept = 0;
  virtual const PlatformProfile& profile() const noexcept = 0;
  virtual std::unique_ptr<IDecoder> create_decoder() = 0;
  virtual IFrameAllocator& frame_allocator() noexcept = 0;
  virtual IImageProcessor& image_processor() noexcept = 0;
  virtual IImageEncoder& image_encoder() noexcept = 0;
  virtual IResourceProvider& resources() noexcept = 0;
  virtual ITelemetry& telemetry() noexcept = 0;
};
```

| 接口 | 职责 | macOS 实现 |
| --- | --- | --- |
| `IDecoder` | H.264/H.265 access unit -> `FrameHandle` | VideoToolbox |
| `IFrameAllocator` | 平台帧池、token retain/release | CVPixelBuffer pool |
| `IImageProcessor` | crop/resize/convert/pad | vImage/Core Image，必要时 CPU fallback |
| `IImageEncoder` | JPEG bytes | ImageIO |
| `IResourceProvider` | 1000 units 与空闲内存门槛 | Profile + 系统内存 |
| `ITelemetry` | 六项统一指标 | sysctl/公开系统接口；不可用项明确标记 |

算法包直接链接目标推理运行时并拥有模型与推理上下文。`PlatformProfile.inference_runtime` 只描述名称、版本、可用性和兼容约束，不向 Engine core 暴露 `MLModel`/`rknn_context`。

### 2.1 能力档案

```text
PlatformProfile {
  schema_version
  platform_id
  adapter_version
  arch
  os_or_bsp
  media_backend {name, version, capabilities}
  codecs[]
  frame_caps[]
  inference_runtime {name, version, availability}
  image_ops {availability, implementation}
  jpeg {availability, implementation}
  resource {total=1000, allocatable, reserved, source}
  metrics[6] {id, availability, unit, source, reason?}
}
```

`availability` 为 `available|degraded|unavailable`。`degraded/unavailable` 必须有非空 reason；不可用指标没有数值字段，不能伪造为 0。

## 3. 媒体后端契约

`media_api` 只暴露项目自有 `EncodedAccessUnit`、track 元数据和生命周期回调，公共头中不得出现 ZLM 类型。

- 同一物理摄像头只创建一个上游 RTSP session。
- 预览复用同一上游 media source；算法共享一次解码结果。预览可以消费编码 ring，不得建立第二条上游连接。
- ZLM `EventPoller`/track 回调只转移带所有权的编码帧引用到有界队列，不执行解码、磁盘、RPC、推理或等待。
- ZLM commit 与加载的 `config.ini` 必须固定并写入部署 Profile/构建报告。
- track ready、track 替换、source 注销和重连都必须重新建立/移除 delegate，不能假设 track 生命周期等于任务生命周期。

停止顺序固定为：

```text
stop accepting frames
-> remove track delegates on owner poller
-> close upstream/session
-> wake encoded queue
-> join decode worker
-> drain per-instance queues
-> flush/join instance workers
-> destroy decoder and frame pool
```

回调捕获外部对象时使用弱引用；不得捕获可能先析构的裸 `this`。

## 4. 图像原语契约

```c
typedef struct av_image_ops {
  uint32_t size;
  uint32_t api_version;
  void* ctx;
  int (*convert)(void* ctx, const av_frame_desc* src,
                 const av_rect* src_roi, const av_image_view* dst,
                 uint32_t filter);
  int (*pad)(void* ctx, const av_image_view* dst,
             const av_rect* region, const uint8_t value[4]);
  int (*alloc)(void* ctx, uint32_t width, uint32_t height,
               uint32_t pixel_format, av_image_view* out);
  int (*free)(void* ctx, av_image_view* image);
} av_image_ops;
```

- Engine 提供机制；letterbox 比例、模型输入形状、通道排列和归一化策略归算法包。
- `convert` 必须使用帧描述符的色彩矩阵/range；ROI 和目标 buffer 全部做边界验证。
- 硬件路径失败时，只有 Profile 声明了 CPU fallback 才能降级；降级必须可观测，不能静默返回伪成功。
- `alloc` 成功后只能用配对的 `free` 释放；算法实例销毁前必须释放全部图像缓冲。

## 5. 多实例调度契约

```text
one camera
  -> one bounded encoded queue
  -> one decoder
  -> shared FrameHandle
       -> sampler A -> bounded latest-frame queue -> worker A
       -> sampler B -> bounded latest-frame queue -> worker B
```

- 每个实例按 `analysis_fps` 独立采样。
- 队列必须有固定容量；满时释放最旧等待帧，再插入最新帧。解码线程禁止等待实例队列。
- 每个实例由独占 worker 串行执行 process/update/flush/destroy；分发器不得直接调用插件。
- 单实例阻塞不得降低其他实例和解码线程的处理速率。
- 队列、采样器、worker 使用可注入 monotonic clock，测试不得依赖真实 sleep。

## 6. 断流、IDR Gate 与 Watchdog

部署 Profile 提供默认值，首个 macOS Profile 为：

```text
ingest_timeout_ms = 5000
decoder_stall_timeout_ms = 3000
reconnect_backoff_ms = [1000, 2000, 4000, 8000, 16000, 30000]
offline_after_ms = configurable
```

状态机：

```text
connecting -> waiting_keyframe -> running
     |              |               |
     +---------- reconnecting <------+ ingest timeout
                    |
                 offline (达到阈值，但继续低频重试)
```

- 启动/重连后必须收齐 codec 配置（H.264 SPS/PPS；H.265 VPS/SPS/PPS）并等到可解码关键帧，再向新 decoder session 喂帧。
  `EncodedPacket::is_keyframe` 只能作为媒体层提示，不能单独打开 gate：H.264 必须解析到 NAL type 5，H.265 必须解析到 IRAP NAL type 16--23；参数集状态可跨 access unit 累积，decoder reset/codec 切换/重连时必须清空。
- “输入队列非空且 decoder 持续无输出”达到 decoder timeout 时，销毁并重建 decoder session，回到 `waiting_keyframe`；算法实例和配置保留。watchdog 必须跟踪已成功送入 decoder 但尚未产出帧的状态，即使编码队列已经被消费为空也必须唤醒 worker 执行 reset。
- 只有媒体后端明确声明支持关键帧请求时才发送 PLI/FIR；不支持时记录 capability reason 并等待下一个 IDR，不能承诺固定 1 秒恢复。
- 所有超时使用 monotonic clock；业务 `wall_time_ns` 不用于 watchdog。

## 7. 资源账本与指标

请求启用/修改 FPS 时：

1. 从 manifest 选择第一个 `tier.fps >= requested_fps` 的档位；
2. 构造候选账本，计算所有启用实例 units 总和；
3. 验证总和 `<= allocatable_units`；
4. 验证系统空闲内存 `>= max(platform_min, package_min)`；
5. 全部通过后原子提交，否则保持旧账本与旧 FPS。

六项指标 ID 固定为 `uptime`、`cpu_usage`、`memory_usage`、`disk_watermark`、`accelerator_load`、`temperature`。采样结果必须同时携带 availability 和时间戳。

## 8. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| media/platform 私有类型出现在公共头 | 构建失败 |
| 同摄像头请求第二个上游 session | 返回已存在 source，不新建连接 |
| 实例队列满 | 释放最旧帧，插入最新帧，增加 dropped 计数 |
| codec 配置或 IDR 未就绪 | 丢弃不可解码帧并保持 waiting_keyframe |
| decoder stall | 重建 decoder，不销毁算法实例 |
| PLI/FIR 不支持 | 跳过请求并记录 capability reason |
| FPS 无档位、units 超限或内存不足 | 原子拒绝，现有实例不变 |
| 指标不可用 | availability=`unavailable`，无伪造值 |

## 9. Good / Base / Bad Cases

- Good：ZLM 回调保存有所有权的帧引用，快速 try-push 后返回。
- Base：摄像头不支持关键帧请求，重连后等待自然 IDR。
- Bad：在 EventPoller 内同步 VideoToolbox 解码，或在 decoder 回调里直接调用插件。

## 10. Tests Required

- 单摄像头连接计数、一次解码多实例、不同 FPS 和丢旧留新测试。
- 16 实例并发、单实例阻塞、同实例无重入与可控 shutdown 测试。
- source 注销/track 替换/delegate 移除/弱引用/线程 join 压力测试。
- 无包 5 秒、输入有包无输出 3 秒、缺参数集、前导 P/B、重建恢复测试；使用 fake clock。
- vImage 与 CPU fallback 像素对比、色彩矩阵和 ROI 越界测试。
- FPS 档位、候选账本回滚、内存门槛和六项指标 availability 测试。
- ASan/UBSan/TSan 覆盖断开、重连、停止和对象销毁顺序。

## 11. Wrong vs Correct

```cpp
// Wrong: ZLM 回调执行阻塞工作
track->addDelegate([this](const Frame::Ptr& frame) {
  return decoder_->decode(frame); // 可能阻塞 EventPoller
});

// Correct: 只转移所有权并非阻塞入队
track->addDelegate([weak_queue](const Frame::Ptr& frame) {
  if (auto queue = weak_queue.lock()) {
    queue->try_push(to_access_unit(frame));
  }
  return true;
});
```
