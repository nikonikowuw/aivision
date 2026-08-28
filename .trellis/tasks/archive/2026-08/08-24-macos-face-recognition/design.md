# Technical Design: macOS Face Recognition

## 1. Architecture Boundaries

本任务跨越 `sdk`、`engine` 和 `algo-packages/macos/arm64` 三个边界，依赖方向保持 `engine -> sdk <- algo-packages`：

- `sdk/include/aivision/*.h` 是 C ABI 的唯一权威定义。
- `sdk/cmake/validate_package.cmake` 和 Engine manifest loader 共同执行安装期校验，但规则必须保持一致，不能各自发明算法类型语义。
- `engine/src/core/algo/algo_sandbox.cpp` 只负责包结构、metadata、一致性和 self-test 调度，不承载模型推理。
- `algo-packages/macos/arm64/face_recognition` 拥有模型加载、图像预处理、推理、后处理、跟踪和结果序列化。
- Core ML、CoreVideo、Accelerate 等平台 Framework 只出现在 macOS 算法包的 Objective-C++/CMake target 中，不进入公共 ABI 头和 `engine_core`。

## 2. Package Layout

```text
algo-packages/macos/arm64/face_recognition/
├── CMakeLists.txt
├── Makefile
├── manifest.json
├── config.schema.json
├── .env
├── testimage.jpg
├── testdata/
│   └── no_valid_face.jpg
├── lib/                         # build/package 输出
├── model/
│   ├── yolov8n.mlpackage
│   ├── scrfd_10g_bnkps.mlpackage
│   └── glintr100.mlpackage
├── weights/
│   ├── scrfd_10g_bnkps.onnx
│   └── glintr100.onnx
├── conversion/
│   ├── convert.sh
│   ├── requirements.txt
│   └── evidence.md
├── src/
│   ├── core/                    # C ABI、状态、配置、结果
│   ├── preprocess/              # NV12/CVPixelBuffer、letterbox、反变换、对齐
│   ├── inference/               # 三个 Core ML runner
│   ├── postprocess/             # YOLO/SCRFD decode、NMS、关联、embedding
│   └── runner/                  # 真实 CVPixelBuffer self-test/CLI
└── tests/
```

包内模型路径由 `.env` 以 package-root 相对路径声明；动态库不得调用 `std::getenv`，不得读取 CWD。

## 3. Manifest and ABI Evolution

### 3.1 Result kind

在 `sdk/include/aivision/result.h` 的现有枚举末尾追加：

```c
AV_RESULT_RECOGNITION = 3
```

不改变 `AV_RESULT_ALARM`、`AV_RESULT_SELF_TEST` 数值，不修改 `av_algo_result` 结构布局。

### 3.2 Library metadata

`av_algo_library_info` 保持现有 200 字节布局。识别包仍然写入固定容量的 `alarm_type_id` 字段，但写入空字符串；不能删除、缩短或移动该字段。

### 3.3 Conditional manifest validation

`algorithm_type` 允许 `object_detection` 和 `face_recognition`：

- `object_detection`：要求 `alarm_type_id`，并保持现有格式校验。
- `face_recognition`：`alarm_type_id` 可省略；若存在必须明确拒绝或按最终 schema 规则处理，避免产生隐式告警语义；首版 manifest 不包含它。
- Engine metadata 校验必须把 `face_recognition` 与空 ABI alarm 字段视为合法组合。
- `files[]`、`entry_library`、`config_schema_file`、`test_image_file` 的实际权威规则必须与当前项目规范和 convention-over-configuration 决策统一；实施时先确认 SDK validator、Engine validator 和包 helper 的差异，再只保留一个可执行契约。

相关同步面包括：公共 SDK 头、SDK CMake validator、Engine sandbox manifest parser、现有 vendored SDK、manifest schema 文档、mock/contract fixtures 和新包 manifest。

## 4. Runtime Data Flow

```text
av_frame_desc (NV12 HOST/CVPixelBuffer)
        │ validate size / format / dimensions / frame_id
        ▼
original RGB frame (original width × height)
        │ save letterbox scale + pad
        ▼
640 × 640 letterbox RGB
        ├──────────────► YOLOv8n ──► person class 0 ──► ByteTrack
        │
        └──────────────► SCRFD 10G bnkps ──► face boxes + 5 landmarks
                                           │ inverse letterbox
                                           ▼
                              original-frame boxes/landmarks
                                           │ center-in-person + IoU association
                                           ▼
                              five-point similarity from original RGB
                                           │
                                           ▼
                                      112 × 112 aligned face
                                           │ GLINTR100
                                           ▼
                              finite 512 float32 → L2 → Base64 LE
                                           │
                                           ▼
                           AV_RESULT_RECOGNITION JSON callback
```

