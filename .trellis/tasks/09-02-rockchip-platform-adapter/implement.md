# Implementation Plan: Engine Rockchip Platform Adapter

## 1. 阶段划分 (Execution Phases)

### Phase 1: 交叉编译环境与依赖库准备
- [ ] 1.1 扩展 `deploy/docker/Dockerfile.cross-rknn`，增加 Rockchip `mpp` (`librockchip_mpp.so`) 和 `librga` (`librga.so`) 的头文件与 aarch64 动态库；
- [ ] 1.2 在 `engine/cmake/toolchain-aarch64-linux.cmake` 中添加对 `mpp` 与 `rga` 的查找路径支持。

### Phase 2: Rockchip Platform 模块实现
- [ ] 2.1 创建 `engine/src/platform/rockchip/` 目录；
- [ ] 2.2 实现 `RockchipTelemetry`：支持 Linux 基础遥测与 Rockchip NPU/温度采集；**采集根路径必须可注入**（构造参数默认 `/`），以便单测喂 fixture 文本；
- [ ] 2.3 实现 `RockchipImageOps`：基于 RGA 2.0 (im2d) 实现 `IImageProcessor` 与 `c_image_ops`（含 CPU NEON fallback），并在其内部私有类中实现 `encode_jpeg` / `encode_thumbnail_jpeg`（MPP MJPEG 优先，libjpeg-turbo 回退）；
- [ ] 2.4 实现 `RockchipDecoder`：基于 Rockchip MPP 实现 H.264/H.265 硬件解码与 `av_frame_desc` 封装（`AV_MEM_PLATFORM_SURFACE` + `AV_OPAQUE_DMABUF`，`opaque` 与 `frame_token` 严格分离）；
- [ ] 2.5 实现 `RockchipPlatformAdapter`：组装上述子模块并提供 `PlatformProfile`（含 DEGRADED 原因串）。

### Phase 3: Engine 主程序装配与 CMake 联动
- [ ] 3.1 `engine/CMakeLists.txt:4`：`PLATFORM_TARGET` 缓存描述补充 `rockchip`，并加取值合法性校验；
- [ ] 3.2 `engine/CMakeLists.txt:6`：确认 `enable_language(OBJCXX)` 保持 `APPLE AND macos` 条件不变（rockchip 不需要 OBJCXX）；
- [ ] 3.3 `engine/src/platform/CMakeLists.txt`：新增 `platform_rockchip` 目标，链接 `rockchip_mpp` / `rga`，应用 `argus_enable_strict_warnings()`；
- [ ] 3.4 `engine/src/app/main.cpp`：新增 `#if defined(ARGUS_PLATFORM_ROCKCHIP)` 分支激活 `RockchipPlatformAdapter`（`platform_id = "rk3576-linux"`）；**媒体后端选择分支（main.cpp:206）与 `ARGUS_USE_ZLM` 不动**，属子任务范围；
- [ ] 3.5 `deploy/scripts/build-rknn-bundle.sh`：`-DPLATFORM_TARGET=mock` 改为 `rockchip`，并额外提取打包 `librockchip_mpp.so` 与 `librga.so`。

### Phase 4: 单元测试、契约测试与验证
> 开发机为 macOS/arm64，**MPP/RGA 运行时正确性无法本地验证**。本阶段只覆盖可离线验证的部分，
> 硬件验收项（PRD 5.2 HW-1~HW-5）移交子任务 `09-02-engine-zlm-aarch64-e2e`。
- [ ] 4.1 `RockchipTelemetry` 单测：注入 fixture 根目录，覆盖 NPU load 单核/多核/畸形/空文件/节点缺失、thermal zone type 匹配、`/proc/stat` 与 `/proc/meminfo` 解析、缺失时保持 NaN 且 profile 为 `DEGRADED`；
- [ ] 4.2 `RockchipImageOps` 纯逻辑单测：RGA 对齐约束判定、fallback 触发条件、letterbox 的 `scale`/`pad_w`/`pad_h` 数值计算（与 macOS 实现同参对拍）；
- [ ] 4.3 边界与风格门禁：`bash engine/scripts/check-boundary.sh` + `make -C engine lint`，并确认 `git diff --stat sdk/` 为空（SDK ABI 零改动）；
- [ ] 4.4 Docker 交叉编译端到端打包验证，`readelf -d` 检查产物动态依赖完整。


---

## 2. 验证命令清单 (Verification Commands)

```bash
# 1. 检查代码边界纯洁性与风格
bash engine/scripts/check-boundary.sh
make -C engine lint

# 2. 确认 SDK ABI 零改动（本任务硬约束）
git diff --stat sdk/    # 期望输出为空

# 3. 本地纯逻辑单测（macOS 上可跑，不依赖 MPP/RGA）
make -C engine test

# 4. 构建 Docker 交叉编译镜像
docker build -t argus-cross-builder:rknn -f deploy/docker/Dockerfile.cross-rknn .

# 5. 执行交叉编译打包
bash deploy/scripts/build-rknn-bundle.sh

# 6. 检查产物中动态库与可执行文件依赖
tar -ztvf dist/argus-engine-rknn-linux-arm64.tar.gz
```

## 3. 回滚点 (Rollback Points)

- Phase 3 之前的改动均为新增文件，回滚 = 删除 `engine/src/platform/rockchip/`；
- Phase 3 触及既有构建文件，逐条 commit，`PLATFORM_TARGET=macos` 的本地构建必须始终保持绿色
  （每次改完 CMake 跑一次 `make -C engine build` 作为回归哨兵）。

