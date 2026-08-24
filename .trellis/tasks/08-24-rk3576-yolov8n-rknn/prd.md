# RK3576 RKNN YOLOv8n 检测算法包

状态：`planning`
范围：**只开发算法包**（用户 2026-08-24 明确），不做 Engine 平台适配层。

## Goal

在 `algo-packages/rknn/rk3576/yolov8n/` 新增一个面向 Rockchip RK3576 NPU 的 YOLOv8n
目标检测算法包：按既有算法包 C ABI 契约交付一个可独立编译、可独立调试、可打包分发的
`libyolov8n.so`，使 Engine 在具备 RK3576 平台适配层后可以零改动地安装并运行。

用户价值：把当前只在 Apple Silicon（Core ML）上跑通的检测能力落到实际边缘设备形态
（RK3576 NPU），为量产硬件提供第一个可安装的算法工件。

## Background：确认事实

### B1 参考实现与规范权威

- 唯一现存算法包 `algo-packages/macos/arm64/yolov8n/`（`platform_id=macos-arm64-coreml`）：
  `src/{preprocess,inference,postprocess,core,runner}` + `vendor/aivision-sdk/` +
  `conversion/` + `manifest.json` + `config.schema.json` + `testimage.jpg`。
- ABI 入口范式：`src/core/algo_entry.cpp:533-554`（静态 `av_algo_abi` 虚表 +
  `AV_EXPORT av_algo_get_abi`）；异常转换范式 `:110-136`（`invoke_abi`）；
  self-test 恰好一次回调 `:432-449`；negotiate 回填 `:340-376`。
- 规范：`.trellis/spec/engine/algo-package-spec.md`、`manifest-schema.md`、
  `directory-structure.md`、`quality-guidelines.md` §4。

### B2 路径与 platform_id 已被脚本硬约束

- `algo-packages/scripts/check-consistency.sh:18-27` 的 `derive_platform_id()` 已内置
  `rknn` 家族规则：`<family>=rknn` 时 `platform_id = "<model>-rknn"`。
  → 包路径必须 `algo-packages/rknn/rk3576/yolov8n/`，`manifest.platform_id` 必须
  `rk3576-rknn`，二者不一致脚本 `exit 1`。
- 同脚本 `:35-42` 强制 `vendor/aivision-sdk/` 与顶层 `sdk/` 全量 SHA-256 一致
  （`include/` + `toolkit/` + `cmake/` + `VERSION`）。
- `sync-sdk.sh:10` 用 `find algo-packages -name vendor -type d` 定位目标
  → **新包必须先手工创建空 `vendor/` 目录，否则同步脚本直接跳过它**。

### B3 SDK 已具备所需元素，本任务不改 ABI

- `sdk/include/aivision/types.h:64-68` 已有 `AV_OPAQUE_DMABUF = 0x2001`；
  `AV_MEM_HOST` / `AV_MEM_PLATFORM_SURFACE` / `AV_PIX_NV12` / `AV_PIX_RGB24` 齐全。
- `av_frame_desc` 152 字节固定布局（`types.h:96-134`），含 `modifier`、`offset[4]`、
  `stride[4]`、色彩四元组，足以描述 DMA-BUF NV12 帧。
- `sdk/cmake/*.cmake` 全部平台中立：`AivisionAlgoPackage.cmake:25` 用
  `${CMAKE_SHARED_LIBRARY_SUFFIX}` 推导入口库名（Linux 下即 `lib/libyolov8n.so`），
  `:32` 无条件 copy 包内 `model/` 目录 → **打包助手无需改动**。
- SDK toolkit 已提供平台中立可复用件：`cv/letterbox.hpp`（含
  `compute_letterbox` 与 `unletterbox_bbox`）、`cv/nms.hpp`（`DetectionBox` +
  class-wise `nms_filter`）、`cv/tracker.hpp`、`utils/{json,event_id,profiler,env}.hpp`。

### B4 Engine 侧现状决定了验证边界

- `engine/src/platform/` 只有 `macos/` 与 `mock/`，**不存在任何 Linux/Rockchip 平台适配层**
  （无 MPP 解码、无 DMA-BUF 帧池、无 RGA `av_image_ops`、无 RK 资源/指标）。
