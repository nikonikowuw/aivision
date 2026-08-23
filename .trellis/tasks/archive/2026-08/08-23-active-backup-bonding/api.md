# 网络工作模式 API 契约

状态：`confirmed`（用户 2026-08-23 确认）
版本：`v1`（在 `08-22-network-configuration/api.md` v0 之上增量）

## 1. Scope

本文件只定义**新增与变更**部分。未提及的约定（base path、认证、响应 envelope、
`Accept-Language`、时间格式、标识符规则、单一 pending 事务约束、HTTP 状态映射）
全部沿用 `.trellis/tasks/08-22-network-configuration/api.md` 第 2 节，逐字不变。

后端边界：

- `app/internal/api/network.go`：新增 `SwitchMode` handler 与请求 DTO。
- `app/internal/service/network.go`：新增 `SwitchMode`；`GetOverview` 增字段。
- `app/internal/pkg/netconfig`：模型扩展、`Capabilities` 方法、fake 的 bond 语义。
- `app/internal/router/router.go`、`internal/pkg/errno`、`app/migrations/`：
  路由、权限、错误码与菜单权限节点。

前端边界：

- `ui/apps/web-antd/src/api/core/network.ts`：类型扩展与 `switchNetworkModeApi`。
- `ui/apps/web-antd/src/views/ops/network/index.vue`：工作模式卡片。
- 三语 `ops.json`。不新增静态 route。

## 2. 变更的共享模型

### 2.1 新增枚举

```ts
type NetworkMode = 'multi-address' | 'active-backup';
```

`TransactionAction` 增加取值 `'mode_switch'`（原为 `'apply' | 'factory_reset'`）。

### 2.2 `BondTopology`（只读）

```ts
interface BondTopology {
  bondInterfaceId: string;
  slaveIds: string[];
  primarySlaveId: string;
  activeSlaveId: string | null;  // 当前实际承载流量的 slave
  miimon: number;                // 固定 100，只读
}
```

### 2.3 既有模型的增量字段

| 模型 | 新增字段 | 说明 |
| --- | --- | --- |
| `NetworkOverview` | `mode: NetworkMode` | 始终有值；后端对空值归一化为 `multi-address` |
| `NetworkOverview` | `bond: BondTopology \| null` | 非 bonding 模式为 `null` |
| `Capabilities` | `supportedModes: NetworkMode[]` | 平台声明，前端据此渲染 |
| `InterfaceInfo` | `isBond: boolean` | 该接口是 bond 逻辑口 |
| `InterfaceInfo` | `masterId: string \| null` | 该接口是某 bond 的 slave |
| `PendingTransaction` | `targetMode?: NetworkMode` | 仅 `mode_switch` 事务出现 |
| `PendingTransaction` | `previousMode?: NetworkMode` | 仅 `mode_switch` 事务出现 |

所有新增字段对既有客户端向后兼容：老前端忽略未知字段即可继续工作。

## 3. 新增端点

### 3.1 切换网络工作模式

```
PUT /api/network/mode
权限：ops:network:mode
```

请求体（切到主备容错）：

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

请求体（切回多址）：

```json
{ "mode": "multi-address" }
```

字段约束：

- `mode` 必填，必须是 `NetworkMode` 枚举值且在 `capabilities.supportedModes` 中。
- `bond` 在 `mode === 'active-backup'` 时必填，其他模式必须省略。
- `bond.slaveIds` 长度恰为 2，元素互不相同，均须是 overview 中 `writable === true`
  且 `masterId === null` 的接口 ID（另：`isBond` 必须为 false，即不能选 bond 逻辑口
  作 slave）。
- `bond.primarySlaveId` 必须是 `slaveIds` 的成员。
- `bond.ipv4` 的字段规则与既有 `PUT /network/interfaces/:interfaceId` 完全一致
  （static 必填 address/prefix；primary 时必填 gateway 与至少一个 DNS；
  非 primary 时不得携带 gateway/DNS；dhcp 时不得携带任何地址字段）。
