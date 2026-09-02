# Product Requirements Document (PRD): 边缘设备存储清理与防爆盘策略系统 (Edge Storage Retention and Cleanup Strategy)

---

## 1. Executive Summary

- **Problem Statement (问题定义)**:
  边缘 AI 盒子（如嵌入式 eMMC、NVMe SSD）的存储空间极度有限。在高并发告警与抓拍场景下，未受控的 JPEG 图片和数据库记录会导致磁盘迅速耗尽（100% 满盘），引发 SQLite 写入失败（`database or disk is full`）、视频流硬解码丢帧卡顿、系统看门狗异常重启甚至 Linux 系统瘫痪“变砖”。
- **Proposed Solution (解决方案)**:
  构建由 Go 后端主导的“单一事实源”边缘存储全生命周期管理系统。采用“日常 TTL 周期巡检 + 磁盘高低水位线动态削峰 + 95% 极危熔断保护”的混合三级防御体系；实行“图文强绑定同生共死”的物理原子删除机制；配合分批让步（Chunked Pacing）与 SQLite Freelist 零写放大自复用，死守边缘设备的高可用与实时性底线。
- **Success Criteria (成功衡量标准)**:
  1. **零满盘宕机**：在极限突发告警压力测试下，磁盘使用率被严格压制在预设安全水位以内，系统可用性达到 99.99%。
  2. **主业务零卡顿**：清理执行期间，视频流解码延迟抖动 $< 5\%$，推理管道丢帧率为 0。
  3. **数据强一致性**：图片文件与数据库记录 100% 同步物理清理，悬空“死记录（有记录无图）”或“孤儿文件（有图无记录）”发生率为 0。
  4. **极危场景自愈**：磁盘达 95% 极危水位时，$< 100\text{ms}$ 内触发抓拍存盘熔断，释放后 $< 10\text{s}$ 内自动恢复。

---

## 2. User Experience & Functionality

### 2.1 User Personas (用户画像)
- **边缘运维工程师 (Edge DevOps / SRE)**：希望设备具备自愈能力，无需人工定期登录设备清理磁盘，且能实时监控设备磁盘状态与清理审计日志。
- **安防与监控操作员 (Security Operator)**：查看告警记录时必须保证“图文同在”，绝不接受有告警记录但图片 404 / 丢失的情况；希望可按项目要求配置告警保留天数（如 15 天或 30 天）。

### 2.2 User Stories & Acceptance Criteria

#### Story 1: 日常 TTL 周期清理 (Scheduled Routine Cleanup)
- **User Story**:
  *As an* 运维工程师，*I want* 系统在每天低峰期自动清理超出保留天数（如 30 天前）的过期数据，*so that* 数据库和磁盘保持健康紧凑，不堆积陈旧数据。
- **Acceptance Criteria**:
  - [ ] 后台定时巡检任务按配置周期（默认 `600s`）运行，在达到保留期限时触发。
  - [ ] 检索所有 `occurred_at < (Now - RetentionDays)` 的告警记录（`alarm_records`）、抓拍过车（`plate_observations`）、抓拍人脸（`face_observations`）以及操作审计日志（`operation_logs`）。
  - [ ] 严格按照“**先删除磁盘图片文件 $\to$ 后删除 SQLite 记录**”的防孤儿顺序执行。
  - [ ] 无论软删除标记如何，清理均执行真实物理 Hard Delete（`Unscoped().Delete()`），确切释放磁盘空间。

#### Story 2: 高低水位紧急削峰 (Watermark Emergency Cleanup)
- **User Story**:
  *As an* 运维工程师，*I want* 当突发告警风暴导致磁盘使用率超过高水位（默认 85%）时，系统自动按 FIFO 淘汰最早的历史数据直至降至安全低水位（默认 70%），*so that* 突发流量不会冲垮存储系统。
- **Acceptance Criteria**:
  - [ ] 基于 `syscall.Statfs` 定期采样存储根目录的物理磁盘使用百分比。
  - [ ] 当 $\text{DiskUsage} \ge \text{HighWatermarkPercent}$（85%）时，自动启动紧急削峰。
  - [ ] 采用分批渐进模式：每次查出最早 200 条记录与图片，批量删除物理文件，再批量物理删除 DB 记录。
  - [ ] 每批次删除后进行 `time.Sleep(50ms)` 让出磁盘 I/O，并重新采样磁盘占用。
  - [ ] 一旦 $\text{DiskUsage} \le \text{LowWatermarkPercent}$（70%）或所有可清理记录已清空，立即执行 Early Exit（提前终止），避免多余 I/O。

