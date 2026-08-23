# 边缘网关模式 API 契约

状态：`draft`（等待用户确认）  
版本：`v1`，在 `.trellis/tasks/archive/2026-08/08-22-network-configuration/api.md` 与归档的 active-backup 模式契约之上增量扩展

## 1. Scope

后端：

- `app/internal/pkg/netconfig/types.go`：gateway mode、plan、state、overview、lease DTO；
- `app/internal/pkg/netconfig/state.go` 及新增 gateway storage：`gateway-leases.json` 的 root-only 原子 envelope；
- `app/internal/pkg/netconfig/gateway*.go`：runtime、fake backend、Linux backend、DHCP server 封装；
- `app/internal/service/network.go`：模式事务、生命周期、租约 overview 和审计编排；
- `app/internal/api/network.go`：现有 mode endpoint 的 gateway 请求分支；
- `app/internal/pkg/errno`、`app/internal/middleware/error_handler.go`、`middleware/oplog.go`：错误和审计契约；
- `app/internal/router/router.go`：复用既有路由，不新增 gateway 路径。

前端：

- `ui/apps/web-antd/src/api/core/network.ts`：gateway 类型与请求模型；
- `ui/apps/web-antd/src/views/ops/network/index.vue`：gateway 表单、风险警示、pending 交互、租约列表；
- `ui/apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/ops.json`：三语文案。

共享边界：gateway 使用既有网络模式 endpoint、响应 envelope、认证、权限和确认/取消事务；不创建独立 store、数据库表、菜单或外部 DHCP 进程。

## 2. Conventions

- Base path：`/api`；认证：`Authorization: Bearer <access-token>`。
- 成功响应：`{ "code": 0, "data": ..., "message": "ok" }`；错误响应 `data=null`，错误码和文案来自 `errno`。
- 时间字段为 UTC RFC3339，经现有前端全局时区 formatter 展示。
- IPv4 地址使用字符串，子网使用既有 canonical `prefix` 整数，不同时接受 dotted mask 和 prefix 两个来源。
- `leaseDurationSeconds` 为整数秒，默认 `3600`，有效范围 `[60,604800]`。
- 只允许一个 pending network transaction；gateway 模式切换必须在 120 秒内确认。
- `gateway` 为非 gateway 模式时为 `null`；旧客户端可忽略该字段。

## 3. Shared models

### 3.1 NetworkMode

```ts
type NetworkMode = 'multi-address' | 'active-backup' | 'gateway';
```

`Capabilities.supportedModes` 决定前端是否展示/启用模式。fake Linux 在 CI 中包含 `gateway`；macOS 不包含；真实 Linux 在平台层真实化完成并通过能力探测后才包含。

### 3.2 GatewayPlan（请求/候选配置）

```json
{
  "downstreamInterfaceId": "eth1",
  "poolStart": "192.168.2.100",
  "poolEnd": "192.168.2.200",
  "prefix": 24,
  "leaseDurationSeconds": 3600,
  "ipForward": false
}
```

| 字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `downstreamInterfaceId` | `string` | 是 | overview 中静态、可写、非主出口的接口 ID |
| `poolStart` | `string` | 是 | IPv4；与下行接口同子网；不得为网络/广播/接口地址 |
| `poolEnd` | `string` | 是 | IPv4；与 `poolStart` 同子网且不小于 start |
| `prefix` | `number` | 是 | IPv4 LAN prefix；与下行接口 prefix 相同；不接受 /31、/32 |
| `leaseDurationSeconds` | `number` | 是 | 60 至 604800 秒，默认 3600 |
| `ipForward` | `boolean` | 是 | 目标 sysctl 开关，默认 false |

请求中不得携带接口名、socket、文件路径、DHCP 原文、命令片段或平台私有字段。

### 3.3 GatewayOverview

`GET /api/network` 的 `data.gateway`：

```json
{
  "downstreamInterfaceId": "eth1",
  "poolStart": "192.168.2.100",
  "poolEnd": "192.168.2.200",
  "prefix": 24,
  "leaseDurationSeconds": 3600,
  "ipForward": false,
  "running": true,
  "conflictDetected": false,
  "leases": [
    {
      "mac": "02:42:ac:11:00:10",
      "ip": "192.168.2.100",
      "startsAt": "2026-08-23T10:00:00Z",
      "expiresAt": "2026-08-23T11:00:00Z",
      "lastRenewedAt": "2026-08-23T10:00:00Z",
      "hostname": "camera-01"
    }
  ]
}
```

