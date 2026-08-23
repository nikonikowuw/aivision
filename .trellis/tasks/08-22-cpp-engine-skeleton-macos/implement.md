# Implement — C++ Engine 框架与 macOS 运行平台（T1）

任务较大，分为 5 个清晰阶段推进。每个阶段都有明确的交付物、独立验证点与准出标准，前一步验证通过才进入下一步。

---

## 阶段规划概览

```
Phase 1: 环境准备、ZLM Spike 风险穿刺与工程骨架 (M0)
   ▼
Phase 2: 纯 C ABI 契约、SDK 工具集与 Mock 契约闭环 (M1) ──► 【核心契约冻结点】
   ▼
Phase 3: 算法包运行时、7 步安装沙箱与单机开发套件 (M2) ──► 【算法生态闭环】
   ▼
Phase 4: macOS 硬件适配层与多实例实时流调度 (M3 + M4) ──► 【硬件与并发验证】
   ▼
Phase 5: 图片落盘、gRPC 跨进程对账与端到端总装 (M5 + M6) ──► 【全量交付验收】
```

---

## Phase 1: 环境准备、ZLM Spike 风险穿刺与工程骨架

> **核心目标**：扫清最大第三方依赖不确定性，建立顶层 CMake 构建骨架。

1. **工具链准备** — `brew install cmake ninja grpc protobuf googletest nlohmann-json`；记录各版本到 `engine/docs/toolchain.md`。
   - 验证：`cmake --version`、`protoc --version`、`grpc_cpp_plugin` 可执行。
2. **【硬风险闸门】ZLMediaKit Spike** — 加 submodule，最小 CMake 编译 ZLM 静态库，编写 20 行 demo 接入测试 RTSP 流并打印 H.264 NAL 长度与 PTS。
   - 验证：demo 持续输出帧信息 ≥60 秒无崩溃。**此步失败则暂停并回到设计评审。**
3. **三大顶层目录骨架** — `sdk/`、`engine/`、`algo-packages/`；配置 `engine/CMakeLists.txt` + `Makefile`，建立 `engine_core` / `platform_api` / `platform_mock` / `platform_macos` / `engine_app` 五个 target 与空实现，接入 gtest。
   - 验证：`make -C engine configure build test` 通过（0 个测试也算）。

---

## Phase 2: 纯 C ABI 契约、SDK 工具集与 Mock 契约闭环

> **核心目标**：锁定最底层的核心契约，交付完整的 SDK 工具库，用纯内存 Mock 跑通契约测试。

1. **`sdk/include/aivision/` 核心头文件落地** — 纯 C ABI（`algo.h` 虚表、`types.h` 144 字节通用帧描述符、`av_frame_ops`、`result.h` 极简结果 Schema）。
2. **跨平台 CV 工具与通用脚手架** — `cv/resize.hpp`、`cv/letterbox.hpp`、`cv/nms.hpp`；`utils/env.hpp`、`utils/json.hpp`、`utils/event_id.hpp`（事件 ID 生成与校验）、`utils/profiler.hpp`（RAII 分段打点）。
3. **`platform_api` 接口与能力档案** — 落 `design.md` §3.4 全部接口、`PlatformProfile`、`Availability`、注册表、`av_image_ops` 接口定义。
4. **SPS VUI 色彩信息解析** — 从 H.264/H.265 SPS VUI 提取色彩四元组，缺失时兜底 BT.709 limited 并记一条日志。
5. **`platform_mock` 纯内存适配器** — 全部接口的可注入假实现（假帧、假推理、纯 CPU 图像处理）。
6. **契约测试套件（Contract Tests）** — `engine/tests/contract/` 覆盖：适配层加载、能力查询、帧生命周期与引用计数（缓冲不提前复用）、资源配额校验、结果回调；ABI 布局断言在 AppleClang 与 aarch64 GCC 下双编译验证。
   - 验证：**AC3、AC16、AC21、AC22**，执行 `make -C engine asan` 内存安全干净。
   - **【冻结点】**：SDK ABI 正式评审并冻结。

---

## Phase 3: 算法包运行时、7 步安装沙箱与单机开发套件

> **核心目标**：打通算法包的独立单机开发闭环，实现引擎端的安全加载与 7 步安装沙箱。

1. **`sdk/` 分发脚手架与同步工具** — `AivisionAlgoSDKConfig.cmake` + `AivisionAlgoPackage.cmake`（`aivision_add_algo_package()`）+ `sdk/VERSION` + `algo-packages/scripts/sync-sdk.sh`（单向同步）+ `check-consistency.sh`。
2. **模块化算法包工程模板** — `algo-packages/` 结构确立，各子模块（`preprocess/`、`inference/`、`postprocess/`、`core/`、`runner/`）具备独立 `CMakeLists.txt`；配置标准 `Makefile`（`build`/`run`/`benchmark`/`asan`/`package`/`clean`）。
3. **单机调试套件（`run_local`）** — 支持读取本地 `.env` 配置文件与环境变量即时覆盖；`make run` 模拟平台帧封装并输出画框打标的 `result.jpg`；`make benchmark` 输出分段性能与 FPS 报告。
4. **引擎算法包运行时与 7 步安装沙箱** — 基于 `fork()` 临时子进程安全加载校验（解压到 `var/packages/`、路径安全、`testimage.jpg` 全流程自测、坏包回滚、参数原子热更新、升级卸载保护）。
5. **测试 fixture 算法包** — 一个 Mock 算法包 + 两个坏包 fixture（自测失败 / 加载失败），打成标准 zip 走真实安装流程。
   - 验证：**AC8、AC10、AC11、AC20、AC25** 在 Mock 平台上先行通过；验证算法包单目录 `/tmp` 独立可搬运构建（AC19）。

