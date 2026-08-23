# 对时服务 - 技术设计

## Architecture

对时服务采用 **通用系统配置表 (system_configs) 作为单一事实来源 + 底层适配器负责执行生效** 的分层架构：

```
HTTP Handler (internal/api/ntp.go)
    ↓
NTP Service (internal/service/ntp.go)
    ├── SystemConfig Repository (internal/repository/system_config.go) ← 读写 PostgreSQL system_configs 表
    └── NTP Executor (internal/pkg/ntp/executor.go)                   ← 系统底层生效与实时状态查询
          ├── chrony    (internal/pkg/ntp/chrony.go)      // Linux chrony 适配
          ├── timesyncd (internal/pkg/ntp/timesyncd.go)  // Linux timesyncd 适配
          ├── darwin    (internal/pkg/ntp/darwin.go)       // macOS 适配
          └── mock      (internal/pkg/ntp/mock.go)         // 单元测试替身
```

### 数据流与职责划分

| 场景 | 数据库 (system_configs 表) | 底层执行器 (Executor) |
| ------ | --------------------------- | --------------------- |
| **查询配置** (`GET /config`) | 从 DB 读取 key 为 `system:time` 的持久化配置 | 不调用 |
| **修改配置** (`PUT /config`) | 校验后更新 DB 中的 `system:time`（支持审计、出厂重置；保存期望配置） | 写入系统 drop-in 配置并重载 NTP 服务；失败时返回错误，启动重放可重试 |
| **查询状态** (`GET /status`) | 不调用 | 实时读取系统时钟同步状态（源、偏移量、是否同步） |
| **立即同步** (`POST /sync`) | 不调用 | 触发 NTP 守护进程立即对时（`makestep` 等） |
| **手动设时** (`POST /set-time`) | 更新 DB 中 `system:time` 的 mode 为 manual | 停用 NTP 守护进程，执行系统设时命令 |
| **服务启动** (Boot Replay) | 从 DB 读取已确认的 `system:time` 配置 | 自动重放应用到底层系统，恢复开机前状态 |

> Boot Replay 由 `cmd/api/main.go` 在启动链中**显式调用** `NTPService.ReplayOnBoot()`（不隐藏于 wire 构造函数，对齐网络任务研究的生命周期结论）。

## Data Model & Migration

### PostgreSQL Schema (`migrations/000007_add_system_configs.up.sql`)

> 编号 000007 以 `app/migrations/` 实际最新值（000006）为基准；`.down.sql` 配套可逆。

创建通用的系统配置表，支撑对时、网络、存储、系统信息等全平台系统级配置：

```sql
CREATE TABLE IF NOT EXISTS system_configs (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    key VARCHAR(64) NOT NULL,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    remark VARCHAR(255)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_system_configs_key ON system_configs (key);

-- 初始化对时服务的默认配置 (key: 'system:time')
INSERT INTO system_configs (key, value, remark)
VALUES (
    'system:time',
    '{"mode": "ntp", "servers": ["pool.ntp.org", "ntp.aliyun.com"]}'::jsonb,
    '系统对时配置'
)
ON CONFLICT (key) DO NOTHING;
```

### GORM Model (`internal/model/system_config.go`)

```go
package model

import "time"

const (
    ConfigKeyTime    = "system:time"
    ConfigKeyNetwork = "system:network"
    ConfigKeyStorage = "system:storage"
    ConfigKeyInfo    = "system:info"
)

type SystemConfig struct {
    ID        uint64    `gorm:"primaryKey" json:"id"`
    CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`
    UpdatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updatedAt"`
    Key       string    `gorm:"size:64;not null;uniqueIndex:uk_system_configs_key" json:"key"`
    Value     string    `gorm:"type:jsonb;not null;default:'{}'" json:"value"` // JSONB 字符串
    Remark    string    `gorm:"size:255" json:"remark"`
}

func (SystemConfig) TableName() string {
    return "system_configs"
}
```

### 对时业务配置 DTO

```go
type TimeConfigValue struct {
    Mode    string   `json:"mode"`    // "ntp" | "manual"
    Servers []string `json:"servers"` // NTP 服务器列表
}
```

## Core Interfaces

### 1. 通用 SystemConfig Repository (`internal/repository/system_config.go`)

```go
type SystemConfigRepository interface {
    GetByKey(ctx context.Context, key string) (*model.SystemConfig, error)
    SetByKey(ctx context.Context, key string, value string, remark string) error
}
```

### 2. NTP Executor (`internal/pkg/ntp/executor.go`)

```go
type SyncStatus struct {
    Synced       bool    `json:"synced"`        // 系统时钟是否已完成同步
    Source       string  `json:"source"`        // 当前有效同步源
    Offset       string  `json:"offset"`        // 时钟偏移量（如 "+0.003s"）
    LastSyncTime *string `json:"lastSyncTime"`  // 最近一次同步时间 (RFC3339)
}

type Executor interface {
    // 状态查询（实时）
    GetStatus(ctx context.Context) (*SyncStatus, error)

    // 系统生效
    ApplyNTP(ctx context.Context, servers []string) error // 应用 NTP 服务器并启动/重载 NTP 服务
    DisableNTP(ctx context.Context) error                 // 停用 NTP 服务（切到手动模式前）
    SyncNow(ctx context.Context) error                    // 触发立即对时
    SetSystemTime(ctx context.Context, t time.Time) error // 手动修改系统时钟
}
```

### 3. Service (`internal/service/ntp.go`)

```go
type NTPConfigDTO struct {
    ID        uint64    `json:"id"`
    Mode      string    `json:"mode"`
    Servers   []string  `json:"servers"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`
}

