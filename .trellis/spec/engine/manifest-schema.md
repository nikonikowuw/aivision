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
| `algorithm_type` | string | `object_detection` 或 `face_recognition`（识别类，无告警语义） |
| `alarm_type_id` | string | 单一告警类型 id，前端据此 i18n 显示告警 |
| `platform_id` | string | `^[a-z0-9]+(?:-[a-z0-9]+)+$`，唯一确定平台与架构 |
| `min_adapter_version` | string | SemVer，SDK/适配层 ABI 最低版本 |
| `runtime_constraints` | object | 平台相关增量约束，由对应平台适配器校验 |
| `resource_profile` | object | 内存阈值和离散 FPS 档位 |
| `self_test` | object | 安装自测超时和输入模式 |

未知顶层字段在 v1 中拒绝，避免拼写错误被静默忽略。需要扩展时提升 `manifest_version` 或在明确的 `extensions` 命名空间中版本化。

### 2.2 告警类型 id

`object_detection` 算法包恰好声明**一个**告警类型 id，表示它产出的业务告警（如安全帽告警、反光衣检测）。`algorithm_type` 决定处理管线，`alarm_type_id` 决定业务告警语义与前端展示；`face_recognition` 包不声明 `alarm_type_id`（ABI 中写入空字符串），若声明则安装校验必须明确拒绝，避免产生隐式告警语义。

```json
"alarm_type_id": "helmet_warning"
```

- `alarm_type_id`：`^[a-z0-9_]{3,32}$`；前端根据 id 做 i18n 显示，算法包不携带显示名。
- 全局筛选键由宿主组合为 `<algorithm_id>:<alarm_type_id>`。
- 结果中的 `alarm_type_id` 固定等于本字段；一次 `process` 最多回调一个检测批次，Engine 再按目标拆分同类型事件。

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

### 2.4 包目录约定、私有配置与完整性

算法包采用**约定优于配置（Convention over Configuration）**的目录结构，无需在 manifest 中冗余声明文件列表与各文件哈希：

```text
<package_root>/
├── manifest.json          # 算法身份、资源档位与元数据（必选）
├── .env                   # 算法私有默认参数与模型路径配置（可选，推荐）
├── config.schema.json     # 算法动态业务参数 Schema（可选，无自定义参数时可省略）
├── testimage.jpg          # 安装自测输入图（必选）
├── lib/
│   └── lib<algorithm_id>.{dylib,so} # C ABI 动态库入口（必选）
└── model/                 # 算法模型与权重目录（可选，由 .env 或算法内部定位）
```

#### 2.4.1 配置与参数覆盖分层

算法实例加载与运行参数遵循严格的三层优先级覆盖机制：

```text
[1. 宿主/Go 下发的动态业务参数 (instance_update_config / instance_args.config_json)]
       ↓ 覆盖
[2. package_root/.env 算法包私有配置文件 (MODEL_PATH, 默认阈值等)]
       ↓ 覆盖
[3. C++ 编译期默认硬编码兜底值]
```

- **包私有配置隔离**：算法库必须严格基于 `package_root` 相对解析 `<package_root>/.env`，**严禁读取宿主进程的全局环境变量（如 `std::getenv`）或依赖当前工作目录（CWD）**，避免多算法包共存时发生环境污染与冲突。
- **动态模型与参数切换**：算法包可以在 `.env` 中通过 `MODEL_PATH=model/yolov8n.mlpackage`（或 `.rknn`/`.om`）指定模型相对路径。更换模型权重或调整默认阈值只需更新 `.env`，无需重新编译 C++ 动态库。

#### 2.4.2 包完整性与安全校验

