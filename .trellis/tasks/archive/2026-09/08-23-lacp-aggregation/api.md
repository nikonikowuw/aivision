# 链路聚合网络模式 API 增量契约

状态：`confirmed`
版本：`v2`（在 active-backup `api.md` v1 之上增量）
对应任务：`08-23-lacp-aggregation`

## 1. Scope

本文件只定义 LACP 增量。base path、认证、统一响应 `{code,data,message}`、Accept-Language、时间格式、单一 pending 事务、确认/取消端点和既有 active-backup 请求保持不变。

后端边界：

- `app/internal/pkg/netconfig`：LACP 模式、bond 参数、状态 DTO、warning DTO。
- `app/internal/service/network.go`：LACP 模式校验、计划构建、状态与 warning 编排。
- `app/internal/api/network.go`：既有 mode 请求 DTO 增加 LACP 字段。
- `app/internal/pkg/errno`、`app/internal/middleware/error_handler.go`：1114 文案与 503 映射。

前端边界：

- `ui/apps/web-antd/src/api/core/network.ts`：枚举、请求/响应类型。
- `ui/apps/web-antd/src/views/ops/network/index.vue`：LACP 表单和状态拓扑。
- `ui/apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/ops.json`：三语文案。

不新增 HTTP 路由、权限码、菜单 migration 或独立 LACP store。

## 2. Shared models

### 2.1 Network mode

```ts
type NetworkMode = 'multi-address' | 'active-backup' | 'lacp-aggregation';
```

### 2.2 LACP request

仍使用：

```text
PUT /api/network/mode
Permission: ops:network:mode
```

LACP 请求示例：

```json
{
  "mode": "lacp-aggregation",
  "bond": {
    "slaveIds": ["eth0", "eth1"],
    "xmitHashPolicy": "layer2+3",
    "ipv4": {
      "mode": "dhcp",
      "primary": true
    }
  }
}
```

字段规则：

| 字段 | 类型 | 规则 |
| --- | --- | --- |
| `mode` | `NetworkMode` | 必填；必须是平台 capability 声明的模式 |
| `bond` | object | LACP 时必填；multi-address 时省略；active-backup 旧规则不变 |
| `bond.slaveIds` | `string[]` | LACP 至少 2 个；元素不重复，必须是可写物理接口，或是 active-backup 直接重建时当前 bond 的完整 slave 集合 |
| `bond.xmitHashPolicy` | `'layer2' \| 'layer2+3' \| 'layer3+4'` | 可选；省略时服务端默认 `layer2+3`；不接受其他字符串 |
| `bond.ipv4` | 既有 IPv4 输入 | 与 `PUT /network/interfaces/:interfaceId` 规则完全一致 |
| `bond.primarySlaveId` | absent | LACP 不使用；不得依赖 primary 选择流量路径 |
| `bond.lacpRate` | absent | 不允许客户端输入；服务端固定 `slow` |

LACP 仍使用系统生成的 `bond0`，不接受接口名、路径、命令片段或平台私有字典。

### 2.3 LACP topology response

`NetworkOverview.bond` 在 `mode= lacp-aggregation` 时增加：

```json
{
  "bondInterfaceId": "bond0",
  "slaveIds": ["eth0", "eth1"],
  "xmitHashPolicy": "layer2+3",
  "lacp": {
    "aggregatorId": 7,
    "negotiated": false,
    "diagnosticCode": "partner_not_configured",
    "slaves": [
      {
        "interfaceId": "eth0",
        "aggregatorId": null,
        "inAggregator": false,
        "actorState": {
          "active": true,
          "shortTimeout": false,
          "aggregation": true,
          "synchronized": false,
          "collecting": false,
          "distributing": false,
          "defaulted": true,
          "expired": false
        },
        "partnerState": {
          "active": false,
          "shortTimeout": false,
          "aggregation": false,
          "synchronized": false,
          "collecting": false,
          "distributing": false,
          "defaulted": true,
          "expired": false
        }
      }
    ]
  }
}
```