- → 本包无法经真实 Engine 端到端验证。可验证面 = 包内 CTest + 独立 runner
  （`make run` / `make benchmark`）+ 包结构校验。`package_validator` 需要 RK 平台适配层
  才能构造真实平台帧（`quality-guidelines.md` §5.1 第 6 步），本任务不具备该前提。

### B5 主机与目标硬件

- 主机 x86_64 Fedora 44；未装 rknn-toolkit2；`/opt` 无 aarch64 交叉工具链；
  仓库内无任何 `.rknn` / `librknnrt` / `rknn_api.h` / `librga` 工件。
- 目标：RK3576 板卡可用，板端具备 `librknnrt.so` + `rknn_api.h`，PC 侧可装 rknn-toolkit2。

### B6 RK3576 平台事实（rknn-pro `soc-matrix.md`）

- RKNN-Toolkit2 明确支持 `RK3576 Series`；NPU 头条算力 6 TOPS（INT8），
  常见图精度 INT8/FP16。CPU 为 4×Cortex-A72 + 4×Cortex-A53。
- **RK3576 的公开一手资料比 RK3568/RK3588 薄**：NPU core 数量、零拷贝解码可行性、
  RGA 支持组合均被标注为「board-SDK dependent，需逐板验证」，不得从 RK3588 经验外推。
- Toolkit2 只跑 PC 端（x86 Linux + Python），不在板端。

## Key Decisions

- **D1 硬件与工具链（用户确认）**：有 RK3576 板卡 + 完整 RKNN SDK。验收取真值：
  `make run` 打印真实检测框、`make benchmark` 出实测分段耗时、`conversion/evidence.md`
  记录完整证据链。rknn-pro 的「模型转换证据门禁」与「设备基线门禁」必须真正采集实测证据，
  **不得以「按规范书写」代替实测**。
- **D2 精度验收口径（用户确认）**：采用「与 PC 端 FP32 对齐」，**不沿用** macOS 包
  `conversion/evidence.md:46` 的「精度不在验收范围」口径。理由：macOS 走 Core ML FP32
  无量化损失，免责口径不可移植到 INT8 包；且 self-test 契约允许 `object_count=0`
  合法通过（`manifest-schema.md` §4.2），仅靠 self-test 无法发现「量化崩掉但链路可跑」。
- **D3 导出路径 = airockchip 三分支原始输出（由 D2 推导）**：用
  `airockchip/ultralytics_yolov8` 或 `rknn_model_zoo` 导出脚本产出**无 decode 头**的
  三个特征图分支（stride 8/16/32），DFL softmax 与 box 解码放 CPU 后处理。
  **不采用** stock 单输出 `1x84x8400`——虽可直接复用
  `algo-packages/macos/arm64/yolov8n/src/postprocess/postprocessor.cpp:1-91`，
  但该张量把 box 坐标（0~640 量级）与 class score（0~1 量级）挤在同一 output tensor 的
  scale/zero_point 下，INT8 误差会直接击穿 D2 门槛；且 transpose/concat 有回退 CPU 风险。
  代价：后处理需新写 DFL 解码，macOS 后处理不可复用。
- **D4 预处理走 `av_image_ops` + 包内 CPU fallback，算法包不直接链 librga**：
  `platform-guidelines.md` §4 定「Engine 提供机制；letterbox 比例、模型输入形状、
  通道排列和归一化策略归算法包」。RGA 属未来 RK 平台适配层对 `image_ops->convert` 的实现。
  生产动态库只做：`image_ops` 非空时经其做 CSC + 缩放；为空（独立 runner / 无平台层）时
  走包内 CPU fallback。两路径需像素一致性测试（`quality-guidelines.md` §8 硬性要求）。
  独立 runner 不在生产动态库内，可自行造帧。
