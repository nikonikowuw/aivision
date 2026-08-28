# Implement — 任务配置模块

> 每个 Phase 结束都是一个可独立验证、可独立回滚的节点。
> 前三个 Phase 完成后后端链路即可用 curl 端到端验证，无需等待前端。

## 阶段规划概览

| Phase | 目标 | 结束时可验证 | 回滚粒度 |
| --- | --- | --- | --- |
| 1 | proto 字段 + 配额/几何纯函数 + R10 reconcile 补齐 | 单测通过，Engine 返回真实 units；失败实例有 ERROR 回流；FPS 热更新生效 | 撤销 proto 两字段 / revert `uds_server.cpp` 改动 |
| 2 | 数据模型 + 迁移 + repository | `migrate-up/down` 可逆，repo 单测通过 | `migrate-down` |
| 3 | service + adapter + API + wire | curl 建任务→挂实例→看到 RUNNING | wire 恢复 Unavailable 桩 |
| 4 | 引用保护 + revision bump 接入 | 卸载被拒、版本切换生效 | 单函数回退 |
| 5 | 前端页面 + 动态表单 + 菜单 seed | 界面完成完整闭环 | 菜单 seed down |

---

## Phase 1：proto 扩展与纯函数模块

**为什么先做**：配额算法是 Phase 3 的前置依赖，且它是纯函数，可在没有任何数据库和 HTTP 的情况下完整验证。proto 改动放最前面，避免后续反复重新生成代码。

**R10 与 R9 同批实施**：R10 的两处 Engine reconcile 语义补齐（失败异步回流 + FPS 热更新生效）同在 `uds_server.cpp`，是 Go 侧闭环的前置依赖——Phase 3 的「失败实例 ≤5s 上报 ERROR」与「改 FPS 生效」验收依赖它，必须在本 Phase 一并改动、一并回归，不能拖到 Phase 5。

- [x] `engine/proto/aivision/v1/engine.proto`：`PlatformProfileInfo` 增加 `total_compute_units = 12`、`reserved_compute_units = 13`，不动既有字段编号
- [x] 重新生成 C++ 与 Go protobuf 代码
- [x] `engine/src/core/ipc/uds_server.cpp` 的 `QueryProfile` 处理：从 platform profile 读取两个值填入响应
- [x] **R10a（reconcile 失败异步回流）**：`apply_desired_state` 回滚块（`uds_server.cpp:442-464`）中，在将 OK 项重标记为 `RECONCILE_ROLLED_BACK` 之后，遍历 `response->results()`，对 `status == RECONCILE_ITEM_STATUS_FAILED` 的实例构造 `InstanceState{instance_id, INSTANCE_STATUS_ERROR, message = code + ": " + error_message}`，经 `app_client_->report_instance_state()` 主动上报（对齐 `mark_package_degraded` 的既有模式，`uds_server.cpp:1270-1290`；上报失败记入 `restart_failures_`）。被回滚牵连的原 OK 项上报 ERROR 属正常瞬态——restore 后重建为 RUNNING。
- [x] **R10b（FPS 热更新生效）**：`reconcile_instance` 的 current 分支（`uds_server.cpp:1112-1123`）检测 `(config.analysis_fps() > 0 ? config.analysis_fps() : 25) != current->get_target_fps()`（访问器见 `algo_instance.hpp:94`）时走重建路径：`remove_instance(config.instance_id())` → 重新取 `current = AlgoManager::instance().get(...)`（变为 null）→ 落入下方 create 路径重新 allocate（对齐 1106-1110 的算法/版本变更模式）。资源账本由 `remove_instance` 释放 + create 路径重算，无需为 `AlgorithmInstance` 新增 `set_target_fps`。
- [x] 新增 `app/internal/service/quota.go`：`ResolveUnits` 复刻 `uds_server.cpp:1130-1140` 三条不变式
- [x] 新增 `app/internal/service/rulegeom.go`：`ValidateRules` 五项几何校验
- [x] `app/internal/pkg/errno/errno.go`：新增错误码 + 三语言消息
  - `CodeCameraInUse`、`CodeResourceExceeded`、`CodeFPSTierExceeded`
  - `CodeRuleOutOfBounds`、`CodeRuleTooFewPoints`、`CodeRuleSelfIntersect`
  - `CodeTaskNotFound`、`CodeTaskAlreadyExists`、`CodeInstanceNotFound`
- [x] `quota_test.go`：覆盖 design §5 列出的全部 7 个边界值
- [x] `rulegeom_test.go`：覆盖坐标越界、顶点不足、自交、方向字段误用
- [x] `engine/tests/unit/` 新增 reconcile 单测（如 `test_uds_reconcile.cpp`），覆盖 R10 两条路径：
  - 某实例 reconcile 失败整体回滚后，该实例收到 `INSTANCE_STATUS_ERROR` 上报且 message 含 code
  - 同算法同版本仅改 `analysis_fps` 后，实例重建、`get_target_fps()` 变为新值、`ResourceLedger` used 随之重算，同任务其他实例不受影响

**验证**

