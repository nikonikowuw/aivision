# 执行计划

## 1. 基线记录

- [x] 记录当前工作区 `git status`、HEAD (`1ca263ab`)、face_recognition 相关 diff。
- [x] 记录 Apple M4 (10-core)、24 GB Unified Memory、macOS 26.5.1 (Darwin 25.5.0)、Release 环境。
- [x] 复跑现有 `make -C algo-packages/macos/arm64/face_recognition benchmark`，保存原始结果 (7.22 ms avg, 138.5 FPS)。

## 2. 算法包 profiling

- [x] 增加可选阶段计时（11 阶段细分）和帧级计数（检测、跟踪、embedding、image request），不改变默认生产行为。
- [x] 输出 JSONL 与 `.summary.json`，包含 latency 分位数 (avg, min, max, p50, p95, p99, fps)、检测数、track 数、embedding 数和 image request 数。
- [x] 增加 `os_signpost` 标记，并通过 CMake `ENABLE_PROFILING` 开关控制（默认 OFF）。

## 3. 输入矩阵

- [x] 当前 fixture：30 warmup + 300 measured (best_shot, all, detection_only)。
- [x] 1080P/4K NV12：记录转换、临时内存和 RSS (best_shot, all, detection_only)。
- [x] synthetic NV12：覆盖 stride 对齐 (packed, aligned64, padded128) 和 0/1/4/16 目标场景。
- [x] VideoToolbox CVPixelBuffer：验证真实 IOSurface 硬件后备 (`surface_iosurface`)。
- [x] 稳定性：60 warmup + 1000 measured，观察 RSS (+0.09 MB 漂移) 和尾延迟。

## 4. 运行时观测

- [x] 使用 `/usr/bin/time -l` 采集 CPU/RSS/缺页/指令周期，`xctrace` 确认为 BLOCKED (缺少完整 Xcode.app)。
- [x] 使用 `/usr/bin/sample` 堆栈采样证实 SCRFD 9-Head 拷贝中的 `CoreML::MultiArrayBuffer::loadBuffer()` 和互斥锁同步开销。
- [x] 保存采集命令、环境、原始 `.jsonl` 与摘要至 `benchmark/` 目录。

## 5. 交付与检查

- [x] 在子任务目录写入 `analysis.md` 和 `benchmark/` 原始数据（全 19 组场景）。
- [x] 每条结论标注 L1/L2/L3/L4，并严格区分直接测量与推断。
- [x] 执行 `make -C algo-packages/macos/arm64/face_recognition build test`，全部 3 个测试用例通过 (100%)。
- [x] 执行 `make -C algo-packages/macos/arm64/face_recognition asan` 和 `package`，ASan 零错误通过，打包成功。
- [x] 执行 `check-boundary.sh`，边界检查和符号纯洁性 100% 通过。
- [x] 将稳定结论和待整改项整理于 `analysis.md`，不在本子任务直接修改生产推理逻辑。

## 风险点

- 当前工作区存在其他未提交变更，不得 reset 或清理。（已遵循：无任何 stash/reset，保留现有变更）
- 单张图片重复帧不能代表真实视频运动场景。（已在报告明确标注合成场景限制，区分 L1 与 L4 推论）
- `MLComputeUnitsAll` 不能作为 ANE 使用证据。（已在报告中区分物理采样与 Core ML 内部调度）
- 真实 VideoToolbox 和 Instruments 可能受当前环境权限限制。（xctrace 已记录为 BLOCKED，成功使用 sample 与 time -l 形成替代观测）