- **D5 校准集 = COCO val2017 子集（用户确认）**：取 100–500 张，与 yolov8n 训练分布
  及 D2 的 golden 基准图（`testimage.jpg` 为 COCO 分布）同分布，校准与验收自洽；
  公开数据集可复现，图片清单 + SHA-256 写入证据链。已知代价：若实际部署为固定机位监控画面，
  INT8 真实表现会差于同分布情况——该风险记入 Risks，不在本任务解决。
- **D6 包身份沿用 macOS 包**：`algorithm_id=yolov8n`、`version=1.0.0`、
  `algorithm_type=object_detection`、`alarm_type_id=object_detect`、COCO 80 类。
  依据 `algo-package-spec.md` §3.1「每个已安装的 `algorithm_id + version + platform_id`
  最多打开一个 Library」——`platform_id` 不同，故与 macOS 包不冲突。
- **D7 帧协商同时接受 DMA-BUF 与 HOST**：`instance_negotiate` 回填
  `pixel_formats=[NV12]`、`memory_types=[PLATFORM_SURFACE, HOST]`、
  `required_opaque_kind=AV_OPAQUE_DMABUF`。理由：DMA-BUF 是未来 MPP 解码器的自然输出
  且是零拷贝前提；HOST 让独立 runner 与无平台层场景可自证。
  **不声称零拷贝**——rknn-pro 规则要求零拷贝必须逐跳证明所有权/布局/同步，
  本任务没有 MPP 上游可证，故只声明「接受 DMA-BUF 输入」，不宣称端到端零拷贝。
- **D8 多实例用 `rknn_dup_context`**：Library 级 `rknn_init` 加载一次模型；
  每个 Instance 用 `rknn_dup_context()` 派生独立上下文，满足 `algo-package-spec.md`
  §3.1（Library 持有模型工件）与 §5（不同 Instance 可并发、不得共享无同步可变状态）。
- **D9 runner 图像 I/O 用单头 stb**：Linux 无 ImageIO/CoreGraphics，`make run` 读
  `testimage.jpg` 与写带框 `result.jpg` 用 `stb_image.h` / `stb_image_write.h`，
  vendored 到 `src/runner/third_party/`。**不得放进 `vendor/aivision-sdk/`**——
  那里被 `check-consistency.sh` 的全量 SHA 校验锁死。stb 只链进 runner target，
  不进生产动态库（`quality-guidelines.md` §4「可视化属于 runner/toolkit，
  不进入算法动态库生产路径」）。
- **D10 板端原生编译**：aarch64 板上直接 `make configure/build`，天然满足
  `quality-guidelines.md` §4 的「复制到 `/tmp` 零依赖构建」硬判据；不引入交叉工具链。

## Requirements

### R1 包骨架与身份

- R1.1 包落在 `algo-packages/rknn/rk3576/yolov8n/`，`manifest.platform_id = rk3576-rknn`；
  `bash algo-packages/scripts/check-consistency.sh` 通过。
- R1.2 先创建空 `vendor/` 目录（B2），再由 `bash algo-packages/scripts/sync-sdk.sh`
  生成 `vendor/aivision-sdk/`；不得手工拷贝或改写 vendored 内容。
- R1.3 `manifest.json` 按 D6 填写身份字段；采用约定优于配置结构（不冗余声明 `files[]`、`entry_library`、`config_schema_file`、`test_image_file`）；zip 整体 SHA-256 作为完整性唯一锚点。
- R1.4 `runtime_constraints` 只声明平台相关增量约束，取值必须来自板端实测
  （`manifest-schema.md` §2.5 要求未知约束字段被拒绝，故字段名需与未来 RK 平台适配器
  的校验实现对齐；本任务先以最小集合落地并在 design 记录待对齐点）。
- R1.5 `resource_profile.fps_tiers` 与 `min_free_memory_mb` 由板端 benchmark 实测标定，
  不照抄 macOS 包的 5/60、15/150、30/300（`manifest-schema.md` §2.3：
  不同 `platform_id` 的 units 不可比较）。
- R1.6 `config.schema.json` 沿用 `confidence_threshold` + `iou_threshold`
  （Draft-07 受限子集，`additionalProperties=false`）。
