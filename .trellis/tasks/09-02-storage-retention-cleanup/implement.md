# 边缘设备存储清理与防爆盘策略实施计划 (Implementation Plan)

---

## 1. 执行原则

- **测试驱动与防御性编码**：每个阶段先定义接口与编写失败测试用例，再进行业务实现。
- **白名单绝对隔离**：确保任何数据清理逻辑中绝不包含底库人员资产（`persons`/`person_faces`）。
- **零破坏性改动**：保持既有 API、报表与 UDS 上报通道的完全兼容，不引入任何 breaking changes。
- **构建质量闭环**：每阶段通过 `go test ./...`、`go vet ./...` 以及 `make wire` 代码生成验证。

---

## 2. 阶段一：基础设施与磁盘采样器 (Storage Sampler)

### 任务清单
- [ ] 创建 `argus/internal/pkg/storage/disk_usage.go`：
  - 定义 `DiskUsage` 结构体（`TotalBytes`, `FreeBytes`, `UsedBytes`, `UsagePercent`）；
  - 定义 `DiskUsageSampler` 接口；
  - 实现基于 `syscall.Statfs` 的跨平台 `statfsSampler`（兼容 Linux 与 macOS/Darwin）；
  - 实现用于单元测试的 `mockDiskUsageSampler`。
- [ ] 编写 `argus/internal/pkg/storage/disk_usage_test.go`：
  - 覆盖真实临时目录采样测试；
  - 覆盖计算百分比边界测试（0% / 100% / 溢出保护）；
  - 覆盖非法路径错误处理。

### 验证命令
```bash
cd argus && go test -v -race ./internal/pkg/storage
```

---

## 3. 阶段二：数据模型与 Repository 批量清理扩展 (Model & Repositories)

### 任务清单
- [ ] 扩展 `argus/internal/model/system_config.go`：
  - 新增常量 `ConfigKeyStorageRetention = "system:storage:retention"`；
  - 新增 `StorageRetentionConfigValue` 结构体与默认配置方法 `DefaultStorageRetentionConfig()`。
- [ ] 扩展各仓储接口与实现：
  - **`AlarmRecordRepository`**（`internal/repository/alarm_record.go`）：
    - `FindExpired(ctx, before time.Time, limit int) ([]model.AlarmRecord, error)`
    - `FindOldest(ctx, limit int) ([]model.AlarmRecord, error)`
    - `HardDeleteBatch(ctx, ids []uint64) error`
    - `CountTotal(ctx) (int64, error)`
  - **`PlateObservationRepository`**（`internal/repository/plate_observation.go`）：
    - `FindExpired(ctx, before time.Time, limit int) ([]model.PlateObservation, error)`
    - `FindOldest(ctx, limit int) ([]model.PlateObservation, error)`
    - `HardDeleteBatch(ctx, ids []uint64) error`
    - `CountTotal(ctx) (int64, error)`
  - **`FaceObservationRepository`**（`internal/repository/face_observation.go`）：
    - `FindExpired(ctx, before time.Time, limit int) ([]model.FaceObservation, error)`
    - `FindOldest(ctx, limit int) ([]model.FaceObservation, error)`
    - `HardDeleteBatch(ctx, ids []uint64) error`
    - `CountTotal(ctx) (int64, error)`
  - **`OperationLogRepository`**（`internal/repository/operation_log.go`）：
    - `DeleteExpired(ctx, before time.Time, limit int) (int64, error)`
    - `CountTotal(ctx) (int64, error)`
- [ ] 编写 Repository 单测（`*_test.go`）：
  - 验证物理 `HardDeleteBatch` 真正移除 SQLite 记录（软删除标记不阻碍物理清理）；
  - 验证按发生时间升序（`occurred_at ASC`）严格按 FIFO 返回最早批次。

### 验证命令
```bash
cd argus && go test -v ./internal/repository/...
```

---

## 4. 阶段三：核心清理服务与 Worker 实现 (Service & Worker)

