# 通用抓拍记录与三层感知体系技术设计 (Technical Design)

---

## 1. 设计目标与系统边界

### 1.1 设计目标
1. **感知-识别-告警三层体系解耦**：确立清晰的领域模型边界。抓拍记录（`captures`）作为全量视觉事实底座，识别记录（`observations`）作为高价值业务身份凭证，告警记录（`alarm_records`）作为安全规则防范事件。
2. **多态视觉感知与自适应联合抓拍**：统一承载人脸（`face`）、机动车/车牌（`vehicle`）、人体/人形（`person`）、非机动车（`non_motor`）。在人脸-人体流水线中实现自适应联合抓拍（背影/低头无脸行人落库为 `person` 保留衣着轨迹；正脸/侧脸行人联合绑定人体切图与人脸特写；近景闸机为 `face`）。
3. **时序事件流与时空轨迹观测**：采用 `Insert Only` 抓拍事件流模型 + 边缘智能冷却机制，天然构成时间序列，支持跨摄像头时空轨迹串联与单镜头运动回溯。
4. **与现有存储三级防御体系深度结合**：抓拍记录与识别记录完全解耦（禁止 DB 级联删除）；85% 高水位紧急削峰时，`cleanCaptureBatch` 严格优先淘汰未识别抓拍（`is_recognized = false`），保障考勤与识别凭据 100% 完整无损。
5. **零跳动友好前端交互**：顶部 Segmented 分类胶囊 + 自适应智能输入框（根据分类无感切换 Placeholder，位置绝对静止，零 Layout Shift）+ 卡片流/表格/时间线多视图与详情抽屉。

### 1.2 系统边界与白名单
- **抓拍感知覆盖范围**：
  - `captures`：通用抓拍记录、全景原图（`panorama_path`）、主特写切图（`crop_path`）、附属特写切图（`sub_crop_path`）。
- **底库与识别资产白名单**：
  - `persons`、`person_faces` 注册人脸底库及特征图绝对免死白名单；
  - `face_observations` / `plate_observations` 文本结构化数据独立保存，不因抓拍图片到期而丢失业务凭证。

---

## 2. 总体架构与数据流转 (Data Flow & Architecture)