- **`miimon` 不接受客户端输入**，由服务端固定为 100。
- 请求体不得携带接口名、命令片段、路径或平台私有字典。

响应：`TransactionResult`，结构与既有四个写端点一致。成功时
`status = "pending_confirmation"`，`expiresAt` 为 120 秒后，`reconnectAddresses`
携带 bond 接口的候选地址。

后续确认与取消复用既有端点，不新增：

```
POST /api/network/transactions/:transactionId/confirm    权限 ops:network:confirm
POST /api/network/transactions/:transactionId/cancel     权限 ops:network:cancel
```

## 4. Auth、RBAC 与 audit

### 4.1 权限码

新增一个，其余复用：

| 用途 | 权限码 |
| --- | --- |
| 切换工作模式 | `ops:network:mode`（新增） |
| 页面与 GET overview/transaction | `ops:network` |
| 单接口 IPv4 配置 | `ops:network:edit` |
| 确认候选 | `ops:network:confirm` |
| 取消候选 | `ops:network:cancel` |
| 恢复出厂 | `ops:network:reset` |

菜单权限节点由 `migrations/000010_add_network_mode_permission.up.sql` 幂等写入
`Network` 菜单下并绑定 super 角色，写法沿用 000009。

### 4.2 操作日志

模式切换及其确认、取消、超时自动回滚写操作日志。摘要包含目标模式、slave 列表、
primary slave、切换前后拓扑与结果；不记录 native snapshot、状态目录路径或平台原始错误。

新增 action key（三语补齐）：

- `system.log.actionNetworkModeSwitch`：模式切换提交与确认；
- 回滚复用既有 `system.log.actionNetworkRollback`；
- 启动恢复复用既有 `system.log.actionNetworkStartupRecovery`。

> 既有 `ApplyInterface` / `FactoryReset` 的审计缺口属 08-22 遗留，本版本不修改。

## 5. Error contract

新增两个错误码，其余复用 08-22 的 1100–1111：

| Code | 名称 | 典型 HTTP | 触发条件 |
| ---: | --- | ---: | --- |
| 1112 | `CodeNetworkBondSlaveInvalid` | 409 | slave 数量不为 2、重复、不存在、不可写、已属其他 bond，或 primary 不在 slave 集合内 |
| 1113 | `CodeNetworkBondModeConflict` | 409 | 目标模式与当前拓扑冲突（如已处于该模式，或需先退回多址） |

复用既有码的场景：

- 平台不支持目标模式 → `1106 CodeNetworkUnsupported`（503）
- 已有待确认事务 → `1101 CodeNetworkTransactionPending`（409）
- bond 的 IPv4 参数非法 → `1100 CodeNetworkInvalidConfig`（400）
- 平台应用失败且已补偿 → `1107 CodeNetworkApplyFailed`（503）

不向客户端返回平台原始错误；`data` 为 `null`，诊断细节只写受控 zap 与操作日志。

## 6. 兼容性声明

- 状态文件 `schemaVersion` **保持 1**，不迁移。新增字段全部 `omitempty`，
  08-22 写出的 `factory.json` / `last-valid.json` 仍可读取。
- 既有五条路由的路径、请求体、响应体与错误码逐字不变。
- `/network/mode` 与 `/network/interfaces/:interfaceId` 不构成路由冲突。

## 7. 本轮平台支持矩阵

| 平台实现 | `supportedModes` |
| --- | --- |
| `FakePlatform`（`fake_platform: true`，当前 `config.yaml` 默认） | `multi-address`, `active-backup` |
| `LinuxPlatform` | `multi-address` |
| `DarwinPlatform` | `multi-address` |

`LinuxPlatform` 本轮不声明 `active-backup`：其 `Apply`/`Restore` 仍委托 fake，
无真实 bond 能力。等平台层真实化后加入即可，API 契约无需变更。

## 8. Changelog

- `v1 draft`：新增网络工作模式概念、`PUT /network/mode` 端点、`ops:network:mode` 权限、
  errno 1112/1113、`BondTopology` 与 capability 的 `supportedModes`；
  状态文件 schema 版本不变。
