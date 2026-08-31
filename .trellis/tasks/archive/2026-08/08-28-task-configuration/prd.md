# 任务配置模块

## Goal

打通「摄像头 → 分析任务 → 算法实例 → 运行」的业务配置闭环，把已就绪但无人使用的 `engineipc` 双向 IPC 骨架接上真实业务数据，使用户能够在管理界面为摄像头创建分析任务、挂载算法实例、配置运行参数与采样 FPS、启停实例，并看到 Engine 回报的真实运行状态。

用户价值：当前 Engine 侧的任务调度、算法管理、资源账本与帧分发全部已实现并通过 e2e，但 Go 侧注入的是 `UnavailableDesiredStateAdapter` / `UnavailableReportAdapter` 两个 fail-closed 桩，导致整条链路无法从界面驱动。本任务补齐业务层后，PRD §7.4 / §7.6 / §7.7 首次具备可演示、可验收的端到端形态。

## Background / 已确认事实

参考需求：`docs/prd/ai-video-analytics-edge-platform-v1.0.md`（V1.0，待评审）

- §7.4.1：一个摄像头通道对应一个任务；一个任务可配置多个算法实例；同任务算法共享一次上游连接与一次解码；每实例独立运行参数、采样 FPS、状态与资源占用。
- §7.4.2：任务持久化「期望状态」与「实际状态」；重启前启用的任务必须自动恢复；原本停用的保持停用；单任务恢复失败不阻塞其他任务。
- §7.4.3：每算法按自身配置独立采样；帧队列容量受限，过载丢旧留新；能力不兼容时拒绝启用并返回明确原因。
- §7.5.5：任务绑定 `algorithm_id`，**不固定具体版本**；仍有任务配置引用时禁止卸载算法包。
- §7.6：受限 JSON Schema 驱动参数表单，不为单个算法开发专用页面，不允许算法包注入前端 JS；整份配置原子热更新；FPS 更新须先通过计算资源配额校验。
- §7.6.1：区域坐标原点位于**视频有效画面**左上角，横纵坐标统一为 `[0,1]` 归一化；多边形 ≥3 顶点，不越界不自交。
- §7.7：加速计算总容量归一化为 1000 资源单位，声明不高于 1000 的可分配上限与安全保留量；算法包按 FPS 档位声明资源单位；超过可分配容量时拒绝操作并提示受限资源。
- §7.17.3：页面 `/resource/task`，路由名 `ResourceTask`，页面权限码 `resource:task`，图标 `ant-design:profile-outlined`。
- §7.17.5：任务管理页 `keepAlive=true`，重新激活时刷新服务端数据并保留筛选、分页与滚动位置。
- §11.2：验收要求「一个任务可挂多个算法」「参数整份热更新成功或完整回滚」「超过可分配资源上限时拒绝启用」「服务重启后自动恢复原启用任务」。

### 仓库现状（已逐项核实）

**已就绪，本任务直接复用：**

- `app/internal/pkg/engineipc/`：`EngineClient` 已封装全部 Engine RPC；`Runtime` 已绑定 `app.sock` 并注册 `ControlPlaneService` / `ReportService`；错误归一化、传输错误判定、fail-closed 语义均已实现并有测试。
- `app/internal/proto/aivision/v1/`：Go protobuf/gRPC 代码已生成。
- `engine/src/app/main.cpp:240-315`：`control_plane_thread` 已实现「拉取 DesiredState → revision 变大则 apply → 上报 TaskState/InstanceState → 每 10s 上报遥测」完整循环。
- `engine/src/core/ipc/uds_server.cpp:1128-1150`：实例创建时的 FPS 档位换算与资源账本分配已实现。
- `engine/include/aivision/core/resource_ledger.hpp`：`total_units_ = 1000` / `reserved_units_ = 100`，`set_limits()` 可运行时调整。
- `app/internal/model/algorithm.go`：`AlgorithmVersion.FPSTiers`（jsonb）、`ConfigSchema`（jsonb）已持久化；`Algorithm.ActiveVersion` 已维护。
- `ui/apps/web-antd/src/views/ai/algorithm/components/SchemaModal.vue`：已有 JSON Schema 解析逻辑（properties / required / minimum / maximum / enum / default），可作为动态表单的解析基础。
- `ui/apps/web-antd/src/components/video/VideoPlayer.vue`：标准 `<video>` + flv.js，可作为规则编辑器的底图载体。

