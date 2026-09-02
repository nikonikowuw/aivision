# Implementation Plan - 人脸抓拍记录与多帧时序演进

## Phase 1: 通信契约与 Protobuf 扩展
- [x] 1.1 修改 `engine/proto/argus/v1/app.proto`，添加 `FaceCaptureSnapshot`、`FaceCapture`、`ReportFaceCaptureRequest`、`ReportFaceCaptureResponse` 及 RPC `ReportFaceCapture`。
- [x] 1.2 执行 `cd argus && make proto` 生成 Go Protobuf 代码并进行 `proto-check`。

## Phase 2: 数据库迁移与模型定义
- [x] 2.1 创建 `argus/migrations/000034_add_face_captures.up.sql` 及 `.down.sql`。
- [x] 2.2 创建 `argus/migrations/000035_seed_face_capture_menu.up.sql` 及 `.down.sql`。
- [x] 2.3 创建 `argus/internal/model/face_capture.go`，并在 `migrate.go` 中注册模型。
- [x] 2.4 执行 `cd argus && make migrate-up` 验证迁移。

## Phase 3: Go 存储层、业务服务与 HTTP/gRPC 端点
- [x] 3.1 创建 `argus/internal/repository/face_capture.go` 及其单测 `face_capture_test.go`。
- [x] 3.2 创建 `argus/internal/service/face_capture.go` 及其单测 `face_capture_test.go`。
- [x] 3.3 在 `argus/internal/service/report_adapter.go` 中实现 `ReportFaceCapture` gRPC 接口并添加单测。
- [x] 3.4 创建 `argus/internal/api/face_capture.go` 控制器（分页、详情、大图小图流端点）。
- [x] 3.5 注册路由 `internal/router/router.go`，更新 `wire.go` 并执行 `make wire`。

## Phase 4: C++ 媒体推理引擎抓拍与上报流水线
- [x] 4.1 更新 `engine/include/argus/core/uds_ipc.hpp` 和 `engine/src/core/ipc/uds_client.cpp` 中的 `report_face_capture` 客户端接口。
- [x] 4.2 修改 `engine/src/core/ipc/uds_server.cpp`，实现全量人脸跟踪抽样（最多 5 组快照，时间步长与质量跃升判定），增量上报 `ReportFaceCapture` 并复用图片路径触发 `ReportFaceObservation`。
- [x] 4.3 编写/更新 C++ 单元测试（`engine/tests/unit/`），验证快照状态机与上报行为。

## Phase 5: 前端抓拍记录与时序胶卷抽屉
- [x] 5.1 增加前端 API 客户端 `ui/apps/web-antd/src/api/core/capture.ts` 与模型类型。
- [x] 5.2 增加中英文 i18n 语言包（`record.capture.*`）。
- [x] 5.3 实现抓拍记录列表页 `ui/apps/web-antd/src/views/record/capture/index.vue`。
- [x] 5.4 实现时序胶卷抽屉 `ui/apps/web-antd/src/views/record/capture/components/CaptureFilmstripDrawer.vue` 与陌生人一键快速建档联动。

## Phase 6: 全链路测试与质量门禁
- [x] 6.1 `cd argus && make test && make vet`
- [x] 6.2 `make -C engine configure && make -C engine build && make -C engine test && make -C engine lint`
- [x] 6.3 `cd ui && pnpm check`
