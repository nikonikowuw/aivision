# Implement：RK3576 RKNN YOLOv8n 检测算法包

> 执行清单。技术依据见 `design.md`（§ 引用指向该文件），验收见 `prd.md`。
> `PKG=algo-packages/rknn/rk3576/yolov8n`

## 阶段总览

```text
M0 板端基线      -> 版本矩阵落地，V1/V2/V7 确认        [板端]
M1 模型转换      -> .rknn + 证据链 + golden            [PC]
M2 包骨架        -> 目录/CMake/manifest 通过一致性检查  [主机+板端]
M3 推理层        -> rknn_runner，V3/V4/V5/V6 确认      [板端]
M4 预处理        -> 双路径 + 像素一致性                 [板端]
M5 后处理        -> 三分支 DFL 解码                     [板端]
M6 ABI 装配      -> 完整虚表 + 契约测试                 [板端]
M7 工程门禁      -> runner/benchmark/asan/package/可搬运 [板端]
M8 精度对齐      -> AC12 核心门禁                        [PC+板端]
M9 标定与收口    -> fps_tiers/evidence/spec 回写         [板端]
```

M1 与 M2 可并行（M1 在 PC、M2 在板端/主机）。M3 起严格串行。

---

## M0 板端基线与环境（R8）

1. 板端运行 rknn-pro 诊断：
   `~/.claude/skills/rknn-pro/scripts/rknn-diag.sh -o evidence.txt`
2. 主机渲染基线：
   `python3 ~/.claude/skills/rknn-pro/scripts/render-project-baseline.py evidence.txt --write-default`
   → 产出 `.agents/context/rknn-context/{machine_id}.md`
3. 人工复核生成结果与原始 evidence 一致（生成的基线是草稿不是证明）。
4. 逐项落定 `design.md` §9 的 **V1**（版本矩阵）、**V2**（NPU core 拓扑）、
   **V7**（dma_heap 节点与 NV12 布局）。

**验证**：基线文件覆盖内核/BSP、`librknnrt`、rknpu 驱动、`librga` + RGA 驱动、
NPU core 拓扑；每条结论标注来源，无从 RK3588/RK3568 外推的结论。→ AC14

**回滚点**：无代码产出，失败即停并把缺口写回 `prd.md` Risks。

---

## M1 模型转换与证据链（R2，PC 侧）

1. PC 安装 rknn-toolkit2，记录版本。
2. `conversion/export_onnx.py`：airockchip 路径导出无 decode 头三分支 ONNX，
   静态 `[1,3,640,640]`（D3）。
3. 校验 ONNX 契约：
   `python3 ~/.claude/skills/rknn-pro/scripts/inspect-onnx-model.py weights/yolov8n.onnx`
   记录 IR/opset、输入输出名与形状、动态维、**图内是否含归一化前缀**。
4. 构建校准集（D5）：COCO val2017 取 100–500 张 → `conversion/dataset.txt`
   + `conversion/calib_manifest.csv`（文件名 + SHA-256）。校准预处理与
   `design.md` §5 的 letterbox 参数逐参数一致。
5. `conversion/convert_rknn.py`：`target_platform="rk3576"`、
   `mean_values=[[0,0,0]]`、`std_values=[[255,255,255]]`、RGB、
   `do_quantization=True`、`quantized_dtype="asymmetric_quantized-8"`，
   保留 verbose 日志与 build report。
6. `rknn.accuracy_analysis()` 产出逐层 SNR。
7. `conversion/golden_fp32.py`：ONNX/FP32 对固定图集产 golden → `conversion/golden/*.json`（R7.1）。
8. 写 `conversion/evidence.md`（R2.5 全字段 + R2.6 三处归一化状态）。

**验证**：
- `.rknn` 产出且 SHA-256 记录在案；重跑脚本可复现（或记录不可复现原因）→ AC13
- `evidence.md` 中 ONNX 图内 / Toolkit2 / 应用侧三处归一化**均非 `unknown`**，
  且端到端只有一处生效 → R2.6
- build report 反映的实际逐层精度已记录（`do_quantization=True` 只证明请求，不证明结果）

**回滚点**：`.rknn` 与脚本都是新增文件，删除即回滚，不影响仓库其它部分。

---

