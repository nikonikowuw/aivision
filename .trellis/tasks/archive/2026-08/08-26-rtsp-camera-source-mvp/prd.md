# RTSP 摄像头视频源管理 MVP

## Goal

为边缘 AI 视频分析平台提供可管理、可验证、可被后续任务引用的 RTSP 摄像头资源。MVP 只实现 RTSP 手工配置和按需测活，不实现 ONVIF 发现、常驻拉流、任务编排或浏览器预览。

成功标准是：管理员可以在 `/resource/camera` 完成摄像头配置的新增、编辑、列表查询、软删除，并对保存前或保存后的 RTSP 配置执行按需测活；测活由 C++ 引擎使用与正式媒体链路相同的 ZLMediaKit 适配层完成，满足 RTSP `PLAY` 且收到首个有效编码视频帧后才判定成功。

## Background And Evidence

- 产品依据：`docs/prd/ai-video-analytics-edge-platform-v1.0.md` 第 7.2 节和第 11.1 节，V1.0 需要手工 RTSP 摄像头接入、H.264/H.265 输入、媒体源复用和后续任务引用。
- 当前仓库尚无摄像头 Go 模型、CRUD API 或业务页面。
- C++ 已有抽象媒体接口 `engine/include/aivision/media/media_api.hpp` 和 ZLMediaKit 实现 `engine/src/media/zlm/zlm_source.cpp`；当前实现固定使用 TCP，测活需要扩展为临时源、首帧通知和 TCP/UDP 回退。
- Go/C++ 权威 Protobuf 位于 `engine/proto/aivision/v1/`，Go 生成代码位于 `app/internal/proto/aivision/v1/`，生成入口为 `app/scripts/generate-proto.sh`。
- HTTP 路由统一注册在 `app/internal/router/router.go`；前端业务 API 位于 `ui/apps/web-antd/src/api/core/`，页面位于 `ui/apps/web-antd/src/views/`。
- 现有项目使用 `api -> service -> repository -> model`、版本化 PostgreSQL migration、`BaseModel` 毫秒软删除和统一 `{code,data,message}` 响应。

## Confirmed Requirements

### R1. 摄像头领域模型

1. 领域实体名称为 Camera，数据库表为 `cameras`。
2. 数据库内部主键为自增 `uint64 id`，REST API 路径沿用现有 CRUD 风格使用数值 `id`。
3. 额外生成不可变 UUID 字符串 `camera_id`；响应同时返回 `id` 和 `cameraId`。`camera_id` 用于 Go/C++ IPC、后续任务和事件关联，永不复用，用户不可编辑。
4. 使用现有 `BaseModel` 软删除语义：`deleted_at=0` 表示有效，删除后正常查询排除记录。
5. 持久化可编辑字段固定为：
   - `name`：摄像头名称，必填；名称不要求唯一；
   - `rtsp_url`：唯一持久化的完整 RTSP URL，可包含经过百分号编码的明文 userinfo；数据库不拆分用户名和密码列；
   - `remark`：可选备注。
6. 前端表单维护“无凭据 RTSP 地址、用户名、密码”三个临时输入值，并支持粘贴完整 RTSP URL 后自动拆分；提交前由前端编码用户名/密码并拼接为唯一的完整 `rtspUrl`。这些临时字段不进入 API 持久化 DTO。
7. 完整 URL 粘贴采用“自动解析 + 可见校正”，解析规则使用确定性边界：
   - 以最后一个 `@` 作为 userinfo 与 host 的分割线（与常见 RTSP 客户端一致）；userinfo 内按第一个 `:` 拆用户名/密码（密码可含 `:`），无 `@` 则整体为无凭据地址；
   - 因此密码中的 `@`、`/`、`#`、`?` 等未编码字符只要位于最后一个 `@` 之前就不会破坏拆分；
   - 拆出的地址、用户名和密码始终展示为可编辑表单字段，用户可以检查或修正；
   - 仅当拆分结果可疑（如 host 侧无法解析）或路径含 `@` 导致误判时，前端提示用户用“地址/用户名/密码”字段直接输入，不静默提交；
   - 编辑已保存记录时，前端反向解析 URL、解码 userinfo 并填充临时字段；
   - 页面可展示最终将提交的完整 RTSP URL，后端和数据库仍只接收、保存该单一值。
