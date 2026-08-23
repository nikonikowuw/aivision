# C++ Engine 框架与 macOS 运行平台（T1）

## Goal

从零搭建 PRD 所定义的「C++ 媒体推理引擎」平台无关核心层，并交付首个**功能完整**的 `macos-arm64-coreml` 运行平台适配层，使目标检测链路在 macOS 开发机上端到端跑通：RTSP 拉流 → 解码 → 共享帧 → 多算法采样分发 → Core ML 插件算法包（版本化 C ABI）→ 告警结果回调 → 图片落盘 → gRPC 上报。

用户价值：在没有 RK3576/RKNN 开发板的情况下先行推进引擎主体，并用一条真实数据流验证平台抽象边界与插件边界，使后续 `rk3576-rknn`、`ascend310b-cann` 等平台只需新增适配层与算法包，不必修改核心层、算法包 C ABI 或跨进程协议。

## Background / 已确认事实

参考需求：`docs/prd/ai-video-analytics-edge-platform-v1.0.md`（V1.0，待评审）

- §6.2：Go 与 C++ 为独立进程，gRPC/Protobuf 契约，V1.0 传输走 Unix Domain Socket；视频帧不得跨进程；断线重连后按 Go 保存的期望状态全量对账。
- §6.3：核心层不得直接依赖 RKNN/MPP/RGA/ZLM/CUDA；每个运行平台通过适配层注册唯一 `platform_id` 并提供版本化能力档案；一个算法包构建只面向一个明确运行平台；交付「不访问真实硬件的契约测试适配器」。
- §7.2.3：推理与预览必须复用同一个上游媒体源；媒体回调只允许把编码帧引用转移到有界队列。
- §7.4.3：通用帧描述符字段（`platform_id`、内存类型、像素格式、布局、有效/分配宽高、plane offset、stride、modifier、时间戳、`frame_id`、不透明平台扩展）；每算法实例独立采样，帧队列容量受限，过载丢旧留新。
- §7.4.4：只读引用计数帧句柄，引用归零前不得复用底层缓冲；算法不得释放平台原生句柄。
- §7.5.3：算法包只暴露版本化 C ABI，结构体带 `size` / `api_version`，跨 ABI 不传 STL 容器 / C++ 对象 / 异常。
- §7.5.4：安装校验七步（结构、清单与兼容性、路径安全、加载、`testimage.jpg` 全流程推理、结果 Schema 校验、不崩溃不超时）。
- §7.6：受限 JSON Schema 驱动参数，整份配置原子热更新。
- §7.7：加速计算总容量归一化为 1000 资源单位；超配拒绝；内存有独立安全阈值；不同 `platform_id` 的资源数值不可横向比较。
- §7.10：目标检测算法每次回调 = 一条完整独立告警，携带唯一 `event_id`，平台不二次判定。
- §7.11：图片由 C++ 统一管理，临时文件 + 原子重命名；Go 只持有图片 ID 与受限相对路径。
- §7.15：六项通用监控指标，不支持的指标显示「当前平台不支持」，不得伪造为 0。

仓库现状（已核实）：

- 无任何 C/C++ 源码、`CMakeLists.txt` 或 `.proto` 文件 —— 本任务从零开始。
- Go 服务位于 `app/`（Gin + GORM + wire，module `niko-vue-admin/app`），**未引入 gRPC 依赖**；本任务不修改 `app/`。
- 前端位于 `ui/`（vben admin 脚手架），本任务不涉及。
- `.trellis/spec/` 仅有 `backend`(Go) / `frontend` / `guides`，尚无 C++ 编码规范。

## Key Decisions

