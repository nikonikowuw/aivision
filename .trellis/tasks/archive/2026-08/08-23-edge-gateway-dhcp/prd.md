# 边缘网关模式与内置 DHCP Server

状态：`planning`
Parent：`.trellis/tasks/08-23-advanced-network-modes`

## Goal

在网络工作模式框架上注册模式 `gateway`：指定一个下行 LAN 口，由本设备为直连的 RTSP 摄像头
自动分配私网 IP，使摄像头免预设 IP 即插即用；并可选开启内核三层转发，让下行专网与上行网段
互通。DHCP 服务由内置 Go 实现提供，不引入 dnsmasq 外部进程，不写防火墙/NAT 规则。

## 前置依赖

- **必须**在 `08-22-network-configuration` 与 `08-23-active-backup-bonding` 均实现完成后
  才能进入实现：模式框架、capability 协商与前端模式面板在前者建立。
- 与 `08-23-lacp-aggregation` 之间无依赖，可并行。

## Background / Confirmed Facts

- 08-22 的 research 已选定 `github.com/insomniacslk/dhcp` 作为 DHCPv4 客户端候选库；当前 `dev` 分支尚未将该依赖写入 `go.mod`，因此在本 child 激活前必须重新验证并锁定同时支持客户端与 `server4` 的兼容版本。该 child 复用同一依赖实现服务端，不新增外部进程。
- `net.ipv4.ip_forward` 可由 Go 直接写 `/proc/sys/net/ipv4/ip_forward` 完成，无需 shell，
  符合 08-22 R5.8。
- **核心场景不依赖 NAT，也不依赖 ip_forward。** 设备主动拉取摄像头 RTSP 流时，设备与摄像头
  在同一下行直连网段，走本机路由即可。`ip_forward` 只在需要让**其他网段**的主机访问摄像头、
  或摄像头访问下行网段之外的目标时才必要。因此转发是可选增强项，默认关闭。
- 不做 NAT 的已知后果：下行摄像头无法主动访问上行网段之外的目标；上行网段主机要访问摄像头，
  需要其自身或其网关存在指向本设备的回程路由。这是 parent D3 的明确取舍，须在 UI 中说明。
- 现网误投放 DHCP 服务会影响本设备之外的整个广播域，是本 child 最大的外部风险。
- macOS 按 parent D2 显式不支持。

## Key Decisions

- **D1 DHCP Server 只绑定指定下行接口**：socket 绑定到具体接口，不监听 `0.0.0.0`。
  这是防止污染其他网段的第一道防线。
- **D2 启用前强制探测该链路是否已有 DHCP 服务**：探测到已有应答则拒绝启用，返回
  `CodeNetworkDhcpServerConflict`。第二道防线。
- **D3 下行接口必须是静态 IPv4**：同一接口不能既作 DHCP client 又作 DHCP server。
  切换到网关模式时若该接口为 DHCP 模式，请求被拒绝。
- **D4 `ip_forward` 为显式开关，默认关闭**：开启内核转发扩大攻击面，且核心拉流场景并不需要
  （见 Confirmed Facts）。由管理员按需开启，UI 说明其作用与不做 NAT 的后果。
- **D5 租约持久化并可查询**：租约表落盘，服务重启后保持；管理员可在页面查看"哪台摄像头
  拿到了哪个 IP"。理由：这是网关模式唯一的运维产出物，缺了它管理员无法定位摄像头。
- **D6 不做静态 MAC 绑定保留地址**：首版只提供动态池。若现场需要固定摄像头 IP，
  另议。

## Requirements

### R1 模式注册

- R1.1 在 `NetworkMode` 枚举中新增 `gateway`；fake Linux 平台声明支持以完成 CI 验证，真实 Linux 仅在
  `08-23-linux-platform-realization` 提供可用接口与权限探测后声明支持，macOS 永不声明。
- R1.2 模式切换复用既有 API、候选事务、120 秒确认窗口与超时自动回滚。
- R1.3 退出网关模式时必须停止 DHCP 服务、释放 socket，并按 D4 的开关状态恢复
  `ip_forward` 原值。

### R2 DHCP Server 配置

- R2.1 管理员指定一个下行 LAN 接口。该接口必须存在、可写、为静态 IPv4 模式（D3），
  且不是当前整机主出口。