#### Story 3: 极危水位抓拍熔断保护 (Circuit Breaker at 95%)
- **User Story**:
  *As an* 系统架构师，*I want* 当磁盘达到 95% 极端满载时暂停抓拍图片存盘，*so that* 系统不会因写满而崩溃瘫痪，仍能保持核心视频分析和纯文本告警。
- **Acceptance Criteria**:
  - [ ] 当 $\text{DiskUsage} \ge 95\%$ 时，触发系统 `StorageCircuitBreaker.Trip()`。
  - [ ] 熔断激活期间：AI 推理与 UDS 告警上报继续运转，但 Engine / App 丢弃抓拍图片 JPEG 编码落盘（图片路径置空），仅保存轻量纯文本告警元数据。
  - [ ] 产生高危系统告警事件并通过 WebSocket/API 广播通知前端。
  - [ ] 当磁盘降回 $< 85\%$ 时，熔断器自动重置（Reset），恢复抓拍图片正常存盘。

#### Story 4: 资产白名单保护 (Whitelist Asset Protection)
- **User Story**:
  *As an* 业务管理员，*I want* 人员底库和人脸特征库不受任何自动清理策略影响，*so that* 业务核心人脸比对底库不会被误删。
- **Acceptance Criteria**:
  - [ ] `persons`、`person_faces` 表及对应的底库特征照片路径（`raw_image_key`, `aligned_face_key`）享有**绝对免死白名单**，自动清理 Worker 严禁触碰。

#### Story 5: 动态配置与可观测性 (Dynamic Config & Observability)
- **User Story**:
  *As an* 管理员，*I want* 在 Web 控制台查看当前磁盘总/已用/可用容量，并能动态调整保留天数与水位阈值，*so that* 无需重启服务即可根据现场硬件配置自适应调整。
- **Acceptance Criteria**:
  - [ ] 提供 `GET /api/v1/system/storage/status` 接口，返回磁盘总量、已用、剩余、使用率、各表记录数及当前运行状态（Normal / Cleaning / Degraded）。
  - [ ] 提供 `GET /api/v1/system/storage/config` 与 `PUT /api/v1/system/storage/config` 接口，支持读取和动态更新 `retention_days`、`high_watermark_percent`、`low_watermark_percent`、`check_interval_seconds`。
  - [ ] 参数修改后保存至 `system_configs` 表并即时热加载生效。
  - [ ] 每次清理执行后输出结构化审计日志（记录耗时、删除图片数、删除记录数、释放空间）。

### 2.3 Non-Goals (明确不做的范围)
- **不实现全量 SQLite `VACUUM`**：避免 2 倍临时空间暴击和全库排他锁写放大，全量依赖 SQLite Freelist 自动复用。
- **不实现图片压缩转码降质归档**：边缘设备算力有限，不消耗额外 NPU/CPU 资源将 JPEG 重新压缩为缩略图或低画质格式。
- **不提供外部云端或 NAS 自动转存上传**：本阶段专注于单机边缘闭环自愈清理，异地备份属于多机级联调度范围。

---

## 3. System Architecture & Technical Specifications

### 3.1 Architecture Diagram (系统架构与数据流)

```
                            +-------------------------------------------+
                            |          Go Backend Cleaner Worker        |
                            | (Goroutine with Context & Ticker Channel) |
                            +---------------------+---------------------+
                                                  |
                        +-------------------------+-------------------------+
                        |                                                   |
                        v                                                   v
           [ Routine TTL Timer ]                               [ Watermark Poller ]
        (OccurredAt < Now - TTL)                         (syscall.Statfs Usage >= 85%)
                        |                                                   |
                        +-------------------------+-------------------------+
                                                  |
                                                  v
                                    +---------------------------+
                                    | Fetch Oldest Batch (200)  | <--- (alarm_records / observations)
                                    +-------------+-------------+
                                                  |
                                                  v
                                    +---------------------------+
                                    | Step 1: Delete Files      | ---> Storage / os.Remove(ImageRelPath)
                                    +-------------+-------------+
                                                  |
                                                  v
                                    +---------------------------+
                                    | Step 2: Hard Delete Rows  | ---> SQLite: db.Unscoped().Delete()
                                    +-------------+-------------+
                                                  |
                                                  v
                                    +---------------------------+
                                    | Step 3: Pacing Sleep 50ms | ---> Yield I/O to Video/AI Engine
                                    +-------------+-------------+
                                                  |
                                                  v
                                    +---------------------------+
                                    | Re-sample Statfs & Loop   | ---> Stop when Usage <= 70% (Early Exit)
                                    +---------------------------+
```

