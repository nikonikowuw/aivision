# 通用抓拍记录重构与三层感知-识别-告警体系演进 (Generic Capture Records & Three-Tier Perception Architecture)

## 1. Goal

在 Argus 边缘 AI 视频分析与管理系统中，将原先单一的「人脸抓拍记录」全面重构为通用的**「抓拍记录（Capture Records）」**，建立清晰标准的三层视觉分析记录体系（**抓拍感知 - 身份识别 - 安全告警**）。

本重构解决以下核心痛点：
1. **打破单一目标抓拍限制**：突破人脸专属抓拍边界，统一承载人脸（`face`）、机动车/车牌（`vehicle`）、人体/人形（`person`）、非机动车（`non_motor`）等多态视觉感知目标；
2. **人脸-人体自适应联合抓拍（Adaptive Joint Entity Capture）**：在人脸识别/人体分析算法中，实现**零漏抓**的联合感知机制——背影、低头或遮挡行人（无脸）正常按 `person` 抓拍记录衣着体态；正脸/侧脸行人自动在同条记录中补全人脸特写并触发比对，近景闸机直接作为 `face` 抓拍；
3. **多态数据建模与扩展**：采用「核心索引列 + 强类型 JSON 属性扩展」模式，使系统能在 SQLite 嵌入式存储下高效支持不同目标类型的专有特征检索（如车牌号、人脸年龄/口罩、人体服饰颜色等），无需为每个算法包频繁变更 DDL；
4. **时空事件流与轨迹观测**：确立「抓拍事件流（Event Stream / Insert Only + 智能冷却）」模型，保留完整的时间连续性与空间坐标位移，支持单镜头移动回溯与跨摄像头时空轨迹串联；
5. **存储治理与高价值识别记录绝对解耦**：深度集成系统现有的三级存储防御体系（TTL 巡检 + 85%/70% 紧急 FIFO 削峰 + 95% 熔断）。抓拍记录清理时与识别记录完全解耦（无级联删除），优先削峰淘汰未识别抓拍，保障考勤通行证据完好无损；
6. **零跳动友好前端交互（方案 A）**：采用「目标分类 Segmented 胶囊 + 自适应智能搜索框 + 折叠高级属性抽屉」，彻底消除动态表单的跳动问题，支持流式卡片墙（Card Grid）、数据表格（Table）与轨迹时间线（Timeline）多模态视图。

---

## 2. 三层记录体系定位与领域语言 (Ubiquitous Language)

| 体系分层 | 核心模型 / 页面 | 核心定位与业务语义 | 数据生成时机 | 生命周期与存储策略 |
| :--- | :--- | :--- | :--- | :--- |
| **感知层 (Perception)** | **抓拍记录 (`captures`)** | **全量视觉过客事实底座**（*画面中看到了什么目标？*） | 视频流中任意有效目标（人体/人脸/车辆）出现并满足检测与冷却阈值时生成。 | 海量高频，默认 7~15 天 TTL；85% 高水位削峰时优先淘汰。 |
| **身份层 (Identity)** | **识别记录 (`observations`)** | **静态底库比对业务**（*是谁 / 哪辆车？*） | 抓拍目标提取特征并成功比对命中人员库或车辆库（相似度 $\ge$ 阈值）。 | 高价值凭证，长周期（如 90~180 天）；文本快照独立持久化，不随抓拍物理删除而丢失。 |
| **事件层 (Event)** | **告警记录 (`alarm_records`)** | **安全规则防范**（*发生了什么违规？*） | 目标触发了行为规则（绊线、越界、明火烟雾、未戴安全帽等）。 | 独立告警留痕与 Webhook 外发，按告警策略管理。 |

---

## 3. Confirmed Facts & Technical Decisions

### 3.1 人脸-人体自适应联合抓拍规范 (Adaptive Joint Entity Capture)
在人脸识别与人体分析流水线（YOLOv8n + SCRFD）中，按目标可见度自适应落库，确保**零漏抓**：

