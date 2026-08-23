# 链路聚合网络模式 (LACP 802.3ad)

状态：`planning`
Parent：`.trellis/tasks/08-23-advanced-network-modes`

## Goal

在 `08-23-active-backup-bonding` 建立的网络工作模式框架上注册第三种模式：Linux bonding
mode 4（802.3ad LACP）。管理员可将多块物理网卡聚合为单一高吞吐逻辑接口，突破单千兆瓶颈，
为多路 RTSP 视频流提供聚合带宽通道。

## 前置依赖

- **必须**在 `08-22-network-configuration` 与 `08-23-active-backup-bonding` 均实现完成后
  才能进入实现：模式框架、capability 协商、bond rtnetlink 封装与前端模式面板均在前者建立。
- 与 `08-23-edge-gateway-dhcp` 之间无依赖，可并行。

## Background / Confirmed Facts

- mode 4 与 mode 1 共用同一套 bond rtnetlink 创建路径，差异在 `IFLA_BOND_MODE=4` 及
  `IFLA_BOND_XMIT_HASH_POLICY`、`IFLA_BOND_AD_LACP_RATE` 等 802.3ad 专属属性。
- **802.3ad 不会让单条连接跨物理链路。** 流量按 hash 分配到某一条 slave，同一条 TCP
  连接始终走同一条链路。聚合收益只在多条并发连接、且这些连接的 hash 键分散时才成立。
  这是协议特性，不是缺陷。
- **入向（下行）带宽的分流由上联交换机决定，本系统不可控。** 边缘 AI 设备拉 RTSP 流时
  流量以入向为主，因此实际聚合效果取决于交换机的 LAG hash 策略与各摄像头源地址的分布；
  本设备的 `xmit_hash_policy` 只影响出向流量（主要是 ACK 等小包）。需求与验收必须按此
  事实描述，不能承诺"N 块网卡等于 N 倍拉流带宽"。
- 802.3ad 要求同一 aggregator 内的 slave 速率与双工模式一致，否则内核不会将其纳入聚合组。
- LACP 需要上联交换机同步配置 802.3ad 动态 LAG。单侧配置的典型后果是链路不聚合甚至环路，
  这是现场最常见的故障，且**无法自动纠正**，只能可观测化。
- macOS 按 parent D2 显式不支持。

## Key Decisions

- **D1 `xmit_hash_policy` 可配，默认 `layer2+3`**：这是 LACP 部署中必须与交换机侧对齐的
  参数，不同交换机支持度不同，固定死会让聚合形同虚设。取值限定为封闭枚举
  `layer2 | layer2+3 | layer3+4`，不接受任意字符串。
- **D2 暴露 LACP 协商状态**：`NetworkOverview` 返回各 slave 的 LACP actor/partner 状态与
  aggregator 归属。理由：这是判断"交换机是否已配 LAG"的唯一手段，缺了它现场无法排障。
- **D3 slave 数量 2 到当前可写物理网卡数**：不设人为上限，由硬件决定。
- **D4 `lacp_rate` 固定为 `slow`（30s）**：即 802.3ad 默认值，与绝大多数交换机默认配置
  匹配。暴露它只增加两侧不匹配的概率。
- **D5 不做交换机侧自动检测的成功判定**：协商失败只上报状态并允许管理员据此取消，不因
  "未协商成功"就自动回滚——链路可用性由 120 秒防失联机制本身兜底。

## Requirements

### R1 模式注册

- R1.1 在 `NetworkMode` 枚举中新增 `lacp-aggregation`，Linux capability 中声明，macOS 不声明。
- R1.2 `Probe` 阶段验证内核对 802.3ad 相关 bond netlink 属性的支持；不满足则从 capability
  中摘除该模式，fail closed。
- R1.3 模式切换复用既有 API、候选事务、120 秒确认窗口与超时自动回滚，不新增切换通道。

### R2 聚合配置

- R2.1 管理员选取 2 块及以上可写物理网卡作为 slave。
- R2.2 管理员可选择 `xmit_hash_policy`，取值 `layer2 | layer2+3 | layer3+4`，默认 `layer2+3`。
- R2.3 `mode` 设为 4，`lacp_rate` 设为 `slow`。
- R2.4 bond 接口的 IPv4 配置沿用 08-22 的 `dhcp | static` 模型，可作为整机主出口。
- R2.5 提交前校验 slave 存在、可写、指纹未变、互不重复、未被其他虚拟接口占用；若各 slave
  的速率或双工不一致，在响应中给出警告（不阻断，由管理员判断）。
- R2.6 slave 在聚合期间退出可写接口集合，其原 IPv4 配置写入事务 `before` 快照，
  退出模式时精确恢复（同 active-backup 的 D3/R3.4）。

### R3 协商状态可观测

- R3.1 `NetworkOverview` 对处于 LACP 模式的 bond 返回：aggregator ID、各 slave 的
  actor/partner LACP 状态与是否已进入聚合组。
- R3.2 当没有任何 slave 成功进入聚合组时，overview 中给出明确的诊断提示，指向"上联交换机
  未配置 802.3ad LAG"这一最可能原因。