---

## Phase 4: macOS 硬件适配层与多实例实时流调度

> **核心目标**：接入 macOS 原生硬件加速，实现单摄像头多算法实例并发调度与故障隔离。

1. **`platform_macos` 硬件实现** —
   - `IDecoder`：VideoToolbox 硬解 H.264/H.265 → `CVPixelBuffer` NV12，接入帧池；
   - `IInferenceContext`：Core ML 模型加载与 ANE/GPU 推理调度；
   - `IImageProcessor` / `av_image_ops`：`vImage` 矢量加速前处理（Resize / Letterbox）与色彩转换；`ImageIO` JPEG 编码；
   - `IResourceProvider` / `ITelemetry`：1000 归一化资源账本与 6 项系统指标采集。
2. **ZLM 媒体源管理与多实例调度器** —
   - 单摄像头单上游连接，推理与预览复用；
   - 每算法实例独占工作线程消费其独立有界帧队列（丢旧留新 + 自动 `unref`）；
   - 「帧分发」与「执行」解耦，串行契约结构性保证。
3. **断流退避重连与防死锁自愈状态机** —
   - 指数退避重连（1s~30s）、常驻待命不销毁上下文；
   - 双层心跳看门狗（Ingest 5s 超时断开 + Decoder 3s 超时强制销毁重建）；
   - IDR 关键帧硬性准入闸门（丢弃残缺前导 P 帧，防芯片寄存器死锁）；
   - RTSP PLI/FIR 关键帧补发请求。
   - 验证：**AC2**（符号检查）、**AC4、AC5、AC9、AC12（含静默挂起自愈测试）、AC15、AC23、AC24**。

---

## Phase 5: 图片落盘、gRPC 跨进程对账与端到端总装

> **核心目标**：完成图片统一管理、gRPC 跨进程契约对账、YOLOv8n 真实算法包落地与全量验收。

1. **图片生命周期模块** — 目标 ROI 裁剪、JPEG 编码、tmp + rename 原子落盘、批量幂等删除、孤儿扫描。
2. **跨进程契约与 gRPC 实现** — 定义 `proto/aivision/v1/*.proto`（含 person 预留），实现 UDS gRPC Client/Server；断线自动重连。
3. **测试用 Go Stub Server** — 独立 `go.mod`（严守 D5），接收告警上报、模拟重启并驱动 `ApplyDesiredState` 声明式全量对账。
4. **YOLOv8n 真实算法包交付** — 转换 Core ML 模型并归档证据链（`conversion/`）；编写模块化算法源码，闭环测试 `make run`、`make benchmark` 与 `make package`。
5. **文档定稿与全量验收** — 编写 `.trellis/spec/engine/` 规范、平台适配文档；逐项复核 AC1 ~ AC25。
   - 验证：**AC1、AC6、AC7、AC13、AC14、AC17、AC18、AC19、AC25** 全量通过。

---

## 验证命令速查

```bash
# 引擎构建与测试
make -C engine configure      # cmake 配置
make -C engine build          # 构建全部 target
make -C engine test           # gtest 单测 + 契约测试
make -C engine asan           # ASan/LSan 构建并跑测试
make -C engine lint           # clang-format + clang-tidy + AC2 边界检查
make -C engine e2e            # 拉起 stub server + engine，跑端到端脚本

# SDK 与一致性检查
bash algo-packages/scripts/sync-sdk.sh            # 上游 sdk/ → 各包 vendor/
bash algo-packages/scripts/check-consistency.sh   # AC20：SHA-256 + platform_id 一致性

# 算法包单机工程化与调试命令（D11/AC25）
make -C algo-packages/macos/arm64/yolov8n build     # 编译 dylib 与 run_local
make -C algo-packages/macos/arm64/yolov8n run       # 读取 .env 单图推理自测，输出 result.jpg
make -C algo-packages/macos/arm64/yolov8n benchmark # 100 次循环分段性能压测 (Avg/P50/P99/FPS)
CONF_THRESH=0.8 make -C algo-packages/macos/arm64/yolov8n run # 环境变量即时覆盖
make -C algo-packages/macos/arm64/yolov8n asan      # ASan/LSan 内存安全检查
make -C algo-packages/macos/arm64/yolov8n package   # 打出标准分发 zip 包
```

## 风险文件 / 回滚点

- `engine/third_party/ZLMediaKit`（submodule）：Phase 1 步骤 2 是硬风险闸门。
- `sdk/include/*` 与 `engine/include/aivision/platform/*`：Phase 2 末冻结并评审。
- `engine/proto/`：Phase 5 冻结，T2 直接复用。
- 全任务不触碰 `app/`、`ui/`，回滚即删除 `sdk/`、`engine/`、`algo-packages/` 三个顶层目录。

## `task.py start` 前的检查

- [x] `implement.jsonl` / `check.jsonl` 已填入真实 spec/research 条目
- [x] `event_id` 生成与幂等职责已统一（算法生成实例内 ID + 引擎组合全局唯一与去重）
- [x] AC 编号冲突已修正（AC1~AC25 连续不重）
- [ ] `.trellis/spec/engine/` 规范由本任务 Phase 5 产出，实现期先遵循 `.trellis/spec/guides/`
