# 文件上传技术设计

## 1. 目标与边界

新增一个与存储介质无关的上传能力：HTTP 层接收单个 multipart 文件，Service 层完成文件名、大小和内容类型校验，存储层负责持久化并生成可直接访问的 URL。

本次不新增文件元数据数据库表，也不实现删除、分片上传、断点续传或文件管理列表。对象 key 是上传成功响应中的事实标识；后续业务如需把文件绑定到用户或其他资源，再由对应领域保存 key/URL。

## 2. 数据流

```text
multipart request
  -> FileHandler.FormFile("file")
  -> service.FileService.Upload
  -> content/size/name validation
  -> storage.FileStorage.Put
       -> localStorage: temp file + atomic rename
       -> minioStorage: minio.Client.PutObject
  -> UploadedFile DTO
  -> response.Success {code, data, message}
```

公开访问路径不经过认证中间件：本地实现由 Gin 静态路由提供，MinIO 实现由配置的公开 URL 前缀提供。上传写入仍经过 `/api` 认证和 `PermCodeAuthenticated` 权限声明。

## 3. HTTP 契约

- Method/path：`POST /api/file/upload`
- Authentication：`Authorization: Bearer <access-token>`，需要登录，不要求额外角色权限。
- Content-Type：`multipart/form-data`
- File field：`file`
- Limits：单文件默认 `10 MiB`。
- Allowed types：`.jpg`/`.jpeg`、`.png`、`.gif`、`.webp`、`.pdf`；扩展名与文件内容探测出的 MIME 必须匹配。

成功响应的 `data`：

```json
{
  "key": "2026/08/21/9f4c...a1.png",
  "name": "avatar.png",
  "size": 12345,
  "contentType": "image/png",
  "url": "/uploads/2026/08/21/9f4c...a1.png"
}
```

`key` 使用日期目录和随机字节生成，不使用原始文件名；`name` 仅作为展示元数据返回。`url` 不含认证要求，MinIO URL 由 `public_base_url` 与 key 拼接。

校验失败通过现有错误中间件返回 `{code,data:null,message}`：

- 缺失文件、空文件、文件名无效：`CodeInvalidParam`。
- 超出大小：新增 `CodeFileTooLarge`。
- 类型不允许或扩展名/MIME 不匹配：新增 `CodeFileTypeNotAllowed`。
- 存储实现错误：向客户端统一映射为 `CodeInternal`，原始错误仅交给现有日志链路。

## 4. 分层设计

### 4.1 `internal/pkg/config`

扩展 `Config`：

```yaml
storage:
  driver: local # local | minio
  max_size: 10485760
  local:
    root: ./uploads
    url_prefix: /uploads
  minio:
    endpoint: 127.0.0.1:9000
    access_key: ""
    secret_key: ""
    bucket: niko-vue-admin
    use_ssl: false
    public_base_url: http://127.0.0.1:9000/niko-vue-admin
```

`APP_STORAGE_*` 环境变量可以覆盖配置。默认 driver 为 `local`，默认 max size 为 `10 MiB`。配置校验仅要求当前 driver 的字段完整：local 要求 root/url prefix；minio 要求 endpoint、credentials、bucket 和 `public_base_url`，并校验 URL 为 `http/https` 且包含 host。

允许类型是后端的安全契约，集中定义在文件服务/校验模块，不从请求中信任 MIME；`max_size` 可配置但必须为正数。

### 4.2 `internal/pkg/storage`

定义最小接口，避免 API/Service 依赖具体 SDK：

```go
type FileStorage interface {
    Put(ctx context.Context, input PutInput) (StoredObject, error)
}
```

`PutInput` 只包含安全 key、reader、size 和已验证的 content type；`StoredObject` 返回 key、URL、size、content type。实现文件：

- `storage.go`：接口、DTO、按配置选择实现的构造函数。
- `local.go`：创建父目录、写入同目录临时文件、同步后原子 rename；失败时删除临时文件，避免半成品。
- `minio.go`：创建 MinIO client，使用 `PutObject` 写入配置 bucket，并用 `public_base_url` 生成 URL。bucket 的公开访问策略由部署方预先配置；构造函数不在启动时隐式修改 bucket policy。

