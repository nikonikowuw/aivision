# 边缘设备存储清理与防爆盘策略技术设计 (Technical Design)

---

## 1. 设计目标与系统边界

### 1.1 设计目标
本设计旨在为边缘 AI 视频分析系统（Argus）构建工业级存储自愈与防爆盘机制：
1. **三级防御体系**：通过“日常 TTL 周期巡检 + 磁盘高低水位线紧急削峰 + 95% 极危熔断保护”，确保边缘设备在任何高并发告警风暴下永不爆盘宕机。
2. **图文强绑定同生共死**：保证告警/抓拍记录与磁盘 JPEG 证据图片作为不可分割的“证据单元”同步物理硬删除，杜绝“有记录无图片”的死记录。
3. **防孤儿时序**：严格按照“先物理删除磁盘图片文件 $\to$ 后删除 SQLite 记录”的顺序，即使发生突发断电重启也能自愈，杜绝无主孤儿文件。
4. **边缘 I/O 零影响**：采用 200 条分批步进与 50ms 间歇让步（Chunked Paced Deletion），杜绝瞬时 I/O 尖刺，确保主业务视频取流、硬件解码与 AI 推理零卡顿、零掉帧。
5. **SQLite Freelist 零写放大复用**：杜绝在快满盘时执行高危全量 `VACUUM`，依靠 SQLite 自身空闲页池自动复用新数据。
6. **动态配置与状态可观测**：支持配置热更新、磁盘容量与状态查询 API 及审计日志输出。

### 1.2 系统边界与白名单
- **清理覆盖范围**：
  - `alarm_records`：告警记录及 `image_rel_path` 图片文件；
  - `plate_observations`：车牌识别记录及 `image_rel_path`（全景图）、`plate_image_rel_path`（特写图）；
  - `face_observations`：人脸抓拍记录及 `image_rel_path`（全景图）、`face_image_rel_path`（特写图）；
  - `operation_logs`：系统操作审计日志（纯文本 DB 记录，仅受 TTL 保留期约束）。
- **受保护免死资产（绝对白名单）**：
  - `persons`、`person_faces` 及底库照片（`raw_image_key`、`aligned_face_key`）严禁被任何自动清理任务触碰。
- **依赖方向**：
  - Go 后端（Control Plane）作为**单一事实源（Single Source of Truth）**全权主导；
  - C++ Engine 通过 Local Storage 共享目录保持一致性，并在极危熔断时感知丢弃图片写入。

---

## 2. 系统架构与分层职责

```
                          ┌─────────────────────────────────────────────────────────┐
                          │               cmd/api (Lifecycle Manager)               │
                          └────────────────────────────┬────────────────────────────┘
                                                       │ Start(ctx) / Stop()
                                                       ▼
                          ┌─────────────────────────────────────────────────────────┐
                          │       service.StorageCleanupWorker (后台守护协程)         │
                          └──────┬─────────────────────┬────────────────────┬───────┘
                                 │ 磁盘监控             │ 定时 Ticker        │ 状态上报
                                 ▼                     ▼                    ▼
                          ┌──────────────┐      ┌──────────────┐     ┌──────────────┐
                          │ Statfs 采样器 │      │  TTL / 水位   │     │ 熔断状态机    │
                          └──────────────┘      │  决策执行器   │     │ (CircuitBrk) │
                                                └──────┬───────┘     └──────────────┘
                                                       │
                                                       ▼
                          ┌─────────────────────────────────────────────────────────┐
                          │               repository / storage 分批调用               │
                          └──────────┬──────────────────────────────────┬───────────┘
                                     │ 1. 查旧记录 & 删图片              │ 2. 物理删 DB 记录
                                     ▼                                  ▼
                          ┌─────────────────────┐            ┌──────────────────────┐
                          │ pkg/storage (Local) │            │ SQLite GORM Repos    │
                          │  os.Remove(img)     │            │ Unscoped().Delete()  │
                          └─────────────────────┘            └──────────────────────┘
```

### 2.1 模块分工

