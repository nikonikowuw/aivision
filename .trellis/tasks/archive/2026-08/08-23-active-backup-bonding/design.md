# 主备容错网络模式技术设计

状态：`confirmed`（用户 2026-08-23 确认）
对应 PRD：`prd.md`
基准代码：`dev` 分支（`e2ab18f`）

## 1. 设计目标与边界

在既有 netconfig 骨架上引入「整机网络工作模式」这一维度，并以 `active-backup` 作为第一个
非默认模式落地。约束三条：

1. **改动面最小**：`Platform` 接口只增一个只读方法；模式与拓扑搭既有 `HostPlan` /
   `HostSnapshot` 的车流动，不新建平行通道。
2. **状态文件零迁移**：不动 `CurrentSchemaVersion`，靠 `omitempty` 可选字段实现新旧兼容。
3. **能力诚实**：真实平台层尚未具备 bond 能力，就在 capability 中如实不声明，而不是提供
   一个跑不通的分支。

不在本设计内：真实 rtnetlink 下发、08-22 遗留的审计缺口、其余 bonding 模式。

## 2. 模型扩展

### 2.1 新增类型（`netconfig/types.go`）

```go
// NetworkMode 整机网络工作模式。空值等价于 NetworkModeMultiAddress。
type NetworkMode string

const (
    NetworkModeMultiAddress NetworkMode = "multi-address"
    NetworkModeActiveBackup NetworkMode = "active-backup"
)

// AllNetworkModes 枚举的单一事实来源，供校验/迭代/默认值复用。
// 新增模式只需改这里 + 常量定义 + 行为分支，避免校验点散落（评审时确认）。
func AllNetworkModes() []NetworkMode {
    return []NetworkMode{NetworkModeMultiAddress, NetworkModeActiveBackup}
}

// Valid 边界校验用，替代各调用点手写的 switch / slices.Contains。
func (m NetworkMode) Valid() bool {
    return slices.Contains(AllNetworkModes(), m)
}

// BondPlan 绑定拓扑的目标配置。
type BondPlan struct {
    SlaveIDs       []string `json:"slaveIds"`       // 恰好 2 个
    PrimarySlaveID string   `json:"primarySlaveId"` // 必须 ∈ SlaveIDs
    Miimon         int      `json:"miimon"`         // 固定 100，由服务端填充
}

// BondTopology 平台回读的实际绑定拓扑。
type BondTopology struct {
    BondInterfaceID string   `json:"bondInterfaceId"`
    SlaveIDs        []string `json:"slaveIds"`
    PrimarySlaveID  string   `json:"primarySlaveId"`
    ActiveSlaveID   *string  `json:"activeSlaveId"` // 当前实际承载流量的 slave
    Miimon          int      `json:"miimon"`
}
```

`BondPlan.Miimon` 由服务端按 D2 固定写入 100，不从 HTTP 输入读取。

### 2.2 既有类型的可选扩展

```go
type HostPlan struct {
    Interfaces         map[string]InterfacePlan `json:"interfaces"`
    PrimaryInterfaceID *string                  `json:"primaryInterfaceId"`
    Mode               NetworkMode              `json:"mode,omitempty"` // 新增
    Bond               *BondPlan                `json:"bond,omitempty"` // 新增
}

type HostSnapshot struct {
    // ... 既有字段不变
    Mode NetworkMode   `json:"mode,omitempty"` // 新增
    Bond *BondTopology `json:"bond,omitempty"` // 新增
}

type InterfaceInfo struct {
    // ... 既有字段不变
    IsBond   bool    `json:"isBond"`            // 新增：该接口是 bond 逻辑口
    MasterID *string `json:"masterId"`          // 新增：该接口是某 bond 的 slave
}

type Capabilities struct {
    DHCP            bool          `json:"dhcp"`
    StaticIPv4      bool          `json:"staticIpv4"`
    FactoryReset    bool          `json:"factoryReset"`
    WifiAssociation bool          `json:"wifiAssociation"`
    SupportedModes  []NetworkMode `json:"supportedModes"` // 新增
}

type NetworkOverview struct {
    // ... 既有字段不变
    Mode NetworkMode   `json:"mode"`  // 新增：始终有值，空则归一化为 multi-address
    Bond *BondTopology `json:"bond"`  // 新增
}
```

`TransactionAction` 增加取值 `mode_switch`；`PendingTransaction` 增加
`TargetMode NetworkMode` 与 `PreviousMode NetworkMode`（均 `omitempty`）。
`TargetInterfaceID` 在模式切换事务中填 bond 接口 ID（切回多址时填空串）。

### 2.3 兼容性论证（对应 D7 / AC5）

