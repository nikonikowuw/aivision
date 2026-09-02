# 文件存储规范

## Scenario: 文件上传与可替换存储

### 1. Scope / Trigger

- Trigger：新增跨 API、Service、文件系统/对象存储、配置和前端请求层的文件上传能力。
- Scope：`POST /api/file/upload` 单文件上传；支持本地文件系统和 MinIO 两个 `FileStorage` 实现。
- Out of scope：文件元数据表、删除、分片/断点续传、文件列表和权限化下载。

### 2. Signatures

```go
type FileStorage interface {
    Put(ctx context.Context, input PutInput) (StoredObject, error)
}
```

HTTP signature：

- `POST /api/file/upload`
- `Content-Type: multipart/form-data`
- file field：`file`
- 上传需要 `Authorization: Bearer <access-token>`；路由权限注册为 `PermCodeAuthenticated`。

前端 signature：

```ts
uploadFileApi(file: Blob | File): Promise<FileApi.UploadResult>
```

### 3. Contracts

请求与文件约束：

- 单文件默认上限 `10 MiB`，由 `APP_STORAGE_MAX_SIZE` / `storage.max_size` 配置覆盖。
- 允许 `.jpg`、`.jpeg`、`.png`、`.gif`、`.webp`、`.pdf`。
- 扩展名和文件头探测出的 MIME 必须匹配；不能只信任 multipart 的 `Content-Type`。
- 原始文件名只作为展示元数据，key 必须由日期目录和随机值生成，不能使用原始文件名作为路径。

成功响应：

```json
{
  "code": 0,
  "data": {
    "key": "2026/08/21/<random>.png",
    "name": "avatar.png",
    "size": 12345,
    "contentType": "image/png",
    "url": "/uploads/2026/08/21/<random>.png"
  },
  "message": "ok"
}
```

环境/配置键：

- `APP_STORAGE_DRIVER`：`local` 或 `minio`。
- `APP_STORAGE_MAX_SIZE`：正整数，字节数。
- local：`APP_STORAGE_LOCAL_ROOT`、`APP_STORAGE_LOCAL_URL_PREFIX`。
- MinIO：`APP_STORAGE_MINIO_ENDPOINT`、`APP_STORAGE_MINIO_ACCESS_KEY`、`APP_STORAGE_MINIO_SECRET_KEY`、`APP_STORAGE_MINIO_BUCKET`、`APP_STORAGE_MINIO_USE_SSL`、`APP_STORAGE_MINIO_PUBLIC_BASE_URL`。
- MinIO `public_base_url` 必须是无凭据、无 query/fragment 的 `http`/`https` URL；bucket 的公开读取策略由部署配置负责。
- local `url_prefix` 必须是非根绝对路径，不能包含 `//`、反斜杠、`.`、`..`、空路径段、query 或 fragment。

### 4. Validation & Error Matrix

| 条件 | 错误码 | HTTP 状态 |
| --- | --- | --- |
| 缺少 `file`、空文件、空/含路径分隔符的文件名、reader 不足声明大小 | `CodeInvalidParam` | 400 |
| 文件超过 `storage.max_size` | `CodeFileTooLarge` | 400 |
| 扩展名不允许或扩展名与实际 MIME 不匹配 | `CodeFileTypeNotAllowed` | 400 |
| local/MinIO 写入失败或配置未完成 | `CodeInternal`（响应不泄漏原因） | 500 |
| 没有 Bearer token | `CodeUnauthorized` | 401 |
| 上传写路由未注册权限 | `CodeForbidden` | 403 |

### 5. Good / Base / Bad Cases

- Good：认证请求上传真实 PNG，响应 `code=0`，返回随机 key，local URL 可读取完整原始字节。
- Good：切换 `storage.driver=minio` 后 Service/API 代码不变，MinIO `PutObject` 收到相同 key、size、content type。
- Base：请求缺少文件或文件为空，返回统一 `{code,data:null,message}`，不写入目标文件。
- Bad：把 `Filename` 拼接到 local root、使用客户端声明 MIME、把 MinIO secret 写入配置或把 multipart 原文写入操作日志。

### 6. Tests Required

- config：默认值、`APP_STORAGE_*` 覆盖、driver 必填项、public URL 和 local prefix 安全校验。
- storage：local 临时目录写入/读取、原子替换、reader/写入失败清理、key 路径拒绝；MinIO fake client 的参数、公开 URL 和错误传播。
- service：全部允许类型、MIME 不匹配、超限、空文件、路径文件名、头部回放和随机 key 不泄露原名。
- API：multipart 成功、缺少 file、业务错误和底层错误脱敏；至少一条真实 Handler → Service → local Storage 链路。
- router：上传路由未认证为 401、已认证请求经过 `PermCodeAuthenticated`，local 静态 URL 路由已注册。
- frontend：`uploadFileApi` 使用共享 `requestClient.upload`，由 `pnpm check` 完成类型检查。
- docs/deployment：Swagger 包含 `formData file` 和 Bearer security；compose 的 `/uploads/` URL 与后端 `url_prefix` 一致。

### 7. Wrong vs Correct

#### Wrong

```go
path := filepath.Join(root, header.Filename)
contentType := header.Header.Get("Content-Type")
```

这会允许路径穿越、重名覆盖，并信任客户端可伪造的 MIME。

#### Correct

```go
key := newRandomDateKey(extension)
head := readHeader(reader)
contentType := mimetype.Detect(head).String()
store.Put(ctx, storage.PutInput{
    Key: key, Reader: io.MultiReader(bytes.NewReader(head), reader), Size: size, ContentType: validatedType,
})
```

