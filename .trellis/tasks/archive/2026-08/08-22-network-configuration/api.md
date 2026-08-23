# 网络配置服务 API 契约

状态：`draft`（待用户确认；确认前不得执行 `task.py start`）
版本：`v0`

## 1. Scope

后端边界：

- `app/internal/api/network.go`：HTTP DTO、路径参数和统一响应转发。
- `app/internal/service/network.go`：校验、完整 HostPlan 合并、候选事务、恢复和审计。
- `app/internal/pkg/netconfig`：Linux/macOS/fake 平台实现和 root-only 状态存储。
- `app/internal/router/router.go`、`internal/pkg/errno`、`internal/middleware/oplog.go`、
  `internal/model/seed.go`、`app/migrations/`：路由、权限、错误、审计和菜单集成。

前端边界：

- `ui/apps/web-antd/src/api/core/network.ts` 及 `core/index.ts` 导出。
- `ui/apps/web-antd/src/views/ops/network/index.vue`：运维管理下的网络配置页面。
- 三语 `routes.json`、`system.json` 及网络 action/error 文案。
- 不新增静态 route；页面由后端动态菜单的 `/ops/network` component 加载。

共享边界：以下 JSON 字段、枚举、状态和错误码是前后端共同契约；平台 native snapshot、
DHCP option、SystemConfiguration 字典和 root-only 路径永不出现在 API。

## 2. Global conventions

- Base path：`/api`。
- 认证：`Authorization: Bearer <JWT>`；所有 endpoint 先经过现有认证 middleware。
- 成功 envelope：`{"code":0,"data":<T>,"message":"ok"}`，沿用现有 `response.Success`。
- 失败 envelope：`{"code":<businessCode>,"data":null,"message":<localized>}`。
- `Accept-Language` 使用现有 `zh-CN`、`en-US`、`zh-TW` 解析规则；网络错误和 action 文案三语
  必须同时加入 `errno`/前端 locale。
- 时间字段使用 RFC3339 UTC，例如 `2026-08-22T16:00:00Z`。`remainingSeconds` 由服务端
  deadline 计算，前端不得把本地时钟当作事务是否过期的事实来源。
- `interfaceId` 和 `transactionId` 是不透明 ASCII 标识，最大 128 字节；客户端只能使用
  GET 返回的值，不得自行拼接 ifindex、BSD name、路径或 service name。非法格式返回
  `CodeInvalidParam`。
- IPv4 地址和 DNS 使用 dotted decimal；静态 prefix 为整数 `0..32`，响应同时提供规范化
  `subnetMask`。MVP 只接受 IPv4，不接受 IPv6、CIDR 字符串、shell 片段或额外路由。
- 写请求不得通过 query/body 携带命令、参数数组、脚本、文件名、resolver 路径或平台私有
  字典。请求体中的未知字段按项目 JSON binding 策略拒绝或忽略时，必须在实现中统一选择并
  用测试固定；推荐拒绝未知字段。
- 整机一次最多一个待确认事务。任何新的 apply 或 factory reset 在 pending 存在时返回
  `CodeNetworkTransactionPending`，confirm/cancel 只能操作当前 transaction ID。
- HTTP 成功状态为 `200`。业务错误的目标 HTTP 映射：参数 `400`、未认证 `401`、无权限
  `403`、资源不存在 `404`、事务/外部状态冲突 `409`、平台或恢复不可用 `503`；具体仍通过
  `code` 供前端判断。
- 只读 overview/transaction 查询不写操作日志；apply、confirm、cancel、factory reset
  和后台自动 rollback/recovery 均必须可查询审计。网络四类 HTTP 写路径不能依赖现有
  Oplog 的异步 best-effort 语义；实现应切换为同步 Record 或等价可恢复 outbox，正常响应
  只在最终状态、脱敏摘要和 actor 已提交后返回。

## 3. Shared models

### 3.1 NetworkOverview

```json
{
  "platform": "linux",
  "state": "ready",
  "primaryInterfaceId": "linux:ethernet-a1b2c3",
  "defaultRouteInterfaceId": "linux:ethernet-a1b2c3",
  "systemDnsServers": ["192.168.10.1", "1.1.1.1"],
  "interfaces": [],
  "pendingTransaction": null,
  "capabilities": {
    "dhcp": true,
    "staticIpv4": true,
    "factoryReset": true,
    "wifiAssociation": false
  }
}
```