type UpdateNTPConfigInput struct {
    Mode    string   `json:"mode"`
    Servers []string `json:"servers"`
}

type SetTimeInput struct {
    Time time.Time `json:"time"`
}
```

```go
type NTPService interface {
    GetConfig(ctx context.Context) (*NTPConfigDTO, error)
    UpdateConfig(ctx context.Context, input *UpdateNTPConfigInput) error
    GetStatus(ctx context.Context) (*ntp.SyncStatus, error)
    SyncNow(ctx context.Context) error
    SetTime(ctx context.Context, input *SetTimeInput) error
    IsSynced(ctx context.Context) (bool, error)
    ReplayOnBoot(ctx context.Context) error // 开机从 system_configs 重放配置
}
```

## Platform Adapters

### Linux

- **chrony**: 写入独立的 drop-in 配置文件 `/etc/chrony/conf.d/aivision.conf`，执行 `chronyc reload sources`；`chronyc tracking` 解析状态；`chronyc makestep` 触发同步。
- **timesyncd**: 写入 `/etc/systemd/timesyncd.conf.d/aivision.conf`，执行 `systemctl restart systemd-timesyncd`；`timedatectl show-timesync` 解析状态。
- **手动设时**: `timedatectl set-ntp false` 禁用自动对时，`date -s` 设置系统时间。

### macOS

- **NTP 模式**: `systemsetup -setnetworktimeserver <server>`，`systemsetup -setusingnetworktime on`。macOS 系统工具只接受一个服务器，适配器使用列表首项作为当前服务器，完整列表仍保存在配置中供跨平台迁移。
- **状态读取**: `sntp -d <server>` 探测时钟偏移；该探测不等同于实际同步，因此没有可确认的同步时间时 `lastSyncTime` 返回 `null`。
- **手动设时**: `systemsetup -setusingnetworktime off`，`date` 设置系统时间。

### Mock Adapter

- 纯内存状态存储，不触碰任何系统底层命令，供 `go test` 单元测试使用。

## API Endpoints

统一挂载在 `/api/ntp` 下：

| Method | Path | Handler | 描述 | 权限码 |
| -------- | ------ | --------- | ------ | -------- |
| GET | `/api/ntp/config` | GetConfig | 获取当前配置（从 system_configs 表） | `ops:time:read` |
| PUT | `/api/ntp/config` | UpdateConfig | 更新配置并应用到底层 | `ops:time:edit` |
| GET | `/api/ntp/status` | GetStatus | 实时获取时钟同步状态 | `ops:time:read` |
| POST | `/api/ntp/sync` | SyncNow | 触发立即同步（NTP 模式） | `ops:time:edit` |
| POST | `/api/ntp/set-time` | SetTime | 手动设置时间（手动模式） | `ops:time:edit` |
| GET | `/api/ntp/synced` | IsSynced | 内部同步状态查询 | 仅需登录认证（显式注册 `PermCodeAuthenticated`） |

## Error Codes (`internal/pkg/errno`)

| Code | 常量名 | 含义 |
| ------ | -------- | ------ |
| 1201 | `CodeNTPManualNotAllowedInNTPMode` | 历史错误码，保留编号但当前手动设时入口不再使用 |
| 1202 | `CodeNTPSyncNotAllowedInManualMode` | 手动模式下不支持触发 NTP 同步 |
| 1203 | `CodeNTPServersEmpty` | NTP 模式下服务器列表不能为空 |
| 1204 | `CodeNTPInvalidMode` | 无效的对时模式（只允许 ntp / manual） |
| 1205 | `CodeNTPSetTimeFailed` | 系统时间设置失败 |
| 1206 | `CodeNTPSyncFailed` | NTP 同步失败 |
| 1207 | `CodeNTPExecutorUnavailable` | 底层对时执行器不可用 |

## Wire DI

```go
// cmd/api/wire.go 新增 providers
ntp.NewExecutor,                    // → ntp.Executor (平台适配器/自动探测)
repository.NewSystemConfigRepository, // → repository.SystemConfigRepository
service.NewNTPService,              // → service.NTPService
api.NewNTPHandler,                  // → *api.NTPHandler
```

## Menu & Permission Seed

菜单归属：**运维管理**下新增「时间管理」页（决策：归运维管理，PRD 7.17 导航表同步补充）。当前 DB 种子（000005）尚无 Ops catalog，新迁移需一并创建。

```
Ops (routes.ops.ops, /ops, ant-design:tool-outlined)
  └── Time / 时间管理 (routes.ops.time, /ops/time, ant-design:field-time-outlined)
       ├── ops:time                (页面权限)
       ├── ops:time:read           (读取权限按钮)
       └── ops:time:edit           (修改/同步权限按钮)
```

播种方式：遵循 000005 幂等 SQL 迁移模式（`DO $$` 块 + `role_menus` 绑定 super 角色），新库与升级库一致；`internal/model/seed.go` 生产已不再调用，不作为播种载体。前端组件位于 `ui/apps/web-antd/src/views/ops/time/index.vue`。

## Operation Log

在 `oplog.go` 的 `actionI18nMap` 注册（i18n key 与菜单按钮 name 保持一致，同命名空间 `ops.time.*`）：

```go
"PUT /api/ntp/config":    "ops.time.updateConfig",
"POST /api/ntp/sync":     "ops.time.sync",
"POST /api/ntp/set-time": "ops.time.setTime",
```