Service 先按扩展名和文件头校验，存储层只接受安全 key；local 实现使用临时文件和原子 rename，MinIO 实现只通过 `FileStorage` 暴露给上层。

---

## Scenario: 边缘存储生命周期保留、高低水位削峰与极危熔断

### 1. Scope / Trigger

- Trigger：边缘设备（eMMC/SSD）存储空间有限，高并发抓拍和告警引发磁盘满盘、写崩溃或丢帧。
- Scope：Go 后端单点自治的三级防御机制（日常 TTL + 高低水位 85%/70% FIFO 削峰 + 95% 极危抓拍熔断）；`alarm_records`, `plate_observations`, `face_observations`, `face_captures`, `operation_logs` 物理硬清理；底库资产绝对白名单保护；`/api/storage/status`, `/api/storage/config`, `/api/storage/cleanup` API。
- Out of scope：全量 SQLite VACUUM（依赖 Freelist 空闲页复用）、图片二次压缩转码。

### 2. Signatures

```go
type DiskUsageSampler interface {
    GetDiskUsage(path string) (DiskUsage, error)
}

type StorageCleanupService interface {
    GetStatus(ctx context.Context) (*StorageStatusDTO, error)
    GetConfig(ctx context.Context) (*model.StorageRetentionConfigValue, error)
    UpdateConfig(ctx context.Context, input *model.StorageRetentionConfigValue) error
    TriggerCleanup(ctx context.Context) error
    IsCircuitBreakerActive() bool
    Start(ctx context.Context)
    Stop()
}
```

HTTP Endpoints：
- `GET /api/storage/status` (权限：`ops:storage:read`)
- `GET /api/storage/config` (权限：`ops:storage:read`)
- `PUT /api/storage/config` (权限：`ops:storage:edit`)
- `POST /api/storage/cleanup` (权限：`ops:storage:edit`)

### 3. Contracts

- **防孤儿时序**：必须先物理删除磁盘上的图片文件（`storage.Delete`），后物理删除 SQLite 记录（`Unscoped().Delete()`）。即使文件已不存在（`os.IsNotExist`）也视为成功并继续清理 DB，确保断电重启自愈。
- **白名单保护**：`persons` 和 `person_faces` 及其底库图片路径（`raw_image_key`, `aligned_face_key`）严禁被清理 Worker 查询或删除。
- **让步步进流控 (Chunked Pacing)**：每批最多处理 200 条记录；批次之间必须 `time.Sleep(50ms)` 让出磁盘 I/O，杜绝打满 IOPS 阻塞视频流硬解码和 AI 推理。
- **三级防御状态机**：
  - 常规模式：当磁盘使用率 $< 85\%$ 时，按配置 `RetentionDays` 执行 TTL 周期巡检；
  - 紧急削峰模式：当磁盘使用率 $\ge 85\%$ 时，按 FIFO 淘汰最早记录直至使用率降至 $\le 70\%$ 或无更多业务数据（Early Exit）；
  - 极危熔断保护：当磁盘使用率 $\ge 95\%$ 时，激活熔断器（`circuitBreakerActive = true`），系统状态变为 `"degraded"`，Engine / App 丢弃 JPEG 存盘仅保留轻量文本告警；降回 $< 85\%$ 后自动复位。

### 4. Validation & Error Matrix

| 条件 | 错误码 | HTTP 状态 |
| --- | --- | --- |
| `retentionDays < 1` 或 `> 365` | `CodeStorageInvalidConfig` (1420) | 400 |
| `highWatermarkPercent < 50` 或 `> 95` | `CodeStorageInvalidConfig` (1420) | 400 |
| `lowWatermarkPercent < 30` 或 `> 90` | `CodeStorageInvalidConfig` (1420) | 400 |
| `lowWatermarkPercent >= highWatermarkPercent` | `CodeStorageInvalidConfig` (1420) | 400 |
| `checkIntervalSeconds < 30` 或 `> 86400` | `CodeStorageInvalidConfig` (1420) | 400 |
| 磁盘采样或数据库统计内部故障 | `CodeInternal` (1500) | 500 |

### 5. Good / Base / Bad Cases

- Good：高水位突发告警时，Worker 分批并发让步削峰，降至 70% 立即 Early Exit，未造成硬解丢帧。
- Good：图片已被外部手动删除时，清理 Worker 幂等处理，顺畅清除 DB 悬空元数据。
- Bad：先执行 `DELETE FROM alarm_records` 再尝试删除磁盘图片（导致断电后遗留无主孤儿图片）。
- Bad：在快满盘时调用 `VACUUM` 导致 2 倍临时空间写入暴击和全库锁死。

### 6. Tests Required

- Sampler 测试：跨平台 `statfs` 采样、0%/100% 边界、不存在路径容错。
- Repo 测试：5 张业务表的 `FindExpired`, `FindOldest`, `HardDeleteBatch`, `CountTotal`。
- Service 测试：TTL 到期删除、高水位 90% $\to$ 70% 削峰 Early Exit、95% 熔断器置位与复位、底库资产免死白名单。
- API 测试：`/status`, `/config`, `/config` 边界校验、`/cleanup`。

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：先删数据库后删文件；全量一次性删除
db.Where("occurred_at < ?", cutoff).Delete(&model.AlarmRecord{})
for _, path := range imagePaths { os.Remove(path) }
```

#### Correct

```go
// 正确：分批 (200 条) + 先物理删图片 + 后物理硬删 DB + 50ms I/O 让步
for _, rec := range records {
    if rec.ImageRelPath != "" { _ = fileStorage.Delete(ctx, rec.ImageRelPath) }
}
alarmRepo.HardDeleteBatch(ctx, ids)
time.Sleep(50 * time.Millisecond)
```

