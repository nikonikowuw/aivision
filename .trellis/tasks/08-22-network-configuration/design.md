# 网络配置服务技术设计

状态：`draft`（待 API 契约确认）

## 1. 设计目标与边界

本设计把“宿主机网络控制”和“Web 配置事务”分成两个边界：平台包只处理结构化的网络对象，业务服务负责校验、权限上下文、120 秒确认窗口、持久化和审计。HTTP 请求不能携带命令、参数数组、文件路径或平台私有配置字典。

首版的整机不变量如下：

1. 受管接口只能来自平台发现结果和 Linux Profile allowlist；客户端只能引用后端返回的稳定接口 ID。
2. 整机最多一个 `primary` 接口。它提供非作用域默认路由和系统默认 DNS；非主接口只保留地址、前缀和直连路由。
3. 默认路由、系统 DNS、主出口切换和目标接口配置属于同一个候选事务，不能按接口分别提交。
4. 同一时刻最多一个候选事务。应用、确认、取消、超时、重启恢复和恢复出厂都由同一串行协调器处理。
5. 候选配置必须先持久化恢复信息，再修改宿主机；候选状态必须从平台实际回读验证后才能返回成功。
6. 出厂基线不可变；确认后的最后有效配置通过临时文件、校验和和原子替换保存。

`R5.6` 与 `R6.1` 的关系是：Linux 没有 Profile 时可以创建 DHCP/DNS 自动的只读出厂基线，但 ownership 未证明前禁止所有写入；Profile 安装并通过校验后才解除只读状态。

## 2. 模块与依赖方向

### 2.1 后端模块

建议新增以下边界，最终包名采用 `internal/pkg/netconfig`，避免把业务服务和标准库`net` 混在一起：

```text
app/cmd/api/main.go
  -> NetworkRuntime.Start/Close
app/internal/router
  -> app/internal/api/network.go
  -> app/internal/service/network.go
  -> app/internal/pkg/netconfig
       -> linux rtnetlink/DHCP/DNS implementation
       -> darwin SystemConfiguration implementation
       -> fake implementation for tests
  -> app/internal/pkg/netconfig/state.go
       -> root-only atomic state files
```

`NetworkService` 不使用 GORM repository。网络当前状态是宿主机运行态，候选和恢复信息是固定目录中的本地事务日志；数据库只保存 HTTP/系统操作日志。该边界延续`app/internal/pkg/storage/storage.go` 的“小接口 + provider + 测试替身”模式。

当前 `app/Dockerfile` 用 `CGO_ENABLED=0` 构建 Linux Alpine 二进制，`deploy/docker-compose.yml` 只挂载 uploads、暴露普通容器端口，未提供 host network、`NET_ADMIN`/`NET_RAW`、宿主 resolver 或 root-only network state。网络能力首版因此只在原生 host-root 部署矩阵验收；默认容器形态必须在能力检查阶段 fail closed。

### 2.2 平台接口

平台包只暴露封闭的 Go 类型，概念接口如下，具体命名可在实现时按仓库风格微调：

```go
type Platform interface {
    Probe(ctx context.Context) error
    Discover(ctx context.Context) ([]InterfaceInfo, error)
    Read(ctx context.Context) (HostSnapshot, error)
    Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error)
    Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error)
    Close(ctx context.Context) error
}
```

`HostPlan` 只允许 `dhcp|static`、IPv4、prefix、默认网关、DNS、`primary` 等枚举和结构化字段。`HostSnapshot` 由平台产生，包含实际接口状态、非作用域默认路由、系统 DNS、平台指纹和用于恢复的受版本控制 native snapshot；业务层不能从 JSON 反序列化任意 native 数据后传回平台。`Apply`/`Restore` 内部自行校验 snapshot 版本、接口指纹和所有权。

平台包另提供：

- `Clock`、`Platform` 和 `StateStore` 的 fake/provider，用于纯单元测试；
- Linux、macOS 各自的能力检查和 build-tag 实现；
- 只读 `OwnershipStatus`，区分 `managed`、`unproven`、`conflict`、`unsupported`，不把
  “未发现已知管理器”表述为所有外部脚本均不存在。

### 2.3 业务服务与生命周期

`NetworkService` 持有平台、状态存储、操作日志服务、时钟和 logger。构造函数只组装依赖，不得启动 DHCP、定时器或恢复动作。生命周期由 `main` 显式调用：

