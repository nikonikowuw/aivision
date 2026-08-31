# Design — 任务配置模块

## 1. 分层与边界

```
┌─ ui/apps/web-antd/src/views/resource/task/ ────────────────┐
│  index.vue            任务列表 + 新建/启停/删除              │
│  components/                                                │
│    InstanceDrawer.vue 实例抽屉：列表 + 增删改启停            │
│    SchemaForm.vue     config_schema → 可编辑表单             │
└─────────────────────────┬───────────────────────────────────┘
                           │ REST /api/task/*  /api/task/instance/*
┌──────────────────────────▼──────────────────────────────────┐
│  app/internal/api/task.go          HTTP handler + 参数绑定    │
│  app/internal/service/task.go      业务编排 + 配额校验        │
│    ├─ taskService                  任务/实例 CRUD             │
│    ├─ desiredStateAdapter    ◄──── 实现 engineipc 接口        │
│    └─ reportAdapter          ◄──── 实现 engineipc 接口        │
│  app/internal/service/quota.go     FPS 档位换算 + 配额累加     │
│  app/internal/service/rulegeom.go  规则几何校验（纯函数）      │
│  app/internal/repository/task.go   数据访问                   │
│  app/internal/model/task.go        GORM 模型                  │
└──────────────────────────┬──────────────────────────────────┘
                           │ engineipc.Runtime（已存在）
        ┌──────────────────┴───────────────────┐
        │ app.sock（Go 为 server）              │ engine.sock（Go 为 client）
        │  Engine 每 2s 拉 GetDesiredState      │  仅 QueryProfile（取配额上限）
        │  Engine 每 2s 推 ReportTaskState      │
        │           ReportInstanceState         │
        └───────────────────────────────────────┘
```

**边界约束：**

- `service/task.go` 是唯一同时持有「数据库写入」与「revision bump」的地方，二者必须同事务。
- `quota.go` 与 `rulegeom.go` 是纯函数模块，不依赖 gorm / gin / grpc，可独立单测。
- adapter 实现放在 service 层而非 repository 层：`DesiredState()` 需要跨表组装并读取 `algorithms.active_version`，属编排职责。
- 本任务不新增 Go→Engine 的写调用，`EngineClient` 仅用于 `QueryProfile`。

## 2. 数据模型

### 2.1 DDL（`app/migrations/000019_add_analysis_tasks.up.sql`）

```sql
-- 分析任务：与摄像头 1:1，camera_id 即任务标识（不发明独立 task_id）
CREATE TABLE analysis_tasks (
    id              BIGSERIAL    PRIMARY KEY,
    camera_id       VARCHAR(36)  NOT NULL,
    name            VARCHAR(128) NOT NULL,
    desired_enabled BOOLEAN      NOT NULL DEFAULT FALSE,
    actual_status   SMALLINT     NOT NULL DEFAULT 0,
    status_message  VARCHAR(255) NOT NULL DEFAULT '',
    last_frame_at   TIMESTAMPTZ,
    reported_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX uk_analysis_tasks_camera_id
    ON analysis_tasks (camera_id) WHERE deleted_at IS NULL;

-- 算法实例：挂在 camera_id 下（不经 analysis_tasks.id，与 Engine 寻址一致）
CREATE TABLE algorithm_instances (
    id              BIGSERIAL    PRIMARY KEY,
    instance_id     VARCHAR(36)  NOT NULL,
    camera_id       VARCHAR(36)  NOT NULL,
    algorithm_id    VARCHAR(64)  NOT NULL,
    analysis_fps    INTEGER      NOT NULL DEFAULT 0,
    params_json     JSONB        NOT NULL DEFAULT '{}',
    rules_json      JSONB        NOT NULL DEFAULT '[]',
    enabled         BOOLEAN      NOT NULL DEFAULT FALSE,
    actual_status   SMALLINT     NOT NULL DEFAULT 0,
    status_message  VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX uk_algorithm_instances_instance_id
    ON algorithm_instances (instance_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_algorithm_instances_camera_id
    ON algorithm_instances (camera_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_algorithm_instances_algorithm_id
    ON algorithm_instances (algorithm_id) WHERE deleted_at IS NULL;

-- 期望状态版本计数器：单行，只增不减
CREATE TABLE desired_state_revision (
    id       SMALLINT PRIMARY KEY DEFAULT 1,
    revision BIGINT   NOT NULL DEFAULT 0,
    CONSTRAINT ck_desired_state_revision_singleton CHECK (id = 1)
);
INSERT INTO desired_state_revision (id, revision) VALUES (1, 0);
```