- **D1**：macOS 是一等运行平台，交付功能完整的适配层与部署 Profile，而非 walking skeleton 或 mock。系统设计之初即已纳入 macOS，原计划先做 RKNN；因当前无 RK3576 开发板改为 macOS 先行。`rk3576-rknn` 后续作为第二个适配层接入，核心层不得因此改动。
- **D2**：媒体后端在 macOS 与 RK3576 上统一使用 ZLMediaKit（官方支持 macOS 构建）。因此「摄像头上游接入 / 统一媒体源 / 浏览器预览」归属核心层，不进入平台适配层；适配层差异面收敛为：解码、帧内存、图像处理、JPEG 编码、推理运行时、资源配额、设备指标。
- **D3**：macOS 推理运行时为**纯 Core ML（`.mlpackage`）**，优先走 ANE。算法包发布 Core ML 模型工件，并按 PRD §7.5.6 独立维护转换证据（coremltools 版本、转换配置、量化校准来源、`.mlpackage` SHA-256、构建报告），与 RKNN 的 ONNX→RKNN 链路只共用源模型身份，不共用模型工件。
- **D4**：本任务**不含人脸识别子系统**（PRD §7.8.3–7.8.5、§7.9）。gRPC proto 中预留人员/特征/索引相关 service 与 message 定义，本任务不实现其 handler。
- **D5**：本任务**不修改 `app/` 下任何 Go 产品代码**。跨进程契约由 `.proto` + C++ 侧 gRPC client/server + 一个**测试用 Go stub server**（独立 `go.mod`，置于 engine 测试目录）验证。Go 侧产品实现留给 T2。
- **D6**：解码链路为 ZLM 解封装出 H.264/H.265 NAL → VideoToolbox 硬解 → `CVPixelBuffer`，不引入 FFmpeg 依赖。图像处理用 Accelerate/vImage，JPEG 编码用 ImageIO，均为 macOS 系统框架。
- **D7（插件边界与可搬运性）**：算法包是**自包含、可独立搬运的模块化插件工程**。
  - **模块化 CMake 架构**：算法包内部按职责拆分为独立子模块（`preprocess/`、`inference/`、`postprocess/`、`core/`、`runner/`），每个模块拥有独立的 `CMakeLists.txt` 并编译为独立静态/对象库目标。修改单个模块只需增量编译对应模块，极大提升开发迭代速率与代码解耦度。
  - **独立可搬运硬判据**：把单个算法包目录复制到一台只有编译器与平台运行时的机器（例如把 `rknn/rk3576/yolov8n/` scp 到开发板）后，无需 clone 本仓库、无需额外拷贝任何目录，即可完成构建并产出合规分发包。为此 SDK 头与 cmake helper 以 **vendored 副本**内置于每个算法包的 `vendor/aivision-sdk/`，不使用指向仓库其他位置的路径依赖。引擎经七步校验后把分发包解压到安装根目录 `var/packages/<algorithm_id>/<version>/` 并 dlopen；引擎源码树中不存在任何具体算法实现，也不提供从源码目录直接加载的旁路。
