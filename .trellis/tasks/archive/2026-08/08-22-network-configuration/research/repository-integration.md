# 仓库集成模式研究

## 后端边界

当前仓库没有 `internal/platform`、网络模型或网络仓储。`repository` 只封装 GORM；网络
运行态和文件事务不应伪装成数据库 repository，也不需要新增业务表。最接近的平台抽象是
[`app/internal/pkg/storage/storage.go`](../../../../app/internal/pkg/storage/storage.go) 的小接口、
实现选择和测试替身模式。

建议沿用依赖方向：

```text
router -> api -> service -> platform interface
                         -> operation-log service (仅系统后台动作)
```

平台实现与 root-only 文件状态可放在新的 `internal/pkg/netconfig/`（最终包名在设计中冻结，
避免与标准库 `net` 混淆）。业务状态机放 `internal/service/network.go`，Handler 不直接调用
OS API。

关键现有引用：

- Handler 的 JSON 绑定、`c.Error`、统一成功响应：
  `app/internal/api/user.go:17`。
- Service 接口与基础设施依赖模式：`app/internal/service/file.go:39`。
- 路由唯一装配点：`app/internal/router/router.go:102`。
- 写接口权限注册示例：`app/internal/router/router.go:134`；未声明写路由默认 403。
- Wire provider 与 `router.Deps`：`app/cmd/api/wire.go:20`；生成结果从
  `app/cmd/api/wire_gen.go:24` 开始，不能手改。
- 配置加载：`app/internal/pkg/config/config.go:111`。新增网络状态目录、Linux Profile
  allowlist 等配置必须同步进入 struct、defaults、validate 和示例 YAML。
- 错误码与三语消息唯一来源：`app/internal/pkg/errno/errno.go:24`；HTTP/业务错误映射：
  `app/internal/middleware/error_handler.go:14`。

## 启动与 root 检查

API 启动链在 `app/cmd/api/main.go:46` 开始加载配置，约 `:52` 调 wire，`:70` 检查
schema，`:83` 开始监听。当前没有 root 检查。

用户已决定 API 主进程直接 root 运行。检查应位于 `config.Load` 成功后、wire/数据库等昂贵
初始化前，并以可单测的 `requireRoot(euid)` 纯函数封装；不应放 router 或平台首次写入。
`go test` 不执行 main，因此普通单元测试不需要 root。网络服务的 `Start/Close` 生命周期
必须由 main 显式控制，并在监听前完成启动恢复。

## Wire 与生命周期

新增 platform manager、transaction store、NetworkService 和 NetworkHandler 后更新
`wire.go` 并执行 `make wire`。由于现有 injector 主要返回 Gin engine，而网络服务需要
启动恢复、DHCP 协程和关闭等待，设计不能把有副作用的 goroutine 隐藏在构造函数里；应让
wire 返回含 Engine 与 NetworkService 生命周期的应用对象，或由 router deps 暴露明确的
runtime lifecycle。最终选择需保持 main 可见地调用 `Start`/`Close`。

## 操作日志

现有 Oplog 中间件在 `app/internal/middleware/oplog.go:69` 的 `c.Next()` 后采集结果，
写请求体经脱敏后异步调用 `OperationLogService.Record`（约 `:115`）。新增 HTTP 写路由
必须补 `actionI18nMap`（约 `:170`）及三语 action key。

边界：

- apply、confirm、cancel、factory-reset 等 HTTP 写请求由现有中间件记录，包括 Handler
  交出的业务错误。
- timeout rollback、启动恢复、DHCP lease 失效等后台动作不经过 Gin，必须由 NetworkService
  显式调用 `OperationLogService.Record`。
- 候选事务必须持久保存原操作者 ID/用户名、来源 IP、接口、模式和脱敏摘要，后台回滚才能
  形成完整审计。
- 后台记录使用明确的 system action key，不能伪造 HTTP 请求或依赖 Oplog middleware。
- 当前 `app/Dockerfile` 固定 `CGO_ENABLED=0`，`deploy/docker-compose.yml` 没有 host network、
  `NET_ADMIN`/`NET_RAW`、宿主 `/etc/resolv.conf` 或 root-only 网络状态目录挂载；网络首版的
  Linux/macOS 支持矩阵必须是原生 host-root 部署，默认容器形态应在能力检查阶段 fail closed。

## 菜单、迁移与权限

API 启动不依赖每次 seed 自动追加菜单；已部署数据库必须通过版本化 SQL migration 新增
目录、页面和按钮权限，同时同步 `internal/model/seed.go`，保证新库与升级库一致。当前下一
迁移编号需要实施前按 `app/migrations/` 实际最新值确认，不能仅按研究时的编号硬编码。

产品 PRD 的导航把设备运维页面归入“运维管理”；本任务已确认网络页面放在
`运维管理 -> Network`，使用 `/ops/network`、`OpsNetwork`、`routes.ops.network` 和
`ops:network*`。操作日志 action 文案继续使用现有 `system.log.*` namespace；权限码必须保持
`<domain>:<resource>` / `<domain>:<resource>:<action>`。

## 前端模式

- 新增类型化 API 模块并从 `ui/apps/web-antd/src/api/core/index.ts:1` 导出。
- 系统使用 backend 动态权限：`preferences.ts:21` 与 `router/access.ts:25`；业务页面不需要
  添加静态 route module，菜单 component 必须能被现有 `import.meta.glob` 解析。
- 菜单/action 文案使用 i18n key，翻译辅助参考 `src/utils/menu.ts:4`。
- 页面表单沿用 `useVbenForm` 与 Zod，参考 `views/system/user/index.vue:57`；按钮权限用
  `v-access:code`，参考同文件 `:505`。
- 三语同步更新 `zh-CN`、`en-US`、`zh-TW` 的 `routes.json` 与业务命名空间 JSON。
- 倒计时、pending transaction、接口选择和刷新是页面本地服务端状态，不需要新增 Pinia
  store；用 fake timers 的 Vitest 覆盖倒计时与状态恢复。

## 测试锚点

- 平台 fake 确保默认单元/API 测试不触碰真实网卡。
- Service 覆盖输入校验、平台部分失败补偿、全局单候选事务、120 秒确认/回滚、启动恢复。
- API 覆盖统一响应、事务重连查询和非法稳定 ID。
- Router/middleware 覆盖页面/编辑/重置权限与失败写日志，参考
  `app/internal/middleware/middleware_test.go:77`。
- 前端表单测试参考仓库 `form-integration.test.ts:80`，路由动态加载测试参考
  `accessible.test.ts:45`；最终运行 `pnpm check` 与相关 Vitest。
