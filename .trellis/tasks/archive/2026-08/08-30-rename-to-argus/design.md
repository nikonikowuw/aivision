# 全栈重命名技术设计 (Design: Rename to Argus)

## 1. 架构映射表

| 维度 | 旧名称/路径 | 新名称/路径 (`Argus`) |
| :--- | :--- | :--- |
| **Go 模块名** | `niko-vue-admin/app` | `argus/app` |
| **Go 源码 Import** | `niko-vue-admin/app/internal/...` | `argus/app/internal/...` |
| **Protobuf Package** | `package aivision.v1;` | `package argus.v1;` |
| **Proto Go Package** | `niko-vue-admin/app/internal/proto/aivision/v1` | `argus/app/internal/proto/argus/v1` |
| **C++ 命名空间** | `namespace aivision` | `namespace argus` |
| **SDK 头文件路径** | `sdk/include/aivision/` | `sdk/include/argus/` |
| **引擎内部头文件** | `engine/include/aivision/` | `engine/include/argus/` |
| **C ABI 导出函数** | `aivision_algo_create`, `aivision_algo_destroy` | `argus_algo_create`, `argus_algo_destroy` |
| **C ABI 类型前缀** | `aivision_frame_t`, `aivision_status_t` | `argus_frame_t`, `argus_status_t` |
| **CMake 根项目/Target** | `aivision_engine`, `aivision_sdk_*` | `argus_engine`, `argus_sdk_*` |
| **MinIO Bucket** | `niko-vue-admin` | `argus` |
| **Swagger Title** | `niko-vue-admin API` | `Argus API` |
| **前端应用标识** | `vben-admin-monorepo` / `niko-vue-admin` | `argus-admin` |

---

## 2. 详细重构阶段设计

### 阶段 1: Protobuf 协议与生成脚本
1. 移动/重构 `proto/` 目录结构：`proto/aivision/v1/engine.proto` -> `proto/argus/v1/engine.proto`。
2. 更新 package 声明为 `argus.v1`，服务声明为 `ArgusEngineService`。
3. 更新 `app/scripts/generate-proto.sh`，重新生成 Go 与 C++ gRPC 桩代码。

### 阶段 2: SDK 与 C++ 引擎重构
1. 移动头文件目录：
   - `mv sdk/include/aivision sdk/include/argus`
   - `mv engine/include/aivision engine/include/argus`
2. 批量替换 C++ 源码中：
   - `#include "aivision/..."` -> `#include "argus/..."`
   - `namespace aivision` -> `namespace argus`
   - `aivision::` -> `argus::`
   - C ABI 符号 `aivision_` -> `argus_`
3. 更新 CMakeLists.txt 及其 Target 命名、测试用例。
4. 同步更新 `algo-packages/` 中的算法包与 SDK 脚本。

### 阶段 3: Go 后端重构
1. 更新 `app/go.mod`。
2. 全局替换 `app/` 目录下 Go 文件的 import 路径。
3. 重新执行 `make wire` 生成 `wire_gen.go`。
4. 更新 `app/configs/config.yaml` 默认值与 Swagger 注解。
5. 重新生成 Swagger 文档 (`swag init`)。

### 阶段 4: 前端与项目文档
1. 更新 `ui/` 下应用 package 名称与国际化/页面主标题。
2. 更新 `README.md`、`AGENTS.md`、`.trellis/spec/`。