- **D8（目录组织与 `platform_id`）**：`algo-packages/<平台族>/<型号>/<算法>/` 三层。平台族与型号目录**仅为人类可读的组织方式，不进入任何协议**；`platform_id` 以算法清单字段为唯一权威，格式 `<型号>-<推理运行时>`（全小写连字符）。CI 断言「路径推导出的 platform_id == 清单声明值」以堵死错位。映射示例：`macos/arm64/yolov8n` → `macos-arm64-coreml`、`rknn/rk3576/yolov8n` → `rk3576-rknn`、`ascend/ascend310b/yolov8n` → `ascend310b-cann`。同一算法在不同型号下是两份完整独立工程，不做跨型号代码共享（型号间代码本就不一致，共享层会破坏单目录可搬运性）。
- **D9（macOS 兼容性粒度）**：`macos-arm64-coreml` 单包覆盖全部 Apple Silicon，不按 M1/M2/M3/M4 拆分——`.mlpackage` 与芯片代际解耦，硬件特化发生在设备端首次加载编译时，`.dylib` 只区分 CPU 架构。清单中需声明的兼容维度是 `min_os_version`（Core ML 特性随 OS 版本发布）；芯片代际差异体现为资源单位档位不同，靠保守取值兜底。不支持 Intel Mac。
- **D10（SDK 副本同步）**：`scripts/sync-sdk.sh` 做**单向同步**（上游 `sdk/` → 各算法包 `vendor/aivision-sdk/`，禁止反向）；CI 比对每个包 vendor 目录与上游的 SHA-256，不一致即失败。T1 期间 ABI 未冻结，强制全部仓内算法包跟随最新 SDK，第一时间暴露破坏性改动。待 ABI 冻结且出现第二个算法包后，再评估切换到「允许 pin 旧版 SDK」模式以验证向后兼容协商。
- **D11（算法包本地开发调试、基准压测与工程脚手架）**：算法包工程必须具备**零重编译的单机开发调试、性能分析与一键工程化闭环**。每个算法包工程内置：
  1. 标准 `Makefile`，提供 `configure` / `build` / `run` / `benchmark` / `asan` / `package` / `clean` 一致命令（对齐 `app/Makefile` 与 `engine/Makefile`）；
  2. 轻量单机调试器（`standalone_runner`，编译产出 `run_local`）与 `.env.example`，支持读取本地 `.env` 配置文件（模型路径、测试图、置信度/IOU 阈值、目标类别、输出图路径 `OUTPUT_IMAGE=result.jpg` 等）；
  3. **标准前后处理、性能分析与平台工具库（SDK Toolkit）**：SDK 在 `sdk/include/aivision/` 提供标准命名空间划分的高复用算法工具集（随 SDK 一起 vendored 到算法包）：
     - **跨平台 CV 常用算法工具（`aivision/cv/`）**：`resize.hpp`（常规直接拉伸缩放与坐标映射）、`letterbox.hpp`（等比缩放、黑边填充与反向映射）、`nms.hpp`（多类别高效 IoU 与 NMS 过滤）；
     - **通用开发、测试与性能分析工具（`aivision/utils/`）**：`env.hpp`（极简轻量 `.env` 解析）、`json.hpp`（零依赖紧凑结果 JSON 序列化）、`profiler.hpp`（RAII 作用域分段耗时打点工具，用于统计 Preprocess/Inference/Postprocess 阶段耗时与 P99 指标）；
     - **平台原生加速图像工具（`aivision/platform/<平台>/`）**：例如 macOS 提供 `preprocess.hpp`（基于 `vImage` 实现零拷贝/高并发 NV12/BGRA 的 Resize / Letterbox 缩放到模型输入格式）、`frame.hpp`（测试图加载为原生 `CVPixelBuffer` NV12 平台描述符）、`visualizer.hpp`（基于 CoreGraphics 绘制 BBox/Label/Score 并落盘 `result.jpg`）。算法开发者无需重复手写图像预处理与后处理，开发新算法只需聚焦在模型推理本身；
  4. **严格模拟生产帧格式与全流程**：`make run` 加载测试图后，**严禁直接传简单 RGB 内存块**，必须通过平台辅助库将其包装为**对应运行平台的真实原生帧描述符格式**（在 macOS 平台上必须构造包含 `CVPixelBufferRef`、`opaque_kind=AV_OPAQUE_CVPIXELBUFFER`、`pixel_format=AV_PIX_NV12`、plane stride/offset 及 SPS 色彩四元组的完整 `av_frame_desc`），以 100% 真实还原引擎在运行期的送帧行为；
  5. `make run` 驱动算法执行真实推理，在控制台打印检测结果 JSON，**同时调用 `visualizer.hpp` 将检测到的目标框（BBox）、类别标签与置信度绘制在图像上并输出为 `result.jpg`**，供开发者直观肉眼研判算法检测正确性；
  6. `make benchmark` 驱动算法进行多循环（如 100 次）基准压测，输出前处理、推理、后处理分段耗时（Avg/P50/P99）与持续吞吐 FPS 报告，便于快速评估硬件加速效果；
  7. 算法代码支持开发期 `getenv()` 环境变量覆盖（如 `CONF_THRESH=0.8 make run`，`LOOPS=500 make benchmark`），使算法开发者在单机调参、换图测试和快速打包时无需手动敲繁琐的 CMake 命令，也无需启动引擎主进程。

## Requirements

### R1 工程骨架与构建

- R1.1 新建三个顶层目录：`sdk/`（共享契约）、`engine/`（引擎）、`algo-packages/`（插件工程）。CMake ≥ 3.24，C++20。
- R1.2 引擎依赖通过 Homebrew 提供（`grpc`、`protobuf`、`googletest`、`nlohmann-json`），ZLMediaKit 以 git submodule 引入并静态链接。
- R1.3 提供 `engine/Makefile` 封装 `configure` / `build` / `test` / `lint` / `asan` / `e2e`，与 `app/Makefile` 风格一致。
- R1.4 核心层与适配层为独立 CMake target；核心层 target 不得链接任何平台私有库。

