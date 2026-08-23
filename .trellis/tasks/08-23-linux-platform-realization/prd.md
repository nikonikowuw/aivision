# Linux/macOS 平台层真实化 (rtnetlink/DHCP/Bond/SystemConfiguration)

状态：`planning`
Parent：`.trellis/tasks/08-23-advanced-network-modes`（第四 child，2026-08-23 增补）
前继：`08-22-network-configuration`（平台层部分）

## Goal

把 08-22 交付的 fake 平台骨架替换为**真实实现**：

- **Linux**：`LinuxPlatform` 的 `Discover` / `Read` / `Apply` / `Restore` 直接操作
  rtnetlink 与系统 resolver，本系统拥有受管网卡的 DHCPv4 生命周期与配置持久化重放，
  并提供 bonding/LACP 所需的 netlink 原语与内核能力检测。
- **macOS**：`DarwinPlatform` 经 SystemConfiguration C API 真实读写已有 Network
  Service 的 IPv4 与 DNS，commit/apply 分离，Dynamic Store 回读实际生效状态。

完成后：08-22 的「管理员从 Web 改网、无需 SSH」在真实设备上兑现；三个模式 child 的
真实内核验收（如 bonding AC8「拔线 1 秒内切换」）从 `[阻塞]` 变为可执行；业务层、
API 契约与前端**零改动**（各 child 的 D6/D8 已按此未来设计）。

## 为什么需要本任务（缺口声明）

- 08-22 合并到 `dev`（`c19d9aa`..`e2ab18f`）的是**业务层 + fake 平台骨架**：
  `manager_linux.go:109-122` 与 `manager_darwin.go:118-130` 的 `Read`/`Apply`/
  `Restore` 全部 `return p.fake.XXX(ctx)`；Linux 的 `Probe` 恒返回 `nil`；
  `go.mod` 无 netlink 与 dhcp 依赖；`config.yaml:39` 为 `fake_platform: true`。
  即使以 `fake_platform: false` 在真实设备运行，写路径仍只改 fake 内存态——
  **产品目标在任何真实设备上都未达成**。
- modes parent prd 第 18 行声明「真实内核能力全部阻塞于该平台层的真实化」，本任务
  即是该"真实化"的承接者（2026-08-23 增补的第四 child）。
- 三个模式 child 的业务层不依赖本任务（均在 FakePlatform 上开发），但其全部
  `[阻塞]`/`[台架]` 验收以本任务为解锁条件。
- macOS 侧同样只有骨架：08-22 research 定义的 cgo bridge 只实现了服务枚举，
  commit/apply/回滚链路不存在。

## 前置依赖与排期

- `08-22-network-configuration` 完成并归档。其 PRD 中依赖真实平台的条目
  （R5.4–R5.9、R6.1–R6.5 及 AC9/AC11/AC12/AC14）**移交本任务继承**；08-22 归档时
  需补记移交说明，保证每条需求只有一个认领方。
- **排期：与本任务之外的三个模式 child 并行**（已确认）。三个 child 的业务层在
  fake 上自足；本任务的 bond 原语（R7）只影响其真实验收解锁时点。
- `08-23-active-backup-bonding` 若已合并，其 `BondPlan`/`BondTopology` 类型与
  `Capabilities()` 协商是 R7 的接线契约；若未合并，本任务先交付平台内原语，
  capability 接线由后合并的一方完成（D7）。

## Background / Confirmed Facts（已核对代码与研究文档，非推断）

### Linux

- `LinuxPlatform.Discover` 用标准库 `net.Interfaces()`，`Ownership`/`Writable`/
  `Fingerprint`/`IPv4.Mode` 为硬编码（`manager_linux.go:55-99`）；`Probe` 恒
  `nil`；`NewLinuxPlatform` 在 `fake_platform: false` 时也内部构造 fake 兜底。
