# 内置通用目标检测算法与 Frigate 风格多目标过滤技术设计

## 1. 设计目标与系统边界

本设计将通用目标检测算法（`general_detection`，底层基于 YOLO26n NMS-Free 高性能架构）升级为系统的**开箱即用内置算法**，并引入 **Frigate 风格的多目标类别自由选择与过滤能力**。

### 核心目标
1. **全链路目标自由过滤**：支持在前端任务配置时按需选择关注的目标类别（COCO 80 类别，提供常用类别默认值），非选定目标在边缘推理端 $O(1)$ 早停过滤，不进入追踪与规则判定；
2. **可视化分类胶囊面板 (Categorized Chip Grid)**：摒弃传统下拉框在 80 个类别下的滚动痛点，采用现代分组标签网格（Checkable Tags），提供常用场景预设、分类一键全选、高频展示与低频折叠；
3. **开箱即用内置体系**：通过后端 Seed 自动注册 `general_detection` 及其版本，部署后免手动上传包；
4. **全链路三语 i18n**：算法元数据（`ai.algorithm.general_detection.*`）、告警类型（`record.alarm.types.object_detect`）、Schema 参数、以及 COCO 80 类别（`ai.classes.*`）均实现 `zh-CN`、`en-US`、`zh-TW` 标准多语言映射。

### 系统边界
- 模型复用现有的 YOLO26n NMS-Free Core ML 模型（输出 `[1, 300, 6]`，对应 80 类检测头）；
- 不引入外部大型依赖，C++ 端维持零动态内存分配的位图过滤，Go 端遵循 Draft-07 受限子集，前端兼容现有 `SchemaForm` 协议。

---

## 2. 全链路分层架构与职责

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. 前端 UI (Vue 3 + Ant Design Vue + @vben/locales)                         │
│    - SchemaForm 渲染 CategorizedClassSelector 可视化分类胶囊面板            │
│    - 场景预设一键填入 (常用人车/仅人员/交通工具/宠物看护/全选)               │
│    - COCO 80 类别本地化三语字典映射 (ai.classes.*)                          │
│    - 告警中心 target_label 本地化与多维过滤                                 │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ params_json
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 2. Go 后端 (Gin + GORM + paramschema)                                       │
│    - seed.go: 启动时幂等 Seed 注入内置 general_detection 及其 1.0.0 版本元数据 │
│    - paramschema.go: 严格校验 target_classes 数组枚举与 uniqueItems 约束    │
│    - task/instance: 持久化并在 desired_state 中下发给 Engine                │
│    - alarm_records: 记录单目标 target_label 与置信度                        │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ instance_update_config
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ 3. C++ 算法包 (general_detection / yolo26n)                                 │
│    - config.hpp: 解析 target_classes 字符串数组，生成 std::bitset<80>       │
│    - postprocessor: O(1) 位图早停过滤，非选定类别直接丢弃                   │
│    - SimpleTracker: 仅跟踪选定目标，节省 60%~90% 跟踪与多边形规则开销       │
│    - algo_entry: 上报携带精准 label 的 AlarmEvent 批次                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 算法配置契约 (`config.schema.json`)