- R2.2 管理员配置地址池起止地址、子网掩码与租约时长。租约时长以 `leaseDurationSeconds` 表示，默认
  `3600` 秒，允许范围为 `60` 秒至 `604800` 秒（含边界）。
- R2.3 提交前校验：池起止地址合法且起 ≤ 止、与接口地址处于同一子网、不包含接口自身地址、
  不包含网络地址与广播地址、租约时长在上述范围内。任一不满足返回
  `CodeNetworkGatewayPoolInvalid`。
- R2.4 下发给客户端的选项限定为：IP 地址、子网掩码、租约时长、路由器（本接口地址）。
  首版不下发 DNS、域名、NTP 或厂商私有选项。
- R2.5 DHCP 服务 socket 绑定到指定接口（D1）。

### R3 冲突防护

- R3.1 启用前在目标链路探测既有 DHCP 服务；探测到应答则拒绝启用并返回
  `CodeNetworkDhcpServerConflict`，错误信息指明该链路已存在 DHCP 服务。
- R3.2 运行期间若探测到同链路出现其他 DHCP 服务，记录告警日志并在 overview 中体现，
  但不自动停止服务（避免因瞬时干扰中断摄像头租约）。
- R3.3 前端在启用前展示明确警示：该操作将在所选接口的广播域内提供 DHCP 服务，
  务必确认该口未接入生产网络。

### R4 租约管理

- R4.1 租约表持久化到 root-only 状态目录，采用与 08-22 一致的原子写入与版本化 envelope。
- R4.2 服务重启后恢复既有租约，不重新分配已在租期内的地址。
- R4.3 提供租约查询：返回 MAC、分配的 IP、租约起止时间、最近一次续租时间与客户端主机名
  （若客户端提供）。
- R4.4 退出网关模式时清理租约表；重新进入时从空池开始。
- R4.5 恢复出厂也属于网关模式退出路径：必须先停止 DHCP 服务并释放 socket，恢复进入网关模式前的
  `ip_forward` 值，清理租约表，再按既有候选事务协议恢复出厂网络基线；任一步失败都执行补偿恢复。

### R5 三层转发

- R5.1 提供 `ip_forward` 显式开关，默认关闭。
- R5.2 管理员显式关闭开关时写入 `net.ipv4.ip_forward=0`；退出网关模式或恢复出厂时，恢复进入网关模式前保存的原值。
- R5.3 不写 iptables/nftables 规则，不做 NAT、SNAT/DNAT 或端口转发（parent D3）。
- R5.4 前端在开关旁说明其作用，以及"不做 NAT，下行设备无法主动访问上行网段之外的目标；
  上行主机访问摄像头需要回程路由"这一后果。

### R6 API、权限、错误与审计

- R6.1 模式切换复用 `ops:network:mode`；租约查询复用 `ops:network`。
- R6.2 新增 errno（parent 统一登记）：
  - `1115 CodeNetworkGatewayPoolInvalid` / 400：地址池、掩码或租约时长非法；
  - `1116 CodeNetworkDhcpServerConflict` / 409：目标链路已存在 DHCP 服务，或接口为
    DHCP client 模式。
- R6.3 模式切换、`ip_forward` 变更、确认、取消与自动回滚写操作日志；DHCP 租约分配本身
  不写操作日志（量大且非管理员动作），只写受控 zap 日志。
- R6.4 新增 action key 与错误文案补齐三语。

### R7 前端

- R7.1 网关模式面板包含：下行接口选择、地址池起止、掩码、租约时长、`ip_forward` 开关。
- R7.2 R3.3 的警示与 R5.4 的说明必须在提交前可见。
- R7.3 网关模式生效后，页面提供租约列表视图（R4.3 的字段）。
- R7.4 复用既有 pending 倒计时与确认/取消交互；文案三语对齐。

### R8 平台边界与可测试性

- R8.1 DHCP 服务与 `ip_forward` 写入均由 Go 结构化实现，不调用 shell、不启动外部进程。
- R8.2 DHCP 服务的生命周期由 `NetworkService.Start`/`Close` 管理，不在构造函数中启动；
  与 08-22 的生命周期约定一致。
- R8.3 fake platform 支持模拟 DHCP 探测结果与 `ip_forward` 读写，使冲突防护、校验、
  事务与租约持久化逻辑可在 CI 中验证，不触碰开发机真实网络。

