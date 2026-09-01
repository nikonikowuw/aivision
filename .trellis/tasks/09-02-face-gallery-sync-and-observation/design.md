# 技术设计：人脸特征下发与识别记录

## 1. 总体数据流

```
[注册/删除人脸]                          [实时流识别]
      │                                       │
      ▼                                       ▼
 person_faces (Go)                    算法包 AV_RESULT_RECOGNITION
      │  同事务 bump                          │  (persons[].face.embedding)
      ▼                                       ▼
 face_gallery_revision ──┐            uds_server::handle_face_result
                          │                   │
      ┌───────────────────┘                   ├─ 1:N 比对（FaceGallery 内存索引）
      ▼                                       ├─ 阈值判定（per-instance）
 GetFaceGallery(rev)  ◄── Engine 每 2s 拉     ├─ track 上报状态：首次 / 更优
      │                                       ├─ 裁图（全景 + 人脸 ROI）
      ▼                                       ▼
 FaceGallery 原子换库               ReportFaceObservation(event_id, similarity, ...)
                                              │
                                              ▼
                                    Go 单调 upsert → face_observations
                                              │
                                              ▼
                                    /record/faces 查询 + 前端
```

## 2. 版本计数器：为什��必须独立

`gallery_revision` **不能复用** `desired_state_revision`（`model/task.go:133`）。

复用会导致：注册一张人脸 → `desired_state_revision` 递增 → Engine 下一轮 `get_desired_state` 发现 revision 变化 → **重新 apply 全部摄像头任务与算法实例配置**。人脸注册是纯数据变更，不应触发媒体管线的全量对账。

因此新建单行计数器表 `face_gallery_revision`（结构对齐 `desired_state_revision`：`id=1` 单行、只增不减），Engine 侧独立维护 `applied_gallery_revision` 变量。两个版本号互不影响。

`BumpFaceGalleryRevisionTx` 必须与人脸写入在**同一事务**内提交，语义与现有 `RevisionBumper` 一致（见 `service/revision.go` 的注释）。触发点：
- `PersonFaceRepository.Create`
- `PersonFaceRepository.Delete`
- `PersonFaceRepository.DeleteAllByPersonID`（人员删除级联）

## 3. Proto 契约变更

### 3.1 `app.proto` — 底库下发（Engine 拉）

```protobuf
// 单条人脸底库条目
message FaceGalleryEntry {
  string face_id = 1;
  string person_id = 2;
  string person_name = 3;      // 冗余，供 Engine 上报时携带历史快照
  bytes embedding = 4;         // 512 维 L2 归一化 float32 小端字节流（精确 2048 字节）
}

message GetFaceGalleryRequest {
  uint64 current_gallery_revision = 1;
}

message GetFaceGalleryResponse {
  uint64 gallery_revision = 1;
  bool changed = 2;                        // false 时 entries 必为空
  repeated FaceGalleryEntry entries = 3;   // changed=true 时为全量快照
  string code = 4;
  string error_message = 5;
}
```

挂在 `ControlPlaneService`（Go 是 server，Engine 是 client），与 `GetDesiredState` 并列。

### 3.2 `app.proto` — 识别记录上报

```protobuf
message FaceObservation {
  string event_id = 1;         // "<instance_run_id>/<track_id>"，track 稳定，覆盖上报时不变
  string instance_id = 2;
  string camera_id = 3;
  string algorithm_id = 4;
  string algorithm_version = 5;
  int64 wall_time_ns = 6;
  bool time_synced = 7;
  int64 track_id = 8;
  string face_id = 9;          // 命中的底库条目
  string person_id = 10;
  string person_name = 11;     // 底库快照，Go 侧直接落库不再回查
  float similarity = 12;       // 归一化 [0,1]
  BoundingBox face_bbox = 13;
  string image_id = 14;        // 全景图
  string image_rel_path = 15;
  string face_image_id = 16;   // 人脸特写图
  string face_image_rel_path = 17;
}

message ReportFaceObservationRequest { FaceObservation observation = 1; }
message ReportFaceObservationResponse { string code = 1; string error_message = 2; }
```

挂在 `ReportService`，与 `ReportPlateObservation` 并列。

### 3.3 `engine.proto` — per-instance 阈值

```protobuf
// 人脸识别比对配置（系统/引擎级，不进入算法 params_json）
message FaceRecognitionConfig {
  float similarity_threshold = 1;  // 归一化 [0,1]
}
```

