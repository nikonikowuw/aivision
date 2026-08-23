# Linux/macOS 平台层真实化技术设计

状态：`draft`
对应 PRD：`prd.md`
基准代码：`dev` 分支 `e2ab18f` + bonding child M1 已落盘的类型扩展
（`types.go` 的 `NetworkMode`/`BondPlan`/`SupportedModes` 为未提交工作区状态）

## 1. 设计目标与边界

把 `manager_linux.go` / `manager_darwin.go` 中委托 `p.fake` 的写路径替换为真实实现，
同时保证：

1. **业务层零侵入**：`Platform` 接口签名不变（`Capabilities()` 由 bonding child M2
   引入，本任务直接对齐）；service 层唯一新增点是 `Start` 中的确认配置重放块（§10，
   无法避免——rtnetlink 配置不跨重启，重放决策需要同时访问 store 与平台）。
2. **补偿可证明**：rtnetlink 无跨对象原子性，每个应用步骤都有显式逆操作，全部被
   故障注入测试覆盖。
3. **fail closed**：任何前置条件不满足（root、Profile、resolver 所有权、内核能力）
   都拒绝写入并返回可诊断错误，绝不静默降级。

不在本设计内：模式切换业务逻辑（bonding child）、DHCP Server（gateway child）、
交换机侧 LACP 引导（lacp child）。

## 2. 包结构与构建标签

现状 `internal/pkg/netconfig/` 单包混放。真实化后拆分：

```
netconfig/
├── types.go platform.go state.go fake.go      # 不动（fake 继续服务业务层测试）
├── manager_linux.go        # //go:build linux   重写为装配入口
├── linux_profile.go        # //go:build linux   Profile 解析 + MAC 锚点
├── linux_read.go           # //go:build linux   netlink 枚举/读取
├── linux_apply.go          # //go:build linux   补偿序执行器
├── linux_dns.go            # //go:build linux   resolv.conf 原子写入器
├── linux_dhcp.go           # //go:build linux   租约生命周期管理器
├── linux_drift.go          # //go:build linux   rtnetlink 订阅漂移检测
├── linux_bond.go           # //go:build linux   bond/LACP 原语 + Probe 检测
├── manager_darwin.go       # //go:build darwin && cgo   重写
├── bridge_darwin.{c,h}     # 扩展（函数清单见 §12.1）
└── integration_test/       # //go:build linux   netns 特权集成测试
```

依赖新增：`github.com/vishvananda/netlink`、`github.com/vishvananda/netns`（仅测试）、
`github.com/insomniacslk/dhcp`。全部为纯 Go，Linux 主产物保持 `CGO_ENABLED=0`
可构建；darwin 变体需 `CGO_ENABLED=1`。

删除项：`NewLinuxPlatform` 内部构造的 fake 兜底实例（`manager_linux.go:23-26`）——
真实路径失败就返回错误，不再回落内存态；`Discover` 里 `len(list)==0` 回落 fake 的
分支同理。

**接口 ID 决策**：Linux 受管接口 ID 一律使用接口名本身（`eth0`），废弃现有
`"linux:" + name` 前缀。理由：与 FakePlatform 及 bonding child 的 `slaveIds`
语义一致；稳定性由 MAC 锚点保障而非 ID 编码。macOS 维持 ServiceID 作 ID。
首次以新 ID 启动时旧状态文件中无对应条目，走「首次接管」分支重建基线（一次性，
随部署升级说明告知）。

## 3. Linux Profile 与所有权

### 3.1 Profile schema（`/etc/aivision/network-profile.json`）

```json
{
  "version": 1,
  "interfaces": [
    { "name": "eth0", "comment": "uplink A" },
    { "name": "eth1", "comment": "uplink B" }
  ],
  "resolver": { "path": "/etc/resolv.conf", "requireExclusive": true }
}
```

- allowlist 是可写性的唯一事实来源；未列出的接口 `Writable=false`、`Ownership=
  OwnershipUnsupported`，只读展示。