### R2 平台适配接口与能力档案

- R2.1 定义平台适配接口：媒体解码、帧与内存、推理上下文、图像处理、图片编码、资源配额、设备指标。
- R2.2 定义版本化能力档案结构，声明 PRD §6.3 要求的全部字段，每项能力标注「可用 / 不可用 / 降级」及原因。
- R2.3 适配层按 `platform_id` 注册；进程启动时激活唯一一个 `platform_id`，运行期不可切换。
- R2.4 交付 `macos-arm64-coreml` 适配层与 `mock` 契约测试适配器；`mock` 不访问任何硬件、不依赖系统框架，可在 CI 中运行。

### R3 通用帧描述符与生命周期

- R3.1 实现 PRD §7.4.3 全部字段的通用帧描述符，平台私有句柄仅存放于不透明扩展。`platform_id` 在描述符中以数值 tag 承载（完整字符串经能力查询获取），避免热路径字符串开销。
- R3.2 实现只读引用计数帧句柄与缓冲池；引用归零前底层缓冲不得复用。引用计数经 `av_frame_ops` 函数表暴露，帧描述符本身纯值传递、不含函数指针。
- R3.3 算法侧只能拿到只读句柄，不得释放平台原生内存。
- R3.4 具备泄漏与提前复用的检测手段（调试期 poison + 断言 + 单测覆盖）。
- R3.5 帧描述符必须携带色彩四元组（`color_primaries` / `color_transfer` / `color_matrix` / `color_range`）。来源优先取自 H.264/H.265 SPS VUI；VUI 缺失时按 BT.709 limited 兜底并记录一次运行日志。不为此增加摄像头配置项。
- R3.6 帧描述符与算法包 C ABI 的结构体布局必须与编译器无关：按对齐分块排列、显式保留字段、`_Static_assert` 锁定 `sizeof` 与关键 `offsetof`；扩展只允许在末尾追加，永不重排或删除字段。`stride` 使用有符号类型以支持负 stride。

### R4 媒体源与任务调度

- R4.1 接入 ZLMediaKit，一个摄像头只建立一条上游 RTSP 连接，推理与预览复用。
- R4.2 媒体回调只做编码帧引用入有界队列，不执行解码、磁盘、网络等阻塞工作。
- R4.3 一个摄像头任务可挂载多个算法实例，共享一次解码结果。
- R4.4 每个算法实例按自身 FPS 独立采样，持有容量受限帧队列，过载丢弃较旧帧保留最新帧；单实例积压不阻塞解码与其他实例。
- R4.6 每个算法实例拥有一个独占工作线程，串行执行该实例的 `process`——串行由线程独占**结构性保证**，不依赖调度逻辑的正确性。代码结构上「帧分发」与「执行」解耦（`process` 不得硬编码在解码回调中），使将来换成共享线程池时只需替换执行器，不需重写数据流。
- R4.5 支持断流退避重连与「重连中 / 离线」状态，恢复后自动继续分析；重连不销毁算法上下文与配置。
- R4.7 **多重无死锁与静默挂起防御机制（Watchdog & Self-Healing）**：
  1. **双层主动心跳看门狗**：网络拉流看门狗（连续 5s 无 NAL 包主动断开重连）+ 硬件解码看门狗（队列有包但连续 3s 未出帧判定驱动死锁，主动销毁并重建硬件解码会话）；
  2. **IDR 关键帧硬性准入闸门**：启动与重连时严格丢弃所有前导 P/B 帧，只有抓到完整 SPS/PPS + IDR 帧后才送入硬件解码器，从源头掐灭 VPU/解码驱动因残损帧导致的死锁；
  3. **异步非阻塞解码模式**：解码调用严格走异步或带超时的 poll 模式，严禁无限同步阻塞等待硬件返回；
  4. **解码器易耗品秒级自愈**：解码器死锁超时触发时，强制注销老 Session 原地新建，并向摄像头发送 PLI/FIR 关键帧请求指令，1 秒内完成自愈，算法层完全无感。

