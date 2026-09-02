# 人脸抓拍记录与多帧时序演进 (Face Captures & Snapshot Sequences)

## Goal

在 Argus 边缘 AI 视频分析系统中建立**「全量人脸抓拍与多帧时序演进（Face Captures & Snapshots Sequence）」**体系，解决当前系统仅保留高分识别记录、缺乏人脸运动前后时序关系、无法留存陌生人轨迹的缺陷。

实现：
1. **全量人脸目标抓拍**：不论是否命中人脸底库（包含陌生人与已知人员），均对画面中的人脸 Track 保留完整抓拍快照；
2. **Track 级时序演进（1~5 组快照胶卷）**：同一 Track 在生命周期内按时间步长与质量提升保存最多 5 组全景大图与特写小图，还原人员进入、正脸、离开的前后运动轨迹；
3. **跨阈值识别晋升**：全量抓拍作为底层事实流水；当 Track 最佳相似度突破阈值时，自动生成/更新人脸识别通行记录（`face_observations`），且两表物理图片零冗余共享；
4. **前端交互与管理**：新增「人脸抓拍记录」菜单，提供时序胶卷抽屉（Filmstrip Drawer）、全景 BBox 联动高亮、陌生人一键建档与识别记录双向穿透能力。

---

## Confirmed Facts & Technical Decisions

### 1. 业务定位与数据流切分
- **抓拍记录（`face_captures`）**：关注“谁在什么时间出现在哪个镜头”。全量接收视频流中有效人脸 Track，记录 1~5 组多快照时序演进（陌生人与已知人员均入库）。
- **识别记录（`face_observations`）**：关注“底库人员通行与比中”。仅当 Track 最佳相似度 $\ge \text{threshold}$ 时生成/更新一条通行记录。
- **关联机制**：抓拍记录与识别记录共用同一 `event_id`（`<instance_run_id>/<track_id>`），实现双向索引与页面穿透。

### 2. 采样策略与存储治理
- **快照上限**：单个 Track 最多保存 **5 组快照**（Snapshot Item）。
- **采样触发**：首次检测到合格人脸即刻保存第 1 组；后续当时间步长 $\ge 800\text{ms} \sim 1000\text{ms}$ 且姿态/清晰度良好，或人脸质量分/相似度有显著跃升（Margin $\ge 0.05$）时，生成后续快照，直到达到 5 组上限。
- **每组快照内容**：全景背景大图 JPEG + 局部人脸特写小图 JPEG + 时间戳 + BBox 坐标 + 质量分 + 相似度。
- **零冗余物理文件复用**：`face_observations` 记录直接引用对应 Track 最佳匹配帧（Best Match Frame）的磁盘相对路径，磁盘零重复写入。

### 3. 数据表结构与 SQLite 优化
- 采用 **Track 单表聚合 + 结构化 Snapshots JSON** 模型。
- 检索与过滤的核心字段（镜头、时间、最佳相似度、最佳人员、最佳全景/特写路径）作为原生数据库列并建立 B-Tree 索引，列表页查询**零 JSON 解码损耗**。
- 1~5 组快照时序细节保存在 `snapshots_json` 字段中，在 SQLite 层面避免主从表 JOIN、页分裂与复杂的级联清理，实现单行原子 Upsert。

### 4. 引擎上报与时序通信
- **实时增量上报（Monotonic Upsert）**：C++ 媒体引擎捕获到首帧快照即刻触发 gRPC `ReportFaceCapture`，后续快照增量追加（Append），达到零延迟实时大屏体验与掉电容灾。
- 相似度突破阈值时，引擎同时触发/更新 `ReportFaceObservation`。

---

## Requirements

### R1. 数据表与迁移 (Database & Models)
- 新增 `000034_add_face_captures.up.sql` / `.down.sql` 生产表迁移与 GORM 模型 `FaceCapture`：
  - `event_id`：`<run_id>/<track_id>`，带 `deleted_at` 唯一复合索引；
  - 一级列：`camera_id`、`camera_name`、`algorithm_id`、`algorithm_version`、`track_id`、`best_similarity`、`best_quality_score`、`best_person_id`、`best_person_name`、`best_image_rel_path`、`best_face_rel_path`、`best_bbox_json`、`snapshot_count`、`first_observed_at`、`last_observed_at`；
  - JSON 列：`snapshots_json`（保存 1~5 组包含 `snapshotIndex`、`wallTimeNs`、`observedAt`、`imageRelPath`、`faceImageRelPath`、`bbox`、`qualityScore`、`similarity`、`personId`、`personName` 的结构化数组）。
- 新增 `000035_seed_face_capture_menu.up.sql` / `.down.sql` 菜单与权限种子：
  - 菜单路径 `/record/captures`，标题 i18n key `menu.record.faceCaptures`，挂载于「记录管理（record）」目录下；
  - 权限码 `record:capture:query`。

### R2. gRPC 通信契约扩展 (Protobuf)
- 在 `proto/argus/v1/app.proto` 中定义：
  - `message FaceCaptureSnapshot`（包含快照序号、时间戳、BBox、质量分、相似度、人员匹配信息、大图与小图相对路径）；
  - `message FaceCapture`（包含 event_id、instance/camera 元信息、track_id 及本次快照 `snapshot`）；
  - `rpc ReportFaceCapture(ReportFaceCaptureRequest) returns (ReportFaceCaptureResponse)`。
- 两端重新生成 Protobuf / gRPC 代码。