- R3.3 新增 errno `1114 CodeNetworkLacpNegotiationFailed` / 503：仅用于平台确实无法建立
  聚合（如内核拒绝）的情形；单纯"对端未协商"不返回错误，只体现在 overview 状态中。

### R4 前端

- R4.1 模式面板中的 LACP 选项在提交前必须展示前置提示：上联交换机需同步配置 802.3ad
  动态链路聚合组，且需指出单条连接不跨链路、入向分流由交换机决定。
- R4.2 `xmit_hash_policy` 以下拉选择呈现，标注默认值与"需与交换机侧策略对齐"。
- R4.3 拓扑区展示各 slave 的 LACP 协商状态；未进入聚合组的 slave 以警示样式区分。
- R4.4 复用既有 pending 倒计时与确认/取消交互；文案三语对齐。

### R5 平台边界与可测试性

- R5.1 全部 bond 参数下发走 rtnetlink 结构化调用，`xmit_hash_policy` 作为封闭枚举映射为
  内核数值，不透传字符串。
- R5.2 LACP 状态读取通过 netlink 查询，不解析 `/proc/net/bonding/bond0` 文本。
- R5.3 fake platform 支持模拟"已协商 / 未协商 / 部分 slave 进组"三种状态，使 R3 的业务
  逻辑与前端展示可在 CI 中验证。

## Acceptance Criteria

`[CI]` fake platform 自动验证；`[目标机]` 需 Linux host-root 环境；`[台架]` 需真实多网卡 + 支持 802.3ad LAG 的交换机。

- [ ] AC1 `[CI]` Linux capability 含 `lacp-aggregation`，macOS 不含且请求时返回
      `CodeNetworkUnsupported`。
- [ ] AC2 `[CI]` 非法 slave 组合与非法 `xmit_hash_policy` 取值被拒绝，且证明宿主机配置
      未被部分修改；速率/双工不一致时返回警告而非拒绝。
- [ ] AC3 `[CI]` 模拟"无 slave 进入聚合组"时，overview 返回 R3.2 的诊断提示，前端以
      警示样式呈现。
- [ ] AC4 `[目标机]` 成功创建 bond（mode=4, lacp_rate=slow, xmit_hash_policy=选定值），
      slave 正确归属；未确认时 120 秒后自动回滚，bond 拆除、slave 归还并恢复原配置。
- [ ] AC5 `[台架]` 交换机侧配置 LAG 后，overview 显示全部 slave 进入同一 aggregator，
      actor/partner 状态为已协商。
- [ ] AC6 `[台架]` 交换机侧**未**配置 LAG 时，设备不失联，overview 如实反映未协商状态，
      且能通过取消或超时回滚恢复原拓扑。
- [ ] AC7 `[台架]` 多台摄像头（不同源 IP）并发拉流时，聚合总吞吐超过单条链路上限；
      **同时确认**单条 RTSP 连接的吞吐不超过单链路上限属于预期行为，不判为失败。
- [ ] AC8 `[CI]` `app/` 的 `make vet`、`make test` 与 `ui/` 的 `pnpm check`、相关单元测试通过。

## Out of Scope

- 交换机侧配置的自动下发或探测（无带外通道，不在本系统职责内）。
- 其余 bonding 模式；balance-rr / balance-alb 等无需交换机配合的伪聚合模式本轮不做。
- `lacp_rate=fast`、`ad_select`、`min_links` 等进阶 802.3ad 参数。
- 聚合链路的实时吞吐统计与带宽图表。
- 上层业务（RTSP 拉流调度）对聚合拓扑的感知与亲和性编排。
- macOS 的任何等价实现。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| 交换机侧未配 LAG | 链路不聚合，严重时产生环路或大面积丢包 | UI 前置提示 + R3 协商状态可观测 + 120 秒回滚兜底；作为 AC6 独立验收 |
| 用户预期"N 网卡 = N 倍带宽" | 验收争议，被判为功能未达标 | 需求与 AC7 显式写明单流不跨链路、入向由交换机分流；UI 同步提示 |
| `xmit_hash_policy` 与交换机策略不匹配 | 聚合建立但流量集中在单链路 | 暴露为可配项并标注需与交换机对齐；协商状态可见 |
| slave 速率/双工不一致 | 内核静默排除部分 slave | 提交时警告 + overview 显示未进组的 slave |
| 与 active-backup 共用 bond 代码路径 | 一处改动波及两种模式 | 参数差异集中在封闭枚举映射层；两种模式各自保留 CI 用例 |
| 台架依赖（多网卡 + 可配 LAG 的交换机） | 硬件不到位则 AC5-AC7 无法验收 | 台架项与 CI 项分档标注，硬件缺位不阻塞 CI 门禁，但阻塞归档 |

## Artifacts

- `prd.md`：本文件，需求、边界与验收标准。
- `design.md`：LACP 领域模型、协商状态数据流、平台接线与回滚设计，当前为 `draft`。
- `implement.md`：按模型、fake、服务、API、前端、平台联调和质量门禁拆分的执行计划，当前为 `draft`。
- `api.md`：在已归档 active-backup v1 之上的 LACP v2 增量契约，当前为 `draft`；用户确认后作为本 child 的跨前后端契约，不修改已归档文件。