8. `protocol` 为内部字段，当前固定为 `rtsp`；前端不可编辑。协议相关校验、测活和运行能力通过注册/适配抽象选择，后续新增协议不修改核心 CRUD 流程。
9. 传输策略为内部 `transport_policy=auto`，前端不提供 TCP/UDP 选择。TCP 优先，失败后回退 UDP。
10. 不增加源级 `enabled` 状态。实际拉流、断线重连和启停归后续 Camera Task 层。
11. 摄像头列表和详情 API 返回完整明文 `rtsp_url`，访问受现有认证和权限控制；不做加密或额外密钥管理。

### R2. 配置 CRUD

1. 提供摄像头分页列表、创建、更新、单条软删除和批量软删除接口：
   - `GET /api/camera/page`
   - `POST /api/camera`
   - `PUT /api/camera/:id`
   - `DELETE /api/camera/:id`
   - `DELETE /api/camera/batch`（请求体 `{ids:[...]}`，沿用现有用户/角色批量删除模式，权限仍用 `resource:camera:delete`）
2. 字段长度上限冻结为：`name` 128 字符（必填）、`remark` 255 字符（可选）、最终 `rtsp_url` 2048 字符。前端临时用户名/密码不单独限制，受最终 URL 上限约束。
2. 保存只做字段和 RTSP URL 结构校验，不强制测活成功。URL 输入与保存规则为：
   - 用户不需要理解百分号编码；前端负责将独立用户名/密码编码并拼入完整 URL；
   - 编辑既有记录时，前端解析完整 URL，解码 userinfo 到临时用户名/密码字段，并在提交时重新生成完整 URL；
   - 后端接收的仍是单一 `rtspUrl`，去除首尾空白后保存，最大长度 2048；
   - scheme 仅允许大小写不敏感的 `rtsp`，且必须有合法 host；允许域名、IPv4、带方括号 IPv6、可选 `1~65535` 端口、路径和查询参数；
   - 百分号编码必须是合法的两个十六进制数字；拒绝控制字符和 fragment；
   - 后端不再次改写已规范化 URL，避免改变密码、主机、路径或查询参数语义；配置指纹使用去首尾空白后的持久化 URL 原文。
3. 保存配置不信任前端携带的测活结果；配置发生变化后，旧测活结果不能被视为当前配置的有效结果。
4. 后续有任务引用时，删除行为必须能扩展为引用保护；MVP 当前暂无任务表，不实现任务引用检查。
5. 写操作接入现有操作日志；按产品决策，操作日志可记录完整 RTSP URL（含 userinfo 凭据），不做额外脱敏。管理页面可以直接查看完整 URL。
6. 单条删除和批量删除均为软删除，`camera_id` 永不复用。

### R3. 按需 RTSP 测活

1. 提供单一入口 `POST /api/camera/probe`。请求允许带可选数值 `id`、`protocol` 和当前表单的完整 `rtspUrl`。
2. 无 `id` 时测活未保存表单，只返回结果，不写数据库。
3. 带 `id` 且提交配置与数据库当前配置指纹一致时，测活结果才更新该摄像头；配置不一致时只返回临时结果，不覆盖旧测活信息。
4. 测活由 Go 通过现有 `engine.sock` 调用 C++ `EngineService.ProbeCamera`，不在 Go 引入第二套 RTSP 客户端。
5. C++ 使用协议注册表选择 RTSP 适配器和 ZLMediaKit 临时媒体源。成功标准为：
   - RTSP `PLAY` 完成；
   - 找到视频 Track；
   - 收到首个有效编码视频帧/视频包。
