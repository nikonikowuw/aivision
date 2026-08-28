# 摄像头实时预览功能 PRD

## 1. 目标与用户价值 (Goal & User Value)

在边缘 AI 视觉管理平台中，“实时预览”（`/live`）是登录系统的**默认首页**，也是设备监控的核心运维与展示界面。

在实际工业与安防落地中，面临两大严峻挑战：
1. **H.265 浏览器解码瓶颈**：高分辨率主码流（4K/2K/1080p）普遍采用 H.265 编码，多数 PC/浏览器无硬件解码加速或 MSE 不支持，多路软解极易导致浏览器 CPU 100%、严重卡顿掉帧甚至页面崩溃；
2. **多分屏网络与计算压力**：4 分屏或 9 分屏同时拉取多路主码流会耗尽边缘设备的出口带宽与 Web 端渲染算力。

**核心解决方案——主/子码流自适应分级策略**：
- **大部分主流摄像头（海康、大华、宇视、TP-Link等）均具备双码流能力，且子码流普遍为兼容性最好的 H.264 编码（720P/D1/VGA 分辨率）**；
- **单分屏 / 弹窗详情（单画面沉浸展示）**：自动拉取与播放**主码流 (Main Stream)**，保证高画质与细节清晰；
- **4 分屏 / 9 分屏（多分屏实时监控）**：自动拉取与播放**子码流 (Sub Stream)**，保证多路画面毫秒级秒开、H.264 纯硬解、CPU 占用极低；
- **子码流地址智能推导与探测 (Auto-Derivation & Probe)**：各品牌子码流地址与主码流具有确定性的规则差异（如 `101 -> 102`、`subtype=0 -> subtype=1`、`main -> sub`），系统自动推导并探测子码流，用户零心智负担。

---

## 2. 现有事实与产品规范契约 (System Facts & PRD Baseline)

依据 `docs/prd/ai-video-analytics-edge-platform-v1.0.md` 规范：

1. **路由与页面规范 (7.17.4)**：
   - 顶层路由为 `/live`（`LivePreview`），权限码 `live:preview`，图标 `ant-design:video-camera-outlined`；
   - 菜单元数据配置 `hideChildrenInMenu=true`，左侧导航、面包屑与标签页均只显示“实时预览”，无二级菜单；
   - 作为默认首页固定在标签栏（`affix=true`）；
   - **不缓存页面（`keepAlive=false`）**：离开页面时销毁所有播放器实例并释放播放会话；再次进入时使用默认 1 画面空布局。
2. **流媒体与媒体后端规范 (7.3 & 8.2)**：
   - 使用嵌入 C++ 推理引擎的 **ZLMediaKit** 提供 Web 视频预览；
   - 传输协议采用 **WS-FLV / HTTP-FLV**；
   - 纯画面输出：**预览流不叠加目标框、规则区域、事件类型、置信度或识别结果**（纯净原始视频流）；
   - 不提供录像、回放与下载功能；
   - 推理任务与预览拉流：算法推理任务默认绑定主码流（或指定流），与主码流预览复用同一路 `PlayerProxy` 上游拉流源。

---

## 3. 核心机制：主子码流自适应与推导 (Main/Sub Stream Architecture)

### 3.1 分屏自适应流选择规则 (Stream Resolution Strategy)

| 场景 | 播放流类型 | 目标分辨率/编码 | 核心诉求 | 降级机制 |
| :--- | :--- | :--- | :--- | :--- |
| **1 分屏（单画面）** | **主码流 (Main Stream)** | 原画（1080P/2K/4K, H.264/H.265） | 超清大屏画面、细节清晰 | 若主码流拉流失败，提示重试 |
| **详情弹窗 / 放大 Modal** | **主码流 (Main Stream)** | 原画 | 局部排错、高清识别验证 | 同上 |
| **4 分屏 (2x2)** | **子码流 (Sub Stream)** | 标清/流畅（720P/D1, H.264） | 毫秒级秒开、纯硬解、低 CPU、低带宽 | 若子码流探测失败或无子码流，**平滑降级为主码流** |
| **9 分屏 (3x3)** | **子码流 (Sub Stream)** | 标清/流畅（720P/D1, H.264） | 超多路并发、流畅不卡顿 | 若子码流探测失败或无子码流，**平滑降级为主码流** |
| **双击/放大单格** | **从子码流切换至主码流** | 自动平滑切换 | 快速聚焦查看高清画面 | 切换中显示过渡骨架 |

