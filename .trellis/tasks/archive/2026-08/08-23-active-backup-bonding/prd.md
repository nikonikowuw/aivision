# 主备容错网络模式 (Active-Backup Bonding)

状态：`planning`
Parent：`.trellis/tasks/08-23-advanced-network-modes`

## Goal

在 netconfig 平台层引入**网络工作模式**这一概念，并落地第一种高级模式：Linux bonding
mode 1（active-backup）。管理员可将两块物理网卡绑定为单一逻辑接口，主用链路断开时由内核
自动切换到备用链路，业务连接不中断。

本 child 同时承担三种模式共用的框架工作（parent D4）：`NetworkMode` 枚举、capability 协商、
模式切换 API 与前端模式面板骨架。后续两个 child 只在此框架上注册新模式。

**本轮交付范围限定为业务层 + fake 平台**（见 D6）：真实的 Linux bond 内核实现阻塞于 08-22
平台层的真实化，不在本轮。

## 前置依赖

- `08-22-network-configuration` 的代码已合并到 `dev`（提交 `c19d9aa`..`e2ab18f`），
  `Platform` 接口、`HostPlan` 模型、候选事务状态机、root-only 状态存储、API、前端页面与
  菜单 migration 均已存在，本 child 在其上扩展。
- 但 08-22 的**平台层是测试替身骨架**，详见 Background。真实 bond 能力阻塞于其平台层真实化。
- 本 child 是 `08-23-lacp-aggregation` 与 `08-23-edge-gateway-dhcp` 的前置：模式框架在此建立。

## Background / Confirmed Facts

### 08-22 的实际实现状态（已核对代码，非文档推断）

- **业务层是真实的**：`app/internal/service/network.go` 已实现候选事务、`pending.json`
  持久化、`time.AfterFunc` 超时回滚、启动恢复、出厂基线初始化。
- **平台层是 fake 骨架**：`manager_linux.go:109-119` 与 `manager_darwin.go:118-130` 的
  `Read` / `Apply` / `Restore` 全部 `return p.fake.XXX(ctx)`。`go.mod` 无 netlink 与 dhcp
  依赖；Linux 的 `Discover` 用标准库 `net.Interfaces()` 且 `Mode`/`Ownership`/`Writable`/
  `Fingerprint` 为硬编码；macOS cgo bridge 只实现服务枚举。
- `app/configs/config.yaml:39` 为 `fake_platform: true`（`config.go:120` 的内置默认是 `false`）。
- `Capabilities` 目前硬编码在 `service/network.go:199-204`，不是平台声明的。
- 状态存储 `state.go:202` 对 `SchemaVersion != CurrentSchemaVersion` 直接判为
  `ErrStateCorrupt`，**没有多版本兼容读取**。这决定了模式字段只能以可选字段形式加入，
  不能递增 schema 版本（见 D7）。
- 既有 5 个权限码与 6 条路由已在 `router.go:219-233` 注册；菜单与按钮权限由
  `migrations/000009_add_ops_network_menu.up.sql` 写入。

### 技术事实

- Linux bonding 的 `bond` 虚拟接口可通过 rtnetlink 以结构化方式创建：`IFLA_INFO_KIND="bond"`
  携带 `IFLA_BOND_MODE`、`IFLA_BOND_MIIMON`、`IFLA_BOND_PRIMARY` 等属性，slave 绑定通过
  设置从接口的 `IFLA_MASTER` 完成。全程无需 shell、sysfs 字符串写入或 `ifenslave`。
  这是未来真实化时的实现路径，本轮不落地。
- bonding mode 1 的链路检测由内核 `miimon` 承担，切换发生在内核态，不依赖用户态轮询。
- macOS 不存在 active-backup 语义的等价物（其 bond 接口是 802.3ad LAG），按 parent D2
  显式不支持。
- 08-22 的 `HostPlan` 以「受管接口 ID → InterfacePlan」组织，主出口唯一。bond 接口需要
  作为一个新的受管接口进入该模型，而其 slave 必须退出可配置集合——这是对 08-22 模型的
  主要扩展点。

## Key Decisions

- **D1 bond 接口不接受用户命名**：接口名由系统生成（如 `bond0`），不作为 HTTP 输入。
  理由：08-22 禁止根据 HTTP 输入拼接系统标识；接口名会进入内核对象命名空间。
- **D2 `miimon` 固定为 100ms，不暴露给用户**：这是 bonding mode 1 的通行默认值，暴露它
  只会增加误配面。若现场确有调优需求，再作为配置项另议。