- 技术路线已由 08-22 research 定案
  （`.trellis/tasks/08-22-network-configuration/research/linux-network-stack.md`）：
  - rtnetlink 首选 `github.com/vishvananda/netlink`（Apache-2.0），只操作受管
    ifindex 与精确地址/路由元组，禁止 flush 全局状态；
  - DHCPv4 首选 `github.com/insomniacslk/dhcp` 的 `dhcpv4/nclient4`（BSD-3-Clause），
    `AF_PACKET` 绑定 ifindex，需 `CAP_NET_RAW`；租约状态机由应用层编排；
  - DNS 无 rtnetlink API：部署 Profile 保证 `/etc/resolv.conf` 为本系统独占普通
    文件，临时文件 + `fsync` + 原子替换，最多 3 个 nameserver；symlink/只读/
    托管标记拒绝写入；
  - 接口所有权事实来源是部署 Profile allowlist（首配按名称定位后持久化永久 MAC）；
    不存在证明"所有管理器均未托管"的统一 API，漂移靠 rtnetlink 订阅检测；
  - rtnetlink 多条消息不跨对象原子：应用前持久化旧快照，按可补偿顺序修改，
    任一步失败幂等恢复。
- 部署形态：root 原生进程（`CAP_NET_ADMIN` + `CAP_NET_RAW`），不加特权 helper；
  默认 Alpine 容器（`CGO_ENABLED=0`、无 `NET_ADMIN`）不在支持矩阵，必须 fail
  closed。Profile 默认路径 `/etc/aivision/network-profile.json`（`config.go:118`）。

### macOS

- 技术路线已由 08-22 research 定案
  （`.trellis/tasks/08-22-network-configuration/research/macos-systemconfiguration.md`）：
  - 配置面 `SCPreferences`/`SCNetworkConfiguration`，生效面 `SCDynamicStore`，
    二者不可混用；实际状态只能以 Dynamic Store 回读为准；
  - 持久标识用 `SCNetworkServiceGetServiceID`；service 删除重建后 ID 可能变化，
    此时标记不可匹配并停止写入，不按名称猜测；只接受 Ethernet 与 IEEE80211 类型；
  - IPv4 字典 `ConfigMethod = DHCP | Manual`（Manual 含 Addresses/SubnetMasks/
    Router），DNS 字典 `ServerAddresses`；修改前必须复制完整原字典，不得无意删除
    DHCP Client ID 等平台字段；目标 protocol 不存在时 fail closed，不隐式创建 service；
  - Apple 无原生 120 秒事务：候选值 set → `SCPreferencesCommitChanges`（持久化）→
    `SCPreferencesApplyChanges`（应用运行态）→ Dynamic Store 验证；回滚 = 原字典
    再次 set/commit/apply，且回滚前校验 preferences signature 未被外部修改；
  - 主出口事实来源是 Dynamic Store 的 `PrimaryService`/`PrimaryInterface`；
    service order / OverridePrimary 纳入候选快照；
  - cgo 边界：`bridge_darwin.c/.h` 纯 C 封装、窄结构化函数、不向 Go 返回未托管
    CF 对象；`manager_darwin_nocgo.go` 在无 cgo 构建时启动即明确不支持；
    macOS 二进制必须 `CGO_ENABLED=1` 构建。
- macOS 上 DHCP 租约由操作系统 configd 负责——本系统只需设置 ConfigMethod，
  **不自建 DHCP 客户端状态机**（与 Linux 的根本差异）。

### 复用而非重建

候选事务状态机与 120 秒确认窗口、root-only 状态存储（`factory.json`/
`last-valid.json`/`pending.json`）、errno 1106 `CodeNetworkUnsupported`、
`ops:network*` 权限、`system.log.actionNetwork*` 审计全部复用。本任务**不新增**
errno、权限码、菜单 migration 与 HTTP 端点。

## Key Decisions

- **D1 任务结构（已确认）**：挂为 modes parent 第四 child。parent Task Map 与 D1
  已同步增补。理由：它是三个模式 child 的共同前置解锁点，挂在同一 parent 下便于
  跨 child 验收与排期管理；同时它也是 08-22 平台层承诺的直接后继。
- **D2 双平台真实化（已确认，维持 08-22 R5.3 承诺）**：Linux 与 macOS 都在本任务
  范围内真实化，不推迟 macOS。里程碑上 Linux 先行（产品价值在边缘设备），macOS
  随后；两端满足相同的业务 API 与验收语义，平台不支持的能力显式返回。
