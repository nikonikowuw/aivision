# Design：RK3576 RKNN YOLOv8n 检测算法包

> 本文只写技术设计。需求与验收见 `prd.md`；执行顺序见 `implement.md`。
> 决策编号 D1–D10 沿用 `prd.md` 的 Key Decisions。

## 1. 目录与构建拓扑

```text
algo-packages/rknn/rk3576/yolov8n/
├── Makefile                      # configure/build/test/run/benchmark/asan/package/clean
├── CMakeLists.txt                # 顶层：aarch64 门禁 + 版本脚本 + 打包 + CTest
├── manifest.json
├── config.schema.json
├── testimage.jpg
├── .env.example
├── .gitignore
├── vendor/aivision-sdk/          # 由 sync-sdk.sh 生成，禁止手改（见 §1.2）
├── weights/                      # 源权重与中间 ONNX（源码侧证据，不进分发包）
│   ├── yolov8n.pt
│   └── yolov8n.onnx
├── model/
│   └── yolov8n.rknn              # 分发工件；由 .env 或算法内部定位
├── conversion/
│   ├── export_onnx.py            # .pt -> ONNX（airockchip 三分支，D3）
│   ├── convert_rknn.py           # ONNX -> .rknn（INT8，accuracy_analysis）
│   ├── hybrid_quant.py           # 仅在 R2.4 触发时使用
│   ├── golden_fp32.py            # PC 端 ONNX/FP32 golden（R7.1）
│   ├── dataset.txt               # 校准集清单（D5）
│   ├── calib_manifest.csv         # 校准图 文件名 + SHA-256
│   ├── golden/                    # golden 检测结果（json）
│   └── evidence.md                # 证据链（R2.5 / R2.6）
├── src/
│   ├── CMakeLists.txt
│   ├── preprocess/               # NV12 -> 模型输入；image_ops 路径 + CPU fallback
│   ├── inference/                # rknn_runner：Library/Instance 上下文封装
│   ├── postprocess/              # 三分支 DFL 解码 + 反量化 + NMS + 逆 letterbox
│   ├── core/                     # algo_entry.cpp / config.hpp / rules.hpp / manifest_loader
│   └── runner/
│       ├── standalone_runner.cpp
│       └── third_party/          # stb_image.h / stb_image_write.h（D9）
└── tests/
    ├── abi_tests.cpp
    ├── package_tests.cpp
    ├── preprocess_tests.cpp
    └── postprocess_tests.cpp
```

### 1.1 CMake target 拓扑

沿用 macOS 包的分层（`algo-packages/macos/arm64/yolov8n/src/*/CMakeLists.txt`）：

| Target | 类型 | 链接 |
| --- | --- | --- |
| `yolov8n_preprocess` | STATIC | `aivision_sdk_headers` |
| `yolov8n_inference` | STATIC | `aivision_sdk_headers`、`rknnrt` |
| `yolov8n_postprocess` | STATIC | `aivision_sdk_headers` |
| `yolov8n_core` | OBJECT | 上述三者 |
| `yolov8n` | SHARED | `$<TARGET_OBJECTS:yolov8n_core>` + 三个 STATIC + `rknnrt` |
| `run_local` | EXECUTABLE | `yolov8n` + 三个 STATIC + `stb`（header-only） |
| `yolov8n_*_tests` | EXECUTABLE | 按被测模块 |

- `librknnrt` 用 `find_library(RKNN_RT_LIB rknnrt)` + `find_path(RKNN_API_INCLUDE rknn_api.h)`
  定位，找不到时 `FATAL_ERROR` 并提示板端 SDK 路径，**不静默降级到 stub**。
- **stb 只出现在 `run_local` 的 include 路径**，任何 STATIC/SHARED target 都不得引用
  （D9；由 AC2 的导出符号断言与 CMake 依赖分离共同兜住）。
- 警告等级沿用 `-Wall -Wextra -Wpedantic -Werror`；`librknnrt` 是第三方，不继承 `-Werror`。

### 1.2 vendored SDK 的创建顺序（易踩）

`sync-sdk.sh:10` 用 `find algo-packages -name vendor -type d` 定位目标目录。
**必须先 `mkdir -p algo-packages/rknn/rk3576/yolov8n/vendor` 再跑 `sync-sdk.sh`**，
否则脚本静默跳过新包，随后 `check-consistency.sh` 因缺 `vendor/aivision-sdk`
而跳过 SHA 校验（`:36` 的 `if [ -d ... ]`），问题会一直潜伏到打包阶段。