加入 `AlgorithmInstanceConfig.face_recognition = 10`，与 `motion_gate = 9` 完全同构。

### 3.4 删除死契约

删除 `person.proto` 中的 `SyncPersons` / `SyncPersonsRequest` / `SyncPersonsResponse` / `PersonRecord` 及 Engine 侧对应的 UNIMPLEMENTED handler。它是专为本能力预留的空壳，方案定为 Engine 拉取后永远不会被实现，保留即为误导性死契约。`ExtractFaceFeature` 保持不变。

### 3.5 gRPC 消息上限

两端显式设置 32MB：
- Go server：`grpc.MaxRecvMsgSize(32<<20)` / `grpc.MaxSendMsgSize(32<<20)`（`engineipc/runtime.go:36`）
- Go client：`grpc.WithDefaultCallOptions(...)`（`engineipc/client.go:68`）
- C++ server：`builder.SetMaxReceiveMessageSize(32<<20)` / `SetMaxSendMessageSize`（`uds_server.cpp:2519`）
- C++ client：`grpc::ChannelArguments` 同步设置（`uds_client.cpp`）

5000 条目全量约 11MB，留约 3x 余量。

## 4. Engine 侧设计

### 4.1 FaceGallery 模块（新增）

```cpp
// engine/src/core/algo/face_gallery.hpp
struct FaceGalleryEntry { std::string face_id, person_id, person_name; };

class FaceGallery {
public:
    static FaceGallery& instance();
    // 原子换库：构建完成才整体替换，失败不影响旧库
    void swap(uint64_t revision, std::vector<float> flat_embeddings,
              std::vector<FaceGalleryEntry> meta);
    uint64_t revision() const;
    bool ready() const;                 // revision > 0 且条目非空
    // 返回最高归一化相似度及其条目；库空返回 nullopt
    std::optional<MatchResult> match(const float* query_512) const;
private:
    mutable std::shared_mutex mu_;
    std::vector<float> embeddings_;     // N × 512 连续存储，cache 友好
    std::vector<FaceGalleryEntry> meta_;
    uint64_t revision_ = 0;
};
```

**存储布局**：embedding 用单块连续 `std::vector<float>`（N×512），而非 per-entry 的 vector。5000 条目 = 10MB 连续内存，比对时顺序扫描，cache 命中率最优。

**相似度计算**：两端向量均已 L2 归一化，cosine 退化为点积。归一化到 `[0,1]`：

```cpp
const float dot = std::inner_product(q, q + 512, base + i * 512, 0.0f);
const float normalized = (dot + 1.0f) * 0.5f;
```

**并发**：`shared_mutex`，`match()` 持共享锁、`swap()` 持独占锁。换库瞬间阻塞比对，5000 条目的 swap 是 O(1) 的 move，可忽略。

### 4.2 底库拉取（`main.cpp` control_plane_thread）

在现有 2 秒循环内，紧随 `get_desired_state` 之后：

```cpp
argus::v1::GetFaceGalleryResponse gallery_resp;
if (client.get_face_gallery(applied_gallery_revision, &gallery_resp) &&
    gallery_resp.code().empty() && gallery_resp.changed()) {
    if (FaceGallery::instance().load_from(gallery_resp)) {   // 内部校验 + 原子 swap
        applied_gallery_revision = gallery_resp.gallery_revision();
    }
}
```

**fail-static**：RPC 失败、`code` 非空或校验失败时，`applied_gallery_revision` 保持不变、旧库不动，下一轮重试（PRD R3 / AC3）。

**条目校验**（任一失败则整批丢弃，不做部分加载）：`embedding` 长度精确 2048 字节、所有 float 有限（`std::isfinite`）、L2 范数落在 `[0.98, 1.02]`、条目数 ≤ 5000、`face_id`/`person_id` 非空。

### 4.3 结果处理（`uds_server.cpp:1699`）

```cpp
} else if (result.kind == AV_RESULT_RECOGNITION) {
    const auto type = instance->get_algorithm_type();
    if (type == "license_plate_recognition") {
        handle_license_plate_result(instance, result, frame);
    } else if (type == "face_recognition") {
        handle_face_recognition_result(instance, result, frame);   // 新增
    }
}
```

`handle_face_recognition_result` 流程：