### 3.2 Data Flow & Deletion Order (时序与容错)
1. **防孤儿时序**：先删物理图片文件，再删数据库记录。
   - 若删除文件后系统断电：DB 记录依然存在，下次巡检由于文件已不存在（`os.IsNotExist` 视为成功），会顺利清理 DB 记录，自愈完成。
   - 若先删 DB 记录后断电：磁盘文件丢失 DB 外键索引，将沦为无法追踪的孤儿文件。因此**严禁先删 DB**。
2. **多目标引用处理**：
   - 告警和抓拍事件按照批次时间整体推进，同批次全景图在同批记录清理完成后统一物理删除，避免引用悬空。

### 3.3 Integration Points (接口与协议)

#### Data Model Extensions (`internal/model/system_config.go`):
- `storage_retention_days` (`int`, 默认 `30`)
- `storage_high_watermark` (`int`, 默认 `85`)
- `storage_low_watermark` (`int`, 默认 `70`)
- `storage_check_interval` (`int`, 默认 `600`)

#### REST API Endpoints:
- `GET /api/v1/system/storage/status`
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
      "status": "cleaning",
      "circuitBreakerActive": false
    },
    "message": "success"
  }
  ```
- `GET /api/v1/system/storage/config`
- `PUT /api/v1/system/storage/config`
  ```json
  {
    "retentionDays": 30,
    "highWatermarkPercent": 85,
    "lowWatermarkPercent": 70,
    "checkIntervalSeconds": 600
  }
  ```

---

## 4. Risks & Mitigation Strategies (风险与对策)

| 风险项 | 严重级 | 影响描述 | 缓解与防范措施 |
| :--- | :--- | :--- | :--- |
| **I/O 突发阻塞视频流** | **High** | 大量文件并发删除导致磁盘 IOPS 打满，视频硬解码掉帧 | 采用 `BatchSize=200` + `50ms` 休眠限速；单协程串行删除，杜绝瞬时高并发 I/O 尖刺。 |
| **误删底库资产** | **Critical** | 误把注册人员人脸图片当成抓拍图片删除 | 设立硬性白名单，Worker 作用域严格限定在 `alarm_records`, `plate_observations`, `face_observations`, `operation_logs`。 |
| **断电导致数据不一致** | **Medium** | 删除文件过程中设备突然拔电重启 | 采用“先文件、后 DB”策略，文件删除具备幂等性，系统重启后自动对账并恢复。 |
| **SQLite 磁盘不缩小** | **Low** | 物理删除 DB 记录后 `.db` 文件体积未减小 | 依赖 SQLite Freelist 空闲页池自动复用新数据，避免执行高危的 `VACUUM` 重写。 |

---

## 5. Phased Roadmap & Milestones (阶段演进路线)

- **Phase 1: 核心引擎与后台 Worker (MVP)**
  - 实现 `StorageMonitor` 磁盘状态采样器（支持 macOS 与 Linux `statfs` 跨平台适配）。
  - 实现 `StorageCleanupWorker`：支持 TTL 清理、高低水位分批削峰、`50ms` I/O 让步、防孤儿删除逻辑。
  - 支持 `alarm_records`、`plate_observations`、`face_observations`、`operation_logs` 物理清理与底库白名单保护。
  - 单元测试与端到端模拟测试（覆盖 TTL、高水位削峰、Early Exit、幂等性）。
- **Phase 2: 动态配置与管理 API**
  - 实现 `SystemConfig` 存储配置持久化与热加载。
  - 暴露 `status` 与 `config` REST API 及参数校验。
  - 集成极危 95% 抓拍图片熔断保护。
- **Phase 3: 前端监控面板与报警集成**
  - 在前端“系统设置/存储管理”页面呈现磁盘仪表盘、容量图表与参数配置表单。
  - 接入极危熔断全局警告 Toast/Banner 提示。
