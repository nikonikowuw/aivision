# 告警记录模块技术设计 (design.md)

## 1. 架构与数据流

```
+-------------------------------------------------------------+
|                      C++ 推理引擎                            |
|                                                             |
|  +--------------------+        +-------------------------+  |
|  | YOLOv8n 算法包      |        | ImageManager            |  |
|  | - SimpleTracker    | -----> | - 裁剪全景 [0,0,1,1]      |  |
|  | - Track 冷却去重    |        | - JPEG 编码             |  |
|  | - 单目标回调        |        | - catalog.json 跟踪     |  |
|  +--------------------+        +-------------------------+  |
|            |                                |               |
|            +---------------+----------------+               |
|                            v                                |
|                 UDS gRPC (ReportAlarm)                      |
+----------------------------|--------------------------------+
                             |
                             v
+-------------------------------------------------------------+
|                       Go 业务服务                           |
|                                                             |
|  +-------------------------------------------------------+  |
|  | ReportAdapter.AcceptAlarm                             |  |
|  | - event_id 幂等落库                                    |  |
|  | - 提取 max_confidence，JSONB 序列化 objects           |  |
|  +-------------------------------------------------------+  |
|                            |                                |
|                            v                                |
|  +-------------------------------------------------------+  |
|  | Repository & PostgreSQL (alarm_records 表)             |  |
|  +-------------------------------------------------------+  |
|                            ^                                |
|                            |                                |
|  +-------------------------+-----------------------------+  |
|  | AlarmRecordHandler & Service (RESTful API)            |  |
|  | - GET /api/record/alarms (多条件组合分页查询)           |  |
|  | - GET /api/record/alarms/:id (详情与规则快照)         |  |
|  | - GET /api/record/images/:id (受保护的图片流读取)     |  |
|  +-------------------------------------------------------+  |
+----------------------------|--------------------------------+
                             ^
                             | HTTP / JSON / Image Stream
+----------------------------|--------------------------------+
|                        Web 前端                             |
|                                                             |
|  +-------------------------------------------------------+  |
|  | /record/alarm 告警记录管理页面                         |  |
|  | - Ant Design Vue Table / Form 组合筛选                |  |
|  | - 告警详情 Drawer + Canvas/SVG 叠画标注                |  |
|  +-------------------------------------------------------+  |
+-------------------------------------------------------------+
```

## 2. 详细技术决策

### D1. 示例算法包改造 (macOS + RKNN)
- **触发冷却**：在 `InstanceContext` 中维护 `std::unordered_map<int64_t, int64_t> track_alarm_cooldown_`（记录每个 track 上次告警的毫秒时间戳）。
- **单目标回调**：遍历通过 `apply_rules` 过滤后的检测对象，对处于冷却期外的每个目标：
  1. 更新该 track 的冷却时间；
  2. 生成独立的 `algo_event_id`；
  3. 请求全景 ROI：`av_algo_image_req{ .x = 0.0f, .y = 0.0f, .w = 1.0f, .h = 1.0f, .purpose = 0 }`；
  4. 触发 `on_result` 回调。

### D2. 数据库表设计 (`000021_add_alarm_records.up.sql`)
```sql
CREATE TABLE alarm_records (
    id                BIGSERIAL    PRIMARY KEY,
    event_id          VARCHAR(200) NOT NULL,
    instance_id       VARCHAR(36)  NOT NULL,
    camera_id         VARCHAR(36)  NOT NULL,
    algorithm_id      VARCHAR(64)  NOT NULL,
    algorithm_version VARCHAR(32)  NOT NULL,
    alarm_type_id     VARCHAR(128) NOT NULL,
    occurred_at       TIMESTAMPTZ  NOT NULL,
    time_synced       BOOLEAN      NOT NULL DEFAULT TRUE,
    max_confidence    REAL         NOT NULL DEFAULT 0,
    objects_json      JSONB        NOT NULL DEFAULT '[]'::jsonb,
    image_id          VARCHAR(64)  NOT NULL DEFAULT '',
    image_rel_path    VARCHAR(255) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        BIGINT       NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX uk_alarm_records_event_id ON alarm_records (event_id, deleted_at);
CREATE INDEX idx_alarm_records_occurred_at ON alarm_records (occurred_at DESC);
CREATE INDEX idx_alarm_records_camera_id ON alarm_records (camera_id);
CREATE INDEX idx_alarm_records_algorithm_id ON alarm_records (algorithm_id);
CREATE INDEX idx_alarm_records_image_id ON alarm_records (image_id);
```

### D3. 孤儿图片对账流程 (`ReconcileOrphanImages`)
1. 引擎上报 `[]*OrphanImageEntry`；
2. Go 提取所有 `image_id`，在 `alarm_records` 中执行 `SELECT image_id FROM alarm_records WHERE image_id IN (?)`；
3. 命中的 ID 放入 `retain_image_ids`；
4. 未命中的 ID，若其 `created_at_ns` 距当前时间超过 5 分钟（防止极端时序下落库中的图片被误杀），放入 `delete_image_ids`；
5. 返回给引擎，引擎执行物理删除和 catalog 状态刷新。

### D4. 图片受控读取
- Go 业务服务提供 `/api/record/images/:id` 接口（或基于已有存储配置路径）：
  - 校验用户 Bearer 权限；
  - 根据 `image_id` 查找关联记录，获取合法 `image_rel_path`；
  - 安全检查：防止 `..` 路径穿越，从 `var/images/` 读取并返回 `image/jpeg`。

### D5. 前端详情与标注渲染
- 告警详情不仅展示告警发生时间、摄像头名称、算法名称、标签、置信度等文本信息；
- 使用自适应图片容器，在图片加载完成后获取其 `naturalWidth` 和 `naturalHeight`；
- 使用 SVG 或 Canvas 覆盖层：
  - 绘制规则多边形（如配置了 ROI / 警戒线）；
  - 绘制目标 Bounding Box（红框 + 标签 + 置信度），完全消除坐标畸变。
