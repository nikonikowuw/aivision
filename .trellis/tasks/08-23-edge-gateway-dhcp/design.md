# 边缘网关模式技术设计

状态：`draft`（Phase 1 planning）  
对应 PRD：`.trellis/tasks/08-23-edge-gateway-dhcp/prd.md`  
基准代码：`dev` 分支，已包含 active-backup 业务层框架

## 1. 目标与边界

本 child 在既有整机网络模式事务上增加 `gateway` 模式：为一个非主出口、静态 IPv4 的下行接口启动绑定到该接口的内置 DHCPv4 Server，并按显式开关设置 `net.ipv4.ip_forward`。模式切换、确认、取消、超时回滚、恢复出厂和启动恢复都继续使用 08-22/active-backup 已建立的单一候选事务。

本 child 负责：

- `gateway` 模式模型、能力声明和现有 `PUT /api/network/mode` 的增量请求分支；
- 地址池校验、DHCP Server 生命周期、冲突探测、运行期冲突告警；
- `ip_forward` 读写、租约分配与 root-only 租约文件；
- fake runtime 的 CI 语义、Linux runtime 的结构化实现、API、审计和前端面板。

本 child 不负责：

- Linux 接口地址、路由、bond 的 rtnetlink 真实化；该能力由
  `08-23-linux-platform-realization` 提供；
- NAT、SNAT/DNAT、iptables/nftables、防火墙、DNS 转发、DHCP relay；
- macOS 等价实现、IPv6、多个下行 DHCP Server、静态 MAC 保留地址。

在平台层真实化完成前，fake Linux 平台可完成全部业务验收；真实 Linux 的 `gateway` capability 必须等平台层提供可用接口状态、权限和接口 ID 映射后再开启，不能用 fake 结果冒充目标机能力。

## 2. 现有代码边界

- `app/internal/pkg/netconfig/types.go` 已有 `NetworkMode`、`HostPlan`、`HostSnapshot`、`PendingTransaction` 和 `NetworkOverview`；gateway 应沿用这些封闭模型，不建立第二套模式事务。
- `app/internal/service/network.go` 已由 `Start`/`Close` 管理服务生命周期，`SwitchMode`、`ConfirmTransaction`、`CancelTransaction`、`FactoryReset` 和 `handleTimeout` 共享一个 `pending.json` 槽位。
- `app/internal/pkg/netconfig/state.go` 已提供 root-only 目录、schema envelope、SHA-256 校验、临时文件 + fsync + rename；租约文件复用该机制，不使用数据库。
- `app/internal/api/network.go` 与 `router.go` 已有 `PUT /api/network/mode` 和 `ops:network:mode`，租约不需要新权限或新菜单。
- 当前 `app/go.mod` 尚无 `github.com/insomniacslk/dhcp`；激活前必须验证同时支持 `dhcpv4` 与 `server4` 的版本并锁入模块，不能把 research 文档中的候选库当成已安装事实。

## 3. 领域模型

### 3.1 计划、运行状态与租约

在 `netconfig/types.go` 增加以下类型，字段使用现有 camelCase JSON 约定：

```go
type GatewayPlan struct {
    DownstreamInterfaceID string `json:"downstreamInterfaceId"`
    PoolStart             string `json:"poolStart"`
    PoolEnd               string `json:"poolEnd"`
    Prefix                int    `json:"prefix"`
    LeaseDurationSeconds  int64  `json:"leaseDurationSeconds"`
    IPForward             bool   `json:"ipForward"`
}

type GatewayState struct {
    Plan                *GatewayPlan `json:"plan,omitempty"`
    Running             bool          `json:"running"`
    IPForward           bool          `json:"ipForward"`
    PreviousIPForward   *bool         `json:"previousIpForward,omitempty"`
    ConflictDetected    bool          `json:"conflictDetected"`
}

type GatewayLease struct {
    MAC           string    `json:"mac"`
    IP            string    `json:"ip"`
    StartsAt      time.Time `json:"startsAt"`
    ExpiresAt     time.Time `json:"expiresAt"`
    LastRenewedAt time.Time `json:"lastRenewedAt"`
    Hostname      string    `json:"hostname,omitempty"`
}
```

`NetworkMode` 新增 `NetworkModeGateway = "gateway"`，`AllNetworkModes` 和 `Valid` 同步扩展。`GatewayState.PreviousIPForward` 只供内部快照恢复，不映射到 HTTP DTO。

既有模型只做增量字段：

```go
type HostPlan struct {
    // 既有字段保持不变
    Gateway *GatewayPlan `json:"gateway,omitempty"`
}

type HostSnapshot struct {
    // 既有字段保持不变
    Gateway *GatewayState `json:"gateway,omitempty"`
}

type NetworkOverview struct {
    // 既有字段保持不变
    Gateway *GatewayOverview `json:"gateway"`
}
```