### R5 算法包 SDK 与运行时

- R5.1 `sdk/` 交付版本化算法包 C ABI 头文件（`size` + `api_version` 结构体、不透明句柄、错误码、明确所有权，放置于 `include/aivision/algo.h`、`types.h`、`result.h`），纯 C，跨 ABI 不出现 STL / C++ 对象 / 异常。
- R5.2 定义算法清单 Schema：`algorithm_id`、语义化版本、`platform_id`、算法类型、最低适配层版本、`min_os_version` 等运行时约束、支持的帧能力、资源档位、模型与依赖文件、参数 JSON Schema + UI 元数据、`testimage.jpg`。
- R5.3 `sdk/` 交付 cmake helper（`AivisionAlgoSDKConfig.cmake` + `AivisionAlgoPackage.cmake`）与**标准前后处理、性能分析及平台工具库**（`include/aivision/cv/` 下 `resize.hpp`、`letterbox.hpp`、`nms.hpp`；`include/aivision/utils/` 下 `env.hpp`、`json.hpp`、`profiler.hpp`；`include/aivision/platform/<平台>/` 下加速前处理与可视化落盘），使算法包工程在**只拥有自身目录**的前提下即可极速开发、性能剖析、构建并产出规范分发包（配合 D7 的 vendored 副本）。
- R5.4 `scripts/sync-sdk.sh` 单向同步 + CI SHA-256 一致性校验（D10）。
- R5.5 实现安装校验七步流程（PRD §7.5.4），失败返回结构化原因，记录包 SHA-256；通过后解压到 `var/packages/<algorithm_id>/<version>/`，运行时仅从该目录 dlopen。
- R5.6 实现算法实例生命周期与创建时能力协商（帧格式 / 内存类型 / 平台扩展），不兼容时拒绝并返回明确原因。实例创建时下发 `frame_ops`（引用计数）与 `image_ops`（平台加速图像原语）函数表。
- R5.10 适配层提供 `av_image_ops` 的平台实现（convert / pad / alloc / free）：macOS 用 vImage / Core Image，能力不可用时退回 CPU 并在能力档案中标注「降级」。前处理逻辑本身归算法所有，引擎只提供机制不定义策略——不实现「声明式前处理」。
- R5.7 实现参数原子热更新：平台做 Schema 基础校验，实例做最终校验，全部接受才生效并持久化，否则保持旧配置。
- R5.8 实现算法包升级 / 回滚：只停止引用该包的实例，排空在途帧后切换，失败自动回滚并恢复实例；有任务引用时禁止卸载。
- R5.9 本任务只实现 `object_detection` 类型的结果通路；`face_recognition` 在清单枚举中保留，运行时返回未实现。

### R6 结果与图片

- R6.1 **职责绝对切分与统一结果 Schema**：
  - **算法包纯粹职责**：算法包 C ABI 输出仅包含纯检测目标实体数组（`alarm_type_id`、`label`、`class_id`、`confidence`、`bbox` 归一化坐标及可选扩展属性），无目标时返回空；不承担系统级状态编排。
  - **Engine 统一编排职责**：全局唯一的 `event_id` 生成、视频帧收帧纳秒绝对时钟绑定（`wall_time_ns`）、单次推理与端到端性能耗时打点（P99 统计）、抓拍裁剪与原子落盘全部由 Engine 宿主统一承担。重复 `event_id` 幂等忽略。
- R6.2 目标框与规则区域使用 `[0,1]` 归一化坐标，原点为有效画面左上角；多边形 ≥3 顶点、不越界不自交。
- R6.3 图片模块统一管理裁剪、缩放、颜色转换、JPEG 编码；临时文件 + 原子重命名写入；对外只暴露图片 ID 与受限相对路径。
- R6.4 提供按图片 ID 的**幂等**批量删除接口，逐项返回删除结果。
- R6.5 孤儿图片（写图成功但对端未落库）可被扫描识别，提供清理入口。

### R7 计算资源与设备指标