- **D3 slave 网卡在绑定期间不可单独配置 IPv4**：进入 bonding 模式时，被选中的物理网卡从
  可写接口集合中移除，其原有 IPv4 配置由事务快照保存；bond 接口继承整机主出口候选资格。
  退出 bonding 模式时归还 slave 并恢复其快照配置。
- **D4 首版固定 2 块 slave**：mode 1 的主备语义只需要主用 + 备用各一。多于 2 块网卡的
  N 重备份不在本轮范围。
- **D5 primary slave 必选**：管理员必须显式指定主用网卡，不使用内核默认的"先加入者为主"。
  理由：现场往往有明确的线路优先级（如主用接交换机 A、备用接交换机 B）。
- **D6 本轮只做业务层，真实内核实现阻塞外置**：交付 `NetworkMode` 模型、capability 协商、
  模式切换 API、事务复用、fake 平台的 bond 拓扑模拟与前端模式面板。`LinuxPlatform` 诚实地
  声明**不支持** `active-backup`（因其 `Apply` 仍是 fake），只有 `FakePlatform` 声明支持。
  等 08-22 平台层真实化后，只需给 `LinuxPlatform` 补 rtnetlink 实现并把该模式加入其
  capability，业务层无需改动。理由：capability 协商机制本身就是为"平台能力不齐"设计的，
  用它诚实表达当前状态，比伪造一个跑不通的 Linux 分支更符合 R2.2。
- **D7 不递增状态文件 `SchemaVersion`**：`state.go:202` 对版本不匹配直接判 `ErrStateCorrupt`，
  没有多版本兼容读。模式与 bond 拓扑以 `omitempty` 可选字段加入 `HostPlan`/`HostSnapshot`，
  旧文件反序列化后字段为零值即视为 `multi-address`。这样既有 `factory.json` /
  `last-valid.json` 无需迁移，`readEnvelope` 与 checksum 机制零改动。
- **D8 不给 `Platform` 接口加 Apply/Read 之外的写方法**：`Apply(ctx, plan)` 已接收完整
  `HostPlan`，模式随 plan 下传；`Read` 返回完整 `HostSnapshot`，拓扑随 snapshot 返回。
  只新增一个只读的 `Capabilities(ctx)` 方法用于能力声明。三个 child 共用同一接口形状。

## Requirements

### R1 网络工作模式框架

- R1.1 引入 `NetworkMode` 枚举，首版取值 `multi-address`（默认，即 08-22 既有行为）与
  `active-backup`。枚举为封闭字符串集合，前后端共享。
- R1.2 整机在任一时刻处于且仅处于一种工作模式。模式是整机属性，不是接口属性。
- R1.3 `NetworkOverview` 扩展返回当前模式、平台支持的模式列表（capability）以及当前拓扑
  （bond 接口与其 slave 的从属关系）。
- R1.4 新增模式切换 API，接受目标模式与该模式所需参数；切换是一次整机候选事务，复用
  08-22 的 120 秒确认窗口、超时自动回滚与启动恢复，不存在绕过该协议的路径。
- R1.5 模式切换与既有的单接口 IPv4 配置共用同一个 pending 事务槽位：存在待确认事务时，
  模式切换返回 `CodeNetworkTransactionPending`，反之亦然。

### R2 capability 协商

- R2.1 平台实现通过新增的只读 `Capabilities(ctx)` 声明其支持的模式集合，service 层不得再
  硬编码 `Capabilities`（现状 `service/network.go:199-204`）。本轮各平台的声明：
  `FakePlatform` = `multi-address` + `active-backup`；`LinuxPlatform` 与 `DarwinPlatform`
  = 仅 `multi-address`（见 D6）。
- R2.2 请求平台不支持的模式返回 `CodeNetworkUnsupported`，不得静默降级或伪造成功。
- R2.3 `LinuxPlatform` 未来真实化时，需在 `Probe` 阶段验证内核对所需 bond netlink 属性的
  支持，不满足则从 capability 中摘除 `active-backup`，fail closed。本轮 `Probe` 不变。
- R2.4 前端依据 capability 渲染模式面板：不支持的模式隐藏或禁用并给出原因，不发起注定
  失败的请求。

### R3 主备容错配置

- R3.1 管理员从当前可写物理网卡中选取恰好 2 块作为 slave，并指定其中一块为 primary。
- R3.2 bond 接口的 IPv4 配置沿用 08-22 的 `dhcp | static` 模型，包括地址、prefix、
  以及作为主出口时的默认网关与 DNS。
