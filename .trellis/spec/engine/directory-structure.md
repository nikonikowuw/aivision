# 目录结构与构建边界规范

> 适用于 `engine/`、`sdk/` 与 `algo-packages/`。目标是让平台核心、媒体后端、硬件适配和算法插件在构建图上可独立验证，而不是只靠代码评审维持边界。

## 1. Scope / Trigger

新增或修改下列任一内容时必须读取本规范：

- CMake target、链接依赖、第三方库或系统 Framework；
- `engine/`、`sdk/`、`algo-packages/` 下的目录或公共头文件；
- 新媒体后端、新运行平台或新算法包；
- SDK vendoring、算法包打包与可搬运性检查。

## 2. 依赖拓扑与 Target 签名

依赖方向固定为 `engine -> sdk <- algo-packages`。`engine/` 与具体算法包源码树之间没有编译期路径依赖。

```text
sdk_abi_headers (INTERFACE, 纯 C ABI)
        ^                         ^
        |                         |
engine_core                 algorithm package
        ^                         +-- vendor/aivision-sdk/
        |
engine_app
  +-- media_zlm
  +-- platform_macos | platform_mock
```

引擎必须建立以下独立 target：

| Target | 类型 | 允许依赖 | 禁止依赖 |
| --- | --- | --- | --- |
| `sdk_abi_headers` | `INTERFACE` | C 标准头 | C++、系统 Framework、第三方实现库 |
| `platform_api` | `INTERFACE` | `sdk_abi_headers` | 任意平台实现 |
| `media_api` | `INTERFACE` | 公共编码包描述 | ZLM、FFmpeg、平台 Framework |
| `engine_core` | `STATIC` | `sdk_abi_headers`、`platform_api`、`media_api`、gRPC/Protobuf、JSON | ZLM、VideoToolbox、CoreML、RKNN、MPP、RGA、CANN |
| `media_zlm` | `STATIC` | `media_api`、固定 commit 的 ZLMediaKit | 平台推理实现 |
| `platform_macos` | `STATIC` | `platform_api`、VideoToolbox、CoreVideo、Accelerate、ImageIO | ZLM、具体算法实现 |
| `platform_mock` | `STATIC` | `platform_api`、C/C++ 标准库 | 摄像头、GPU/NPU、系统私有 Framework |
| `package_validator` | `EXECUTABLE` | SDK、插件加载器、目标平台适配 | `engine_app` 进程内状态 |
| `engine_app` | `EXECUTABLE` | `engine_core`、一个媒体后端、一个平台适配层 | 具体算法源码/静态库 |

ZLMediaKit 可以作为所有部署 Profile 共用的媒体实现，但必须位于 `media_zlm`，不能直接静态链接进 `engine_core`。这同时满足“统一媒体源”和“核心层不依赖具体媒体库”。

推理模型加载和推理上下文归算法包所有。平台适配层只声明推理运行时能力并提供帧、图像、资源和指标服务，不提供第二套 `IInferenceContext`。

## 3. 目录契约

### 3.1 `sdk/`

```text
sdk/
├── include/aivision/
│   ├── types.h                 # 纯 C：帧描述符、枚举、基础类型
│   ├── algo.h                  # 纯 C：插件虚表、生命周期、回调
│   └── result.h                # 纯 C：结果 kind 和长度上限
├── toolkit/
│   ├── include/aivision/{cv,utils,platform}/
│   └── src/platform/<platform>/   # 可含 .mm/.cpp 与系统 Framework 依赖
├── cmake/
├── docs/
└── VERSION
```

`sdk/include/aivision/*.h` 是 ABI 权威源，必须能被 C11 和 C++20 单独包含。`sdk/toolkit/` 是算法开发辅助库，可以使用 C++20 和目标平台 Framework，但不得被误称为“纯 C、零实现依赖”。

### 3.2 `engine/`

```text
engine/
├── CMakeLists.txt
├── Makefile
├── cmake/
├── third_party/ZLMediaKit/
├── proto/aivision/v1/
├── include/aivision/{core,media,platform}/
├── src/
│   ├── core/{frame,task,algo,image,resource,telemetry,ipc}/
│   ├── media/zlm/
│   ├── platform/{macos,mock}/
│   ├── validator/
│   └── app/
├── tests/{unit,contract,integration,fixtures,stub_server}/
└── docs/
```

### 3.3 `algo-packages/`

```text
algo-packages/
├── scripts/{sync-sdk.sh,check-consistency.sh}
└── <family>/<model>/<algorithm>/
    ├── Makefile
    ├── CMakeLists.txt
    ├── vendor/aivision-sdk/
    ├── src/{preprocess,inference,postprocess,core,runner}/
    ├── conversion/
    └── package/
```

`<family>/<model>` 只用于仓库组织。运行时唯一权威是 `manifest.json.platform_id`；一致性脚本负责校验路径映射，运行时代码不得解析仓库目录名。

算法包 CMake 只能引用当前包目录和 `vendor/aivision-sdk/`。禁止通过 `../`、符号链接或绝对路径读取仓库顶层 `sdk/`、`engine/` 或其他算法包。

## 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| `engine_core` 链接 ZLM 或平台 Framework | 构建边界检查失败 |
| 公共 ABI 头出现 `CVPixelBuffer`、`MLModel`、`rknn_*`、`acl*` | ABI 纯洁性检查失败 |
| `sdk/toolkit/platform/macos` 使用 `CVPixelBuffer` | 允许，但只能由 macOS toolkit target 编译 |
| 算法包引用父仓库路径 | 可搬运性检查失败 |
| `engine_app` 链入具体算法符号 | 符号纯洁性检查失败 |
| 未固定 ZLMediaKit commit | 配置阶段失败 |
| 同一构建激活多个 `platform_id` | 配置阶段失败 |

## 5. Good / Base / Bad Cases

- Good：`engine_core` 通过 `media_api` 消费编码帧，`engine_app` 注入 `media_zlm` 与 `platform_macos`。
- Base：无硬件 CI 构建 `engine_core + platform_mock`，不编译 macOS Framework 代码。
- Bad：为了复用 ZLM 回调，直接在 `src/core/media` 包含 ZLM 头并链接 ZLM。

## 6. Tests Required

- CMake graph 测试：读取 File API/构建产物依赖，断言 target 依赖矩阵。
- 头文件测试：C11、AppleClang C++20、aarch64 GCC 分别编译三个 ABI 头。
- 符号测试：`nm`/`otool -L` 断言核心层无平台私有符号，算法包只导出 `av_algo_get_abi`。
- 可搬运性测试：把单个算法包复制到仓库外目录后执行 `make build package`。
- SDK 一致性测试：比较 vendored SDK 与上游 SDK 的规范化 SHA-256。

## 7. Wrong vs Correct

```cmake
# Wrong: 核心层直接依赖具体媒体库和平台 Framework
target_link_libraries(engine_core PRIVATE ZLMediaKit CoreML)

# Correct: 具体实现只在装配层出现
target_link_libraries(engine_core PUBLIC media_api platform_api)
target_link_libraries(engine_app PRIVATE engine_core media_zlm platform_macos)
```

```cmake
# Wrong: 算法包跳出自身目录读取顶层 SDK
target_include_directories(yolov8n PRIVATE ../../../../sdk/include)

# Correct
target_include_directories(yolov8n PRIVATE
  "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include")
```