- **zip 整体 SHA-256 是包完整性的唯一锚点**：由 Engine / Go 后端在安装包发布或上传时记录与审计（`<archive>.zip.sha256`），不再在 manifest 内部手工维护冗余的单文件哈希清单。
- **解压与安装校验**：
  - Engine 解压算法包时校验约定必须存在的文件（`manifest.json`、`testimage.jpg`、`lib/lib<algorithm_id>.*`）。
  - 若包含 `config.schema.json`，校验其为合法的 JSON Schema 且 `additionalProperties=false`。
  - 受部署 Profile 的解压上限（文件数、解压总大小、zip bomb 防御）与路径安全校验（禁止绝对路径、`..` 越界与符号链接逃逸）约束。

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
- `self_test.input_mode` 为 `test_image`（固定使用包根目录下的 `testimage.jpg` 作为输入）；`timeout_ms` 必须位于部署 Profile 允许的范围内。
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

算法对一帧中所有完成规则和冷却判定的目标只回调一次，批次内目标保持算法输出顺序。顶层 `event_id` 是算法生成的批次 ID；Engine 生成目标级 `AlarmEvent` 时追加从 1 开始的目标序号，确保每个目标有独立幂等键。

算法输出不包含 `algorithm_id`、算法版本、实例 ID、业务时间或图片路径，这些字段由 Engine 从可信上下文补齐。