1. `config.Load` 后立即执行可单测的 root 检查；非 root 在初始化数据库和监听端口前失败。
2. Wire 装配完成、数据库 schema 检查通过后，调用 `NetworkService.Start`。
3. `Start` 获取本进程/状态目录锁，执行启动恢复、首次接管基线初始化、平台能力检查，然后启动 DHCP lease workers、rtnetlink/Dynamic Store 漂移观察和候选超时协调。
4. `Start` 失败时不得启动 HTTP listener；恢复失败的接口可以让服务启动为只读 degraded，但应明确记录并让写 API 返回 recovery-failed。
5. 收到 SIGINT/SIGTERM 后先停止 HTTP 接收，再调用 `NetworkService.Close`，取消 leaseworkers 和定时器，等待平台调用结束，最后关闭 logger。

`App` 需要增加 `Network NetworkRuntime` 或等价生命周期字段。不要把副作用隐藏在`router.New` 或 provider 构造函数中；`wire.go` 只声明 provider，`wire_gen.go` 由 `make wire`生成，不能手改。

## 3. 规范化模型和主出口

### 3.1 逻辑模型

业务层维护完整的 `HostPlan`，而不是把局部 patch 直接交给平台：

- `interfaces`: 每个受管稳定 ID 一个 `InterfacePlan`；
- `mode`: `dhcp` 或 `static`；
- `address`、`prefix`: 静态模式必填，DHCP 模式必须为空；
- `gateway`、`dns`: 只有主出口可以设置；静态主出口必填，非主出口必须为空；
- `primaryInterfaceID`: 可为空，表示当前没有整机出口；只要存在非作用域默认网关或系统
  默认 DNS 就不能为空。

API 一次只接受一个接口的用户输入，服务读取当前完整计划后合并目标接口；如果目标被提升为主出口，服务同时生成旧主出口的降级计划。这样 Linux 的默认路由/DNS 和 macOS 的 service order/PrimaryService 都能在一个完整计划中验证和回滚。

### 3.2 应用规则

提交前按以下顺序执行，不触碰宿主机：

1. 解析并规范化 IPv4 与 prefix，拒绝非 IPv4、prefix 不在 `0..32`、广播/网络地址等
   不符合项目策略的值；静态地址与网关必须在同一可达子网。
2. 验证 DNS 是合法 IPv4、去重、数量在平台上限内；MVP 最多 3 个系统 DNS。
3. 验证接口 ID 当前存在、属于可写集合、平台指纹没有变化，且没有 recovery/ownership
   conflict。
4. 将请求合并到完整 `HostPlan`，验证主出口唯一性、主/非主出口字段约束和当前平台能力。
5. 读取 `before` 实际快照；如果实际状态与最近确认状态存在外部漂移，拒绝写入并记录
   conflict，不用写操作覆盖未知变更。

应用时平台负责补偿性顺序，而不是假设 rtnetlink 或 SystemConfiguration 提供跨对象原子事务：保存旧快照后，按地址、直连路由、默认路由、DNS、DHCP lease 的依赖顺序修改；任何一步失败都调用幂等 `Restore(before)`。成功后必须再次 `Read`，验证目标接口、primary、非作用域默认路由和系统 DNS 与计划一致。

DHCP 的动态细节不进入用户静态配置模型：主出口 DHCP 的租约网关/DNS 可安装为整机值，非主出口 DHCP 客户端只安装地址和前缀，忽略其默认路由与 resolver 更新。DHCP 回滚只承诺恢复 DHCP 模式并重新 DORA/续租，不承诺拿回原 IP。

## 4. 候选事务状态机

### 4.1 持久状态

状态目录由配置指定但不是 HTTP 输入；生产默认使用平台固定的 root-only 目录。目录、父目录、文件必须拒绝 symlink，owner 为 root，权限不允许 group/other 写入；状态目录不得被静态文件服务挂载。

至少包含以下文件：

```text
factory.json       # 首次接管基线，不可变
last-valid.json    # 最近一次确认且实际验证成功的完整 HostPlan/snapshot
pending.json       # 最多一个候选事务，含 before/candidate/deadline/stage
lock               # 进程/状态目录互斥
```

每个 JSON 是版本化 envelope，包含 `schemaVersion`、`generation`、`platform`、稳定接口指纹、UTC 时间、内容 SHA-256 和数据。`pending.json` 还保存 transaction ID、boot ID、`before` native snapshot、candidate plan、截止时间、stage、操作者 ID/用户名、来源 IP、脱敏 action summary 和客户端连接提示。审计所需身份信息不得只存在 Gin context。

原子写入使用同目录临时文件、严格 mode、完整写入、`fsync`、`rename`、目录 `fsync`；先写 pending 再应用平台。factory 只允许安全创建一次，任何覆盖或校验失败都进入故障状态。

### 4.2 状态转换