### 1.3 单导出符号（macOS 写法不可用）

macOS 包用 `-Wl,-exported_symbols_list`（`CMakeLists.txt:61-66`）。Linux 用 version script：

```cmake
file(GENERATE OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/yolov8n.map"
     CONTENT "{ global: av_algo_get_abi; local: *; };\n")
target_link_options(yolov8n PRIVATE
    "-Wl,--version-script=${CMAKE_CURRENT_BINARY_DIR}/yolov8n.map")
```

配合已有的 `C/CXX_VISIBILITY_PRESET hidden` + `VISIBILITY_INLINES_HIDDEN`。
测试用 `nm -D --defined-only` 断言导出集合恰为 `{av_algo_get_abi}`。

### 1.4 架构门禁

顶层 CMakeLists 在非 `aarch64` 上 `FATAL_ERROR`（对应 macOS 包 `:18-20` 的
Apple Silicon 门禁）。D10 走板端原生编译，不引入交叉工具链分支。

## 2. 从 macOS 包的移植矩阵

| 文件 | 处置 | 说明 |
| --- | --- | --- |
| `src/core/config.hpp`（175 行） | **原样移植** | 纯 C++ JSON 游标解析 `confidence_threshold`/`iou_threshold`，无平台依赖 |
| `src/core/rules.hpp`（240 行） | **原样移植** | ROI/Mask/Line 几何判定，只依赖 `aivision/algo.h` + `cv/nms.hpp` |
| `src/core/algo_entry.cpp`（554 行） | **结构移植 + 三处改写** | 见 §3 |
| `src/core/manifest_loader.mm` | **废弃** | 约定优于配置，由 `package_root` 相对加载 `.env` 解析 `MODEL_PATH` |
| `src/inference/coreml_runner.mm` | **全新实现** `rknn_runner.cpp` | 见 §4 |
| `src/preprocess/preprocessor.mm` | **全新实现** `preprocessor.cpp` | 见 §5 |
| `src/postprocess/postprocessor.cpp` | **全新实现** | D3 改了输出契约，91 行的 `1x84x8400` 解析不可复用；见 §6 |
| `src/runner/standalone_runner.mm`（658 行） | **重写为 `.cpp`** | 见 §7 |
| SDK toolkit `cv/{letterbox,nms,tracker}.hpp`、`utils/{json,event_id,profiler,env}.hpp` | **直接复用** | 平台中立 |

`algo_entry.cpp` 的这些部分逐字保留：`invoke_abi` 异常转换（`:110-136`）、
`copy_text`/`contains`/`validate_caps_input`（`:138-154`）、
`library_query` 回填（`:251-265`）、`update_config` 候选-原子交换（`:384-396`）、
`set_rules`（`:404-416`）、self-test 分支（`:432-449`）、告警序列化与 image_req 组装
（`:451-484`）、`last_error` 截断（`:516-525`）、静态虚表与 `av_algo_get_abi`（`:533-554`）。

## 3. C ABI 层改写点

### 3.1 常量

```cpp
constexpr const char* kPlatformId  = "rk3576-rknn";   // 原 "macos-arm64-coreml"
constexpr const char* kAlgorithmId = "yolov8n";        // D6 沿用
constexpr const char* kAlarmTypeId = "object_detect";  // D6 沿用
```

### 3.2 `validate_frame`（改写 `algo_entry.cpp:156-178`）

macOS 版硬绑 `AV_OPAQUE_CVPIXELBUFFER` + `AV_MEM_PLATFORM_SURFACE`。按 D7 放宽为：

- `pixel_format == AV_PIX_NV12` 且 `plane_count == 2`（不变）；
- `memory_type == AV_MEM_PLATFORM_SURFACE` 时要求 `opaque_kind == AV_OPAQUE_DMABUF`
  且 `opaque` 非空；
- `memory_type == AV_MEM_HOST` 时要求 `opaque_kind == AV_OPAQUE_NONE`；
  此时帧数据经 `stride[]` + `offset[]` 从宿主内存读取；
- 尺寸区间、stride 下界、色彩枚举白名单的校验逻辑不变。

### 3.3 `instance_negotiate`（改写 `algo_entry.cpp:340-376`）