字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `platform` | `linux \| darwin` | 当前运行平台，不支持时仍返回能力状态 |
| `state` | 枚举 | `ready \| degraded \| ownership_conflict \| recovery_failed \| unsupported` |
| `primaryInterfaceId` | `string \| null` | 逻辑主出口；无出口时为空 |
| `defaultRouteInterfaceId` | `string \| null` | 平台实际非作用域默认路由接口；用于检测 primary 漂移 |
| `systemDnsServers` | `string[]` | 平台实际非作用域系统 DNS；未知时为空并由状态字段说明 |
| `interfaces` | `NetworkInterface[]` | 所有已发现接口，不仅是可写接口 |
| `pendingTransaction` | `PendingTransaction \| null` | 当前整机唯一候选事务 |
| `capabilities` | object | 平台能力；`wifiAssociation` 首版恒为 `false` |

### 3.2 NetworkInterface

```json
{
  "id": "linux:ethernet-a1b2c3",
  "name": "eth0",
  "displayName": "Factory Ethernet",
  "type": "ethernet",
  "mac": "02:42:ac:11:00:02",
  "linkStatus": "up",
  "ownership": "managed",
  "writable": true,
  "isPrimary": true,
  "ipv4": {
    "mode": "static",
    "address": "192.168.10.20",
    "prefix": 24,
    "subnetMask": "255.255.255.0",
    "gateway": "192.168.10.1",
    "dnsServers": ["192.168.10.1"],
    "status": "effective"
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `string` | 稳定 opaque ID；Linux 来自 Profile/硬件指纹，macOS 来自 service ID 封装 |
| `name` | `string` | 展示/诊断名称，如 Linux ifname 或 macOS BSD name，不能作为写入 ID |
| `displayName` | `string` | Profile 或 SystemConfiguration 展示名称，可能为空 |
| `type` | `ethernet \| wifi \| other` | macOS 仅 Ethernet/Wi-Fi Service 可写；Linux 物理有线才可写 |
| `mac` | `string \| null` | 脱敏以外的硬件标识可展示，但不得作为客户端自造 ID |
| `linkStatus` | `up \| down \| unknown` | 当前链路状态 |
| `ownership` | `managed \| unproven \| conflict \| unsupported` | 平台所有权状态 |
| `writable` | `boolean` | 后端是否允许此 ID 写入；false 时任何写请求拒绝 |
| `isPrimary` | `boolean` | 当前逻辑主出口 |
| `ipv4` | object | 实际有效 IPv4 状态；字段未知时用 `null`/`unknown`，不伪造 0 |
| `ipv4.mode` | `dhcp \| static \| unknown \| unsupported` | 当前实际获取模式 |
| `ipv4.address` | `string \| null` | 当前实际地址 |
| `ipv4.prefix` | `integer \| null` | 当前 prefix |
| `ipv4.subnetMask` | `string \| null` | 规范化掩码 |
| `ipv4.gateway` | `string \| null` | 只有整机非作用域默认出口返回值；非主接口为 null |
| `ipv4.dnsServers` | `string[]` | 只有主出口的系统非作用域 DNS 返回值；未知时为空且 status 不为 effective |
| `ipv4.status` | `effective \| unavailable \| conflict \| unsupported` | 字段是否代表实际生效值 |

### 3.3 PendingTransaction

```json
{
  "id": "txn-01J8NETWORKABC",
  "status": "pending_confirmation",
  "action": "apply",
  "createdAt": "2026-08-22T16:00:00Z",
  "expiresAt": "2026-08-22T16:02:00Z",
  "remainingSeconds": 97,
  "targetInterfaceId": "linux:ethernet-b2c3d4",
  "previousPrimaryInterfaceId": "linux:ethernet-a1b2c3",
  "candidatePrimaryInterfaceId": "linux:ethernet-b2c3d4",
  "reconnectAddresses": [
    {"interfaceId": "linux:ethernet-b2c3d4", "address": "192.168.20.20", "prefix": 24}
  ],
  "requiresReconnect": true,
  "candidate": {
    "mode": "static",
    "address": "192.168.20.20",
    "prefix": 24,
    "subnetMask": "255.255.255.0",
    "gateway": "192.168.20.1",
    "dnsServers": ["192.168.20.1"]
  }
}
```

`status` 对客户端可见值为 `applying | pending_confirmation | committing | rolling_back`；
服务端恢复完成后返回 TransactionResult，不保留可继续确认的 pending。

候选摘要只包含目标接口配置和主出口迁移信息，不含 native snapshot、文件路径或内部错误。
`reconnectAddresses` 是后端回读的候选地址列表，不能保证管理客户端当前连接仍可达。

### 3.4 TransactionResult

```json
{
  "transactionId": "txn-01J8NETWORKABC",
  "status": "confirmed",
  "expiresAt": "2026-08-22T16:02:00Z",
  "overview": {},
  "reconnectAddresses": [],
  "reason": null
}
```

`status` 为 `pending_confirmation | confirmed | rolled_back | recovery_failed`。`reason` 只
返回稳定的 i18n/error reason key，不返回 shell、路径、堆栈、DHCP 原始报文或 CoreFoundation
错误文本。

## 4. Endpoints

### 4.1 查询网络状态

`GET /api/network`

权限：`ops:network`。只读，不记操作日志。

响应：`NetworkOverview`。

后端必须以平台实际回读生成 `defaultRouteInterfaceId`、`systemDnsServers` 和 interface
IPv4 值；不能直接把 `last-valid` 文件当作当前状态。

### 4.2 查询当前候选事务

`GET /api/network/transactions/:transactionId`

权限：`ops:network`。只读，不记操作日志。

响应：当前 `PendingTransaction`。transaction ID 不存在、已完成或已回滚返回
`CodeNetworkTransactionNotFound`；页面重新连接后用 overview 返回的 ID 查询或直接使用
overview 中的 pending。

### 4.3 应用接口候选配置

`PUT /api/network/interfaces/:interfaceId`

权限：`ops:network:edit`。由 Oplog middleware 记录 `system.log.actionNetworkApply`。

请求示例：

```json
{"mode":"dhcp","primary":true}
```

```json
{
  "mode": "static",
  "primary": false,
  "address": "192.168.30.20",
  "prefix": 24
}
```

```json
{
  "mode": "static",
  "primary": true,
  "address": "192.168.30.20",
  "prefix": 24,
  "gateway": "192.168.30.1",
  "dnsServers": ["192.168.30.1", "1.1.1.1"]
}
```

字段规则：

| 字段 | 类型 | 规则 |
| --- | --- | --- |
| `mode` | `dhcp \| static` | 必填 |
| `primary` | `boolean` | 必填；可把当前主出口降级为空主出口，或提升目标接口 |
| `address` | `string` | static 必填，dhcp 必须省略 |
| `prefix` | `integer` | static 必填，`0..32`；dhcp 必须省略 |
| `gateway` | `string` | 仅 static + primary 必填；非主接口必须省略 |
| `dnsServers` | `string[]` | 仅 static + primary 必填且 1..3 个；非主接口必须省略；dhcp 必须省略 |

服务把请求合并为完整 HostPlan。如果 `primary=true`，旧主出口自动降级并保留其地址、
prefix、直连路由；旧主出口的 gateway/DNS 被清除。任何组合校验或 ownership/外部漂移
失败都在平台 Apply 前返回错误。

成功：`TransactionResult`，状态为 `pending_confirmation`，并返回服务端回读的候选状态、
120 秒截止时间和可能的新地址。

### 4.4 确认候选事务

`POST /api/network/transactions/:transactionId/confirm`

权限：`ops:network:confirm`。由 Oplog middleware 记录 `system.log.actionNetworkConfirm`。

请求：无 body。

服务校验 transaction ID、deadline、当前平台实际候选状态和调用权限；确认只固化已经实际
应用的 candidate，不重新执行平台写入。返回 `TransactionResult(status=confirmed)`。

过期、ID 不匹配、候选被外部修改或当前不存在 pending 时返回相应网络错误，不能把重复确认
当作新的写入。

### 4.5 取消候选事务

`POST /api/network/transactions/:transactionId/cancel`

权限：`ops:network:cancel`。由 Oplog middleware 记录 `system.log.actionNetworkCancel`。

请求：无 body。

服务执行 before snapshot 的补偿性恢复，回读旧状态后返回
`TransactionResult(status=rolled_back)`。取消失败返回 recovery/apply 错误并把系统置为
recovery-failed 或 conflict；不能删除 pending 逃避恢复。

### 4.6 恢复接口出厂基线

`POST /api/network/interfaces/:interfaceId/factory-reset`

权限：`ops:network:reset`。由 Oplog middleware 记录 `system.log.actionNetworkReset`。

请求：无 body。

服务加载该接口所属的不可变 factory baseline，并将其与整机主出口/共享默认路由/DNS 一起
构成候选 HostPlan；若 baseline 使主出口改变，旧主出口降级也在同一事务中完成。返回
`TransactionResult(status=pending_confirmation)`，仍需在 120 秒内 confirm。

确认成功后清理该接口所属完整 HostPlan 的 last-valid，使不可变 factory baseline 成为
  该状态的唯一恢复来源；不能保留与 factory 主出口相矛盾的 last-valid。若清理过程中进程
  重启，pending stage 负责先完成恢复/提交收尾，再执行清理；后续普通确认重新生成
  last-valid。

## 5. Auth、RBAC 和 audit

### 5.1 权限码

| 用途 | 权限码 |
| --- | --- |
| 页面和 GET overview/transaction | `ops:network` |
| 应用 DHCP/static/主出口 | `ops:network:edit` |
| 确认候选 | `ops:network:confirm` |
| 取消候选 | `ops:network:cancel` |
| 恢复出厂 | `ops:network:reset` |

- 菜单 migration 与 `model/seed.go` 必须同步创建运维管理下的 `Network` 页面及五个权限节点，
  对应 `routes.ops.network`、`/ops/network`、`OpsNetwork` 和 `ops:network*`。操作日志 action
  仍使用 `system.log.actionNetwork*`，不与 RBAC 权限 namespace 混用。super role 自动拥有
  全部节点，普通角色由现有角色菜单绑定控制。GET 路由也显式注册页面权限，避免“能看到菜单
  但 API 只要求登录”的权限漂移。

### 5.2 操作日志

HTTP body 只记录 mask 后的摘要，至少包含目标接口 ID、mode、primary、变更前后地址/prefix、
是否发生主出口切换和结果；不记录 native snapshot、配置文件路径或 DHCP 原始数据。

自动事件不经过 Gin，NetworkService 直接构造 `model.OperationLog` 调用
`OperationLogService.Record`：

- `system.log.actionNetworkRollback`：取消、超时、应用失败补偿；
- `system.log.actionNetworkStartupRecovery`：启动回滚、last-valid/factory 恢复；
- `system.log.actionNetworkLease`：DHCP 租约失效/恢复对实际配置产生修改时。

自动事件使用 pending 中保存的原操作者和来源 IP；没有关联操作者时使用固定 system actor，
并在 summary 说明触发原因。所有 action key 和菜单/页面文案均加入三语 locale。

## 6. Error contract

以下是新增 errno 候选值，实施时必须在 `internal/pkg/errno` 固化并补齐三语文案；数值应避开
现有 1001-1017 和 1500：

| Code | 名称 | 典型 HTTP | 触发条件 |
| ---: | --- | ---: | --- |
| 1100 | `CodeNetworkInvalidConfig` | 400 | IPv4/prefix/gateway/DNS/primary 组合非法 |
| 1101 | `CodeNetworkTransactionPending` | 409 | 已存在整机候选事务 |
| 1102 | `CodeNetworkTransactionNotFound` | 404 | transaction ID 不存在/已完成 |
| 1103 | `CodeNetworkTransactionExpired` | 409 | deadline 已过 |
| 1104 | `CodeNetworkInterfaceNotManaged` | 409 | ID 不在当前可写集合或指纹变化 |
| 1105 | `CodeNetworkOwnershipConflict` | 409 | Linux 外部管理器/漂移/Resolver 非本系统所有 |
| 1106 | `CodeNetworkUnsupported` | 503 | 平台或能力不支持 |
| 1107 | `CodeNetworkApplyFailed` | 503 | 平台应用失败且补偿完成或进入故障 |
| 1108 | `CodeNetworkRecoveryFailed` | 503 | before/last-valid/factory 恢复失败 |
| 1109 | `CodeNetworkStateCorrupt` | 503 | root-only envelope 损坏/版本未知/校验和不符 |
| 1110 | `CodeNetworkExternalDrift` | 409 | 当前状态被外部修改，拒绝覆盖 |
| 1111 | `CodeNetworkNotReady` | 503 | 启动恢复/能力检查未完成 |

不向客户端返回平台原始错误。业务错误仍由现有统一 middleware 生成 localized `message`；
`data` 为 `null`，诊断细节只写受控 zap/operation log。

## 7. Changelog

- `v0 draft`：首次定义 Linux/macOS 网络状态、DHCP/static IPv4、单一主出口、整机候选事务、
  确认/取消/恢复出厂、RBAC、审计和统一错误契约。
- 导航决策已确认：页面归入 `OpsNetwork`，路径 `/ops/network`，权限域为 `ops:network*`；
  审计 action 继续使用 `system.log.*` namespace。
- 待确认：用户确认完整 endpoint、字段和错误码后，将状态改为 `confirmed` 并记录确认日期。