| 感知场景 | `target_type` | 主特写图（`crop_path`） | 附属人脸特写（`face_crop_path`） | `bbox_json` 结构 | 业务语义与能力 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **① 有身无脸（背影 / 低头 / 远景）** | `person` | 🚶 全身站姿切图 | `null` (或空) | `{"person_bbox": [x, y, w, h]}` | 记录过客身体、衣着颜色与时空轨迹（**100% 杜绝漏抓**） |
| **② 有身有脸（正脸 / 侧脸同行）** | `person`（联合）| 🚶 全身站姿切图 | 👤 高清人脸特写切图 | `{"person_bbox": [...], "face_bbox": [...]}` | 一体化表达：全身衣着 + 人脸特征 + 身份比对 |
| **③ 纯人脸（近景门禁 / 闸机）** | `face` | 👤 人脸特写切图 | `null` | `{"face_bbox": [x, y, w, h]}` | 专注近距离面部识别 |
| **④ 车辆 / 车牌** | `vehicle` | 🚗 车辆全景切图 | 🪪 局部高清车牌切图 | `{"vehicle_bbox": [...], "plate_bbox": [...]}` | 车辆特征 + 车牌 OCR |

### 3.2 数据表模型与多态字段设计 (Schema & Polymorphism)
- 废弃原 `face_captures`，新建通用的 `captures` 表：
  - **基础公共索引列**：`id`, `event_id` (全局唯一), `target_type` (枚举索引: `face`|`person`|`vehicle`|`non_motor`), `track_id`, `camera_id`, `camera_name`, `task_id`, `algorithm_id`, `algorithm_version`, `captured_at` (毫秒时间戳), `confidence`, `quality_score`, `is_recognized` (布尔索引);
  - **视觉图片列**：`panorama_image_id`, `panorama_path`, `crop_image_id`, `crop_path` (主特写), `sub_crop_image_id`, `sub_crop_path` (附属人脸/车牌特写), `bbox_json` (多目标归一化坐标);
  - **异构属性列 (`attributes_json`)**：强类型结构化存储不同算法专有特征，支持 SQLite `json_extract` 检索：
    - `face`: `{"gender": "male", "age": 28, "mask": false, "glasses": true, "pitch": 0.1, "yaw": -0.2, "roll": 0.0}`
    - `person`: `{"upper_color": "black", "lower_color": "blue", "hat": false, "bag": true, "has_face": true, "face_attributes": {...}}`
    - `vehicle`: `{"plate_number": "京A88888", "plate_color": "blue", "vehicle_type": "sedan", "vehicle_color": "white", "brand": "audi"}`

### 3.3 抓拍事件流与轨迹回溯 (Event Stream & Trajectory)
- **事件流落库**：采用 `Insert Only` 抓拍事件流模型，结合算法端智能冷却（同目标 2~3 秒内仅在姿态/画质显著改善时抓拍），记录精确的时空点位 $(x, y, t)$。
- **轨迹观测**：以时间序列（`captured_at ASC`）为基础，前端提供「以图搜轨迹」或「按车牌/属性检索」，一键串联目标在各点位的移动时间线。

### 3.4 存储治理与解耦保护 (Storage & Decoupled Retention)
- **数据完全解耦**：`observations`（识别记录）通过 `capture_id` 软引用抓拍，**禁止数据库级联删除（No ON DELETE CASCADE）**。
- **85% 削峰优先级（Priority FIFO）**：磁盘 $\ge 85\%$ 时，`cleanCaptureBatch` 优先批量删除 `is_recognized = false` 的陌生人/普通过客抓拍（占 90%+ 存储），未识别抓拍清空后才按 FIFO 削峰历史抓拍。
- **底库白名单**：`persons` / `person_faces` 注册图拥有绝对免死保护。
- **优雅降级**：抓拍图片到期清理后，识别记录文本完好保留，抓拍原图占位提示「已过期清理」，底库图照常展示。