两种实现都校验 key 不能是绝对路径、不能包含 `..` 路径段，并只接受 Service 生成的相对 key。

### 4.3 `internal/service/file.go`

`FileService` 是 API 与存储之间的边界。Service 接收包含 `io.Reader`、原始文件名和声明大小的输入，不依赖 Gin/multipart 类型。

处理顺序：

1. 校验文件名非空且不含 NUL；只取扩展名用于 allowlist 和 key 生成，不把原始名称用于路径。
2. 校验声明大小大于 0 且不超过配置上限。
3. 读取有限的文件头，使用 `github.com/gabriel-vasile/mimetype` 探测实际 MIME，并与扩展名映射比较。
4. 用 `io.MultiReader` 将已读取的文件头和剩余内容重新交给存储实现，避免丢失前缀。
5. 生成随机 key，调用 `FileStorage.Put`，把存储结果和原始名称组装为 `UploadedFile`。

### 4.4 `internal/api/file.go`

Handler 只负责：

- 对请求体施加 multipart 总量上限保护；
- 提取 `file` 字段、打开文件并在请求结束时关闭；
- 转换为 Service 输入；
- 交错误给统一错误处理中间件，成功调用 `response.Success`。

Handler 不直接读写本地文件或调用 MinIO SDK。

### 4.5 `internal/router` 与 DI

- `router.Deps` 增加 `FileHandler`。
- 在 `/api/file` 下注册 `POST /upload`。
- 调用 `PermMiddleware.Register(http.MethodPost, "/api/file/upload", middleware.PermCodeAuthenticated)`。
- local driver 注册 `engine.StaticFS(cfg.Storage.Local.URLPrefix, http.Dir(cfg.Storage.Local.Root))`；minio driver 不注册本地静态路由。
- `wire.go` 增加 storage provider、FileService、FileHandler，运行 `make wire` 生成 `wire_gen.go`。

### 4.6 前端与文档

新增 `ui/apps/web-antd/src/api/core/file.ts`：在 `FileApi` 命名空间声明 `UploadResult`，导出 `uploadFileApi(file)` 并调用现有 `requestClient.upload`。通过 `api/core/index.ts` 暴露，不新增请求客户端或页面。

为操作日志新增 `system.log.actionUpload` 的中/英/繁体文案，并在 `actionI18nMap` 注册上传路由。Handler 增加 Swagger 注释及响应类型，运行 `make swagger` 更新 `app/docs`。

Docker 本地存储场景将 `./uploads` 挂载为持久化 volume，并让 Nginx 将 `/uploads/` 代理到后端静态路由；MinIO 场景在配置示例中说明 bucket/public URL 前置条件。

## 5. 依赖与兼容性

新增直接依赖：

- `github.com/minio/minio-go/v7`，用于 MinIO PutObject。
- `github.com/gabriel-vasile/mimetype`，用于文件头 MIME 探测；该模块已在依赖图中存在，但需提升为直接依赖。

不涉及数据库迁移，不改变既有 JSON 响应或认证流程。存储 driver 配置错误在启动阶段失败，避免服务运行到第一次上传才发现配置问题。

## 6. 测试策略

- config：默认值、local/minio 必填配置、非法 driver、非法 max size/URL。
- service：表驱动测试空文件、超限、非法扩展名、内容不匹配、合法各类型、reader 前缀未丢失、存储错误映射和随机 key 不含原始文件名；使用 fake `FileStorage`。
- local storage：真实临时目录写入、读取内容、目录创建、路径穿越拒绝、写入失败清理临时文件。
- MinIO storage：使用窄接口 fake client 验证 bucket/key/size/content type 传递、URL 生成和 SDK 错误返回。
- API/router：multipart 成功、缺失字段、认证/权限、统一错误响应和公开本地静态 URL；不依赖外部 MinIO 服务。
- frontend：类型检查由 `pnpm check` 覆盖，上传函数复用现有 RequestClient upload 测试契约。
