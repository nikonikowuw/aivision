# PRD: 全栈重命名为 Argus (Full Stack Project Rename to Argus)

## 1. 目标 (Goal)

将项目整体由原开发脚手架名称 `niko-vue-admin` 及旧标识 `aivision` 全面重命名为 **`Argus`**。
涵盖 Go 后端模块（Module 名、import 路径、Swagger、Proto、Wire）、C++ 媒体推理引擎与 SDK 契约（命名空间、头文件路径、CMake Target、导出符号）、算法插件包、前端应用标识与配置、根项目文档与构建脚本，建立完全统一、干净的 `Argus` 命名规范。

---

## 2. 详细重命名清单与需求 (Requirements)

### 2.1 Go 后端 (`app/`)
1. **Go 模块名与 Import 路径**：
   - `app/go.mod` 声明更新为 `module argus/app`。
   - 全局替换 `niko-vue-admin/app` 为 `argus/app`。
2. **Protobuf 契约与代码生成**：
   - `proto/` 中 `option go_package = "argus/app/internal/proto/argus/v1";`（或相应路径）。
   - `proto` 协议包由 `package aivision.v1;` 更新为 `package argus.v1;`。
   - `app/scripts/generate-proto.sh` 调整并重新生成 Protobuf Go / C++ 源码。
3. **Wire 依赖注入**：
   - 更新 `app/cmd/api/wire.go` 后重新生成 `wire_gen.go`。
4. **配置与 Swagger 文档**：
   - 配置文件 `app/configs/config.yaml` 中对象存储 bucket 由 `niko-vue-admin` 更新为 `argus`。
   - Swagger 注解 `@title Argus API`、`@description Argus 后端 API 接口文档`，重新生成 swagger 文档。
5. **迁移与环境变量**：
   - 环境变量前缀 `APP_*` / `ARGUS_*` 保持兼容或迁移。

### 2.2 C++ 媒体推理引擎 (`engine/`) 与 算法 SDK (`sdk/`)
1. **C++ 命名空间 (Namespace)**：
   - `namespace aivision` 统一变更为 `namespace argus`（包括 `argus::media`, `argus::platform`, `argus::cv`, `argus::utils` 等）。
2. **SDK 与 Include 路径**：
   - 头文件目录重命名：`sdk/include/aivision/` -> `sdk/include/argus/`。
   - 引擎私有头文件：`engine/include/aivision/` -> `engine/include/argus/`。
   - 全局 C++ 源码 `#include "aivision/..."` -> `#include "argus/..."`。
3. **C ABI 规范导出符号与契约**：
   - 纯 C ABI 导出前缀：`aivision_algo_*` -> `argus_algo_*`，`aivision_result_*` -> `argus_result_*`，结构体/枚举前缀对齐。
4. **CMake 项目与 Target**：
   - CMake Target（`aivision_core`, `aivision_media`, `aivision_sdk_abi`, `aivision_algo_sdk` 等）变更为 `argus_*` 前缀。
   - 检查脚本（如 `check-boundary.sh`）更新检查路径。

### 2.3 算法插件包 (`algo-packages/`)
1. **SDK 依赖同步**：
   - 同步更新算法包内部 vendor 或引用的 `argus-sdk`。
   - 算法包源码中的 include、C ABI 导出函数（`argus_algo_create` 等）及命名空间同步迁移。
2. **CMake 构建与验证**：
   - 算法包的 CMake target 和打包脚本更新。

### 2.4 前端界面 (`ui/`)
1. **应用名称与配置**：
   - `ui/apps/web-antd/package.json` 中的 `name` 更新。
   - 页面标题、系统名称、Logo 文案等替换为 `Argus`。

### 2.5 根项目、文档与构建系统
1. **文档与说明**：
   - `README.md`、`AGENTS.md` 及 `.trellis/spec/` 中的项目名称与架构说明同步更新。
2. **构建脚本**：
   - 根目录 Makefile / CMakeLists.txt 等构建 target 命名与说明同步。

---

## 3. 验收标准 (Acceptance Criteria)

- [x] **Go 后端构建与测试**：`go test ./...` 与 `go build ./...` 在 `app/` 目录下全部通过，Wire 重新生成无冲突。
- [x] **Protobuf 重新生成**：Go 与 C++ 双端 gRPC 桩代码重新生成成功，无旧 package 引用残留。
- [x] **C++ 引擎与 SDK 构建与单测**：`make -C engine configure && make -C engine build && make -C engine test` 全部通过。
- [x] **C ABI 符号一致性**：引擎动态加载与算法包符号检查通过（`bash algo-packages/scripts/check-consistency.sh`）。
- [x] **前端检查与构建**：`pnpm --filter web-antd build` 或 `pnpm check` 通过。
- [x] **全局无旧残留**：代码库中无非必要的 `niko-vue-admin` 遗留引用。