`GatewayOverview` 是对外投影，包含下行接口、池范围、prefix、租约时长、当前 `ipForward`、`running`、`conflictDetected` 与 `leases`；不返回 `PreviousIPForward`、状态目录、socket、原始 DHCP 报文或平台错误。

### 3.2 兼容性

- `CurrentSchemaVersion` 仍为 1；旧的 factory/last-valid/pending envelope 反序列化后 `Gateway` 为 nil。
- 租约使用独立文件 `gateway-leases.json`，缺失表示空租约表，不修改既有三个状态文件的 checksum。
- `GatewayPlan` 只出现在 `mode=gateway` 的候选/最后有效配置；退出模式时写 nil。
- 既有客户端忽略 overview 新增的 `gateway` 字段即可继续使用多址和接口 IPv4 API。

## 4. Runtime 抽象

不要把 DHCP library、`/proc` 路径或 raw socket 暴露到 service/API。`netconfig` 增加窄接口：

```go
type GatewayRuntime interface {
    Snapshot(ctx context.Context) (GatewayState, error)
    Probe(ctx context.Context, plan GatewayPlan) (responded bool, err error)
    Apply(ctx context.Context, plan GatewayPlan, before GatewayState) (GatewayState, error)
    Restore(ctx context.Context, before GatewayState) (GatewayState, error)
    Leases(ctx context.Context) ([]GatewayLease, error)
    Close(ctx context.Context) error
}
```

`networkService` 持有一个 runtime 实例。构造函数只组装依赖；runtime 不在构造函数启动 goroutine、打开 socket 或写 sysctl。副作用只允许出现在 `Start`、模式事务操作和 `Close`。

runtime 内部再隔离平台后端：

```go
type GatewayBackend interface {
    ReadIPForward(ctx context.Context) (bool, error)
    WriteIPForward(ctx context.Context, enabled bool) error
    ProbeDHCP(ctx context.Context, interfaceName string, prefix netip.Prefix) (bool, error)
    StartDHCP(ctx context.Context, cfg DHCPServerConfig, leases LeaseStore) (DHCPServer, error)
}
```

- `FakeGatewayBackend`：内存模拟冲突探测、转发开关、server 生命周期和客户端租约事件；测试可注入失败点。
- `LinuxGatewayBackend`：用 Go 文件 API 读写 `/proc/sys/net/ipv4/ip_forward`，用绑定接口的 `server4` 启动 DHCP，不调用 shell，不写防火墙规则。接口 ID 到系统接口名的转换由 Linux 平台层的受管接口信息提供，不能直接信任 HTTP 输入。
- Darwin/其他平台不提供可用 backend；即使构造了 runtime，也必须先由 capability 拦截并返回 `CodeNetworkUnsupported`。

## 5. 地址池与模式校验

校验全部发生在 `SwitchMode` 写入 pending 或调用任何平台写操作之前；失败时平台快照、runtime 状态、租约文件和 `ip_forward` 均不改变。

`ValidateGatewayPlan` 使用 `net/netip`，并以 overview 中的接口状态为事实来源：

1. 目标接口存在、`Writable=true`、`IsBond=false`、`MasterID=nil`，且不是当前 `PrimaryInterfaceID`。
2. 目标接口必须是 `IPModeStatic`，具有有效 IPv4 地址和 prefix；DHCP client 接口返回 `CodeNetworkDhcpServerConflict`。
3. 请求 `prefix` 必须是有效的 IPv4 LAN prefix，并与目标接口 prefix 相同；使用 prefix 作为 08-22 API 的唯一掩码表示，页面负责展示 dotted mask。
4. `poolStart`/`poolEnd` 必须是 IPv4，且 `poolStart <= poolEnd`；两者都必须在接口子网内。
5. 地址池不得包含网络地址、广播地址或下行接口自身地址；/31、/32 不作为 DHCP LAN 子网接受。
6. `leaseDurationSeconds` 默认 3600，允许 `[60, 604800]`，越界返回 `CodeNetworkGatewayPoolInvalid`。
7. `IPForward` 只是目标开关，不参与池地址计算；其真实旧值由 runtime 在写入前读取并进入 `GatewayState.PreviousIPForward`。

模式 capability 和模式冲突校验沿用 `SwitchMode` 现有顺序：未就绪、pending 冲突、能力不支持、当前快照读取、同模式冲突、gateway 专属校验。所有校验完成后才执行 DHCP 冲突探测；探测收到 DHCP 响应返回 `CodeNetworkDhcpServerConflict`，探测基础设施失败返回 `CodeNetworkApplyFailed`，两者都不写入平台。

