# 链路聚合网络模式技术设计（LACP 802.3ad）

状态：`draft`
对应 PRD：`prd.md`
基准代码：当前 `dev` 分支；前置 active-backup 已在 `12cfa8f` 实现并归档

## 1. 目标与边界

在现有整机 `NetworkMode`、`Platform.Capabilities`、单一候选事务、120 秒确认窗口和网络模式前端面板上，增加 Linux bonding mode 4（802.3ad LACP）。本 child 交付业务层与 fake 平台可测试语义；真实 Linux rtnetlink 下发和真实协商状态读取由平台层真实化 child 提供实现接线，但本设计定义其结构化契约。

本设计不新增模式切换通道，不修改 macOS 使其模拟 LACP，不自动配置交换机，不承诺单条 TCP 连接跨多条物理链路，也不把对端未配置 LAG 当作应用失败。

## 2. 复用边界与模式转换

### 2.1 复用既有机制

- 模式切换继续使用 `NetworkService.SwitchMode` 与 `PUT /api/network/mode`。
- `HostPlan.Mode` 标识整机模式，`HostPlan.Bond` 携带最终 bond 拓扑参数；不创建第二套 LACP plan。
- `PendingData.Before` 保存完整的切换前快照；应用失败、用户取消、120 秒超时和启动恢复均调用同一个 `Platform.Restore`。
- `last-valid.json` 保存确认后的完整计划和快照；退出 bond 时从其中恢复 slave 的原 IPv4 与主出口信息。
- `ops:network:mode`、confirm/cancel 权限、既有审计 action 和状态文件 schema 版本均复用，不新增 migration。

### 2.2 bond 到 bond 的转换

`Platform.Apply` 接收的是目标最终状态，平台层负责在候选事务内拆除旧 bond 并建立目标 bond；回滚仍以 `Before` 为唯一恢复事实来源。

- 从 `multi-address` 进入 LACP：slave 必须来自当前可写、未占用的物理网卡集合。
- 从 active-backup 直接切到 LACP：允许复用当前 bond 的全部 slave（它们此时 `Writable=false`，但归属当前 bond）；请求不得混入其他接口。这样支持 parent 的跨模式集成验收，且不打开任意已占用接口的选择口。
- 从 LACP 直接切到 active-backup：只有当前 LACP 恰好有 2 个 slave 且请求使用同一组 slave 时才允许；多于 2 个 slave 或更换 slave 集合时先退回 `multi-address`，再重新选择，返回既有 `CodeNetworkBondModeConflict`。
- 其他目标模式退出 LACP 时，按现有 active-backup 的退出路径删除 bond、归还 slave，并从 `last-valid` 恢复原接口计划。

## 3. 领域模型扩展

### 3.1 模式与封闭参数

在 `app/internal/pkg/netconfig/types.go` 增加：

- `NetworkModeLACP NetworkMode = "lacp-aggregation"`，加入 `AllNetworkModes()`。
- `BondXmitHashPolicy` 封闭枚举：`layer2`、`layer2+3`、`layer3+4`；默认值为 `layer2+3`。
- `BondLACPRate` 内部枚举只保留 `slow`，服务端固定填充，不接受 HTTP 输入；内核映射为 slow 的数值 0。

`BondPlan` 增加 `XmitHashPolicy` 与内部 `LACPRate` 可选字段；既有 `SlaveIDs`、`PrimarySlaveID`、`Miimon` 保留以兼容 active-backup。LACP 不使用 `PrimarySlaveID`，请求也不出现该字段。所有新增字段使用 `omitempty`，旧状态文件仍可反序列化。

hash policy 只在服务层接受上述三个字符串；平台适配层使用显式映射：`layer2 -> 0`、`layer3+4 -> 1`、`layer2+3 -> 2`。业务层和 JSON 不接受或透传任意内核字符串。`mode=4` 与 `lacp_rate=slow` 由平台从目标模式和封闭字段映射为 rtnetlink 属性；不使用 shell、sysfs 或 `/proc/net/bonding/bond0`。

### 3.2 LACP 状态 DTO

在 `netconfig` 中增加稳定的业务 DTO，平台负责把 netlink 原始数值转换为语义字段：