- R1.7 动态库只导出 `av_algo_get_abi`：Linux 用 `-Wl,--version-script`（macOS 那份
  `-Wl,-exported_symbols_list` 不可用），并由测试用 `nm -D --defined-only` 断言。

### R2 模型转换与证据链

- R2.1 `yolov8n.pt` → ONNX：静态 shape `[1,3,640,640]`，无 decode 头，三分支输出
  （stride 8/16/32）。导出前后用 `inspect-onnx-model.py` 记录 IR/opset、
  输入输出名/形状/dtype、动态维、图内是否含归一化前缀。
- R2.2 RKNN-Toolkit2 转换：`target_platform="rk3576"`，
  `mean_values=[[0,0,0]]`、`std_values=[[255,255,255]]`、RGB 通道序、
  `do_quantization=True`、`quantized_dtype="asymmetric_quantized-8"`。
- R2.3 校准集按 D5：COCO val2017 子集 100–500 张，`dataset.txt` 与图片清单
  （文件名 + SHA-256）入库；校准预处理与运行期预处理必须同一套 letterbox 参数。
- R2.4 跑 `rknn.accuracy_analysis()`，逐层 SNR 写入 `conversion/evidence.md`；
  若 DFL / 检测头卷积掉点导致 R7 不达标，走 Toolkit2 官方两步混合量化
  （`hybrid_quantization_step1(proposal=True)` → 编辑 `*.quantization.cfg` →
  `step2`），把敏感层提到 `float16`。
- R2.5 `conversion/` 入库可复现脚本（导出 + 转换 + 精度分析）与 `evidence.md`；
  evidence 至少含：Toolkit2 版本、target、ONNX 路径与 SHA-256、`.rknn` 路径与 SHA-256、
  完整 `rknn.config()` / `build()` 请求、verbose 转换日志要点、build report 反映的
  **实际逐层精度**（含混合精度层清单）、板端 `rknn_query` 到的输入输出 tensor 属性
  （type / format / dims / stride / qnt_type / zero_point / scale）。
- R2.6 归一化三处定位（ONNX 图内 / Toolkit2 `mean`+`std` / 应用侧）在 evidence.md 中
  逐项标注 `confirmed-present | confirmed-absent`，**不允许留 `unknown`**；
  端到端只能有一处生效。

### R3 C ABI 实现

- R3.1 `av_algo_abi` 12 个函数指针全部实现；所有 ABI 入口 `noexcept` 并把异常转为
  `AV_ERR_*`（移植 `algo_entry.cpp:110-136` 的 `invoke_abi`）。
- R3.2 Library 级：以 `package_root` 为根解析私有配置 `<package_root>/.env` 的 `MODEL_PATH`（默认 fallback 到 `model/yolov8n.rknn`），
  `rknn_init` 加载一次；失败返回 `AV_ERR_MODEL_LOAD_FAILED`。严格禁止依赖 CWD、
  全局环境变量（`std::getenv`）、仓库路径或自写日志文件。
- R3.3 Instance 级：按 D8 用 `rknn_dup_context` 派生独立上下文；持有独立配置、
  规则状态、tracker 状态与跨帧轨迹。
- R3.4 `instance_negotiate` 按 D7 回填；offered 无 NV12 或无可用 memory_type 时返回
  `AV_ERR_INCOMPATIBLE_FRAME`；accepted 不得超出 offered 子集。
- R3.5 `instance_update_config`：整份替换，先解析构造候选再原子交换，失败返回
  `AV_ERR_CONFIG_INVALID` 且旧配置不变。
- R3.6 `instance_set_rules`：ROI / Mask / 分界线过滤，几何非法返回 `AV_ERR_INVALID_ARG`；
  目标锚点语义归算法（人=脚底中心、车=中心点）；热更新时清空跨帧轨迹状态。
- R3.7 `AV_INSTANCE_INSTALL_SELF_TEST` 实例走真实预处理→推理→后处理→序列化路径，
  在调用栈内恰好回调一次 `AV_RESULT_SELF_TEST`，零检测仍返回合法 JSON，
  不产图片请求、不写文件。
