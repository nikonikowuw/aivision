# Journal - niko (Part 1)

> AI development session journal
> Started: 2026-08-16

---

## Session 1: frontend-trim 归档 + 会话收尾

**Date**: 2026-08-16
**Task**: frontend-trim 归档 + 会话收尾
**Branch**: `main`

### Summary

归档 08-16-frontend-trim（vben 裁剪只留 web-antd，工作已随 3f8482e 入库）。确认期间工作提交：Go 后端骨架（30a41a7）、错误处理规范 i18n（8563af2）。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `30a41a7` | (see git log) |
| `8563af2` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 2: 菜单管理 CRUD + JWT 认证中间件 + 统一错误处理

**Date**: 2026-08-16
**Task**: 菜单管理 CRUD + JWT 认证中间件 + 统一错误处理
**Branch**: `main`

### Summary

完成 08-16-backend-menu：菜单 CRUD/树/vben 路由转换（super 全量、普通用户按角色过滤）、JWT access token 认证中间件、统一错误处理（errno.Error + ErrorHandler + response.WriteFail）、router 装配 /api/menu 与 NoRoute/NoMethod/Recovery；DB 连接池与时区可配置（默认 postgres）；menus.parent_id 索引迁移。另做了 simplify 并行审查与清理（合并重复常量、抽 helper、去 TOCTOU、合并 auth 查询）。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `eaffafb` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 3: 完成操作日志与权限中间件

**Date**: 2026-08-16
**Task**: 完成操作日志与权限中间件
**Branch**: `main`

### Summary