```text
idle
  -> prepared       pending 已持久化，尚未修改平台
  -> applying       平台补偿性应用中
  -> pending_confirmation  实际回读与 candidate 相符，启动 120s
  -> committing     用户确认，正在固化 last-valid
  -> idle           last-valid 原子替换成功，pending 删除

prepared/applying/pending_confirmation
  -> rolling_back   取消、超时、应用失败、关闭或启动发现未完成事务
  -> idle           before 恢复并回读成功，pending 清理
  -> recovery_failed  恢复失败或外部状态冲突，禁止后续写入
```

所有 HTTP 写入和后台回调都经过一个串行 mutex。候选期间新的 apply、factory reset、DHCP 改变整机默认值的后台动作一律排队或拒绝；对外返回 transaction-pending。确认按以下顺序防止崩溃窗口：

1. 把 pending stage 原子更新为 `committing`；
2. 原子写入 `last-valid.json`；
3. 删除 pending 并 fsync 目录。

重启看到 `committing` 时，如果实际状态与 candidate 相符，补完成清理；否则先尝试恢复 before，不能把未知实际状态直接固化。

### 4.3 超时与重启

超时使用单调时钟运行时计时，deadline 同时保存墙上 UTC 时间供重启恢复。超时回滚必须可重复执行；回滚完成后读取实际状态并记录后台审计。进程在 pending 状态重启时，`Start` 在开放 HTTP 前优先恢复 before；不得因为进程重启而确认候选。

如果平台报告当前配置已被外部修改，恢复操作不应盲目覆盖外部新配置；记录冲突、标记接口 recovery-failed 或 ownership-conflict，并停止写入，等待人工恢复。只有平台快照指纹仍匹配候选时，才执行自动 before 回滚。

## 5. Linux 实现

- 接口发现以 root-only Profile allowlist 为准；首次按部署声明定位后持久化永久 MAC/硬件指纹和 ifindex 校验，客户端不能传入名称直接越过 allowlist。
- link、IPv4 地址、直连路由和默认路由使用 `github.com/vishvananda/netlink` 的`NETLINK_ROUTE` API；只操作受管 ifindex 和本系统记录的精确对象，不 flush 全局路由。
- DHCPv4 使用 `github.com/insomniacslk/dhcp`，本系统拥有每个受管接口的 DORA、T1/T2、renew、rebind、release 和链路取消生命周期。DHCP option 中的地址、网关、DNS、classlessroute 在安装前重新验证。
- 非主 DHCP 租约不安装默认路由和 DNS；主 DHCP 租约才能更新系统共享值。默认路由切换时删除/恢复精确的本系统 route tuple。
- DNS 只写 Profile 明确独占的普通 `/etc/resolv.conf`。symlink、只读文件、已知 resolver 托管或 owner 不符时拒绝写入；写入同目录临时文件并 fsync/rename。Linux 没有统一内核 DNSAPI，不能用 rtnetlink 假装解决 resolver 所有权。
- 对 NetworkManager、systemd-networkd、ifupdown、ConnMan 做 best-effort 冲突检测，订阅 rtnetlink 事件检测实际漂移；不声称可识别所有厂商脚本。声明缺失、已知管理器托管或外部覆盖时读可用、写拒绝。

## 6. macOS 实现

- 从当前 `SCNetworkSet` 枚举既有 `SCNetworkService`，只接受 Ethernet 和 IEEE80211；持久 ID 使用 `SCNetworkServiceGetServiceID`，service 名称/BSD name 只用于展示和校验。service 删除重建或 set 指纹变化时停止写入，不能按名称猜测。
- IPv4/DNS 通过 SystemConfiguration protocol 字典读写；先复制完整原字典，保留未管理字段。DHCP 使用 `ConfigMethod=DHCP`，静态使用 Manual/Addresses/SubnetMasks/Router，DNS 使用`ServerAddresses`。不触碰 CoreWLAN 的 SSID、密码或关联。
- 候选先用 `SCPreferencesLock` 获取排他锁，保存原始 IPv4/DNS 字典、service/set 指纹以及 service order/primary override，再 SetConfiguration、`SCPreferencesCommitChanges`、`SCPreferencesApplyChanges`。commit 后仍是可回滚候选，不能当作用户已确认。
- 实际状态通过 `SCDynamicStore` 读取 service IPv4/DNS 和 DHCP 信息，并读取实际`PrimaryService`/`PrimaryInterface` 验证主出口。仅 preferences 写成功不足以返回应用成功。
- 主出口切换把 service order、`OverridePrimary`（目标系统支持时）和 DNS/IPv4 变更纳入同一候选；非主 DHCP 可能留下系统内部 scoped route/resolver，但若 Dynamic Store 把它报为整机非作用域 PrimaryService 或默认 DNS，则应用失败并回滚。对外只承诺非作用域主出口唯一，不承诺删除所有系统内部 scoped 状态。
- Go bridge 使用 `bridge_darwin.c/.h` 释放所有 CF 对象并只返回窄结构化结果；`manager_darwin.go` 要求 cgo，`manager_darwin_nocgo.go` 显式返回 unsupported。Linux 构建不包含 CoreFoundation 引用。

