# RTSP 摄像头视频源管理 MVP — 技术设计

> 本文档记录实现边界、契约、数据流与关键权衡。所有需求见 `prd.md`；执行顺序与验证见 `implement.md`。

## 1. 总览与数据流

```text
浏览器 /resource/camera
  │  GET/POST/PUT/DELETE /api/camera*
  ▼
Go: api.Handler → service.CameraService → repository.CameraRepository → GORM (PostgreSQL cameras 表)
  │                            │
  │        POST /api/camera/probe（可选 id + 配置指纹校验）
  ▼                            ▼
  └── engineipc.EngineClient.ProbeCamera ──► engine.sock
                                             ▼
                                    C++ EngineServiceImpl::ProbeCamera
                                             ▼
                                    protocol registry → RTSP adapter
                                             ▼
                                    media::IMediaBackend → ZLM MediaPlayer 临时源
                                    （TCP 5s → 释放 → UDP 5s；首帧即成功）
```

## 2. 分层组件

### 2.1 Go 后端（app/）

| 组件 | 文件（新增） | 职责 |
|---|---|---|
| Model | `internal/model/camera.go` | `Camera` 结构 + `TableName()` |
| Repository | `internal/repository/camera.go` | CRUD、分页、软删、批量删、按 id 查 |
| Service | `internal/service/camera.go` | URL 校验、协议注册表、配置指纹、测活编排、错误映射 |
| Protocol registry | `internal/service/camera_protocol.go`（或 pkg 下） | `rtsp` 适配器注册：URL 校验 + Probe 请求构造 |
| API | `internal/api/camera.go` | Gin handlers、DTO、`code=0` 响应 |
| Router | `internal/router/router.go`（修改） | 注册 `/api/camera*` 路由 + 权限 |
| Migration | `migrations/000011_*.up/down.sql` | cameras 表 + camera 菜单/权限种子 |
| Wire | `cmd/api/wire.go`（修改，`make wire`） | 注入 repository/service/handler |

### 2.2 C++ 引擎（engine/）

| 组件 | 文件 | 职责 |
|---|---|---|
| proto | `proto/aivision/v1/engine.proto`（修改） | 新增 `ProbeCamera` RPC + 消息 |
| 生成代码 | `app/internal/proto/...`（重新生成） | Go stub |
| IPC 服务 | `engine/src/core/ipc/uds_server.cpp`（修改） | `EngineServiceImpl::ProbeCamera` |
| 测活核心 | 新增 `engine/src/core/camera/probe_rtsp.cpp/.hpp`（建议） | 协议无关的测活流程：超时、TCP→UDP、首帧同步、清理 |
| 媒体抽象 | `engine/include/aivision/media/media_api.hpp`（修改） | 提供一次性测活接口（或复用 IMediaSource + 事件） |
| ZLM 实现 | `engine/src/media/zlm/zlm_source.cpp`（修改） | 传输模式可配置；移除/避免凭据日志 |
| 单测 | `engine/tests/unit/test_probe_rtsp.cpp`（新增） | mock media backend 覆盖成功/失败/超时/回退/取消/清理 |

## 3. 数据库设计（cameras 表）

遵循项目规范：显式 snake_case 列、无外键、毫秒软删除（`BaseModel`）、UUID 由 Go 生成。

```sql
CREATE TABLE cameras (
    id                 BIGSERIAL PRIMARY KEY,
    camera_id          VARCHAR(36)  NOT NULL UNIQUE,           -- 不可变 UUID
    protocol           VARCHAR(16)  NOT NULL DEFAULT 'rtsp',
    name               VARCHAR(128) NOT NULL,
    rtsp_url           VARCHAR(2048) NOT NULL,
    remark             VARCHAR(255) NOT NULL DEFAULT '',
    transport_policy   VARCHAR(16)  NOT NULL DEFAULT 'auto',
    config_hash        VARCHAR(64)  NOT NULL DEFAULT '',        -- 配置指纹，用于测活乐观并发
    last_probe_status  VARCHAR(16)  NOT NULL DEFAULT 'never',   -- never|success|failed
    last_probe_at      TIMESTAMPTZ,
    last_probe_error_code   VARCHAR(64) NOT NULL DEFAULT '',
    last_probe_error_message VARCHAR(255) NOT NULL DEFAULT '',
    last_success_at    TIMESTAMPTZ,
    last_success_transport  VARCHAR(16) NOT NULL DEFAULT '',
    last_codec         VARCHAR(16)  NOT NULL DEFAULT '',
    last_width         INT          NOT NULL DEFAULT 0,
    last_height        INT          NOT NULL DEFAULT 0,
    last_fps           DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at         BIGINT NOT NULL DEFAULT 0               -- BaseModel 毫秒软删除
);
CREATE INDEX idx_cameras_deleted_id ON cameras (deleted_at, id);
CREATE INDEX idx_cameras_name ON cameras (deleted_at, name);
```

