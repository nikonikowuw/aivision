# 高级网络工作模式（Parent）

状态：`planning`（parent 任务，持有需求集合与任务地图，不作为实现目标）

## Goal

在 `08-22-network-configuration` 已交付的 netconfig 平台层之上，为工控机与边缘 AI 计算设备
新增三种进阶网络工作模式：主备容错（Active-Backup Bonding）、链路聚合（LACP 802.3ad）、
边缘网关与内置 DHCP Server。三种模式共用同一套候选事务、120 秒防失联回滚、RBAC 与审计机制，
由本 parent 任务统一需求边界与跨 child 验收，具体实现拆入三个可独立验证的 child 任务。

## Background / Confirmed Facts

- **08-22 的代码已合并到 `dev`（`c19d9aa`..`e2ab18f`），但平台层是测试替身骨架**：
  `manager_linux.go:109-119` 与 `manager_darwin.go:118-130` 的 `Read`/`Apply`/`Restore`
  全部委托 `p.fake`；`go.mod` 无 netlink 与 dhcp 依赖；`config.yaml:39` 为
  `fake_platform: true`。业务层（候选事务、状态存储、API、前端页面、菜单 migration）是真实的。
  三个 child 的**业务层**可在其上落地，**真实内核能力**则全部阻塞于该平台层的真实化
  （已由第四 child `08-23-linux-platform-realization` 承接，见 Task Map）。
- 08-22 已明确把 `Bond`、`链路聚合`、`自动主备切换`、`防火墙`、`端口转发` 列入 Out of Scope
  （`.trellis/tasks/08-22-network-configuration/prd.md:191-192`）。本任务是其显式后继，
  不是对该边界的违反，但每一处扩展都必须在 child 的 `design.md` 中写明扩展点。
- 08-22 已确立且本任务必须**复用而非重建**的机制：
  - `Platform` 接口（`Probe/Discover/Read/Apply/Restore/Close`）与封闭的 `HostPlan`/`HostSnapshot`
    类型（`design.md:44-59`）；
  - 整机单一候选事务 + 120 秒确认窗口 + 超时自动回滚 + 启动恢复（`design.md:112-160`）；
  - root-only 原子状态存储 `factory.json` / `last-valid.json` / `pending.json`（`design.md:114-130`）；
  - `ops:network*` 五个权限码与 `system.log.actionNetwork*` 审计 action（`api.md:301-332`）；
  - errno `1100-1111` 网络错误码段（`api.md:334-355`）。
- 08-22 的安全取舍：Go API 以 root 运行，因此**禁止 shell 解释器执行网络配置**，
  **禁止根据 HTTP 输入拼接命令**（`prd.md:126-128`）。本任务全部新增能力必须同样通过
  结构化系统 API 完成。
- 08-22 的平台矩阵是 Linux + macOS 双平台，两端满足相同业务语义（`prd.md:113-114`）。
  本任务引入的能力均为 Linux-only，因此必须新增 capability 协商机制，见 D2。
- 默认容器形态（`app/Dockerfile` 的 `CGO_ENABLED=0`、`deploy/docker-compose.yml` 无
  host network 与 `NET_ADMIN`）不支持网络写入，本任务同样只在原生 host-root 部署矩阵验收。

## Key Decisions

- **D1 任务结构**：拆为 parent + 4 个 child（三种模式 + 平台层真实化，后者于
  2026-08-23 增补）。三种模式的技术栈、风险面与验收台架互不重叠，
  可分别规划、实现、检查与归档；平台层真实化是三者的共同真实验收解锁点。parent 只持有需求集合、任务地图、跨 child 验收与集成评审。
- **D2 macOS 显式不支持**：bonding mode 1/4、`net.ipv4.ip_forward` 与 DHCP server 均为
  Linux-only 能力。macOS 平台实现对这些模式返回 `CodeNetworkUnsupported`，前端依据后端
  下发的 capability 声明隐藏或禁用对应面板。**不得静默降级**，也不用 macOS bridge 近似
  模拟——bridge 与 bonding 语义不等价，会产生假绿测试。代价已知：macOS 开发机无法端到端
  自测，只能依赖 fake platform 测试替身 + Linux 目标机集成验证。