## 7. 启动恢复与所有权

启动检查顺序：

1. root、固定目录权限和单进程锁；
2. 平台 `Probe` 与 `Discover`，核验 service/硬件指纹；
3. 校验 factory、last-valid、pending envelope 和 boot/generation；
4. 有 pending 时先按 stage 回滚或完成 committing 收尾；
5. 实际状态不匹配 last-valid 时恢复 last-valid；失败再尝试 factory；
6. factory 损坏、无法恢复或平台能力缺失时标记 recovery-failed，记录系统审计，禁止写入；
7. 完成读取和 lease/漂移 worker 后才允许监听 HTTP。

首次接管时：Linux 读取 Profile；缺失时建立 DHCP/DNS 自动 factory 但保持 `unproven` 只读；macOS 保存完整 IPv4/DNS、service/set 指纹与主出口相关 preference 快照。最后有效配置只在用户确认且平台实际验证成功后写入。

## 8. API、RBAC、审计与前端数据流

完整 endpoint、DTO、错误码和权限见同目录 `api.md`。这里固定跨层规则：

- API handler 只负责绑定封闭 DTO、解析 ID、调用 service、`c.Error` 和 `response.Success`，不调用 netlink/SystemConfiguration。
- router 在 `app/internal/router/router.go` 注册 `/api/network`，GET 也显式注册`ops:network`，写路由分别注册 apply/confirm/cancel/reset 权限；未注册写路由保持默认 403。
- `internal/pkg/errno` 添加网络错误码和三语文案，并更新 `error_handler.go` 的 HTTP 状态映射；响应仍为 `{code,data,message}`。
- Oplog middleware 的 HTTP apply/confirm/cancel/reset 通过 `actionI18nMap` 记录；网络写路径必须使用同步 Record（或等价的可恢复 outbox），不能沿用现有“goroutine 失败只 warn”的 best-effort 语义。推荐 Oplog 根据 `/api/network` 写路径切换同步记录，保持最终 HTTP 状态、脱敏摘要和 actor 后再返回；后台超时、启动恢复、DHCP lease 失败/恢复调用 `OperationLogService.Record`，使用 system actor、固定内部动作 key 和脱敏 JSON summary。现有日志表无需保存 raw platformsnapshot。
- 菜单通过下一版本化 migration 和 `model/seed.go` 同步增加运维管理下的 Network 页面及 read/edit/confirm/cancel/reset 权限，使用 `OpsNetwork`、`/ops/network`、`routes.ops.network` 和 `ops:network*`。操作日志动作文案仍放在现有 `system.log.*` namespace；前端不添加静态 route，后端动态菜单的 component 指向网络页面。
- 前端 `src/api/core/network.ts` 提供 overview、apply、transaction、confirm、cancel、reset 方法并从 `core/index.ts` 导出；页面使用现有 `useVbenForm`/Zod，显示当前实际状态和完整候选摘要，主出口切换明确展示默认路由/DNS 迁移与断连风险，倒计时通过服务端 deadline 计算。

## 9. 兼容性、失败关闭和部署

- Linux 需要 root、`CAP_NET_ADMIN`、DHCP 所需 raw packet 能力和 Profile；macOS 需要 root、cgo 及 SystemConfiguration framework。两端缺能力时返回明确 unsupported，不静默降级到 shell 命令。
- 新状态 envelope 通过 `schemaVersion` 拒绝未知版本并保留 factory；旧/损坏状态不能被空值覆盖。升级部署先创建目录、权限和 Profile，再启用写权限菜单。
- 自动化普通测试只使用 fake platform、临时时钟和临时状态目录。Linux 特权集成使用独立 network namespace/veth/DHCP test server；macOS 集成使用隔离 Network Service，均需显式环境变量/build tag，并在每个退出路径恢复快照。
- 确认恢复出厂候选后，factory baseline 成为该完整 HostPlan 的唯一恢复来源：按 R6.4 清理该接口/整机范围的 last-valid，而不是留下与 factory 主出口矛盾的正式快照；后续普通确认才重新生成 last-valid。若清理过程中进程重启，pending stage 保证启动恢复先完成事务再清理。
- 回滚点是：禁用新菜单写权限、停止 NetworkService 写入并保留只读状态、恢复部署 Profile；不允许通过删除 state files 规避恢复协议。