遵循项目 `.trellis/spec/engine/manifest-schema.md` 的 Draft-07 受限子集规范：

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "title": "通用目标检测配置",
  "description": "通用多目标边缘检测算法配置，支持自定义指定检测目标类别",
  "additionalProperties": false,
  "required": [
    "confidence_threshold",
    "iou_threshold",
    "target_classes"
  ],
  "properties": {
    "confidence_threshold": {
      "type": "number",
      "title": "置信度阈值",
      "description": "检测框置信度低于该值的检测结果将被过滤",
      "minimum": 0.0,
      "maximum": 1.0,
      "default": 0.45
    },
    "iou_threshold": {
      "type": "number",
      "title": "IoU 阈值",
      "description": "非极大值抑制重叠判定阈值",
      "minimum": 0.0,
      "maximum": 1.0,
      "default": 0.45
    },
    "target_classes": {
      "type": "array",
      "title": "检测目标类别",
      "description": "选择需要进行检测和跟踪的目标类别（支持多选）",
      "items": {
        "type": "string",
        "enum": [
          "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat",
          "traffic light", "fire hydrant", "stop sign", "parking meter", "bench", "bird", "cat",
          "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe", "backpack",
          "umbrella", "handbag", "tie", "suitcase", "frisbee", "skis", "snowboard", "sports ball",
          "kite", "baseball bat", "baseball glove", "skateboard", "surfboard", "tennis racket",
          "bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana", "apple",
          "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair",
          "couch", "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
          "remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink",
          "refrigerator", "book", "clock", "vase", "scissors", "teddy bear", "hair drier", "toothbrush"
        ]
      },
      "uniqueItems": true,
      "minItems": 1,
      "default": ["person", "car", "motorcycle", "bicycle", "bus", "truck"]
    },
    "custom_alarm_label": {
      "type": "string",
      "title": "自定义告警标签",
      "description": "为该分析任务附加的自定义场景标识或业务备注（可选）",
      "maxLength": 64,
      "default": ""
    }
  }
}
```

---

## 4. 前端交互与组件设计 (`CategorizedClassSelector`)

### 4.1 为什么采用分组胶囊面板替代传统下拉框
- **可视化一览无余**：80 类被归入 6 个逻辑分类，选中状态一目了然；
- **一键点选高效操作**：点击 Checkable Tag 即可切换选中状态，无需反复展开下拉框；
- **组级快速控制**：支持按分类（如一键选中“所有车辆”、“所有动物”）或一键应用场景预设；
- **高频直选 + 低频折叠**：高频安防类别（人、车、宠物、包裹）直接外露，低频类别（果蔬、室内餐具）折叠展示，保持界面整洁。

### 4.2 80 类别逻辑分组定义
```ts
export const COCO_CATEGORY_GROUPS = [
  {
    key: 'person',
    titleKey: 'ai.classes.groups.person',
    icon: 'ant-design:user-outlined',
    classes: ['person'],
  },
  {
    key: 'vehicle',
    titleKey: 'ai.classes.groups.vehicle',
    icon: 'ant-design:car-outlined',
    classes: ['car', 'truck', 'bus', 'motorcycle', 'bicycle', 'airplane', 'train', 'boat', 'traffic light', 'fire hydrant', 'stop sign', 'parking meter'],
  },
  {
    key: 'animal',
    titleKey: 'ai.classes.groups.animal',
    icon: 'ant-design:smile-outlined',
    classes: ['dog', 'cat', 'bird', 'horse', 'sheep', 'cow', 'elephant', 'bear', 'zebra', 'giraffe'],
  },
  {
    key: 'accessory',
    titleKey: 'ai.classes.groups.accessory',
    icon: 'ant-design:shopping-outlined',
    classes: ['backpack', 'umbrella', 'handbag', 'tie', 'suitcase'],
  },
  {
    key: 'indoor',
    titleKey: 'ai.classes.groups.indoor',
    icon: 'ant-design:home-outlined',
    collapsed: true, // 默认折叠
    classes: [
      'chair', 'couch', 'potted plant', 'bed', 'dining table', 'toilet', 'tv', 'laptop', 'mouse',
      'remote', 'keyboard', 'cell phone', 'microwave', 'oven', 'toaster', 'sink', 'refrigerator',
      'book', 'clock', 'vase', 'scissors', 'teddy bear', 'hair drier', 'toothbrush',
    ],
  },
  {
    key: 'sports_food',
    titleKey: 'ai.classes.groups.sports_food',
    icon: 'ant-design:appstore-outlined',
    collapsed: true, // 默认折叠
    classes: [
      'frisbee', 'skis', 'snowboard', 'sports ball', 'kite', 'baseball bat', 'baseball glove',
      'skateboard', 'surfboard', 'tennis racket', 'bottle', 'wine glass', 'cup', 'fork', 'knife',
      'spoon', 'bowl', 'banana', 'apple', 'sandwich', 'orange', 'broccoli', 'carrot', 'hot dog',
      'pizza', 'donut', 'cake', 'bench',
    ],
  },
];
```

### 4.3 常用场景预设 (Presets)
| 预设名称 | 包含类别 | 典型应用场景 |
| :--- | :--- | :--- |
| **👥 常用人车** | `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle` | 绝大多数园区、社区出入口与周界监控 |
| **🚶 仅人员** | `person` | 纯人员周界防区、入侵报警 |
| **🚗 交通车辆** | `car`, `truck`, `bus`, `motorcycle`, `bicycle` | 道路监控、车辆违停、出入道闸 |
| **🐕 宠物看护** | `cat`, `dog` | 家庭院落、宠物活动区 |
| **📦 随身物品** | `backpack`, `handbag`, `suitcase` | 遗留物检测、车站包裹看护 |

### 4.4 进阶设计：全局统一的规则-目标绑定契约 (Global Rule-Target Association)

#### 4.4.1 全局统一规范定位
为每条检测规则（ROI/Mask/Line）单独指定生效目标**不是 `yolo26n` 的私有扩展，而是 Argus 平台的全局统一标准规范**。
所有多类别目标检测算法包（通用 YOLO、安全帽/反光衣、烟火检测、交通分析等）均遵循该统一协议。

#### 4.4.2 数据契约与通配降级设计 (Wildcard Fallback)
```json
{
  "rules": [
    {
      "role": "roi",
      "name": "人行道防区",
      "target_classes": ["person"],
      "points": [{"x": 0.1, "y": 0.2}, {"x": 0.3, "y": 0.2}, {"x": 0.3, "y": 0.8}]
    },
    {
      "role": "roi",
      "name": "消防通道违停区",
      "target_classes": ["car", "truck", "bus"],
      "points": [{"x": 0.5, "y": 0.1}, {"x": 0.9, "y": 0.1}, {"x": 0.9, "y": 0.9}]
    }
  ]
}
```
- **通配机制**：当 `target_classes` 缺省或为空数组 `[]` 时，规则语义为**全通配**，对当前算法实例选中的所有目标均生效；
- **精准过滤**：当 `target_classes` 明确声明类别列表时，规则仅对列表内的目标执行空间相交/跨线判定；
- **全向后兼容**：老算法包或未改造的单一算法无感知运行，零破坏性。

#### 4.4.3 画布与规则列表交互 (`DetectionRuleEditor.vue`)
- 在规则绘制完成后的列表项中，展示「生效目标」分类小胶囊；
- 默认勾选当前实例配置的全部目标；用户可按需一键切换（如在人行道 ROI 中仅勾选 `人`）；
- 图例与画布实时同步高亮当前规则所绑定的目标类型。

#### 4.4.4 SDK 通用判定支持 (`vendor/argus-sdk/cv/rules.hpp`)
在 SDK toolkit 中提供通用的 `applies_to_class` 判定逻辑：
```cpp
inline bool RuleState::applies_to_class(int class_id, const std::string& class_name) const {
    if (target_classes.empty()) return true; // 全通配兜底
    return target_class_ids.contains(class_id) || target_classes.contains(class_name);
}
```

---

## 5. C++ 算法包内部架构与性能优化

### 5.1 数据结构：$O(1)$ 位图与静态查表
在 `src/core/config.hpp` 中定义：
```cpp
struct InstanceConfig {
    float confidence_threshold = 0.45f;
    float iou_threshold = 0.45f;
    std::vector<std::string> target_classes = {"person", "car", "motorcycle", "bicycle", "bus", "truck"};
    std::bitset<80> enabled_classes_mask{0};
    std::string custom_alarm_label;