- R3.8 正常实例：零检测零回调；有检测时生成 `instance_run_id` 内唯一且不含 `/` 的
  `event_id`，`alarm_type_id` 等于 manifest 声明值，bbox 为 `[0,1]` 归一化 `[x,y,w,h]`。
- R3.9 `instance_flush` / `instance_destroy` 有界返回，释放全部 RKNN 上下文与图像缓冲；
  `image_ops->alloc` 的缓冲必须用配对 `free` 释放。
- R3.10 `last_error` 截断安全且 NUL 结尾。

### R4 预处理

- R4.1 `image_ops` 非空时经 `convert` 完成 NV12 → 模型输入格式的 CSC 与缩放，
  经 `pad` 填充 letterbox 边（值 114）；缓冲经 `alloc`/`free` 成对管理。
- R4.2 `image_ops` 为空时走包内 CPU fallback：NV12 → RGB888，使用帧描述符声明的
  色彩矩阵与 range（缺失时 BT.709 limited 兜底并只记一次降级日志），
  letterbox 到 640×640、pad 114。
- R4.3 两条路径对同一输入的像素一致性测试（容差内）——`quality-guidelines.md` §8 硬性要求。
- R4.4 输入 buffer 的宽高与 stride 必须满足板端 `rknn_query` 到的
  输入 tensor 权威要求（含 `w_stride` / `h_stride` 与物理布局字节数），
  不按「宽×高×3」自行推算。
- R4.5 帧描述符校验：拒绝非 NV12、非 NV12 平面数、尺寸越界、stride 小于宽度、
  不支持的色彩枚举（移植 `algo_entry.cpp:156-178` 的 `validate_frame` 并按 D7 放宽
  memory_type / opaque_kind）。

### R5 后处理

- R5.1 三分支 DFL 解码：对每个 stride 层，先在**量化域**用换算后的阈值筛候选
  （阈值换算需 checked rounding + 饱和处理，禁止拿原始整数与浮点阈值直接比较），
  再对通过的候选做反量化与 DFL softmax 解码。
- R5.2 反量化一律用板端 `rknn_query` 到的仿射量化契约 `(value - zero_point) * scale`；
  不从 `.rknn` 文件名、host 缓冲类型或 `want_float` 推断精度与归一化。
- R5.3 class-wise NMS 复用 `aivision::cv::nms_filter`；逆 letterbox 复用
  `aivision::cv::LetterboxInfo::unletterbox_bbox`（`cv/letterbox.hpp:22-38`）。
- R5.4 输出 `aivision::cv::DetectionBox`，COCO 80 类标签；非有限值、零面积框、
  越界框一律丢弃。
- R5.5 tracker 复用 `aivision::cv::SimpleTracker`（`cv/tracker.hpp`）。

### R6 独立工程门禁（`quality-guidelines.md` §4）

- R6.1 提供 `make configure / build / test / run / benchmark / asan / package / clean`，
  在干净 build 目录均可运行，`configure` 可重复。
- R6.2 私有配置与参数覆盖：算法生产库内部严格基于 `package_root` 解析 `<package_root>/.env`（严禁调用 `std::getenv`），
  支持通过修改 `.env` 调整默认阈值或模型路径而无需重编动态库；独立 runner 作为宿主模拟器时支持将调试环境变量/CLI 参数写入或覆盖传递；提供 `.env.example`。
- R6.3 runner 必须把测试图包装为**真实 `av_frame_desc`**，不得向 `process` 传伪造裸 RGB；
  按 D7 提供 HOST NV12 与 DMA-BUF NV12 两条造帧路径各跑通一次。
- R6.4 `make run` 打印检测结果并生成带 bbox / label / confidence 的 `result.jpg`。
- R6.5 `make benchmark` 预热后输出 preprocess / inference / postprocess / end-to-end 的
  Avg / P50 / P99 与持续 FPS，并记录 loops、输入尺寸、模型 digest 与运行平台。
- R6.6 `make asan` 同时开 ASan + UBSan，先跑包内 CTest 再跑真实 runner。
- R6.7 `make package` 生成 zip 与同名 `<archive>.sha256`。
- R6.8 从仓库外 `/tmp` 复制后可完整 `make build package`；`ldd` 输出不得出现 engine 库。