**注意**：`algorithm_instances` 无 `algorithm_version` 列（D11）。版本在组装 `DesiredState` 时从 `algorithms.active_version` 读取。

### 2.2 状态码映射

`actual_status` 直接存 proto 枚举数值，避免二次映射失配：

| 值 | TaskStatusCode | InstanceStatusCode |
| --- | --- | --- |
| 0 | UNSPECIFIED | UNSPECIFIED |
| 1 | STARTING | STARTING |
| 2 | RUNNING | RUNNING |
| 3 | DEGRADED | DEGRADED |
| 4 | RECONNECTING | STOPPED |
| 5 | STOPPED | ERROR |
| 6 | ERROR | — |

两个枚举的数值语义不同（task 的 5=STOPPED，instance 的 5=ERROR），model 层须分别定义常量，禁止共用。

## 3. 关键接口

### 3.1 Repository

```go
// app/internal/repository/task.go

type TaskFilter struct {
    Page       int
    PageSize   int
    CameraID   string
    Name       string // 模糊匹配
    Configured *bool  // nil=全部；true=有实例；false=无实例
}

type TaskRepository interface {
    CreateTask(ctx context.Context, task *model.AnalysisTask) error
    UpdateTask(ctx context.Context, task *model.AnalysisTask) error
    DeleteTaskCascade(ctx context.Context, cameraID string) (bool, error) // 同事务删实例
    GetTaskByCameraID(ctx context.Context, cameraID string) (*model.AnalysisTask, error)
    ListTaskPage(ctx context.Context, filter *TaskFilter) ([]model.AnalysisTask, int64, error)
    CountTasksByCameraID(ctx context.Context, cameraID string) (int64, error) // 供删摄像头保护

    CreateInstance(ctx context.Context, inst *model.AlgorithmInstance) error
    UpdateInstance(ctx context.Context, inst *model.AlgorithmInstance) error
    DeleteInstance(ctx context.Context, instanceID string) (bool, error)
    GetInstance(ctx context.Context, instanceID string) (*model.AlgorithmInstance, error)
    ListInstancesByCameraID(ctx context.Context, cameraID string) ([]model.AlgorithmInstance, error)
    ListEnabledInstances(ctx context.Context) ([]model.AlgorithmInstance, error) // 配额累加 + DesiredState

    // 状态回报：仅状态码变化时调用
    UpdateTaskStatus(ctx context.Context, cameraID string, status int8, msg string) error
    UpdateInstanceStatus(ctx context.Context, instanceID string, status int8, msg string) error

    // revision：必须在业务事务内调用
    BumpRevision(ctx context.Context) (uint64, error)
    CurrentRevision(ctx context.Context) (uint64, error)

    // 事务包装器：业务写 + BumpRevision 原子提交
    InTx(ctx context.Context, fn func(ctx context.Context, r TaskRepository) error) error
}
```

`InTx` 是本设计的关键：任何改变 `DesiredState` 内容的写入都必须在同一事务内 bump revision，否则会出现「配置已改但 Engine 不知道」或「revision 已增但配置未落」。

### 3.2 RevisionBumper（解耦算法模块）

```go
// app/internal/service/revision.go

// RevisionBumper 供其它模块在改变 DesiredState 内容后触发版本递增，
// 避免 algorithmService 直接依赖 taskRepository。
type RevisionBumper interface {
    BumpRevision(ctx context.Context) error
}
```