`state.go:187-228` 的 `readEnvelope` 先校验 `SchemaVersion`，再对 `Data` 重新规范化并比对
SHA-256，最后 `json.Unmarshal` 进目标结构。本设计：

- 不改 `CurrentSchemaVersion`（仍为 1），旧文件不会命中 `state.go:202` 的损坏分支；
- 新增字段全部 `omitempty`，旧文件的 `Data` 字节不变 → checksum 仍匹配；
- 旧文件反序列化后 `Mode` 为 `""`、`Bond` 为 `nil`，由归一化函数 `normalizeMode()` 统一
  映射为 `multi-address`。

归一化只在读侧做一次，写侧一律写显式值。

## 3. 平台接口与能力声明

### 3.1 接口变更（`netconfig/platform.go`）

```go
type Platform interface {
    Type() PlatformType
    Capabilities(ctx context.Context) Capabilities // 新增，只读
    Probe(ctx context.Context) error
    Discover(ctx context.Context) ([]InterfaceInfo, error)
    Read(ctx context.Context) (HostSnapshot, error)
    Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error)
    Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error)
    Close(ctx context.Context) error
}
```

模式信息不需要新的写方法：`Apply` 已接收完整 `HostPlan`（含 `Mode` / `Bond`），`Read`
返回完整 `HostSnapshot`（含 `Mode` / `Bond`）。后两个 child 沿用同一形状。

### 3.2 各实现的声明

| 实现 | `SupportedModes` | 理由 |
| --- | --- | --- |
| `FakePlatform` | `multi-address`, `active-backup` | 业务层验证与演示环境（`config.yaml` 默认 `fake_platform: true`） |
| `LinuxPlatform` | `multi-address` | 其 `Apply`/`Restore` 仍委托 fake，无真实 bond 能力（D6） |
| `DarwinPlatform` | `multi-address` | parent D2 显式不支持 |
| `DefaultPlatform` | `multi-address` | 其他 OS |

`service/network.go:199-204` 硬编码的 `Capabilities` 字面量删除，改为
`s.platform.Capabilities(ctx)`。其余四个布尔字段的取值由各平台按现状照搬，避免行为回归。

未来 `LinuxPlatform` 真实化时，只需在其 `Probe` 中检测 bond netlink 属性支持并把
`active-backup` 加入返回值，业务层与前端无需改动。

## 4. Fake 平台的 bond 语义

`FakePlatform` 是本轮唯一能演示模式切换的实现，其行为即为模式语义的可执行规范。

### 4.1 进入 `active-backup`

`Apply` 检测到 `plan.Mode == active-backup && plan.Bond != nil` 时：

1. 以固定 ID `bond0` 创建 `InterfaceInfo`：`IsBond=true`、`Type=ethernet`、
   `LinkStatus` 取两块 slave 的逻辑或（任一 up 即 up）、`Writable=true`、
   `Fingerprint="fp-bond0"`、`MAC` 取 primary slave 的 MAC。
2. 两块 slave：`MasterID=&"bond0"`、`Writable=false`、`IsPrimary=false`、
   `IPv4` 清空为 `Mode=unknown` / `Status=unavailable`。
3. bond0 的 IPv4 按 `plan.Interfaces["bond0"]` 应用，复用既有 DHCP/static 分支逻辑
   （`fake.go:194-225`）。
4. 填充 `HostSnapshot.Bond`：`ActiveSlaveID` 设为 primary slave。

### 4.2 退回 `multi-address`

`plan.Mode` 为空或 `multi-address` 时：删除 `bond0` 条目，把带 `MasterID` 的接口
`MasterID` 置 nil、`Writable=true`，其 IPv4 由 `plan.Interfaces` 中对应条目恢复——
退出时这些条目由 service 层从 `last-valid` 还原（见 5.2 步骤 8）。

### 4.3 回滚路径

`Restore(snapshot)` 无需为 bond 增加任何特判：`fake.go:256-259` 已经是「用 snapshot 的
interfaces map 整体替换」，而 `before` 是切换前的完整 `HostSnapshot`。补充两行：同步
`f.mode` 与 `f.bond` 字段。这使 R4.1 / R4.2 / R4.3 三条回滚路径共用同一机制。

### 4.4 Discover 顺序

`fake.go:141-151` 先按固定 keys 输出再补其他，`bond0` 会落在尾部；service 层
`network.go:170-178` 按 `Name` 升序重排，`bond0` 最终排在 `eth0` 之前。测试断言按名称查找，
不依赖下标。

## 5. 服务层

### 5.1 新增入口