说明：

- `lacp.negotiated=true` 仅当至少一个有效 aggregator 建立且所有目标 slave 进入同一 aggregator；部分进组仍为 false，但逐 slave 状态如实返回。
- 没有任何 slave 进入 aggregator 时，`diagnosticCode=partner_not_configured`；这不是 API error，不触发自动回滚。
- actor/partner 状态是业务布尔字段，不把内核原始 bitmask 暴露给前端。
- active-backup 的 `primarySlaveId`、`activeSlaveId`、`miimon` 字段及旧 JSON 语义保持不变。

### 2.4 Interface link attributes

`InterfaceInfo` 增加可选只读字段：

```ts
speedMbps?: number;
duplex?: 'unknown' | 'half' | 'full';
```

用于提交前检测 slave 速率/双工不一致；未知值不伪造。

### 2.5 Non-blocking warnings

```ts
interface NetworkWarning {
  code: 'bond_slave_link_mismatch';
  interfaceIds: string[];
}
```

`TransactionResult.warnings` 和 `PendingTransaction.warnings` 可选返回。速率/双工不一致时返回 warning，仍允许进入 pending confirmation；前端展示警告但不把它当 error。

## 3. Response and errors

成功响应继续使用既有 `TransactionResult`，LACP 提交时：

- `status = "pending_confirmation"`；
- `expiresAt` 为现有确认窗口截止时间；
- `overview.mode = "lacp-aggregation"`；
- `overview.bond.lacp` 携带当前可读协商状态；
- `warnings` 携带非阻断 warning；
- `reconnectAddresses` 按既有 bond IPv4 规则返回。

错误矩阵：

| Code | 名称 | HTTP | 条件 |
| ---: | --- | ---: | --- |
| 1100 | `CodeNetworkInvalidConfig` | 400 | bond IPv4 非法 |
| 1101 | `CodeNetworkTransactionPending` | 409 | 已有待确认事务 |
| 1106 | `CodeNetworkUnsupported` | 503 | macOS、未通过 capability/probe 或平台未声明 LACP |
| 1107 | `CodeNetworkApplyFailed` | 503 | 一般平台应用失败且补偿完成 |
| 1112 | `CodeNetworkBondSlaveInvalid` | 409 | slave 不存在、重复、不可写、占用、指纹变化或数量不足 |
| 1113 | `CodeNetworkBondModeConflict` | 409 | 目标拓扑冲突，需先回到 multi-address |
| 1114 | `CodeNetworkLacpNegotiationFailed` | 503 | 内核拒绝建立 mode 4 或 LACP 属性，且已执行 before 补偿 |

对端交换机未配置 LAG、部分 slave 未入 aggregator、actor/partner 未同步均不返回 1114；这些情况只通过 `overview.bond.lacp` 表达。

## 4. Auth and audit

- 切换、确认、取消和超时回滚沿用 `ops:network:mode`、`ops:network:confirm`、`ops:network:cancel`。
- 既有 mode switch audit action 继续使用；摘要增加 mode、slave IDs、hash policy 和 warning 结果。
- 不记录原始 netlink 属性、状态目录路径或平台内部错误；LACP 诊断状态可进入 overview，不作为管理员操作日志错误码。

## 5. Compatibility and changelog

- `NetworkMode` 是追加枚举；旧客户端忽略新模式和新增响应字段即可继续使用既有模式。
- 状态 envelope `schemaVersion` 保持 1；新增字段全部可选，旧 factory/last-valid 数据读取后仍归一化到 multi-address。
- active-backup 请求不得因为 LACP 增量改变字段约束或响应行为。

变更记录：

- `v2 draft`：追加 `lacp-aggregation`、LACP bond 参数、协商状态、非阻断 warning 和 errno 1114；不新增端点/权限/migration。