- `requireExclusive=true` 时校验 `/etc/resolv.conf` 必须是普通文件（`Lstat` 非
  symlink、属主 root、非 world-writable），否则 Probe 失败。

### 3.2 MAC 锚点（`<state_dir>/anchors.json`）

首配按名称定位接口后，立即持久化 `{name → permanent MAC}`。后续每次 Probe/Read：
MAC 不匹配 → 该接口 `Ownership=conflict` 并拒绝写入（防插槽混淆）。虚拟接口
（veth/bridge/tun）通过 `/sys/class/net/<n>/device` 缺失 + `ARPHRD` 类型排除在
候选之外。

### 3.3 已知管理器检查（best-effort）

仅检查 NetworkManager：通过 `/run/NetworkManager/` pid 文件存在性 + D-Bus
`org.freedesktop.NetworkManager` 可达时查询设备 `Managed` 状态。不可达 ≠ 未托管，
结论只用于告警与 `Ownership=unproven` 标注；真正的防线是 §9 的漂移订阅。

## 4. Linux Read / Discover

单次 netlink dump 组装快照（`netlink.LinkList` + `AddrList` + `RouteList` +
`RouteListFiltered` 默认路由）：

- `LinkStatus` 取 carrier（`IFLA_CARRIER`），admin up 与 oper up 分开判断；
- 地址取 IPv4 元组列表；网关从默认路由表推导（只认本系统安装的 metric 范围）;
- `IPv4.Mode` 以状态存储的 last-valid 为准（运行态无法区分「DHCP 租约地址」与
  「恰好同值的静态地址」）；无记录时 `unknown`；
- `Fingerprint` = 对「受管接口集合 × 地址元组 × 默认路由 × resolv.conf 内容」的
  SHA-256，供既有 1110 漂移比对机制消费；
- `Native.Data` 存放精确元组 JSON（每接口已安装地址集、已安装路由、resolv.conf
  哈希），作为补偿序的事实来源（见 §5）。

## 5. Linux Apply 补偿序（核心）

`Apply(ctx, plan)` 执行前先 `Read` 校验指纹与调用方预期一致（不一致返回 1110 对应
错误，拒绝覆盖外部修改——复用既有语义），然后按下表顺序执行。每步成功后把逆操作
压入内存 undo 栈；任一步失败按逆序执行 undo 后返回 `ErrApplyFailed`。

| # | 步骤 | 操作 | 逆操作 |
| --- | --- | --- | --- |
| 1 | 链路 UP | `LinkSetUp`（原 admin down 才记 undo） | `LinkSetDown` |
| 2 | 地址替换 | 按 `Native` 记录逐元组 `AddrDel(old)`，再 `AddrAdd(new)` | 删新增、还旧删 |
| 3 | 直连路由 | 内核随地址自动维护，无需操作 | — |
| 4 | 默认路由迁移 | **先加新**（`RouteAdd` metric=100，dev=新主出口）→ **后删旧** | 删新加、还原旧删 |
| 5 | 非主出口清缺省 | 对受管非主出口 `RouteDel` 其默认路由（若有） | 还原 |
| 6 | DNS | §8 原子写入（primary 的 DNS 列表变化时） | 写回原文件内容 |

要点：

- 步骤 4 先加后删是刻意顺序：切换窗口内新旧默认路由并存（metric 区分），管理连接
  不断，对应 08-22 R2.9「尽量保留旧管理地址/链路」。
- 全部操作只携带 ifindex 与精确元组；不存在 flush、通配删除或接口名拼接。
- 崩溃一致性：service 层已在 `SetPending` 后才调 `Apply`，崩溃残留由启动恢复的
  `Restore(pending.Before)` 收敛（§10）；`Before` 即完整目标状态，undo 栈只是
  同步路径的优化。