## M2 包骨架（R1）

1. `mkdir -p $PKG/vendor` —— **必须先建空 vendor 目录**（`design.md` §1.2），
   否则 `sync-sdk.sh` 静默跳过本包。
2. `bash algo-packages/scripts/sync-sdk.sh`
3. 建目录树（`design.md` §1）与 `.gitignore`（照搬 macOS 包并去掉 `*.dSYM/`）。
4. 顶层 `CMakeLists.txt`：aarch64 门禁、`find_library(rknnrt)` / `find_path(rknn_api.h)`
   缺失即 `FATAL_ERROR`、version script 单导出（§1.3）、`-Wall -Wextra -Wpedantic -Werror`、
   `aivision_add_algo_package(yolov8n ... PLATFORM_ID rk3576-rknn)`、CTest。
5. `Makefile`：照搬 macOS 版 8 个 target，`asan` 去掉 `CMAKE_OBJCXX_FLAGS`。
6. `manifest.json`（D6 + R1.3）：精简为最新规范格式（无 `files[]`、`entry_library` 等冗余字段）；
   `fps_tiers` / `min_free_memory_mb` / `runtime_constraints` 先填**明确标注为待标定的占位值**，
   M9 用实测替换。
7. `config.schema.json`、`testimage.jpg`、`.env.example` 从 macOS 包搬运（增加 `MODEL_PATH=model/yolov8n.rknn` 等默认项）。
8. 移植 `src/core/config.hpp` 与 `src/core/rules.hpp`（原样，`design.md` §2）。

**验证**：
- `[主机]` `bash algo-packages/scripts/check-consistency.sh` 通过 → AC1
- `[板端]` `make -C $PKG configure` 成功（此时尚无源文件可 build）

**回滚点**：整个 `$PKG` 目录为新增，`rm -rf $PKG` 即完全回滚。

---

## M3 推理层（`design.md` §4）

1. 模型与私有配置加载：以 `package_root` 相对读取 `<package_root>/.env` 解析 `MODEL_PATH`（缺省 fallback 为 `model/yolov8n.rknn`）；严禁在算法库内使用 `std::getenv`。
2. `src/inference/rknn_runner.{hpp,cpp}`：
   - `LibraryRuntime`：`rknn_init` + `rknn_query` 缓存 input/output attr；
   - `InstanceRuntime`：`rknn_dup_context` 派生 + 独立 I/O buffer；
   - buffer 分配同时满足 `rknn_query` 权威 size 与物理布局；老头文件无
     `size_with_stride` 时 CMake 探测宏优雅回退。
3. 板端落定 `design.md` §9 的 **V3 / V4 / V5 / V6**：
   - V3：写两实例并发 process 用例，实测互不串扰。
     **不通过则退化为每实例独立 `rknn_init`**，并把结论回写 `design.md` §4.1 与 `prd.md` D8。
   - V4：打印全部 output attr，定稿后处理分派（3 输出 or 6 输出）。
   - V5：确认 cls 分支 sigmoid 是否已在图内。
   - V6：确认 `pass_through` 语义与所需 stride。

**验证**：单测能 `rknn_init` → `dup` → 喂全零输入 → 拿到形状正确的输出；
两实例并发用例通过或已记录退化决策。

**回滚点**：M3 只新增 `src/inference/` 与私有配置解析模块，
删除后 M2 骨架仍可 configure。

---

## M4 预处理（R4，`design.md` §5）

1. `src/preprocess/preprocessor.{hpp,cpp}`：`prepare_input` / `release_input`。
2. 路径 A：`image_ops` 的 `alloc`→`convert`→`pad`（值 114）。
3. 路径 B：CPU fallback，NV12→RGB888 用帧声明色彩矩阵与 range，
   缺失时 BT.709 limited 兜底 + 只记一次降级日志。
4. letterbox 参数一律取 `aivision::cv::compute_letterbox`，
   且与 M1 步骤 4 的校准预处理逐参数一致。
5. `tests/preprocess_tests.cpp`：注入 fake `av_image_ops` 与 CPU fallback 对拍。

**验证**：`make -C $PKG test` 中预处理像素一致性用例通过 → AC11

**回滚点**：路径 A 若在无平台层下难以构造 fake ops，先交路径 B 并把 AC11 标为未达成，
**不得**删掉一致性测试来"通过"。