- **D3 库与技术路线沿用 08-22 research 结论**：Linux 用 `vishvananda/netlink` +
  `insomniacslk/dhcp nclient4`；macOS 用 SystemConfiguration C API + 自建 cgo
  bridge。全程结构化系统 API，禁止 shell 解释器执行网络配置、禁止根据 HTTP 输入
  拼接命令（08-22 R5.8）。网关 child 未来复用 `insomniacslk/dhcp` server 侧，
  本任务引入该依赖属于双向摊薄成本。
- **D4 所有权模型**：Linux 以 Profile allowlist 为唯一事实来源 + 永久 MAC 锚点 +
  已知管理器 best-effort 检查 + rtnetlink 漂移订阅（fail closed，08-22 R5.6/AC9）；
  macOS 以 ServiceID + service/interface 指纹为标识锚点，signature 变化视为外部
  冲突并拒绝覆盖。两端都不声称能识别所有第三方修改源。
- **D5 DHCP 责任边界**：Linux 由本系统拥有租约全生命周期（DORA/T1/T2/到期/
  INIT-REBOOT 重放）；macOS 由操作系统拥有，本系统只切换 ConfigMethod 并从
  `SCDynamicStoreCopyDHCPInfo` 回读。回滚到 DHCP 时两端都只恢复模式重新租约，
  不承诺 IP 字面值不变。
- **D6 持久化重放归本系统**：Linux 启动时静态配置精确重放、DHCP 配置
  INIT-REBOOT → 重新 DORA，boot ID 变化视为重启边界；macOS 确认配置已在
  SCPreferences 持久层，重启由系统加载，本系统启动时校验 last-valid 与实际状态
  一致性即可。未完成 pending 事务先回滚（复用既有 `Start` 启动恢复编排）。
- **D7 capability 接线点**：`LinuxPlatform.Capabilities()`（bonding child 引入）
  基于 `Probe` 的内核能力检测结果动态声明——bond 属性探测失败则不声明
  `active-backup`，fail closed（bonding child R2.3 钩子在本任务落地）。合并顺序
  决定接线方：本任务后合并则由本任务接线，反之由 bonding child 接线。
  `DarwinPlatform` 对高级模式恒返回仅 `multi-address`（modes parent D2）。
- **D8 测试分层**：业务层测试继续跑 FakePlatform（零回归锚点）；Linux 平台层新增
  特权集成测试（netns + veth + 内嵌 DHCP server，build tag 隔离）进 CI 门禁；
  macOS 真实 commit/apply 不能进自动化 CI，拆出无副作用的字典转换/错误映射单测，
  端到端在专用 macOS 机器上手动执行（AC 标注 `[macOS]`）。
- **D9 容器形态不变**：默认容器部署继续不支持网络写入且必须 fail closed
  （08-22 R5.9 逐字沿用）。部署交付物：host-root systemd 单元 + Linux Profile
  样例 + macOS launchd daemon 说明；macOS 二进制构建矩阵需 `CGO_ENABLED=1`
  变体（不影响现有 `CGO_ENABLED=0` Linux 容器产物）。
- **D10 cgo 边界（macOS）**：bridge 只暴露窄的结构化 C 函数，不向 Go 返回未托管
  CF 对象、不接受命令/文件路径/脚本文本；CF/Go 指针不跨调用边界长期存活；
  每次 `SCError()` 显式处理；`SCPreferencesLock` 忙碌重试有界。

## Requirements

### R1 Discover / Read 真实化（Linux）

- R1.1 接口枚举改走 netlink：链路属性（名称、MAC、MTU、carrier、admin/oper
  status）、IPv4 地址与前缀、直连路由、默认路由。ID 与指纹基于持久硬件标识，
  删除 `net.Interfaces()` 与硬编码 `Ownership`/`Writable`/`Fingerprint`。
- R1.2 可写性由 Profile allowlist 决定；allowlist 外接口仍展示但 `Writable=false`；
  bond/vlan/bridge 等虚拟接口只读展示。
- R1.3 `HostSnapshot` 全部字段为宿主机实际生效值；读取失败返回明确错误，不伪造空值。

### R2 Apply 真实化（Linux，候选配置下发）