```go
type LACPPortState struct {
    Active         bool `json:"active"`
    ShortTimeout   bool `json:"shortTimeout"`
    Aggregation    bool `json:"aggregation"`
    Synchronized   bool `json:"synchronized"`
    Collecting     bool `json:"collecting"`
    Distributing   bool `json:"distributing"`
    Defaulted      bool `json:"defaulted"`
    Expired        bool `json:"expired"`
}

type LACPPortStatus struct {
    InterfaceID  string         `json:"interfaceId"`
    AggregatorID *uint16        `json:"aggregatorId,omitempty"`
    InAggregator  bool           `json:"inAggregator"`
    ActorState    LACPPortState  `json:"actorState"`
    PartnerState  LACPPortState  `json:"partnerState"`
}

type LACPStatus struct {
    AggregatorID   *uint16           `json:"aggregatorId,omitempty"`
    Negotiated     bool              `json:"negotiated"`
    Slaves         []LACPPortStatus  `json:"slaves"`
    DiagnosticCode string            `json:"diagnosticCode,omitempty"`
}
```

`BondTopology` 增加 `XmitHashPolicy`、`LACP *LACPStatus`。active-backup 的 `PrimarySlaveID`、`ActiveSlaveID`、`Miimon` 字段行为不变；LACP 时这些字段为空或保持零值，前端以 `LACP` 状态区渲染。`DiagnosticCode` 使用稳定 code 而非后端本地化句子，首个 code 为 `partner_not_configured`，表示没有任何 slave 同时进入可用聚合组；它只影响展示，不改变事务结果。

### 3.3 速率/双工和非阻断警告

`InterfaceInfo` 增加可选的 `SpeedMbps` 与 `Duplex`（`unknown|half|full`）字段。真实 Linux 平台从结构化链路能力读取，fake 提供可控默认值和测试注入点。服务在 LACP 提交前比较所有选中 slave 的已知速率与双工：发现不一致时仍继续应用，并在 `TransactionResult.Warnings` 及 `PendingTransaction.Warnings` 写入稳定的 `bond_slave_link_mismatch` warning code 与涉及接口 ID。未知值不伪造为一致，也不凭未知值阻断提交。

## 4. 能力协商与平台边界

### 4.1 Fake 平台

`FakePlatform` 的 `SupportedModes` 增加 LACP。其 `applyMode` 依据最终 `HostPlan.Mode` 创建固定 `bond0`，标记任意数量 slave 为 `MasterID=bond0`、`Writable=false`、清空 slave IPv4，并保留 bond IPv4 应用逻辑。fake 通过测试注入支持三种场景：

1. 所有 slave 已进入同一 aggregator，actor/partner 状态同步、collecting/distributing 为真；
2. 没有 slave 进入 aggregator，`Negotiated=false` 且 `DiagnosticCode=partner_not_configured`；
3. 只有部分 slave 进入 aggregator，`Negotiated=false`，每个 slave 独立返回状态与 aggregator ID。

fake 默认使用“已协商”场景保证成功路径简洁；测试显式切换到其他场景。fake 的 `Restore` 必须同步恢复 `mode`、`bond` 和完整接口快照，不能仅删除 bond0。

### 4.2 Linux 平台

`LinuxPlatform.Capabilities` 只在 `Probe` 已完成且 LACP 所需 bonding netlink 属性验证成功时声明 `lacp-aggregation`；未探测、探测失败或权限/内核能力不确定时均 fail closed，仅声明 `multi-address`。当前平台层仍是 fake 委托时不能伪造真实 Linux 能力；平台真实化 child 接入后，探针结果才允许打开该 capability。

真实 Apply/Read 的接线契约：

1. 创建/更新 bond 走 rtnetlink `IFLA_INFO_KIND=bond`，设置 mode 4、封闭映射后的 xmit hash policy、slow LACP rate 和既有 bond 监测参数；slave 绑定使用结构化 master 属性。
2. Read 通过 link-info/bond slave netlink 属性读取 aggregator ID、actor oper port state、partner oper port state 与成员归属，并转换为 `LACPStatus`；禁止解析 `/proc/net/bonding/bond0`。
3. 交换机未配置 LAG 时，只要内核成功创建 bond，Apply 视为成功，Read 返回未协商状态；只有内核拒绝 mode/属性或无法建立 bond 的平台失败才返回内部 LACP apply sentinel，由 service 映射为 `CodeNetworkLacpNegotiationFailed`（1114/503）。
4. 速率/双工不一致由平台提供给业务层并产生 warning；不因该 warning 自动回滚。

