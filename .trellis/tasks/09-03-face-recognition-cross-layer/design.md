# face_recognition SDK Engine Go 跨层分析与验证设计

## 1. 范围与系统边界

本任务对 `face_recognition` 业务链路开展跨层数据契约、并发边界、资源生命周期和端到端一致性审计与验证。
跨越四个核心层次：
1. **算法包层 (Algorithm Package Layer)**：`algo-packages/macos/arm64/face_recognition/`
2. **C ABI 契约层 (SDK C ABI Layer)**：`sdk/c_abi/include/argus/c_abi/algorithm.h`、`media_frame.h`
3. **推理与抓拍引擎层 (Engine Core Layer)**：`engine/src/core/ipc/uds_server.cpp`、`FaceGallery`、`FramePool`
4. **后端业务与持久化层 (Go Backend Layer)**：`argus/internal/pkg/engineipc/`、`argus/internal/service/report_adapter.go`、`argus/internal/model/`

不变量承诺：
- 严格遵循跨层思考指南（`cross-layer-thinking-guide.md`）
- 保持公共 C ABI 强兼容与符号隔离
- 保持 Engine 现有流水线生产调度不变
- 保持 Go 后端 GORM 数据模型与 SQLite 迁移脚本不变

---

## 2. 端到端数据流与所有权拓扑

```text
[Camera/Media Source]
         │ (YUV/NV12 原始帧)
         ▼
    [FramePool] ─── (allocates frame_token, ref_count=1)
         │
         ▼
[AlgorithmInstance / Worker Thread]
         │ passes const av_frame_desc* (borrowed reference, read-only)
         ▼
[face_recognition Plugin]
         │ 1. Preprocess: NV12 -> ARGB -> Letterbox
         │ 2. SCRFD 10G: MultiArray -> Decode -> NMS
         │ 3. ByteTracker: association & track state
         │ 4. Quality Evaluation: sharpness, pose, illumination
         │ 5. Optional Alignment: 112x112 similarity transform
         │ 6. Optional GLINTR: 512-dim float32 L2-normalized embedding
         │ 7. JSON Serialization: schema_version=1, persons[]
         ▼
[av_algo_result_t Callback] ─── (Synchronous callback to Engine)
         │ * Pointer validity: only during callback execution!
         ▼
[Engine: handle_face_recognition_result]
         │ 1. Envelope & Security Validation: json_len, safe_component
         │ 2. Frame & Track Integrity: frame_id match, track_id monotonic
         │ 3. Embedding Strict Validation: base64, 512-dim, norm in [0.98, 1.02]
         │ 4. Image Requests Validation: purpose, ROI within [0, 1]
         │ 5. FaceGallery Matching: Top-5 cosine distance match
         │ 6. Snapshot Decision: interval >= 800ms / quality jump / sim jump
         │ 7. Retain Frame: frame_ops->retain(frame_token), ref_count++
         ▼ (enqueue PendingCapture to bounded queue)
[CaptureWorkerThread]
         │ 1. Crop & Encode Panorama JPEG (0, 0, 1, 1)
         │ 2. Crop & Encode Face Close-up JPEG (from ROI)
         │ 3. Release Frame: frame_ops->release(frame_token), ref_count--
         │ 4. Write JPEG to storage: ${date}/${camera_id}/${event_id}.jpg
         ▼
[UDS gRPC Client] ─── ReportFaceObservation / ReportFaceCapture
         │
         ▼ (Unix Domain Socket)
[Go Backend: engineipc.ReportService]
         │ 1. Unmarshal protobuf & Method routing
         │ 2. Safety check: isPathSafe, storage circuit breaker (<95% disk)
         ▼
[Go Backend: ReportAdapter]
         │ 1. Normalize BBox: [xmin, ymin, xmax, ymax]
         │ 2. Monotonic Upsert (face_observations): ignore outdated/lower quality
         │ 3. Append Snapshot (face_captures): append up to 5 temporal snapshots
         ▼
[SQLite DB: WAL Mode] ── (face_observations & face_captures tables)
```

