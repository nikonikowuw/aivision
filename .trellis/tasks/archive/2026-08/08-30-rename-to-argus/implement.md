# 执行计划 (Implementation Plan: Rename to Argus)

## 步骤与验证

### 步骤 1: Protobuf 协议层变更
- **目标**: 将 `proto/aivision/v1/engine.proto` 重构并重命名为 `proto/argus/v1/engine.proto`，更新包名 `package argus.v1` 及 `go_package`。
- **动作**: 调整 `app/scripts/generate-proto.sh`，重新生成 Go 与 C++ proto 代码。
- **验证**: 检查生成的 Go 与 C++ pb 桩代码文件路径与符号正确。

### 步骤 2: C++ SDK & Engine & Algo Packages 重命名
- **目标**: 磁盘头文件目录重命名、全量 C++ 源码命名空间与 include、C ABI 符号替换，CMakeLists 目标重命名。
- **动作**:
  1. `mv sdk/include/aivision sdk/include/argus`
  2. `mv engine/include/aivision engine/include/argus`
  3. 全局替换 `aivision` -> `argus`（命名空间、头文件 include、C ABI 结构体与函数前缀）
  4. 同步 `algo-packages/` 算法插件包及脚本。
- **验证**: `make -C engine configure && make -C engine build && make -C engine test` 编译并单测全部通过。

### 步骤 3: Go 后端重命名
- **目标**: 更新 `go.mod` 及所有 Go 源码 import，重生成 wire，更新配置与 Swagger。
- **动作**:
  1. 修改 `app/go.mod` 首行为 `module argus/app`。
  2. 批量将 `niko-vue-admin/app` 替换为 `argus/app`。
  3. 执行 `make -C app wire` 重新生成 `wire_gen.go`。
  4. 更新 Swagger 注解并生成，更新 `config.yaml`。
- **验证**: `cd app && go test ./... && go build -o bin/api ./cmd/api` 成功。

### 步骤 4: 前端配置与全局文档
- **目标**: 前端包名与页面标题、`README.md`、`AGENTS.md`、`.trellis/` 统一更新。
- **动作**: 调整相关 json/md 文档，替换旧名称。
- **验证**: 前端 `pnpm check` 或编译无报错，全局 grep 确认无脏残留。