## 6. DHCP Server 与租约

### 6.1 Server 行为

- socket 绑定到目标下行接口和 DHCP 服务端口，不监听 `0.0.0.0`；接口关闭或 runtime 停止时关闭 socket 并等待 worker 退出。
- DORA、renew、rebind 和 release 由 server handler 统一进入租约表 mutex；同一 MAC 在有效租期内优先复用原 IP。
- 池内地址按稳定顺序分配，先回收过期 lease，再选择未占用地址；没有可用地址时不覆盖其他有效租约，记录受控 zap warning。
- 下发选项仅包括 yiaddr、subnet mask、lease time 和 router（下行接口地址）；不下发 DNS、域名、NTP、TFTP 或厂商私有选项。
- 从 DHCP Host Name option 读取 `hostname`，没有则省略；不保存原始 DHCP 报文。
- 分配、续租、释放后通过 `LeaseStore.Save` 原子落盘；租约事件只写结构化 zap，不写 OperationLog。

### 6.2 租约存储

增加独立 `GatewayLeaseStore`：

```go
type GatewayLeaseStore interface {
    Load(ctx context.Context) ([]GatewayLease, error)
    Save(ctx context.Context, leases []GatewayLease) error
    Clear(ctx context.Context) error
}
```

`FileGatewayLeaseStore` 使用现有 `FileStateStore` 的 envelope 实现，数据包含内部版本号和租约数组，文件权限为 0600，目录为 0700，写入采用临时文件、fsync、rename 和目录 fsync。读取 checksum/版本失败返回 `CodeNetworkStateCorrupt`，不能静默当成空表。

退出 gateway、取消、超时、恢复出厂成功进入非 gateway 状态时清除租约表；进程 `Close` 只停止 server 并保留已确认租约，下一次 `Start` 从文件恢复有效 lease。过期租约在加载和首次分配时清理。

### 6.3 冲突监测

启用前执行一次阻塞式 DHCP probe；启用后由 runtime 以命名常量间隔执行同接口探测。发现其他响应时设置 `ConflictDetected=true` 并写 warning，但不停止本系统 server，避免瞬时探测误报造成全部摄像头租约中断。探测恢复无响应时清除标记并写 info；overview 每次读取从 runtime 快照投影当前值。

## 7. 事务、恢复与生命周期

`networkService` 增加 `readHostSnapshot` 和 `restoreHostSnapshot` 两个内部编排点：前者合并 platform snapshot 与 runtime snapshot，后者按资源依赖恢复 runtime 和 platform。

### 7.1 进入 gateway

1. 检查 ready、pending、capability、模式冲突和 gateway plan。
2. 读取 platform + runtime 的完整 `before` 快照；读取旧 `ip_forward`。
3. 执行启用前 DHCP probe；收到应答或 probe 失败立即返回。
4. 将 `HostPlan.Gateway`、`before.Gateway` 和完整候选写入 `pending.json`。
5. 先调用 `platform.Apply` 应用完整 host plan（目标下行接口已经是静态 IPv4，平台层不得把它切回 DHCP）。
6. 调用 `runtime.Apply`：保存 `before.IPForward`，设置目标 0/1，启动绑定接口的 DHCP Server 和冲突监测。
7. 两步均成功后启动既有 120 秒 timer，返回 `pending_confirmation` 和下行接口的重连地址（若有）。

任一步失败：先恢复 runtime，再恢复 platform；两者都成功才清除 pending 并返回 `CodeNetworkApplyFailed`。恢复失败保留 pending、记录 `CodeNetworkRecoveryFailed` 所需状态，禁止返回“已自动恢复”的假成功。

### 7.2 退出 gateway、取消与超时

退出模式的候选 `HostPlan.Gateway=nil`，保留/恢复原有接口 IPv4 plan。执行顺序为：

1. `runtime.Restore(before.Gateway)`：停止 server、释放 socket、停止 monitor、清除租约、恢复进入模式前的 `ip_forward`；
2. `platform.Apply` 应用多址候选；
3. 确认后写 last-valid，清除 pending。

取消和超时使用同一个 `restoreHostSnapshot`：先恢复 runtime，再恢复 platform。若 platform 恢复失败但 runtime 已恢复，保留故障状态并记录审计；不能清除仍可用于重试的 pending。

### 7.3 恢复出厂

`FactoryReset` 生成的 factory plan 必须保证 `Gateway=nil`。当当前状态是 gateway 时，恢复出厂与退出模式使用同一反向清理序列，包含停止 DHCP、释放 socket、恢复 `ip_forward`、清理租约，再应用 factory interface plan，并继续沿用 120 秒确认协议。

