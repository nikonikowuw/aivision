# 摄像头实时预览功能 Technical Design

## 1. 总体架构与数据流图

```mermaid
flowchart TD
    subgraph Frontend [Vue 3 Web 前端]
        LiveGrid[实时预览页 1/4/9 分屏 (/live)]
        CameraModalPreview[摄像头管理列表快捷预览 Modal]
        Player[WS-FLV / mpegts.js 播放器组件]
    end

    subgraph GoBackend [Go 后端管理服务 :8000]
        Router[/api/live/* & /api/camera/:id/preview*]
        CameraService[Camera / Live Service]
        IPCClient[UDS gRPC Client]
    end

    subgraph CppEngine [C++ 推理与媒体引擎]
        UDSServer[UDS gRPC Server]
        StreamManager[LiveStreamManager]
        TaskScheduler[TaskScheduler (推理任务)]
        ZLMediaKit[嵌入式 ZLMediaKit (:8080)]
    end

    subgraph CameraSource [摄像头 RTSP 视频源]
        RTSPStream[RTSP 视频流]
    end

    LiveGrid -->|HTTP GET/POST /api/camera/:id/preview/start| Router
    Router --> CameraService
    CameraService -->|gRPC StartCameraPreview(camera_id, rtsp_url)| IPCClient
    IPCClient -->|engine.sock UDS| UDSServer
    UDSServer --> StreamManager
    StreamManager <-->|检查是否已有任务在拉流| TaskScheduler
    StreamManager -->|创建/复用 PlayerProxy| ZLMediaKit
    ZLMediaKit -->|RTSP 拉流（单路复用）| RTSPStream
    ZLMediaKit -.->|WS-FLV ws://host:8080/live/camera_id.live.flv| Player
```

---

## 2. 模块职责与接口设计

### 2.0 数据库模型扩展 (Database Schema)

在 `cameras` 表及 GORM 模型中扩展子码流字段：

```go
type Camera struct {
    BaseModel
    CameraID          string     `gorm:"column:camera_id;type:varchar(64);not null;uniqueIndex" json:"cameraId"`
    Name              string     `gorm:"column:name;type:varchar(128);not null" json:"name"`
    Protocol          string     `gorm:"column:protocol;type:varchar(32);not null;default:'rtsp'" json:"protocol"`
    RtspURL           string     `gorm:"column:rtsp_url;type:text;not null" json:"rtspUrl"`
    SubRtspURL        string     `gorm:"column:sub_rtsp_url;type:text;not null;default:''" json:"subRtspUrl"`
    Remark            string     `gorm:"column:remark;type:text;not null;default:''" json:"remark"`
    // ... 其他测活与元数据字段 ...
}
```

- **添加/编辑逻辑**：保存时自动推导并探测 `SubRtspURL`，支持前端高级选项中手动覆盖；
- **预览时直接读取**：单分屏读 `RtspURL`，多分屏直接读取 `SubRtspURL`（若为空平滑降级为 `RtspURL`）。

---

### 2.1 C++ 引擎与 gRPC IPC (engine.proto)

在 `engine.proto` 中扩充预览流控制 RPC：

```protobuf
message StartCameraPreviewRequest {
  string camera_id = 1;
  string rtsp_url = 2; // 完整 RTSP URL（含 userinfo）
}

message StartCameraPreviewResponse {
  string code = 1;          // 空串表示成功，失败时为稳定错误码
  string error_message = 2; // 诊断信息
  string stream_path = 3;   // 如 "/live/<camera_id>.live.flv"
  int32 http_port = 4;      // 如 8080
  int32 ws_port = 5;        // 如 8080
}

message StopCameraPreviewRequest {
  string camera_id = 1;
}

message StopCameraPreviewResponse {
  string code = 1;
  string error_message = 2;
}

service EngineService {
  // ... 现有 RPC ...
  rpc StartCameraPreview(StartCameraPreviewRequest) returns (StartCameraPreviewResponse);
  rpc StopCameraPreview(StopCameraPreviewRequest) returns (StopCameraPreviewResponse);
}
```

### 2.2 ZLMediaKit 流媒体生命周期管理

- **流路径规则**：`vhost="__defaultVhost__"`, `app="live"`, `stream="<camera_id>"`；
- **WS-FLV 地址**：`ws://<host>:<port>/live/<camera_id>.live.flv`；
- **HTTP-FLV 地址**：`http://<host>:<port>/live/<camera_id>.live.flv`；
- **按需拉流与自动注销**：
  - 使用 `PlayerProxy` 包装上游 RTSP 拉流；
  - 开启 `enable_no_reader` 与 `no_reader_ms = 10000`（10秒无 Reader 自动销毁）；
  - 当摄像头存在运行中的推理任务时，`PlayerProxy` 由任务持有生命周期，不随无预览 Reader 而销毁。

### 2.3 Go 后端 API

1. `POST /api/camera/:id/preview/start`
   - 校验摄像头存在且有效；
   - 调用 C++ 引擎 `StartCameraPreview`；
   - 动态获取当前请求 Host 或服务端配置构建完整的 `ws_url` 和 `http_url`，返回前端：
     ```json
     {
       "code": 0,
       "data": {
         "cameraId": "uuid",
         "name": "前台球机",
         "streamPath": "/live/uuid.live.flv",
         "wsUrl": "ws://192.168.1.50:8080/live/uuid.live.flv",
         "httpUrl": "http://192.168.1.50:8080/live/uuid.live.flv"
       },
       "message": "success"
     }
     ```
2. `POST /api/camera/:id/preview/stop`
   - 显式通知引擎减少/停止预览引用。
3. `GET /api/camera/options`（或复用列表 API）
   - 获取当前可供预览选择的摄像头快速下拉列表（含 `id`, `cameraId`, `name`, `status`）。

### 2.4 前端播放器与视图设计

1. **播放器组件封装 (`VideoPlayer.vue`)**：
   - 基于 `mpegts.js`（或 `jessibuca`）封装原生 Canvas/Video 播放引擎；
   - 支持 WS-FLV / HTTP-FLV 自动重连与延迟追踪追帧策略；
   - 内置控制条（全屏、截屏 Canvas 导出、静音、刷新、关闭）；
   - 状态展示（连接中、正在播放、离线重连中、播放错误）。
2. **实时预览页面 (`views/live/index.vue`)**：
   - 顶部工具栏：1（默认） / 4 / 9 分屏切换按钮、全屏所有画面按钮、全部刷新按钮；
   - 网格主体：自适应 Grid 布局，每个 Cell 维护独立播放器及当前选中的 `camera_id`；
   - 选路弹窗/下拉菜单：点击空网格弹出轻量级摄像头选择卡片；
   - 页面离开生命周期：在 `onUnmounted` 中调用所有子播放器的 `destroy()` 释放 WebSocket 连接。
3. **摄像头管理页面联动 (`views/resource/camera/index.vue`)**：
   - 操作列增加「预览」按钮，点击弹出复用 `VideoPlayer` 的 Modal。

---

## 3. 兼容性与安全性

- **多网卡/跨网段访问**：Go 后端从 HTTP Request Header (`r.Host` / `X-Forwarded-Host`) 智能提取访问 IP，拼装客户端可直达的 WS-FLV 地址；
- **RBAC 权限**：菜单权限码 `live:preview`，操作日志记录预览启动；
- **资源隔离与保护**：单个边缘设备限制最大并发拉流路数，超出时返回明确业务错误码（如 `1016: 预览连接数达到上限`）。