### 3.2 子码流地址智能规则库与推导逻辑 (Auto-Derivation Engine)

系统内置主流厂商 RTSP 规则匹配与替换引擎：

| 厂商 / 协议 | 主码流典型 Pattern (Main) | 子码流推导 Pattern (Sub) | 替换/提取规则 |
| :--- | :--- | :--- | :--- |
| **海康威视 (Hikvision)** | `/Streaming/Channels/101`<br>`/Streaming/Channels/201` | `/Streaming/Channels/102`<br>`/Streaming/Channels/202` | 正则匹配 `/Channels/(\d+)01` $\rightarrow$ 替换为 `/Channels/$102` |
| **海康 (旧版/NVR)** | `/h264/ch1/main/av_stream`<br>`/ch1/main/av_stream` | `/h264/ch1/sub/av_stream`<br>`/ch1/sub/av_stream` | 替换 `/main/` $\rightarrow$ `/sub/` |
| **大华 / 乐橙 (Dahua)** | `channel=1&subtype=0`<br>`channel=1&subtype=main` | `channel=1&subtype=1`<br>`channel=1&subtype=sub` | 参数替换 `subtype=0` $\rightarrow$ `subtype=1` 或 `subtype=main` $\rightarrow$ `sub` |
| **宇视 (Uniview)** | `/video1`<br>`/media/video1` | `/video2`<br>`/media/video2` | 路径末尾 `/video1` $\rightarrow$ `/video2` |
| **天地伟业 (Tiandy)** | `/ch1/main/av_stream` | `/ch1/sub/av_stream` | 路径 `/main/` $\rightarrow$ `/sub/` |
| **TP-Link / 水星** | `/stream1`<br>`/ch1/0` | `/stream2`<br>`/ch1/1` | 路径 `/stream1` $\rightarrow$ `/stream2` 或 `/0$` $\rightarrow$ `/1` |
| **通用 RTSP 规范** | `.../main/...` 或 `.../live/ch0` | `.../sub/...` 或 `.../live/ch1` | 词法替换 `main` $\rightarrow$ `sub`、`ch0` $\rightarrow$ `ch1` |

**推导与落库策略**：
1. **自动推导**：保存摄像头主码流 `rtsp_url` 时，系统自动根据规则推导生成 `sub_rtsp_url`；
2. **自动测活校验**：测活接口同时对主码流和推导的子码流发起异步验证，记录主/子码流的各自实际编码格式（如主码流 H265、子码流 H264）；
3. **手动覆盖能力**：在摄像头编辑的高级选项中，允许专业用户查看推导的子码流地址并进行自定义覆盖（非必填）；
4. **降级兜底**：若无法推导出子码流或子码流不可达，`sub_rtsp_url` 自动退化指向主码流 `rtsp_url`。

### 3.3 数据模型扩展 (Data Model Extension)

在 `cameras` 表及 Go GORM 模型中增加 `sub_rtsp_url` 字段：
- **列定义**：`sub_rtsp_url text NOT NULL DEFAULT ''`；
- **生命周期**：
  - 添加/编辑摄像头时，若用户未显式指定，后端根据主码流 `rtsp_url` 自动推导并探测；
  - 探测成功则持久化 `sub_rtsp_url`；若该摄像头无子码流能力或探测失败，则存为空串 `""`；
  - 预览读取：多分屏（4/9分屏）拉流时，优先读取持久化的 `sub_rtsp_url`；若为空，则平滑降级使用 `rtsp_url` 主码流。

