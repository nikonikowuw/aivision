# macOS face recognition algorithm package

## Goal

在 Apple Silicon macOS 上新增可独立安装和验证的 `face_recognition` 算法包，接收现有 NV12/CVPixelBuffer 帧，在包内完成“人体检测、人体跟踪、人脸检测、原图取脸、五点对齐和特征提取”，向上游输出可用于后续图库检索的 512 维归一化 embedding。

业务价值是让上游服务只负责图库、1:N/1:1 相似度匹配和身份管理，而不需要重复实现视频帧上的检测、对齐和特征提取。

## In Scope

### 1. SDK、Engine 和 manifest 契约

- 正式支持 `algorithm_type: "face_recognition"`，不再把它视为预留枚举。
- 在 `av_result_kind` 末尾追加 `AV_RESULT_RECOGNITION = 3`，保持已有枚举值不变。
- 保持 `av_algo_library_info` 的布局、大小和 `alarm_type_id[64]` 字段不变。
- 人脸识别包的 `alarm_type_id` 在 manifest 中不再强制要求；`library_query` 将 ABI 字段填为空字符串。
- 目标检测包继续要求非空 `alarm_type_id`，既有 `object_detection` 行为保持兼容。
- manifest、Engine 加载器、SDK 校验脚本、vendored SDK 和契约测试必须对上述条件使用同一套规则。

### 2. 算法包身份与资产

包路径、身份和平台固定为：

```text
algo-packages/macos/arm64/face_recognition/
algorithm_id: face_recognition
version: 1.0.0
algorithm_type: face_recognition
platform_id: macos-arm64-coreml
```

包必须自包含并遵循现有约定目录，至少包括：

- `manifest.json`
- `.env`
- `config.schema.json`
- `testimage.jpg`
- `lib/libface_recognition.dylib`
- `model/`
- `weights/`
- `conversion/`

模型来源固定为：

```text
/home/nikoniko/work/tentcoo/rknn_model_zoo/examples/insightface/antelopev2
```

首版复制并使用：

- `scrfd_10g_bnkps.onnx`
- `glintr100.onnx`

首版不使用 `genderage.onnx`、`2d106det.onnx` 和 `1k3d68.onnx`。人体检测模型复用现有 YOLOv8n Core ML 模型，但复制到本包的 `model/` 中，不依赖其他算法包运行。

`weights/` 保存可审计的源 ONNX 权重，`model/` 保存实际运行时 Core ML 模型；`conversion/` 保存 `coremltools 9.0` 转换脚本和输出/哈希证据。

### 3. 识别流水线

每个输入帧按以下流程处理：

1. 校验 ABI、帧格式、尺寸和严格递增的 `frame_id`。
2. 将原始 NV12/CVPixelBuffer 转换为算法内部可访问的原图 RGB 数据，同时保留原始尺寸。
3. 将图像 letterbox 到 `640x640`。
4. 在同一张 `640x640` letterbox 图上运行 YOLOv8n 和 SCRFD。
5. YOLOv8n 只保留 COCO class `person`，并由 ByteTrack 维护人体轨迹。
6. SCRFD 输出的人脸框和五点关键点反变换回原图坐标。
7. 以人脸中心落入人体框为基本条件完成关联；多个人体候选时按 IoU 选择最佳人体；同一人体多张脸时只保留关联分数最高的一张；未关联的人脸丢弃。
8. 使用反变换后的五点关键点，直接从原图执行五点相似变换，生成 `112x112` 对齐脸图；禁止从 `640x640` letterbox 图直接截脸后再识别。
9. 运行 `glintr100`，严格验证输出为 512 维有限 `float32`，执行 L2 归一化并 Base64 编码。
10. 按 `track_id` 升序序列化结果。

### 4. 结果协议

正常识别结果使用 `kind == AV_RESULT_RECOGNITION`，JSON 顶层固定包含 `schema_version: 1` 和 `persons`。每个人体最多一个 `face`，没有有效人脸时为 `null`。坐标全部是原图归一化坐标：bbox 为 `[x, y, w, h]`，landmark 为 `[x, y]`。

embedding 对象必须包含：

```json
{
  "model": "glintr100",
  "dimension": 512,
  "dtype": "float32",
  "normalized": true,
  "encoding": "base64",
  "byte_order": "little_endian",
  "data": "..."
}
```

`data` 是 512 个 IEEE 754 little-endian `float32` 原始字节的 Base64 表示，不使用 JSON 数字数组、不压缩、不量化。

### 5. 生命周期、配置和错误语义

- library 级一次性加载并共享三个 Core ML 模型；instance 级保存配置、ByteTrack 状态、轨迹分配器、错误和回调上下文。
- 同一 instance 串行处理且不支持重入；不同 instance 可并行。
- `track_id` 从 1 递增，只在单个 instance 生命周期内有效；`instance_flush` 清理轨迹并重置分配器；实例重启不保证延续。
- 后续 `frame_id` 必须严格大于上一次成功接收的值；重复或倒退帧返回 `AV_ERR_INVALID_ARG`，不推理、不回调；flush 后允许从任意新值开始。
- 配置优先级为 `instance_update_config / instance_args` > 包根目录 `.env` > 编译期默认值。
- 默认参数为：人体阈值 `0.35`、人脸阈值 `0.50`、人脸 NMS `0.40`、最大人体数 `16`、跟踪缓存 `30` 帧、轨迹匹配阈值 `0.80`。模型输入尺寸、embedding 维度和归一化契约不可由业务配置覆盖。
- 无人体时不回调；有人体时回调 `persons[]`，没有有效人脸的人体保留为 `face: null`。
- 部分人脸特征提取失败时，成功目标继续输出，失败目标使用 `face: null`。
- 当前帧检测到人脸但所有 embedding 都失败时，返回 `AV_ERR_INFERENCE_FAILED` 且不回调；全局模型/输入故障同样返回错误。
- `instance_flush` 不额外生成识别结果。