### R7 精度验收（D2 落地）

- R7.1 PC 端用 ONNX/FP32 对固定测试图集产出 golden 检测结果（脚本入库、结果入库）。
- R7.2 板端 RKNN INT8 对同一图集的结果与 golden 对齐：目标数量一致、
  逐框 IoU ≥ 阈值、置信度偏差在容差内。阈值与容差在 design.md 中给出具体数值并说明依据。
- R7.3 不达标时按 R2.4 走混合量化后重测；仍不达标则记录为阻塞项，不得静默降低门槛。

### R8 板端基线证据

- R8.1 在目标板运行 rknn-pro 的 `rknn-diag.sh` 采集基线，渲染
  `.agents/context/rknn-context/{machine_id}.md`，至少覆盖：内核版本与 BSP 来源、
  `librknnrt` 版本、rknpu 驱动版本、`librga` 版本与 RGA 驱动版本、NPU core 拓扑。
- R8.2 Toolkit2 版本、模型 target、Runtime 版本、驱动与头文件版本必须相互匹配并记录；
  版本结论不得从 RK3588/RK3568 经验外推（B6）。

## Acceptance Criteria

标注口径：`[主机]` x86_64 开发机可执行；`[PC]` 装有 rknn-toolkit2 的 x86 Linux；
`[板端]` RK3576 目标板。

- [ ] AC1 `[主机]` `bash algo-packages/scripts/check-consistency.sh` 通过：
      路径推导出的 `rk3576-rknn` 与 manifest 声明一致，vendored SDK 全量 SHA 与
      `sdk/` 一致。
- [ ] AC2 `[板端]` 干净目录下 `make configure && make build && make test` 全绿；
      `nm -D --defined-only build/libyolov8n.so` 只有 `av_algo_get_abi` 一个导出。
- [ ] AC3 `[板端]` `make run` 用真实 NV12 `av_frame_desc` 跑通并打印检测框，
      生成非空且带框的 `result.jpg`；HOST 与 DMA-BUF 两条造帧路径均通过。
- [ ] AC4 `[板端]` `make benchmark` 输出四段（preprocess / inference / postprocess /
      end-to-end）的 Avg / P50 / P99 与持续 FPS，并记录 loops、输入尺寸、模型 digest、
      运行平台；实测值用于标定 R1.5 的 `fps_tiers` 与 `min_free_memory_mb`。
- [ ] AC5 `[板端]` `make asan` 先跑 CTest 再跑真实 runner，ASan + UBSan 零报告，
      且未通过关闭泄漏检测或大范围 suppressions 取得。
- [ ] AC6 `[板端]` `make package` 产出 zip 与 `<archive>.sha256`；把包复制到
      `/tmp` 后可完整 `make build package`，`ldd` 无 engine 库、无父仓库路径依赖。
- [ ] AC7 `[板端]` self-test 契约：`AV_INSTANCE_INSTALL_SELF_TEST` 实例对测试图
      恰好回调一次 `AV_RESULT_SELF_TEST`，零检测时仍返回合法 JSON（`status=ok`、
      `stages` 非空、`object_count>=0`），无图片请求、无文件写入。
- [ ] AC8 `[板端]` ABI 负例：请求错误 api_version 返回 NULL；`size` 过小、
      缺配置的 NORMAL 实例、非法 config JSON、几何非法规则分别返回
      `AV_ERR_UNSUPPORTED_API` / `AV_ERR_INVALID_ARG` / `AV_ERR_CONFIG_INVALID` /
      `AV_ERR_INVALID_ARG`，且非法配置后旧配置仍生效。
- [ ] AC9 `[板端]` negotiate：offered 含 NV12 时回填 NV12 + {PLATFORM_SURFACE, HOST}
      子集且尺寸区间有交集；offered 无 NV12 时返回 `AV_ERR_INCOMPATIBLE_FRAME`
      且不开始分帧。
- [ ] AC10 `[板端]` 规则过滤：ROI / Mask / 分界线单独与组合场景、空规则全通过、
      锚点判定、跨帧方向判定、热更新后旧轨迹清空，全部符合预期。
