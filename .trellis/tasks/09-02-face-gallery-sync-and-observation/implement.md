# 实施计划

三阶段串行推进，每阶段有独立可验证的门禁。阶段间存在强依赖，不拆分为子任务。

## 阶段 1：契约与底库下发

**目标**：Go 能下发底库、Engine 能拉取并原子换库，比对能力可用但尚未接入结果流。

- [ ] 1.1 `app.proto` 新增 `FaceGalleryEntry` / `GetFaceGalleryRequest` / `GetFaceGalleryResponse` 与 `ControlPlaneService.GetFaceGallery`；新增 `FaceObservation` / `ReportFaceObservation` 相关 message 与 RPC。
- [ ] 1.2 `engine.proto` 新增 `FaceRecognitionConfig`，加入 `AlgorithmInstanceConfig.face_recognition = 10`。
- [ ] 1.3 删除 `person.proto` 的 `SyncPersons` / `SyncPersonsRequest` / `SyncPersonsResponse` / `PersonRecord` 及 Engine 侧 UNIMPLEMENTED handler；保留 `ExtractFaceFeature`。
- [ ] 1.4 重新生成 Go pb 与 C++ pb，确认 `descriptor_smoke_test.go` 通过。
- [ ] 1.5 两端 gRPC max message size 设为 32MB（Go server/client + C++ server/client 共 4 处）。
- [ ] 1.6 Migration `000032`：`algorithm_instances.face_recognition_json` 列 + `face_gallery_revision` 单行计数器表（含 down 脚本）。
- [ ] 1.7 Go model / repository：`AlgorithmInstance.FaceRecognitionJSON`；`BumpFaceGalleryRevisionTx`，接入 `PersonFaceRepository` 的 Create / Delete / DeleteAllByPersonID 三处**同事务**调用。
- [ ] 1.8 Go 侧 `GetFaceGallery` 实现：同一只读事务内读 revision + 查询条目；revision 相同返回 `changed=false` 空响应。
- [ ] 1.9 `PersonFaceRepository.Create` 追加全局 5000 条目上限检查 + 错误码 `CodeFaceGalleryFull`（1410）与三语文案 + HTTP 映射。
- [ ] 1.10 Engine 新增 `core/algo/face_gallery.{hpp,cpp}`：连续内存布局、`shared_mutex`、条目校验、原子 swap、归一化点积 `match()`。
- [ ] 1.11 Engine `uds_client` 新增 `get_face_gallery`；`main.cpp` control_plane_thread 接入拉取与 fail-static 处理。
- [ ] 1.12 阈值下发：Go 侧 DesiredState 组装填充 `face_recognition`；Engine 侧 `AlgorithmInstance` 保存阈值。

**验证点**：
- `cd argus && go test ./...` 通过，新增底库下发与 revision 同事务递增的单测覆盖。
- `make -C engine test` 通过，`FaceGallery` 单测覆盖：正常加载、非法 embedding（长度/NaN/范数越界）整批拒绝、超限拒绝、swap 后 `match()` 结果正确、空库 `ready()` 为 false。
- 手工：注册人脸后 ≤2s Engine 日志出现底库加载记录（条目数 + revision）；停止 Go 服务后 Engine 日志显示拉取失败但不清库。

## 阶段 2：比对、记录上报与落库

**目标**：实时流中识别到已注册人员即生成记录，覆盖语义正确。