6. 测活阶段不要求硬件解码、像素转换或 JPEG 编码；在可获取时返回编码、宽度、高度和帧率。
7. 自动传输策略为每次 TCP 先试，最多 5 秒；失败后彻底释放临时源，再以 UDP 试最多 5 秒；总请求约 10 秒。收到首帧立即结束当前尝试。
8. 无论成功、失败、超时、取消或 IPC 返回异常，临时媒体源必须释放，不得留下两个上游连接。
9. 测活失败是结构化业务结果而不是 HTTP 业务错误：统一响应 `code=0`，数据包含 `status=failed`、稳定 `failureCode`、诊断信息、每次尝试和耗时。参数非法、摄像头不存在、IPC 不可用或服务内部错误才使用统一错误码。
10. 稳定失败码至少包括：`RTSP_CONNECT_FAILED`、`RTSP_AUTH_FAILED`、`RTSP_PLAY_TIMEOUT`、`RTSP_NO_VIDEO_TRACK`、`RTSP_NO_FIRST_FRAME`、`RTSP_MEDIA_ERROR`、`RTSP_UNSUPPORTED_PROTOCOL`、`RTSP_PROBE_CANCELLED`。前端只依赖机器码并通过 i18n 显示文案；ZLMediaKit 原始错误只用于内部诊断。
11. 测活结果分为最近一次尝试和最后一次成功：保留最近状态、时间、失败码/信息；独立保留最后成功时间、实际成功传输方式、codec、width、height、fps。
12. 测活元数据绑定配置指纹，至少由 `protocol + canonical_url + transport_policy` 组成。测活完成后只有当前配置仍匹配发起时指纹才落库；否则返回 `persisted=false`、`stale=true`。

### R4. HTTP/IPC 契约

1. 在现有 `engine.proto` 的 `EngineService` 中新增 `ProbeCamera` RPC，不新建独立 gRPC 服务。
2. RPC 请求不要求 `camera_id`，以支持未保存表单测活；请求包含 `protocol` 和完整 URL。
3. RPC 响应区分 RPC 处理状态和测活结果；RPC `code` 只表示参数/引擎处理是否成功，测活失败放在结构化结果中。
4. 生成的 Go/C++ Protobuf 代码必须由现有脚本更新，并通过 descriptor/proto 一致性测试。

### R5. 前端体验

1. 新增动态菜单和页面：`/resource/camera`、路由名 `ResourceCamera`、页面权限 `resource:camera`，归属资源管理。
2. 页面提供分页列表、新增、编辑、删除和测活操作；不提供源级启停、ONVIF 发现或预览按钮。
3. 按钮权限码冻结为：`resource:camera:add`、`resource:camera:edit`、`resource:camera:delete`、`resource:camera:probe`。
4. 操作日志动作键冻结为：`POST /api/camera` → `resource.camera.add`、`PUT /api/camera/:id` → `resource.camera.edit`、`DELETE /api/camera/:id` → `resource.camera.delete`、`POST /api/camera/probe` → `resource.camera.probe`。测活是写操作，接入操作日志；操作日志可记录完整 URL，不做额外脱敏。
3. 表单包含名称、RTSP 地址、用户名、密码和备注；其中用户名/密码为前端临时字段，允许任意可输入凭据字符。用户可粘贴完整 URL，前端按“自动解析 + 可见校正”流程拆分并显示凭据，再编码拼接为提交用完整 `rtspUrl`。测活使用当前表单生成的完整 URL，可在保存前执行。
4. 列表显示名称、完整 URL（视觉上可截断但可查看/复制）、最近测活状态、最近测活时间、最后成功传输方式和最后已知媒体信息。
5. 测活期间按钮进入 loading/禁用状态；返回结构化失败结果时显示失败原因，不把摄像头离线当成未捕获异常。
6. 页面、按钮和 API 类型遵循现有 Vue/vben/Ant Design 组合、三语 i18n 和权限指令模式。

### R6. 可扩展性

1. RTSP 是第一个协议适配器；核心 Camera CRUD 不根据 URL 前缀散落协议分支。
2. 协议注册/适配抽象至少覆盖 URL 校验、测活和后续运行适配入口。
3. MVP 不预先加入 ONVIF 专属字段或通用无类型扩展字段。