### R3. C++ 媒体推理引擎改造 (Engine IPC & Pipeline)
- 在 `engine/src/core/ipc/uds_server.cpp` 中扩展人脸跟踪上报状态机 `face_track_states_`：
  - 记录每个 Track 的 `snapshot_count`、`last_snapshot_wall_time_ns`、`best_quality_score`、`best_similarity`；
  - **全量抓拍抽样**：对所有检测跟踪到的人脸（不论 `similarity` 大小），在首帧、时间间隔 $\ge 800\text{ms}$ 且质量合格、或质量显著提升时，编码大图与特写并发出 `ReportFaceCapture`，最多记录 5 组；
  - **识别通行联动**：当 `similarity >= threshold` 且较上次上报提升 $\ge 0.03$ 时，组装 `FaceObservation` 发出 `ReportFaceObservation`，图片路径复用本次抓拍生成的路径。

### R4. Go 后端服务与增量单调 Upsert (Backend Service)
- 实现 `FaceCaptureRepository.UpsertIncremental(ctx, capture)`：
  - 若 `event_id` 不存在：插入新记录，`snapshot_count = 1`，初始化 `snapshots_json` 为 1 元素数组；
  - 若 `event_id` 已存在且 `snapshot_count < 5`：原子追加新快照到 `snapshots_json`，`snapshot_count++`，若新快照质量或相似度更高，则单调更新 `best_*` 列，更新 `last_observed_at`。
- 实现 `FaceCaptureService` 与 HTTP API 接口：
  - `GET /api/v1/record/captures`：多维度分页查询（支持 `cameraId`、`status` [all/recognized/stranger]、`minSimilarity`、`maxSimilarity`、`timeRange` 过滤）；
  - `GET /api/v1/record/captures/:id`：获取抓拍详情与完整 `snapshots` 列表；
  - `GET /api/v1/record/captures/:id/snapshots/:index/panorama`：安全受控读取指定快照全景大图；
  - `GET /api/v1/record/captures/:id/snapshots/:index/face`：安全受控读取指定快照特写小图。

### R5. 前端抓拍记录与时序胶卷 (Frontend View)
- 在 `ui/apps/web-antd/src/views/record/capture/` 新增页面：
  - `index.vue`：VxeGrid 高性能表格，展示抓拍 ID、最佳特写、最佳全景、识别状态（Tag：`已识别: 张三 (0.92)` / `陌生人`）、快照帧数（Tag：`3 帧快照`）、镜头名、抓拍时间、操作按钮；
  - `CaptureFilmstripDrawer.vue`（时序胶卷抽屉）：
    - **顶部**：大画面预览区，显示当前选中帧的 1080P 全景图（叠加绿色 BBox 目标框）与局部特写对比；
    - **底部**：横向时间轴胶卷（Filmstrip），平铺展示 1~5 张时序小卡片（显示序号 `Frame #1`、时间戳、质量分、相似度）；
    - **交互**：点击任意胶卷卡片，顶部全景大图与 BBox 实时切换联动；
  - **陌生人一键快速建档**：陌生人抓拍点击「快速建档」，打开人员新增抽屉并自动将该抓拍的最佳特写图作为底库特征样本。

---

## Constraints

1. **零破坏性改动**：现有 `face_observations` 识别记录表结构与 API 契约保持完全向后兼容。
2. **存储保护**：单 Track 快照上限严格限制为 $\le 5$ 组，杜绝突发长时停留导致的磁盘 I/O 和存储爆满。
3. **安全规范**：生产环境表结构变更严格走版本化 SQL 迁移脚本；图片流读取必须通过后端鉴权与路径合法性校验。
4. **性能规范**：抓拍主列表查询不得在 SQL 中使用 `LIKE` 查询 JSON，必须使用原生索引列完成高并发过滤。

---

## Acceptance Criteria

- [ ] **AC1 (迁移与模型)**：执行 `make migrate-up` 成功创建 `face_captures` 表与菜单权限数据，各索引与约束正确建立。
- [ ] **AC2 (契约与编译)**：Protobuf 新增 `ReportFaceCapture` 契约，C++ 与 Go 编译无告警。
- [ ] **AC3 (全量陌生人抓拍)**：未注册人脸（陌生人）在视频流中移动时，生成包含 1~5 组快照的 `face_captures` 记录，`best_person_id` 为空，不产生 `face_observations` 记录。
- [ ] **AC4 (已知人员抓拍与识别双落库)**：已注册底库人员在视频流中移动时，生成包含 1~5 组快照的 `face_captures` 记录，且同时生成 `face_observations` 识别记录；两表图片路径完全一致（零冗余写盘）。
- [ ] **AC5 (增量多快照追加与上限)**：目标在画面持续移动时，抓拍记录的 `snapshot_count` 随时间步长从 1 递增至最多 5 组，`snapshots_json` 包含各时刻的时间戳与坐标，超过 5 组后平稳停止追加。
- [ ] **AC6 (时序胶卷抽屉交互)**：管理端打开抓拍详情抽屉，能横向查看 1~5 张胶卷缩略图，点击不同帧时，主预览区的全景大图与人脸框高亮正确联动更新。
- [ ] **AC7 (陌生人快速建档)**：在抓拍列表对陌生人点击「快速建档」，成功带入人脸样本图并完成人员库注册；注册完成后该人员在后续视频流中能被正常识别。
- [ ] **AC8 (质量门禁)**：`cd argus && go test ./...`、`make -C engine test`、`make -C engine asan`、`make -C engine lint`、`pnpm check` 全部绿色通过。

---

## Out of Scope (后续迭代)

- 陌生人高频出现轨迹聚集与“以图搜图”（ReID 跨镜头检索，另立任务）；
- 抓拍记录自动化生命周期淘汰（与告警、车牌统一配置定时滚动清理）；
- 开放第三方 HTTP Webhook 实时抓拍推送。
