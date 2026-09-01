# 人脸特征下发与识别记录

## Goal

打通「已注册人脸样本 → Engine 底库 → 实时流 1:N 比对 → 识别记录落库 → 管理端查询」的完整闭环。

上游 `09-01-person-face-registration`（注册与特征提取）与 `09-01-main-stream-face-recognition`（主流接入与算法包出特征）已完成；本任务补齐中间缺失的下发、比对与记录三段。

## Confirmed Facts

### 上游现状（实现前已验证）

- 算法包 `face_recognition` 已在实时流产出 `AV_RESULT_RECOGNITION`，JSON 含 `persons[].track_id`、`face.bbox`、`face.landmarks`、`face.embedding.data`（512 维 float32、L2 归一化、小端、Base64），格式见 `.trellis/spec/engine/manifest-schema.md` §4.3。
- 算法包在存在 embedding 时已主动构造 `av_algo_image_req`（`purpose = kImagePurposeFaceCrop`）请求 Engine 裁剪人脸 ROI（`algo-packages/macos/arm64/face_recognition/src/core/algo_entry.cpp:697-712`）。
- 算法包已实现轨迹级限流：`track_confirm_frames=2` 防抖、`min_face_size=40` 与 `quality_threshold=35` 门槛、`max_recognitions_per_track=3` 上限、`reextract_interval_frames=45` 与 `quality_update_margin=10` 重采样（`src/core/config.hpp:13-27`）。**Engine 每条轨迹最多收到 3 次带 embedding 的结果。**
- `engine/src/core/ipc/uds_server.cpp:1699` 的 `AV_RESULT_RECOGNITION` 分支目前只路由 `license_plate_recognition`，**人脸结果被静默丢弃**。
- `person_faces` 表已存 `embedding BYTEA`、`face_id`、`person_id`、`algorithm_id/version`、质量分与两张图 key（`argus/migrations/000028`）；`persons.primary_face_id` 已加（`000030`）。
- Engine 主循环每 2 秒调用 `get_desired_state(applied_revision)` 拉取期望状态（`engine/src/app/main.cpp:248`、`:313` 的 20×100ms 轮询）；Go 侧用 `BumpRevisionTx` 在业务事务内递增 revision。
- `AlgorithmInstanceConfig.motion_gate`（proto）+ `algorithm_instances.motion_gate_json`（DB `000023`）+ `InstanceFormModal.vue`（前端）是「per-instance 引擎级配置、不进 `params_json`」的完整先例。
- `plate_observations`（表 `000024` + `ReportPlateObservation` + `/record/plates` 四个端点 + 权限码 `record:plate` + `views/record/plate/`）是识别记录模块的完整先例。
- Go 侧现有上报语义为「重复 `event_id` 幂等丢弃」（`report_adapter.go:262`、`:346`）。
- Engine 上报带重试队列（`uds_server.cpp:1155-1172`），**失败事件重新入队会导致乱序到达**。

### 本任务确认的产品与技术边界

- **比对在 Engine 内执行**。算法包只出特征、不做比对，C ABI 与算法包本任务零改动。
- **底库为全局单库**，不做按摄像头/分组的布控范围划分。
- **底库由 Engine 主动拉取**（`ControlPlaneService` 新增 RPC），不用 Go 推。Engine 重启后 revision 归零自动触发全量重拉，无需重启探测。
- **底库全量替换 + `gallery_revision` 对账**，版本未变返回空响应。不做增量同步。
- **底库条目为 face-level**（每张注册脸一条），命中取最高相似度条目的 `person_id`。不做 person 级平均向量聚合，不做 top-k 投票。
- **命中判定只用阈值**，不加次高分 margin 检查。
- **相似度全链路使用归一化值 `[0,1]`**，公式 `normalized = (cos + 1) / 2`。DB 列、proto 字段、前端展示一律为归一化值，不出现原始 cosine。
- **阈值为 per-instance 配置**，默认 `0.7`（归一化）≡ `0.4`（原始 cosine）。
- **记录粒度为 track 级、更优则覆盖**：`event_id = <instance_run_id>/<track_id>`，相似度提升 ≥ `0.03`（归一化）才重新上报，Go 侧按 `event_id` upsert。
- **upsert 必须带单调条件**（`WHERE similarity < ?`），防止重试队列乱序导致高分记录被低分覆盖。
- **允许身份跳变**：覆盖时 `person_id` 可变（只会向更高相似度方向变）。
- **未命中（陌生人）不落记录**，但 `person_id` 列允许空串以便后续扩展。
- **每条记录存两张图**（全景 + 人脸特写），覆盖时一并换新图；旧图不主动删除，交由现有 `ReportOrphanImages` / `ReconcileImages` 对账链路回收。
- **底库硬上限 5000 条目**（MVP），超限在 Go 侧注册时拒绝，不在下发时截断。
- **记录冗余存 `person_name` 与 `camera_name`**，作为历史事实快照。
- 本期不做开放 API（`/api/v1/face-observations`）。

## Requirements