- [ ] 2.1 Engine `handle_face_recognition_result`：JSON 校验 → 库就绪检查 → Base64 解码与 embedding 校验 → 比对 → 阈值判定。
- [ ] 2.2 Engine track 上报状态表：首次命中 / 提升 ≥ 0.03 才上报；实例销毁时释放；条目数上界保护。
- [ ] 2.3 Engine 抓拍：判定需上报后才裁图，全景走 `capture_id`、人脸特写用算法包提供的 `kImagePurposeFaceCrop` ROI。
- [ ] 2.4 Engine 构造 `FaceObservation` 并接入现有上报队列与重试路径。
- [ ] 2.5 Migration `000031`：`face_observations` 表 + 6 个索引（含 down 脚本）。
- [ ] 2.6 Go model `FaceObservation` + repository（插入、单调 upsert、分页查询、按 image_id 的孤儿对账查询）。
- [ ] 2.7 `ReportAdapter.AcceptFaceObservation`：插入 → 唯一冲突 → 带 `similarity <` 条件的 Updates → `RowsAffected == 0` 视为幂等成功。
- [ ] 2.8 孤儿图片对账接入：`face_observations` 的两个 image_id 纳入 retain 集合计算。

**验证点**：
- Go 单测必须覆盖：正常插入、同 event_id 更高相似度覆盖、**同 event_id 更低相似度不覆盖（乱序重试场景，AC7）**、`person_id` 随覆盖变更、空 `event_id` 拒绝。
- `make -C engine test` 覆盖：库未就绪时丢弃、低于阈值不上报、首次上报、提升不足 0.03 不重报、提升足够则同 `event_id` 重报。
- `make -C engine asan` 通过（重点验证 embedding 解码与裁图路径无越界）。
- 手工端到端：注册人员 → 出现在摄像头 → 记录生成且两张图可读；未注册人员经过 → 无记录、无图片产生。

## 阶段 3：管理端

**目标**：可查、可筛、可配。

- [ ] 3.1 Go API：`/record/faces` 四个端点 + service + handler，结构对齐 `plate_observation.go`。
- [ ] 3.2 路由与权限注册：列表/详情为 `record:face`，图片流为 `PermCodeAuthenticated`。
- [ ] 3.3 Migration `000033`：`record:face` 权限码 + 记录菜单 seed（对齐 `000025`）。
- [ ] 3.4 前端 `api/core/face-observation.ts` 四端点封装。
- [ ] 3.5 前端 `views/record/face/`：列表页（缩略图、姓名、相似度色阶、摄像头、时间）+ 筛选 + 详情抽屉（全景/特写对比）。
- [ ] 3.6 前端 `InstanceFormModal.vue` 新增阈值输入，按 `face_recognition` 算法类型条件渲染。
- [ ] 3.7 三语 i18n 补齐。

**验证点**：
- `cd argus && go test ./...` 覆盖新增 API 的路由、参数绑定、权限与图片流响应。
- `pnpm check` 通过（typecheck + oxlint + cspell）。
- 手工：登录后可查询筛选记录、预览两张图；修改实例阈值后 ≤2s 生效。

## 全局质量门禁（最后一轮）

- [ ] `cd argus && go test ./...`
- [ ] `cd argus && go vet ./...`
- [ ] `make -C engine test`
- [ ] `make -C engine asan`
- [ ] `make -C engine lint`
- [ ] `cd ui && pnpm check`

**不需要执行**：`sync-sdk.sh` / `check-consistency.sh`（本任务不改 C ABI 与算法包）。

## 回滚点

| 阶段 | 回滚方式 |
| --- | --- |
| 1 | 移除 `GetFaceGallery` 拉取调用，Engine 退回无底库状态；migration down |
| 2 | 摘除 `handle_face_recognition_result` 分支，回到「人脸结果静默丢弃」的当前行为 |
| 3 | 前端页面与路由为纯新增，直接移除 |

## 风险提示（实施中需持续确认）

1. **阈值 0.7（≡ cosine 0.4）未经实测**。实施完成后应在真实摄像头下验证误识/漏识，把实测结论回写到 spec。
2. **单调 upsert 是本任务最容易写错的地方**。`RowsAffected == 0` 必须返回成功而非错误 —— 返回错误会让 Engine 无限重试同一条已被正确拒绝的低分上报。
3. **`BumpFaceGalleryRevisionTx` 必须在业务事务内**。分开提交会产生「人脸已删但 Engine 仍能识别」或「已注册但永不生效」的静默故障。
