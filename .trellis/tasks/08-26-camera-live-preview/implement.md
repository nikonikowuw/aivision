# 摄像头实时预览功能 Implementation Plan

## 1. 任务拆分与执行阶段

### 阶段一：子码流推导规则与 C++ 双码流引擎 (Engine & IPC)
1. **Proto 升级**：
   - 在 `engine/proto/aivision/v1/engine.proto` 中定义 `StreamType`（`MAIN` / `SUB`）枚举与 `StartCameraPreview` / `StopCameraPreview` RPC；
   - 生成 Go pb 桩代码与 C++ pb 头文件；
2. **C++ 引擎流媒体服务与多流管理 (LiveStreamManager)**：
   - 在 `engine/src/app/main.cpp` 中初始化并启动 ZLMediaKit HTTP/WS 服务监听器（默认端口 8080，支持配置覆盖）；
   - 实现 `LiveStreamManager`：管理 `<camera_id>_main` 与 `<camera_id>_sub` 的独立 `PlayerProxy` 拉流实例；
   - 实现与 `TaskScheduler` 的主码流拉流源复用（有算法任务时直接复用，无算法任务时按需拉流并在 10 秒无观众时自动释放）；
   - 子码流配置 `enable_no_reader` 自动回收；
3. **C++ 单元测试与质量验证**：
   - 编写 `LiveStreamManager` 单测，覆盖主/子码流独立建立、单流多路复用与超时销毁；
   - 跑通 `make -C engine test`、`make -C engine asan`、`make -C engine tsan`。

### 阶段二：Go 后端子码流推导与预览 API (Backend API & IPC)
1. **数据库迁移**：
   - 新增迁移脚本 `000013_add_camera_sub_url`：在 `cameras` 表增加 `sub_rtsp_url` 与 `last_sub_probe_*` 字段；
   - 新增迁移脚本 `000014_seed_live_preview_menu`：注册 `/live` 顶层菜单与 `live:preview` 权限码；
2. **子码流推导引擎 (`internal/pkg/rtsp`)**：
   - 实现海康、大华、宇视、TP-Link 等主流厂商 RTSP 子码流正则推导与兜底回退算法；
   - 编写全面的单测用例覆盖各品牌主/子码流互转；
3. **Camera / Live Service & Engine IPC Client**：
   - 摄像头新增/更新时自动推导并持久化 `sub_rtsp_url`；
   - 实现 `StartPreview(cameraId, streamType)` 服务：调用引擎 IPC 并拼装完整的客户端 WebSocket / HTTP-FLV 播放 URL；
4. **REST 控制器与路由**：
   - 实现 `POST /api/camera/:id/preview/start?stream_type=main|sub`
   - 实现 `POST /api/camera/:id/preview/stop`
   - 实现 `GET /api/camera/options`（轻量级摄像头下拉列表供画格选择）；
   - 注册路由并完成操作日志 `actionI18nMap` 与单测闭环；
5. **后端质量检查**：
   - 运行 `go test ./...` 与 `go vet ./...` 确保全部通过。

### 阶段三：Vue 3 前端自适应多画格监控与播放器 (Frontend UI & Player)
1. **前端播放器引入与封装 (`VideoPlayer.vue`)**：
   - 引入支持 H.264 硬件直解与 H.265 自适应解码的 Web 播放器（如 `jessibuca` / `mpegts.js`）；
   - 实现主/子码流动态切换、自动断流重连、画面截图（Canvas 下载）、全屏与静音控制；
   - 画面清晰标注「主码流·超清」或「子码流·流畅」徽标；
2. **实时预览主页 (`views/live/index.vue`)**：
   - 实现 1（默认） / 4 / 9 分屏网格布局；
   - 1 分屏模式下自动拉取「主码流」；4 / 9 分屏模式下自动批量拉取「子码流」；
   - 支持双击画格快速放大至 1 分屏并无缝切为主码流，再次双击切回原分屏并降回子码流；
   - 实现空画格选择摄像头绑定与更换；
   - 在 `onUnmounted` 中彻底注销所有画格播放器实例；
3. **摄像头管理页面联动 (`views/resource/camera/index.vue`)**：
   - 列表操作列增加「实时预览」操作按钮，弹出主码流高清预览 Modal；
   - 摄像头表单中展示自动推导的子码流地址，并支持高级自定义修改；
4. **路由、菜单与三语国际化**：
   - 配置 `/live` 顶层路由（`affix=true`, `keepAlive=false`, `hideChildrenInMenu=true`）；
   - 在中/英/繁语言包中补齐实时预览、分屏、码率、流切换等所有文案；
5. **前端质量门禁**：
   - 运行 `pnpm check` 与 `pnpm test:unit` 验证。

---

## 2. 验证与回归清单 (Verification Checklist)

- [ ] C++ 单元测试与 Sanitizer 门禁：`make -C engine test && make -C engine asan`
- [ ] Go 单元测试与代码检查：`cd app && go test ./... && go vet ./...`
- [ ] 前端类型与静态检查：`cd ui && pnpm check && pnpm test:unit`
- [ ] 全链路实流联调验证：启动模拟推流服务，验证 1 分屏（主码流）与 4/9 分屏（子码流）自动切换与秒开播放。