要点：
- `camera_id` 用 Go `github.com/google/uuid` 生成（若项目已有 UUID 依赖则复用，否则引入）。
- `config_hash` 持久化便于页面判断“最后结果是否对应当前配置”。
- 生产仅走 migration；sqlite AutoMigrate 仅单测用。

## 4. Go 契约

### 4.1 REST API

```text
GET    /api/camera/page?page=&pageSize=&name=      → {items:[CameraItem], total}
POST   /api/camera                                 → CameraItem   (body: {name, rtspUrl, remark})
PUT    /api/camera/:id                             → CameraItem   (body: {name, rtspUrl, remark})
DELETE /api/camera/:id                             → null
DELETE /api/camera/batch                           → null         (body: {ids:[...]})
POST   /api/camera/probe                           → ProbeResult  (body: {id?, protocol?, rtspUrl})
```

CameraItem 返回：
```json
{
  "id": 12,
  "cameraId": "uuid...",
  "protocol": "rtsp",
  "name": "门口",
  "rtspUrl": "rtsp://user:p%40ss@192.168.1.10/live",
  "remark": "",
  "lastProbeStatus": "success|failed|never",
  "lastProbeAt": "...",
  "lastProbeErrorCode": "",
  "lastSuccessAt": "...",
  "lastSuccessTransport": "tcp",
  "lastCodec": "H264",
  "lastWidth": 1920,
  "lastHeight": 1080,
  "lastFps": 25,
  "createdAt": "...",
  "updatedAt": "..."
}
```

ProbeResult（`code=0`，无论测活成功失败）：
```json
{
  "status": "success|failed",
  "failureCode": "RTSP_PLAY_TIMEOUT",
  "failureMessage": "测活失败",
  "attempts": [
    {"transport": "tcp", "elapsedMs": 5000, "failureCode": "RTSP_PLAY_TIMEOUT"},
    {"transport": "udp", "elapsedMs": 3200, "failureCode": "RTSP_NO_FIRST_FRAME"}
  ],
  "selectedTransport": "udp",
  "codec": "H264",
  "width": 1920,
  "height": 1080,
  "fps": 25,
  "elapsedMs": 8250,
  "persisted": true,
  "stale": false
}
```

### 4.2 配置指纹

```text
configHash = sha256(protocol + "\x00" + strings.TrimSpace(rtspUrl) + "\x00" + transportPolicy)
```

- 保存/更新时计算并写入 `config_hash`。
- Probe 请求带 `id` 时：service 读取 DB 当前 `config_hash`，与请求 URL 计算值比对；一致才在完成后落库，否则 `persisted=false, stale=true`。

### 4.3 测活持久化规则

- 无 `id`：只返回结果，不落库。
- 有 `id` + 指纹一致：成功 → 更新 `last_probe_status=success, last_probe_at, last_success_at, last_success_transport, last_codec/width/height/fps`；失败 → 更新 `last_probe_status=failed, last_probe_at, last_probe_error_*`，保留最后成功媒体信息。
- 有 `id` + 指纹不一致：返回结果 + `persisted=false, stale=true`，不覆盖。

### 4.4 错误映射

| 情况 | 响应 |
|---|---|
| 参数非法（协议不支持、URL 格式错、长度超限） | `errno.CodeInvalidParam`（HTTP 业务错误） |
| `id` 不存在 | `errno.CodeNotFound` |
| IPC 不可用 / transport error | `CodeInternal`（`errno` 或现有 engineipc 映射） |
| C++ RPC 返回非空 code（如 RTSP 相关） | 按现有 engineipc `RemoteError` 机制处理为业务错误或内部错误 |
| 测活失败（RTSP 不可达等） | `code=0` + `data.status=failed` |

