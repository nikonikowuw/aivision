# 算法清单、配置与结果 Schema 规范

> 本规范定义 `manifest.json`、算法配置、资源档位、包文件完整性、正常告警结果和安装自测结果。解析与校验必须由共享组件拥有，禁止安装器、UI 和运行时各自实现一套规则。

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
| `alarm_types` | array | 至少 1 项，ID 包内唯一 |
| `platform_id` | string | `^[a-z0-9]+(?:-[a-z0-9]+)+$` |
| `min_adapter_version` | string | SemVer |
| `runtime_constraints` | object | 由对应平台适配器严格校验 |
| `resource_profile` | object | 内存阈值和离散 FPS 档位 |
| `entry_library` | string | 必须引用 `files[]` 中 kind=`library` 的一项 |
| `config_schema_file` | string | 必须引用 kind=`config_schema` 的一项 |
| `test_image_file` | string | 必须引用 kind=`test_image` 的一项 |
| `self_test` | object | 安装自测超时和输入模式 |
| `files` | array | 包内除 `manifest.json` 外全部普通文件的清单和 SHA-256 |

未知顶层字段在 v1 中拒绝，避免拼写错误被静默忽略。需要扩展时提升 `manifest_version` 或在明确的 `extensions` 命名空间中版本化。

### 2.2 告警类型

```json
"alarm_types": [
  {
    "id": "object_detected",
    "name": "目标检测",
    "i18n_key": "alarm.type.object_detected",
    "description": "检测到配置类别时触发"
  }
]
```

- `id`：`^[a-z0-9_]{3,32}$`，包内唯一。
- `name`：1-64 字符；`i18n_key` 可选；`description` 最多 128 字符。
- 全局筛选键由宿主组合为 `<algorithm_id>:<alarm_type_id>`。
- `alarm_type_id` 是事件级字段；不同告警类型必须发不同回调，不能挂在单个 object 上。

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

### 2.4 文件完整性

```json
"files": [
  {
    "path": "lib/libyolov8n.dylib",
    "kind": "library",
    "sha256": "<64 lowercase hex>"
  },
  {
    "path": "model/yolov8n.mlpackage/Data/com.apple.CoreML/model.mlmodel",
    "kind": "model",
    "sha256": "<64 lowercase hex>"
  }
]
```

- `path` 使用 `/` 分隔的规范化相对路径；禁止空段、`.`、`..`、反斜杠、绝对路径和重复项。
- `kind` 为 `library|model|dependency|config_schema|test_image|metadata`。
- `files[]` 必须与压缩包内除 `manifest.json` 外的普通文件集合完全相等，多文件和少文件都拒绝。
- `.mlpackage` 是目录，不对目录本身哈希；其全部普通文件分别列入 `files[]`。转换证据中的 tree digest 计算为：按 UTF-8 相对路径排序，对每项拼接 `path + NUL + raw_sha256_bytes` 后整体 SHA-256。
- 上传 zip 的整体 SHA-256 由 Engine 在 manifest 外记录，用于审计和版本追踪。

### 2.5 运行时约束和自测

macOS 示例：

```json
"runtime_constraints": {
  "arch": "arm64",
  "min_os_version": "14.0"
},
"self_test": {
  "timeout_ms": 10000,
  "expected_result_kind": "self_test"
}
```

平台适配器负责解释本平台约束；未知约束字段必须拒绝。`timeout_ms` 必须位于部署 Profile 允许的范围内。

## 3. 算法配置契约

`config.schema.json` 只描述算法拥有的参数，例如阈值、类别和 ROI。`analysis_fps` 属于 Engine 调度配置，不能混入算法 JSON Schema。

支持 JSON Schema Draft-07 的受限子集：

- 类型：`object|array|string|number|integer|boolean`；
- 关键字：`required`、`properties`、`additionalProperties=false`、`items`、`minimum`、`maximum`、`multipleOf`、`minItems`、`maxItems`、`uniqueItems`、`minLength`、`maxLength`、`enum`、`pattern`、`default`；
- UI 元数据：`x-ui.widget`、`x-ui.step`、`x-ui.i18n`；
- 禁止：外部 `$ref`、`oneOf`、`anyOf`、`allOf`、`not`、递归 schema 和自定义脚本。

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