**空洞，本任务负责填补：**

- `app/cmd/api/wire_gen.go:106-107`：注入的是 `UnavailableDesiredStateAdapter()` / `UnavailableReportAdapter()` 两个 fail-closed 桩，整条链路无业务实现。
- `app/internal/repository/algorithm.go:260`：`CountActiveInstances` 是占位实现，注释写明「在未来的 analysis_tasks 表建立前返回 0」，导致 §7.5.5「有引用禁止卸载」形同虚设。
- 无 `analysis_tasks` / `algorithm_instances` 表、model、repository、service、api；迁移最新到 `000018`。
- 无 `/resource/task` 页面与菜单 seed。
- `engine/proto/.../engine.proto` 的 `PlatformProfileInfo` **不暴露算力单位**，只有 `max_cameras` / `max_instances`，Go 无法做 §7.7 要求的同步配额拒绝。
- **Engine reconcile 失败无 ERROR 回流**：`apply_desired_state` 失败回滚后失败实例不注册进 `AlgoManager`，`main.cpp:257-288` 上报循环遍历不到且只报 RUNNING/STOPPED——失败实例永远无状态上报（见 R10）。
- **Engine FPS 热更新不生效**：`reconcile_instance` 对已存在且算法/版本未变的实例仅更新 params/rules，不重建、不更新 `target_fps_`、不重算资源账本（见 R10）。

### 已核实的关键行为参数

| 项 | 实测值 | 来源 |
| --- | --- | --- |
| Engine 控制面轮询周期 | **2 秒**（20 × 100ms 可中断分段睡眠） | `engine/src/app/main.cpp:311` |
| 状态上报频率 | 每轮全量上报所有 TaskState + InstanceState | `main.cpp:257-288` |
| FPS 档位换算 | 取第一个 `tier_fps >= target_fps` 的 units | `uds_server.cpp:1133-1138` |
| `analysis_fps <= 0` | 默认按 25 处理 | `uds_server.cpp:1130` |
| `analysis_fps` 超最高档 | **直接拒绝** `RESOURCE_LIMIT_EXCEEDED`，不钳到最高档 | `uds_server.cpp:1140` |
| `fps_tiers` 顺序保证 | 按 `fps` 严格递增 | `.trellis/spec/engine/manifest-schema.md:57` |
| 实例上报状态 | 常规仅 RUNNING / STOPPED 两态 | `main.cpp:271-278` |
| reconcile 失败处理 | 整体回滚（清空全量再重建）；失败实例不注册、**无 ERROR 上报** | `uds_server.cpp:442-464, 886-921` |
| 已存在实例热更新 | current 分支仅更新 params/rules，**FPS 变更不生效**（`target_fps_` 仅构造时写入） | `uds_server.cpp:1124-1136` / `algo_instance.hpp:118` |

## Key Decisions

- **D1（范围）**：交付后端全链路 + 前端任务管理页 + Schema 驱动动态表单。检测规则绘制界面拆为子任务 `08-28-detection-rule-editor`；**后端 rules 存储与几何校验留在本任务**，因为 `DesiredState` 组装必须携带该字段，schema 需先定死。

- **D2（任务身份）**：新建 `analysis_tasks` 表，以 `camera_id` 为唯一业务键，**不发明独立 `task_id`**。理由：Engine 侧 `CameraTaskConfig` 与 `ReconcileItemResult`（「task 为 camera_id」）均按 camera_id 寻址，独立 ID 会强制在 IPC 边界做双向映射且 Engine 报错时无法回指。表名沿用 `repository/algorithm.go:261` 注释中已表达的 `analysis_tasks` 命名意图。