## Acceptance Criteria

`[CI]` fake platform 自动验证；`[目标机]` 需 Linux host-root 环境；`[台架]` 需真实摄像头或 DHCP 客户端设备。

- [x] AC1 `[CI]` fake Linux capability 含 `gateway`，macOS capability 不含且请求时返回
      `CodeNetworkUnsupported`；真实 Linux 只有在平台层真实化完成并通过能力探测后才开放该模式。
- [x] AC2 `[CI]` 非法地址池组合（起 > 止、跨子网、含接口地址、含网络/广播地址、租约时长
      越界）均返回 `CodeNetworkGatewayPoolInvalid`，且证明宿主机配置未被部分修改。
- [x] AC3 `[CI]` 目标接口为 DHCP client 模式、或探测到该链路已有 DHCP 服务时，返回
      `CodeNetworkDhcpServerConflict`。
- [x] AC4 `[CI]` 租约表原子持久化并可在重启后恢复；退出模式时被清理。
- [x] AC5 `[CI]` `ip_forward` 开关的开启、关闭与退出模式时恢复原值三条路径有测试覆盖。
- [x] AC6 `[CI]` 权限缺失时返回 403；模式切换与 `ip_forward` 变更产生可查询审计，
      租约分配不写操作日志。
- [ ] AC7 `[目标机]` 未确认时 120 秒后自动回滚：DHCP 服务停止、socket 释放、
      `ip_forward` 恢复原值、接口配置还原。
- [ ] AC8 `[台架]` 接入下行 LAN 口的摄像头无需预设 IP 即可获得池内地址，本设备可直接拉取
      其 RTSP 流；租约列表正确显示该摄像头的 MAC 与 IP。
- [ ] AC9 `[台架]` 开启 `ip_forward` 后，上行网段中已配置回程路由的主机可访问下行摄像头；
      **同时确认**下行摄像头无法主动访问上行网段之外的目标属于预期行为（不做 NAT），
      不判为失败。
- [x] AC10 `[CI]` `app/` 的 `make vet`、`make test` 与 `ui/` 的 `pnpm check`、相关单元测试通过。

## Out of Scope

- NAT、SNAT/DNAT、端口转发、防火墙规则（parent D3，列入 parent Deferred）。
- DHCP 选项扩展：DNS、域名、NTP、TFTP/PXE、厂商私有选项。
- 静态 MAC 绑定保留地址（D6）。
- DHCPv6 与 IPv6 相关的任何能力。
- 多个下行接口同时开启 DHCP 服务；首版限定单个下行口。
- 摄像头发现、ONVIF 探测、RTSP 拉流本身。
- DHCP relay、二级网关级联。
- macOS 的任何等价实现。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| DHCP 服务误投放到生产网 | 污染整个广播域，影响范围远超本设备 | 三道防线：socket 绑定单接口（D1）、启用前探测（D2/R3.1）、UI 强警示（R3.3） |
| 运行期出现第二个 DHCP 服务 | 地址冲突，摄像头随机失联 | 告警 + overview 体现，但不自动停服，避免瞬时干扰导致租约中断（R3.2） |
| 开启 `ip_forward` 扩大攻击面 | 设备成为跨网段跳板 | 默认关闭、显式开关、退出模式恢复原值（D4/R5.2） |
| 不做 NAT 导致用户预期落空 | 验收争议 | Confirmed Facts、R5.4 UI 说明与 AC9 显式写明后果 |
| 租约表损坏 | 重启后地址重复分配 | 沿用 08-22 的版本化 envelope + 校验和 + 原子替换（R4.1） |
| 目标接口同时是主出口 | 整机出口被下行专网占用，设备失去上行 | R2.1 禁止选择当前主出口 |
| 台架依赖（真实摄像头或 DHCP 客户端） | 硬件不到位则 AC8/AC9 无法验收 | 分档标注；CI 项不受阻，但归档需台架通过 |

## Artifacts

- `prd.md`：本文件。
- `design.md`：网关模式、DHCP server、租约状态与 `ip_forward` 生命周期的技术设计。
- `implement.md`：后端、API、前端与测试的执行计划及前置门禁。
- `api.md`：本 child 的前后端 API 契约；不修改已归档或正在收敛的 active-backup 契约文件。