ROI polygon 必须至少 3 点、坐标在 `[0,1]`、无自交；矩形必须满足 `x+w<=1`、`y+h<=1`。

## 4. 结果 Schema

### 4.1 正常告警

算法输出不包含 `algorithm_id`、算法版本、实例 ID、业务时间或图片路径，这些字段由 Engine 从可信上下文补齐。

```json
{
  "event_id": "ev_42",
  "alarm_type_id": "object_detected",
  "objects": [
    {
      "label": "person",
      "confidence": 0.895,
      "bbox": [0.245, 0.132, 0.180, 0.450],
      "track_id": 102
    }
  ],
  "extra": {
    "inference_time_ms": 12.4
  }
}
```

约束：

- `event_id`：`^[A-Za-z0-9._/-]{1,128}$`，在 `instance_run_id` 内唯一；
- `alarm_type_id`：必须属于 manifest 的 `alarm_types[].id`；
- `objects`：数组，可为空但字段必填；每个 `confidence` 在 `[0,1]`；
- `bbox`：`[x,y,w,h]`，值在 `[0,1]` 且不越界；
- `extra`：可选 object，序列化后结果总大小不得超过 `AV_MAX_RESULT_JSON_BYTES`。

### 4.2 安装自测结果

```json
{
  "status": "ok",
  "stages": ["preprocess", "inference", "postprocess", "serialize"],
  "object_count": 0
}
```

- `status` 固定为 `ok`；
- `stages` 必须按顺序完整出现；
- `object_count >= 0`，不作为安装成败或精度判断依据；
- self-test result 不得包含图片请求。

## 5. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| manifest 未知字段、缺字段、格式错误 | `PACKAGE_MANIFEST_INVALID` |
| zip 文件集合与 `files[]` 不一致 | `PACKAGE_FILE_SET_MISMATCH` |
| SHA-256 不匹配 | `PACKAGE_CHECKSUM_MISMATCH` |
| 平台/OS/adapter 不兼容 | `PACKAGE_INCOMPATIBLE` |
| 请求 FPS 无对应档位或总 units 超限 | `RESOURCE_LIMIT_EXCEEDED` |
| 算法配置 Schema 失败 | `CONFIG_SCHEMA_INVALID` |
| 算法最终校验失败 | `AV_ERR_CONFIG_INVALID`，旧配置保留 |
| 告警类型未声明、bbox 越界、JSON 超限 | 丢弃该结果并记录 `ALGO_RESULT_INVALID` |
| self-test 格式错误 | 拒绝安装 |

## 6. Good / Base / Bad Cases

- Good：15 FPS 精确命中 15 FPS 档位；12 FPS 向上选择 15 FPS 档位。
- Base：测试图无目标，self-test 返回 `object_count=0` 并安装成功。
- Bad：manifest 只列模型目录，不列目录内文件；运行时无法证明包内容完整。

## 7. Tests Required

- manifest JSON Schema 正反例、未知字段、SemVer、路径规范化和重复文件测试。
- zip 实际文件集合、逐文件 SHA-256 和 `.mlpackage` tree digest 测试。
- FPS 精确命中、向上取档、超最大档、units 超限和内存门槛测试。
- 配置 good/base/bad、ROI 越界/自交、revision 过期和原子回滚测试。
- 告警零/多对象、未声明类型、重复 event ID、bbox 越界和大小上限测试。
- self-test 零检测成功与非法 stages/status 测试。

## 8. Wrong vs Correct

```json
// Wrong: 单点 units 无法定义 5/15/30 FPS 的资源变化
{"resource_units": 150, "preferred_fps": 15, "max_fps": 30}

// Correct: 离散、可验证、不可外推
{"fps_tiers": [{"fps": 5, "units": 60}, {"fps": 15, "units": 150}]}
```