## 5. engine.proto 扩展

```protobuf
// 摄像头测活请求（RTSP MVP）
message ProbeCameraRequest {
  string protocol = 1;  // 当前固定 "rtsp"
  string url = 2;       // 完整 RTSP URL（可含百分号编码 userinfo）
}

message ProbeCameraResponse {
  string code = 1;                  // 空串表示 RPC 处理成功（非测活结果）
  string error_message = 2;         // 仅诊断，客户端不得解析
  string status = 3;                // success | failed
  string failure_code = 4;          // RTSP_* 稳定码（status=failed 时有值）
  repeated ProbeAttempt attempts = 5; // 每次尝试（tcp/udp）
  string selected_transport = 6;    // 实际成功传输方式
  string codec = 7;                 // H264/H265/...（可获得时）
  uint32 width = 8;
  uint32 height = 9;
  double fps = 10;
  uint64 elapsed_ms = 11;
}

message ProbeAttempt {
  string transport = 1;   // tcp | udp
  string failure_code = 2; // 空串表示该次成功
  uint64 elapsed_ms = 3;
}
```

- 加入 `service EngineService { rpc ProbeCamera(...) returns (...); }`。
- 重新生成 Go stub（`app/scripts/generate-proto.sh`）与 C++ stub。
- Go `EngineClient` 增加 `ProbeCamera` 包装方法（现有 `call` 包装模式）。
- `engineipc` 契约测试 / fake engine 增加 `ProbeCamera` 用例。

## 6. C++ 测活实现

### 6.1 核心流程（probe_rtsp）

```text
ProbeCamera(protocol, url):
  adapter = protocol_registry.lookup(protocol)          // 无 → 返回 UNSUPPORTED_PROTOCOL
  for transport in [tcp, udp]:                          // 当前固定顺序
    source = media_backend->create_source(transport)     // ZLM 临时源
    attempt = run_once(source, url, 5s)                  // 同步等待首帧
    source->stop(); source.reset()                       // 无论成败必须释放
    record attempt
    if success: return success(selected_transport, codec, w, h, fps)
  return failed(last failure_code)
```

### 6.2 run_once（单次尝试）

- 创建 MediaPlayer（EventPoller 上），设置 `kRtspTransportType` 为当前 transport，设置凭据（若 URL 含 userinfo 由 ZLM 解码）。
- `player->play(url)`。
- 用 `std::promise`/`std::condition_variable` 有界等待（5 秒），条件为：
  - 收到首个视频 Track 编码帧（onPacket 且 track 为 video）→ 成功；
  - `setOnShutdown`/`setOnPlayResult` 错误 → 记录失败码；
  - 超时 → 记录失败码。
- 成功时从 track 提取 codec/width/height/fps。
- 关键：**等待必须可取消且不阻塞 EventPoller 线程**；回调内只设置标志 + notify，不做阻塞工作。

### 6.3 传输模式支持

- `IMediaSource::start` 当前签名只接受 `rtsp_url`；需增加传输策略参数（或新增 `start_probe(url, transport)`），ZLM `zlm_source.cpp` 根据 transport 设置 `RTP_TCP` / `RTP_UDP`。
- 现有 CameraTask 路径不破坏：默认仍 TCP（与 `transport_policy=auto` 的首次尝试一致）。

### 6.4 凭据与日志

- 产品决策：数据库/API/操作日志均明文记录完整 RTSP URL（含 userinfo），不做额外脱敏；访问受现有权限控制。
- 自有代码（Go/C++）不主动打印完整 URL 或解码后的 userinfo；若 ZLM `RtspPlayer.cpp` 的 `DebugL` 输出凭据，按产品决策可保留，但实现时确认其具体位置与是否影响默认日志级别。
- 错误消息传回 Go 时只含诊断文本；Go 侧不额外对响应做脱敏（与产品决策一致）。

### 6.5 稳定失败码