```text
accepted.pixel_formats      = [AV_PIX_NV12]
accepted.memory_types       = offered ∩ [AV_MEM_PLATFORM_SURFACE, AV_MEM_HOST]   # 保持偏好序
accepted.required_opaque_kind = AV_OPAQUE_DMABUF   # 当 accepted 含 PLATFORM_SURFACE
                              = AV_OPAQUE_NONE     # 当只剩 HOST
```

约束不变：accepted 必须是 offered 的子集；交集为空返回 `AV_ERR_INCOMPATIBLE_FRAME`。

### 3.4 `run_pipeline`

```text
log_color_fallback_once
  -> Preprocessor::prepare_input(frame, image_ops, model_input_attr)   # §5
  -> RknnRunner::run(instance_ctx, input_view, raw_outputs)            # §4
  -> Postprocessor::decode(raw_outputs, output_attrs, conf, iou, w, h) # §6
  -> tracker.update()
  -> apply_rules()
```

失败路径与 macOS 版一致：预处理失败 / 推理失败 → `set_error` + 返回
`AV_ERR_INFERENCE_FAILED`（`algo_entry.cpp:191-219`、`:430`）。

## 4. RKNN 推理层（`src/inference/rknn_runner.{hpp,cpp}`）

### 4.1 两级上下文（D8）

```text
LibraryContext
  ├── package_root / platform_id / log
  ├── model_path        (优先读取 package_root/.env 中的 MODEL_PATH，缺省 model/yolov8n.rknn)
  ├── rknn_context base_ctx           <- rknn_init 一次
  └── 缓存的 input/output tensor attr <- rknn_query 一次（只读、可共享）

InstanceContext
  ├── rknn_context ctx                <- rknn_dup_context(base_ctx)
  ├── 独立 input/output buffer
  ├── InstanceConfig / rules / tracker / 跨帧轨迹
  └── event_counter / self_test_emitted
```

- `library_open` 失败（模型缺失、`rknn_init` 非 0）→ `AV_ERR_MODEL_LOAD_FAILED`。
- `library_close` 必须在全部 Instance 销毁后调用（`algo-package-spec.md` §5 保证
  `library_open/close` 不与该 Library 的实例调用重叠）。
- `instance_destroy` 释放 dup 出的 context 与全部 I/O buffer。

> **待板端验证（阻塞 D8）**：`rknn_dup_context` 的并发语义随 Runtime 版本变化。
> 必须实测「两个 Instance 并发 process，结果互不串扰」。不通过则退化为每 Instance
> 独立 `rknn_init`（代价：模型权重多份内存），并把该结论写回本文件与 spec。

### 4.2 张量 I/O 契约

- 输入输出的 `type / format / dims / w_stride / h_stride / size_with_stride /
  qnt_type / zp / scale` 一律来自 `rknn_query`，**不硬编码、不按公式推算**（R4.4/R5.2）。
- 缓冲分配必须同时覆盖 `rknn_query` 返回的权威 size 与物理布局要求；
  头文件较老没有 `size_with_stride` 时优雅回退（CMake 探测宏）。
- 走 `RKNN_TENSOR_FLOAT32` + `pass_through=0` 会触发 host→device 格式转换，
  **不是 NPU FP32 执行**；本包输入按量化域直接喂（`pass_through` 语义在板端确认后定稿）。
- 输出取 `want_float=0` 拿原生量化值，反量化在后处理里做（R5.1 的量化域筛选前提）。

## 5. 预处理（`src/preprocess/preprocessor.{hpp,cpp}`）

统一接口，两条实现路径（D4）：

```cpp
struct PreparedInput {
    av_image_view view;        // 模型输入 buffer 描述
    aivision::cv::LetterboxInfo lb;  // 供后处理逆映射
    bool from_image_ops;       // 决定用 image_ops->free 还是包内 free
};

class Preprocessor {
public:
    static bool prepare_input(const av_frame_desc* src, const av_image_ops* image_ops,
                              const rknn_tensor_attr& in_attr, PreparedInput& out);
    static void release_input(PreparedInput& in, const av_image_ops* image_ops);
};
```

- **路径 A（`image_ops != nullptr`）**：`alloc` 目标 buffer → `convert` 做 NV12→RGB
  的 CSC + 等比缩放（用帧描述符声明的色彩矩阵与 range）→ `pad` 填充 letterbox 边（114）。
  未来 RK 平台层用 RGA 实现这三个原语，算法包不感知。