## Out of Scope

- 图库管理、身份注册、1:N/1:1 相似度搜索和具体人员身份返回。
- gender/age、3D landmark 和其他 antelopev2 模型。
- RKNN、RK3576、Linux 或非 Apple Silicon 运行时移植。
- 质量模型、质量分数和 AdaFace 等替代识别模型。
- 修改既有 YOLOv8n 算法包的检测输出或回调语义。
- 前端页面和 Go 业务层的人员库产品功能；本任务只提供 SDK/Engine 可加载的算法结果契约。

## Acceptance Criteria

### A. 契约和包加载

- [ ] `face_recognition` manifest 可通过 manifest/schema 校验，`alarm_type_id` 缺省合法；`object_detection` manifest 的原有必填行为不变。
- [ ] SDK 和 vendored SDK 编译后包含 `AV_RESULT_RECOGNITION == 3`，所有已有 ABI `sizeof/offsetof` 断言不变。
- [ ] Engine 能加载、查询并校验 `face_recognition`；查询结果的 `algorithm_id`、版本、类型匹配 manifest，`alarm_type_id` 为空。
- [ ] 包复制到仓库外仍可 configure/build/package，运行时只依赖 package root 下的文件，不依赖 CWD、全局环境变量或其他算法包。

### B. 模型和流水线

- [ ] 包含指定来源的 `scrfd_10g_bnkps.onnx`、`glintr100.onnx` 和本包自己的 YOLOv8n Core ML 模型。
- [ ] `coremltools 9.0` 转换产物可加载，转换证据记录输入尺寸、输出名称/shape、预处理、误差和 SHA-256。
- [ ] YOLOv8n 和 SCRFD 都在 `640x640` letterbox 图上检测；检测结果正确反变换到原图。
- [ ] 人脸对齐采样源是原图，五点相似变换输出 `112x112`，不是从 letterbox 图二次截取。
- [ ] GLINTR 输出严格为 512 维有限向量，最终 L2 范数在约定容差内。

### C. 结果和状态

- [ ] 正常结果 `kind` 为 `AV_RESULT_RECOGNITION`，JSON 满足 `schema_version: 1`、原图归一化坐标、单人体单人脸和 embedding 二进制契约。
- [ ] 结果按 `track_id` 升序；未关联人脸被丢弃；人体无脸输出 `face: null`。
- [ ] 连续帧中的 track ID 稳定，flush 重置状态，实例之间的可变跟踪状态不互相污染。
- [ ] 无人体不回调；有人体按规则回调；部分 embedding 失败可降级；全部 embedding 失败返回错误且不回调。

### D. 配置和调用边界

- [ ] 配置 schema、`.env` 和运行时覆盖实现六个默认参数及三层优先级，非法更新保留旧配置。
- [ ] 同一 instance 串行、不同 instance 可并行；倒退/重复 `frame_id` 被拒绝。
- [ ] library/instance 创建、flush、destroy 的模型和跟踪资源释放完整，错误不跨越 C ABI。

### E. 测试和质量证据

- [ ] 提供多人体/多脸清晰测试图和有人体但无有效脸的负例图，记录来源、许可证和 SHA-256。
- [ ] 单测覆盖 letterbox 反变换、原图五点对齐、人体/人脸关联、track 生命周期、配置优先级、Base64 解码/L2 范数和结果排序。
- [ ] 契约测试覆盖 `AV_RESULT_RECOGNITION`、manifest 条件字段、回调次数、错误语义、ABI 版本/大小和多实例并发边界。
- [ ] 运行并记录包 configure/build/test/asan/package、Engine build/test/asan/tsan/lint、SDK consistency 和可搬运性检查；无法执行的 macOS/Core ML 项必须明确记录原因。
- [ ] ONNX 与 Core ML 的 SCRFD 框/关键点误差、GLINTR embedding 余弦相似度、端到端延迟和内存证据已保存；身份匹配准确率不作为本包验收项。

## Risks and Deferred Verification

- 需要在实际 macOS 环境确认三个 ONNX 模型的真实输入输出名称、固定尺寸和 Core ML 转换兼容性；这是实现前技术验证，不改变已确认的流水线。
- 需要确认共享 Core ML 模型对象在不同 instance 并发调用下的线程安全策略，并以测试结果决定是否使用细粒度推理上下文/同步。
- antelopev2 权重的许可证和再分发条件需要在打包前记录；不满足再分发条件时，任务应保留转换脚本和来源校验方式，但不能把权重静默标成可发布资产。

## Blocking Open Questions

无。产品行为、范围、兼容性和错误语义已经确认；上述项目属于实现阶段可验证的技术风险。