注入到 `algorithmService`，在 `UploadAndInstall`（首次安装设 ActiveVersion）、`ActivateVersion`、`UninstallVersion` 三处调用。**必须与各自的业务事务同事务**，否则算法激活成功但 revision 未增，Engine 永不换版本。

### 3.3 DesiredStateAdapter

```go
func (a *desiredStateAdapter) DesiredState(
    ctx context.Context, currentRevision uint64,
) (*aivisionv1.DesiredState, error) {
    rev, err := a.repo.CurrentRevision(ctx)
    if err != nil { return nil, err }

    // 即使 rev == currentRevision 也返回完整快照：
    // Engine 侧 main.cpp:249 自己判断 revision 是否变大，
    // 返回空快照会让 Engine 无法区分「没变化」与「配置被清空」。
    tasks, instances, activeVersions, err := a.repo.LoadDesiredSnapshot(ctx)
    ...
}
```

组装规则：

- `tasks`：`desired_enabled = true` 的任务，`rtsp_url` 从 `cameras` 表 JOIN 取。
- `instances`：`enabled = true` 且其所属任务也 `desired_enabled = true` 的实例。任务停用时其实例不进入快照——由 Go 侧过滤，不依赖 Engine 判断。
- `algorithm_version`：从 `algorithms.active_version` 填充；若为空串（算法包未激活任何版本），跳过该实例并记 warn 日志。
- `active_package_versions`：全部 `active_version != ''` 的算法。

### 3.4 ReportAdapter

```go
type reportAdapter struct {
    repo  repository.TaskRepository
    mu    sync.RWMutex
    tasks map[string]*runtimeTaskState  // camera_id  → 实时状态
    insts map[string]*runtimeInstState  // instance_id → 实时状态
}

func (a *reportAdapter) AcceptTaskState(ctx context.Context, s *aivisionv1.TaskState) error {
    a.mu.Lock()
    prev, existed := a.tasks[s.GetCameraId()]
    changed := !existed || prev.status != int8(s.GetStatus())
    a.tasks[s.GetCameraId()] = &runtimeTaskState{
        status:      int8(s.GetStatus()),
        message:     s.GetMessage(),
        lastFrameAt: s.GetLastFrameWallTimeNs(),
        reportedAt:  time.Now(),
    }
    a.mu.Unlock()

    if !changed {
        return nil  // D6：状态码未变则不写库
    }
    return a.repo.UpdateTaskStatus(ctx, s.GetCameraId(), int8(s.GetStatus()), s.GetMessage())
}
```

`AcceptAlarm` / `AcceptMetrics` / `ReconcileOrphanImages` 本任务保持返回 `IPC_UNAVAILABLE`（沿用 `unavailableReportAdapter` 的行为），在 Non-Goals 中已声明。为此 `reportAdapter` **嵌入** `unavailableReportAdapter` 以获得这三个方法的默认实现，避免遗漏。

`GET /task/list` 读取时合并：库中取配置与状态码，内存中取 `current_fps`、`last_frame_at`、`reported_at`。若内存无该 key（Go 刚重启且 Engine 尚未上报），实时字段返回 null，前端显示「等待上报」。

## 4. 数据流

### 4.1 创建并启用实例

```
POST /api/task/instance
  │
  ├─ 1. 参数绑定与基础校验（gin binding）
  ├─ 2. 校验 camera_id 对应任务存在
  ├─ 3. 校验 algorithm_id 存在且 active_version != ''
  ├─ 4. params_json 按 config_schema 校验（服务端复校，不信任前端）
  ├─ 5. rules_json 几何校验（rulegeom.Validate）
  ├─ 6. 配额校验（quota.go）
  │      units := ResolveUnits(fpsTiers, analysisFps)   // 见 §5
  │      used  := Σ ResolveUnits(每个已启用实例)
  │      if used + units > total - reserved → 400 CodeResourceExceeded
  │
  └─ 7. repo.InTx:
           INSERT algorithm_instances
           UPDATE desired_state_revision SET revision = revision + 1 RETURNING revision
     → 200 OK { instanceId, status: STARTING }

≤2s 后 Engine control_plane_thread：
  GetDesiredState(applied) → revision 变大 → apply_desired_state
    ├─ 成功 → ReportInstanceState(RUNNING) → 状态码变化 → 落库
    └─ 失败 → 回滚后对 FAILED 实例 ReportInstanceState(ERROR, msg)（R10 补齐）
       → 状态码变化 → 落库

  ⚠ 回滚语义：任何实例失败 → restore_runtime_state 清空全部再重建
     （全系统所有任务所有实例 stop→recreate），未失败的实例也经历一次抖动。
  ⚠ 现状修正：现有 Engine 对 reconcile 失败实例不上报任何状态（失败实例不进
     AlgoManager，main.cpp 上报循环遍历不到且只报 RUNNING/STOPPED）——R10 未实现前，
     本条 ERROR 分支不成立，实例永久停在 STARTING。

前端：保存后每 1s 轮询 GET /task/instance/list，
      直到 status ∈ {RUNNING, ERROR, STOPPED} 或超时 15s 停止轮询
```