- **路径 B（`image_ops == nullptr`）**：包内 CPU fallback。NV12→RGB888 用帧声明的
  BT.709 矩阵与 range；色彩元数据缺失时按 BT.709 limited 兜底，并且**只记一次**降级日志
  （移植 `algo_entry.cpp:180-189` 的 `log_color_fallback_once`）。letterbox 参数一律来自
  `aivision::cv::compute_letterbox(frame.w, frame.h, net_w, net_h)`，pad 值 114。
- 两条路径写入同一 `av_image_view` 布局（宽高/stride 取自 `in_attr`），
  因此 AC11 的像素一致性可以逐字节对拍（容差用于 CSC 定点舍入差）。
- letterbox 参数必须与 `conversion/` 里校准预处理使用的参数完全一致（R2.3），
  否则量化标定与运行期分布错位。

## 6. 后处理（`src/postprocess/postprocessor.{hpp,cpp}`）

### 6.1 输入契约（D3）

三个 stride 层，每层通道 `64 + 80`：

| stride | 特征图 | box 分支 | cls 分支 |
| --- | --- | --- | --- |
| 8 | 80×80 | 64 = 4 边 × 16 DFL bins | 80 |
| 16 | 40×40 | 64 | 80 |
| 32 | 20×20 | 64 | 80 |

实际输出个数与通道排布**以板端 `rknn_query` 的 output attr 为准**；
导出脚本版本不同可能是 6 个输出（box/cls 分开）而非 3 个合并输出。
定稿前在 `conversion/evidence.md` 记录真实 output 列表，代码按查询结果分派，
不按形状猜测边界。

### 6.2 解码流程（R5.1–R5.5）

```text
for each stride level:
  for each grid cell:
    1. 在量化域筛选：q_thresh = quantize(conf_thresh, zp, scale)
       - 换算带 checked rounding + 饱和；禁止原始整数与浮点阈值直接比较
       - 未过阈的 cell 直接跳过，不做任何反量化与 softmax
    2. 过阈 cell 才反量化： v = (q - zp) * scale
    3. cls 分支取 max 得 class_id / score（sigmoid 位置以导出图为准，见 evidence）
    4. box 分支 DFL：每边 16 bins softmax -> 期望值 -> 距离
       -> ltrb 距离 × stride + grid 中心 -> xyxy（网络输入坐标系）
    5. 归一化到 [0,1]（除以 net_w/net_h）-> NormalizedBBox
  end
end
-> aivision::cv::nms_filter(candidates, iou_thresh)      # class-wise，SDK 复用
-> LetterboxInfo::unletterbox_bbox(box, orig_w, orig_h)  # 逆 letterbox，SDK 复用
-> DetectionBox{label=COCO_CLASSES[id], confidence, x,y,w,h, track_id=-1}
```

- 非有限值、`w<=0 || h<=0`、`x_min>=x_max`、越界框一律丢弃（沿用 macOS 版的防御，
  `postprocessor.cpp:56-73`）。
- COCO 80 类标签表从 `algo-packages/macos/arm64/yolov8n/src/postprocess/postprocessor.cpp:13-23`
  原样搬运。
- 后处理耗时若 > 2 ms，再考虑 NEON 向量化（A72/A53 上）；首版不预优化。

## 7. 独立 runner（`src/runner/standalone_runner.cpp`）

对应 macOS 版 658 行的 Linux 重写。macOS 用 ImageIO 读 jpg、CoreGraphics/CoreText 画框，
Linux 无对应设施 → D9 用 stb 单头库。

职责：

1. 读 `.env`（`aivision::utils::env`，命令行/环境变量在 runner 宿主模拟器内优先）取
   `CONF_THRESH` / `IOU_THRESH` / `OUTPUT_IMAGE` / `TARGET_CLASSES` / 输入图 / 模型路径。
2. `stb_image` 解码 `testimage.jpg` → RGB → 转 NV12。
3. 造真实 `av_frame_desc`（R6.3），两条路径：
   - **HOST 路径**：NV12 平面放宿主内存，`memory_type=AV_MEM_HOST`、
     `opaque_kind=AV_OPAQUE_NONE`、`stride[]` 按实际填。
   - **DMA-BUF 路径**：从 `/dev/dma_heap/system`（或板端可用 heap）分配，
     mmap 写入 NV12，`memory_type=AV_MEM_PLATFORM_SURFACE`、
     `opaque_kind=AV_OPAQUE_DMABUF`、`opaque` 持 fd。
     heap 不可用时明确跳过并打印原因，**不伪装成通过**。