复用 prd.md R3.10 的集合；C++ 负责分类：
- 连接错误（OPTIONS/DESCRIBE 层）→ `RTSP_CONNECT_FAILED`
- 401/403 鉴权 → `RTSP_AUTH_FAILED`
- PLAY 超时/无响应 → `RTSP_PLAY_TIMEOUT`
- 无视频 Track → `RTSP_NO_VIDEO_TRACK`
- 有 Track 无首帧 → `RTSP_NO_FIRST_FRAME`
- 其他媒体错误 → `RTSP_MEDIA_ERROR`
- 未知协议 → `RTSP_UNSUPPORTED_PROTOCOL`
- 取消/超时整体 → `RTSP_PROBE_CANCELLED`

## 7. 操作日志

- 按产品决策：操作日志记录完整 RTSP URL（含 userinfo），**不做额外脱敏改造**（`mask.MaskJSONBody` 现有关键字不含 `rtspUrl`，无需修改）。
- 访问控制由现有权限中间件负责；与列表/详情接口同等受 `resource:camera:*` 权限保护。

## 8. 前端

### 8.1 路由与菜单

- 后端 migration 种子（幂等）新增菜单：
  - 目录 `Resource`（若不存在）→ 菜单 `Camera`：`type=menu, path=/resource/camera, component=/resource/camera/index, permission=resource:camera, name=resource.camera, icon=...`
  - 按钮权限：`resource:camera:add|edit|delete|probe`
  - 绑定 super 角色（沿用 000009/000010 模式）。
- 前端 `pageMap` glob 自动匹配 `views/resource/camera/index.vue`，无需手写静态路由。

### 8.2 页面

- 文件：`ui/apps/web-antd/src/views/resource/camera/index.vue`
- API 层：`ui/apps/web-antd/src/api/core/camera.ts`（`CameraApi` 命名空间，类型化）
- 列表：vxe-grid + 搜索（name 模糊）+ 分页；列：名称、完整 URL（可查看/复制）、最近测活状态（Tag）、最近测活时间、最后成功传输方式、最后媒体信息、操作。
- 表单（vben-form 弹窗）：名称、无凭据地址、用户名、密码（临时）、备注；测活按钮。
- URL 解析/编码 util：`ui/apps/web-antd/src/utils/rtsp.ts`（或 api/core/camera.ts 内）：
  - `parseRtspUrl(url)` → `{address, username, password}`（标准解析 + 摄像头兼容拆分）；
  - `buildRtspUrl(address, username, password)` → 百分号编码拼回。
- 权限：`v-access:code="['resource:camera:add']"` 等。
- i18n：`resource/camera.json`（zh-CN/zh-TW/en-US 三语）。
- 测活交互：表单内“测试连接”按钮 → `POST /api/camera/probe`（带当前生成 URL，可带 id）→ loading/禁用 → 显示结构化结果（成功显示媒体信息；失败显示 failureCode 本地化文案）。

## 9. 关键权衡与风险

1. **测活超时 10s 内**：Go HTTP handler 与 engineipc 调用需设略高于 10s 的 deadline（如 12s）；前端请求超时相应调高。坏地址不会拖垮页面。
2. **首次帧同步不阻塞 EventPoller**：回调只 notify；等待方在独立线程/调用栈上。
3. **临时源泄漏防护**：所有退出路径（成功/失败/超时/取消/IPC 取消）统一 RAII/scope guard 释放。
4. **配置指纹并发**：保存与测活并发时以 `config_hash` 比对为准，不回滚已保存的新配置。
5. **C++ 协议扩展**：`ProbeCamera` 通过协议注册表分发；后续新增协议只加适配器。
6. **凭据明文**：产品决策，数据库/API/操作日志均明文；访问受现有权限控制，不额外脱敏。
7. **ZLM 传输回退**：若某设备 TCP 成功后 UDP 也允许，正式运行与测活顺序一致（TCP 优先），保证结果代表性。

## 10. 兼容与回滚

- 新增表与菜单通过版本化 migration；回滚执行对应 `.down.sql`。
- 新增 RPC 为向后兼容扩展；旧 Go/C++ 互不感知新增方法（gRPC 协议兼容）。
- 前端菜单由后端驱动，旧前端不显示新菜单；新前端依赖后端 migration 完成后才可见。