## Acceptance Criteria

### 数据与 API

- [ ] 迁移创建 `cameras` 表，显式 snake_case 列、软删除字段、UUID `camera_id` 唯一性和必要索引；up/down 可重复执行。
- [ ] CRUD 支持名称、完整 RTSP URL、备注，返回 `id` 与 `cameraId`；名称可重复；删除为软删除且 `camera_id` 不复用。
- [ ] 保存不要求测活成功；非法协议或 URL 返回统一参数错误。
- [ ] 列表/详情返回完整 URL；操作日志按产品决策可记录完整 URL（不做额外脱敏）。
- [ ] 测活单入口支持保存前和已保存配置；配置指纹不匹配时结果不落库。

### C++ 与跨进程

- [ ] `EngineService.ProbeCamera` 在 Go/C++ 生成代码、客户端封装、服务端实现和契约测试中一致。
- [ ] 测活确实执行 PLAY、视频 Track 检查和首个编码帧判定，而不是只执行 OPTIONS/DESCRIBE。
- [ ] TCP 5 秒失败后释放资源并尝试 UDP 5 秒；成功返回实际传输方式和可获得的媒体信息。
- [ ] 所有退出路径释放临时媒体源，单次测活不会遗留重复上游连接。
- [ ] C++ 核心通过适配器/注册表选择协议，不把 RTSP 专属细节扩散到通用业务模型。

### 前端与权限

- [ ] `/resource/camera` 动态路由和菜单权限正确加载，无权限用户不能通过直接 URL 访问。
- [ ] 新增/编辑/删除/测活按钮按权限显示，表单值和测活结果状态正确处理。
- [ ] 中文、英文、繁体中文文案键完整对齐，`pnpm check` 通过。

### 质量

- [ ] Go repository/service/API 单测覆盖 URL 校验、分页、软删除、明文 URL 往返、测活持久化和指纹并发规则。
- [ ] C++ 单测覆盖成功首帧、无视频 Track、超时、TCP→UDP 回退、取消和清理。
- [ ] Go/C++ IPC fake 测试覆盖结构化测活失败、IPC 不可用和响应字段映射。
- [ ] `make -C app test`/项目规定的 Go 测试与 vet、相关 C++ 测试、前端 `pnpm check` 通过。

## Out Of Scope

- ONVIF 自动发现、凭据发现和 Profile 解析。
- 浏览器实时预览、录像、回放、下载和算法叠加。
- Camera Task、算法实例、常驻拉流、自动重连、任务状态上报和 DesiredState 对账。
- 源级启用/停用。
- H.264/H.265 解码兼容性验证、JPEG 生成和算法推理。
- 加密存储、密钥轮换和独立凭据管理；数据库仍只保存完整 `rtsp_url`。
- 厂商、型号、通道号等 ONVIF 派生字段。

## Technical Notes For Design Phase

- 当前 `engine/src/media/zlm/zlm_source.cpp` 固定 RTP TCP，需要抽取传输选择并增加一次性首帧同步等待能力。
- ZLMediaKit `RtspUrl::setup` 会通过 `UrlDecodeUserOrPass` 解码百分号编码的凭据，能够传递编码后的特殊字符；其 `RtspPlayer.cpp` 当前 `DebugL` 会输出解析后的用户名和密码，实现必须移除或改成不含凭据的日志。
- 操作日志脱敏：按产品决策，操作日志可记录完整 RTSP URL（含 userinfo），不需要额外脱敏改造。
- C++ 临时媒体源的回调不能执行数据库、HTTP 或其他阻塞工作；首帧通知应使用有界、可取消的同步原语。
- REST 使用数据库 `id`，跨进程与后续任务使用 `camera_id`。
- 生产 schema 只通过版本化 PostgreSQL migration；sqlite 仅用于单元测试 AutoMigrate。

## Remaining Product Decisions

- 无。产品需求已全部冻结。
- 摄像头页面的按钮权限码和操作日志动作键需要与菜单 migration 一起冻结；建议使用 `resource:camera:add|edit|delete|probe`。
