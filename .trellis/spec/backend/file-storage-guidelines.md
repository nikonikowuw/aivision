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
