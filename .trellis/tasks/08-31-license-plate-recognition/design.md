# 车牌识别 MVP 技术设计

## 1. 设计目标与边界

本设计实现 Apple Silicon macOS/Core ML 上的车牌识别结果记录：算法包从视频帧中输出稳定车牌识别结果，Engine 生成抓拍和宿主上下文，通过 `app.sock` 上报 Go，Go 持久化为 `plate_observations`，Vue 管理端提供列表和详情查询。

本期不实现名单、黑白名单、告警策略、道闸控制、跨摄像头 ReID、导出和自动清理。通行记录与抓拍图片保留，但不执行到期删除。

依赖方向保持：`engine -> sdk <- algo-packages`。Core ML 和 Apple 平台类型只存在 macOS 算法包的 Objective-C++/CMake 实现中。

## 2. 分层职责

### SDK

- 在既有 `av_result_kind` 中复用 `AV_RESULT_RECOGNITION`，不新增结果枚举值。
- 保持 `av_algo_result`、`av_algo_image_req` 和既有 ABI 布局不变。
- 为识别结果增加按算法类型区分的 JSON schema 校验支持；不能破坏现有 `face_recognition` 的 `persons` schema。
- 公共 C ABI 只表达帧、回调、ROI 和结果内存边界，不引入 Core ML 类型或 Go 业务字段。

### 算法包

`algo-packages/macos/arm64/license_plate_recognition/` 自包含模型、配置、预处理、推理、后处理、跟踪和结果序列化。

算法包负责：

- 车牌/车辆检测、透视或旋转矫正、OCR。
- 车牌颜色和类型分类。
- 跨帧 track 管理、文本投票和去重。
- 输出原图归一化 bbox、置信度和车牌文本。
- 提交全景图与车牌裁剪图的 ROI。

算法包不负责：

- 生成摄像头、实例、算法版本、业务时间和图片 ID。
- 写最终图片文件或调用 Go。
- 判断名单和生成业务告警。

### Engine

- 解析并校验 `license_plate_recognition` manifest 与 ABI metadata。
- 将算法结果转换为结构化车牌观测。
- 根据可信运行上下文生成全局事件 ID。
- 复用 image catalog、原子 JPEG 写入、固定容量异步队列、重试和孤儿对账。
- 通过独立 `ReportPlateObservation` RPC 上报，不伪装成 `ReportAlarm`。

### Go 后端

- 接收并校验车牌观测 IPC。
- 以 `event_id` 幂等写入 `plate_observations`。
- 提供分页列表、详情和受 RBAC 保护的图片读取 API。
- 不在 MVP 中执行名单匹配、告警生成或自动数据清理。

### Vue 前端

- 在车辆通行菜单下提供记录列表和详情。
- 使用现有 `requestClient`、API 类型和路由权限体系。
- 不实现名单、告警策略和导出页面。

## 3. 算法结果契约

### 3.1 Manifest

```json
{
  "algorithm_id": "license_plate_recognition",
  "algorithm_type": "license_plate_recognition",
  "version": "1.0.0",
  "platform_id": "macos-arm64-coreml"
}
```

识别类算法不声明 `alarm_type_id`。manifest、SDK validator、Engine loader 和算法 `library_query` 对该条件保持一致。

### 3.2 JSON

`AV_RESULT_RECOGNITION` 的车牌 payload：

```json
{
  "schema_version": 1,
  "plates": [
    {
      "track_id": 1024,
      "plate_text": "粤B12345",
      "normalized_text": "粤B12345",
      "plate_color": "blue",
      "plate_type": "standard",
      "confidence": 0.96,
      "ocr_confidence": 0.94,
      "bbox": [0.35, 0.42, 0.18, 0.09],
      "vehicle_bbox": [0.18, 0.20, 0.54, 0.65]
    }
  ]
}
```

校验规则：

- `schema_version` 固定为 `1`，`plates` 非空且不超过统一上限。
- `plate_text` 为原始展示文本，`normalized_text` 为匹配/查询文本；两者均限制长度和允许字符。
- 首期 `plate_color` 支持 `blue`、`yellow`、`green`；`plate_type` 至少支持 `standard`、`new_energy`，不能识别时不输出伪造文本。
- 两个置信度均为有限值且在 `[0,1]`。
- bbox 使用原图 `[x,y,w,h]` 归一化坐标，必须在画面范围内。
- `track_id` 仅用于实例内跨帧关联，不作为业务幂等键。
- 结果大小不得超过 `AV_MAX_RESULT_JSON_BYTES`。

### 3.3 与人脸识别的兼容

Engine 根据 `algorithm_type` 选择识别 payload parser：`face_recognition` 仍校验 `persons`，车牌算法校验 `plates`。不能把顶层字段统一改为一个模糊的 `items`，也不能让车牌结果通过人脸 parser。

## 4. 运行时数据流

```text
NV12/CVPixelBuffer frame
  -> original RGB + coordinate transform
  -> detection on 640x640 letterbox
  -> plate/vehicle association
  -> original-image plate crop/rectification
  -> OCR + color/type classification
  -> track text vote + deduplication
  -> AV_RESULT_RECOGNITION callback
  -> Engine validates JSON and retains frame
  -> Engine encodes panorama and plate crop
  -> image catalog commit
  -> ReportPlateObservation over app.sock
  -> Go idempotent insert
  -> GET /record/plates and protected image endpoint
```

