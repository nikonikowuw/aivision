# 人员人脸注册与多脸管理实施计划

## 实施阶段总览

| 阶段 | 目标 | 核心产出 | 验证方式 |
| :--- | :--- | :--- | :--- |
| **Phase 1** | Proto 契约与 Go 桩代码生成 | `person.proto`, `person.pb.go`, `person_grpc.pb.go` | `argus/scripts/generate-proto.sh`, `go test` |
| **Phase 2** | C++ 引擎单脸特征提取与切图 | `uds_server.cpp`, `person_service_test.cpp` | `make -C engine test` |
| **Phase 3** | 存储层扩展 (`Get` / `Delete`) | `storage.go`, `local.go`, `minio.go`, `storage_test.go` | `go test ./internal/pkg/storage/...` |
| **Phase 4** | 数据库模型、Migration 与 Repository | `person_face_sample.go`, `000028_*.sql`, `repository/` | `go test ./internal/model/... ./internal/repository/...` |
| **Phase 5** | Go IPC Client、业务错误码与 Service 业务逻辑 | `engineipc/client.go`, `errno.go`, `service/person.go` | `go test ./internal/service/...` |
| **Phase 6** | API Handler、路由挂载与 Wire DI 装配 | `api/person.go`, `router.go`, `wire_gen.go` | `make wire && make test && make vet` |
| **Phase 7** | 前端 API、人脸管理弹窗与国际化 | `person.ts`, `FaceManagementModal.vue`, `index.vue`, `resource.json` | `pnpm check` |
| **Phase 8** | 跨层集成验证与质量门禁 | 全链路测试、ASan 内存安全检查、代码风格检查 | 全套测试与静态分析通过 |

---

## 阶段细化任务

### Phase 1: Proto 契约与代码生成
1. 编辑 `engine/proto/argus/v1/person.proto`：
   - 增加 `NormalizedRect`、`ExtractFaceFeatureRequest`、`ExtractFaceFeatureResponse`；
   - 在 `PersonService` 中声明 `rpc ExtractFaceFeature(ExtractFaceFeatureRequest) returns (ExtractFaceFeatureResponse);`；
2. 运行 `argus/scripts/generate-proto.sh` 重新生成 Go 桩代码；
3. 检查生成的 `person.pb.go` 与 `person_grpc.pb.go`。

*验证检查项*：
- `git status` 显示 proto 生成文件更新；
- Go 代码编译无语法错误。

---

### Phase 2: C++ 引擎单脸特征提取与切图
1. 在 `engine/src/core/ipc/uds_server.cpp` 中实现 `PersonServiceImpl::ExtractFaceFeature`：
   - 校验图片请求体大小 `<= 10MB`；
   - 使用平台 ImageIO / CoreGraphics 从内存字节解码图片为 24-bit RGB；
   - 校验解码宽高 `<= 3840x2160` 且总像素 `<= 8,294,400`；
   - 调度活跃的 `face_recognition` 算法包执行 SCRFD 人脸检测与 5 关键点定位；
   - 严格进行单人脸判定（0 人脸 -> `NO_FACE_DETECTED`，>1 人脸 -> `MULTIPLE_FACES_DETECTED`）；
   - 人脸质量门槛判定（置信度 `>= 0.50`，短边 `>= 40px`，综合质量 `>= 35.0`，不达标返回 `FACE_QUALITY_TOO_LOW` / `FACE_TOO_SMALL`）；
   - 五点相似变换几何对齐生成 `112x112` RGB 图像；
   - 运行 GLINTR 模型提取 512 维特征向量，校验有限值并执行 L2 归一化，序列化为 2048 字节小端 raw bytes；
   - 将 112x112 特写图编码为高质量 JPEG（quality 90%）；
2. 编写 `engine/tests/` 下针对 `ExtractFaceFeature` 的单元测试。

*验证检查项*：
- `make -C engine test` 全部通过。

---

### Phase 3: 存储层扩展 (`Get` / `Delete`)
1. 在 `argus/internal/pkg/storage/storage.go` 中扩展 `FileStorage` 接口：
   - 增加 `Get(ctx context.Context, key string) (io.ReadCloser, int64, string, error)`；
   - 增加 `Delete(ctx context.Context, key string) error`；
2. 在 `local.go`、`minio.go`、`nop.go` 中实现 `Get` 与 `Delete`；
3. 保持路径穿越防范（`validateKey`）及删除幂等性；
4. 补充 `storage_test.go` 与 `file_test.go`。

*验证检查项*：
- `go test -v -race ./internal/pkg/storage/...` 全部通过。

---