macOS capability 永远不声明 LACP，请求沿既有 capability 失败路径返回 `CodeNetworkUnsupported`。

## 5. 服务层数据流

`SwitchMode` 在触碰平台前依次完成：模式枚举与 capability 校验、pending 冲突、当前快照读取、目标拓扑冲突检查、slave 集合校验、hash policy 默认化与枚举校验、bond IPv4 校验、速率/双工 warning 计算、完整 `HostPlan` 构建。LACP 的最小 slave 数为 2，上限为当前可用/当前 bond 可重用的 slave 数；元素必须存在、物理、未被其他虚拟接口占用、指纹未漂移且不重复。

进入 LACP 时：

- `plan.Mode=NetworkModeLACP`；`plan.Bond.SlaveIDs` 保存选择顺序，`XmitHashPolicy` 写显式归一化值，`LACPRate` 写 `slow`；`PrimarySlaveID` 为空。
- bond IPv4 仍写入 `plan.Interfaces["bond0"]`；若为主出口，`PrimaryInterfaceID` 指向 bond0。
- 生成 `mode_switch` pending，保存 `TargetMode`、`PreviousMode`、warnings、before 快照和候选计划。

应用失败时，先按既有补偿调用 `Restore(before)` 并清理 pending；若错误是 LACP 平台拒绝 sentinel，返回 1114，否则沿既有 1107/1108 语义处理。partner 未协商不走错误分支。

`GetOverview` 和模式切换成功响应都从平台候选快照填充完整 `Mode`、`Bond`、`Capabilities`、接口列表和 pending；特别是没有成员进入 aggregator 时，响应必须携带 `diagnosticCode`。confirm/cancel/timeout/startup recovery 不改变现有控制流，只确保 LACP 的候选数据随快照完整恢复。

## 6. API 与前端

详细增量契约写入本任务 `api.md`，激活前必须由用户确认。端点仍为 `PUT /api/network/mode`：

- `mode=active-backup` 的旧请求保持兼容；
- `mode=lacp-aggregation` 的 `bond` 要求 `slaveIds`、可选 `xmitHashPolicy`、既有 `ipv4`；服务端默认 `layer2+3`，不接收 `primarySlaveId` 或 `lacpRate`；
- `mode=multi-address` 省略 `bond`；
- 1114 由统一错误中间件映射 HTTP 503；未协商不返回错误。

前端扩展 `NETWORK_MODES`、`BondTopology`、LACP DTO、warning 类型和 `BondParams`。模式面板依据 capability 显示 LACP；LACP 表单使用下拉选择 hash policy，默认 `layer2+3`，提交前显示交换机 LAG、单流不跨链路和入向分流由交换机决定的提示。拓扑区按 slave 展示 actor/partner 状态、aggregator ID、是否进组；未进组使用警示样式并显示 `partner_not_configured` 三语文案。pending 倒计时和确认/取消完全复用现有状态。

## 7. 持久化、兼容性与审计

- `CurrentSchemaVersion` 保持 1；新增模式、bond 参数、LACP 状态、warnings 均是可选 JSON 字段。
- 旧的 `factory.json`、`last-valid.json` 和 active-backup 数据反序列化后行为不变；空模式仍归一化为 `multi-address`。
- 不新增权限、路由、菜单 migration；模式切换继续使用既有审计 action，摘要增加目标模式、hash policy、slave 集合和 warning 结果，但不写原始 netlink 数据。
- `CodeNetworkLacpNegotiationFailed=1114` 加入 errno 三语文案与 HTTP 503 映射；warning 和 partner 未协商不写错误日志作为失败。

## 8. 风险与跨 child 接口

- 平台层真实化必须提供 LACP capability probe、bond netlink mode/hash/rate 配置、状态读取及 speed/duplex 字段；若只提供 bond 创建而不提供状态读取，LACP capability 不得打开。
- 不同内核对 bond netlink 属性和状态字段的可用性不同，探针失败时宁可隐藏模式。
- 交换机单侧配置可能造成不聚合甚至现场环路；UI 提示、状态诊断和 120 秒回滚是本系统能提供的边界，不尝试自动修复交换机。
- 多 slave 的原 IPv4 快照必须保持在候选计划中，退出 LACP 时按 `last-valid` 精确恢复；不能只恢复 bond0。