### 3.5 前端交互规范（方案 A：胶囊分类 + 智能搜索框）
- **分类胶囊 (Segmented)**：`[ 全部 ]` `[ 👤 人脸 ]` `[ 🚗 机动车 ]` `[ 🚶 行人 ]` 常驻顶部。
  - 切 `[ 🚶 行人 ]`：展示全量人体抓拍（包含背影、侧身与正脸）；
  - 切 `[ 👤 人脸 ]`：精准筛选出露脸的人脸抓拍（包含纯人脸与人脸人体联合抓拍中的人脸特写）。
- **自适应智能搜索框**：根据所选分类自动切换 Placeholder（如切机动车显示 `"输入车牌号 (如: 京A88888)"`，切人脸显示 `"输入抓拍 ID..."`），输入框与查询按钮位置绝对静止，零 Layout Shift。
- **视图多模态**：默认卡片流（Card Grid，悬浮放大 + ROI 标框），支持一键切换数据表格（Table）与时空轨迹（Timeline）；点击弹出详情抽屉（Detail Drawer，三图联动：全景多框 + 全身特写 + 人脸特写）。

### 3.6 API 与 gRPC IPC 协议升级
- API 统一为 `/api/record/captures`（分页、详情、安全图片流），权限码 `record:capture:read`。
- gRPC 协议将 `ReportFaceCapture` / `FaceCapture` 升级为通用 `ReportCapture(CaptureEvent)`。

---

## 4. Requirements & Implementation Plan

### R1. 数据表迁移与 GORM 模型 (Database & Models)
- **M1.1**: 新增版本化 SQL 迁移脚本 `000037_create_generic_captures.up.sql` / `.down.sql`：
  - 创建 `captures` 表，建立 `idx_captures_event_id` (Unique), `idx_captures_target_type`, `idx_captures_captured_at`, `idx_captures_camera_id`, `idx_captures_task_id`, `idx_captures_is_recognized` 索引；
- **M1.2**: 新增 `000038_seed_generic_capture_menu.up.sql` / `.down.sql`：
  - 更新菜单路径为 `/record/capture`，组件 `RecordCapture`，标题 `menu.record.captures`，权限码 `record:capture:read`；
- **M1.3**: 在 `app/internal/model/capture.go` 中定义 `CaptureRecord` GORM 模型与 JSON 属性结构体（`FaceAttributes`, `VehicleAttributes`, `PersonAttributes`，支持联合人脸属性嵌套）。

### R2. gRPC IPC 协议升级 (Protobuf & Engine)
- **P2.1**: 修改 `engine/proto/argus/v1/app.proto`：
  - 定义通用 `CaptureEvent` 结构体（包含 `target_type`, `bbox`, `sub_bbox`, `confidence`, `quality_score`, `attributes_json`, `image_id`, `crop_image_id`, `sub_crop_image_id` 等）；
  - 定义 `rpc ReportCapture(ReportCaptureRequest) returns (ReportCaptureResponse)`；
  - 重新生成 Go 与 C++ 的 Protobuf 代码。
- **P2.2**: 改造 C++ Engine (`engine/src/core/ipc/uds_server.cpp`) 与算法包（`face_recognition`）：
  - 在 YOLOv8n + SCRFD 联合流水线中，支持自适应生成 `person`（带或不带人脸特写）及 `face` 的 `CaptureEvent` 并统一通过 IPC 上报。

### R3. Go 后端业务逻辑与存储清理适配 (Backend Services)
- **B3.1**: 新增 `app/internal/repository/capture_repository.go`：
  - 实现通用分页查询（支持 `target_type`、时间范围、摄像头、`attributes_json` 关键词检索）、详情查询、以及分批过期清理 `FindExpired`、`FindOldestUnrecognized`、`HardDeleteBatch`；
