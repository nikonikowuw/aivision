# 文件上传实施计划

## 1. 配置与依赖

- [x] 在 `app/internal/pkg/config/config.go` 增加 Storage/Local/MinIO 配置结构、默认值和校验。
- [x] 在 `app/configs/config.yaml` 增加 local 默认配置和 MinIO 配置示例，补充 `app/internal/pkg/config/config_test.go`。
- [x] 将 `minio-go/v7` 和 `mimetype` 声明为直接依赖，执行 `go mod tidy` 并检查依赖 diff。

验证：配置单测覆盖默认值、driver 切换、必填项和非法值；`go mod tidy` 后 `go test ./internal/pkg/config` 通过。

## 2. 存储抽象与实现

- [x] 新建 `app/internal/pkg/storage/storage.go`，定义 `FileStorage`、输入/输出 DTO、允许的 key 约束和构造函数。
- [x] 新建 `local.go`，实现临时文件写入、`Sync`、原子 rename、失败清理和本地 URL 生成。
- [x] 新建 `minio.go`，实现 MinIO client 构造、PutObject、公开 URL 拼接和窄 client 接口，便于单测替身。
- [x] 添加 storage 包单测：本地真实临时目录、路径安全、MinIO fake client 和错误路径。

验证：`go test ./internal/pkg/storage`；检查目标文件不会使用原始文件名作为路径，临时文件在失败后清理。

## 3. Service/API 契约

- [x] 在 `app/internal/pkg/errno/errno.go` 增加文件超限和类型不允许错误码及三语文案。
- [x] 在统一错误处理中间件中将文件参数错误映射为 HTTP 400，存储错误保持内部错误响应。
- [x] 新建 `app/internal/service/file.go`，实现文件名、大小、MIME/扩展名双重校验、头部回放、随机 key 和存储调用。
- [x] 新建 `app/internal/api/file.go`，实现 multipart `file` 字段读取、请求体限制、文件关闭和统一响应/Swagger 注释。
- [x] 添加 service/API 单测，锁定合法类型、非法输入、响应字段和错误行为。
- [x] 添加真实 Handler → Service → local Storage 链路测试，确认返回 key 对应的文件内容可读。

验证：`go test ./internal/service ./internal/api`；通过 JSON 断言确认失败响应为 `{code,data:null,message}`，且不包含底层错误详情。

## 4. 路由、DI、日志和公开访问

- [x] 扩展 `router.Deps`，注册 `/api/file/upload` 和 `PermCodeAuthenticated`。
- [x] local driver 注册静态 URL 路径；minio driver 返回配置的公开地址。
- [x] 在 `wire.go` 增加 storage provider、FileService、FileHandler，执行 `make wire`，不手工编辑 `wire_gen.go`。
- [x] 在 `oplog` action map 和 zh-CN/en-US/zh-TW 日志文案中注册上传动作。
- [x] 更新 router/oplog 测试，验证认证、权限和 action 路由行为。
- [x] 更新 Docker volume/Nginx 配置，确保默认 local URL 在 compose 环境可访问且文件持久化。

验证：`go test ./internal/router ./internal/middleware`；通过路由表检查上传写路由已声明权限；启动配置下本地 URL 可读取。

## 5. 前端 API 与 Swagger

- [x] 新建 `ui/apps/web-antd/src/api/core/file.ts`，提供类型化 `uploadFileApi`，复用 `requestClient.upload`。
- [x] 从 `ui/apps/web-antd/src/api/core/index.ts` 导出文件 API。
- [x] 执行 `make swagger` 更新 `app/docs/swagger.yaml`、`swagger.json`、`docs.go`。

验证：`cd ui && pnpm check`；`cd ../app && make swagger` 后检查 Swagger 包含 multipart 参数、Bearer security 和响应 DTO。

## 6. 全量质量检查

- [x] 执行 `gofmt -w` 并确认 `gofmt -l .` 无输出。
- [x] 执行 `cd app && make test`。
- [x] 执行 `cd app && make vet`。
- [x] 执行 `cd ui && pnpm check`（若前端 API 文件已加入）。
- [x] 检查 `git diff` 只包含文件上传相关变更，确认无密钥、临时上传文件或生成物污染。

风险与回滚点：

- MinIO SDK 或配置校验出现兼容问题时，可保留接口和 local 实现，暂时让 driver 校验拒绝 minio；不回退已确定的 API 契约。
- Swagger/生成文件只通过 `make swagger`、`make wire` 更新；若生成工具不可用，记录阻塞，不手工伪造生成输出。
- Docker/Nginx 的公开 `/uploads` 路径必须与 local `url_prefix` 一致，否则上传成功但 URL 不可访问；验收时用真实 HTTP GET 验证。