非 gateway 模式返回 `null`。`previousIpForward`、运行时 socket、状态路径、原始 DHCP 报文和内部错误不返回。

### 3.4 TransactionResult

模式切换继续返回既有 `TransactionResult`。进入 gateway 时 `status=pending_confirmation`、`expiresAt` 为确认截止时间；`overview.gateway` 为候选运行状态，`reconnectAddresses` 使用下行接口静态地址（如有）。确认/取消 endpoint 不变。

## 4. Endpoints

### 4.1 切换到 gateway

```http
PUT /api/network/mode
Permission: ops:network:mode
```

请求：

```json
{
  "mode": "gateway",
  "gateway": {
    "downstreamInterfaceId": "eth1",
    "poolStart": "192.168.2.100",
    "poolEnd": "192.168.2.200",
    "prefix": 24,
    "leaseDurationSeconds": 3600,
    "ipForward": false
  }
}
```

service 在平台写入前校验：mode capability、接口静态模式/非主出口、pool 合法性、租约边界和启用前 DHCP probe。接口已是 DHCP client 或 probe 收到已有响应均拒绝。

### 4.2 退出 gateway

```http
PUT /api/network/mode
Permission: ops:network:mode
```

请求：

```json
{ "mode": "multi-address" }
```

`gateway` 必须省略；服务停止 DHCP、释放 socket、清理租约、恢复进入 gateway 前的 `ip_forward`，再应用多址候选。

### 4.3 确认/取消候选

```http
POST /api/network/transactions/:transactionId/confirm
Permission: ops:network:confirm

POST /api/network/transactions/:transactionId/cancel
Permission: ops:network:cancel
```

路径和响应沿用 08-22 契约。取消、超时和恢复出厂都执行 gateway 资源补偿。

### 4.4 查询 overview 与租约

```http
GET /api/network
Permission: ops:network
```

租约查询不新增 endpoint：通过 `data.gateway.leases` 返回，和 `conflictDetected` 同步反映运行期状态。页面刷新或既有 overview 轮询即可获取最新租约。

### 4.5 恢复出厂

```http
POST /api/network/interfaces/:interfaceId/factory-reset
Permission: ops:network:reset
```

路径沿用既有契约。factory plan 不包含 gateway；当前为 gateway 时先停止 server、释放 socket、恢复 `ip_forward`、清理 lease，再进入既有候选确认流程。

## 5. Error contract

| Code | 名称 | HTTP | 触发条件 |
| ---: | --- | ---: | --- |
| 1106 | `CodeNetworkUnsupported` | 503 | 平台/capability 不支持 gateway |
| 1101 | `CodeNetworkTransactionPending` | 409 | 已有待确认事务 |
| 1100 | `CodeNetworkInvalidConfig` | 400 | 通用模式或 IPv4 结构非法 |
| 1115 | `CodeNetworkGatewayPoolInvalid` | 400 | pool、prefix、接口关系或租约时长非法 |
| 1116 | `CodeNetworkDhcpServerConflict` | 409 | 接口为 DHCP client 或 probe 收到已有 DHCP 响应 |
| 1107 | `CodeNetworkApplyFailed` | 503 | 平台/runtime 应用失败且补偿成功 |
| 1108 | `CodeNetworkRecoveryFailed` | 503 | runtime/platform 补偿失败 |
| 1109 | `CodeNetworkStateCorrupt` | 503 | lease/state envelope 损坏或版本未知 |

响应不泄露平台错误、proc 路径、socket、DHCP 原始报文或堆栈。

## 6. Auth and audit

- 模式切换复用 `ops:network:mode`；overview 与租约复用 `ops:network`；确认、取消、恢复出厂复用既有权限。
- HTTP middleware 的 mode route 使用现有网络模式 action 映射；service 内部使用 `system.log.actionNetworkGatewaySwitch` 记录 gateway 提交/确认与 `ip_forward` 目标，回滚复用 `system.log.actionNetworkRollback`。
- 审计摘要只含目标模式、下行接口、pool 范围、租约时长、`ipForward` 和结果；DHCP 分配、续租、释放只写受控 zap，不写 OperationLog。
- 新增 errno、action key 和页面文案必须同时补齐 zh-CN、en-US、zh-TW。

## 7. Changelog

- `v1 draft`：注册 `gateway` mode；扩展既有 `/network/mode` 请求；在 `/network` overview 中返回 gateway 状态与租约；新增 pool/conflict 错误码；保持状态 schema 1、单一 pending 和既有确认/取消路径不变。