1. 解析 JSON，校验 `schema_version == 1`、`persons` 为数组且 ≤ 4096。
2. `FaceGallery::ready()` 为 false → **直接返回**（PRD R4，冷启动丢弃）。
3. 对每个 `persons[i]`：跳过 `face` 为 null 或 `embedding` 为 null 的条目；Base64 解码 → 校验 2048 字节 → 反序列化为 512 float。
4. `FaceGallery::match()` 取最高归一化相似度。
5. 低于该实例的 `similarity_threshold` → 丢弃（PRD：陌生人不落库）。
6. 查 track 上报状态表决定是否上报（见 4.4）。
7. 需要上报才裁图（PRD R8）：全景图复用现有 `capture_id` 机制；人脸特写图使用算法包已提供的 `av_algo_image_req`（`purpose = kImagePurposeFaceCrop`）ROI。
8. 构造 `FaceObservation`，`event_id = instance->get_run_id() + "/" + std::to_string(track_id)`，走现有上报队列（含重试）。

`event_id` 的组成部分必须通过现有 `is_safe_component` 校验（仅允许 alnum / `.` / `_` / `-`），`run_id` 与十进制 `track_id` 均满足。

### 4.4 Track 上报状态表

```cpp
struct FaceTrackReportState { float reported_similarity; };
// key: track_id（run_id 天然按实例隔离）
std::unordered_map<int64_t, FaceTrackReportState> face_track_states_;
```

判定：

```cpp
constexpr float kFaceReportImprovementMargin = 0.03f;   // 归一化空间
auto it = states.find(track_id);
const bool should_report = (it == states.end()) ||
                           (similarity >= it->second.reported_similarity + kFaceReportImprovementMargin);
```

上报后写入 `reported_similarity = similarity`。

**生命周期**：状态表随 `AlgorithmInstance` 存活，实例停止/销毁时整体释放。算法包侧 `max_person_count=16` + `max_recognitions_per_track=3` 保证条目数极小，但仍需上界保护：条目数超过 4096 时清空（防御异常 track_id 分配）。

## 5. Go 侧设计

### 5.1 Migration（3 个）

| 编号 | 内容 |
| --- | --- |
| `000031_add_face_observations` | 记录表 + 索引 |
| `000032_add_face_recognition_to_instances` + `face_gallery_revision` | `algorithm_instances.face_recognition_json JSONB DEFAULT '{}'`；`face_gallery_revision` 单行计数器表 |
| `000033_seed_record_face_menu` | `record:face` 权限码 + 记录菜单（对齐 `000025_seed_record_plate_menu`） |

`face_observations` 表：

```sql
CREATE TABLE face_observations (
    id                     BIGSERIAL    PRIMARY KEY,
    event_id               VARCHAR(200) NOT NULL,
    instance_id            VARCHAR(64)  NOT NULL DEFAULT '',
    camera_id              VARCHAR(64)  NOT NULL,
    camera_name            VARCHAR(128) NOT NULL DEFAULT '',
    algorithm_id           VARCHAR(64)  NOT NULL DEFAULT '',
    algorithm_version      VARCHAR(32)  NOT NULL DEFAULT '',
    track_id               BIGINT       NOT NULL DEFAULT 0,
    face_id                VARCHAR(64)  NOT NULL DEFAULT '',
    person_id              VARCHAR(64)  NOT NULL DEFAULT '',
    person_name            VARCHAR(128) NOT NULL DEFAULT '',
    similarity             REAL         NOT NULL DEFAULT 0,   -- 归一化 [0,1]
    bbox_json              JSONB        NOT NULL DEFAULT '{}'::jsonb,
    time_synced            BOOLEAN      NOT NULL DEFAULT FALSE,
    image_id               VARCHAR(200) NOT NULL DEFAULT '',
    image_rel_path         VARCHAR(255) NOT NULL DEFAULT '',
    face_image_id          VARCHAR(200) NOT NULL DEFAULT '',
    face_image_rel_path    VARCHAR(255) NOT NULL DEFAULT '',
    observed_at            TIMESTAMPTZ  NOT NULL,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at             BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_face_observations_event_id ON face_observations (event_id, deleted_at);
CREATE INDEX idx_face_observations_observed_at ON face_observations (observed_at DESC);
CREATE INDEX idx_face_observations_person_id ON face_observations (person_id);
CREATE INDEX idx_face_observations_camera_id ON face_observations (camera_id);
CREATE INDEX idx_face_observations_image_id ON face_observations (image_id);
CREATE INDEX idx_face_observations_face_image_id ON face_observations (face_image_id);
```