```go
type SwitchModeInput struct {
    Mode           netconfig.NetworkMode
    SlaveIDs       []string
    PrimarySlaveID string
    BondIPv4       ApplyInterfaceInput // 复用既有 IPv4 输入结构
    ActorID        uint64
    ActorUsername  string
    ClientIP       string
}

SwitchMode(ctx context.Context, input SwitchModeInput) (*netconfig.TransactionResult, error)
```

### 5.2 执行顺序

沿用 `ApplyInterface`（`network.go:232-432`）的骨架，逐步替换校验内容：

1. `s.mu.Lock()`；`!s.ready` → `CodeNetworkNotReady`。
2. `store.GetPending()` 非空 → `CodeNetworkTransactionPending`（与单接口配置共用槽位，R1.5）。
3. `slices.Contains(platform.Capabilities(ctx).SupportedModes, input.Mode)` 为假 →
   `CodeNetworkUnsupported`。
4. `platform.Read(ctx)` 取 `before` 快照。
5. **模式冲突校验** → `CodeNetworkBondModeConflict`：
   - 目标模式与 `before.Mode` 相同（无变化）；
   - 目标为 `active-backup` 但当前已处于 `active-backup`（需先退回多址）。
6. **slave 校验** → `CodeNetworkBondSlaveInvalid`（仅目标为 `active-backup` 时）：
   数量恰为 2、两者不相同、均存在于 `before.Interfaces`、均 `Writable`、
   均无 `MasterID`、`PrimarySlaveID ∈ SlaveIDs`。
7. **bond IPv4 校验**：复用 `NormalizeAndValidateIPv4` / `ValidateGatewayInSubnet` /
   `ValidateDNSServers`（`validator.go`），规则与 `ApplyInterface` 第 3 步一致 →
   `CodeNetworkInvalidConfig`。
8. **构建 `HostPlan`**：复制 `before` 的全部接口计划为基线，然后
   - 目标 `active-backup`：两个 slave 的条目**保留原 IPv4 配置**（不清空），追加 `bond0` 的
     `InterfacePlan`，`Bond` 填 `BondPlan{SlaveIDs, PrimarySlaveID, Miimon: 100}`，
     `PrimaryInterfaceID` 按 bond 是否为主出口设置。平台层（`applyMode`）会实际清空 slave
     状态并标记 `MasterID`，`Apply` 主循环按 `MasterID != nil` 跳过 slave 的 IPv4 应用，因此
     plan 保留原值不影响进入后的运行状态；其目的是让 slave 原值随 `last-valid` 持久化，供
     退出模式时恢复（R3.4，见下）。
   - 目标 `multi-address`：`Bond` 置 nil，移除 bond 条目，两个 slave 的计划从
     `last-valid` 或 `before` 中的原值恢复。
9. 生成事务（`Action: mode_switch`、`TargetMode`、`PreviousMode`），`ReconnectAddresses`
   填 bond 的候选地址。
10. `store.SetPending` → `platform.Apply` → 失败则 `Restore(before)` + `ClearPending` +
    `CodeNetworkApplyFailed`。
11. `time.AfterFunc(timeout, handleTimeout)`，与既有逻辑共用同一个 `s.timer` 字段。

第 5–7 步全部在触碰平台之前完成，保证校验失败时状态零修改（AC3）。

### 5.3 确认、取消与超时

三条路径全部复用既有实现，**不改其控制流**，只在其中补一处审计：

- `ConfirmTransaction`（`network.go:434-469`）、`CancelTransaction`（`:471-502`）、
  `handleTimeout`（`:576-589`）读到的 `pending.Transaction.Action == mode_switch` 时，
  额外调用 `recordSystemLog` 写模式切换审计；其他 action 的行为逐字不变。
- 这样既满足 R5.3，又不触碰 08-22 遗留的 `ApplyInterface` 审计缺口（Out of Scope）。

`CancelTransaction` 与 `handleTimeout` 已经调用 `platform.Restore(ctx, pending.Before)`，
而 `Before` 含完整拓扑，因此 bond 拆除与 slave 归还自动成立（R4.1/R4.2）。
`Start`（`:122-130`）的启动恢复分支同理（R4.3）。

### 5.4 Overview

`GetOverview` 增加两行：`Mode: normalizeMode(snapshot.Mode)`、`Bond: snapshot.Bond`；
`Capabilities` 改为平台声明。其余逻辑不动。

## 6. API 层

### 6.1 端点

```
PUT /api/network/mode        权限 ops:network:mode
```

请求体（切到主备）：

```json
{
  "mode": "active-backup",
  "bond": {
    "slaveIds": ["eth0", "eth1"],
    "primarySlaveId": "eth0",
    "ipv4": {
      "mode": "static",
      "primary": true,
      "address": "192.168.1.100",
      "prefix": 24,
      "gateway": "192.168.1.1",
      "dnsServers": ["192.168.1.1"]
    }
  }
}
```