### Phase 4: 数据库模型、Migration 与 Repository
1. 新建 `argus/internal/model/person_face_sample.go`，定义 `PersonFaceSample` 模型与表名 `person_face_samples`；
2. 在 `argus/internal/model/migrate.go` 中将 `PersonFaceSample` 加入 `AutoMigrate` 列表；
3. 新增版本化 SQL 迁移文件：
   - `argus/migrations/000028_add_person_face_samples.up.sql`
   - `argus/migrations/000028_add_person_face_samples.down.sql`
4. 新建 `argus/internal/repository/person_face_sample.go`，定义 `PersonFaceSampleRepository` 接口及实现：
   - `Create(ctx, sample)`
   - `ListByPersonID(ctx, personId)`
   - `GetBySampleID(ctx, sampleId)`
   - `Delete(ctx, personId, sampleId)`
   - `DeleteByPersonID(ctx, personId)`
   - `CountByPersonID(ctx, personId)`
   - `CheckSHA256Conflict(ctx, sha256)`
5. 更新 `argus/internal/repository/person.go`，在删除人员时支持事务内软删关联的人脸样本并返回关联图片 key；
6. 编写 repository 单元测试。

*验证检查项*：
- `go test -v -race ./internal/model/... ./internal/repository/...` 全部通过。

---

### Phase 5: Go IPC Client、业务错误码与 Service 业务逻辑
1. 在 `argus/internal/pkg/engineipc/client.go` 中增加 `ExtractFaceFeature` 方法；
2. 在 `argus/internal/pkg/errno/errno.go` 中新增错误码（1401 ~ 1409）及三语映射；
3. 在 `argus/internal/middleware/error_handler.go` 中完善 HTTP 状态码映射；
4. 在 `argus/internal/service/person.go` 中新增人脸样本相关业务逻辑：
   - `ListFaceSamples(ctx, personId)`
   - `CreateFaceSample(ctx, personId, file, header)`
   - `DeleteFaceSample(ctx, personId, sampleId)`
   - `ReadOriginalImage(ctx, personId, sampleId)`
   - `ReadAlignedImage(ctx, personId, sampleId)`
   - 包含文件类型前置校验、SHA-256 去重、最多 10 张上限并发控制、Engine RPC 错误精准映射、向量合规校验、私有存储写入、DB 事务写入及存储补偿回滚机制；
5. 在人员删除逻辑中接入级联人脸样本软删与物理文件成对清理；
6. 编写 service 单元测试（使用 fake EngineClient 与 mock Storage）。

*验证检查项*：
- `go test -v -race ./internal/service/...` 全部通过。

---

### Phase 6: API Handler、路由挂载与 Wire DI 装配
1. 在 `argus/internal/api/person.go` 中实现人脸样本 API 控制器方法；
2. 在 `argus/internal/router/router.go` 中挂载 5 个新端点并注册 `PermMiddleware` 权限映射；
3. 调整 `argus/cmd/api/wire.go`（若有新增依赖），执行 `make -C argus wire` 重新生成 Wire；
4. 编写 API 单元测试 `argus/internal/api/person_test.go`。

*验证检查项*：
- `make -C argus wire && make -C argus test && make -C argus vet` 全部通过。

---

### Phase 7: 前端 API、人脸管理弹窗与国际化
1. 更新 `ui/apps/web-antd/src/api/core/person.ts`，增加人脸样本类型及 API 函数（`listPersonFacesApi`, `uploadPersonFaceApi`, `deletePersonFaceApi`, `getOriginalImageUrl`, `getAlignedImageUrl`）；
2. 创建 `ui/apps/web-antd/src/views/resource/person/components/FaceManagementModal.vue`：
   - 样本卡片网格展示（特写缩略图、评分徽标、版本、时间）；
   - 上传人脸照片（文件校验、进度反馈、错误提示）；
   - 预览原图（带 BBox 框）与特写图；
   - 删除确认与即时刷新；
3. 更新 `ui/apps/web-antd/src/views/resource/person/index.vue`，增加人脸样本数列与“人脸管理”操作入口；
4. 更新 `zh-CN/resource.json`、`en-US/resource.json`、`zh-TW/resource.json` 国际化资源。

*验证检查项*：
- `pnpm --filter web-antd check`（TypeScript 类型检查、oxlint、cspell）通过。

---

### Phase 8: 跨层集成验证与质量门禁
1. 执行后端全量测试与静态检查：`make -C argus test && make -C argus vet`；
2. 执行引擎构建与契约测试：`make -C engine test`；
3. 执行前端全量检查：`pnpm check`；
4. 使用 `trellis-check` 进行规范符合度与跨层数据流全面复核。