- **D3 网关模式内置 Go DHCP Server，不做 NAT**：复用 08-22 已引入的
  `github.com/insomniacslk/dhcp`（08-22 用其 client 侧，本任务用 `server4`），不引入
  dnsmasq 外部进程；仅开启 `net.ipv4.ip_forward` 做纯三层转发，不写 iptables/nftables 规则。
  理由：守住 08-22「不碰防火墙/端口转发」与「禁止 shell 调用」两条既定边界，避免外部依赖的
  打包、版本与进程监管成本。NAT 与端口转发列入 Deferred。
- **D4 模式框架随第一个 child 落地**：`NetworkMode` 枚举、capability 协商、模式切换 API 与
  前端模式面板骨架不单独成 child，也不由 parent 实现，而是在
  `08-23-active-backup-bonding` 中一并建立；后续两个 child 只注册新模式。
  理由：避免为尚不存在的第二、三种模式提前抽象。
- **D5 多址模式不重新实现**：`Multi-Address` 是 08-22 的既有交付物与默认模式。本任务对它
  只做一件事——把它表示为模式枚举中的默认值，使其可作为其他模式的回退目标。
- **D6 bonding 通过 rtnetlink 创建**：`bond` 虚拟接口的创建、slave 绑定（`IFLA_MASTER`）、
  mode/miimon/primary/xmit_hash_policy 参数下发全部走 rtnetlink 结构化调用，不使用
  `ip link` / `ifenslave` / sysfs 字符串写入。与 08-22 的 R5.4/R5.8 一致。

## Task Map

| Child | 目录 | 优先级 | 交付物 | 前置 |
| --- | --- | --- | --- | --- |
| 平台层真实化 | `08-23-linux-platform-realization` | P1 | Linux rtnetlink/DHCP/Bond 原语 + macOS SystemConfiguration 真实实现 | 08-22 实现完成；与三个模式 child 并行 |
| 主备容错 Bonding | `08-23-active-backup-bonding` | P2 | 模式框架 + capability 协商 + bonding mode 1 | 08-22 实现完成 |
| 链路聚合 LACP | `08-23-lacp-aggregation` | P3 | bonding mode 4 + 交换机侧配置引导 | active-backup-bonding 完成 |
| 边缘网关 DHCP | `08-23-edge-gateway-dhcp` | P3 | 内置 DHCP Server + ip_forward | active-backup-bonding 完成 |

顺序约束写在各 child 的 `prd.md` / `implement.md` 中，不由树位置隐含。LACP 与网关两个 child
之间无依赖，可并行。平台层真实化与三个模式 child 均可并行——模式 child 的业务层在 fake 上自足，
其 `[阻塞]`/`[台架]` 验收由平台层真实化解锁。

## Requirements（parent 直接持有）

### R1 需求边界与一致性

- R1.1 三个 child 共享同一套模式切换语义：任意模式切换都是一次整机候选事务，纳入 120 秒
  确认窗口与超时自动回滚；不存在绕过该协议的"快速切换"路径。
- R1.2 三个 child 共享同一套 capability 语义：平台不支持的模式必须显式返回
  `CodeNetworkUnsupported`，前端据 capability 隐藏或禁用，不得静默降级或伪造成功。
- R1.3 三个 child 新增的 errno 统一在 08-22 的 `1100-1111` 之后顺延分配，由本 parent 登记，
  避免 child 之间号段冲突：

  | Code | 名称 | 典型 HTTP | 归属 child |
  | ---: | --- | ---: | --- |
  | 1112 | `CodeNetworkBondSlaveInvalid` | 409 | active-backup-bonding |
  | 1113 | `CodeNetworkBondModeConflict` | 409 | active-backup-bonding |
  | 1114 | `CodeNetworkLacpNegotiationFailed` | 503 | lacp-aggregation |
  | 1115 | `CodeNetworkGatewayPoolInvalid` | 400 | edge-gateway-dhcp |
  | 1116 | `CodeNetworkDhcpServerConflict` | 409 | edge-gateway-dhcp |

  平台或能力不支持一律复用 08-22 既有的 `1106 CodeNetworkUnsupported`，不新增同义码。

- R1.4 三个 child 新增的权限码统一在 `ops:network*` namespace 下扩展，不新建 namespace。
  本轮只新增一个：`ops:network:mode`（切换工作模式）。confirm / cancel / reset / 只读
  overview 全部复用 08-22 既有权限码。

### R2 集成评审

- R2.1 每个 child 归档前，由本 parent 复核其对 08-22 契约的扩展是否向后兼容：既有
  `Multi-Address` 模式的 API 行为、状态文件 schema 版本与前端页面不得回归。