- R2.1 可补偿顺序：接口 UP → 地址精确替换（元组级删除/添加）→ 直连路由 → 默认
  路由迁移（主出口切换：旧降级新提升）→ DNS 原子写入。任一步失败幂等恢复已执行步骤。
- R2.2 只操作受管 ifindex 与本系统记录的地址/路由元组；禁止 flush 接口全部地址、
  操作非受管接口。
- R2.3 业务层校验（08-22 R2.3）之上，平台层对越界输入双层拒绝。
- R2.4 DNS 写入遵循 research 契约（普通文件、fsync、原子替换、≤3 nameserver、
  symlink/只读/托管标记拒绝）。

### R3 Restore 真实化（回滚与恢复出厂）

- R3.1 静态旧配置精确恢复；DHCP 旧配置恢复模式并重新租约（D5）。
- R3.2 恢复出厂按 `factory.json` 基线走同一 `Restore` 路径。
- R3.3 幂等：已达目标状态的步骤跳过而非报错；超时与启动恢复竞争下重复回滚结果一致。

### R4 DHCPv4 客户端生命周期（Linux）

- R4.1 DORA / T1 Renew / T2 Rebind / 到期清理 / 重新 DORA 全状态机；XID、客户端
  MAC、Server Identifier 校验；NAK 与重复 Offer 处理。
- R4.2 租约地址/网关/DNS/classless route 安装前校验；非法租约拒绝安装并重试 Discover。
- R4.3 carrier 变化与 context 取消时干净终止租约；重启后 INIT-REBOOT → 重新 DORA。
- R4.4 租约事件写受控 zap 日志；失败达阈值时接口标记降级并在 overview 可见。

### R5 所有权与漂移检测（Linux）

- R5.1 启动校验：Profile 缺失/非法 → 相关接口 fail closed；非特权进程 → 监听前失败。
- R5.2 首配持久化永久 MAC；后续 MAC 不匹配 → 拒绝写入并告警。
- R5.3 已知管理器托管检查（NetworkManager 等 best-effort）；冲突 → 拒绝写入并
  返回明确业务错误（复用既有 1110 漂移语义）。
- R5.4 rtnetlink 订阅：受管接口出现非本系统写入的变更 → 漂移标记、拒绝后续写入、
  告警；提供显式重新接管路径。

### R6 启动恢复与重放

- R6.1 启动序列：状态文件校验（既有）→ 未完成事务回滚（既有编排 + 本任务真实
  Restore）→ 确认配置重放（Linux 按 D6；macOS 一致性校验）→ 漂移检测就绪 → ready。
- R6.2 重放失败按 08-22 R6.5 降级：接口标记恢复失败、禁止写入、可诊断错误。

### R7 bond netlink 原语（为三个模式 child 铺路）

- R7.1 bond 创建/删除（`IFLA_INFO_KIND="bond"` + `IFLA_BOND_MODE`/`MIIMON`/
  `PRIMARY` 等）、slave 绑定解绑（slave `IFLA_MASTER`）的结构化原语；全程
  netlink，无 shell/sysfs 字符串写入。
- R7.2 `Probe` 检测内核对所需 bond 属性支持（active-backup、802.3ad、miimon）；
  不支持则从 capability 摘除（D7，fail closed）。
- R7.3 原语输入是封闭 Go 结构（与 bonding child `BondPlan` 对齐）；模式切换业务
  逻辑仍归各 child。
- R7.4 LACP（mode 4）属性一并交付（同属 `IFLA_BOND_*` 族，增量小）；交换机侧
  协商验收归 `08-23-lacp-aggregation`。

### R8 macOS 真实化（SystemConfiguration）

- R8.1 服务发现：遍历当前 `SCNetworkSet` 的已有 Network Service，持久标识用
  ServiceID，类型限 Ethernet/IEEE80211；BSD 名仅诊断用途。ServiceID 失配 →
  标记不可匹配并停止写入。
- R8.2 IPv4/DNS 读写：只修改既有 protocol 字典（DHCP/Manual + ServerAddresses），
  修改前深拷贝原字典；protocol 缺失 fail closed，不隐式创建 service。