### 7.4 Start/Close

- `Start` 初始化 state/lease store、执行平台 probe 和 pending 启动恢复；发现 dangling pending 时恢复其完整 before（runtime 在前、platform 在后）。无 pending 时，如 last-valid 为 gateway，则按已确认 gateway plan、保存的 `PreviousIPForward` 和 lease 文件启动 runtime，不再次要求用户确认。
- 已确认 gateway 的 runtime 启动失败时服务不得标记为 ready；返回明确启动错误并保留状态文件，避免 API 显示“运行中”但没有 DHCP 服务。
- `Close` 停止 DHCP server、monitor 和 worker，等待 goroutine 退出，保留已确认 lease/plan，不清理 `ip_forward` 或把已确认配置伪装成退出模式；下一次 `Start` 负责恢复。

## 8. API、权限与审计

沿用 `PUT /api/network/mode`，只新增 `gateway` 请求分支，不新增模式切换端点。`GET /api/network` 的 `gateway` 投影同时提供 overview 状态和租约列表，因此不新增租约查询权限或路由。

请求校验由 handler 做结构校验，service 做所有语义校验。错误码新增：

- `1115 CodeNetworkGatewayPoolInvalid`：地址池、prefix 或租约时长非法，HTTP 400；
- `1116 CodeNetworkDhcpServerConflict`：下行接口为 DHCP client 或探测到已有服务，HTTP 409。

需要同步 `errno` 三语文案和 `error_handler.go` 的 HTTP 映射。模式切换继续使用 `ops:network:mode`，overview/lease 继续使用 `ops:network`。

审计：模式提交、确认、取消、超时、恢复出厂和 `ip_forward` 变化记录现有 OperationLog action key；租约分配不入审计表。建议新增 `system.log.actionNetworkGatewaySwitch`，回滚复用 `system.log.actionNetworkRollback`，摘要只包含目标接口、池范围、目标模式、`ipForward` 和结果，不记录原始报文、路径或平台错误。

## 9. 前端

在 `views/ops/network/index.vue` 的既有模式表单中增加 gateway 分支：

- 从 `supportedModes` 判断是否渲染/禁用 gateway；fake Linux 可用，macOS 不显示可提交能力；
- 下行接口只列出静态、可写、非主出口、非 bond/slave 接口；
- 地址池起止、prefix、租约时长和 `ip_forward` 开关均有类型化模型，默认租约 3600 秒；
- 提交按钮之前始终显示“该接口所在广播域将提供 DHCP，确认未接入生产网络”的警示；
- `ip_forward` 旁始终显示“仅做三层转发、不做 NAT；下行设备不能主动访问上行网段之外目标，上行主机访问摄像头需要回程路由”；
- 复用当前 pending 倒计时、确认、取消和断线重连地址交互；
- gateway 生效后从 overview.gateway.leases 渲染 MAC、IP、租期起止、最近续租和 hostname，并显示 conflict 状态；
- 所有可见文案三语维护在 `ops.json`，时间使用现有全局时区格式化，不在页面硬编码 ISO 字符串。

不新增 store；gateway 表单和 pending 继续使用页面局部状态，API 类型集中在 `api/core/network.ts`。

## 10. 测试边界

- netconfig：gateway mode 枚举/旧 envelope 兼容、地址池边界、prefix/接口/主出口/DHCP client 校验、lease envelope 原子读写和损坏拒绝。
- fake runtime：无冲突启动、冲突拒绝、socket/worker 关闭、lease 复用/过期回收、ip_forward 三条路径、运行期 conflict 标志。
- service：进入成功、进入失败补偿、退出、cancel、timeout、startup recovery、factory reset、pending 冲突、capability 不支持；每个失败用例断言平台和 runtime 没有部分修改。
- API：gateway 请求 shape、模式/字段互斥、1115/1116 HTTP 映射、403 权限和 overview DTO；旧 active-backup/multi-address 请求回归。
- frontend：capability gating、表单字段联动、风险提示始终可见、NAT 后果文案、lease 列表和三语 key 对齐。
- Linux 特权集成（独立 build tag/network namespace）：真实 socket 绑定、DORA/renew/release、重启恢复、ip_forward 恢复；不触碰开发机默认网络。

## 11. 回滚点

- M1 模型/状态/纯校验可独立回退，不改变现有状态 schema。
- M2 runtime/backend 只在 gateway capability 开启后生效；若目标机失败可关闭 capability，保留只读 overview。
- M3 service 事务改动回退前先停止 gateway server、恢复 `ip_forward` 并清理 lease；不得只回滚代码而留下系统资源。
- M4 API/errno/前端可独立回退；已有 multi-address 与 active-backup 请求体保持兼容。