### 4.2 整份配置热更新

PRD §7.6 要求「全部接受并立即生效，或拒绝并保持旧配置」。本设计用**单事务 + 单 revision** 实现原子性：

```
PUT /api/task/instance/:instanceId
  body: { analysisFps, paramsJson, rules }   ← 三者必须整份提交

  校验顺序：schema → 几何 → 配额（任一失败即 400，不写库不 bump）
  repo.InTx:
     UPDATE algorithm_instances SET analysis_fps, params_json, rules_json
     BumpRevision
```

Engine 侧拿到的是全新快照，`apply_desired_state` 内部要么整体接受要么整体拒绝。Go 不使用 `UpdateInstanceConfig` RPC——该 RPC 只能传 `params_json`，无法与 FPS、rules 一起原子提交（见 prd.md D3 的分析）。

**FPS 变更的 Engine 侧生效（R10，必须实现）**：`reconcile_instance` 的 current 分支（`uds_server.cpp:1124-1136`）对「已存在且算法/版本未变」的实例只更新 params/rules，不重建、不更新 `target_fps_`、不重算资源账本——**FPS 修改不会生效**。R10 要求在 current 分支检测 `config.analysis_fps() != current->get_target_fps()` 时触发重建路径（释放旧资源 → 重新 `allocate` → remove + create），或为 `AlgorithmInstance` 新增 `set_target_fps` 并同步重算资源账本（档位换算复用 §5 的 `ResolveUnits`）。

### 4.3 算法包版本切换

```
POST /api/algorithm/:id/activate
  algorithmService.ActivateVersion:
     同事务：UPDATE algorithms SET active_version = ?
             revisionBumper.BumpRevision()
  → 下次 GetDesiredState 中 instances[].algorithm_version 已变
  → Engine 感知 revision 变大 → 重建相关实例
```

无此 bump 则 Engine 永不换版本（prd.md D11）。

## 5. 配额算法（必须与 Engine 逐条一致）

Engine 侧权威实现：`engine/src/core/ipc/uds_server.cpp:1130-1140`

```go
// app/internal/service/quota.go

var ErrFPSTierExceeded = errors.New("analysis_fps exceeds highest declared tier")

// ResolveUnits 复刻 Engine 的档位换算。tiers 必须按 fps 升序
// （manifest-schema.md §3 保证 fps_tiers 严格递增）。
func ResolveUnits(tiers []model.FPSTier, analysisFPS int32) (uint32, error) {
    target := analysisFPS
    if target <= 0 {
        target = 25 // 对齐 uds_server.cpp:1130
    }
    for _, t := range tiers {
        if t.FPS >= target { // 第一个 >= target 的档位，即向上取整
            return t.Units, nil
        }
    }
    // 对齐 uds_server.cpp:1140：超过最高档直接拒绝，不钳到最高档
    return 0, ErrFPSTierExceeded
}
```

**三条不变式**，任一条偏离都会造成「Go 放行 → Engine 拒绝 → 2 秒后 ERROR」：

