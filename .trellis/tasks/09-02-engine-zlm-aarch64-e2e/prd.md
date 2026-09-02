# PRD: Engine aarch64 ZLMediaKit 与 RK 端到端验收

> 父任务：`09-02-rockchip-platform-adapter`（platform 适配层）。本任务承接其**媒体后端**与**硬件验收**部分。
> 依赖：父任务的 `platform_rockchip` 完成并通过其 5.1 门禁后再启动本任务。

## 1. 背景

`platform_rockchip` 只解决硬件解码/图像/遥测抽象层。但 Engine 的媒体拉流后端当前被硬绑在 macOS：

- `engine/CMakeLists.txt:67`：`set(ARGUS_USE_ZLM ON)` 的条件是 `APPLE AND PLATFORM_TARGET STREQUAL "macos"`，
  ZLMediaKit 子项目也只在该分支下 `add_subdirectory`；
- `engine/src/app/main.cpp:206`：`#if defined(ARGUS_PLATFORM_MACOS)` 用 `create_zlm_backend()`，
  `#else` 一律 `create_mock_backend()`。

因此即便 platform 适配层全部完成，RK 板卡上跑的仍是 mock 拉流，**无法做任何真实 RTSP 端到端验证**。
aarch64 下启用 ZLMediaKit 涉及 third_party 交叉编译与依赖裁剪，是独立且体量不小的工程，故单独成任务。

## 2. 目标

1. 让 ZLMediaKit 在 `aarch64-linux-gnu` 交叉编译工具链下成功构建（沿用 macOS 分支已有的 feature 裁剪开关）；
2. 解耦 `ARGUS_USE_ZLM` 与 `APPLE`，改为按「平台是否提供真实媒体后端」判定；
3. `main.cpp` 的媒体后端选择从 `ARGUS_PLATFORM_MACOS` 改为基于 ZLM 可用性的编译宏；
4. 在真实 RK3576/RK3588 板卡上完成父任务移交的全部硬件验收项。

## 3. 功能需求

### FR-1: ZLMediaKit aarch64 交叉编译
- [ ] 在 `deploy/docker/Dockerfile.cross-rknn` 中补齐 ZLM 所需的 aarch64 依赖；
- [ ] 复用现有 feature 裁剪开关（OPENSSL/WEBRTC/SRT/FFMPEG/API/SERVER/HLS/RTPPROXY 等）以控制体积；
- [ ] 交叉编译产物可被 `argus-engine` 正确链接并打包进便携包。

### FR-2: 构建条件解耦
- [ ] 引入独立开关（如 `ARGUS_ENABLE_ZLM`，默认按 `PLATFORM_TARGET` 推导），移除 `ARGUS_USE_ZLM` 对 `APPLE` 的耦合；
- [ ] `main.cpp` 媒体后端选择改为基于该开关而非 `ARGUS_PLATFORM_MACOS`；
- [ ] `PLATFORM_TARGET=macos` 的既有构建与行为**完全不变**（回归哨兵）。

### FR-3: RK 板卡端到端验收
承接父任务 PRD 5.2 的 HW-1 ~ HW-5。

## 4. 验收标准

- [ ] **AC-1**：`PLATFORM_TARGET=rockchip` 下 ZLMediaKit 交叉编译通过，产物链接成功。
- [ ] **AC-2**：`PLATFORM_TARGET=macos` 本地构建与单测全绿，行为无回归。
- [ ] **AC-3 (= HW-1)**：1080p H.264/H.265 码流经 MPP 产出合法 `AV_PIX_NV12` 帧，像素与时间戳正确无花屏。
- [ ] **AC-4 (= HW-2)**：`c_image_ops` 的 convert/pad 输出 640x640 RGB，与 CPU 参考实现 PSNR >= 38dB。
- [ ] **AC-5 (= HW-3)**：设备上 ASan/Valgrind 下 retain/release 严格配对，无泄漏与野指针。
- [ ] **AC-6 (= HW-4)**：RK3576/RK3588 上正确读出 NPU 利用率与芯片温度。
- [ ] **AC-7 (= HW-5)**：加载 `algo-packages/rknn/rk3576/yolov8n`，单路 RTSP 实时分析稳定 >= 25fps。

## 5. 范围外

- 不实现 platform 适配层本身（属父任务）；
- 不修改 SDK C ABI；
- 不修改算法包模型权重与推理结构。

## 6. 前置条件与风险

- **硬件依赖**：AC-3 ~ AC-7 必须在真实 RK 板卡上执行，无板卡时本任务无法收尾；
- **ZLM 交叉编译不确定性**：`engine/third_party/ZLMediaKit` 当前处于 dirty 状态，需先确认其基线版本与本地改动，
  再评估 aarch64 构建的实际工作量（建议先做一次 spike 编译再细化 design/implement）。