```bash
make -C engine build && make -C engine test   # 含 R10 新增 reconcile 单测
cd app && go test ./internal/service/ -run 'TestResolveUnits|TestValidateRules' -v
make -C app vet
```

**Review gate**：

1. `ResolveUnits` 与 `uds_server.cpp:1130-1140` 逐行比对，确认三条不变式无偏差——这是整个乐观提交方案的正确性基础。
2. R10a：确认回滚块对 FAILED 实例有 ERROR 上报、上报失败有记录（对齐 `mark_package_degraded`），不存在「失败实例永久停在 STARTING」的路径。
3. R10b：确认 FPS 变更走重建路径、`get_target_fps()` 与资源账本同步更新，同任务其他实例不受影响。

---

## Phase 2：数据模型与仓储层

- [x] `app/internal/model/task.go`：`AnalysisTask`、`AlgorithmInstance`、`DetectionRule` / `DetectionPoint`
  - 状态码常量按 task/instance 两套枚举分别定义（design §2.2）；软删除遵循 database-guidelines（毫秒 deleted_at=0 + 复合唯一索引，偏离 design §2.1 的 TIMESTAMPTZ 部分索引，系 spec CRITICAL 约束优先）
- [x] `app/migrations/000019_add_analysis_tasks.up.sql` / `.down.sql`：三张表 + 索引
- [x] `app/internal/repository/task.go`：`TaskRepository` 接口与实现
- [x] `InTx` 事务包装器：确保业务写与 `BumpRevision` 同事务
- [x] `LoadDesiredSnapshot`：JOIN cameras 取 rtsp_url，JOIN algorithms 取 active_version
- [x] `task_repository_test.go`：sqlite AutoMigrate 单测（遵循项目约定，AutoMigrate 仅用于单测）

**验证**

```bash
make -C app migrate-up && make -C app migrate-version
make -C app migrate-down   # 确认可逆
make -C app migrate-up
cd app && go test ./internal/repository/ -run TestTask -v
```

**Review gate**：确认所有改变 `DesiredState` 内容的写方法都只能经 `InTx` 调用，无绕过路径。

---

## Phase 3：服务层、适配器与 API

**本 Phase 结束即打通后端全链路**，可脱离前端验证。

- [x] `app/internal/service/revision.go`：`RevisionBumper` 接口
- [x] `app/internal/service/task.go`：`TaskService` 接口与实现
  - 任务：List / Create / Update / SetEnabled / Delete
  - 实例：ListByCamera / Create / Update（整份提交）/ SetEnabled / Delete
  - 校验顺序固定：schema → 几何 → 配额（design §4.1）
  - 辅助：`ListAvailableCameras`——未建任务的摄像头轻量列表，供 `GET /api/task/available-cameras` 下拉（design §8 数据契约）
  - 附：`paramschema.go` 自研受限 JSON Schema 校验器（无第三方依赖，manifest-schema §3 受限子集 + Draft-07 语义）
- [x] 配额上限缓存：启动异步 `QueryProfile`，5 分钟刷新，`total == 0` 视为未获取（design §7 兼容性）
- [x] `desiredStateAdapter`：实现 `engineipc.DesiredStateAdapter`
- [x] `reportAdapter`：实现 `engineipc.ReportAdapter`，**嵌入 `unavailableReportAdapter`** 以保留未实现方法的 fail-closed 行为（实际：类型未导出不可嵌入，改为显式返回 `IPC_UNAVAILABLE`，语义一致）
- [x] `app/internal/api/task.go`：HTTP handler + DTO 绑定
- [x] `app/internal/router/router.go`：路由常量 + Group + `PermMiddleware.Register`
  - 页面权限 `resource:task`，按钮权限 `resource:task:add` / `:edit` / `:delete`
- [x] 写操作接入操作日志（§7.16.2：任务与实例启停、参数热更新）
- [x] `app/cmd/api/wire.go`：替换两个 `Unavailable*` 为真实 adapter
- [x] `make wire` 重新生成 DI 代码
- [x] `task_service_test.go`：配额拒绝不写库不 bump、整份更新原子性、状态码未变不写库

**验证**

```bash
make -C app test && make -C app vet
make -C app build

# 端到端（需 Engine 运行）
curl -X POST /api/task -d '{"cameraId":"...","name":"大门"}'
curl -X POST /api/task/instance -d '{"cameraId":"...","algorithmId":"yolov8n","analysisFps":5,"paramsJson":{...}}'
sleep 5 && curl /api/task/instance/list?cameraId=...   # 期望 status=RUNNING
```

**Review gate**：

1. 确认 `reportAdapter` 未实现的三个方法仍返回 `IPC_UNAVAILABLE`，未意外静默接受
2. 确认配额拒绝路径**不写库、不 bump revision**——这是「拒绝操作不影响现有实例」（§9 异常场景表）的关键
3. 用写入计数或日志确认状态码未变化时零数据库写入

---

## Phase 4：引用保护与 revision bump 接入

**为什么单独一个 Phase**：这些改动侵入已有的 `algorithm.go` / `camera.go`，与前三个 Phase 的新增代码风险性质不同，独立成节点便于回退。