YOLO 和 SCRFD 共用检测输入的 letterbox 结果，但识别输入不共用该图：对齐采样从原图 RGB 直接进行。letterbox 的 scale、padding、边界裁剪和归一化坐标转换必须由同一个 preprocess owner 管理，避免 YOLO、SCRFD、JSON 各自反算。

## 5. Detection, Association and Tracking

- YOLO 后处理只保留 COCO `person` class，按 `person_detection_threshold` 和现有 NMS 规则生成候选。
- ByteTrack 只保存 instance 私有轨迹；`track_buffer`、`track_match_threshold` 和最大输出人数由候选配置控制。
- SCRFD 后处理使用 `face_detection_threshold`、`face_nms_threshold`，解码五点关键点。
- 关联第一条件是人脸中心点落入人体框；多个人体包含时选择人脸/人体 IoU 最大者；每个人体最终最多一张脸，冲突时保留关联分数最高的一张；没有人体关联的脸丢弃。
- 所有对外 bbox/landmark 都使用原图坐标并裁剪/校验到 `[0,1]`，不得把 letterbox 坐标泄漏到结果。
- 序列化前按 `track_id` 升序排序，数组位置不承担身份语义。

## 6. Model Runtime

Library open 阶段解析 package root、读取私有 `.env`、加载并校验 YOLOv8n、SCRFD 和 GLINTR 三个 Core ML 模型；任一模型不可加载则整个 library 打开失败。Instance create 只构造配置、tracker、序列化缓冲和回调状态，不重复加载模型。

每个 runner 对输入名、输出名、shape、dtype 和有限值进行严格校验。GLINTR 必须产生单个 512 维向量；模型输出不能动态改变结果 schema。转换脚本保留原始输出，C++ 负责 SCRFD decode/NMS、相似变换和 GLINTR 后处理。

同一 instance 的 process 串行、不同 instance 可并行。模型共享策略必须经过实际 Core ML 并发测试；如果共享 `MLModel` 对象不能安全重入，应使用不改变 library 资源所有权语义的细粒度预测上下文，而不是把所有 instance 静默串成一条全局队列。

## 7. Configuration and State

配置解析按以下顺序形成候选状态：

```text
instance_args.config_json / instance_update_config
        > package_root/.env
        > compiled defaults
```

候选状态完整校验后原子替换；失败保留旧配置并返回 `AV_ERR_CONFIG_INVALID`。模型契约参数（模型类型、输入尺寸、512 维输出、L2/encoding）不暴露为业务动态参数。

Instance 状态至少包括：last accepted frame ID、track map、next track ID、configuration、last error、callback context 和 self-test flag。`frame_id` 首帧可任意合法值，之后严格递增；flush 清空轨迹、重置 ID 和帧序列门槛，destroy 释放全部资源。

## 8. Callback and Error Matrix

- 无人体：process 成功、零回调。
- 有人体且无有效脸：回调一条 `AV_RESULT_RECOGNITION`，人体 `face: null`。
- 有效脸和部分失败：成功 embedding 正常输出，失败人体 `face: null`。
- 检测到脸但本帧所有 embedding 失败：`AV_ERR_INFERENCE_FAILED`，零回调。
- 模型未加载、输入非法、Core ML 全局失败：返回对应 ABI 错误，零回调。
- 重复/倒退 frame ID：`AV_ERR_INVALID_ARG`，不运行 pipeline。
- 所有 ABI 入口捕获 C++/Objective-C++ 异常并转换为稳定 `av_algo_status`。

结果 JSON 必须不超过 `AV_MAX_RESULT_JSON_BYTES`，embedding data 的字节长度必须是 `512 * sizeof(float)` 的 Base64 解码结果，向量范数在约定误差内。

## 9. Compatibility and Rollback

- 旧 `object_detection` 包、manifest、结果 kind 和 `alarm_type_id` 行为必须通过回归测试。
- 新类型若无法通过当前 Engine validator，应先扩展契约并同步 vendored SDK，再安装新包；不能通过绕过校验或伪造告警 ID 上线。
- 模型转换或 Core ML 验证失败时，保留源 ONNX 和转换证据，包不进入可安装产物；不提交无法验证的二进制替代物。
- 包 zip 使用外部整体 SHA-256，安装 validator 仍执行路径、文件、ABI 和 self-test 门禁。