- R7.1 实现归一化 1000 单位的资源账本：汇总全部启用实例的资源单位，超出可分配上限时拒绝启用或拒绝 FPS 热更新，并返回受限资源原因。
- R7.2 实现最低可用内存安全阈值检查，低于阈值拒绝启动并回滚。
- R7.3 macOS 能力档案声明可分配上限、安全保留量与资源档位来源（T1 阶段标注为「开发期估算」）。
- R7.4 采集六项通用指标（运行时间、CPU、内存、磁盘水位、加速器负载、温度）；不支持的指标标记为「当前平台不支持」，不得伪造为 0。

### R8 跨进程契约

- R8.1 定义 `.proto`：C++ → 对端的告警结果上报、图片元数据、任务与实例状态上报、设备指标上报；对端 → C++ 的任务下发、实例启停、参数热更新、算法包安装/升级/回滚、图片删除、期望状态全量对账。
- R8.2 proto 中**预留**人员 / 人脸特征 / 索引相关 service 与 message 定义（本任务不实现 handler）。
- R8.3 C++ 侧实现 gRPC client 与 server（over Unix Domain Socket），断线自动重连并在重连后执行全量对账。
- R8.4 视频帧不得出现在任何 proto message 中。

### R9 示例算法包

- R9.1 交付 `algo-packages/macos/arm64/yolov8n/`，类型 `object_detection`，`platform_id` = `macos-arm64-coreml`，为模块化自包含独立工程（D7/D8），各子模块（前处理、推理、后处理、C ABI 核心、调试 Runner）均具备独立 `CMakeLists.txt`。
- R9.2 支持热更新目标类别列表与置信度阈值；检测到配置类别时输出标准一次性告警事件。
- R9.3 提供单机本地调试工具（`standalone_runner`）、`.env.example` 以及标准 `Makefile`（支持 `build` / `run` / `benchmark` / `asan` / `package` / `clean`）。调试运行必须**完整模拟生产帧管线**（加载图片后包装为平台原生帧格式如 macOS 的 `CVPixelBuffer` NV12 描述符，禁止传伪造的纯 RGB 内存块），支持通过 `.env` 或环境变量即时覆盖运行参数，零重编译验证单图推理，**自动输出画框打标的 `result.jpg`**，支持一键 `make benchmark` 打印分段性能报表与 FPS 吞吐，一键打出合规 zip 分发包。
- R9.4 记录完整转换证据：源模型身份、coremltools 版本、转换配置、量化校准来源、`.mlpackage` SHA-256、构建报告、运行时张量属性。证据留在源码工程内，不进入分发包。
- R9.5 检测精度不属于本任务验收范围。

### R10 文档与规范

- R10.1 新增 `.trellis/spec/engine/` C++ 编码规范（目录结构、ABI 约定、错误处理、日志、测试）。
- R10.2 交付平台适配开发文档（`engine/docs/platform-porting.md`）与算法包开发文档（`sdk/docs/`，含「如何单独搬运一个算法包到目标机器编译」）。

## Acceptance Criteria

