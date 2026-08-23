# 算法清单、配置与结果 Schema 规范

> 本规范定义 `manifest.json`、算法配置、资源档位、包完整性、正常告警结果和安装自测结果。解析与校验必须由共享组件拥有，禁止安装器、UI 和运行时各自实现一套规则。

## 1. Scope / Trigger

新增 manifest 字段、配置控件、结果字段、资源档位或安装校验规则时必须读取本规范，并同步更新：JSON Schema、C++ 解析器、打包 helper、fixture 与 Go/前端契约文档。

## 2. `manifest.json` 契约

### 2.1 顶层字段

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `manifest_version` | integer | v1 固定为 `1` |
| `algorithm_id` | string | `^[a-z0-9_-]{3,32}$` |
| `version` | string | SemVer，不允许 `v` 前缀 |
| `name` | string | 1-64 字符 |
| `description` | string | 可选，最多 256 字符 |
| `algorithm_type` | string | `object_detection`；`face_recognition` 仅保留枚举，当前返回未实现 |
| `alarm_type_id` | string | 单一告警类型 id，前端据此 i18n 显示告警 |
| `platform_id` | string | `^[a-z0-9]+(?:-[a-z0-9]+)+$`，唯一确定平台与架构 |
| `min_adapter_version` | string | SemVer，SDK/适配层 ABI 最低版本 |
| `runtime_constraints` | object | 平台相关增量约束，由对应平台适配器校验 |
| `resource_profile` | object | 内存阈值和离散 FPS 档位 |
| `entry_library` | string | 必须引用 `files[]` 中 kind=`library` 的一项 |
| `config_schema_file` | string | 必须引用 kind=`config_schema` 的一项 |
| `test_image_file` | string | 必须引用 kind=`test_image` 的一项 |
| `self_test` | object | 安装自测超时和输入模式 |
| `files` | array | 关键入口文件的清单和 SHA-256 |

未知顶层字段在 v1 中拒绝，避免拼写错误被静默忽略。需要扩展时提升 `manifest_version` 或在明确的 `extensions` 命名空间中版本化。

### 2.2 告警类型 id

一个算法包恰好声明**一个**告警类型 id，表示它产出的业务告警（如安全帽告警、反光衣检测）。`algorithm_type` 决定处理管线，`alarm_type_id` 决定业务告警语义与前端展示。

```json
"alarm_type_id": "helmet_warning"
```

- `alarm_type_id`：`^[a-z0-9_]{3,32}$`；前端根据 id 做 i18n 显示，算法包不携带显示名。
- 全局筛选键由宿主组合为 `<algorithm_id>:<alarm_type_id>`。
- 结果中的 `alarm_type_id` 固定等于本字段；一次 `process` 可回调多个同类型事件。

### 2.3 离散资源档位

```json
"resource_profile": {
  "min_free_memory_mb": 256,
  "fps_tiers": [
    {"fps": 5, "units": 60},
    {"fps": 15, "units": 150},
    {"fps": 30, "units": 300}
  ]
}
```

契约：

- `fps_tiers` 按 `fps` 严格递增，`fps` 和 `units` 均为正整数，`units <= 1000`。
- 请求 FPS 使用第一个 `tier.fps >= requested_fps` 的 `units`；不存在该档位则拒绝，不允许外推。
- 资源账本按选中档位求和；不同 `platform_id` 的 units 不可比较。
- `min_free_memory_mb` 是独立安全门槛，不参与 units 求和。
- `units` 是包作者声明、平台验收的绝对消耗；T1 阶段标注为「开发期估算」，平台实测后再校准。

### 2.4 文件清单与完整性

```json
"files": [
  {"path": "lib/libyolov8n.dylib", "kind": "library", "sha256": "<64 lowercase hex>"},
  {"path": "config.schema.json", "kind": "config_schema", "sha256": "<64 lowercase hex>"},
  {"path": "testimage.jpg", "kind": "test_image", "sha256": "<64 lowercase hex>"},
  {"path": "model/yolov8n.mlpackage/Data/com.apple.CoreML/model.mlmodel", "kind": "model", "sha256": "<64 lowercase hex>"}
]
```

契约：