- R8.3 应用协议：候选 set → commit → apply → Dynamic Store 验证实际地址/router/
  DNS 后才进入待确认状态；验证失败、取消、超时或启动发现 pending 时按原字典
  set/commit/apply 回滚；回滚前校验 preferences signature 未被外部修改。
- R8.4 实际状态回读一律走 `SCDynamicStore`（含 `SCDynamicStoreCopyDHCPInfo`），
  不把 preferences 字典当作生效状态；主出口以 Dynamic Store `PrimaryService`
  为准，service order/OverridePrimary 纳入事务快照。
- R8.5 出厂基线在首次接管时保存完整原字典 + 指纹 + set ID，此后不可被普通确认覆盖。
- R8.6 cgo 边界遵循 D10；`darwin && !cgo` 构建在能力检查时明确失败（08-22 R3.6）。

### R9 测试与验收分层

- R9.1 业务层既有测试全部继续跑 FakePlatform，行为零变化。
- R9.2 Linux 特权集成测试（netns + veth + 内嵌 DHCP server，build tag 隔离）：
  静态应用/回滚、DHCP 全生命周期、默认路由迁移、DNS 场景、漂移、启动重放、
  bond 原语。
- R9.3 rtnetlink 应用步骤逐项故障注入，验证补偿序与重复回滚幂等。
- R9.4 macOS：字典转换/错误映射/service 匹配逻辑做无副作用单测；端到端
  （commit/apply/回滚/重启恢复）在专用 mac 手动执行并留验收记录。

## Acceptance Criteria

标注口径：`[CI]` 自动化门禁（含 netns 特权集成测试）；`[目标机]` Linux host-root
真实设备；`[台架]` 真实交换机/双链路硬件；`[macOS]` 专用 mac 手动端到端验收。

- [ ] AC1 `[CI]` netns：静态 IPv4/网关/DNS 应用后 netlink 回读与 resolv.conf 逐字段
      一致；超时自动回滚后逐字段恢复。
- [ ] AC2 `[CI]` netns：内嵌 DHCP server 下 DORA、T1 续约、NAK 后重新 Discover、
      到期清理全通过；非法租约被拒绝安装。
- [ ] AC3 `[CI]` netns：外部进程追加地址 → 漂移标记置位 → 写入被拒且错误明确；
      显式重新接管后恢复可写。
- [ ] AC4 `[CI]` Linux fail-closed 四场景：Profile 缺失 / MAC 不匹配 / symlink
      resolv.conf / 非 root，均阻止写入且可诊断不泄露内部路径。
- [ ] AC5 `[CI]` 启动重放：预置已确认静态配置与未完成 pending 事务后启动，静态
      重放生效、pending 先回滚；boot ID 变化路径覆盖。
- [ ] AC6 `[CI]` netns：bond0（mode=1, miimon=100, primary=vethA）创建、两 veth
      绑定、删除与 slave 归还成功；不支持 bond 属性的内核上 Probe 摘除对应 capability。
- [ ] AC7 `[CI]` rtnetlink 步骤逐项故障注入，补偿后与初始快照逐字段相等；重复回滚幂等。
- [ ] AC8 `[CI]` 业务层 FakePlatform 测试全绿且无断言变更——平台层对业务层零侵入。
- [ ] AC9 `[目标机]` host-root systemd 部署：物理网卡 DHCP 与静态配置应用、确认
      持久化、整机重启后重放生效；非 root 启动监听前失败。
- [ ] AC10 `[目标机]` 08-22 AC9 三场景（声明缺失、已知管理器托管、外部漂移）真实
      设备复现并全部阻止写入。
- [ ] AC11 `[台架]` 双网卡 + 交换机：bonding child AC8 解锁——切 active-backup 后
      拔主用链路 1 秒内切换、TCP 不断开。
- [ ] AC12 `[macOS]` 专用 mac：Ethernet 与 Wi-Fi service 的 DHCP/Manual 切换、
      静态地址/网关/DNS 应用经 Dynamic Store 验证生效；不确认超时后原字典恢复；
      重启后确认配置保持；service 被外部删除重建时停止写入并报冲突。
- [ ] AC13 `[macOS]` 主出口迁移：提升新主出口后 Dynamic Store `PrimaryService` 与
      系统 DNS 跟随变化；回滚后旧主出口恢复；scoped route/resolver 不作为对外承诺。