| 层次 | 模块 / 文件 | 职责描述 |
| :--- | :--- | :--- |
| **基础工具层** | `internal/pkg/storage/disk_usage.go` | 封装跨平台（Linux `syscall.Statfs_t` / Darwin `syscall.Statfs_t`）磁盘总容量、可用容量与使用百分比采样器。 |
| **数据模型层** | `internal/model/system_config.go` | 新增 `ConfigKeyStorageRetention` 键与 `StorageRetentionConfig` 结构体定义。 |
| **数据访问层** | `internal/repository/*` | 为 `alarm_record`, `plate_observation`, `face_observation`, `operation_log` 提供 `FindOldestBatch`、`HardDeleteBatch`、`Count` 接口。 |
| **业务服务层** | `internal/service/storage_cleanup.go` | 实现后台 Worker、三级防御算法（TTL、高低水位削峰、95% 熔断）、Chunked Pacing 流控、动态配置加载与状态查询。 |
| **API 交互层** | `internal/api/storage_cleanup.go` | 暴露 `GET /status`, `GET /config`, `PUT /config` 处理函数，执行输入校验与 RBAC 鉴权。 |
| **路由装配层** | `internal/router/router.go` | 注册存储管理路由组；在 `cmd/api/main.go` 中注册 Worker 的生命周期启动与优雅关闭。 |

---

## 3. 核心机制与算法详细设计

### 3.1 磁盘采样器 (`DiskUsageSampler`)
在 Go 中通过 `syscall.Statfs(path, &stat)` 采集指定挂载点或存储目录的状态：
$$\text{TotalBytes} = \text{stat.Blocks} \times \text{uint64}(\text{stat.Bsize})$$
$$\text{FreeBytes} = \text{stat.Bavail} \times \text{uint64}(\text{stat.Bsize})$$
$$\text{UsedBytes} = \text{TotalBytes} - \text{FreeBytes}$$
$$\text{UsagePercent} = \frac{\text{UsedBytes}}{\text{TotalBytes}} \times 100.0$$

> **跨平台兼容**：在 Linux 上 `Bsize` 为 `int64`/`uint64`，在 Darwin (macOS) 上为 `uint32`。通过 build tags 或标准类型转换保证编译一致性。

### 3.2 决策执行流与状态机

```
                        +-------------------------------+
                        |  Worker Loop (Interval Ticker)|
                        +---------------+---------------+
                                        |
                                        v
                        +-------------------------------+
                        | Sample Disk Usage via Statfs  |
                        +---------------+---------------+
                                        |
                 +----------------------+-----------------------+
                 |                                              |
      [ Usage >= 95% ]                                [ Usage < 95% ]
                 |                                              |
                 v                                              v
    +-------------------------+                    +-------------------------+
    | Set CircuitBreaker=ON   |                    | If CircuitBreaker==ON:  |
    | Log Emergency Warning   |                    |   Set CircuitBreaker=OFF|
    +------------+------------+                    +------------+------------+
                 |                                              |
                 +----------------------+-----------------------+
                                        |
                 +----------------------+-----------------------+
                 |                                              |
      [ Usage >= 85% (HighWM) ]                       [ Usage < 85% ]
                 |                                              |
                 v                                              v
    +-------------------------+                    +-------------------------+
    | Enter Emergency Mode:   |                    | Enter Routine Mode:     |
    | Run FIFO Cleanup Loop   |                    | Run TTL Expiration Check|
    | until Usage <= 70%      |                    | (OccurredAt < Now - TTL)|
    +-------------------------+                    +-------------------------+
```

### 3.3 分批渐进与让步流控算法 (Chunked Paced Cleanup)

为避免 I/O 阻塞视频主业务，任何清理均严禁一次性全量加载或全量删除。

```go
func (w *StorageCleanupWorker) purgeOldestRecords(ctx context.Context, targetLowPercent float64) error {
    const batchSize = 200
    const paceInterval = 50 * time.Millisecond

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // 1. 检查当前磁盘水位
        usage, err := w.sampler.GetDiskUsage()
        if err != nil {
            return err
        }
        if usage.UsagePercent <= targetLowPercent {
            w.log.Info("watermark reached low threshold, early exiting",
                zap.Float64("current", usage.UsagePercent),
                zap.Float64("target", targetLowPercent))
            break
        }

        // 2. 依次按时间最早查询各表一批记录
        cleanedCount := 0
        
        // 告警记录清理
        n1, err := w.cleanOldestAlarmRecords(ctx, batchSize)
        if err != nil { return err }
        cleanedCount += n1

        // 过车记录清理
        n2, err := w.cleanOldestPlateObservations(ctx, batchSize)
        if err != nil { return err }
        cleanedCount += n2

        // 人脸抓拍记录清理
        n3, err := w.cleanOldestFaceObservations(ctx, batchSize)
        if err != nil { return err }
        cleanedCount += n3

        if cleanedCount == 0 {
            // 已无更多可清理的业务记录
            w.log.Warn("no more deletable records available, disk remains above low watermark",
                zap.Float64("current_usage", usage.UsagePercent))
            break
        }

        // 3. I/O 让步休眠，释放磁盘带宽给视频硬解与抓拍
        time.Sleep(paceInterval)
    }
    return nil
}
```