请求体（切回多址）：`{"mode": "multi-address"}`。

响应沿用 `TransactionResult`，与既有四个写端点一致。`miimon` 不出现在请求体中（D2）。

### 6.2 路由与权限注册

`router.go:219-233` 的 `networkGroup` 内追加：

```go
networkGroup.PUT("/mode", deps.NetworkHandler.SwitchMode)
```

以及 `deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+"/network/mode", "ops:network:mode")`。

注意：路由 `/network/mode` 与既有 `/network/interfaces/:interfaceId` 不冲突（不同前缀段）。

### 6.3 错误码

`errno.go` 在 1111 之后追加，三语文案同步：

| Code | 名称 | HTTP |
| ---: | --- | ---: |
| 1112 | `CodeNetworkBondSlaveInvalid` | 409 |
| 1113 | `CodeNetworkBondModeConflict` | 409 |

## 7. 数据迁移

新增 `app/migrations/000010_add_network_mode_permission.up.sql`，沿用 000009 的幂等写法：
在 `Network` 菜单下插入 `type='button'`、`permission='ops:network:mode'`、
`name='ops.network.mode'` 的节点，并绑定 super 角色。`down.sql` 按 permission 删除该节点
及其 `role_menus` 关联。

## 8. 前端

### 8.1 类型与 API（`api/core/network.ts`）

新增 `NetworkMode`、`BondTopology`、`SwitchModeParams` 类型；`Capabilities` 增
`supportedModes`；`NetworkOverview` 增 `mode` / `bond`；`InterfaceInfo` 增 `isBond` /
`masterId`。新增 `switchNetworkModeApi(data)`。

模式枚举用 `as const` 对象 + 派生联合类型（评审时确认），避免视图层散落魔法字符串，
同时保留联合类型的穷尽性检查：

```ts
// 与后端 netconfig.NetworkMode 常量逐字一致
const NETWORK_MODES = {
  MultiAddress: 'multi-address',
  ActiveBackup: 'active-backup',
} as const;
type NetworkModeValue = (typeof NETWORK_MODES)[keyof typeof NETWORK_MODES];
```

### 8.2 页面（`views/ops/network/index.vue`）

在现有接口列表卡片之前插入「工作模式」卡片：

- 模式选择：依据 `overview.capabilities.supportedModes` 渲染；不在其中的模式禁用并
  在 tooltip 说明「当前平台不支持」。
- 当前拓扑：`active-backup` 时展示 bond 接口与两块 slave 的从属关系及各自 `linkStatus`，
  并标出 `activeSlaveId`。
- 切换表单：slave 多选（限 2）+ primary 单选 + bond 的 IPv4 表单（复用现有
  `formModel` 的 mode/address/prefix/gateway/dns 字段与 `calculatedSubnetMask` 计算属性）。
- 提交前二次确认弹窗，文案含「网络拓扑将被重构、可能短暂失联、120 秒未确认自动回滚」。
- 倒计时与确认/取消复用既有 `startCountdown` / `handleConfirm` / `handleCancel`
  （`index.vue:113-226`），不新建一套。
- slave 在列表中以从属样式呈现，其编辑按钮按 `writable=false` 自动禁用（既有逻辑已覆盖）。

### 8.3 i18n

`ops.json` 三语新增模式名称、拓扑标签、警示文案、按钮与 action key；
`errno.go` 的 1112/1113 三语文案在后端补齐。

## 9. 测试策略

| 层 | 文件 | 覆盖 |
| --- | --- | --- |
| 平台 | `netconfig/netconfig_test.go` | fake 的 bond 创建/拆除/回滚；`Capabilities` 各实现声明；旧 envelope 读取 |
| 服务 | `service/network_test.go` | `SwitchMode` 的 11 步全部分支；pending 冲突；unsupported；slave 非法组合；超时回滚；启动恢复；审计写入 |
| API | `api/network_test.go` | 端点 DTO 绑定、权限 403、错误码映射 |
| 前端 | `views/ops/network/*.test.ts` | capability 驱动的禁用渲染、拓扑展示、确认流程 |

关键断言：校验失败路径必须断言 `platform.Read()` 的结果与调用前逐字段相等（AC3）。

## 10. 风险与回滚

- 本轮改动集中在新增字段与新增方法，既有五条写路径的控制流不变，回滚即 revert 提交。
- `Platform` 接口新增方法会使任何外部实现失效——仓库内仅四个实现，全部同步修改。
- `Capabilities` 从硬编码改为平台声明是唯一一处既有行为改动点，需在测试中固定四个布尔
  字段的取值不变，防止 `pnpm check` 之外的隐性回归。