- **D3（提交语义 —— 对 PRD §7.6 的有意识偏离）**：采用**乐观提交 + 状态回报**。Go 完成资源配额预校验后即写库并 bump revision，返回 200；Engine 在 ≤2 秒内拉取应用，失败经 `ReportInstanceState(ERROR)` 异步回流，前端轮询状态列展示。

  PRD §7.6 字面要求「仅在算法实例确认成功后持久化新配置」，本决策不满足该字面语义。原因是 Engine 采用纯拉模式，`ApplyDesiredStateResponse` 由 Engine 自调自取，不回传 Go；若改为 Go 主动推送并同步等待结果，会引入一致性缺陷：

  ```
  Go 推 revision=N → Engine 应用成功 → Go 事务回滚（DB 仍为 N-1）
  → 下轮 Engine 拉到 N-1，判断 N-1 > N 为假 → Engine 永不回退
  → Engine 运行着一份 DB 中不存在的配置
  ```

  除非引入两阶段提交或允许 revision 回退，该路径不可靠。故保留拉模式，以「Go 侧预校验覆盖可判定的拒绝原因（资源配额、算法存在性、FPS 档位）」逼近同步语义，仅把帧能力协商这类只有 Engine 能判定的失败留给异步回流。

  **⚠ 已核实的前提修正（2026-08-28）**：D3 假设的「失败经 `ReportInstanceState(ERROR)` 异步回流」在当前 Engine 代码中**并不存在**——`apply_desired_state` 失败回滚后失败实例不注册进 `AlgoManager`，`main.cpp:257-288` 的上报循环遍历不到它，且该循环只报 RUNNING/STOPPED。因此任何 Go 预校验无法拦截的失败（内存不足、帧能力协商）都会让实例**永久停在 STARTING**。该回流路径须由本任务在 Engine 侧补齐（见 R10），否则 D3 的「异步回流」是空头承诺。

- **D4（revision 生成）**：单行计数器表 `desired_state_revision(id=1, revision)`，业务事务内 `UPDATE ... SET revision = revision + 1 RETURNING revision`。**不采用「各行自带 revision 取 max()」**：删除持有最大 revision 的行会导致 max() 回退，而 Engine 的 `desired.revision() > applied_revision` 是严格单调判断，回退等于该次删除永不生效。

- **D5（推拉模式）**：保持 Engine 拉模式，Go 不主动调用 `ApplyDesiredState` / `UpsertTask` / `SetInstanceState` / `UpdateInstanceConfig`。理由：拉模式下「Engine 崩溃」「网络断开」「Engine 刚启动」是同一种情况，恢复后自动收敛，Go 无需维护存活探测、重试队列与补偿逻辑；且符合 §6.2「以 Go 保存的期望状态全量对账，不依赖可能丢失的增量命令」。

- **D6（状态落库）**：实际状态以内存为主，**仅状态码变化时写库**；`current_fps`、`last_frame_wall_time_ns` 只驻内存，API 读取时与库中配置合并返回。理由：16 路 × 2 实例场景下 Engine 每 2 秒全量上报 48 条，全部落库即 24 UPDATE/s 持续无变化重写，产生大量 dead tuple；而状态码变化是启停/断流/出错等低频事件。Go 重启后状态码从库恢复，实时字段 ≤2 秒补齐。

- **D7（实例交互形态）**：任务列表页 + 实例抽屉。PRD §7.17.3 只定义了一个页面权限码 `resource:task`，实例不能有独立菜单；实例字段较多（算法、版本、FPS、参数、规则、状态、资源占用），抽屉空间优于可展开子行，且与现有 `VersionsDrawer.vue` 交互语言一致。

- **D8（任务创建）**：用户手动创建，新建时摄像头下拉**只列出尚未建任务的摄像头**。任务名可独立于摄像头名。

- **D9（删除保护）**：摄像头存在关联分析任务时**拒绝删除**，返回 `CameraInUse` 错误码，与算法包 `CodeAlgoInUse` 的保护模式一致；删除任务时级联删除其全部算法实例（实例无法脱离任务存在）。

- **D10（配额分母 —— 突破 D1 的零 proto 改动边界）**：为 `PlatformProfileInfo` 新增 `total_compute_units` / `reserved_compute_units` 两个只读字段，C++ 侧从 `ResourceLedger` 读值填充。proto3 新增字段向后兼容，不破坏既有契约；C++ 改动约 5 行。没有该字段则 §7.7「超配时拒绝操作并提示受限资源」无法实现，只能退化为「先返回成功、2 秒后变 ERROR」。

  Go 只需 `total` 与 `reserved` 两个**静态上限**，不需要 Engine 的实时 `used`——Go 是配置的唯一权威，已启用实例的 units 由自身累加。

- **D11（算法版本绑定）**：`algorithm_instances` 表**不存 `algorithm_version`**，组装 `DesiredState` 时从 `algorithms.active_version` 动态填充，以满足 §7.5.5「任务绑定 algorithm_id，不固定具体版本」。

  **连带必须处理**：算法包激活版本变更会改变 `DesiredState` 内容却不触碰任务配置，若不 bump revision，Engine 将永不感知版本切换。故 `algorithmService` 的 `UploadAndInstall` / `ActivateVersion` / `UninstallVersion` 三处均须 bump revision。为避免模块直接耦合，通过 `RevisionBumper` 接口注入。