- [ ] AC11 `[板端]` 预处理双路径像素一致性：注入 `av_image_ops` 与包内 CPU fallback
      对同一输入的输出在容差内一致。
- [ ] AC12 `[PC]`+`[板端]` **精度对齐（D2 核心门禁）**：板端 RKNN INT8 对固定测试图集的
      检测结果与 PC 端 ONNX/FP32 golden 对齐——目标数量一致、逐框 IoU ≥ 阈值、
      置信度偏差在容差内。不达标时经混合量化重测仍不达标，则该 AC 不得勾选。
- [ ] AC13 `[PC]` `conversion/` 下脚本可复现地重跑出同一 `.rknn`（SHA-256 一致或
      记录不可复现原因）；`evidence.md` 含 R2.5 全部字段，且 R2.6 三处归一化状态
      无 `unknown`。
- [ ] AC14 `[板端]` `.agents/context/rknn-context/{machine_id}.md` 已生成并覆盖
      R8.1 全部条目；文中所有版本结论标注来源，无从其他 SoC 外推的结论。

## Out of Scope

- Engine 侧 RK3576 平台适配层（MPP 解码、DMA-BUF 帧池、RGA `av_image_ops`、
  JPEG 编码、资源账本与六项指标）——「只开发算法」的直接推论。
- 经真实 Engine / `package_validator` 的端到端安装验证——缺 RK 平台适配层（B4）。
- 后端 Go 服务、前端 UI 的任何改动。
- SDK ABI 头改动（B3：现有枚举已够用）。
- COCO mAP 正式评测工程（数据集管理 + pycocotools 基准）——D2 选了更轻的
  「固定图 vs PC FP32 golden 对齐」。
- 端到端零拷贝声明与 RGA 直接集成（D4 / D7）。
- 交叉编译工具链（D10 走板端原生编译）。
- macOS 包的任何改动。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| INT8 量化击穿 D2 对齐门槛 | AC12 不过，包不可用 | R2.4 两步混合量化把 DFL/检测头提到 float16；仍不过则记阻塞，不降门槛（R7.3） |
| COCO 校准集与真实部署分布不符 | 现场精度差于验收表现 | D5 已知代价，记为 deferred；后续拿到场景数据再出 `1.1.0` 重标定 |
| RK3576 公开资料薄、NPU core 拓扑与 RGA 组合需逐板确认 | 从 RK3588 外推会得出错误结论 | R8 强制板端基线采集；B6 已标注不得外推 |
| Toolkit2 / librknnrt / rknpu 驱动 / BSP 版本错配 | 转换成功但板端加载失败或结果错乱 | R8.2 记录全链版本并交叉校验 |
| `rknn_dup_context` 的并发语义随 Runtime 版本变化 | 多实例并发下结果串扰 | D8 需在板端实测两实例并发用例；不通过则退化为每实例独立 `rknn_init` 并记入 design |
| 无 RK 平台层，`av_image_ops` 路径无真实实现可测 | R4.1 只能靠注入 fake ops 验证 | AC11 用注入的测试用 `image_ops` 对拍 CPU fallback；真实 RGA 实现留给平台层任务 |
| 输入 tensor stride / 物理布局按错误公式推算 | 越界写或静默错位 | R4.4 强制以 `rknn_query` 权威值为准 |
| stb 误入生产动态库 | 违反 `quality-guidelines.md` §4 | D9 限定 stb 只链 runner target；AC2 的导出符号断言与 CMake 依赖分离共同兜住 |

## Artifacts

- `prd.md`：本文件。
- `design.md`：目录与 target 拓扑、C ABI 实现映射（可移植 / 需重写清单）、
  RKNN Library/Instance 生命周期、预处理双路径、三分支 DFL 后处理契约、
  runner 造帧方案、精度对齐阈值与依据、待板端确认项清单。
- `implement.md`：分阶段执行清单、每阶段验证命令与回滚点。
- `research/`：模型转换证据链与板端基线在实施期落盘
  （`conversion/evidence.md` 与 `.agents/context/rknn-context/`）。