```
 [ 摄像头视频流 RTSP / Video Source ]
                 │
                 ▼
 ┌─────────────────────────────────────────────────────────────────────────┐
 │                       C++ Engine (媒体推理管道)                         │
 │                                                                         │
 │   ┌──────────────────────┐        ┌────────────────────────────────┐    │
 │   │ YOLOv8n / YOLO26n    │        │ SCRFD / 算法模型               │    │
 │   │ (人体/车辆/通用检测) │        │ (人脸检测 / 特征提取)          │    │
 │   └──────────┬───────────┘        └───────────────┬────────────────┘    │
 │              │                                    │                     │
 │              ▼                                    ▼                     │
 │   ┌────────────────────────────────────────────────────────────────┐    │
 │   │          自适应联合分析与冷却分发器 (Capture Dispatcher)       │    │
 │   │   - 编码全景 JPEG (Panorama)                                   │    │
 │   │   - 目标主特写 (Crop) + 附属人脸/车牌特写 (Sub-Crop)           │    │
 │   └──────────────────────────────┬─────────────────────────────────┘    │
 └──────────────────────────────────┼──────────────────────────────────────┘
                                    │ gRPC UDS (ReportCapture)
                                    ▼
 ┌─────────────────────────────────────────────────────────────────────────┐
 │                       Go 后端 (Control Plane)                           │
 │                                                                         │
 │   ┌───────────────────────────┐         ┌──────────────────────────┐    │
 │   │  ReportAdapter.Accept()   │         │  StorageCleanupWorker    │    │
 │   │  - 写入 captures 表       │         │  - 30d TTL 巡检          │    │
 │   │  - 若比中底库:            │         │  - 85% Priority FIFO     │    │
 │   │    is_recognized = true   │         │    (优先淘汰未识别抓拍)   │    │
 │   │    写入 observations      │         │  - 95% 极危熔断保护      │    │
 │   └─────────────┬─────────────┘         └────────────┬─────────────┘    │
 └─────────────────┼────────────────────────────────────┼──────────────────┘
                   │                                    │
                   ▼                                    ▼
 ┌───────────────────────────────────────┐   ┌─────────────────────────────┐
 │           SQLite Database             │   │       Local FileSystem      │
 │  - captures (全量感知流水)            │   │  /var/lib/argus/images/     │
 │  - observations (底库比对记录)        │   │  - captures/ (全景/特写)    │
 │  - alarm_records (规则告警记录)       │   │  - persons/ (底库绝对白名单)│
 └───────────────────────────────────────┘   └─────────────────────────────┘
                   │
                   ▼ RESTful API (/api/record/captures)
 ┌─────────────────────────────────────────────────────────────────────────┐
 │                     Vue3 前端 (Web-AntD Vben 5.7)                       │
 │   [ 目标胶囊: 全部 | 人脸 | 机动车 | 行人 ] + [ 自适应智能搜索框 ]      │
 │   [ 视图切换: 流式卡片网格 (Card Grid) / 数据表格 (Table) / 轨迹轴 ]    │
 │   [ 详情抽屉: 全景双框联动标注 + 全身特写 + 高清人脸特写 ]              │
 └─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 详细契约与接口设计 (Contracts & Schemas)

### 3.1 数据库表结构 (`captures`)

```sql
CREATE TABLE IF NOT EXISTS `captures` (
    `id` INTEGER PRIMARY KEY AUTOINCREMENT,
    `event_id` VARCHAR(64) NOT NULL,
    `target_type` VARCHAR(32) NOT NULL,
    `track_id` BIGINT NOT NULL DEFAULT 0,
    `camera_id` BIGINT NOT NULL DEFAULT 0,
    `camera_name` VARCHAR(128) NOT NULL DEFAULT '',
    `task_id` BIGINT NOT NULL DEFAULT 0,
    `algorithm_id` VARCHAR(64) NOT NULL DEFAULT '',
    `algorithm_version` VARCHAR(32) NOT NULL DEFAULT '',
    `captured_at` BIGINT NOT NULL,
    
    `confidence` REAL NOT NULL DEFAULT 0.0,
    `quality_score` REAL NOT NULL DEFAULT 0.0,
    `bbox_json` TEXT NOT NULL DEFAULT '',
    
    `panorama_image_id` VARCHAR(64) NOT NULL DEFAULT '',
    `panorama_path` VARCHAR(255) NOT NULL DEFAULT '',
    `crop_image_id` VARCHAR(64) NOT NULL DEFAULT '',
    `crop_path` VARCHAR(255) NOT NULL DEFAULT '',
    `sub_crop_image_id` VARCHAR(64) NOT NULL DEFAULT '',
    `sub_crop_path` VARCHAR(255) NOT NULL DEFAULT '',
    
    `is_recognized` INTEGER NOT NULL DEFAULT 0,
    `attributes_json` TEXT NOT NULL DEFAULT '',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `deleted_at` DATETIME DEFAULT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS `idx_captures_event_id` ON `captures` (`event_id`, `deleted_at`);
CREATE INDEX IF NOT EXISTS `idx_captures_target_type` ON `captures` (`target_type`);
CREATE INDEX IF NOT EXISTS `idx_captures_captured_at` ON `captures` (`captured_at`);
CREATE INDEX IF NOT EXISTS `idx_captures_camera_id` ON `captures` (`camera_id`);
CREATE INDEX IF NOT EXISTS `idx_captures_task_id` ON `captures` (`task_id`);
CREATE INDEX IF NOT EXISTS `idx_captures_is_recognized` ON `captures` (`is_recognized`);
```

### 3.2 gRPC 通信协议 (`engine/proto/argus/v1/app.proto`)

```protobuf
syntax = "proto3";

package argus.v1;

import "argus/v1/common.proto";

option go_package = "argus/app/internal/proto/argus/v1;argusv1";

message CaptureEvent {
  string event_id = 1;          // 目标级全局事件 ID
  string instance_id = 2;       // 算法实例 ID
  string camera_id = 3;         // 摄像头 ID
  string camera_name = 4;       // 摄像头名称快照
  string algorithm_id = 5;      // 算法包 ID
  string algorithm_version = 6; // 算法版本
  string target_type = 7;       // 目标类型: "face" | "person" | "vehicle" | "non_motor"
  int64 track_id = 8;           // 跟踪 ID
  int64 wall_time_ns = 9;       // 纳秒时间戳
  bool time_synced = 10;
  
  BoundingBox bbox = 11;        // 主目标归一化坐标 (如人脸框或人体框)
  BoundingBox sub_bbox = 12;    // 附属目标归一化坐标 (如联合抓拍中的人脸框)
  float confidence = 13;        // 检测置信度 [0, 1]
  float quality_score = 14;     // 综合质量分 [0, 1]
  
  string image_id = 15;         // 全景图 ID
  string image_rel_path = 16;   // 全景图相对路径
  string crop_image_id = 17;    // 主特写图 ID
  string crop_image_rel_path = 18; // 主特写图相对路径
  string sub_crop_image_id = 19;   // 附属特写图 ID (可选)
  string sub_crop_image_rel_path = 20; // 附属特写图相对路径 (可选)
  
  string attributes_json = 21;  // 异构特征属性 JSON (性别、年龄、车牌号、衣着颜色等)
  bool is_recognized = 22;      // 是否同时比中底库
}

message ReportCaptureRequest {
  CaptureEvent capture = 1;
}

message ReportCaptureResponse {
  string code = 1;          // 空串表示成功
  string error_message = 2; // 诊断信息
}
```

### 3.3 RESTful API 契约 (`internal/api/capture.go`)

- **分页列表查询**：`GET /api/record/captures`
  - Query 参数：
    - `page` (int, 默认 1)
    - `pageSize` (int, 默认 20, 最大 100)
    - `targetType` (string, `all` | `face` | `person` | `vehicle` | `non_motor`)
    - `cameraId` (uint64, 可选)
    - `startTime` / `endTime` (ISO 8601 / RFC 3339 字符串)
    - `keyword` (string, 支持车牌号模糊检索、事件 ID)
    - `isRecognized` (bool / string, 可选)
    - `minQuality` (float32, 可选)
  - 响应：`{ code: 0, data: { items: [...CaptureItem], total: 1240 }, message: "ok" }`

- **详情查询**：`GET /api/record/captures/:id`
  - 响应：包含全景图 URL、特写图 URL、双 BBox 坐标对象、完整解析后的 Attributes 结构。

- **安全图片流读取**：`GET /api/record/captures/:id/image?kind=panorama|crop|sub_crop`
  - 内部鉴权并读取磁盘文件，返回 `image/jpeg` 流。

---

## 4. 存储削峰与解耦保护机制 (Storage & Retention)

在 `internal/service/storage_cleanup.go` 中：

1. **分批与清理接口**：
   - `captureRepo.FindExpired(ctx, cutoff, limit)`：按 `captured_at < cutoff` 查找过期抓拍；
   - `captureRepo.FindOldestUnrecognized(ctx, limit)`：**核心优先级削峰**——查询 `is_recognized = 0` 中 `captured_at` 最早的记录；
   - `captureRepo.FindOldest(ctx, limit)`：兜底削峰（未识别抓拍清空后才调用）。
2. **防孤儿物理删除顺序**：
   - 先调用 `storage.Delete(ctx, rec.PanoramaPath)`、`rec.CropPath`、`rec.SubCropPath` 物理删除文件；
   - 后调用 `captureRepo.HardDeleteBatch(ctx, ids)` 物理硬删除 SQLite 记录；
   - 严格每批 200 条并 `50ms` I/O 让步。
3. **识别记录隔离**：
   - 抓拍记录硬删除时，`observations` 表中对应的考勤与通行记录绝对保留，实现证据链解耦。

---

## 5. 前端交互与组件设计 (Frontend UI/UX)

### 5.1 布局架构（零跳动设计）
- **顶部 FilterBar**：
  - 左侧：`a-segmented` 胶囊组件（`全部`、`👤 人脸`、`🚗 机动车`、`🚶 行人`）；
  - 中间：摄像头点位选择器 + 日期时间范围选择器；
  - 右侧：`a-input-search` 万能智能搜索框（根据 Segmented 当前值自适应切换 Placeholder，位置固定，避免 Layout Shift）；
  - 操作区：`[ 查询 ]`、`[ 重置 ]`、`[ 高级筛选 ▾ ]`（展开抽屉展示低频阈值/颜色滑块）。
- **视图切换区**：
  - 右上角提供 `a-radio-group` 图标按钮：`[ 网格视图 (Card Grid) ]` | `[ 表格视图 (VxeGrid) ]`。
- **详情抽屉 (`CaptureDetailDrawer.vue`)**：
  - 顶部：全景图 Canvas 预览器（高亮绘制 `face_bbox` 绿色框与 `person_bbox` 蓝色框）；
  - 中部：局部特写对比区（并排展示人体站姿全身切图 + 高清人脸特写切图）；
  - 底部：属性解析标签云与快捷动作按钮（「以图搜轨迹」、「快速注册到底库」）。