---

## 3. 关键字段跨层契约对齐矩阵

| 字段语义 | 算法包输出 (JSON) | C ABI 结构体 | Engine 内部表示 | gRPC Proto 字段 | Go 后端 Model | 校验规则与转换逻辑 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **帧标识** | `"frame_id": 123` | `av_frame_desc.frame_id` | `uint64_t` | `frame_id` (通用抓拍) | - | 算法输出必须严格等于输入帧 `frame_id`，否则拒绝 |
| **纳秒时间戳** | `"pts_ns": 1000000000` | `av_frame_desc.pts_ns` | `uint64_t` | `wall_time_ns` | `ObservedAt (time.Time)` | 从 `wall_time_ns` 转为 Go `time.Unix(0, ns)` |
| **运行实例** | - | - | `instance->get_run_id()` | `instance_id` | `InstanceID` | 安全组件字符校验 `[a-zA-Z0-9._-]`，长度 <= 128 |
| **轨迹号** | `"track_id": 1` | - | `int64_t track_id` | `track_id` | `TrackID` | 必须非负整数，同帧内单调递增且不重复 |
| **事件主键** | - | - | `${run_id}/${track_id}` | `event_id` | `EventID (PRIMARY KEY)` | 贯穿 Engine、gRPC 与 Go 数据库的唯一标识 |
| **人脸框** | `"bbox": [x, y, w, h]` | `av_algo_image_req.x/y/w/h` | `std::array<double, 4>` | `FaceBBox {xmin, ymin, xmax, ymax}` | `BBoxJSON (string)` | 算法输出 `[x,y,w,h]`，Engine 转换为 `[xmin,ymin,xmax,ymax]` 并存入 JSON |
| **五关键点** | `"landmarks": [[x,y]..]` | - | 5 组 `std::pair<double, double>` | - (可扩展) | - | 坐标严格限制在 `[0.0, 1.0]`，供仿射对齐使用 |
| **特征向量** | `"embedding": {...}` | - | 512维 `vector<float>` | - (底库内匹配) | - | 512 维 float32，Base64 编码，L2 模长限制在 `[0.98, 1.02]` |
| **相似度** | - | - | `float similarity` | `similarity` | `Similarity (float32)` | 余弦相似度 `[-1.0, 1.0]`，低于 0 或异常置 0，比较阈值 |
| **底库版本** | - | - | `FaceGallery::revision()` | `gallery_revision` | `GalleryRevision` | uint64 单调递增，热换库原子递增 |
| **全景图路径** | - | - | `image_id` / `image_rel_path` | `image_id` / `image_rel_path` | `ImageID` / `ImageRelPath` | 路径安全校验 `isPathSafe`，防目录穿越 |
| **特写图路径** | - | - | `face_image_id` / `...` | `face_image_id` / `...` | `FaceImageID` / `...` | 对应 `result.images` 切图，安全路径 |

---

## 4. 资源生命周期、所有权与并发边界

### 4.1 帧缓冲区引用计数生命周期 (Frame Buffer Lifecycle)
- **初始态**：`FramePool` 分配 `av_frame_desc`，初始 `ref_count = 1`。
- **算法推理阶段**：算法包通过借用指针 `const av_frame_desc*` 同步读取像素数据，不持有引用，不调用 release。
- **抓拍决策阶段**：若判定需要产生全景图或特写抓拍，Engine 调用 `frame_ops->retain(frame_token)`，引用计数增至 `ref_count = 2`。
- **抓拍队列阶段**：帧元数据放入 `PendingCapture`，进入 `capture_queue_`。若队列满，必须立即调用 `frame_ops->release(frame_token)`，防止泄漏。
- **后台抓拍工作者阶段**：抓拍线程完成 JPEG 编码后，调用 `frame_ops->release(frame_token)`。
- **视频主管道释放阶段**：视频解码/分发主线程完成帧处理后，调用其自有的 `frame_ops->release(frame_token)`。当两者均完成时，缓冲块归还池中。

