# 对时服务 - 实施计划

## Phase 1: 数据模型与基础设施

### 1.1 通用系统配置表与模型

- [ ] 创建 SQL 迁移脚本 `migrations/000007_add_system_configs.up.sql` 和 `000007_add_system_configs.down.sql`（编号以实际最新值 000006 为基准，含 `system_configs` 表 + 默认 `system:time` 配置）
- [ ] 创建菜单播种迁移 `migrations/000008_seed_ops_time.up.sql`（含 `.down.sql`）：创建 Ops catalog（`/ops`、`routes.ops.ops`）+ 时间管理菜单（`/ops/time`、`routes.ops.time`、`ops:time`）+ 按钮权限（`ops:time:read`/`ops:time:edit`），绑定 super 角色，遵循 000005 幂等 `DO $$` 模式
- [ ] 创建 GORM 模型 `internal/model/system_config.go` (`SystemConfig`, `ConfigKeyTime` 等常量)
- [ ] 更新 `internal/model/migrate.go` 添加 `&SystemConfig{}`（供单元测试 AutoMigrate 使用；同步注释 8→9 张表）
- [ ] 创建通用 Repository `internal/repository/system_config.go`
- [ ] 验证：`make test` 数据库模型及迁移测试通过

### 1.2 NTP Executor 接口与适配器

- [ ] 创建 `internal/pkg/ntp/executor.go` — 定义 `Executor` 接口和 `SyncStatus` 结构体
- [ ] 创建 `internal/pkg/ntp/mock.go` — Mock 适配器（测试用，纯内存）
- [ ] 创建 `internal/pkg/ntp/detect.go` — `NewExecutor()` 工厂函数，按平台自动加载
- [ ] 创建 `internal/pkg/ntp/chrony.go` (build tag: linux) — Linux chrony 适配器（drop-in 配置）
- [ ] 创建 `internal/pkg/ntp/timesyncd.go` (build tag: linux) — Linux timesyncd 适配器
- [ ] 创建 `internal/pkg/ntp/darwin.go` (build tag: darwin) — macOS 适配器
- [ ] 验证：`go build ./internal/pkg/ntp/...` 编译通过

### 1.3 错误码

- [ ] `internal/pkg/errno/errno.go` 新增 1201-1207 错误码常量
- [ ] `internal/pkg/errno/message.go` 新增三语消息（zh-CN, en-US, zh-TW）
- [ ] 验证：编译通过，无冲突

## Phase 2: 后端业务层

### 2.1 Service

- [ ] 创建 `internal/service/ntp.go` — `NTPService` 接口与实现（基于 `SystemConfigRepository` + `ntp.Executor` 联动 + 开机重放）
- [ ] 创建 `internal/service/ntp_test.go` — 包含 mock repository + mock executor 的完整单元测试
- [ ] 验证：`go test ./internal/service/... -run TestNTP` 全量通过
- [ ] `ReplayOnBoot` 由 `cmd/api/main.go` 启动链显式调用（不隐藏于 wire 构造函数）

### 2.2 Handler

- [ ] 创建 `internal/api/ntp.go` — `NTPHandler`（6 个 endpoint 的参数绑定与响应）
- [ ] 创建 `internal/api/ntp_test.go` — HTTP 接口测试
- [ ] 验证：单元测试通过

### 2.3 路由与权限注册

- [ ] `internal/router/router.go` — 在 `Deps` 注入 `NTPHandler`，注册 `/api/ntp/*` 路由与 `ops:time:read` / `ops:time:edit` 权限码；`GET /api/ntp/synced` 显式注册 `PermCodeAuthenticated`
- [ ] `internal/middleware/oplog.go` — 在 `actionI18nMap` 注册 NTP 写操作 i18n key（`ops.time.*`）
- [ ] 验证：编译通过

### 2.4 Wire DI

- [ ] `cmd/api/wire.go` — 新增 `ntp.NewExecutor`、`repository.NewSystemConfigRepository`、`service.NewNTPService`、`api.NewNTPHandler`
- [ ] 运行 `make wire` 重新生成 `cmd/api/wire_gen.go`
- [ ] 验证：`make build` 编译成功

## Phase 3: 后端集成验证

- [ ] `make test` 全量通过
- [ ] `make vet` 无静态检查告警
- [ ] `make build` 成功输出二进制

## Phase 4: 前端实现

### 4.1 API 层

- [ ] 创建 `apps/web-antd/src/api/ntp.ts` — 封装 6 个 API 端点调用及 TypeScript 类型定义

### 4.2 页面开发

- [ ] 创建 `apps/web-antd/src/views/ops/time/index.vue`
- [ ] 顶部：对时模式切换（NTP 自动对时 / 手动设置）
- [ ] NTP 模式区域：
  - 实时同步状态卡片（同步源、偏移量、最后同步时间、同步指示灯）
  - NTP 服务器列表（动态增、删、改服务器地址）
  - 「立即同步」与「保存配置」按钮
- [ ] 手动模式区域：
  - 日期时间选择器（DateTimePicker）
  - 「应用时间」按钮（带危险提示二次确认）

### 4.3 路由与国际化

- [ ] 菜单由后端 `/menu/all` 动态下发（组件路径 `/ops/time/index`），无需静态路由；路由配置添加 `/ops/time`
- [ ] 补充 `zh-CN`、`en-US`、`zh-TW` 语言包：`routes.ops.ops`（运维管理）、`routes.ops.time`（时间管理）、`ops.time.*` 按钮/操作日志翻译
- [ ] 验证：页面切换及多语言显示正常

## Phase 5: 端到端质量验证

- [ ] 后端 `make test`、`make vet` 全部通过
- [ ] 前端 `pnpm check` (typecheck + lint) 全部通过
- [ ] 核心流程验证：
  1. 读取初始配置及状态
  2. 修改 NTP 服务器列表并保存，验证 DB 落地及底层应用
  3. 触发立即同步
  4. 切换到手动模式并设置具体时间
  5. 切换回 NTP 模式并验证自动恢复

## Risky Files

| 文件 | 风险点 | 回滚/隔离策略 |
| ------ | -------- | ------------- |
| `internal/router/router.go` | 核心路由分发 | 仅新增独立的 ntpGroup，不影响既有路由 |
| `cmd/api/wire.go` | 全局依赖注入 | 仅追加 provider，出问题回退后重新 `make wire` |
| `migrations/000008_seed_ops_time.*` | 种子菜单与权限码 | 严格追加，不破坏既有菜单 ID/权限树 |
| `migrations/` | 数据库表结构 | 严格配套 `.down.sql` 保证可逆 |
