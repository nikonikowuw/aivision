# RTSP 摄像头视频源管理 MVP — 执行计划

> 顺序执行，每步带验证点。文档契约见 `prd.md`，技术设计见 `design.md`。

## Phase A：C++ 引擎（先做，作为 Go 测活的依赖）

### A1. 扩展 engine.proto
- 在 `engine/proto/aivision/v1/engine.proto` 增加 `ProbeCamera` RPC + `ProbeCameraRequest/Response` + `ProbeAttempt`（字段见 design.md §5）。
- 验证：`app/scripts/generate-proto.sh` 重新生成 Go stub；`app/internal/proto/.../engine.pb.go` 出现新消息与方法；C++ stub（engine/.../proto）同步更新。

### A2. 媒体抽象支持传输策略 + 首帧同步
- `engine/include/aivision/media/media_api.hpp`：`IMediaSource::start` 增加 transport 参数（或新增 probe 专用方法）；定义一次性测活接口（如 `probe_once(url, transport, timeout) -> ProbeOutcome`）。
- `engine/src/media/zlm/zlm_source.cpp`：根据 transport 设置 `RTP_TCP`/`RTP_UDP`；暴露 track 元数据（codec/width/height/fps）与首帧事件。
- 验证：`make -C engine build` 通过；现有 `make -C engine test` 不回归（CameraTask 默认 TCP 不变）。

### A3. 实现测活核心 probe_rtsp
- 新增 `engine/src/core/camera/probe_rtsp.cpp/.hpp`：协议注册表、TCP→UDP 顺序、5s/次超时、首帧有界等待（promise/cv）、RAII 释放、稳定失败码分类。
- 协议注册表：MVP 注册 `rtsp`；未知协议返回 `RTSP_UNSUPPORTED_PROTOCOL`。
- 验证：新增单测 `engine/tests/unit/test_probe_rtsp.cpp`：
  - 成功（收到首帧 + 元数据）；
  - 无视频 track；
  - 超时无首帧；
  - TCP 失败 → UDP 成功（回退）；
  - 两者失败；
  - 取消/超时整体（`RTSP_PROBE_CANCELLED`）；
  - 每条路径都验证 source 已 stop/释放（mock 计数）。

### A4. EngineServiceImpl::ProbeCamera
- `engine/src/core/ipc/uds_server.cpp`：实现 `ProbeCamera`，校验参数、调用 probe_rtsp、填充响应；RPC `code` 仅表示处理成功，测活结果放 `status/failure_code`。
- 凭据日志：排查并移除 ZLM `RtspPlayer.cpp` 中的 DebugL 凭据输出（若存在）。
- 验证：`make -C engine build`；`make -C engine test`；`make -C engine lint`；`make -C engine asan`（如常）。

## Phase B：Go 后端

### B1. 模型 + 迁移
- `app/internal/model/camera.go`：`Camera`（字段/JSON 见 design.md §3/§4），`TableName() = "cameras"`，含 BaseModel 毫秒软删除字段、`camera_id`、测活元数据列。
- `app/migrations/000011_add_cameras.up.sql / .down.sql`：建表 + 索引（见 design.md §3）。
- `app/migrations/000012_seed_resource_camera_menu.up.sql / .down.sql`：幂等菜单 + 按钮权限 + super 绑定（沿用 000009/000010 模式）。
- 验证：`make -C app migrate-up`；`make migrate-version` 显示最新；`psql` 检查表/菜单存在（或 `make test` 的 sqlite AutoMigrate 覆盖模型字段）。

### B2. Repository
- `app/internal/repository/camera.go`：`Create/Update/Delete/BatchDelete/GetByID/ListPage(filter{page,pageSize,name})`；软删 `deleted_at=0` 过滤；返回 `camera_id`。
- 单测 `camera_test.go`（sqlite 内存）：CRUD、软删后查询排除、批量删、分页 + name 模糊。

### B3. Service + 协议注册表
- `app/internal/service/camera.go`：`CameraService` 接口 + 实现：
  - URL 校验（rtsp scheme、host、长度、非法 escape/控制字符/fragment 拒绝）→ `CodeInvalidParam`；
  - 生成 `camera_id`（UUID）；
  - 计算/写 `config_hash`；
  - Probe 编排：读 DB → 指纹比对 → 调 engineipc → 按规则落库；
  - 错误映射（见 design.md §4.4）。