- [ ] AC1 `engine/` 在 macOS 上一条命令完成构建，全部单测通过。
- [ ] AC2 核心层 target 的链接依赖中不含平台私有库；核心层与 `sdk/` 的公共头文件中不出现 `CVPixelBuffer`、`MLModel`、`rknn_*` 等平台私有类型（提供可执行的检查脚本或测试）。
- [ ] AC3 `mock` 契约测试适配器可在无摄像头、无模型、无 GPU 的环境完成：适配层加载、能力档案查询、帧生命周期（含引用计数归零校验）、资源配额校验、算法结果回调全流程。
- [ ] AC4 输入一路真实 RTSP（或本地回放的 RTSP 模拟源）H.264 1080p，引擎持续解码并向示例包分发帧，日志可观测到实际采样 FPS 与丢帧数。
- [ ] AC5 同一摄像头任务挂载 2 个算法实例（不同 FPS），二者共享同一次上游连接与同一次解码；人为让其中一个实例阻塞时，另一个实例的 FPS 不下降。
- [ ] AC23 **串行契约**：并发压力测试下，同一实例的 `process` 从未被重入（算法侧断言 + 引擎侧计数器双向验证）；16 个算法实例同时运行时，单个实例阻塞不影响其他实例的处理速率。
- [ ] AC24 **图像原语**：算法通过 `av_image_ops` 完成裁剪 / 缩放 / 色彩转换 / padding，在 macOS 上走 vImage 路径；强制退回 CPU 实现时结果像素一致（同一输入两条路径输出可比对）。
- [ ] AC6 示例包检测到配置类别时生成告警：图片按图片 ID 落盘（可见临时文件已原子重命名），gRPC 上报被 Go stub server 收到，字段含 `event_id`、算法 ID/版本、归一化目标框、置信度、时间与时间同步状态。
- [ ] AC7 重复 `event_id` 的上报在 C++ 侧被幂等忽略，不重复落盘图片。
- [ ] AC8 参数热更新：合法配置整份生效（类别列表 / 阈值 / FPS 立刻变化）；非法配置整份拒绝，实例继续使用旧配置且不中断。
- [ ] AC9 资源配额：把示例包 FPS 提高到超过 1000 单位可分配上限时被拒绝，返回结构化受限原因，且现有实例不受影响。
- [ ] AC10 算法包升级：安装新版本示例包后，只有引用该包的实例被暂停并恢复，同任务内另一算法实例不中断；构造一个自测失败的坏包，安装被拒绝且当前版本继续运行；构造一个加载期失败的包，自动回滚旧版本并恢复实例。
- [ ] AC11 有任务引用时卸载算法包被拒绝。
- [ ] AC12 断流与静默卡死自愈恢复：
  - 断开上游流后任务进入「重连中」，算法上下文与配置保留；恢复后自动继续分析，无需重新配置。
  - **静默挂起防御测试**：人为在模拟源中制造网络无数据或送入畸形残缺 P 帧时，双层看门狗在 3~5 秒内主动超时介入，强制注销并重建解码会话，日志可观测到看门狗触发与自愈重建流水，进程无任何死锁挂起。
- [ ] AC13 gRPC 断线：杀掉 Go stub server 再拉起，C++ 自动重连并发起期望状态全量对账请求。
- [ ] AC14 图片删除接口对同一图片 ID 调用两次均返回成功（幂等），逐项返回结果。
- [ ] AC15 设备指标接口返回六项指标，macOS 不支持的项明确标记为不支持而非 0。
- [ ] AC16 帧生命周期单测证明：算法持有句柄期间底层缓冲不被复用，句柄释放后缓冲回池；ASan/LSan 干净。
- [ ] AC17 示例算法包附带完整转换证据文件，`.mlpackage` SHA-256 与记录一致。
- [ ] AC18 `.trellis/spec/engine/` 规范、平台适配文档与算法包开发文档已提交。
- [ ] AC19 **可搬运性（D7 核心判据）**：把 `algo-packages/macos/arm64/yolov8n/` 单独复制到 `/tmp` 下的空目录（本仓库其余部分不可见），在那里独立完成构建并产出合规分发包；`otool -L` 确认产物不依赖任何 engine 库；引擎从 `var/packages/` 安装加载该包运行；`nm` 确认 engine 构建产物中不含该算法符号。
- [ ] AC20 **契约一致性**：CI 检查通过 —— 每个算法包 `vendor/aivision-sdk/` 与上游 `sdk/` 的 SHA-256 一致（D10）；每个算法包由目录路径推导出的 `platform_id` 与其清单声明值一致（D8）。
- [ ] AC21 **ABI 布局稳定**：`sdk/` 头中的 `_Static_assert` 锁定 `sizeof`/`offsetof`，在 AppleClang 与 GCC（交叉编译到 aarch64）两种编译器下均编译通过；单测验证「用旧 `size` 值构造的描述符能被新版解析代码安全读取」的前向兼容路径。
- [ ] AC22 **色彩正确性**：对一段已知为 BT.709 limited 的测试码流，解析出的色彩四元组与码流 VUI 一致；对一段 VUI 缺失的码流，兜底为 BT.709 limited 并产生一条日志；NV12→RGB 转换使用描述符声明的矩阵与范围（可通过已知色块图的像素值断言）。
- [ ] AC23 **本地调试与工程化规范**：算法包工程提供标准 `Makefile`；一键 `make build` 编译出动态库与 `run_local`；`make run` 严格模拟真实帧封装（加载 JPEG 构造为包含 `CVPixelBufferRef` 的 NV12 平台帧描述符送入 `process`，非裸 RGB 内存）；修改 `.env` 中的置信度阈值与目标类别后，直接执行 `make run` 即可生效且无需重新编译，**并在当前目录成功生成画有目标检测框、类别标签与置信度的 `result.jpg`**；执行 `make benchmark` 可正确执行多循环压测并打印分段耗时与持续 FPS 报告；支持通过命令行前缀环境变量（如 `CONF_THRESH=0.8 make run`）实现即时覆盖；一键 `make package` 输出合规分发 zip 包。