- R1. Go 提供全局人脸底库查询与下发能力，Engine 通过新增的 `ControlPlaneService` RPC 按 `gallery_revision` 对账拉取；版本未变时响应体不含任何向量数据。
- R2. 底库内容变更（人脸注册、人脸删除、人员删除级联）必须在同一业务事务内递增 `gallery_revision`，避免「数据已改但版本未增」导致 Engine 永不感知。
- R3. Engine 在内存中维护 face-level 底库索引，拉取成功后原子替换；拉取失败时保留旧底库（fail-static），不清空。
- R4. Engine 冷启动且底库尚未就绪时，收到的人脸结果直接丢弃，不缓存、不排队、不上报。
- R5. Engine 解析 `face_recognition` 的 `AV_RESULT_RECOGNITION` 结果，对每个含 embedding 的人脸执行 1:N 归一化相似度比对，取最高分条目；最高分 ≥ 该实例阈值时判定命中。
- R6. 相似度阈值为 per-instance 配置，经 proto `AlgorithmInstanceConfig` 走现有 DesiredState 通道下发，不进入算法 `params_json`。
- R7. Engine 按 `instance_run_id + track_id` 维护上报状态：首次命中上报一条记录；后续同 track 命中的相似度提升 ≥ 0.03 时，以同一 `event_id` 携带新抓拍图重新上报。
- R8. 抓拍图只在判定「确实需要上报」之后才裁剪与落盘，不得每帧预先抓图。
- R9. Go 接收人脸识别记录上报并按 `event_id` upsert，且 upsert 必须带 `similarity` 单调递增条件；不满足条件时视为幂等成功。
- R10. 提供人脸识别记录的分页查询、详情查询与两个受保护图片流端点，权限码与路由结构对齐现有 `/record/plates`。
- R11. 管理端提供识别记录列表页（含人员/摄像头/时间范围筛选、抓拍图预览）与算法实例的阈值配置项，三语 i18n 完整。
- R12. 特征向量属于敏感生物识别数据：日志、列表响应与错误信息不得泄露原始 embedding。

## Constraints

- 生产 schema 变更必须使用版本化 migration；AutoMigrate 仅供 SQLite 单测。
- 本任务不修改 C ABI、不修改算法包、不需要执行 `sync-sdk.sh` / `check-consistency.sh` 契约同步。
- 不改动 `alarm_records` / `plate_observations` 现有的「重复 event_id 幂等丢弃」语义；upsert 是人脸记录独有的新分支。
- 阈值默认值 `0.7`（归一化）为初始猜测，**必须在真实现场实测校准**，不得作为已验证结论对外承诺。
- 两端 gRPC max message size 需显式设置为 32MB 以容纳 5000 条目底库（约 11MB）。

## Acceptance Criteria

- [ ] AC1. 注册/删除人脸后，`gallery_revision` 在同事务内递增；Engine 在 ≤ 2 秒内拉到新底库并完成原子换库，日志可见加载条目数与版本号。
- [ ] AC2. `gallery_revision` 未变化时，`GetFaceGallery` 响应不含任何 embedding 数据。
- [ ] AC3. Go 服务不可用期间，Engine 保留旧底库并继续正常识别；Go 恢复后自动重新对账。Engine 重启后无需任何外部触发即可恢复完整底库。
- [ ] AC4. 底库达到 5000 条目上限后，继续注册人脸返回明确的业务错误码，不产生「注册成功但识别不到」的状态。
- [ ] AC5. 已注册人员出现在视频流中时生成识别记录，`person_id`/`person_name`/`camera_name`/`similarity`/两张抓拍图齐全，且 `similarity` 为 `[0,1]` 归一化值。
- [ ] AC6. 同一 track 内多次识别只产生一条记录；相似度提升 ≥ 0.03 时记录被更新为更高相似度与新抓拍图。
- [ ] AC7. 单调 upsert 有单元测试覆盖：先落 0.91、后到 0.65 的乱序上报不得覆盖高分记录。
- [ ] AC8. 未注册人员出现在视频流中时不产生任何记录与抓拍图。
- [ ] AC9. 阈值可按实例独立配置并生效，同一人在不同阈值的实例下判定结果可不同。
- [ ] AC10. 管理端可分页查询、筛选识别记录并预览两张抓拍图；三语文案完整。
- [ ] AC11. `cd argus && go test ./...`、`make -C engine test`、`make -C engine asan`、`make -C engine lint`、`pnpm check` 全部通过。

## Out of Scope

- 按摄像头/分组的布控范围划分（`face_groups` 与关联表）。
- 增量底库同步、底库分页拉取、50000 条目规模支撑（后续独立任务）。
- 陌生人抓拍与陌生人告警。
- 识别记录/告警记录/车牌记录的数据保留与自动清理策略（三表统一处理，另立任务）。
- 开放 API `/api/v1/face-observations`。
- 底库为空时关闭算法包特征提取以省算力的动态开关。
- 次高分 margin 判定、top-k 投票、person 级向量聚合。

## Known Issues（本任务不修，但需记录）

- **gRPC 默认 4MB 接收上限**：`argus/internal/pkg/engineipc/runtime.go:36` 与 `engine/src/core/ipc/uds_server.cpp:2519` 两端均未设置 max message size。`person-face-registration` PRD 声明的「注册图片 ≤ 10 MiB」实际会在 gRPC 传输层被拒（`RESOURCE_EXHAUSTED`），进不到业务校验。本任务因为要设置 32MB 上限会顺带缓解该路径，但**注册链路的上限一致性属于上游任务的缺陷**。
- **记录表无保留策略**：`alarm_records` 与 `plate_observations` 均无数据清理机制，在边缘设备上会无限增长。本任务新增的记录表对齐现状，统一治理另立任务。