---

## 4. 功能需求规划 (Detailed Requirements)

### R1: 顶层导航与首页改造 (`/live`)
- 登录后默认进入 `/live`；
- 顶层目录配置 `hideChildrenInMenu=true`，`affix=true`，`keepAlive=false`；
- 默认展示 **1 画面空布局**，顶部工具栏提供 1 / 4 / 9 分屏切换按钮与全屏按钮。

### R2: 实时预览分屏自适应播放控制 (Live Grid Multi-View)
- **空网格交互**：点击空网格弹出轻量级摄像头选择器；
- **分屏联动流切换**：
  - 在 1 分屏模式下：为选中的摄像头请求 `streamType=main`；
  - 在 4 / 9 分屏模式下：为选中的摄像头批量请求 `streamType=sub`（若无可用子码流自动降级 `main`）；
  - 从 4/9 分屏切换到 1 分屏，或双击某个画格放大时：动态无缝重连至对应的主码流；
- **播放器控制与状态**：
  - 画面左上角叠加摄像头名称与「主码流 / 子码流」标签；
  - 右下角展示实时帧率、分辨率、当前解码模式（Hardware / WASM）；
  - 悬浮控制栏：截图保存（Canvas 导出）、全屏切换、静音开关、重新连接、移除此画格。

### R3: 摄像头管理列表快捷预览 (`/resource/camera`)
- 列表操作列点击「实时预览」，弹出大尺寸 Modal 窗口，默认以**主码流**高清呈现。

### R4: C++ 推理与流媒体引擎 (Engine & ZLMediaKit)
- **多流路径隔离**：
  - 主码流流标识：`live/<camera_id>_main`
  - 子码流流标识：`live/<camera_id>_sub`
- **按需拉流与复用**：
  - 主码流与子码流作为独立的 `PlayerProxy` 按需建立；
  - 若推理任务运行在主码流上，主码流 `PlayerProxy` 常驻，预览只建立客户端 WS-FLV 监听；子码流仍遵循无观众 10 秒自动销毁；
- **gRPC IPC 扩展**：
  - `StartCameraPreview(camera_id, stream_type="main"|"sub", url)`
  - `StopCameraPreview(camera_id, stream_type)`

### R5: Web 端 H.265 软硬解兼容播放器
- **H.264（子码流）**：采用 MSE/WebCodecs 硬件加速播放，极低延迟（< 500ms），极低 CPU；
- **H.265（单分屏主码流）**：
  - 现代支持 HEVC 的浏览器（Chrome 107+ 开启硬解/Edge/Safari）走原生硬件加速；
  - 不支持原生 HEVC 的老环境自动无缝启用 WebAssembly 软解引擎（如 `jessibuca` / `mpegts.js-wasm`），仅单画面软解，CPU 安全可控。

---

## 5. 验收标准 (Acceptance Criteria)

- [ ] **AC-1 (主子码流自动推导)**：输入海康、大华等主流摄像头主码流 RTSP 地址后，系统能准确推导出对应的子码流地址，并支持手动微调；
- [ ] **AC-2 (分屏自适应切流)**：在 1 分屏下播放主码流高清流；切换至 4 分屏或 9 分屏时，各画格自动切换至子码流 H.264 低功耗流播放；
- [ ] **AC-3 (H.265 多分屏流畅度)**：即使主码流为 4K/2K H.265 编码，4 分屏与 9 分屏依然能够通过子码流 H.264 极速点亮并流畅播放，浏览器 CPU 保持平稳（< 30%）；
- [ ] **AC-4 (弹窗高清预览)**：列表页点击预览或首页双击放大时，能无缝拉取主码流高清画面，提供截图、全屏等完整工具；
- [ ] **AC-5 (单流多端复用与释放)**：多窗口/多画格打开同一摄像头的同一种码流时，引擎后台仅维持 1 路对上游摄像头的 RTSP 连接；全部关闭后按需释放。