- R3.3 提交前校验：两块 slave 存在、可写、指纹未变、互不相同、当前未被其他虚拟接口占用。
- R3.4 进入模式时，slave 的原 IPv4 配置写入事务 `before` 快照；退出时精确恢复。
- R3.5 bond 参数 `mode=1`、`miimon=100`、`primary=<指定网卡>` 进入 `HostPlan` 并由平台消费。
  本轮由 `FakePlatform` 模拟其效果；真实内核下发随平台真实化补齐。

### R4 事务、回滚与恢复

- R4.1 模式切换失败时，服务调用 `platform.Restore(ctx, before)`，必须完成 bond 接口拆除
  与 slave 归还，不允许留下孤立的 bond 接口或未归还的 slave。
- R4.2 120 秒内未确认则自动回滚，回滚范围同 R4.1，复用既有 `handleTimeout`。
- R4.3 服务重启时若发现未完成的模式切换事务，启动恢复阶段执行同样的回滚，复用既有
  `Start` 中的启动恢复分支。
- R4.4 确认成功后，当前模式与 bond 拓扑随 `HostPlan` 写入 `last-valid.json`。
  **不递增 `SchemaVersion`**（D7），既有旧文件必须仍可读取。
- R4.5 恢复出厂操作在 bonding 模式下必须先退出模式再恢复接口基线。

### R5 API、权限、错误与审计

- R5.1 新增权限码 `ops:network:mode` 与端点 `PUT /api/network/mode`，在 `router.go` 与
  新 migration 中注册；confirm/cancel 复用既有 `ops:network:confirm` / `ops:network:cancel`；
  overview 复用 `ops:network`。
- R5.2 新增 errno（parent 统一登记）：
  - `1112 CodeNetworkBondSlaveInvalid` / 409：slave 不存在、不可写、重复、已被占用或数量不符；
  - `1113 CodeNetworkBondModeConflict` / 409：目标模式与当前拓扑冲突。
  - 平台不支持复用既有 `1106 CodeNetworkUnsupported`。三语文案在 `errno.go` 补齐。
- R5.3 模式切换本身、以及针对模式切换事务的确认、取消与自动回滚均写操作日志，摘要包含
  目标模式、slave 列表、primary、切换前后拓扑与结果；不记录 native snapshot 与内部路径。
  **审计补写仅限模式切换路径**：既有 `ApplyInterface` / `FactoryReset` 的审计缺口属于
  08-22 遗留，不在本 task 修改范围（已单独提示）。
- R5.4 新增审计 action key 并补齐 `zh-CN` / `en-US` / `zh-TW` 三语文案。
- R5.5 API 契约变更以 `v1` 写入本 child 自有 `api.md`；跨前后端契约须在激活任务前由用户确认。

### R6 前端

- R6.1 在既有 `/ops/network` 页面新增工作模式面板，展示当前模式、可用模式与当前拓扑。
- R6.2 切换模式前弹出确认，明确提示网络拓扑将被重构、可能短暂失联，并说明 120 秒
  未确认自动回滚。
- R6.3 复用 08-22 的 pending 事务倒计时与确认/取消交互，不新建一套。
- R6.4 拓扑以可读方式呈现 bond 与 slave 的从属关系及各 slave 的链路状态。
- R6.5 页面、按钮、错误与 action 文案三语对齐。

### R7 平台边界与可测试性

- R7.1 模式与 bond 拓扑经封闭的 Go 结构体随 `HostPlan` 下传，不接受任意字符串、命令片段
  或平台私有字典。未来真实化时 bond 的创建、参数下发、slave 绑定与拆除必须走 rtnetlink
  结构化调用，不使用 shell、`ip link`、`ifenslave` 或 sysfs 字符串写入。
- R7.2 `FakePlatform` 扩展支持 bond 接口的创建/拆除、slave 归属标记与拓扑快照，使全部
  业务逻辑、事务、回滚、capability 与权限行为可在不触碰真实网络的前提下单测。
- R7.3 `Platform` 接口只新增只读的 `Capabilities(ctx)`，其余签名不变（D8）。

## Acceptance Criteria

`[CI]` fake platform 自动验证，属本轮门禁；`[阻塞]` 依赖 08-22 平台层真实化，本轮不验收。