- **D12（配额算法一致性）**：Go 的 FPS 档位换算必须与 `uds_server.cpp:1130-1140` **逐条一致**，包括「非正数默认 25」「取第一个 `tier_fps >= target_fps`」「超过最高档直接拒绝而非钳位」三条。任一条不一致都会产生「Go 放行 → Engine 拒绝 → 2 秒后 ERROR」的误判，正是 D3 试图避免的体验。该算法须有独立单测，并以 `fps_tiers` 边界值覆盖。

## Requirements

### R1 数据模型与迁移

- 新增 `analysis_tasks` 表：`camera_id`（唯一业务键）、`name`、`desired_enabled`（期望态）、`actual_status`（实际态）、`status_message`、`last_frame_at`、`reported_at`。
- 新增 `algorithm_instances` 表：`instance_id`（UUID，唯一）、`camera_id`、`algorithm_id`、`analysis_fps`、`params_json`、`rules_json`、`enabled`、`actual_status`、`status_message`。
- 新增 `desired_state_revision` 单行计数器表，初始化一行 `id=1, revision=0`。
- 遵循项目数据层约定：显式表名列名（snake_case）、不建外键、`status` 用 `int8`、版本化 SQL 迁移（`app/migrations/`）。

### R2 期望状态适配器

- 实现 `DesiredStateAdapter`：按当前 revision 组装全量 `DesiredState`，含 tasks、instances、active_package_versions。
- 实例的 `algorithm_version` 从 `algorithms.active_version` 动态填充。
- 仅 `desired_enabled=true` 的任务与 `enabled=true` 的实例进入 DesiredState。

### R3 状态回报适配器

- 实现 `ReportAdapter` 的 `AcceptTaskState` / `AcceptInstanceState`：内存缓存全量状态，状态码变化时落库。
- `AcceptAlarm` / `AcceptMetrics` / `ReconcileOrphanImages` 本任务保持 fail-closed 或最小可接受实现，不纳入验收范围。

### R4 任务与实例 API

- 任务：列表（分页、按状态与摄像头筛选）、创建、更新名称、启停、删除。
- 实例：按任务列表、创建、更新（参数 + FPS + 规则整份提交）、启停、删除。
- 全部写操作接入现有操作日志模块（§7.16.2 要求「任务及算法实例启停」「算法参数热更新」入日志）。
- 统一响应体 `{code,data,message}`，错误码集中定义在 `internal/pkg/errno`。

### R5 资源配额校验

- 启动时经 `QueryProfile` 获取并缓存 `total_compute_units` / `reserved_compute_units`；Engine 不可用时使用上次成功值，从未成功过则拒绝启用实例并返回明确原因。
- 创建或启用实例、修改 FPS 时，校验 `Σ units(已启用实例) + units(本次) ≤ total - reserved`，超出则拒绝并在错误信息中给出当前已用、本次申请与可用上限。
- 档位换算严格遵循 D12。

**⚠ 平台容量校准（研究项，本任务范围外，但配额设计须预留校准空间）**：

`total_compute_units=1000` 是归一化账本，尚未与 RK3576 真实硬件能力校准。已知基准（2026-08 调研，基于官方 Brief Datasheet 像素吞吐推算）：

| 维度 | RK3576 能力 | 说明 |
| --- | --- | --- |
| VPU 解码 | H.264 4K@60（≈8~9 路 1080P@25/30）；H.265 4K@120（≈16~19 路 1080P@25/30） | 解码路数 = 任务数（每任务一路解码、多实例共享），与实例数无关 |
| NPU | 6 TOPS INT8（YOLOv8n 单路约 5~10ms/帧） | 16 路推理约 80~160ms/轮，NPU 大概率先于 VPU 打满 |
| 工程稳态建议 | H.265 源 8~12 路 1080P；H.264 源 6~8 路 | `max_cameras=16` 是 H.265 理论极限，不应作为稳态配置 |