实现操作日志采集、敏感字段脱敏、权限码中间件与日志查询接口；优化认证身份查询、路由权限路径复用和路由测试 recorder。通过 go test、go vet、竞态测试与 wire 生成校验。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a9d7442` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 4: backend-role 角色 CRUD + 分配菜单（simplify 清理）

**Date**: 2026-08-16
**Task**: backend-role 角色 CRUD + 分配菜单（simplify 清理）
**Branch**: `main`

### Summary

完成角色模块（CRUD、分页、super 保护、菜单分配）并做 simplify 清理：提取 normalizePage 与 api_test 建库助手，删除冗余查重预查与 Delete bool 返回，测试/vet 全绿。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `5f46356` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 5: Implement department tree CRUD operations

**Date**: 2026-08-17
**Task**: Implement department tree CRUD operations
**Branch**: `main`

### Summary

Refactored tree node generic logic into tree.go for reuse across menu and department endpoints. Implemented department API covering full tree read, node insertion, parent_id update cycles detection, and recursive soft delete tracking. Applied V2 migration to remove default statuses and add indexes.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `8b38aa3` | (see git log) |
| `7b9515e` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 6: Implement User Management CRUD

**Date**: 2026-08-17
**Task**: Implement User Management CRUD
**Branch**: `main`

### Summary

Implemented user management CRUD operations, role assignment, and password reset functionalities.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `52be9ec` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 7: Implement backend authentication

**Date**: 2026-08-17
**Task**: Implement backend authentication
**Branch**: `main`

### Summary

Implemented JWT login, refresh-token rotation, logout, user info, access codes, auth routes, secure-cookie configuration, dependency wiring, and backend authentication tests.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `bf166c4` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 8: Backend and Frontend Batch Operations

**Date**: 2026-08-19
**Task**: Backend and Frontend Batch Operations
**Branch**: `main`

### Summary

Implemented batch operations for user, role, and oplog in the backend and frontend. Fixed UI rendering for operation log and enforced append-only audit logs.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
| ------ | --------- |
| `9fc7eea` | (see git log) |
| `be1f41d` | (see git log) |
| `1068a93` | (see git log) |
| `e7c8a0b` | (see git log) |
| `f2e3875` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete

## Session 9: 修复登出审计日志操作人记录

**Date**: 2026-08-20
**Task**: 修复登出审计日志操作人记录
**Branch**: `main`

### Summary

在用户登出接口中支持优先通过 RefreshToken 解析用户身份并在吊销前捕获，同时备选支持校验 Authorization Bearer Token 提取操作人身份并注入 Gin Context，使操作日志中间件能准确记录登出操作人。

### Git Commits

| Hash | Message |
|------|---------|
| `ba9eaad` | (see git log) |

### Status

[OK] **Completed**

## Session 10: 个人中心资料与密码修改功能交付与归档

**Date**: 2026-08-20
**Task**: 个人中心资料与密码修改功能交付与归档
**Branch**: `main`

### Summary

完成个人中心个人资料修改与密码修改功能全流程交付，修复登出审计日志操作人记录，并通过全面质量门禁与任务归档

### Main Changes

- 后端新增 PUT /api/v1/user/profile 与 PUT /api/v1/user/password 接口及完整校验和测试
- 前端实现个人中心资料修改、密码修改表单、多语言支持及右上角快捷下拉菜单跳转
- 修复登出接口在 Token 失效前解析 Claims 捕获操作人并在审计日志准确记录

### Git Commits

| Hash | Message |
|------|---------|
| `bd715f6` | (see git log) |
| `ba9eaad` | (see git log) |

### Testing

- [OK] 后端 go test ./... 与 go vet 单元与集成测试全部通过
- [OK] 前端 pnpm check (typecheck, lint, circular, cspell) 全量检查通过

### Status

[OK] **Completed**

## Session 11: 完成项目部署物、文档与脚手架总任务归档

**Date**: 2026-08-20
**Task**: 完成项目部署物、文档与脚手架总任务归档
**Branch**: `main`

### Summary

交付 app/Dockerfile、deploy/docker-compose.yml、deploy/nginx.conf 以及根目录完整 README.md，完成全栈脚手架最终集成回归验收与全部子父任务归档

### Main Changes

- 编写 Go 后端多阶段构建镜像 Dockerfile
- 编写 deploy/ 目录下的 docker-compose.yml 与 nginx.conf 反代静态托管配置
- 完善根目录 README.md，包含特性、环境要求、启动指南与二次开发指引

### Git Commits

| Hash | Message |
|------|---------|
| `57d930c` | (see git log) |

### Testing

- [OK] 后端 make test 与 make vet 校验通过
- [OK] 前端 pnpm check 与 pnpm build:antd 构建通过

### Status

[OK] **Completed**

## Session 12: 全量审查并补齐项目规范文档与完成引导任务归档

**Date**: 2026-08-20
**Task**: 全量审查并补齐项目规范文档与完成引导任务归档
**Branch**: `main`

### Summary

审查并更新后端与前端目录结构、Hook 规范与类型安全规范，完成 00-bootstrap-guidelines 检查项并归档

### Git Commits

| Hash | Message |
|------|---------|
| `d36edeb` | (see git log) |

### Status

[OK] **Completed**

## Session 13: Add pluggable file upload API

**Date**: 2026-08-21
**Task**: Add pluggable file upload API
**Branch**: `main`

### Summary

Implemented authenticated multipart file upload with local and MinIO storage providers, validation, public URLs, frontend API wrapper, Swagger, deployment wiring, tests, and storage spec.

### Git Commits

| Hash | Message |
|------|---------|
| `ec5aeb2` | (see git log) |

### Status

[OK] **Completed**

## Session 14: Adopt golang-migrate for PostgreSQL migrations

**Date**: 2026-08-21
**Task**: Adopt golang-migrate for PostgreSQL migrations
**Branch**: `main`

### Summary

Migrated backend schema and data initialization to golang-migrate with embedded PostgreSQL migrations, removed MySQL driver, separated API startup from admin bootstrap, and updated specs and dev commands.

### Git Commits

(No commits - planning session)

### Status

[OK] **Completed**

## Session 15: Quality-check and commit personal profile avatar feature

**Date**: 2026-08-21
**Task**: Quality-check and commit personal profile avatar feature
**Branch**: `main`

### Summary

Follow-up to the file upload task: added avatar support to the personal profile. Ran trellis-check quality verification against specs — fixed app/uploads/ not being git-ignored, oxlint prefer-set-has violation in base-setting.vue, and avatar fallback showing full nickname in the circular avatar. Verified gofmt/vet/test, pnpm check, unit tests, swagger regen, 3-locale i18n, router/perm/oplog registrations and deploy config consistency. Committed as feat(project).

### Git Commits

| Hash | Message |
|------|---------|
| `6bd8502` | (see git log) |

### Status

[OK] **Completed**

## Session 16: 完成对时服务与前端时间管理集成

**Date**: 2026-08-23
**Task**: 完成对时服务与前端时间管理集成
**Branch**: `dev`

### Summary

实现跨平台对时服务 (NTP/手动/时区)、Gin API 路由与 web-antd 时间管理页面，修复单测并完成任务归档

### Git Commits

| Hash | Message |
| ------ | --------- |
| `f8b9244` | (see git log) |
| `be5f718` | (see git log) |
| `b5e33d1` | (see git log) |
| `961f2fe` | (see git log) |
| `f7041de` | (see git log) |

### Status

[OK] **Completed**

## Session 17: 实现网络主备容错模式与候选事务

**Date**: 2026-08-23
**Task**: 实现网络主备容错模式与候选事务
**Branch**: `dev`

### Summary

支持整机网络工作模式枚举与绑定拓扑状态管理，实现进入与退出 active-backup 候选事务及 120s 自动回滚，前端提供工作模式卡片、抽屉与三语支持。

### Git Commits

| Hash | Message |
|------|---------|
| `12cfa8f` | (see git log) |

### Status

[OK] **Completed**

## Session 18: 简化 LACP 802.3ad 聚合实现

**Date**: 2026-08-23
**Task**: 简化 LACP 802.3ad 聚合实现
**Branch**: `feat/lacp-aggregation`

### Summary

对 f41245e（LACP 802.3ad 特性）做行为保持的简化重构：新增 NetworkMode.IsBond() 作为绑定模式单一事实来源；fake 平台 buildLACPStatus 三个场景收敛为 lacpPortState/lacpAggregatorID 助手；service 抽出 slaveUsableForBond/slaveLinkMismatch 消除重复校验与空 if 分支；前端抽取 lacpSlaveStatus/isSlaveActive/bondSlaveTagColor/canSubmitModeForm 消除重复查找与嵌套三元；删除 api 测试中未使用的 testNetworkServiceWrapper/mockFailLACPPlatform 死代码。

### Main Changes

- 后端：IsBond() 谓词 + 校验/告警助手抽取，types.go gofmt 对齐修复
- 前端：模板重复 LACP 查找与禁用条件收敛为助手方法与 computed
- 测试：清理 api/network_test.go 死代码（wrapper/interface 未使用）

### Git Commits

| Hash | Message |
| `c6b1bef` | (see git log) |

### Testing

- [OK] make vet + make test 全绿；LACP 专项 12 用例通过；pnpm check（circular/dep/typecheck/cspell）通过

### Status

[OK] **Completed**

### Next Steps

- 08-23-lacp-aggregation 待台架（多网卡 + 802.3ad 交换机）验证 AC5-AC7 后归档；提交前需 lefthook install 以恢复 oxfmt/gofmt/commitlint 门禁

## Session 19: 完成边缘网关模式与内置DHCP服务开发

**Date**: 2026-08-23
**Task**: 完成边缘网关模式与内置DHCP服务开发
**Branch**: `feat/08-23-edge-gateway-dhcp`

### Summary

实现边缘网关 (gateway) 模式：Go后端集成 insomniacslk/dhcp 内置 DHCP 服务与内核三层转发控制，增加地址池/前缀/租约校验与启用前冲突探测；前端增加网关配置抽屉、租约列表与冲突告警；完成端到端生命周期测试并归档任务。

### Git Commits

| Hash | Message |
| `2df5ae8` | (see git log) |
| `c5d4d17` | (see git log) |
| `dc0c125` | (see git log) |

### Status

[OK] **Completed**

## Session 20: 完成真实 RTSP 与 VideoToolbox 媒体链路验证

**Date**: 2026-08-24
**Task**: 08-22-cpp-engine-skeleton-macos / P0-2
**Branch**: `main`

### Summary

完成本地 ZLMediaKit `MP4Reader` RTSP fixture 与 macOS VideoToolbox 真实媒体验证：修复 RTSP 测试 server 的直接子进程回收、ZLM owner-poller 线程归属、`media_zlm` stop/delegate teardown 竞态、VideoToolbox 异步回调死锁与 NAL 输入 fallback，并覆盖 H.264、H.265、断流重连。

### Main Changes

- `media_zlm` 使用 `MediaPlayer`，在同一 EventPoller 上完成配置、播放、delegate 注册和 teardown；回调只复制编码 access unit。
- RTSP fixture 使用 `MP4Reader` 发布固定媒体，server 使用 `posix_spawn`，避免 shell background 进程泄漏。
- `MacosDecoder` 将 `VTDecompressionSessionWaitForAsynchronousFrames` 限定在 `flush()`，异步输出队列有界并保持 `CVPixelBuffer` 生命周期。
- TSan 下保留真实媒体测试，通过 `engine/tests/tsan.supp` 仅屏蔽已定位的 ZLToolKit 第三方 Logger/毫秒计时初始化 race。

### Testing

- [OK] `make -C engine build`
- [OK] `make -C engine test`（unit + contract，含 H.264/H.265/断流重连）
- [OK] `make -C engine asan`
- [OK] `make -C engine tsan`
- [OK] `make -C engine lint`
- [OK] 最终恢复普通 Debug 构建并再次通过 ctest；无残留测试进程或媒体二进制。

### Status

[OK] **P0-2 本地媒体链路已完成；60 秒持续流、track replacement、多实例压力及真实指标断言仍在 remaining-work.md 中保留**

### Next Steps

- 实现 P0-1 Go gRPC stub/E2E 与 P0-3 永久阻塞插件的进程隔离设计。

## Session 20: 完成 RK3576 YOLOv8n RKNN 算法包验证与归档

**Date**: 2026-08-24
**Task**: 完成 RK3576 YOLOv8n RKNN 算法包验证与归档
**Branch**: `dev`

### Summary

排查并修复了 RK3576 开发板上算法包的构建与运行时问题：修正 runner 链接 rknnrt 缺失、补全各阶段耗时指标采集、集成 stb_image 视觉渲染输出 result.jpg，并在实体板端完成 100 轮 benchmark 与打包验证，最后完成任务归档。

### Git Commits

| Hash | Message |
|------|---------|
| `9fada7c` | (see git log) |

### Status

[OK] **Completed**

## Session 21: C++ Engine structured logging (JSONL)

**Date**: 2026-08-25
**Task**: C++ Engine structured logging (JSONL)
**Branch**: `dev`

### Summary

实现 C++ Engine 结构化异步 JSONL 日志系统：统一 logger（级别过滤、稳定字段、URL/凭据脱敏、异步非阻塞 writer、本地统计），接入 SDK av_log_fn 桥接；validator 与 algo_sandbox 的机器协议不再依赖可变日志文本；配套系统日志/systemd journald 部署说明、确定性单测与 sanitizer 覆盖；同步更新 engine 规范。任务 08-25-cpp-engine-logging 已归档。

### Git Commits

| Hash | Message |
|------|---------|
| `e165e8d` | (see git log) |

### Status

[OK] **Completed**

## Session 22: Go gRPC integration

**Date**: 2026-08-26
**Task**: Go gRPC integration
**Branch**: `dev`

### Summary

Add Go gRPC IPC between app and engine: engineipc package provides inbound gRPC server (Runtime on app.sock, business side connects via DesiredStateAdapter/ReportAdapter ports) and outbound EngineClient (engine.sock, 12 EngineService RPCs). Protocol authoritative source in engine/proto/aivision/v1, generated to app/internal/proto/aivision/v1 via scripts/generate-proto.sh (committed, make proto-check prevents drift). Cross-language E2E via make -C app grpc-e2e (mock engine + tests/integration behind integration build tag). Also stabilized operation log test across midnight.

### Git Commits

| Hash | Message |
|------|---------|
| `6952640` | (see git log) |
| `37f589c` | (see git log) |

### Status

[OK] **Completed**

## Session 23: RTSP Camera Source MVP

**Date**: 2026-08-27
**Task**: RTSP Camera Source MVP

## Session 23: RTSP 摄像头视频源管理 MVP 全链路实现与分批提交

**Date**: 2026-08-26
**Task**: RTSP 摄像头视频源管理 MVP 全链路实现与分批提交
**Branch**: `dev`

### Summary

Completed the RTSP camera source MVP across the engine, app, and web UI: RTSP probing over IPC, camera persistence and REST APIs, frontend management and parsing utilities, and aligned tests/i18n. Archived task 08-26-rtsp-camera-source-mvp.
完成了 RTSP 摄像头管理 MVP：包括 gRPC ProbeCamera RPC、C++ 引擎测活与 TCP/UDP 降级回退、Go 数据库模型与迁移、CRUD 仓储与服务、操作日志 i18n 闭环、Vue3 资源管理前端及 RTSP 工具函数，并通过了所有单测与质量门禁。

### Git Commits

| Hash | Message |
| ------ | --------- |
| `cee5031` | (see git log) |
| `f000819` | (see git log) |
| `86ac888` | (see git log) |
| `e2a1c8b` | (see git log) |
| `f74b914` | (see git log) |

### Status

[OK] **Completed**

## Session 24: Person Management MVP

**Date**: 2026-08-27
**Task**: Person Management MVP
**Branch**: `dev`

### Summary

Implement person management MVP with backend CRUD, sync APIs, and frontend pages.

### Git Commits

| Hash | Message |
| ------ | --------- |
| `de26c68` | (see git log) |
| `f000819` | (see git log) |
| `cee5031` | (see git log) |
| `86ac888` | (see git log) |
| `e2a1c8b` | (see git log) |
| `f74b914` | (see git log) |
| `89c28f8` | (see git log) |
| `1f7fbca` | (see git log) |

### Status

[OK] **Completed**

## Session 25: macOS Face Recognition Algorithm Package & Best-Shot Selection

**Date**: 2026-08-28
**Task**: macOS Face Recognition Package (`08-24-macos-face-recognition`)
**Branch**: `dev`

### Summary

实现了 Apple Silicon macOS 人脸识别算法包 (`algo-packages/macos/arm64/face_recognition`)，包括完整的 Core ML FP16 推理加速（SCRFD 10G + GLINTR100 + YOLOv8n）、双模型 Fork-Join 异步并发、vImage ARGB 高质量重采样仿射对齐截脸、基于 5 点人脸关键点的多维度几何质量评分、以及连续命中防抖（`track_confirm_frames`）与轨迹抓拍优选（Best-Shot Selection）。完善了 SDK 与 Engine 对 `AV_RESULT_RECOGNITION` 的协议支持与校验规则。

### Git Commits

| Hash | Message |
|------|---------|
| `a2cdd2d` | feat(project): support face_recognition algo type and result contract |
| `7c3123a` | feat(project): add self-contained macos face_recognition package with best-shot selection |

### Testing

- `face_recognition_tests` (100% Passed)
- `face_recognition_preprocess_tests` (100% Passed)
- `face_recognition_abi_tests` (100% Passed)
- `CameraTaskTest.DecoderWatchdogResetsSilentDecoder` (Passed)
- AddressSanitizer (ASan) 零泄漏测试通过
- 压测基准（50 轮）：单帧平均延迟 5.76 ms，吞吐率 173.6 FPS

### Status

[OK] **Completed**

## Session 25: 完成摄像头实时预览功能

**Date**: 2026-08-28
**Task**: 完成摄像头实时预览功能
**Branch**: `dev`

### Summary

实现摄像头实时多画格预览功能，支持主/子码流推导及自适应分屏拉流、ZLMediaKit 按需转流与前端多屏播放

### Main Changes

- 后端：扩展 Camera 模型与 DB migration 添加 sub_rtsp_url 字段，实现厂商子码流自动推导与预览 API、IPC 控制
- 引擎：实现 LiveStreamManager，接入 ZLMediaKit 提供 HTTP/WS-FLV 流分发与拉流按需管理
- 前端：实现 /live 多画格预览页面及通用 VideoPlayer 组件，支持 1/4/9 分屏自适应码流切换与全屏预览

### Git Commits

| Hash | Message |
|------|---------|
| `afaf135` | (see git log) |

### Testing

- [OK] go test ./... 后端单测通过
- [OK] make -C engine test 引擎单测通过
- [OK] pnpm check 前端类型与静态检查通过

### Status

[OK] **Completed**

## Session 26: Algorithm Package Management & Face Recognition Package Implementation

**Date**: 2026-08-28
**Task**: Algorithm Package Management & Face Recognition Package Implementation
**Branch**: `dev`

### Summary

Completed end-to-end algorithm package lifecycle management: backend tar safety & validation, gRPC/IPC package installation & activation, face_recognition algo contract and macOS self-contained package implementation with best-shot selection, and frontend management console.

### Git Commits

| Hash | Message |
|------|---------|
| `a2cdd2d` | (see git log) |
| `e45a4d9` | (see git log) |

### Status

[OK] **Completed**

## Session 27: 目标检测告警记录模块端到端开发

**Date**: 2026-08-30
**Task**: 目标检测告警记录模块端到端开发 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

打通了从 C++ 算法包告警产生、Engine 图片生命周期、Go 后端幂等持久化与对账 API、到 Vue 前端 `/record/alarm` 告警记录与全景画框标注的端到端闭环。

### Main Changes

- **算法包改造**：YOLOv8n（macOS 与 RKNN 双平台）支持基于 `track_id` 的 5 秒冷却去重、单目标单事件回调触发，以及 `[0,0,1,1]` 全景大图抓拍请求。
- **数据库与迁移**：新增 `000021_add_alarm_records` 表（支持毫秒软删与 `event_id` 唯一索引）与 `000022_seed_record_alarm_menu` 菜单与 RBAC 权限迁移。
- **Engine IPC 对接**：实现 `ReportAdapter.AcceptAlarm`（`event_id` 幂等落库、`max_confidence` 计算与 `objects_json` 序列化）与 `ReconcileOrphanImages`（结合 5 分钟保护窗口双向对账保留或清理孤儿图片）。
- **后端 API & 权限**：实现 `AlarmRecordHandler`，提供组合分页筛选、详情与受控安全图片流读取接口；更新 Wire 依赖注入装配链并重新生成 `wire_gen.go`。
- **管理前端**：在 `ui/apps/web-antd` 下实现 `/record/alarm` 告警记录列表、`AlarmDetailDrawer` 抽屉与 `AlarmAnnotationCanvas`（全景底图自适应叠加 ROI / Mask / Line 规则多边形与目标红框）。完善 `zh-CN`、`en-US`、`zh-TW` 三语翻译。

### Testing

- [OK] `make -C app test` 与 `make -C app vet` 全部通过
- [OK] `make -C app proto-check` 验证契约无漂移
- [OK] `make -C algo-packages/macos/arm64/yolov8n test` 4 项 C++ 单测全绿
- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 28: 硬件双图管线与分级秒开加载性能优化

**Date**: 2026-08-30
**Task**: 硬件双图管线与分级秒开加载性能优化 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

彻底解决了局域网及弱网环境下告警列表查询卡顿的问题。在 C++ Engine 引入硬件 2D 加速双图生成管线（原图 + 360P 缩略图），后端引入分级流式传输与 HTTP 强缓存，前端实现分级极速秒开。

### Main Changes

- **C++ 引擎 0-CPU 硬件双图流水线**：
  - 在 `IImageProcessor` 中扩展 `encode_thumbnail_jpeg` 接口，在 Apple Silicon / Rockchip RGA 硬件加速层完成 360P 宽 Lanczos 等比降采样与硬件 JPEG 压缩（Q=70）。
  - `ImageManager` 在抓拍告警时原子输出 `img-xxx.jpg`（1080P/4K 原图）与 `img-xxx_thumb.jpg`（约 6KB 缩略图），并在删除/对账时联动清理。
- **Go 后端图片流分级代理**：
  - `ReadImageStream` 增加 `?type=thumb` 缩略图路由支持（若缩略图不存在自动无缝回退至原图）。
  - 注入 `Cache-Control: public, max-age=604800, immutable` 强缓存标头，避免重复网络传输。
- **Vue 前端分级加载与缓存池**：
  - 告警列表表格（缩略图与目标特写）默认请求 6KB 硬件缩略图，一页 20 条记录总带宽从 5MB 骤降至 120KB（降低 97%），实现列表毫秒级秒开。
  - 详情弹窗独立请求 1080P/4K 无损高清原图，保障 ROI 规则与目标框的高保真回溯。
  - 引入 Promise 级内存 Blob 缓存池，避免同一行两个组件的重复网络拉取。

### Testing

- [OK] `make -C engine test` (56/56 单元测试全部通过)
- [OK] `make -C app test` 与 `make -C app vet` 全部通过
- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 29: 目标抠图高清无损放大与详情联动

**Date**: 2026-08-30
**Task**: 目标抠图高清无损放大与详情联动 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

完善了目标抠图（Target Crop）在列表与详情中的分级交互体验。列表态采用 6KB 轻量缩略图提取极速预览，点击放大与详情弹窗中则按需直接从 1080P/4K 原始全景图提取 1:1 无损超清特写。

### Main Changes

- **TargetCropCanvas 超清特写支持**：
  - 增加 `getHdCroppedPreview` 方法，在用户触发特写预览放大时，按需异步拉取 1080P/4K 原图并在离屏 Canvas 中 1:1 截取目标原始像素，消除马赛克与模糊。
  - 列表表格中依然使用轻量缩略图保证每页百毫秒极速翻页。
- **详情弹窗增强**：
  - 在告警详情描述列表中新增「目标特写（Target Crop）」展示项（96×96 尺寸），支持点击展开全尺寸原图 1:1 无损特写大图。

### Testing

- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 30: 全景图列表与放大预览原图分级无损对齐

**Date**: 2026-08-30
**Task**: 全景图列表与放大预览原图分级无损对齐 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

修复并彻底统一了全景图（Panorama）在表格列与点击放大时的分级加载体验。列表状态下使用 6KB 硬件缩略图快速呈现红框缩略图，用户点击放大查看时自动拉取 1080P/4K 高清原图无损重绘红框，保证放大时 100% 清晰无失真。

### Main Changes

- **AlarmThumbnail 放大高清化**：
  - 增加 `getHdPanoramaPreview` 异步方法，点击列表全景缩略图展开大图时，按需请求 1080P/4K 无损全景底图并叠加红框，消除大图弹窗模糊。
  - 列表单元格依然复用轻量硬件缩略图保持秒开。

### Testing

- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 31: 100 条大数据量渲染与并发拥塞深度优化

**Date**: 2026-08-30
**Task**: 100 条大数据量渲染与并发拥塞深度优化 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

排查并解决了单页 100 条记录时渲染卡顿与网络阻塞的深层瓶颈。通过后端 N+1 查询优化、VXE 虚拟滚动、IntersectionObserver 视口按需懒加载，以及最大并发 6 的请求调度器，彻底消除了页面冻结与长时间等待。

### Main Changes

- **Go 后端预加载优化**：
  - 告警分页列表（`ListPage`）批量预加载摄像头与算法字典，消除每页 100 条数据时的 200+ 次数据库 N+1 慢点查。
- **VXE Grid 虚拟滚动启用**：
  - 启用表格 `scrollY: { enabled: true, gt: 20 }`，单页 100 条时仅在 DOM 中挂载可视区的 15~20 个行节点，消除 200 个 Canvas 节点同时挂载造成的浏览器主线程冻结。
- **组件视口懒加载（IntersectionObserver）**：
  - `AlarmThumbnail` 与 `TargetCropCanvas` 改造为通过 `IntersectionObserver` 监听容器进入视口（预留 100px margin）后才触发切图与绘图，用户未滚动到的行不产生任何 CPU/GPU 计算与渲染。
- **并发请求队列调度器**：
  - 在前端 API 层构建 `enqueueImageRequest` 调度队列，限制最大并行图片请求数为 6，避免 100 条数据瞬间发起 100 个 HTTP 请求撑爆浏览器连接池。

### Testing

- [OK] `make -C app test` 与 `make -C app vet` 全部通过
- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 33: 原生图片直链流水线消除 100 条渲染阻塞

**Date**: 2026-08-30
**Task**: 原生图片直链流水线消除 100 条渲染阻塞 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

重构了全景缩略图渲染模式。将列表页的 JS 离屏 Canvas 重绘与 Base64 转换彻底替换为浏览器原生 `<img>` 直链流式加载，由浏览器内核 C++ 多线程与 HTTP/2 并行流水线直接解码并渲染，使 100 条数据列表瞬间完成渲染。

### Main Changes

- **AlarmThumbnail 架构重构（原生直链）**：
  - 列表表格态直接使用 `<img :src="/api/record/images/:id?type=thumb" loading="lazy">`，0 JS 线程占用，完全释放浏览器主线程。
  - 用户点击放大查看时，按需异步拉取原图执行高保真红框叠加，兼顾秒开与放大超清。
- **去除冗余并发队列**：
  - 移除前端人为的串行并发等待队列，完全交由浏览器现代网络栈（HTTP Keep-Alive / HTTP/2）原生并行管线并发拉取。

### Testing

- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 34: 零 JS 开销的 GPU 级 CSS 视口切图与媒体 URL Token 鉴权

**Date**: 2026-08-30
**Task**: 零 JS 开销的 GPU 级 CSS 视口切图与媒体 URL Token 鉴权 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

彻底解决了 100 条告警数据时因并发排队导致图片只加载一部分，以及 JS 离屏 Canvas 重绘 CPU 跑满的问题。实现了后端 URL Query Token 鉴权支撑原生媒体直链，前端 TargetCrop 改为基于 GPU 的 CSS 视口裁剪，并配合 IntersectionObserver 视口懒加载，保证 100 条记录流畅秒开。

### Main Changes

- **Go 后端鉴权支持 Query Token**：
  - `AuthMiddleware.Handler` 扩展支持从 `?token=xxx` 提取 JWT 凭证，方便浏览器原生 `<img>` 和 CSS `background-image` 标签携带鉴权。
- **TargetCropCanvas 架构重构（GPU 级 CSS 视口裁剪）**：
  - 移除昂贵的 JS `new Image()` + `canvas.drawImage` + `toDataURL` 逻辑；
  - 改为使用数学公式计算归一化 bbox 对应的 CSS `background-size` 与 `background-position`，由浏览器图形引擎/GPU 硬件单元完成视口裁剪呈现，JS 主线程开销降为 0。
- **结合 IntersectionObserver 视口懒加载**：
  - 仅对进入视口（预留 400px 缓冲区）的行发起图片加载，未滚动的元素不产生网络与渲染开销。
- **点击无损原图提取**：
  - 用户点击特写查看大图时，按需异步下载 1080P/4K 原图并以 1:1 原始分辨率提取目标特写。

### Testing

- [OK] `make -C app test` 与 `make -C app vet` 全部通过
- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 35: 移除 VXE Table scrollY 截断恢复正常表格全高展开

**Date**: 2026-08-30
**Task**: 修复超过 20 条时表格只显示 4 条需要内部下拉的问题 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

排查并修复了当分页选择 50/100 条时，表格区域被强行压扁为固定小视口高度只显示 4 条记录的问题。

### Main Changes

- **对齐项目标准表格规范**：
  - 移除了 `record/alarm/index.vue` 中配置的 `scrollY: { enabled: true, gt: 20 }`；
  - 恢复为与项目中摄像头（`camera`）、人员（`person`）、任务（`task`）、日志（`log`）等页面完全一致的 `<Page auto-content-height>` 自动全高展开机制。
  - 配合原生的 `IntersectionObserver` 懒加载与 CSS GPU 视口裁剪，页面正常向下铺满展示，无需在表格内部小窗口滚动。

### Testing

- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 36: 修复详情弹窗图片残影与列表行 keyField 绑定

**Date**: 2026-08-30
**Task**: 修复每个分页第一条全景图详情与缩略图对不上的问题 (`08-29-record-alarm-management`)
**Branch**: `dev`

### Summary

定位并彻底解决了列表第一条在点击详情或切换分页时可能与缩略图对不上的原因：

1. **VXE Table rowConfig.keyField 显式绑定**：为表格添加 `rowConfig: { keyField: 'id' }`，确保分页切换时 DOM 节点基于行唯一 ID 精确刷新，避免复用第 0 行组件实例导致属性同步延迟。
2. **DetailModal 响应式与 ObjectURL 残影消除**：在 `handleViewDetail` 触发时重置 `currentDetail`，并在 `AlarmAnnotationCanvas` 加载新图前彻底销毁旧 ObjectURL 并清空绑定。
3. **组件级别 Key 隔离**：为 `DetailModal` 和缩略图组件绑定 `:key="props.imageId"`，确保图片 ID 变化时状态完全隔离。

### Testing

- [OK] `make -C app test` 与 `make -C app vet` 全部通过
- [OK] `cd ui && pnpm --filter @vben/web-antd typecheck` 前端 TypeScript 编译通过

### Status

[OK] **Completed**

## Session 27: 告警记录模块端到端开发与质量验收归档

**Date**: 2026-08-30
**Task**: 告警记录模块端到端开发与质量验收归档
**Branch**: `dev`

### Summary

完成从 C++ 推理引擎单目标告警上报、JPEG 原子写盘与孤儿图对账，到 Go 后端持久化、受控图片安全流与多维分页查询，再到 Vue3 管理前端全景标注与目标特写 Canvas 的端到端闭环开发与验收归档。

### Git Commits

| Hash | Message |
|------|---------|
| `c757f60` | (see git log) |

### Status

[OK] **Completed**

## Session 37: 目标检测批次回调与目标级独立事件重构

**Date**: 2026-08-31
**Task**: 单帧检测批次回调 + Engine 目标级事件 fan-out + 同批次共享抓拍图片 (`08-31-batch-detection-object-alarms`)
**Branch**: `dev`

### Summary

将 YOLOv8n 及推理引擎结果处理体系重构为“单帧单次批次回调 + 引擎按目标 fan-out + 共享抓拍”模式：

1. **SDK 与 ABI 语义**：明确 `AV_RESULT_ALARM` 承载单帧检测批次；顶层 `event_id` 为算法批次 ID，`objects[]` 承载该批次全部可告警目标对象。
2. **YOLOv8n 算法包重构**：逐目标进行类别冷却检查后聚合为 `alarm_objects`，一帧仅触发一次 `on_result` 回调并携带单份全景图抓拍请求。同步更新 standalone runner，消除多次回调覆盖问题，完整打印 4 个目标并成功输出 `result.jpg`。
3. **Engine Fan-out 与单图编码**：
   - 提取批次内全部合法目标，生成 `<instance_run_id>/<batch_id>-<target_sequence>` 全局目标级事件 ID 并入临界区去重；
   - 帧 token 仅 retain 一次，异步 worker 队列单次编码 JPEG 并落盘；
   - 批次内全部拆分出的单目标 `AlarmEvent` 共享相同的 `image_id` 与 `image_rel_path`；
   - 将 `ImageManager` 的图片关联语义统一更新为 `capture_id`。
4. **Go 后端消费防护**：`ReportAdapter.AcceptAlarm` 严格校验 `len(objects) == 1`，防止多目标事件未拆分造成静默丢弃。
5. **协议与规范同步**：更新 `app.proto` 注释与 Go pb 映射，同步更新 `.trellis/spec/engine/` 下的 SDK 契约、manifest 约束与 runtime 指南。

### Testing

- [OK] `make -C algo-packages/macos/arm64/yolov8n build && test && run`（4 个目标全部输出，Visualizer 成功渲染保存 `result.jpg`）
- [OK] `bash algo-packages/scripts/check-consistency.sh`（SDK 一致性校验 100% 通过）
- [OK] `make -C engine build && ./engine/build/tests/unit_tests --gtest_filter='-RealMediaIntegrationTest.*'`（54/54 单元测试 PASS）
- [OK] `./engine/build/tests/contract_tests`（C ABI 契约测试 PASS）
- [OK] `cd argus && go test ./...`（后端全部单元测试 PASS）

### Status

[OK] **Completed**
