# 内置通用目标检测算法与 Frigate 风格多目标类别过滤

## Goal

将内置通用目标检测算法（`general_detection`，底层基于 YOLO26n NMS-Free 高性能架构）升级为系统开箱即用的**内置算法**，并在前端/后端/算法全链路实现类似 **Frigate** 的多目标类别自由选择与高效过滤能力。支持在前端任务配置时按需勾选关注的目标类别（COCO 80 类别，提供常用类别默认值），支持置信度阈值与类别过滤，支持用户自定义业务告警标签，并在 C++ 算法后处理层通过 `std::bitset<80>` 实现 $O(1)$ 早停过滤以大幅降低跟踪与规则判定算力消耗。

## Background

目前系统中的通用目标检测算法包仅支持全局 `confidence_threshold` 和 `iou_threshold`，无法在实例层指定过滤目标；算法包会将所有 80 类目标全部送入跟踪器（`SimpleTracker`）和多边形/跨线规则判定，容易引发无关物体误报并消耗不必要的 CPU/内存资源；同时算法包尚未内嵌至系统种子数据，部署后无法开箱即用。

通过在全链路（算法包 Schema、C++ 解析与后处理、后端 Seed 与 Schema 校验、前端动态分类面板与表单）贯彻目标过滤机制，可以实现如同 Frigate 一样强大、精准、友好的边缘目标分析体验。

## Requirements

- **R1. 算法 Schema 扩展 (`config.schema.json`)**
  - 遵循 JSON Schema Draft-07 受限子集规范；
  - 增加 `target_classes` 字段（`type: array`, `items.type: string`, `items.enum: [80 COCO classes]`, `uniqueItems: true`, `default: ["person", "car", "motorcycle", "bicycle", "bus", "truck"]`）；
  - 增加 `custom_alarm_label` 字符串字段（可选，用于用户填写自定义业务标签或场景备注）。

- **R2. C++ 算法包配置解析 (`src/core/config.hpp`)**
  - 升级 `JsonCursor` 支持安全解析字符串数组；
  - 构建 `kCocoClasses[80]` 与名称查找表，生成 `std::bitset<80>` 位图；
  - 保持对缺失可选字段的健壮默认值回退。

- **R3. 高性能后处理与目标过滤 (`src/postprocess/postprocessor.cpp` & `algo_entry.cpp`)**
  - 在后处理阶段，首先通过 `enabled_classes_mask.test(cls_id)` 执行 $O(1)$ 快速过滤；
  - 非关注目标直接丢弃，不执行 Letterbox 逆变换，不进入 `SimpleTracker` 跟踪，不执行 ROI/跨线规则判定；
  - 极大降低跟踪器计算开销和内存抖动。

- **R4. 结果上报与事件标记**
  - 确保检测到的目标在结果 JSON 中携带准确的 `label`（如 `person`, `car`）和 `confidence`；
  - 告警记录入库时正确记录 `target_label`。

- **R5. 内置算法种子数据 (`argus/internal/model/seed.go`)**
  - 在后端初始化 Seed 流程中自动注册通用检测算法基础信息（`AlgorithmID: "general_detection"`, `Name: "通用目标检测"`, `AlgorithmType: "object_detection"`, `AlarmTypeID: "object_detect"`）及其默认版本 `1.0.0`（包含 FPS 档位、完整 ConfigSchema 与 Manifest 元数据），实现开箱即用。

- **R6. 前端动态表单与分类面板体验 (`CategorizedClassSelector` + `SchemaForm.vue`)**
  - 依托前端动态 SchemaForm 体系，将 `target_classes` 渲染为支持分组折叠、一键全选、场景预设（常用人车、仅人员、交通工具、宠物看护等）的可视化分类胶囊面板（Checkable Tags）。

- **R7. 全链路国际化 (i18n) 契约与本地化映射**
  - **三语严格对齐**：同步更新 `zh-CN`、`en-US`、`zh-TW` 语言包；
  - **内置算法元数据 i18n**：算法名称与描述使用 i18n key（`ai.algorithm.general_detection.name` / `description`）；
  - **告警类型 i18n**：`object_detect` 在告警中心映射为通用目标检测告警；
  - **COCO 80 类别标签 i18n**：在 `ai.classes.*` 中提供 80 类别的中英繁对照字典，前端在分类面板与告警记录 `target_label` 中自动本地化显示（例如 `person` -> "人" / "Person" / "人"，`car` -> "汽车" / "Car" / "汽車"）。

- **R8. 质量与回归测试**
  - 为算法包添加针对 `target_classes` 过滤、畸形 JSON 配置、单类别/多类别/全类别过滤的完整 C++ 单元测试与 ABI 测试；
  - 为后端 paramschema 和 seed 编写验证测试；
  - 验证前端 i18n key 在 zh-CN、en-US、zh-TW 下无缺失和硬编码。

## Acceptance Criteria

- [x] AC1. 算法包的 `config.schema.json` 包含 `target_classes` 且符合 Draft-07 受限子集，通过后端 `paramschema.go` 编译和校验。
- [x] AC2. C++ 算法包单元测试覆盖不同 `target_classes` 配置：
  - 配置 `["person"]` 时，仅检测人，同一画面的车辆/动物被过滤；
  - 配置 `["car", "bus"]` 时，仅检测机动车；
  - 配置空或默认值时，按默认策略生效。
- [x] AC3. `make -C algo-packages/macos/arm64/yolo26n test`（或重命名后目录）全部通过，ABI 契约测试通过。
- [x] AC4. 后端启动 Seed 包含 `general_detection` 算法与版本数据，通过 `go test ./internal/...`。
- [x] AC5. 前端在任务配置界面可正常渲染 `target_classes` 分类胶囊面板，创建/更新任务时下发的 `params_json` 符合 Schema。
- [x] AC6. COCO 80 类目标及算法元数据完成 `zh-CN`、`en-US`、`zh-TW` 三语对齐，分类面板与告警列表显示正确的本地化文本。
- [x] AC7. 运行 `make -C engine test` 保持原有引擎能力不破坏。

## Out of Scope

- 模型本身的重新训练与权重微调（复用现有的 YOLO26n Core ML 模型）。
- 针对每个类别的独立神经网络模型切换（复用单模型多类别多头）。

## Technical Notes

- `config.schema.json` 必须满足 Draft-07 受限子集（禁止 `$ref`, `oneOf`, `anyOf`, `allOf` 等）。
- 算法包解析配置时不得使用全局环境变量，严格保证线程安全与零外部依赖。
- 后处理中的位图过滤必须位于 Letterbox 逆变换之前，以最大化加速后处理。