---

## M5 后处理（R5，`design.md` §6）

1. `src/postprocess/postprocessor.{hpp,cpp}`：按 M3-V4 定稿的输出布局分派。
2. 量化域阈值换算（checked rounding + 饱和）→ 过阈才反量化 `(q-zp)*scale`。
3. DFL：每边 16 bins softmax 求期望 → ltrb 距离 × stride + grid 中心 → xyxy。
4. 复用 `aivision::cv::nms_filter` 与 `LetterboxInfo::unletterbox_bbox`。
5. COCO 80 类标签表从 macOS 包 `postprocessor.cpp:13-23` 原样搬运。
6. `tests/postprocess_tests.cpp`：构造已知量化输入，断言解码出的框位与类别；
   覆盖非有限值、零面积、越界框的丢弃分支。

**验证**：`make -C $PKG test` 后处理用例全绿。

**回滚点**：后处理是纯函数模块，可独立回退重写而不影响 M3/M4。

---

## M6 ABI 装配（R3，`design.md` §3）

1. `src/core/algo_entry.cpp`：从 macOS 版结构移植，改 §3.1 常量、
   §3.2 `validate_frame`、§3.3 `negotiate`、§3.4 `run_pipeline`；
   其余逐字保留（`design.md` §2 末尾清单）。
2. `tests/abi_tests.cpp` / `tests/package_tests.cpp`：
   - 错误 api_version 返回 NULL；虚表 `size` / 函数指针完整性；
   - Library/Instance 创建销毁顺序与中途失败清理；
   - `negotiate` 正反例（含 offered 无 NV12 → `AV_ERR_INCOMPATIBLE_FRAME`）；
   - 非法 config → `AV_ERR_CONFIG_INVALID` 且旧配置仍生效；
   - 规则几何非法 → `AV_ERR_INVALID_ARG`；ROI/Mask/Line 组合与热更新清轨迹；
   - self-test 恰好一次回调、零检测仍合法、无图片请求；
   - 重复 event_id 不产生、`event_id` 不含 `/`；
   - `last_error` 截断与 NUL 结尾；
   - C++ 异常不越 ABI（注入抛异常路径断言返回 `AV_ERR_INTERNAL`）。
3. 导出符号断言：`nm -D --defined-only build/libyolov8n.so` 恰为 `av_algo_get_abi`。

**验证**：`make -C $PKG build && make -C $PKG test` 全绿 → AC2 / AC7 / AC8 / AC9 / AC10

**回滚点**：`algo_entry.cpp` 是装配层，逻辑错误可回退到 M5 状态重装。

---

## M7 工程门禁（R6，`design.md` §7）

1. vendored stb 到 `src/runner/third_party/`（**不得**放 `vendor/aivision-sdk/`）。
2. `src/runner/standalone_runner.cpp`：`.env` 读取、stb 解码、
   HOST 与 DMA-BUF 两条造帧路径、完整 ABI 调用链、结果打印、
   `stb_image_write` 输出带框 `result.jpg`、`--benchmark` 四段统计。
3. dma_heap 不可用时明确跳过并打印原因，**不伪装成通过**。
4. `make asan` 串起 CTest + 真实 runner。

**验证**：
- `make -C $PKG run` 打印检测框 + 生成非空带框 `result.jpg`，两条造帧路径均跑通 → AC3
- `make -C $PKG benchmark` 四段 Avg/P50/P99 + FPS + loops/尺寸/digest/平台 → AC4
- `make -C $PKG asan` 零报告，未关泄漏检测、未用大范围 suppressions → AC5
- `make -C $PKG package` 产 zip + `.sha256`；
  `cp -R $PKG /tmp/portable-yolov8n && make -C /tmp/portable-yolov8n build package` 通过，
  `ldd` 无 engine 库、无父仓库路径 → AC6

**回滚点**：runner 与生产动态库解耦，runner 问题不阻塞 M6 成果。

---

## M8 精度对齐（R7，`design.md` §8）—— 核心门禁

1. `run_local --align conversion/golden/` 对固定图集比对。
2. 判定：目标数量一致、逐框 IoU ≥ 0.90、置信度绝对偏差 ≤ 0.05、
   同类别贪心最大 IoU 匹配（初值，见 `design.md` §8.2）。