- `path` 使用 `/` 分隔的规范化相对路径；禁止空段、`.`、`..`、反斜杠、绝对路径和重复项。
- `kind` 为 `library|config_schema|test_image|model`。
- `files[]` 只列**关键入口文件**（单文件），每项必须实际存在于包内且 SHA-256 匹配；缺失或哈希不符即拒绝安装。
- `library`/`config_schema`/`test_image` 各恰好一项（由 `entry_library`、`config_schema_file`、`test_image_file` 引用）；`model` 为 0..N 项。
- `.mlpackage` 这类目录结构**不逐文件枚举**：目录内非关键文件由 zip 整体完整性覆盖，不对目录本身哈希。
- 包内允许存在 `files[]` 之外的附加文件（README、license 等），不阻断安装；但受部署 Profile 的解压上限（文件数、解压总大小、zip bomb 防御）与路径安全校验约束。
- **zip 整体 SHA-256 是包完整性的唯一锚点**，由 Engine 在 manifest 外记录，用于审计和版本追踪；入口文件哈希用于安装后定位与验证关键文件。

### 2.5 运行时约束和自测

macOS 示例：

```json
"runtime_constraints": {
  "min_os_version": "14.0"
},
"self_test": {
  "timeout_ms": 10000,
  "input_mode": "test_image"
}
```

- 帧兼容性不由 manifest 声明：平台可产出的帧格式由 `PlatformProfile.frame_caps` 定义（见 `platform-guidelines.md`），算法包是否适配由「`platform_id` 匹配 + 安装自测用真实平台帧跑一次」验证，具体实例的格式/尺寸协商走 `instance_negotiate`（见 `algo-package-spec.md` §3.3）。
- `platform_id` 已唯一确定平台与架构，`runtime_constraints` 只声明平台相关增量约束（如 `min_os_version`），不重复声明 `arch`；未知约束字段必须拒绝。
- `self_test.input_mode` 为 `test_image`（使用 `test_image_file` 作为输入）；`timeout_ms` 必须位于部署 Profile 允许的范围内。
- 安装自测的期望结果类型恒为 self-test（validator 检查 `kind == SELF_TEST`），不需要包作者声明。

## 3. 算法配置契约

`config.schema.json` 只描述算法拥有的参数，例如阈值、类别。`analysis_fps`、检测规则（ROI/Mask/分界线）等任务级配置属于 Engine 管理，不能混入算法 JSON Schema。

支持 JSON Schema Draft-07 的受限子集：

- 类型：`object|array|string|number|integer|boolean`；
- 关键字：`required`、`properties`、`additionalProperties=false`、`items`、`minimum`、`maximum`、`multipleOf`、`minItems`、`maxItems`、`uniqueItems`、`minLength`、`maxLength`、`enum`、`pattern`、`default`；
- 注解：`title`（参数显示名）、`description`（参数含义说明）；
- 禁止：外部 `$ref`、`oneOf`、`anyOf`、`allOf`、`not`、递归 schema、自定义脚本和 `x-ui` 等 UI 元数据。

`config.schema.json` 声明参数的结构约束与展示注解：`title` 用于前端显示参数名，`description` 解释参数含义，两者是 JSON Schema 标准注解、提供默认文本；UI 控件选择由前端自行决定，不定义 `x-ui` 等 UI 元数据，也不定义 i18n key（前端需要多语言时按字段路径自行覆盖）。

### 3.1 示例