4. `dlopen` 自身 `.so`（或直接链接）走完整 ABI：`library_open` → `instance_create`
   → `negotiate` → `process` → 收回调 → `flush` → `destroy` → `library_close`。
5. 打印检测结果；用 `stb_image_write` 输出带 bbox/label/confidence 的 `result.jpg`
   （字形用内置点阵，不引入字体依赖）。
6. `--benchmark`：预热后按 `aivision::utils::profiler` 统计
   preprocess / inference / postprocess / end-to-end 的 Avg/P50/P99 + 持续 FPS，
   并打印 loops、输入尺寸、模型 digest、运行平台（R6.5）。

## 8. 精度对齐方案（D2 / R7 / AC12）

### 8.1 流程

```text
[PC]  conversion/golden_fp32.py
        输入：weights/yolov8n.onnx + 固定图集
        预处理：与板端 CPU fallback 逐参数一致（letterbox 640×640、pad 114、
                RGB、/255 归一化）
        输出：conversion/golden/<image>.json  {label, confidence, bbox[x,y,w,h]}

[板端] run_local --align conversion/golden/
        对同一图集跑 RKNN INT8，与 golden 逐图比对，打印差异表并给出 PASS/FAIL
```

### 8.2 判定阈值（初值，实施期以实测校准并写回本文件）

| 指标 | 初值 | 依据 |
| --- | --- | --- |
| 目标数量 | 完全一致 | INT8 若丢框/多框，是量化崩坏的最直接信号 |
| 逐框 IoU | ≥ 0.90 | 同一模型同一输入，INT8 与 FP32 的框位差异应远小于检测任务的 0.5 IoU 判正阈值；0.90 留出量化舍入余量又能捕捉真实偏移 |
| 置信度绝对偏差 | ≤ 0.05 | asymmetric INT8 在 [0,1] 分数域的典型量化步长约 1/255≈0.004，0.05 约 12 个量化步，容错但不至于放过掉点 |
| 匹配方式 | 同类别 + 贪心最大 IoU | 避免跨类别误配掩盖类别翻转 |

阈值一旦在实施期调整，必须在 `evidence.md` 写明调整前后值与实测依据，
不允许为了让 AC12 通过而无依据放宽（R7.3）。

### 8.3 不达标时的处置链

```text
AC12 FAIL
  -> rknn.accuracy_analysis() 定位低 SNR 层
  -> hybrid_quantization_step1(proposal=True)
  -> 编辑 *.quantization.cfg：只把 DFL / 检测头卷积等敏感层提到 float16
  -> hybrid_quantization_step2 重建 .rknn
  -> 重测 AC12
  -> 仍 FAIL：记录为阻塞项交回规划，不勾 AC12
```

## 9. 待板端确认清单（实施期必须逐项落定并回写本文件）

| # | 待确认项 | 影响 | 落点 |
| --- | --- | --- | --- |
| V1 | `librknnrt` / rknpu 驱动 / Toolkit2 / BSP 版本矩阵 | 转换与运行时是否匹配 | R8.2、`.agents/context/rknn-context/` |
| V2 | RK3576 NPU core 数量与 `rknn_set_core_mask` 可用取值 | 多实例调度策略 | §4.1、`evidence.md` |
| V3 | `rknn_dup_context` 并发是否互不串扰 | D8 成立与否 | §4.1 的阻塞项 |
| V4 | 实际 output tensor 个数与通道排布（3 输出 or 6 输出） | 后处理分派 | §6.1 |
| V5 | sigmoid 是否已在图内（cls 分支是否需再 sigmoid） | 分数解释正确性 | §6.2 步骤 3、R2.6 |
| V6 | 输入 `pass_through` 语义与所需 `w_stride`/`h_stride` | 预处理输出布局 | §4.2、R4.4 |
| V7 | dma_heap 节点可用性与 NV12 布局约定 | runner DMA-BUF 路径 | §7 步骤 3 |
| V8 | `runtime_constraints` 该声明哪些字段 | manifest 能否被未来 RK 适配器接受 | R1.4 |
| V9 | benchmark 实测值 → `fps_tiers` / `min_free_memory_mb` | 资源账本可用性 | R1.5、AC4 |

> `manifest-schema.md` §2.5 规定「未知约束字段必须拒绝」。V8 在 RK 平台适配器落地前
> 无对手方可对齐，因此本任务取**最小集合**（只放已能被现有 validator 逻辑接受的字段），
> 并把待对齐点留在本文件；平台层任务落地时按此清单补齐。