- R2.2 全部 child 完成后执行一次跨模式集成验证，覆盖模式之间的相互切换与回滚。

## Cross-child Acceptance Criteria

验收分三档标注，避免把需要真实台架的项目混进 CI 门禁：
`[CI]` 可在 fake platform 上自动验证；`[目标机]` 需 Linux host-root 环境；`[台架]` 需真实硬件。

- [ ] AC1 `[CI]` 模式枚举、capability 协商与模式切换 API 在 fake platform 上完整可测，
      覆盖每种模式的成功切换、不支持返回、事务冲突、超时回滚与启动恢复。
- [ ] AC2 `[CI]` macOS 构建下三种高级模式均返回 `CodeNetworkUnsupported`，前端在
      capability 缺失时不渲染对应面板；该行为有测试固定。
- [ ] AC3 `[目标机]` 任意两种模式之间的相互切换均可成功应用，且未确认时能在 120 秒后
      自动回滚到切换前的完整拓扑（含 bond 接口拆除与 slave 归还）。
- [ ] AC4 `[目标机]` 三种模式各自的状态在服务重启后可正确恢复；候选事务残留被启动恢复清理。
- [ ] AC5 `[台架]` 三个 child 各自的硬件验收项（见各 child PRD）全部通过。
- [ ] AC6 `[CI]` `app/` 的 `make vet`、`make test` 与 `ui/` 的 `pnpm check`、相关单元测试通过。

## Out of Scope

- NAT、SNAT/DNAT、端口转发、防火墙规则管理（见 D3，列入 Deferred）。
- 其余 bonding 模式（mode 0/2/3/5/6）、VLAN、网桥、策略路由、ECMP。
- IPv6 相关的任何能力，与 08-22 保持一致。
- DHCP Server 的 DNS 转发、TFTP/PXE、静态 MAC 绑定保留地址。
- 摄像头发现、ONVIF 探测、RTSP 拉流本身——本任务只负责为其提供网络通路。
- 多设备集中下发；范围仍是运行本系统的本机。
- macOS 上的任何等价实现（见 D2）。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| 08-22 仍在实现中（`in_progress`），契约可能继续变动 | child 的扩展点建立在移动靶上 | 08-22 完成并归档前所有 child 停留在 planning；其 `design.md` / `api.md` 定稿后再补 child 的 design |
| 模式切换比单接口改地址风险高一个量级 | bond 创建失败可能同时丢失两块网卡 | 复用 120 秒事务；回滚必须包含 bond 拆除与 slave 归还，作为 AC3 强制项 |
| macOS 开发机无法自测 Linux-only 能力 | 实现期反馈环变长，易积累集成缺陷 | fake platform 覆盖全部业务逻辑；Linux 目标机验证作为 child 的独立门禁 |
| LACP 需要交换机侧同步配置 LAG | 单侧配置会导致链路不通或环路 | 界面前置提示 + 需求侧承认这是台架验收项，不做自动协商检测 |
| 内置 DHCP Server 误接入生产网 | 与现网 DHCP 冲突，影响范围超出本设备 | 仅允许在明确指定的下行接口启用；启用前检测该链路已有 DHCP 响应则拒绝 |
| bonding 参数经 rtnetlink 下发的内核兼容性 | 不同内核版本对 bond netlink 属性支持有差异 | Probe 阶段做能力检查，不支持则 fail closed 返回 unsupported |
| 新增 errno/权限码与 08-22 冲突 | 前后端契约错位 | 由 parent 统一登记号段（R1.3/R1.4） |
| **Deferred** | | |
| NAT / 端口转发 | 网关模式下行设备无法主动访问外网 | 本轮不做；如现场确有需求，另开独立 task 重新评估安全边界 |

## Artifacts

- `prd.md`：本文件，parent 需求集合与任务地图已收敛。
- `design.md` / `implement.md`：parent 不实现，不产出。
- `08-23-active-backup-bonding`：`prd.md` / `design.md` / `implement.md` / `api.md` 已就绪，
  范围限定为业务层模式框架 + fake 平台，等待用户 review 后 `task.py start`。
- `08-23-lacp-aggregation` / `08-23-edge-gateway-dhcp`：`prd.md` 已就绪；
  `design.md` / `implement.md` 待模式框架落地后补齐。
