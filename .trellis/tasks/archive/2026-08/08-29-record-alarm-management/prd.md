# 告警记录模块端到端开发 PRD

## 1. Goal

打通 AI 视频分析边缘平台的「目标检测告警记录」端到端闭环：
从 C++ 推理引擎产生单目标告警事件及全景截图、经 UDS gRPC 异步上报至 Go 业务服务、完成幂等持久化与孤儿图片双向对账、提供受控图片安全读取与结构化分页查询 API、并在管理前端 `/record/alarm` 页面展示告警列表、告警详情及画框标注（全景底图叠加规则区域与目标框），完成 YOLOv8n 端到端实测验收。

## 2. Scope & Boundaries

### In Scope
1. **算法包改造**：YOLOv8n 示例包（macOS/arm64 与 RKNN/rk3576）支持单目标单事件回调、全景大图请求（`[0,0,1,1]`）以及基于 `track_id` 的事件冷却去重（默认 5s）。
2. **Go 数据持久化**：
   - 数据库迁移新增 `alarm_records` 表（单表内联设计，支持毫秒软删/硬删与 `event_id` 唯一索引）。
   - 编写 GORM Model、Repository 与 Service 层，处理告警落库与多维组合查询。
3. **Engine IPC 闭环**：
   - `ReportAdapter.AcceptAlarm` 实现：接收 C++ `AlarmEvent`，基于 `event_id` 幂等落库，解析 objects、计算 `max_confidence` 并持久化。
   - `ReportAdapter.ReconcileOrphanImages` 实现：根据 `image_id` 批量反查数据库，判定保留（retain）或物理删除（delete），解决写图成功但落库失败的孤儿文件泄漏问题。
4. **图片安全读取**：
   - 提供签名/鉴权图片访问能力，防止未授权路径遍历或越权读取。
5. **后端 API & 权限**：
   - 告警记录分页查询、详情查看 API。
   - 数据库迁移新增「智能记录 / 告警记录」菜单与按钮权限种子数据（`record:alarm`, `record:alarm:query` 等）。
6. **管理前端**：
   - 实现 `/record/alarm` 告警记录页面（表格、多维筛选表单、分页）。
   - 实现告警详情 Drawer/Modal：展示元数据、目标属性，并在全景图上通过 Canvas/SVG 正确叠加绘制「规则多边形区域（黄色）」与「目标边界框（红色）」。

### Out of Scope
- 人脸抓拍记录（`capture`）与人脸识别记录（`recognition`）的事件生产与库表（等待人脸子系统就绪）。
- Webhook 平台对外分发与重试推送。
- 7 天留存与高低水位 FIFO 磁盘清理调度（归属存储管理子系统）。

## 3. Requirements

### 3.1 告警事件生产与上报规范
- 算法包对每个触发规则的目标独立生成一条告警事件与唯一 `event_id`。
- 截图请求为全景画面（`[0, 0, 1, 1]`），引擎保存为 JPEG 并记录在 `catalog.json`。
- 引擎通过 `ReportService.ReportAlarm` 上报给 Go；Go 处理成功后返回 `CodeOK`，引擎将图片置为 `reported`。

### 3.2 存储与查询模型
- 表名 `alarm_records`：
  - `id`: BIGSERIAL 主键。
  - `event_id`: VARCHAR(200) 唯一索引（`<instance_run_id>/<algo_event_id>`）。
  - `instance_id`, `camera_id`, `algorithm_id`, `algorithm_version`, `alarm_type_id`。
  - `occurred_at`: TIMESTAMPTZ，发生时间（支持倒序索引）。
  - `time_synced`: BOOLEAN，时间同步标志。
  - `max_confidence`: REAL，用于置信度范围筛选。
  - `objects_json`: JSONB，包含单个目标的 label, confidence, bbox, track_id。
  - `image_id`, `image_rel_path`: 关联图片标识与相对路径。
- 查询支持组合条件：
  - 时间范围（`start_time`, `end_time`）
  - 摄像头（`camera_id`）
  - 任务（`camera_id` 下的任务）
  - 算法（`algorithm_id`）
  - 事件类型（`alarm_type_id`）
  - 置信度区间（`min_confidence`, `max_confidence`）
  - 分页（Page, PageSize）按 `occurred_at DESC` 排序。

### 3.3 孤儿图片对账（ReconcileOrphanImages）
- 引擎定期或启动时上报 `unreported` 的孤儿图片列表。
- Go 批量反查 `alarm_records` 表：
  - 命中 `image_id` 则指示引擎 `retain_image_ids`（并更新 catalog 为 reported）。
  - 未命中且创建时间超过保护窗口（如 5 分钟）则指示引擎 `delete_image_ids` 物理删除。

### 3.4 前端界面与标注
- 页面遵循 Vben 5 / Ant Design Vue 规范，接入路由与 RBAC 权限。
- 详情弹窗具备自适应 Canvas/SVG 容器，将归一化坐标 `[0.0, 1.0]` 映射到全景图真实渲染尺寸，高亮显示目标框和触发的规则区域。

## 4. Acceptance Criteria

- [ ] YOLOv8n 示例包对每个规则触发目标独立回调，并带有基于 track_id 的冷却去重。
- [ ] 引擎上报 `AlarmEvent`，Go 成功持久化到 `alarm_records`，重复 `event_id` 返回成功且不产生脏数据。
- [ ] 孤儿图片对账 RPC 能够正确识别落库与未落库图片，成功返回保留与清理列表。
- [ ] 后端单元测试覆盖 Repository、Service、ReportAdapter 与 Controller。
- [ ] 前端 `/record/alarm` 页面成功渲染，支持多条件筛选、分页与重置。
- [ ] 告警详情能正确展示全景图，并在图上准确叠加目标框和规则区域。
- [ ] `make -C app test` 与 `pnpm --filter web-antd check` 全绿。