- [ ] `app/internal/repository/algorithm.go:260`：`CountActiveInstances` 填真实查询，删除占位注释
- [ ] `app/internal/service/algorithm.go`：注入 `RevisionBumper`，在三处调用
  - `UploadAndInstall`（首次安装设 ActiveVersion）
  - `ActivateVersion`
  - `UninstallVersion`
  - 三处均须与各自业务事务同事务
- [ ] `app/internal/service/camera.go` 的 `DeleteCamera` / `BatchDeleteCamera`：前置 `CountTasksByCameraID` 检查，返回 `CodeCameraInUse`
- [ ] 回归测试：卸载被引用算法包被拒、删除有任务的摄像头被拒、版本切换后 DesiredState 更新

**验证**

```bash
make -C app test
# 卸载保护
curl -X DELETE /api/algorithm/yolov8n/versions/1.0.0   # 期望 1021 CodeAlgoInUse
# 摄像头保护
curl -X DELETE /api/camera/1                            # 期望 CodeCameraInUse
# 版本切换 → revision 变化 → Engine 换版本
```

**Review gate**：确认三处 bump 都在事务内，且没有「先提交业务再 bump」的顺序错误——那会产生短暂窗口期内 Engine 拉到不一致快照。

---

## Phase 5：前端页面

- [ ] `ui/apps/web-antd/src/api/task.ts`：RequestClient 封装
- [ ] `views/resource/task/index.vue`：任务列表 + 筛选 + 启停 + 删除
- [ ] `components/TaskFormModal.vue`：摄像头下拉仅列未建任务的（数据来自 `GET /api/task/available-cameras`，value 用 `camera_id` 业务键）
- [ ] `components/InstanceDrawer.vue`：实例列表 + 增删改启停
- [ ] `components/InstanceFormModal.vue`：选算法 + 设 FPS（`max` 绑定 `fpsTiers` 最高档，算法/版本切换联动）+ 嵌 SchemaForm
- [ ] `components/SchemaForm.vue`：按 design §8 的类型映射表实现，复用 `SchemaModal.vue` 的解析逻辑
- [ ] 状态轮询：保存后 1s 间隔，命中稳定态或 15s 超时停止
- [ ] i18n：`routes.resource.task` 及页面内文案（zh-CN / en / zh-TW，与 errno 三语言保持一致）
- [ ] `app/migrations/000020_seed_resource_task_menu.up.sql` / `.down.sql`
  - 路径 `/resource/task`，路由名 `ResourceTask`，权限码 `resource:task`
  - 图标 `ant-design:profile-outlined`，`keepAlive=true`，`affix=false`
  - 按钮权限 `resource:task:add` / `:edit` / `:delete`

**验证**

```bash
cd ui && pnpm check && pnpm test:unit
make -C app migrate-up
# 手动：登录 → 资源管理 → 任务管理 → 建任务 → 挂实例 → 观察状态收敛
# 手动：切走再回来，确认筛选/分页/滚动位置保留且数据刷新
# 手动：用无 resource:task 权限的账号访问 /resource/task，确认被拒
```

**Review gate**：确认 `SchemaForm` 不读取任何 `x-ui` 字段——`manifest-schema.md:129` 明确禁止算法包声明 UI 元数据。

---

## 验证命令速查

```bash
# 后端
make -C app migrate-up
make -C app test
make -C app vet
make -C app build
make -C app wire          # 改 wire.go 后必须

# 引擎（Phase 1 后）
make -C engine build
make -C engine test
make -C engine e2e        # 确认既有 e2e 未被 proto 改动破坏

# 前端
cd ui && pnpm check
cd ui && pnpm test:unit
```

## 风险文件 / 回滚点

| 文件 | 风险 | 回滚方式 |
| --- | --- | --- |
| `engine/proto/.../engine.proto` | 改动跨 Go/C++ 两栈 | 仅新增字段，删除即回滚；旧代码不读取新字段 |
| `engine/src/core/ipc/uds_server.cpp` | R10 侵入 reconcile 核心路径 | 改动集中两处（回滚块 + current 分支），Phase 1 独立验证 + git revert |
| `app/cmd/api/wire.go` | 一行之差决定整条链路启用与否 | 恢复 `Unavailable*Adapter`，系统退回当前状态 |
| `app/internal/service/algorithm.go` | 侵入已稳定模块 | Phase 4 独立成节点，可单独 revert |
| `app/internal/service/camera.go` | 同上 | 同上 |
| `app/migrations/000019` / `000020` | 数据结构与菜单 | `make migrate-down` |
| `app/internal/service/quota.go` | 与 Engine 漂移会造成误判 | 单测锁定三条不变式 |

## `task.py start` 前的检查

- [x] `prd.md` 含需求、约束与可执行验收标准
- [x] `design.md` 含分层、DDL、接口、数据流、配额算法与回滚方案
- [x] `implement.md` 含有序清单、验证命令、review gate 与回滚点
- [x] 子任务 `08-28-detection-rule-editor` 已创建并关联
- [ ] `implement.jsonl` / `check.jsonl` 已配置 spec 清单（Phase 1.3）