本任务不实施校准（1000 单位 ↔ 路数/TOPS 的映射属平台适配专项，建议独立任务跟踪），但要求 R10 的 ERROR 回流 + R5 的三数字错误信息保证超硬件能力时**用户可感知、可诊断**，而非静默失败。
- 创建或启用实例、修改 FPS 时，校验 `Σ units(已启用实例) + units(本次) ≤ total - reserved`，超出则拒绝并在错误信息中给出当前已用、本次申请与可用上限。
- 档位换算严格遵循 D12。

### R6 规则存储与几何校验

- `rules_json` 按 `DetectionRule` 结构持久化：`role`（ROI/MASK/LINE）、`line_direction`、`points[]`。
- 后端几何校验：坐标 ∈ [0,1]；区域多边形 ≥3 顶点且不自交；分界线 ≥2 顶点；`line_direction` 仅 LINE 有效。
- 校验器独立可单测，不依赖 HTTP 层。

### R7 算法包引用保护

- 填补 `repository/algorithm.go:260` 的 `CountActiveInstances`，返回引用指定 `algorithm_id + version` 的实例数。
- 摄像头删除前检查关联任务，存在则拒绝。

### R8 前端任务管理页

- `/resource/task` 页面：任务列表（摄像头、任务名、期望态、实际态、实例数、操作）、新建任务、启停、删除。
- 实例抽屉：该任务的实例列表、新建实例（选算法 + 设 FPS + 配参数）、编辑、启停、删除。
- Schema 驱动动态表单：依据 `algorithm_versions.config_schema` 渲染可编辑表单，支持 boolean / integer / number / string / enum / array，遵守 `minimum` / `maximum` / `multipleOf` / `enum` / `default` / `required` 约束。
- 保存后轮询状态列直至稳定态（RUNNING / ERROR / STOPPED），启动中显式展示中间态——Engine 轮询周期 2 秒，无中间态会让用户误判操作失败。
- 菜单 seed 迁移：`/resource/task` → `ResourceTask` → `resource:task`，图标 `ant-design:profile-outlined`，`keepAlive=true`，标题使用 `routes.*` 国际化键。

### R9 proto 与 Engine 最小改动

- `PlatformProfileInfo` 新增 `total_compute_units = 12` / `reserved_compute_units = 13`。
- `uds_server.cpp` 的 `QueryProfile` 处理从 `ResourceLedger` 读值填充。
- 重新生成 Go 与 C++ protobuf 代码。
- 不改动任何既有字段编号与语义。

### R10 Engine reconcile 语义补齐（与 R9 同批实施，Go 侧闭环的前置依赖）

经代码核实，Engine 现有 reconcile 有两处缺口直接破坏本任务的端到端闭环，必须在 R9 之外补上：

- **reconcile 失败异步回流缺失**：`apply_desired_state`（`uds_server.cpp:335`）逐项 reconcile 失败后整体回滚，失败的实例**不注册进 `AlgoManager`**；`main.cpp:257-288` 的上报循环只遍历 `AlgoManager::instance_ids()` 且只报 RUNNING/STOPPED。结果：任何 Go 预校验无法拦截的失败（内存不足、帧能力协商等）都让实例**永久停在 STARTING**，前端中间态永不收敛，D3 的「异步回流」落空。须在失败回滚后，对 `response.results` 中 `FAILED` 的实例主动 `report_instance_state(INSTANCE_STATUS_ERROR, code+message)`（对齐 `mark_package_degraded` 的既有模式，`uds_server.cpp:1280-1290`）。
- **FPS 热更新不生效**：`reconcile_instance` 的 current 分支（`uds_server.cpp:1124-1136`）对「已存在且算法/版本未变」的实例只调 `update_params` / `set_rules`，不重建、不更新 `target_fps_`、不重算资源账本；`AlgorithmInstance` 无 `set_fps`，`target_fps_` 仅构造时写入（`algo_instance.hpp:118`）。后果：修改 FPS（同算法同版本）后 `instance_configs_` 与 Go 库均显示新值，但运行时抽帧节奏与资源占用仍是旧值——击穿 §7.6「FPS 更新须先通过计算资源配额校验」。须在 current 分支检测 `config.analysis_fps() != current->get_target_fps()`，触发重建路径（释放旧资源 → 重新 `allocate` → remove + create），或新增 `set_target_fps` 并同步重算资源账本。