1. `analysisFPS <= 0` → 按 25 处理
2. 取**第一个** `tier.FPS >= target` 的 units（向上取整，非最近邻）
3. 超过最高档 → **拒绝**，而非取最高档

单测须覆盖：`tiers=[{5,60},{15,150},{25,220}]` 下 `fps=3→60`、`fps=5→60`、`fps=6→150`、`fps=25→220`、`fps=26→ErrFPSTierExceeded`、`fps=0→220`、`fps=-1→220`。

### 配额上限缓存

```go
type quotaLimits struct {
    total, reserved int32
    fetchedAt       time.Time
    ok              bool // 是否曾成功获取
}
```

- 服务启动后异步 `QueryProfile` 一次，失败则按指数退避重试。
- 每次配额校验前若距上次获取超过 5 分钟，触发异步刷新（不阻塞当前请求）。
- Engine 不可用且 `ok == false`（从未成功获取）→ 拒绝启用实例，返回 `CodeEngineUnavailable`。已有实例不受影响。
- Engine 不可用但 `ok == true` → 使用上次值继续校验。

### 配额分母与平台容量校准（研究项）

`total_compute_units=1000` / `reserved_units=100` 来自 `resource_ledger.hpp` 默认值，是归一化账本，**未与目标平台（RK3576）真实能力对齐**。已知硬件基准（官方 Brief Datasheet 像素吞吐推算，2026-08）：

- **VPU 解码**：H.264 4K@60 ≈ 8~9 路 1080P；H.265/AV1 4K@120 ≈ 16~19 路 1080P（像素吞吐：H.264 档 ≈ 498 Mpix/s，H.265 档 ≈ 995 Mpix/s）。
- **解码路数 = 任务数（摄像头数）**：每任务一路解码、多实例共享（§7.4.1），实例不占解码资源——16 路摄像头 = 16 路解码，与实例数无关。
- **NPU（6 TOPS INT8）**：YOLOv8n 单路 5~10ms/帧，16 路 ≈ 80~160ms/轮——NPU 先于 VPU 成为瓶颈。
- **工程稳态建议**：H.265 源 8~12 路、H.264 源 6~8 路 1080P；`max_cameras=16` 是 H.265 理论极限，不宜作稳态配置。

本任务不实施校准（1000 单位 ↔ 路数/TOPS 映射属平台适配专项，建议独立任务跟踪）；`max_cameras=16` 保持现状，但硬件超限时依赖 R10 的 ERROR 回流与 R5 的三数字错误信息让用户可诊断。

## 6. 规则几何校验

```go
// app/internal/service/rulegeom.go — 纯函数，无外部依赖

func ValidateRules(rules []DetectionRule) error
```

校验项：

| 检查 | 规则 | 错误码 |
| --- | --- | --- |
| 坐标范围 | 所有点 x,y ∈ [0,1] | `CodeRuleOutOfBounds` |
| 区域顶点数 | ROI/MASK ≥ 3 | `CodeRuleTooFewPoints` |
| 分界线顶点数 | LINE ≥ 2 | `CodeRuleTooFewPoints` |
| 多边形自交 | ROI/MASK 边两两不相交（相邻边共享端点除外） | `CodeRuleSelfIntersect` |
| 方向字段 | 非 LINE 规则不得携带非零 `line_direction` | `CodeInvalidParam` |

自交检测用标准线段相交判定（叉积符号 + 共线时的区间重叠），对 ROI/MASK 的闭合多边形逐对检查非相邻边。顶点数在配置场景下极小（通常 <20），O(n²) 可接受。

**坐标归一化基准**（前端职责，此处记录契约）：PRD §7.6.1 规定原点为「视频有效画面左上角」。`<video>` 在 `object-fit: contain` 下会产生黑边，归一化必须基于实际画面区域：

```
videoAR   = videoWidth / videoHeight
elemAR    = clientWidth / clientHeight
若 videoAR > elemAR：画面满宽，上下黑边
    renderW = clientWidth
    renderH = clientWidth / videoAR
    offsetX = 0
    offsetY = (clientHeight - renderH) / 2
否则：画面满高，左右黑边
    renderH = clientHeight
    renderW = clientHeight * videoAR
    offsetX = (clientWidth - renderW) / 2
    offsetY = 0

归一化坐标 = ((clickX - offsetX) / renderW, (clickY - offsetY) / renderH)
```