## Out of Scope

- 人脸识别子系统：底库图质量准入、特征提取、10,000 人内存索引、候选/激活版本原子切换、特征契约迁移（→ T3）。
- `app/` 下任何 Go 产品代码改动：gRPC server、migration、落库、查询 API（→ T2）。
- 前端页面与菜单接入（→ T4）。
- Webhook 推送、数据留存与高低水位清理、签名 URL（→ T4）。
- ONVIF 自动发现（Go 侧职责）。
- 浏览器视频预览的协议冻结与前端播放（ZLM 接入本身在范围内，浏览器端验证不在）。
- `rk3576-rknn`、`ascend310b-cann` 等其他平台的适配层与算法包实现（目录与命名规则在本任务确立，实现不在）。
- Intel Mac（x86_64）支持。
- YOLOv8n 的检测精度调优。
- 算法包数字签名。
- macOS 上的性能基线验收（PRD §10.1 的 FPS 数值属于 RK3576 Profile，不适用于 macOS）。

## Risks / Deferred

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| ZLMediaKit 在 macOS 的构建与 API 稳定性未验证 | 阻塞 R4 全部 | 实现前先做最小 spike：submodule 编译 + 拉一路 RTSP 打印帧信息；失败则评估退回自研最小 RTSP 客户端（`media` 层接口不变） |
| Core ML 在 C++/Objective-C++ 中的批量与异步接口形态未定 | 影响 R5 推理上下文抽象 | 先在示例包内跑通单张推理，再抽象上下文接口 |
| macOS 归一化 1000 资源单位缺乏实测依据 | 配额校验数值无意义 | 本任务只验证机制正确性；档位数值标注「开发期估算」，待实测表补充 |
| macOS 无标准 NPU 负载 / 温度采集 API | R7.4 部分指标不可用 | 按 PRD §7.15 明确标记「当前平台不支持」，不伪造 |
| 平台抽象被 macOS 实现反向污染 | RKNN 接入时返工 | AC2 自动化检查 + `mock` 适配器双重约束；设计评审对照 PRD §6.3 逐条核对 |
| vendored SDK 副本漂移 | 算法包与引擎 ABI 不一致 | D10 的单向同步脚本 + CI SHA-256 校验（AC20） |
| 色彩空间处理错误 | 推理精度无声下降，极难定位（只表现为 mAP 低几个点） | 帧描述符强制携带色彩四元组（R3.5）；用已知色块图做像素级断言（AC22） |
| 跨编译器 ABI 布局漂移 | 第三方算法包用不同编译器构建后读到错位字段 | 显式对齐分块 + `_Static_assert` 锁 sizeof/offsetof + 双编译器验证（R3.6 / AC21） |

## Artifacts

- `prd.md`（本文件）
- `design.md`：架构、目录布局、接口边界、数据流、构建与依赖策略
- `algo-package-spec.md`：算法包规范草案 —— 加载序列、两级生命周期、初始化参数、能力协商、调用与回调、线程模型、超时、错误码、内存所有权、版本演进。M2 落地为 `sdk/docs/abi.md` + `sdk/include/aivision_algo.h` 并在 M2 末冻结
- `implement.md`：有序实施清单与验证命令
- `api.md`：不适用 —— 本任务无前端参与，跨进程契约以 `.proto` 形式落在 `design.md` 与代码中