- **B3.2**: 新增 `app/internal/service/capture_service.go`：
  - 实现 `CaptureService`（分页、详情、安全图片流读取 `ReadImageStream`，支持全景大图、主特写图、附属特写图）；
- **B3.3**: 改造 `app/internal/service/report_adapter.go`：
  - 实现 `AcceptCapture(ctx, event)` 幂等落库，解析属性并写入 `captures` 表；若关联识别记录则标记 `is_recognized = true`；
- **B3.4**: 升级 `app/internal/service/storage_cleanup.go`：
  - 将 `cleanFaceCaptureBatch` 替换为 `cleanCaptureBatch`，支持未识别抓拍优先淘汰机制；
- **B3.5**: 新增 `app/internal/api/capture.go` 并注册到 Gin Router (`internal/router/router.go`)。

### R4. 前端 Vue 页面与组件改造 (Frontend UI)
- **F4.1**: API 契约：更新 `apps/web-antd/src/api/core/capture.ts`，支持 `CaptureQuery`、`CaptureItem` 与多态属性类型；
- **F4.2**: 路由与国际化：更新路由配置、权限码映射以及中英文多语言包（`menu.record.captures` 等）；
- **F4.3**: 抓拍记录主页面 `apps/web-antd/src/views/record/capture/index.vue`：
  - 顶部实现 **方案 A（Segmented 胶囊 + 自适应智能搜索框 + 更多筛选折叠面板）**；
  - 核心展示区实现**流式卡片网格（Card Grid）**，卡片自适应渲染人脸/车辆/人体 Tag 与双特写缩略图；
  - 右上角支持卡片网格与 VxeGrid 数据表格一键切换；
- **F4.4**: 详情抽屉 `CaptureDetailDrawer.vue`：
  - 高清全景大图预览（带目标高亮 ROI 标框与缩放交互，支持人脸+人体双框同时标注）；
  - 目标特写抠图与对应分类的属性面板展示（全身切图 + 人脸切图联动）；
  - 提供「以图搜轨迹 / 查看前后时空抓拍」快捷入口。

---

## 5. Acceptance Criteria

- [ ] **数据库与模型**：执行 `make migrate-up` 后顺利生成 `captures` 表及其全套索引，菜单种子正常植入，GORM 单元测试覆盖全部 CRUD 与 JSON 属性解析。
- [ ] **自适应人体-人脸联合抓拍**：
  - 背影、低头无脸行人能够 100% 成功落库为 `target_type=person`（全身特写切图有效，`face_crop_path` 为空）；
  - 正脸/侧脸行人能够成功联合落库（全身切图与人脸切图均有效，双 ROI 标框完整）；
  - 纯人脸近景能够成功落库为 `target_type=face`。
- [ ] **gRPC IPC**：Engine 与 Go 之间的 `ReportCapture` 通信畅通，`go test ./internal/pkg/engineipc/...` 契约测试全部通过。
- [ ] **存储三级防御与解耦**：
  - 执行抓拍清理时，`observations`（识别记录）零数据受损；
  - 85% 高水位削峰时，系统严格优先淘汰 `is_recognized = false` 的未识别抓拍；
  - 人员底库图片受到白名单绝对保护。
- [ ] **后端 API 健全性**：`GET /api/record/captures` 支持多分类筛选与车牌/属性关键词检索，`go test ./internal/service/...` 覆盖率 $\ge 85\%$。
- [ ] **前端体验**：
  - 顶部分类胶囊切换流畅，输入框 Placeholder 动态切换且无任何 Layout Shift；
  - 卡片网格能自适应呈现人脸、机动车、人形等多态目标缩略图与属性标签；
  - 详情抽屉能正确绘制全景大图上的目标 ROI 标框（支持人脸+人体双框联动）。
- [ ] **质量门禁**：通过 `make vet`、`make test`（后端）以及 `pnpm check`（前端）。