该契约由子任务 `08-28-detection-rule-editor` 实现，本任务的后端校验独立于它。

## 7. proto 与 Engine 改动

```protobuf
// engine/proto/aivision/v1/engine.proto
message PlatformProfileInfo {
  // ... 既有 1-11 字段保持不变 ...
  int32 total_compute_units    = 12; // 归一化算力总量（PRD §7.7 基准 1000）
  int32 reserved_compute_units = 13; // 系统安全保留量
}
```

C++ 侧在 `uds_server.cpp` 的 `QueryProfile` 处理中补两行，从 `platform_adapter` 的 profile 读取（`macos_platform.mm:16-17` 已有 `total_compute_units = 1000` / `reserved_compute_units = 100`）。

**兼容性**：proto3 新增字段向后兼容——旧 Go 客户端忽略新字段，新 Go 客户端读旧 Engine 得到 0。故 Go 侧须把 `total == 0` 视为「Engine 版本过旧」并等同于「未成功获取」处理，不可当成「容量为 0」而拒绝一切操作。

## 8. 前端结构

```
views/resource/task/
├── index.vue                  任务列表页
├── components/
│   ├── TaskFormModal.vue      新建/编辑任务（摄像头下拉仅列未建任务的）
│   ├── InstanceDrawer.vue     实例抽屉
│   ├── InstanceFormModal.vue  新建/编辑实例
│   └── SchemaForm.vue         config_schema 驱动的动态表单
└── ...
api/task.ts                    RequestClient 封装
```

### 未建任务摄像头下拉（D8 数据契约）

`TaskFormModal` 的摄像头下拉数据来自**新增接口** `GET /api/task/available-cameras`，**不复用** `GET /api/camera/page` 做前端过滤——前端拿不到「哪些摄像头已有任务」的数据，且分页语义会被过滤破坏。

- 返回：未建任务（`analysis_tasks` 无未软删记录）的摄像头轻量列表，无分页（摄像头 16 级规模），`[{cameraId, name, protocol}]`。
- 实现：`taskService.ListAvailableCameras` = `cameraRepository` 全量查询 − `taskRepository` 已建任务的 `camera_id` 集合（`SELECT camera_id FROM analysis_tasks WHERE deleted_at IS NULL`），service 层内存过滤。
- 下拉选项 `value` 用 **`camera_id` 业务键**（与 Engine 寻址一致，D2），非 DB 主键 `id`。
- 权限：`resource:task:add`（与任务创建一致）。

### FPS 档位前端引导

`InstanceFormModal` 选中算法（`activeVersion`）后，从 `fpsTiers`（`manifest-schema.md` 保证严格递增）取**末项 fps** 绑定 `InputNumber :max`，`:min="1"`，并把档位列表（如 `5 / 15 / 25`）作为 `description` 展示。算法或激活版本切换时 `max` 联动更新。

前端 `max` 只是**防误操作引导**——后端 `ResolveUnits` 的超最高档硬校验（400 + `ErrFPSTierExceeded`，D12）必须保留作为兑底，二者不冲突。

`SchemaForm.vue` 支持的类型映射（受限 JSON Schema，见 `manifest-schema.md` §4）：

| JSON Schema | ant-design-vue 控件 | 约束来源 |
| --- | --- | --- |
| `boolean` | `Switch` | — |
| `integer` | `InputNumber` `:precision="0"` | `minimum` / `maximum` |
| `number` | `InputNumber` | `minimum` / `maximum` / `multipleOf` |
| `string` | `Input` | `minLength` / `maxLength` / `pattern` |
| `string` + `enum` | `Select` | `enum` |
| `array` + `items.enum` | `Select` `mode="multiple"` | `minItems` / `maxItems` / `uniqueItems` |

