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
|------|---------|
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
|------|---------|
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
|------|---------|
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
|------|---------|
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