**认知约束（不改动）**：`restore_runtime_state`（`uds_server.cpp:886-921`）采用「清空全部再重建」——任何实例失败都会令**全系统所有任务的所有实例** stop→重建（回滚粒度 = 整个 DesiredState，非单个实例）。这是全局快照对账的固有代价，本任务不改动该语义；R10 的 ERROR 上报让该影响对用户可见，而非静默重建。

## Acceptance Criteria

### 后端

- [ ] `make -C app migrate-up` 成功，三张新表结构符合 R1。
- [ ] `make -C app test` 与 `make -C app vet` 通过。
- [ ] 创建任务 → 挂 2 个不同算法实例 → 启用 → `GET /task/list` 在 ≤5 秒内返回两个实例状态均为 RUNNING。
- [ ] 同一任务挂载多个算法实例可正常运行（§11.2）。
- [ ] FPS 档位换算单测覆盖：正常档位、落在档位之间（向上取整）、`fps<=0` 默认 25、超过最高档拒绝——四种情形结果与 `uds_server.cpp:1130-1140` 一致。
- [ ] 资源超配时创建/启用实例返回 4xx 与结构化错误码，错误信息含已用/申请/上限三个数字，且**不写库、不 bump revision**（§11.2）。
- [ ] 几何校验单测覆盖：坐标越界、多边形顶点数不足、多边形自交、分界线顶点数不足、LINE 外角色携带 `line_direction`。
- [ ] 删除存在关联任务的摄像头返回 `CameraInUse`，摄像头与任务均未被删除。
- [ ] 存在实例引用时卸载算法包被拒绝（`CountActiveInstances` 生效，§7.5.5）。
- [ ] 算法包激活版本切换后，`DesiredState` 中实例的 `algorithm_version` 随之更新，且 revision 已 bump。
- [ ] Engine 断开后重连，按 Go 库中期望状态完成全量对账，无需人工干预（§6.2）。
- [ ] Go 服务重启后，原 `desired_enabled=true` 的任务自动恢复运行；原停用任务保持停用（§7.4.2、§11.2）。
- [ ] 状态码未变化时不产生数据库写入（可通过写入计数或日志断言）。
- [ ] 修改实例 FPS（同算法同版本）后：Engine 运行时 `get_target_fps()` 变为新值、`ResourceLedger` used 随之重算，且同任务其他实例不受影响（R10）。
- [ ] 触发 Engine 独有的 reconcile 失败（构造 Go 预校验无法拦截的失败）时，失败实例 ≤5 秒内上报 ERROR 并落库，而非永久停在 STARTING（R10）。

### 前端

- [ ] `pnpm check` 与 `pnpm test:unit` 通过。
- [ ] 登录后 `/resource/task` 可见于「资源管理」下，无 `resource:task` 权限的用户无法从菜单或直接 URL 访问。
- [ ] 动态表单能依据 `config_schema` 正确渲染 boolean / integer / number / string / enum / array，并在越界输入时阻止提交。
- [ ] 保存实例后状态列展示中间态，并在 ≤5 秒内收敛到 RUNNING 或 ERROR。
- [ ] 页面 `keepAlive=true`，返回时保留筛选、分页与滚动位置并刷新数据（§7.17.5）。

### 契约

- [ ] `make -C engine build` 与 `make -C engine test` 通过。
- [ ] `QueryProfile` 返回的 `total_compute_units` / `reserved_compute_units` 与 `ResourceLedger` 实际值一致。
- [ ] 既有 proto 字段编号与语义零改动，`engine e2e` 仍通过。
- [ ] Engine 单测覆盖 R10 两条路径：reconcile 失败实例收到 `INSTANCE_STATUS_ERROR` 上报；FPS 变更触发重建且资源账本重算。

## Non-Goals

- 不实现检测规则绘制界面（拆至 `08-28-detection-rule-editor`）。
- 不实现告警落库、图片管理、Webhook 推送（`AcceptAlarm` / `ReconcileOrphanImages` 留待后续任务）。
- 不实现设备监控时序落库与图表（`AcceptMetrics` 同上）。
- 不实现人脸识别相关的抓拍/识别记录与人员索引同步。
- 不改动 Engine 的推拉模式、轮询周期或任何既有 proto 字段（R9 的新增字段与 R10 的 reconcile 语义补齐属于本任务范围，均不触碰这三项）。
- 不实现算法包升级期间的实例暂停/恢复编排（§7.5.5 的升级流程属算法包管理范畴）。
- 不做多设备/集中管理，不做任务配置的导入导出。