`config.schema.json` 示例（yolov8n 安全帽检测包）：

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["confidence_threshold", "iou_threshold"],
  "properties": {
    "confidence_threshold": {
      "type": "number",
      "title": "置信度阈值",
      "description": "检测框置信度低于该值的检测结果将被过滤",
      "minimum": 0,
      "maximum": 1,
      "default": 0.5
    },
    "iou_threshold": {
      "type": "number",
      "title": "IoU 阈值",
      "description": "非极大值抑制使用的交并比阈值",
      "minimum": 0,
      "maximum": 1,
      "default": 0.45
    }
  }
}
```

对应的实例配置 JSON（Engine 原样传给 `instance_update_config`）：

```json
{
  "confidence_threshold": 0.55,
  "iou_threshold": 0.45
}
```

- 算法只收到 `properties` 里声明的字段；`title`/`description` 仅供前端展示，不进算法。
- 检测规则（ROI/Mask/分界线）属于任务级配置，与 `analysis_fps` 一样由 Engine 管理并下发给实例，不放入 `config.schema.json`。

实例更新 envelope 的概念契约：

```text
InstanceDesiredConfig {
  instance_id: string
  desired_revision: uint64
  analysis_fps: uint32
  algorithm_config_json: bytes
}
```

更新顺序固定为：校验 revision -> 查资源档位/内存门槛 -> 校验算法 Schema -> 调用 `instance_update_config` -> 更新内存中的采样 FPS -> 成功响应。Go 仅在成功响应后持久化新 revision；Engine 不维护第二份业务配置数据库。

检测规则（ROI/Mask/分界线）为任务级配置：坐标使用 `[0,1]` 归一化、区域多边形 ≥3 顶点且不自交、分界线 ≥2 顶点，由 Engine 校验并经 `av_algo_instance_args.rules` 下发给算法，判定语义（锚点/跨线）归算法（见 `algo-package-spec.md` §3.4），不进入算法 `config.schema.json`。

## 4. 结果 Schema

### 4.1 正常告警

算法输出不包含 `algorithm_id`、算法版本、实例 ID、业务时间或图片路径，这些字段由 Engine 从可信上下文补齐。

```json
{
  "event_id": "ev_42",
  "alarm_type_id": "helmet_warning",
  "objects": [
    {
      "label": "person",
      "confidence": 0.895,
      "bbox": [0.245, 0.132, 0.180, 0.450],
      "track_id": 102
    }
  ]
}
```

约束：

- `event_id`：`^[A-Za-z0-9._-]{1,128}$`，在 `instance_run_id` 内唯一；**禁止 `/`**，因为全局事件 ID 是 `<instance_run_id>/<algo_event_id>`，`/` 会破坏无歧义拆分；
- `alarm_type_id`：必须等于 manifest 的 `alarm_type_id`；
- `objects`：数组，可为空但字段必填；每个 `confidence` 在 `[0,1]`；
- `bbox`：`[x,y,w,h]`，值在 `[0,1]` 且不越界；
- 序列化后结果总大小不得超过 `AV_MAX_RESULT_JSON_BYTES`。

### 4.2 安装自测结果

```json
{
  "status": "ok",
  "stages": ["preprocess", "inference", "postprocess", "serialize"],
  "object_count": 0
}
```

- `status` 固定为 `ok`；
- `stages` 为非空字符串数组，算法自报内部阶段名，validator 只校验非空与类型，**不校验命名和顺序**；
- `object_count >= 0`，不作为安装成败或精度判断依据；
- self-test result 不得包含图片请求。

## 5. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| manifest 未知字段、缺字段、格式错误 | `PACKAGE_MANIFEST_INVALID` |
| 入口文件缺失或 SHA-256 不匹配 | `PACKAGE_CHECKSUM_MISMATCH` |
| 平台/OS/adapter 不兼容 | `PACKAGE_INCOMPATIBLE` |
| 请求 FPS 无对应档位或总 units 超限 | `RESOURCE_LIMIT_EXCEEDED` |
| 算法配置 Schema 失败 | `CONFIG_SCHEMA_INVALID` |
| 算法最终校验失败 | `AV_ERR_CONFIG_INVALID`，旧配置保留 |
| alarm_type_id 与 manifest 声明不一致、bbox 越界、JSON 超限 | 丢弃该结果并记录 `ALGO_RESULT_INVALID` |
| self-test 格式错误 | 拒绝安装 |

## 6. Good / Base / Bad Cases

- Good：15 FPS 精确命中 15 FPS 档位；12 FPS 向上选择 15 FPS 档位；包内 README 等附加文件不阻断安装，zip 整体 SHA 仍是完整性锚点。
- Base：测试图无目标，self-test 返回 `object_count=0` 并安装成功。
- Bad：把 `.mlpackage` 内每个文件都写进 `files[]` 并手工维护哈希，coremltools 一升级清单就失配；或入口 library 哈希与包内实际文件不符仍被放行。

## 7. Tests Required

- manifest JSON Schema 正反例、未知字段、SemVer、路径规范化和重复文件测试。
- 入口文件存在性与 SHA-256、zip 整体 SHA-256、附加文件与解压上限测试。
- FPS 精确命中、向上取档、超最大档、units 超限和内存门槛测试。
- 配置 good/base/bad、检测规则几何校验（越界/自交/点数）、revision 过期和原子回滚测试。
- 告警零/多对象、未声明类型、重复 event ID、bbox 越界和大小上限测试。
- self-test 零检测成功与非法 stages/status 测试。

## 8. Wrong vs Correct

```json
// Wrong: 全量枚举包内每个文件并要求集合完全相等，打包工具一升级就失配
{"files": ["<.mlpackage 内部每个文件>", "..."]}

// Correct: 只列关键入口文件，zip 整体 SHA-256 锚定包完整性
{"files": [
  {"path": "lib/libyolov8n.dylib", "kind": "library", "sha256": "..."},
  {"path": "config.schema.json", "kind": "config_schema", "sha256": "..."},
  {"path": "testimage.jpg", "kind": "test_image", "sha256": "..."}
]}
```

```json
// Wrong: 单点 units 无法定义 5/15/30 FPS 的资源变化
{"resource_units": 150, "preferred_fps": 15, "max_fps": 30}

// Correct: 离散、可验证、不可外推
{"fps_tiers": [{"fps": 5, "units": 60}, {"fps": 15, "units": 150}]}
```