```json
{
  "event_id": "batch_42",
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
- `event_id`：`^[A-Za-z0-9._-]{1,128}$`，是批次 ID，在 `instance_run_id` 内唯一；**禁止 `/`**，因为 Engine 组合目标事件 ID 时使用 `<instance_run_id>/<batch_event_id>-<target_sequence>`；
- `alarm_type_id`：必须等于 manifest 的 `alarm_type_id`；
- `objects`：必须为非空数组；每个对象必须包含 `label`、`confidence`、`bbox` 和 `track_id`，`confidence` 在 `[0,1]`；
- `bbox`：`[x,y,w,h]`，值在 `[0,1]` 且不越界；
- 序列化后结果总大小不得超过 `AV_MAX_RESULT_JSON_BYTES`；
- Engine 为同一批次编码一张共享抓拍，并在每条目标级 `AlarmEvent` 中复用其 `image_id` 和相对路径。

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

### 4.3 人脸识别结果（`AV_RESULT_RECOGNITION`）

识别类结果顶层固定包含 `schema_version: 1` 和 `persons`。每个人体最多一个 `face`，没有有效人脸时为 `null`。坐标全部是原图归一化坐标：bbox 为 `[x, y, w, h]`，landmark 为 `[x, y]`。

```json
{
  "schema_version": 1,
  "persons": [
    {
      "track_id": 1,
      "bbox": [0.0626, 0.3695, 0.2315, 0.4672],
      "confidence": 0.8462,
      "face": {
        "bbox": [0.1419, 0.3880, 0.0492, 0.0469],
        "confidence": 0.8535,
        "landmarks": [
          [0.1667, 0.4125],
          [0.1667, 0.4125],
          [0.1667, 0.4125],
          [0.1645, 0.4100],
          [0.1708, 0.4099]
        ],
        "embedding": {
          "model": "glintr100",
          "dimension": 512,
          "dtype": "float32",
          "normalized": true,
          "encoding": "base64",
          "byte_order": "little_endian",
          "data": "HYQFvcTvNryC8Ko8..."
        }
      }
    }
  ]
}
```

约束：
- 顶层 `schema_version` 固定为 `1`；
- `persons` 数组按 `track_id` 升序排列；
- `embedding`：满足通用嵌入表示规范。在开启轨迹抓拍优选时，非关键帧或质量未达标的人脸 `embedding` 可为 `null`；
- `embedding.data`：当存在时为 512 个 IEEE 754 little-endian `float32` 原始字节的 Base64 字符串，已做 L2 归一化；
- 只有在至少关联并成功提取一张人脸 embedding 时触发 `AV_RESULT_RECOGNITION` 结果回调。

## 5. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| manifest 未知字段、缺字段、格式错误 | `PACKAGE_MANIFEST_INVALID` |
| 约定的核心文件缺失或 Zip 包 SHA-256 不匹配 | `PACKAGE_CHECKSUM_MISMATCH` |
| 平台/OS/adapter 不兼容 | `PACKAGE_INCOMPATIBLE` |
| 请求 FPS 无对应档位或总 units 超限 | `RESOURCE_LIMIT_EXCEEDED` |
| 算法配置 Schema 失败 | `CONFIG_SCHEMA_INVALID` |
| 算法最终校验失败 | `AV_ERR_CONFIG_INVALID`，旧配置保留 |
- `alarm_type_id` 与 manifest 声明不一致、批次为空、对象缺字段、bbox 越界、JSON 超限 | 丢弃该结果并记录 `ALGO_RESULT_INVALID`
| self-test 格式错误 | 拒绝安装 |

## 6. Good / Base / Bad Cases

- Good：15 FPS 精确命中 15 FPS 档位；12 FPS 向上选择 15 FPS 档位；通过 `<package_root>/.env` 声明模型路径与默认参数；zip 整体 SHA-256 作为完整性锚点。
- Base：测试图无目标，self-test 返回 `object_count=0` 并安装成功。
- Bad：在 manifest 里枚举所有内部文件并手工维护 SHA-256；算法库直接读取操作系统全局环境变量（`std::getenv`）导致多实例配置冲突。

## 7. Tests Required

- manifest JSON Schema 正反例、未知字段、SemVer 测试。
- 约定文件存在性（`manifest.json`、`testimage.jpg`、`lib/lib<algo>.*`）、zip 整体 SHA-256、附加文件与解压上限测试。
- `.env` 包内隔离读取、三层配置优先级（Go 下发 > .env > 硬编码默认值）覆盖测试。
- FPS 精确命中、向上取档、超最大档、units 超限和内存门槛测试。
- 配置 good/base/bad、检测规则几何校验（越界/自交/点数）、revision 过期和原子回滚测试。
- 告警批次包含多个目标、Engine fan-out 后目标事件 ID 唯一、同批次目标事件共享图片、未声明类型、重复目标事件 ID、bbox 越界和大小上限测试。
- self-test 零检测成功与非法 stages/status 测试。

## 8. Wrong vs Correct

```json
// Wrong: 在 manifest 中冗余维护 files[] 列表与逐文件 SHA-256，打包工具或模型升级时极易失配
{
  "manifest_version": 1,
  "algorithm_id": "yolov8n",
  "files": [
    {"path": "lib/libyolov8n.dylib", "kind": "library", "sha256": "..."},
    {"path": "model/yolov8n.mlpackage/...", "kind": "model", "sha256": "..."}
  ]
}

// Correct: 约定优于配置，manifest 只保留元数据，模型与默认参数放 package_root/.env，zip 整体 SHA-256 锚定完整性
{
  "manifest_version": 1,
  "algorithm_id": "yolov8n",
  "version": "1.0.0",
  "name": "YOLOv8n Object Detection",
  "algorithm_type": "object_detection",
  "alarm_type_id": "object_detect",
  "platform_id": "macos-arm64-coreml",
  "min_adapter_version": "1.0.0",
  "runtime_constraints": {
    "min_os_version": "14.0"
  },
  "resource_profile": {
    "min_free_memory_mb": 256,
    "fps_tiers": [
      {"fps": 5, "units": 60},
      {"fps": 15, "units": 150},
      {"fps": 30, "units": 300}
    ]
  },
  "self_test": {
    "timeout_ms": 10000,
    "input_mode": "test_image"
  }
}
```

```json
// Wrong: 单点 units 无法定义 5/15/30 FPS 的资源变化
{"resource_units": 150, "preferred_fps": 15, "max_fps": 30}

// Correct: 离散、可验证、不可外推
{"fps_tiers": [{"fps": 5, "units": 60}, {"fps": 15, "units": 150}]}
```