- [ ] AC14 `make vet`、`make test` 全绿（Linux 部分；macOS cgo 文件交叉编译通过）；
      无新增 shell 调用、`os/exec` 或 sysfs 字符串写入（安全门禁）。

## Out of Scope

- Wi-Fi 关联/扫描/凭据管理（macOS 只改既有 service 的 IPv4/DNS 参数，不碰 CoreWLAN，
  08-22 D6 不变）。
- 模式切换业务逻辑、`NetworkMode` 框架、capability 协商的 service 层实现——归
  `08-23-active-backup-bonding`；本任务只提供平台原语与 Probe 检测。
- LACP 交换机侧配置引导与协商验收——归 `08-23-lacp-aggregation`。
- DHCP Server（网关下行服务）——归 `08-23-edge-gateway-dhcp`。
- IPv6、VLAN/网桥管理、NAT/防火墙、策略路由（沿用 08-22 Out of Scope）。
- 默认容器形态的网络写入支持（08-22 R5.9 不变）。
- miimon 以外的链路监测方式与其参数调优界面。

## Risks / Deferred

| Risk | Impact | Planning Response |
| --- | --- | --- |
| netns 集成测试需要特权 Linux runner | 普通 CI runner 无 NET_ADMIN；macOS 开发机无法本地执行 | 自托管 runner 或 privileged 容器 job 承载；build tag 隔离，非特权环境跳过并显式报告 |
| 内核/发行版对 bond netlink 属性支持差异 | 目标机 Probe 失败或行为不一致 | R7.2 Probe fail closed；目标机矩阵明确最低内核版本；rtnetlink 是共同 ABI（research 已论证） |
| DHCP 状态机隐蔽缺陷导致现场断网 | T1/T2/NAK/重放边界场景 | R9.2 全场景 + 故障注入；失败阈值降级可见；120 秒事务兜底 |
| 补偿顺序错误留下半配置 | 回滚后状态残留 | R9.3 逐步注入；补偿序在 design.md 成文并逐条对应测试 |
| 与 bonding child 的 `BondPlan` 契约漂移 | R7 原语与业务层类型不匹配 | D7 明确接线分支；契约以其 design.md 为准并在合并时对齐 |
| macOS cgo 引入构建矩阵复杂度 | 交叉编译、CI 覆盖缺口 | D9 双产物策略（Linux CGO_ENABLED=0 不变 + darwin CGO_ENABLED=1 变体）；cgo 文件只在 darwin 构建标签内 |
| macOS ServiceID/签名被外部变更 | 配置失配或误覆盖他人配置 | R8.1/R8.3 失配即停、signature 冲突拒绝覆盖；AC12 覆盖 |
| 专用 mac 手动验收不可重复 | 回归靠人肉 | R9.4 拆无副作用单测最大化自动化；手动项固化成 checklist 并留记录 |
| 08-22 遗留条目移交不清 | 双头认领或漏认领 | 归档时补记移交说明；本 PRD 已逐条列出继承范围 |

## 已确认决策记录（2026-08-23）

1. 任务定位：modes parent 第四 child（Task Map 与 parent D1 已同步增补）。
2. macOS 真实化纳入本任务范围，不推迟；08-22 R5.3 双平台语义承诺维持。
3. 排期与三个模式 child 并行；bond 原语（R7）优先级高于 macOS 里程碑。
4. 08-22 以「业务层 + fake 交付」收口归档，平台层遗留条目移交本任务（归档时补记）。

## Artifacts

- `prd.md`：本文件。
- `design.md`：待编写——rtnetlink 应用/补偿序、DHCP 状态机、Profile schema、
  漂移订阅、macOS cgo bridge 函数清单与 commit/apply 流程、netns 测试骨架、
  与 `BondPlan` 的对齐契约。
- `implement.md`：待编写——里程碑（建议顺序：Linux 基础 → bond 原语 → macOS →
  加固与部署物）与验证命令（含 runner 要求）。
- 无 `api.md`：HTTP 契约零变更；`supportedModes` 值变化是运行时数据而非 schema
  变更，前端已按 bonding child R2.4 泛化渲染。