### 3.4 图文强绑定与防孤儿单批处理逻辑
以 `AlarmRecord` 为例：
1. `records, err := repo.FindOldestBatch(ctx, limit)`（按 `occurred_at ASC` 排序）；
2. 提取本批次涉及的所有 `image_rel_path`；
3. **步骤一（物理删文件）**：遍历图片路径，调用 `storage.Delete(ctx, relPath)`。若文件已不存在（`os.IsNotExist`），忽略并继续；
4. **步骤二（物理删记录）**：提取记录 `ID` 列表，执行 `db.Unscoped().Where("id IN ?", ids).Delete(&model.AlarmRecord{})`；
5. 返回实际删除记录数。

---

## 4. 接口与数据契约规范

### 4.1 数据模型与配置定义

```go
// internal/model/system_config.go
const (
    ConfigKeyStorageRetention = "system:storage:retention"
)

// StorageRetentionConfigValue 存储清理策略配置 DTO
type StorageRetentionConfigValue struct {
    RetentionDays        int  `json:"retentionDays"`        // 常规保留天数 (1~365, 默认 30)
    HighWatermarkPercent int  `json:"highWatermarkPercent"` // 高水位触发阈值 (50~95, 默认 85)
    LowWatermarkPercent  int  `json:"lowWatermarkPercent"`  // 低水位目标阈值 (30~90, 默认 70)
    CheckIntervalSeconds int  `json:"checkIntervalSeconds"` // 巡检周期秒数 (60~3600, 默认 600)
    AutoCleanupEnabled   bool `json:"autoCleanupEnabled"`   // 是否启用自动清理 (默认 true)
}
```

### 4.2 REST API 契约

#### 1. 查询存储状态与统计
- **Route**: `GET /api/v1/system/storage/status`
- **Permission**: `system:storage:view`
- **Response**:
```json
{
  "code": 0,
  "data": {
    "totalBytes": 64424509440,
    "usedBytes": 54760833024,
    "freeBytes": 9663676416,
    "usagePercent": 85.0,
    "alarmRecordCount": 128400,
    "plateObservationCount": 42100,
    "faceObservationCount": 35600,
    "operationLogCount": 12500,
    "status": "cleaning",
    "circuitBreakerActive": false,
    "lastCleanupAt": "2026-09-02T03:00:00Z",
    "lastFreedBytes": 524288000
  },
  "message": "success"
}
```

#### 2. 获取存储清理配置
- **Route**: `GET /api/v1/system/storage/config`
- **Permission**: `system:storage:view`
- **Response**:
```json
{
  "code": 0,
  "data": {
    "retentionDays": 30,
    "highWatermarkPercent": 85,
    "lowWatermarkPercent": 70,
    "checkIntervalSeconds": 600,
    "autoCleanupEnabled": true
  },
  "message": "success"
}
```

#### 3. 修改存储清理配置
- **Route**: `PUT /api/v1/system/storage/config`
- **Permission**: `system:storage:config`
- **Request Body**:
```json
{
  "retentionDays": 15,
  "highWatermarkPercent": 80,
  "lowWatermarkPercent": 65,
  "checkIntervalSeconds": 300,
  "autoCleanupEnabled": true
}
```
- **Validation Rules**:
  - `retentionDays`: $\in [1, 365]$
  - `highWatermarkPercent`: $\in [50, 95]$
  - `lowWatermarkPercent`: $\in [30, 90]$ 且必须 $< \text{highWatermarkPercent}$
  - `checkIntervalSeconds`: $\in [30, 86400]$

---

## 5. 质量保证与安全考量

1. **白名单安全测试**：
   - 编写单元测试验证：即使磁盘使用率达到 99%，自动清理 Worker 也绝不会查询或删除 `persons`、`person_faces` 表及底库特征图片。
2. **幂等性与容错测试**：
   - 模拟磁盘文件已被手动删除或部分缺失场景，验证 Worker 不会 panic，并能正常清除 DB 中的悬空元数据。
3. **优雅停机（Graceful Shutdown）**：
   - Worker 监听 `context.Context`，在收到 `SIGINT`/`SIGTERM` 时，完成当前正在执行的单次 `batch`（$< 50\text{ms}$）后立即安全退出，绝不留半事务状态。