3. FAIL → `accuracy_analysis` 定位低 SNR 层 → `hybrid_quantization_step1(proposal=True)`
   → 只把 DFL / 检测头卷积等敏感层提到 `float16` → `step2` 重建 → 重测。
4. 阈值若调整，在 `evidence.md` 写明调整前后值与实测依据。

**验证**：AC12 通过。仍不通过则**记录为阻塞项交回规划，不勾 AC12、不无依据放宽阈值**（R7.3）。

**回滚点**：混合量化产出新 `.rknn`，保留原 INT8 版本与两者证据以便对比。

---

## M9 标定与收口

1. 用 M7 的 benchmark 实测标定 `manifest.json` 的 `fps_tiers` 与
   `min_free_memory_mb`，替换 M2 的占位值（R1.5、`design.md` §9 V9）。
2. 落定 `runtime_constraints` 最小集合（V8），并把待对齐点保留在 `design.md` §9。
3. 补齐 `conversion/evidence.md`：板端 `rknn_query` 到的 input/output tensor 属性
   （type/format/dims/stride/qnt_type/zp/scale）、V2–V6 的实测结论。
4. 回写 `design.md` §9 全部 V 项的最终结论。
5. 重跑全量验证命令（下节）。
6. Phase 3.3：把稳定契约写进 `.trellis/spec/engine/`
   （候选：`platform-guidelines.md` 补 RK 家族条目；`quality-guidelines.md` §4
   补 Linux 包的 version-script 单导出与 `ldd` 判据；`algo-package-spec.md` 视
   V3 结论决定是否补「多实例推理上下文派生」条款）。

---

## 全量验证命令

```bash
# 主机（x86_64）
bash algo-packages/scripts/check-consistency.sh

# 板端（RK3576, aarch64）
PKG=algo-packages/rknn/rk3576/yolov8n
make -C $PKG clean
make -C $PKG configure
make -C $PKG build
make -C $PKG test
nm -D --defined-only $PKG/build/libyolov8n.so     # 期望仅 av_algo_get_abi
make -C $PKG run
make -C $PKG benchmark
make -C $PKG asan
make -C $PKG package
cp -R $PKG /tmp/portable-yolov8n && make -C /tmp/portable-yolov8n build package
ldd /tmp/portable-yolov8n/build/libyolov8n.so     # 期望无 engine 库

# 精度门禁（PC 出 golden，板端比对）
python3 $PKG/conversion/golden_fp32.py
$PKG/build/run_local --align $PKG/conversion/golden/
```

> 报告完成前必须给出：执行过的命令、结果、跳过项和跳过原因
> （`.trellis/spec/engine/index.md` §4）。仅构建成功不能替代 ABI、sanitizer、
> 可搬运性与精度对齐验证。

## 风险高的文件与注意点

| 文件 | 风险 | 缓解 |
| --- | --- | --- |
| `$PKG/vendor/aivision-sdk/**` | 手改即破坏 `check-consistency.sh` 的全量 SHA | 只由 `sync-sdk.sh` 生成，任何改动先改顶层 `sdk/` 再同步 |
| `$PKG/manifest.json` | 未知顶层字段直接被 `validate_package.cmake:19-30` 拒绝 | 字段集严格限定在 `manifest-schema.md` §2.1 的 16 个 |
| `$PKG/CMakeLists.txt` | 漏 version script → 多余导出符号 → 拒绝打包 | AC2 的 `nm -D` 断言进 CTest |
| `src/runner/third_party/` | stb 误链进生产动态库 | 只在 `run_local` 的 include/link 路径；AC2 兜底 |
| `src/inference/rknn_runner.cpp` | buffer 尺寸按公式推算 → 越界写 | 一律以 `rknn_query` 权威值为准（R4.4/R5.2） |
| `src/postprocess/postprocessor.cpp` | 拿原始整数与浮点阈值直接比较 | 量化域阈值换算带 checked rounding + 饱和 |

## 本任务不碰的路径

`app/`、`ui/`、`engine/`、`sdk/`（除非 V 项确认需要改 ABI —— 当前结论是不需要，
见 `prd.md` B3）、`algo-packages/macos/**`。
