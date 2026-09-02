# 通用抓拍记录实施执行计划 (Implementation Plan)

---

## 阶段 1：数据库迁移与 Protobuf IPC 契约升级 (Database & IPC)

- [ ] **1.1 编写 SQL 迁移脚本**
  - 创建 `app/migrations/000037_create_generic_captures.up.sql`（`captures` 表及 6 个核心 B-Tree 索引）与 `.down.sql`；
  - 创建 `app/migrations/000038_seed_generic_capture_menu.up.sql`（`/record/capture` 菜单、`record:capture:read` 权限）与 `.down.sql`；
  - 验证：在 `app/` 目录下执行 `make migrate-up` 成功应用迁移。
- [ ] **1.2 升级 gRPC Protobuf 契约**
  - 修改 `engine/proto/argus/v1/app.proto`：新增 `CaptureEvent` 结构体与 `ReportCapture` RPC 声明，支持 `sub_bbox`、`sub_crop` 与多态 `target_type`；
  - 重新编译生成 Go 代码：`cd app && make wire` 或对应 proto 生成指令；
  - 验证：Go proto 文件包含 `ReportCaptureServer` 与 `CaptureEvent` 定义。

---

## 阶段 2：Go 后端数据访问、业务服务与存储适配 (Backend Implementation)

- [ ] **2.1 新增 GORM 模型与属性结构**
  - 在 `app/internal/model/capture.go` 中定义 `CaptureRecord`、`FaceAttributes`、`VehicleAttributes`、`PersonAttributes`；
  - 编写模型序列化/反序列化单元测试。
- [ ] **2.2 实现 Repository 层**
  - 在 `app/internal/repository/capture_repository.go` 中实现：
    - `Create(ctx, record)`
    - `FindPage(ctx, filter)` (支持 target_type, camera_id, time_range, keyword)
    - `FindByID(ctx, id)`
    - `FindExpired(ctx, cutoff, limit)`
    - `FindOldestUnrecognized(ctx, limit)` (85% 优先削峰)
    - `FindOldest(ctx, limit)` (兜底削峰)
    - `HardDeleteBatch(ctx, ids)`
  - 验证：编写 `capture_repository_test.go`，测试通过。
- [ ] **2.3 实现 Service 层与 Adapter 改造**
  - 在 `app/internal/service/capture_service.go` 中实现分页查询、详情解析、多图流控安全读取（`ReadImageStream`）；
  - 在 `app/internal/service/report_adapter.go` 中实现 `AcceptCapture`，处理抓拍事件流落库与 `is_recognized` 关联标记；
  - 验证：编写 `capture_service_test.go` 与 `report_adapter_test.go`。
- [ ] **2.4 存储三级防御与解耦改造**
  - 修改 `app/internal/service/storage_cleanup.go`：
    - 将 `cleanFaceCaptureBatch` 替换为 `cleanCaptureBatch`；
    - 削峰逻辑优先调用 `captureRepo.FindOldestUnrecognized` 淘汰普通过客抓拍；
    - 验证：运行 `storage_cleanup_test.go`，确保抓拍清理不会级联影响 `observations` 记录。
- [ ] **2.5 API 控制器与路由装配**
  - 在 `app/internal/api/capture.go` 实现 `ListPage`, `GetDetail`, `ReadImage` 处理函数；
  - 在 `app/internal/router/router.go` 注册 `/api/record/captures` 路由组；
  - 验证：编写 `capture_api_test.go`，API 状态码与响应格式均符合规范。

---

## 阶段 3：C++ Engine 推理管道与自适应抓拍上报 (Engine Integration)

- [ ] **3.1 改造 Engine IPC 上报分发器**
  - 在 `engine/src/core/ipc/uds_server.cpp` 中新增 `ReportCapture` 通用分发处理逻辑；
  - 支持将各算法包回调的感知目标统一封装为 `CaptureEvent` 发送至 Go 后端。
- [ ] **3.2 适配人脸识别/人体联合流水线**
  - 在 `algo-packages/macos/arm64/face_recognition/src/core/algo_entry.cpp` 中：
    - 当 `enable_person_detection = true` 时，背影/无脸行人生成 `target_type=person`（主特写为全身切图）；
    - 正脸行人联合生成 `target_type=person`（同时绑定全身切图与人脸特写图）；
    - 纯人脸近景生成 `target_type=face`；
  - 验证：在 `engine/` 下运行 `make test`。

---

## 阶段 4：前端 Vue3 界面、组件与交互改造 (Frontend Implementation)

- [ ] **4.1 API 请求与 TypeScript 类型定义**
  - 更新 `apps/web-antd/src/api/core/capture.ts`：定义 `CaptureItem`, `CaptureQuery`, `CaptureDetail` 等强类型接口。
- [ ] **4.2 路由、权限与国际化**
  - 检查并更新菜单路由 `menu.record.captures` 与权限码 `record:capture:read`；
  - 在 `zh-CN.json` 与 `en-US.json` 中配置抓拍记录与各分类 Tag 的多语言词条。
- [ ] **4.3 抓拍记录列表主页 (`views/record/capture/index.vue`)**
  - 实现顶部 Segmented 分类胶囊（全部/人脸/机动车/行人）；
  - 实现自适应智能搜索框（Placeholder 无感切换，位置固定零跳动）；
  - 实现流式卡片网格（Card Grid）展示，自适应渲染不同目标分类的属性 Tag 与缩略图；
  - 支持右上角一键切换为 VxeGrid 数据表格模式。
- [ ] **4.4 抓拍详情抽屉 (`CaptureDetailDrawer.vue`)**
  - 实现全景大图 Canvas 预览器（高亮绘制人脸框与人体框）；
  - 实现全身切图与人脸高清特写并排对比预览；
  - 实现动态特征属性解析面板。

---

## 阶段 5：全链路质量检验与回归 (Verification & Check)

- [ ] **5.1 后端全量测试与代码检查**
  - `cd app && make vet`
  - `cd app && make test`
- [ ] **5.2 前端代码质量与类型检查**
  - `cd ui && pnpm check`
- [ ] **5.3 引擎与算法包回归**
  - `make -C engine test`