`person_id` 允许空串（陌生人扩展位）。两个 image_id 索引供孤儿图片对账使用（对齐 `plate_observations`）。

### 5.2 单调 upsert（本任务最关键的正确性点）

`ReportAdapter.AcceptFaceObservation`：

```go
// 1. 尝试插入
err := tx.Create(&record).Error
if err == nil { return nil }
if !isUniqueViolation(err) { return err }

// 2. 唯一冲突 → 仅当新相似度更高时覆盖
res := tx.Model(&model.FaceObservation{}).
    Where("event_id = ? AND deleted_at = 0 AND similarity < ?", eventID, similarity).
    Updates(map[string]any{ /* 全部业务字段 + updated_at */ })
if res.Error != nil { return res.Error }
// RowsAffected == 0 表示已有记录相似度不低于本次上报（含重试队列乱序）
// 视为幂等成功，不返回错误、不触发 Engine 重试
return nil
```

**必须覆盖的测试场景**：先落 `similarity=0.91`，再到达 `similarity=0.65` 的迟到重试 → 记录保持 0.91，`person_id` 不变（PRD AC7）。

### 5.3 底库下发服务

`DesiredStateAdapter`（或新建 `FaceGalleryAdapter`）实现 `GetFaceGallery`：

1. 读 `face_gallery_revision`；等于请求值 → 返回 `changed=false` + 空 `entries`（PRD AC2）。
2. 不等 → 查询全部未软删的 `person_faces` join `persons` 取 `person_name`，条目数上限 5000。
3. 返回 `changed=true` + 全量条目 + 当前 revision。

**读取一致性**：revision 读取与条目查询必须在同一只读事务内，否则可能返回「revision 是新的、数据是旧的」的组合，导致 Engine 永久停在错误状态。

### 5.4 底库容量拒绝

`PersonFaceRepository.Create` 现有事务内已做 per-person ≤ 10 的检查，追加全局条目数检查：`COUNT(*) WHERE deleted_at = 0 >= 5000` → 返回新错误码 `CodeFaceGalleryFull`（1410 序列，接现有 1401-1409）。

### 5.5 查询接口

| 方法 | 路径 | 权限码 |
| --- | --- | --- |
| GET | `/api/record/faces` | `record:face` |
| GET | `/api/record/faces/:id` | `record:face` |
| GET | `/api/record/faces/:id/panorama` | `PermCodeAuthenticated` |
| GET | `/api/record/faces/:id/face` | `PermCodeAuthenticated` |

结构与 `router.go:377-388` 的车牌四端点完全同构。列表支持 `personId` / `personName` / `cameraId` / 时间范围 / 分页。

## 6. 前端设计

- `views/resource/task/components/InstanceFormModal.vue`：新增 `similarityThreshold` 数字输入（步进 0.01、范围 0~1、默认 0.7），仅当算法类型为 `face_recognition` 时显示 —— 条件渲染方式对齐现有 `motionGate` 字段。
- `views/record/face/`：新建记录列表页，结构参照 `views/record/plate/`。列：抓拍特写缩略图、人员姓名、相似度（带阈值色阶 Tag）、摄像头名称、识别时间。筛选：人员姓名、摄像头、时间范围。详情抽屉展示全景图 + 人脸特写对比。
- `api/core/face-observation.ts`：四个端点的封装，图片走 Bearer 认证的 blob 流（对齐车牌图片加载方式）。
- i18n：`zh-CN` / `en-US` / `zh-TW` 三份 `resource.json` 同步补齐。

## 7. 风险与回滚

| 风险 | 缓解 |
| --- | --- |
| 阈值 0.7（≡ cosine 0.4）偏宽松，现场可能误识 | per-instance 可调，无需改代码；PRD 明确标注待实测校准 |
| 换库瞬间阻塞比对 | swap 为 O(1) move，实测应 < 1ms；如成为问题改用双缓冲 + 原子指针 |
| Engine 与 Go 的归一化口径不一致 | 全链路只传归一化值，Engine 是唯一的换算点，Go/前端不做任何相似度换算 |
| 覆盖上报产生孤儿图片 | 复用现有 `ReportOrphanImages` + 5 分钟保护期；不主动删图 |
| 底库读取与 revision 不一致 | 同一只读事务内读取（§5.3） |

**回滚**：所有变更为增量。Engine 侧摘除 `handle_face_recognition_result` 分支即回到「人脸结果丢弃」的现状；proto 新增字段/RPC 向后兼容；migration 提供对应 down 脚本。