- DHCP 接口：步骤 2/4 由租约管理器代执行（§7），`Apply` 只负责切换模式并等待
  首个租约（有界 10s，超时判 apply 失败触发整体回滚——诚实优于半生效）。

## 6. Linux Restore 幂等回滚

`Restore(ctx, snapshot)` 不走 undo 栈，而是**收敛式**实现：以 snapshot 为目标状态，
计算 diff 后执行与 §5 相同的原语（删多余地址、补缺失地址、默认路由收敛、DNS 写回、
DHCP 模式重启租约）。幂等由「diff 驱动 + 元组级操作」天然保证：重复执行第二次
diff 为空。超时回滚与启动恢复竞争同一 `Restore` 时，后者空转返回成功。

## 7. DHCP 租约生命周期（`linux_dhcp.go`）

```
Idle ──carrier up──▶ InitReboot ──ACK──▶ Bound ──T1──▶ Renewing ──ACK──▶ Bound
                        │NAK/超时          │  ▲T2─────────▶ Rebinding ──┘
                        ▼                  │NAK/到期
                     Discover ◀──重新DORA──┘
                        │OFFER→REQUEST→ACK
                        ▼
                      Bound
```

- 每受管接口一个协程，持有 `nclient4.Client`（`AF_PACKET` 绑 ifindex）；事件入口：
  Apply 模式切换、carrier 变化（复用 §9 订阅）、context 取消。
- **租约校验**（安装前）：地址合法单播、前缀合理、classless route 逐条解析校验；
  非法 → 拒绝安装、按指数退避重新 Discover。
- **安装动作**：静态路径同款原语——地址 + （若为主出口）默认路由 + DNS 写入；
  非主出口明确丢弃租约中的 router/DNS（08-22 R2.1）。
- **降级可见**：连续 3 次 DORA 失败 → 接口 `IPv4.Status=unavailable`、日志告警、
  继续退避重试；不影响整机其余部分。
- 与事务的关系：租约协程只被平台层调用；service 层无感知（`Read` 反映实际状态），
  120 秒窗口内续约/重绑属于日常运维动作，不算外部漂移（漂移比对排除本进程安装源，
  以 `Native.Data` 登记的元组为准）。

## 8. DNS 原子写入器（`linux_dns.go`）

唯一写入点（静态 Apply 步骤 6 与 DHCP 安装共用）：

1. 前置校验：`Lstat` 普通文件、root 属主；symlink/托管标记 → `ErrResolverConflict`。
2. 同目录临时文件写入 ≤3 条 `nameserver`（内容来自 primary 的 DNS 或租约 DNS），
   `chmod 0644`、`fsync`、`rename`。
3. 目录 fd `fsync` 保证 rename 持久。

## 9. rtnetlink 漂移订阅（`linux_drift.go`）

- `netlink.AddrSubscribe` + `RouteSubscribe`，过滤 ifindex ∈ 受管集合。
- 变更与本进程 `Native.Data` 登记的期望元组比对：外来变更 → 置原子 drift 标志 →
  后续 `Apply`/`Restore` 直接拒绝（错误映射到既有 1110 语义）→ overview 呈现
  `StateOwnershipConflict`。
- 解除路径（本轮，不新增端点）：① 外部变更消失（观测回归期望态）自动清除；
  ② 服务重启触发全量重放 reconcile，成功即清除。HTTP 显式接管端点列入 Deferred
  （需新权限码，超出「不新增端点」边界）。
- DHCP 自身动作（续约换地址）在登记簿内，不误报。

## 10. 启动序列与确认配置重放

既有 `Start`（`network.go:80-135`）五步之间插入一步，其余不动：

```
1 Init → 2 Probe → 3 Read → 4 工厂基线初始化
→ 4.5【新增】确认配置重放 reconcile
→ 5 pending 回滚（既有） → ready
```

reconcile（`service/network.go` 新增私有方法，~40 行）：

