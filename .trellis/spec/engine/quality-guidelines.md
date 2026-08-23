# C++ 质量、测试与算法包工程化规范

> 本规范把构建边界、内存安全、媒体生命周期、包安全、安装自测和本地开发体验转换为可重复执行的质量门禁。

## 1. Scope / Trigger

修改 C++/Objective-C++、CMake、第三方版本、算法包、validator、媒体回调或 CI 时必须读取本规范。

## 2. 构建与静态质量门禁

项目自有 target 使用 C++20，并开启：

```text
-Wall -Wextra -Wpedantic -Werror
```

第三方 target 不继承 `-Werror`。Objective-C++ 只存在于平台实现和 toolkit 平台实现中。

统一命令：

```bash
make -C engine configure
make -C engine build
make -C engine test
make -C engine asan
make -C engine tsan
make -C engine lint
make -C engine e2e
```

`lint` 至少执行：格式检查、clang-tidy、公共 ABI 头禁用私有类型、CMake target 依赖矩阵、动态库导出符号和 `engine_core` 链接纯洁性。

Sanitizer：

- ASan + UBSan：单测、Mock 契约、包 validator fixture 和 shutdown 压力测试；
- LSan：支持的平台启用，任何预期存活全局必须显式说明；
- TSan：有界队列、frame token、ZLM delegate、重连、实例 worker 和停止顺序；
- Sanitizer 构建不能以关闭泄漏检测或大范围 suppressions 伪造通过。

## 3. 确定性测试资产

仓库必须提供可再生 fixture 或生成脚本：

- H.264/H.265：BT.709 limited VUI、无 VUI、前导 P/B、损坏 access unit；
- 固定 1080p 本地 RTSP 回放源及断流/静默控制脚本；
- 已知色块 JPEG/YUV 和 CPU/vImage 期望像素；
- Mock 正常包、额外导出符号包、校验和错误包、自测无回调/多回调/超时包；
- 固定模型转换输入身份和 `.mlpackage` 入口文件校验工具脚本；

测试不得依赖公共互联网摄像头、开发者手工输入或真实 sleep。ZLMediaKit commit、构建选项和 test `config.ini` 必须固定。

## 4. 算法包独立工程门禁

每个包必须提供：

```bash
make configure
make build
make run
make benchmark
make asan
make package
make clean
```

- `make run` 读取 `.env`，环境变量优先于文件；不重新编译即可修改阈值、类别、模型路径、输入图和输出图。
- macOS runner 必须把测试图转换成真实 `CVPixelBuffer` NV12 `av_frame_desc`，不能向 process 传伪造裸 RGB。
- `make run` 打印结果并生成带 bbox/label/confidence 的 `result.jpg`；该可视化属于 runner/toolkit，不进入算法动态库生产路径。
- `make benchmark` 预热后输出 preprocess/inference/postprocess/end-to-end 的 Avg/P50/P99 与持续 FPS，并记录 loops、输入尺寸、模型 digest 和运行平台。
- 从仓库外 `/tmp` 构建时，不允许读取父仓库；`otool -L`/`ldd` 不得出现 engine 库。

## 5. 安装 Validator 进程模型

Engine 不得在多线程主进程 `fork()` 后直接调用 `dlopen`、Core ML 或 Objective-C Framework。必须启动独立 `package_validator` 可执行文件：

```text
Engine -> posix_spawn/exec package_validator
       -> bounded stdout/stderr/result pipe
       -> wall-clock deadline
       -> exit status + structured result
```

macOS `fork` child 在 exec 前只能执行 async-signal-safe 操作。Linux 也沿用独立 executable，保持跨平台一致。

### 5.1 七步校验

1. 流式读取 zip central directory，验证总压缩大小、解压大小、文件数和重复路径上限；
2. 拒绝绝对路径、`..`、反斜杠、NUL、symlink、hardlink、device、FIFO 和非普通文件；
3. 解压到新建 staging 目录，使用 `openat`/等价安全 API，确保目标始终位于 staging root；
4. 解析 manifest，验证关键入口文件存在性与 SHA-256、平台、OS/运行时和 adapter 兼容性；zip 整体 SHA-256 由 Engine 校验；
5. `dlopen(RTLD_NOW|RTLD_LOCAL)`，验证唯一导出、ABI、Library query 与 manifest 一致；
6. 创建 self-test instance，用真实平台帧执行一次 process，验证恰好一条 self-test 结果且不超时；
7. validator 完整销毁 instance/library 并退出成功后，Engine 原子安装 staging 目录。

解压上限由部署 Profile 提供并有安全默认值；测试至少覆盖 zip bomb、路径穿越、重复路径、case collision 和超时 kill。validator 超时后必须终止整个进程组并清理 staging。

## 6. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| 格式/clang-tidy/依赖图不通过 | `lint` 失败 |
| ASan/UBSan/TSan 报告 | 对应质量门禁失败 |
| 算法包在仓库外不能构建 | 可搬运性失败 |
| 动态库有额外导出符号 | 拒绝打包/安装 |
| zip 路径或类型不安全 | validator 拒绝，未 dlopen |
| 入口文件缺失或 SHA-256 不匹配 | validator 拒绝 |
| self-test 零检测但 self-test JSON 合法 | 成功 |
| self-test 无回调、多回调、崩溃或超时 | 拒绝并清理 staging |
| validator 被 signal 终止 | 结构化 `VALIDATOR_CRASHED` |

## 7. Good / Base / Bad Cases

- Good：主进程通过 `posix_spawn` 启动 validator，超时后 kill process group 并清理 staging。
- Base：测试图无目标，但完整链路返回 self-test `object_count=0`。
- Bad：主进程 fork 后直接调用 Core ML，或只检查 zip 文件名而不限制类型/大小。

## 8. Tests Required

- 每个公共 Make target 在干净 build dir 可运行；`configure` 可重复。
- C11/AppleClang/aarch64 GCC ABI 编译；单导出和链接依赖检查。
- ASan/UBSan/TSan 覆盖帧生命周期、队列、delegate、重连和 shutdown。
- validator 全错误矩阵、崩溃、超时、进程组清理和 staging 清理测试。
- 算法包 `/tmp` build/run/package、环境覆盖、result.jpg 像素非空和 benchmark 字段测试。
- RTSP 60 秒解码之外，还必须重复执行断开、track 替换、停止、join 和析构。
- E2E 固定输入，断言 event ID、告警类型、图片原子落盘、gRPC ACK 和重连对账。

## 9. Wrong vs Correct

```cpp
// Wrong: 多线程进程 fork 后加载 Framework
if (fork() == 0) {
  dlopen(plugin, RTLD_NOW);
  run_coreml();
}

// Correct: exec 独立 validator
posix_spawn(&pid, validator_path, &actions, &attrs, argv, environ);
```

```bash
# Wrong: 在仓库内构建无法证明可搬运
cmake -S algo-packages/macos/arm64/yolov8n -B build

# Correct
cp -R algo-packages/macos/arm64/yolov8n "$TMPDIR/portable-yolov8n"
make -C "$TMPDIR/portable-yolov8n" build package
```