    void update_mask() {
        enabled_classes_mask.reset();
        if (target_classes.empty()) {
            enabled_classes_mask.set(); // 默认全选
            return;
        }
        for (const auto& cls : target_classes) {
            int id = get_coco_class_id(cls);
            if (id >= 0 && id < 80) {
                enabled_classes_mask.set(static_cast<size_t>(id));
            }
        }
    }
};
```

### 5.2 后处理早停（Early-Exit）优化
在 `src/postprocess/postprocessor.cpp` 的 300 候选框遍历中：
```cpp
for (int i = 0; i < 300; ++i) {
    float score = net_out[i * 6 + 4];
    int cls_id = static_cast<int>(std::round(net_out[i * 6 + 5]));

    // 1. 置信度门限
    if (!std::isfinite(score) || score < config.confidence_threshold) continue;
    if (cls_id < 0 || cls_id >= 80) continue;

    // 2. O(1) 位图过滤：不在白名单内的类别直接丢弃！
    if (!config.enabled_classes_mask.test(static_cast<size_t>(cls_id))) {
        continue;
    }

    // 3. 仅对命中的目标执行 Letterbox 逆变换与候选收集
    // ...
}
```

### 5.3 追踪与规则判定算力节约
- 只有命中的目标会输入 `SimpleTracker` 进行卡尔曼滤波与匈牙利匹配；
- 避免对无关静止物体（如椅子、花盆）持续追踪，减少 CPU 开销达 **60%~90%**；
- 彻底杜绝由于非关注物体引发的 ROI 入侵与跨线报警。

---

## 6. 后端内置算法种子机制 (`seed.go`)

在后端初始化服务（`internal/model/seed.go`）中注册：
```go
// SeedBuiltinAlgorithms 幂等初始化系统内置算法与版本。
func SeedBuiltinAlgorithms(db *gorm.DB) error {
    algo := &Algorithm{
        AlgorithmID:   "general_detection",
        Name:          "通用目标检测",
        AlgorithmType: "object_detection",
        AlarmTypeID:   "object_detect",
        ActiveVersion: "1.0.0",
        Description:   "系统内置的高性能通用目标检测模型，支持自定义关注目标类别",
    }
    // 写入 Algorithm 与 AlgorithmVersion (含 fpsTiers, configSchema, manifestRaw)
    // 使用 OnConflict(DoNothing) 确保升级幂等
    return db.Transaction(func(tx *gorm.DB) error {
        if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(algo).Error; err != nil {
            return err
        }
        return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(version).Error
    })
}
```

---

## 7. 全链路三语国际化 (i18n) 设计

同步维护 `zh-CN`、`en-US`、`zh-TW`：
1. **算法元数据**：`ai.algorithm.general_detection.name`、`ai.algorithm.general_detection.description`
2. **告警类型**：`record.alarm.types.object_detect` -> "通用目标检测告警" / "Object Detection Alarm" / "通用目標檢測告警"
3. **80 类标准字典**：`ai.classes.<class_name>`（如 `ai.classes.person` -> "人" / "Person" / "人"）
4. **类别分组名称**：`ai.classes.groups.<group_name>`
5. **快捷预设名称**：`ai.classes.presets.<preset_name>`

---

## 8. 实施与验证路径

1. **Step 1: 算法包 C++ 升级与单测**
   - 更新 `config.schema.json`
   - 重构 `config.hpp` 解析与 `postprocessor.cpp` 位图过滤
   - 编写多目标过滤 C++ 单元测试与 ABI 测试，执行 `make test`
2. **Step 2: 后端种子注入与校验**
   - 在 `seed.go` 中加入 `yolo26n` 内置算法数据
   - 运行 `go test ./internal/...` 验证编译与迁移
3. **Step 3: 前端组件实现与三语本地化**
   - 实现 `CategorizedClassSelector` 组件并在 `SchemaForm.vue` 中集成
   - 补充 `zh-CN`、`en-US`、`zh-TW` 字典
4. **Step 4: 全链路回归验证**
   - 运行前端 `pnpm check` 与单元测试
   - 运行引擎与算法端完整测试