算法输出的车牌 ROI 和 Engine 的图片生成保持同一原图坐标语义。Engine 对一帧/一个批次最多编码一张全景图和每个需要的车牌裁剪图，图片路径只以 `image_id` 与规范化相对路径传播。

## 5. IPC Proto

在 `engine/proto/argus/v1/app.proto` 新增：

```protobuf
message PlateObservation {
  string event_id = 1;
  string instance_id = 2;
  string camera_id = 3;
  string algorithm_id = 4;
  string algorithm_version = 5;
  int64 wall_time_ns = 6;
  bool time_synced = 7;
  int64 track_id = 8;
  string plate_text = 9;
  string normalized_text = 10;
  string plate_color = 11;
  string plate_type = 12;
  float confidence = 13;
  float ocr_confidence = 14;
  BoundingBox plate_bbox = 15;
  BoundingBox vehicle_bbox = 16;
  string image_id = 17;
  string image_rel_path = 18;
  string plate_image_id = 19;
  string plate_image_rel_path = 20;
}

message ReportPlateObservationRequest {
  PlateObservation observation = 1;
}

message ReportPlateObservationResponse {
  string code = 1;
  string error_message = 2;
}
```

`ReportService` 增加 `ReportPlateObservation`。传输错误使用 gRPC status，业务拒绝使用稳定 `code`；诊断文本不供客户端解析。Proto 只传图片引用，不传视频帧、JPEG bytes 或张量。

## 6. 数据模型

新增 `plate_observations` 表和 `model.PlateObservation`：

```text
id
created_at / updated_at / deleted_at
event_id
instance_id
camera_id
algorithm_id
algorithm_version
occurred_at
time_synced
track_id
plate_text
normalized_text
plate_color
plate_type
confidence
ocr_confidence
plate_bbox_json
vehicle_bbox_json
image_id
image_rel_path
plate_image_id
plate_image_rel_path
```

约束和索引：

- `UNIQUE(event_id, deleted_at)`，适配项目 soft-delete 约定。
- `normalized_text, occurred_at DESC`、`camera_id, occurred_at DESC`、`occurred_at DESC`、`plate_color, occurred_at DESC`。
- 不建数据库外键；显式 TableName、列名和 JSON 序列化标签。
- 生产使用版本化 SQL migration；sqlite AutoMigrate 仅用于单元测试。
- 图片字段只保存 ID 和规范化相对路径；读取时重新验证路径位于 image root 下。

## 7. API 与前端

后端建议增加：

- `GET /record/plates`：分页列表，支持 normalized plate、camera、occurred time、color、type、confidence。
- `GET /record/plates/:id`：详情。
- `GET /record/plates/:id/image`：全景图受保护读取。
- `GET /record/plates/:id/plate-image`：车牌裁剪图受保护读取。

前端新增 `src/api/core/plate.ts` 和车辆通行视图/路由模块。页面只展示 MVP 字段：时间、车牌、颜色、类型、置信度、摄像头、算法版本、全景图和车牌裁剪图。无图片、未同步时间和低置信度必须有明确空状态。

## 8. 失败、重试与一致性

- 非法算法结果：Engine 丢弃，不写图片、不上报，并记录 `ALGO_RESULT_INVALID`。
- 图片写入成功但 IPC 失败：图片保持 `unreported`，记录不在 Go 端生成；重连后按既有 orphan 机制恢复。
- Go 重复收到相同 event：唯一约束/幂等查询返回成功，不重复插入。
- Go 数据库暂时失败：返回稳定业务错误，Engine 不把失败 ACK 当作成功。
- Engine 重启：任务状态由 Go DesiredState 恢复；已写图片按 catalog 对账，MVP 不主动清理业务引用图片。
- 算法失败：丢弃当前帧并计数，达到既有阈值后实例进入 degraded/error，其他任务继续运行。

## 9. 兼容、发布与回滚

- 首发只打包 `macos-arm64-coreml`，RKNN 不改动、不验收。
- 先更新 SDK/Engine/Proto 契约，再构建算法包和后端；生成的 Go protobuf 文件必须与 proto 同步。
- 新算法包安装失败不影响既有 object detection 和 face recognition 实例。
- 包初始化或 self-test 失败时不激活版本；已有激活版本继续运行。
- 数据库 migration 和 API 可独立回滚；如果已产生记录，回滚前需保留数据导出/备份方案，不能静默删除记录。

## 10. 关键技术风险

- 当前识别结果规范对人脸固定使用 `persons`；必须实现按算法类型路由，否则会发生 parser 误用。
- Core ML 模型输入输出名称、shape、并发预测安全性需要在 Apple Silicon 机器上实际验证。
- 车牌识别效果依赖车牌像素宽度、快门、补光、视角和运动模糊；MVP 记录实际指标，但不提前承诺统一现场阈值。
- 车牌文本是敏感数据；日志脱敏和图片 API RBAC 必须与功能同时交付。