- `GetLastValid()` 为空 → 直接通过（全新部署）。
- 否则 `platform.Read` 当前态，与 `lastValid.Plan` 按接口比对（mode/addr/prefix/
  gateway/dns 任一不符即视为漂移）→ 不符则 `platform.Apply(lastValid.Plan)` 整体
  收敛；符合则跳过。
- Apply 失败 → 尝试 `Restore(factory.Snapshot)`（08-22 R6.5 两级恢复），仍失败 →
  接口标记 `StateRecoveryFailed` 并拒绝写入。
- 顺序理由：放在 pending 回滚**之前**会让刚恢复的 before 又被 last-valid 覆盖产生
  竞争；放在之后则语义为「回滚完成后向确认基线收敛」，且刚回滚的内容通常等于
  last-valid，reconcile 空转，代价一次 Read。

## 11. Bond 原语与能力检测（`linux_bond.go`）

### 11.1 原语（输入直接对齐 `types.go` 的 `BondPlan`）

```go
type BondPrimitives interface {
    BondCreate(plan BondPlan, bondName string) error      // IFLA_INFO_KIND=bond + MODE/MIIMON/PRIMARY
    BondDestroy(bondName string) error                    // LinkDel
    SlaveAttach(bondName, slaveName string) error         // slave IFLA_MASTER=bondIfindex
    SlaveDetach(slaveName string) error                   // IFLA_MASTER=0
    BondRead(bondName string) (*BondTopology, error)      // 含 ActiveSlaveID（IFLA_BOND_ACTIVE_SLAVE）
}
```

- 全部经 `netlink.LinkModify`/`netlink.Bond` 结构化属性，零 shell/sysfs 字符串。
- mode 4（802.3ad）属性族同批交付（AD_SYS_PRIORITY 等）， lacp child 只消费。
- bond 名固定 `bond0`（与 bonding child D1 一致），创建前校验名未被占用。

### 11.2 Probe 能力检测

1. `unix.Prctl(CAP_GET...)` 或等效 capset 自查 `CAP_NET_ADMIN`+`CAP_NET_RAW`；
2. bonding 模块可用性：`/sys/module/bonding` 存在，缺失时尝试 ` AF_ALG` 式内核
   自动加载不可行——记录 `supportedModes` 摘除原因，fail closed；
3. 内核版本 ≥ 4.x 下 `IFLA_BOND_*` 属性全集按版本表裁剪（design 附录维护最小版本
   矩阵：active-backup ≥ 3.x、802.3ad sys priority ≥ 4.x）。

检测结果供 `Capabilities()`（bonding child M2 引入的方法）动态声明
`supportedModes`；接线归属按合并顺序（PRD D7）。

## 12. macOS SystemConfiguration 实现

### 12.1 bridge_darwin.h 函数清单（增量）

```c
// 已有
int  sc_get_services(sc_service_info_t**, int*);
void sc_free_services(sc_service_info_t*);

// 新增：读取
int sc_get_prefs_signature(char out_sig[65]);
int sc_get_ipv4(const char* svc_id, sc_ipv4_t* out);        // method/addresses/masks/router
int sc_get_dns (const char* svc_id, sc_dns_t* out);         // servers[]
int sc_ds_read(const char* svc_id, sc_state_t* out);        // DynamicStore 实际态 + is_primary
void sc_free_...(各结构);

// 新增：写入（仅修改字典，不创建 service）
int sc_set_ipv4(const char* svc_id, int method, ...);       // DHCP=0 / Manual=1
int sc_set_dns (const char* svc_id, const char** servers, int n);
int sc_set_service_order(const char** svc_ids, int n);      // 主出口迁移
int sc_commit_and_apply(void);                              // CommitChanges+ApplyChanges 原子调用对
int sc_lock_prefs(void);  int sc_unlock_prefs(void);        // 有界重试
```

约束：结构体定长数组 + C 内存分配/释放成对；Go 侧永不持有 CF 引用；每个函数
返回 `SCError` 映射码（映射表集中在 `bridge_errors.go`）。