### 任务清单
- [ ] 创建 `argus/internal/service/storage_cleanup.go` 与 `argus/internal/service/storage_config.go`：
  - 定义 `StorageCleanupService` 接口与 `StorageStatusDTO`；
  - 实现 `StorageCleanupWorker` 结构体，持有各 Repo、`storage.FileStorage`、`DiskUsageSampler`、`zap.Logger`；
  - **TTL 清理引擎**：按保留天数计算截止时间，分批拉取旧数据，执行“先删图片文件 $\to$ 后物理删 DB 记录”；
  - **水位线削峰引擎**：当磁盘 $\ge 85\%$ 时循环执行 FIFO 批次清理（每批 200 条），批次间 `50ms` 让步休眠，降至 $70\%$ 时 Early Exit；
  - **95% 极危熔断器**：维护原子布尔状态 `circuitBreakerActive`，对外提供查询与事件通知；
  - **生命周期控制**：实现 `Start(ctx context.Context)` 与 `Stop()`，支持平滑优雅退出。
- [ ] 编写 `argus/internal/service/storage_cleanup_test.go`：
  - 模拟 TTL 到期测试：验证图片文件被成功删除且 DB 记录被清除；
  - 模拟高水位削峰测试：通过 mock sampler 模拟 90% 磁盘占用，验证逐批清理并于 70% 触发 Early Exit；
  - 模拟断电容错测试：当图片文件不存在时，删除不报错且 DB 记录正常清理；
  - 模拟极危熔断测试：验证 95% 水位时状态机正确置位与复位；
  - 验证人员底库表零触碰（白名单测试）。

### 验证命令
```bash
cd argus && go test -v -race ./internal/service/...
```

---

## 5. 阶段四：API Handler、参数校验与路由注册 (API & Router)

### 任务清单
- [ ] 创建 `argus/internal/api/storage_cleanup.go`：
  - `GetStorageStatus(c *gin.Context)`：返回磁盘使用情况、各表记录数统计、清理状态与熔断状态；
  - `GetStorageConfig(c *gin.Context)`：获取当前保留天数与高低水位配置；
  - `UpdateStorageConfig(c *gin.Context)`：更新配置，包含严格范围校验（$1 \le \text{days} \le 365$, $30 \le \text{low} < \text{high} \le 95$）。
- [ ] 注册路由（`argus/internal/router/router.go`）：
  - 在 `/api/v1/system/storage` 下注册 `GET /status`, `GET /config`, `PUT /config`；
  - 绑定 RBAC 权限点：`system:storage:view` 与 `system:storage:config`。
- [ ] 编写 API 单元测试（`argus/internal/api/storage_cleanup_test.go`）：
  - 覆盖正常状态查询与配置修改；
  - 覆盖参数边界非法校验（如 low $\ge$ high、负数天数等返回 400 错误）。

### 验证命令
```bash
cd argus && go test -v ./internal/api/...
```

---

## 6. 阶段五：Wire 依赖注入与生命周期装配 (Wire & Lifecycle)

### 任务清单
- [ ] 更新 `argus/cmd/api/wire.go`：
  - 将 `DiskUsageSampler`、`StorageCleanupService`、`StorageCleanupWorker`、`StorageHandler` 纳入依赖注入 ProviderSet；
  - 重新生成 `wire_gen.go`。
- [ ] 更新 `argus/cmd/api/main.go`：
  - 在服务启动时启动 `storageWorker.Start(ctx)`；
  - 在监听到 `SIGINT`/`SIGTERM` 退出信号时，调用 `storageWorker.Stop()` 实现优雅停机。

### 验证命令
```bash
cd argus && make wire && go vet ./...
```

---

## 7. 阶段六：端到端质量验证与验收 (E2E & Quality Gate)

### 验收清单
- [ ] 执行全套单元测试与竞态检测：`go test -v -race ./...`。
- [ ] 执行静态检查：`go vet ./...`。
- [ ] 检查内存泄漏与 Goroutine 泄漏（Worker 退出后无残留 goroutine）。
- [ ] 验证端到端接口响应格式符合 `{code: 0, data: ..., message: "success"}` 标准。

### 验收命令
```bash
cd argus && make test && make vet
```