`title` 作字段标签，`description` 作 help 文案，`default` 作初始值，`required` 决定必填标记。**不支持 `x-ui` 等 UI 元数据**——`manifest-schema.md:129` 明确禁止算法包声明 UI 元数据，控件选择完全由前端按类型推导。

**状态轮询**：保存实例后启动 1s 间隔轮询，命中稳定态（RUNNING/ERROR/STOPPED）或累计 15s 后停止。列表页常驻不轮询，仅在有实例处于 STARTING 时开启。

## 9. 重要权衡

| 权衡 | 选择 | 代价 | 缓解 |
| --- | --- | --- | --- |
| 同步拒绝 vs 架构简洁 | 乐观提交 + Go 侧预校验 | 帧能力协商失败仍异步暴露 | 预校验覆盖资源/算法/FPS 三类可判定失败，占实际拒绝的绝大多数 |
| revision 单行锁 | 单行计数器 | 所有配置写入串行化 | 16 路规模下配置写入本就极低频，串行反而保证 revision 与快照严格对应 |
| 状态不全量落库 | 仅变化时写 | Go 重启后 ≤2s 内无实时 FPS | 状态码从库恢复，前端显示「等待上报」而非伪造 0 |
| 配额上限缓存 | 启动取一次 + 5 分钟刷新 | Engine 改 `set_limits()` 后最多 5 分钟不同步 | `set_limits` 当前仅在启动时由 profile 调用一次，运行期不变 |
| 实例挂 camera_id 而非 task.id | 与 Engine 寻址一致 | 任务软删后实例需显式级联清理 | `DeleteTaskCascade` 同事务处理 |
| 回滚粒度 | 全局快照对账：任何实例失败 → 全系统清空重建 | 未失败实例经历 stop→recreate 抖动 | R10 的 ERROR 上报让失败可见；Go 预校验拦截绝大多数失败 |
| FPS 热更新生效 | Engine 侧重建或新增 `set_target_fps`（R10） | 资源账本须重算，不能只改成员变量 | 档位换算复用 §5 `ResolveUnits`，与 D12 同一套单测 |

## 10. 回滚与风险控制

**回滚路径**：本任务全部改动可通过以下方式独立回退——

1. `make -C app migrate-down`（三张新表 + 菜单 seed）
2. `wire.go` 恢复注入 `UnavailableDesiredStateAdapter` / `UnavailableReportAdapter`
3. proto 的两个新字段即使保留也无害（旧代码不读取）

回滚后系统退回当前状态：Engine 拉到 `IPC_UNAVAILABLE`，不启动任何任务，其余模块（摄像头、算法包、人员）不受影响。

**风险点**：

| 风险 | 触发条件 | 处理 |
| --- | --- | --- |
| 配额算法与 Engine 漂移 | `uds_server.cpp` 换算逻辑后续被改 | design 中记录权威位置；单测断言三条不变式；spec 中固化契约 |
| revision bump 遗漏 | 新增改变 DesiredState 的写路径时忘记 bump | 全部写路径经 `repo.InTx` 且 `InTx` 内强制 bump；code review 检查项 |
| 状态内存与库不一致 | 进程重启期间 Engine 上报丢失 | 状态码以库为准，实时字段以内存为准，二者语义不重叠 |
| `algorithms.active_version` 为空 | 算法包已安装但未激活 | 组装快照时跳过该实例并记 warn，不让 Engine 收到空版本 |
| 旧 Engine 返回 units=0 | 部署了未含新字段的 Engine | 视为「未成功获取」，拒绝启用实例而非按容量 0 拒绝一切 |
| FPS 热更新不生效 | 修改 FPS（同算法同版本）后 Engine 运行时仍用旧 `target_fps_`，账本不同步 | R10 实现重建路径或 `set_target_fps`；验收用例覆盖「改 FPS → Engine `get_target_fps()` 变化 + 账本重算」 |
| reconcile 失败无 ERROR 回流 | 任何 Go 预校验无法拦截的失败（内存不足、帧能力协商）让实例永久卡 STARTING | R10 在回滚后对 FAILED 实例主动上报 ERROR；前端中间态得以收敛 |