### 12.2 Apply 流程（`manager_darwin.go` 重写）

```
锁 SCPreferences(重试×3)
→ 深拷贝原 IPv4/DNS 字典 + signature + service order（存入返回快照 Native.Data）
→ sc_set_ipv4 / sc_set_dns（目标 protocol 缺失 → fail closed）
→ （主出口变化）sc_set_service_order
→ sc_commit_and_apply
→ 轮询 sc_ds_read 直到期望值出现（deadline 15s）
→ 超时/失败 → 返回 ErrApplyFailed（service 触发 Restore）
```

### 12.3 Restore 流程

读当前 signature 与 Native 中登记的一致（否则报冲突拒绝覆盖）→ 按原字典
set → commit+apply → Dynamic Store 验证。幂等：字典级 set 天然幂等。

### 12.4 主出口

事实来源 = Dynamic Store `PrimaryService`；迁移手段 = `SCNetworkSetSetServiceOrder`
把目标 service 置顶。不承诺消除 scoped route/resolver（research 既定口径）。

## 13. 测试设计

### 13.1 Linux netns 集成套件（`integration_test/`）

- 环境 guard：`//go:build linux` + 运行时检查（root 且 `NETCONFIG_NETNS_TEST=1`，
  否则 `t.Skip` 并输出显式原因行供 CI 断言）。
- 骨架：程序化 `netns.New()` + veth 对 + 把测试进程之一线程迁入 ns
  （`vishvananda/netns`），DHCP server 用 `insomniacslk/dhcp server4` 绑 ns 内接口。
- 用例 ↔ AC 映射：AC1 静态往返 / AC2 租约全周期（NAK、双 Offer、T1 快进——时钟注入）
  / AC3 外部注入地址（测试内裸 netlink 写）→ drift / AC5 杀进程模拟崩溃 → 重启重放
  / AC6 bond 原语 / AC7 逐故障注入（netlink 调用包装层注入点）。
- 时钟：租约计时器经接口注入 `clock`，测试快进 T1/T2 不真等。

### 13.2 单测与手动

- fake 平台用例零改动跑绿 = 业务层零侵入证明（AC8）。
- macOS：bridge 字典转换/错误映射拆为无副作用纯函数直测；commit/apply 端到端按
  PRD R9.4 checklist 在专用 mac 手动执行并留档（AC12/AC13）。

## 14. 部署物与构建矩阵

| 产物 | 构建 | 说明 |
| --- | --- | --- |
| Linux api 二进制 | `CGO_ENABLED=0 GOOS=linux` | 现状不变 |
| macOS daemon | `CGO_ENABLED=1` launchd plist | 新增；文档给 LaunchDaemon 样例（root、KeepAlive） |
| systemd unit | `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW` + `User=root` | 替代裸 root 运行的加固选项 |
| Profile 样例 | `deploy/network-profile.example.json` | 随部署文档 |

容器形态不动（08-22 R5.9）；CI 增加 privileged job 跑 netns 套件。

## 15. 风险与开放问题

| 风险 | 缓解 |
| --- | --- |
| `Start` 插入 reconcile 是对「业务层零改动」承诺的唯一破例 | §10 已论证必要性；改动为纯新增私有方法，既有五步控制流不动 |
| netlink 订阅在 ns 销毁时的句柄泄漏 | 订阅 goroutine 绑 context，Close 级联退出（集成用例覆盖） |
| macOS `SCPreferencesLock` 死锁/忙 | 有界重试 ×3 后放弃本次事务，不阻塞其他接口 |
| bond 属性版本矩阵不准 | Probe 版本表保守起步，目标机验收（AC9/AC10）实测修订附录 |
| 接口 ID 从 `linux:name` 改为 `name` 造成旧状态失配 | 一次性走首次接管重建基线；部署说明标注 |

Open Questions：无（四项定位/范围/排期/收尾决策已于 2026-08-23 由用户确认，
记录于 prd.md「已确认决策记录」）。