- [x] AC1 `[CI]` `NetworkOverview` 返回当前模式、`supportedModes` 与拓扑；`LinuxPlatform` /
      `DarwinPlatform` 的 `supportedModes` 只含 `multi-address`，向其请求 `active-backup`
      返回 `CodeNetworkUnsupported`。
- [x] AC2 `[CI]` 模式切换走完整候选事务：pending 冲突、超时回滚、启动恢复、
      确认后持久化四条路径均有测试覆盖。
- [x] AC3 `[CI]` 非法 slave 组合（不存在、重复、数量不为 2、不可写、primary 不在 slave 集合内、
      指纹变化）返回 `CodeNetworkBondSlaveInvalid`，且证明状态未被部分修改。
- [x] AC4 `[CI]` 权限缺失时模式切换返回 403；模式切换及其确认、取消、自动回滚均产生
      可查询审计。
- [x] AC5 `[CI]` 未递增 `SchemaVersion`，08-22 写出的旧 `factory.json` / `last-valid.json`
      仍可读取；既有 `multi-address` 的 API 行为与前端页面无回归。
- [x] AC6 `[CI]` 在 `FakePlatform` 上切到 `active-backup` 后，overview 出现 bond 接口、
      两块 slave 标记归属且退出可写集合；切回 `multi-address` 后 bond 消失、slave 归还
      并恢复原 IPv4 配置。
- [x] AC7 `[CI]` `app/` 的 `make vet`、`make test` 与 `ui/` 的 `pnpm check`、相关单元测试通过。
- [ ] AC8 `[阻塞]` Linux 目标机上真实创建 bond0（mode=1, miimon=100, primary=指定网卡）、
      拔线 1 秒内切换、TCP 连接不断开。**阻塞于 08-22 Linux 平台层真实化，本轮不验收。**

## Out of Scope

- **真实 Linux bond 内核实现（rtnetlink 下发、slave 绑定、miimon 生效）**：阻塞于 08-22
  平台层真实化，本轮只交付业务层与 fake 模拟（D6）。
- **08-22 平台层的真实化本身**（rtnetlink、DHCP 客户端、SystemConfiguration 写入）。
- **08-22 遗留的审计缺口**：`ApplyInterface` 未写操作日志、`ConfirmTransaction` /
  `FactoryReset` 收了 actor 参数却未使用。本 task 只补模式切换路径的审计。
- 其余 bonding 模式（0/2/3/4/5/6）；mode 4 见 `08-23-lacp-aggregation`。
- 多于 2 块 slave 的 N 重备份。
- `miimon` 以外的链路监测方式（如 ARP monitoring）与其参数调优。
- bond 之上再叠加 VLAN、网桥或二次绑定。
- macOS 的任何等价实现。
- 切换过程中对上层业务（RTSP 拉流、推理任务）的主动通知或重连编排。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| 交付物在真实硬件上不可用 | 模式面板可切换但内核无 bond，易被误认为功能已就绪 | `LinuxPlatform` 诚实声明不支持该模式（D6），UI 据 capability 禁用；AC8 明确标注 `[阻塞]` |
| 08-22 平台层真实化时接口形状可能变 | 本轮的模型扩展需返工 | D8 只加只读 `Capabilities`，模式随既有 `HostPlan`/`HostSnapshot` 流动，改动面最小 |
| 状态文件兼容 | 递增版本会让旧文件直接判损坏（`state.go:202`） | D7 不递增版本，用 `omitempty` 可选字段；AC5 固定该行为 |
| 绑定操作同时影响两块网卡 | 真实化后失败可能造成整机失联 | 复用 120 秒事务；`Restore(before)` 天然覆盖拓扑恢复；AC6 在 fake 上先固定语义 |
| slave 原配置恢复不完整 | 退出模式后网卡处于无地址状态 | `before` 快照是完整 `HostSnapshot`，`Restore` 整体替换；AC6 验证 |
| 模式框架被后两个 child 复用 | 此处设计缺陷会放大 | 框架部分在 `design.md` 中单独成节，parent 集成评审时复核 |
| 审计只补模式路径造成不一致 | 同一页面部分操作有日志、部分没有 | 属 08-22 遗留，已在 Out of Scope 声明并单独提示，不在本 task 静默扩大改动面 |

## Artifacts

- `prd.md`：本文件。
- `design.md`：技术设计，对照 `dev` 分支真实代码编写。
- `implement.md`：执行计划与验证命令。
- `api.md`：模式切换端点契约，激活前须用户确认。