### 4.2 C ABI 回调内存可见性与悬挂防御
- 算法包向 Engine 回传的 `av_algo_result_t` 结构体以及指向的 `json`、`images` 指针，**生命周期仅限定在回调函数执行期间**。
- Engine 在回调内部同步完成 JSON 解析、向量拷贝与结构体验证，严禁将裸指针投递给异步线程。

### 4.3 人脸底库热换并发控制 (FaceGallery RCU-Style)
- `FaceGallery` 使用原子智能指针实现无锁并发读（RCU 语义）：
  - 推理线程：获取当前底库快照的 `shared_ptr` 进行匹配，耗时极短且不被阻塞。
  - 控制平面线程：构建全新的底库索引后，原子替换智能指针，并递增 `revision`。旧底库在所有正在匹配的读线程退出后自动析构。

### 4.4 Go 后端单调 Upsert 与迟到覆盖防御 (Monotonic Upsert)
- 网络重试、乱序处理可能导致“后发生但低相似度/低质量”的识别记录迟到。
- Go 后端 `AcceptFaceObservation` 在落库时使用 `UpsertMonotonic`：
  - 若 `event_id` 不存在，执行 `INSERT`。
  - 若 `event_id` 已存在，仅当新记录的 `Similarity` 高于现有记录时，才更新匹配人名与特征信息；否则保留原有最高质量匹配。

---

## 5. 风险矩阵与防御机制

| 风险项 | 潜在后果 | 触发条件 | 代码防御锚点 |
| :--- | :--- | :--- | :--- |
| **图片孤儿 (Image Orphan)** | 磁盘堆积无主 JPEG 图片，浪费存储 | 抓拍切图写盘成功，但 UDS RPC 失败或 DB 写入失败 | 存储清理任务 (`storage_cleanup`) 定期扫描无关联文件并清理；磁盘使用率 >= 95% 时触发熔断 |
| **内存悬挂 (Frame Leak)** | 内存持续泄漏导致 OOM | 抓拍任务入队失败或异常未释放 retain | `uds_server.cpp` 入队异常捕获中显式判断 `pending.frame_retained` 并调用 `release` |
| **迟到降级 (Late Degradation)** | 人脸识别误将高置信度结果覆写为低置信度 | 跟踪目标转头导致后续帧置信度下降且延迟到达 | `faceRepo.UpsertMonotonic` 强制校验单调递增性 |
| **底库悬挂 (Stale Gallery Match)** | 引用已删除的人员进行特征比对 | 底库删除人员与推理匹配并发发生 | `FaceGallery` 包含全局 revision，匹配时携带 revision，DB 关联时做弱一致性容忍 |
| **Base64 溢出与畸形向量** | 内存越界或 NaN 污染浮点计算 | 算法包产生格式错误的 Embedding | `valid_embedding` 严格校验 512 维、有限数判定及模长 `[0.98, 1.02]` |

---

## 6. 验证计划与测试清单

1. **已有单元测试回归 (L1)**：
   - SDK 契约测试：`engine/tests/unit/test_c_abi.cpp`
   - Engine UDS 与人脸底库测试：`test_uds_reconcile`、`test_face_gallery`
   - Go 后端单调落库测试：`argus/internal/service/face_observation_test.go`、`face_capture_test.go`
2. **端到端契约集成验证 (L1/L2)**：
   - 验证算法包产生的真实 JSON 报文与 Engine 解析器的字段匹配度。
   - 验证 512 维向量在序列化、Base64 编解码、模长校验和 Top-K 匹配中的数值一致性。
   - 验证图片请求 ROI 坐标在算法包、Engine 切图与 Go 数据库中的几何一致性。