- `app/internal/service/camera_protocol.go`：`ProtocolRegistry`，注册 `rtsp` 适配器（URL 校验 + Probe 请求构造）。
- 单测 `camera_test.go` / `camera_probe_test.go`：URL 校验（合法/非法/特殊字符凭据/长度）、指纹一致性落库、指纹不一致 stale、无 id 不落库、失败不覆盖成功元数据。

### B4. engineipc 客户端 + 契约测试
- `app/internal/pkg/engineipc/client.go`：新增 `ProbeCamera` 包装（沿用 `call` 模式）。
- `app/internal/pkg/engineipc/fake_engine_test.go` 与 server/client 测试：fake 支持 `ProbeCamera`，覆盖成功/失败/transport error 映射。
- 验证：`make -C app test`（engineipc 包）。

### B5. API Handler + Router + Wire
- `app/internal/api/camera.go`：handlers（GetPage/Create/Update/Delete/BatchDelete/Probe）+ DTO + swagger 注释。
- `app/internal/router/router.go`：注册路由 + `PermMiddleware.Register`（`resource:camera` 页面权限在菜单层，接口按 `resource:camera:*` 注册）。
- `app/cmd/api/wire.go`：加入 repository/service/handler providers；`make wire` 重新生成。
- 操作日志：按产品决策，`OplogMiddleware` 记录完整 `rtspUrl`（不做额外脱敏）；如需避免日志体积过大，可对超长请求体保持现有 `Truncate` 行为。
- 单测 `camera_test.go`（gin + sqlite + fake engineipc）：全 API 覆盖 + 权限码 + 响应结构。
- 验证：`make -C app vet`；`make -C app test`。

## Phase C：前端

### C1. API 层
- `ui/apps/web-antd/src/api/core/camera.ts`：`CameraApi` 命名空间（Item/Query/ProbeResult 类型）+ CRUD/probe 函数。
- `ui/apps/web-antd/src/utils/rtsp.ts`（或并入 api）：`parseRtspUrl` / `buildRtspUrl`（含百分号编码与兼容拆分）。
- 验证：typecheck 通过。

### C2. 页面
- `ui/apps/web-antd/src/views/resource/camera/index.vue`：列表 + 表单弹窗 + 测活按钮 + 权限指令 + 状态展示。
- i18n：`locales/langs/{zh-CN,zh-TW,en-US}/resource/camera.json`（三语对齐）。
- 验证：`pnpm check`（circular + dep + typecheck + cspell）；`pnpm dev:antd` 手动走查。

## Phase D：跨层集成与质量门禁

### D1. 端到端
- 起 Go（`make -C app dev`）+ C++ engine（假流源或真实 RTSP）；通过 `POST /api/camera/probe` 验证：
  - 无 id 测活（临时）；
  - 已保存 id + 指纹一致 → 落库并可在列表看到元数据；
  - 指纹不一致 → stale；
  - 假流不可达 → `status=failed` + 稳定码。
- 验证：真实摄像头或本地 `test_real_media` 模式。

### D2. 一致性检查
- `bash algo-packages/scripts/sync-sdk.sh` / `check-consistency.sh`（若涉及 C ABI 变化则跑，本任务不涉及算法包，仅确认无回归）。
- `make -C app vet && make -C app test`；`make -C engine test && make -C engine lint`；`pnpm check`。

### D3. 操作日志复核
- 手动确认：操作日志可记录完整 rtsp URL（按产品决策，不脱敏）；自有代码不主动打印完整 URL 到 zap/C++ 日志；ZLM DebugL 凭据位置已排查确认。

## Phase E：收尾（Trellis）
- `trellis-check` 全量质量验证。
- `trellis-update-spec`：把「RTSP 测活契约」「前端 URL 解析/编码」「日志脱敏 URL」写入对应 spec。
- `task.py finish`；提交（conventional commit，如 `feat(camera): rtsp camera source management MVP`）。

## 已知待实现时确认项
- ZLM 凭据日志（`RtspPlayer.cpp` DebugL）具体位置与默认日志级别影响（按产品决策可保留，仅排查确认）。
- 前端“最后一个 `@`”拆分规则与路径含 `@` 误判时的提示交互（按 prd.md R1.7）。
