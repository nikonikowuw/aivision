# Linux/macOS 平台层真实化实施计划

状态：`planning`
对应：`prd.md`、`design.md`、`research/baseline-and-contracts.md`

## 1. 实施边界与启动门禁

本任务只实现 `app/` 中的真实平台层及其必要的 service 启动/错误映射接线。HTTP 端点、
JSON schema、权限、errno、菜单 migration 与前端不变；因此不创建 `api.md`，也不需要
前后端契约确认。

- [ ] 本规划摘要经用户显式确认后，执行
      `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-23-linux-platform-realization`。
- [ ] 以开始时的 `dev` HEAD 重新核对 `08-23-lacp-aggregation` 是否带来未合并的
      `BondPlan`/DTO 变动；以已提交的 `types.go` 为唯一代码契约。
- [ ] 在 Linux 实现首次落盘前，锁定兼容的 `vishvananda/netlink` 版本；将实际直接使用的
      `github.com/insomniacslk/dhcp` 提升为直接依赖，`netns` 保持测试专用依赖。
- [ ] 任何一个真实平台 Probe 失败时保持 capability fail closed；不得回落
      `FakePlatform` 或伪造空快照。

**开始验证**：`task.py current --source` 指向本任务，`task.json.status` 为
`in_progress`，两个 JSONL context manifest 均可通过 `task.py validate`。

## 2. 里程碑

每个里程碑单独提交、单独通过其验证点后再进入下一项。真实系统写入只在 Linux netns 或
专用 macOS 主机执行；普通单测不得触碰开发机网络。

### M0. 基线完整性与构建矩阵

目标文件：

- `app/internal/pkg/netconfig/netconfig_test.go`
- `app/go.mod`、`app/go.sum`
- `app/Makefile`
- 仅在用户确认有合格 runner 时新增项目 CI workflow

工作项：

- [ ] 删除 `netconfig_test.go:622` 唯一的已提交 `=======` 冲突标记；不改相邻 LACP 或
      gateway 测试语义。该修复先单独提交，恢复可解析的 netconfig 测试基线。
- [ ] 对已实际导入的 DHCP 执行 `go mod tidy`，使其成为直接依赖并写入 `uio`、`packet`
      等传递模块 checksum。`vishvananda/netlink` 在 M1 首次引入生产代码时以已审阅版本
      加入模块；`netns` 仅随 integration test 加入。
- [ ] 新增 `make test-netconfig-integration`，唯一执行
      `NETCONFIG_NETNS_TEST=1 sudo -E go test -tags=netconfig_integration ./internal/pkg/netconfig/...`；
      普通 `make test` 不带 tag，继续使用宿主机 cgo/SQLite 测试环境。
- [ ] 在已有合格 self-hosted privileged Linux runner 时，新增最小项目 CI workflow 调用
      上述 target；没有 runner 时只写外部 pipeline 接入说明，并保持 AC1–AC7 未完成。
- [ ] 验证 Darwin cgo bridge 的 C/H 文件只经 `darwin && cgo` 的 manager 路径参与构建，
      Linux `CGO_ENABLED=0` 选择忽略它们；不因无效担忧添加多余 C build tag。

验证：

```bash
cd app
go test ./internal/pkg/netconfig/...
make vet
make test
CGO_ENABLED=0 GOOS=linux go build -o /tmp/aivision-api ./cmd/api
CGO_ENABLED=0 GOOS=linux go build -o /tmp/aivision-migrate ./cmd/migrate
CGO_ENABLED=0 GOOS=linux go build -o /tmp/aivision-bootstrap ./cmd/bootstrap
```

**回滚点**：M0 只恢复既有测试可解析性、模块完整性和可选测试入口；不改变网络运行态、
HTTP 契约或默认容器权限。

### M1. Linux 安全边界、构造与真实读取

目标文件：

- `app/internal/pkg/netconfig/factory.go`
- `app/internal/pkg/netconfig/manager_linux*.go`
- 新增 `linux_profile.go`、`linux_read.go` 与聚焦单测
- `app/internal/service/network.go` 与相关 service 测试

工作项：

- [ ] 将 `stateDir` 从 `networkService` 注入平台构造函数，同步所有 fallback 和测试构造点；
      在 state dir 建立 root-only、原子写入的 `anchors.json`。
- [ ] 定义并严格解析版本化 Profile：allowlist 是唯一可写性依据，resolver 路径与独占要求
      必须显式存在；缺失、畸形、非 root、缺少必要 capability 或 resolver 所有权不满足时
      `Probe` 返回错误。
- [ ] 删除 Linux/Darwin 真实路径中的 fake 回退。`Start` 对真实 `Probe` 或初次 `Read`
      错误直接失败，确保 API 不会开始监听。
- [ ] `Capabilities()` 在基础 Probe 成功后声明 `multi-address` 与 `gateway`；gateway 的
      DHCP socket/冲突探测继续由既有 `GatewayRuntime` 在模式提交时执行。bond 模式仍由 M3
      的独立属性/状态探测决定，Darwin 始终不声明 gateway。
- [ ] 使用 netlink 枚举链路、IPv4、直连/默认路由、carrier/admin/oper 状态、速度与双工；
      受管物理接口 ID 统一为接口名。未列 allowlist 或虚拟接口只读展示。
- [ ] 首次接管仅在 `PermHWAddr` 可用时落盘永久 MAC；后续读到不匹配时标记
      `OwnershipConflict` 并拒绝写入。`HostSnapshot.Native` 保存精确地址、路由、DNS hash
      和版本化平台事实。

验证：

```bash
cd app
gofmt -w internal/pkg/netconfig/*.go internal/service/network.go
go test ./internal/pkg/netconfig/... ./internal/service/...
make vet
```

新增用例：Profile 的拒绝场景、MAC 锚点首次/失配、allowlist 可写性、netlink 数据到 DTO 的
转换、真实平台绝不调用 fake、`Start` 在 Probe/Read 失败时不进入 ready，以及真实 Linux
在基础 Probe 成功后开放 `gateway`、在失败或 Darwin 下保持关闭。

**回滚点**：本阶段不写地址、路由或 DNS；revert 仅恢复构造和读取行为。

### M2. Linux 静态 Apply/Restore、DNS 与错误映射

目标文件：

- 新增 `linux_apply.go`、`linux_dns.go`、可注入的低层 netlink/DNS 操作测试 seam
- `manager_linux.go`、`validator.go`（仅复用既有 sentinel）
- `app/internal/service/network.go` 与测试

工作项：

- [ ] 按 Native 快照执行收敛式、ifindex 和精确元组级的静态配置：链路 up、地址替换、
      新默认路由优先、旧路由清理、DNS。禁止 flush、通配删除、`os/exec` 和 sysfs 写入。
- [ ] 每个成功步骤压入逆操作；失败按相反顺序补偿。`Restore` 基于目标快照重新计算 diff，
      重复执行应为空操作成功，不依赖瞬态 undo 栈。
- [ ] 仅由一个 DNS writer 修改 resolver：检查普通文件/属主/权限，生成最多 3 个
      nameserver，临时文件 `fsync`、rename、目录 `fsync`；symlink、只读或托管冲突失败关闭。
- [ ] 将 `ErrOwnershipConflict`、`ErrExternalDrift`、`ErrUnsupported`、普通 Apply 失败和
      LACP 内核拒绝映射到既有 1105、1110、1106、1107、1114。映射逻辑只能有一个私有
      service helper，避免 Apply/Cancel/Timeout 路径漂移。
- [ ] 保持现有 pending 持久化先于 Apply 的事务边界；平台自身出错不会清掉诊断证据。

验证：

```bash
cd app
go test ./internal/pkg/netconfig/... ./internal/service/...
make vet
make test
```

新增用例：逐步故障注入的补偿顺序、重复 Restore、DNS 普通文件/symlink/只读、默认路由
迁移顺序、现有 errno 映射和 fake 业务层用例无断言变更。

**回滚点**：该阶段必须在合并前具备 Linux netns 验证；真实配置异常时以
`Restore(before)` 收敛，代码回滚按提交粒度进行。

### M3. Bond/LACP rtnetlink 原语与能力协商

目标文件：

- 新增 `linux_bond.go` 与 Linux 单测/特权集成测试
- `manager_linux.go`、`types.go`（仅在当前 DTO 缺少平台回读信息时最小补充）
- 相关 service 测试

工作项：

- [ ] 以最终 `HostPlan.Mode`/`HostPlan.Bond` 为唯一输入，实现 multi-address、
      active-backup、LACP 之间的 teardown/create/attach/detach 收敛；不引入第二个
      Platform 方法或模式事务。mode 与 bond 参数不匹配在平台边界拒绝。
- [ ] 使用 `netlink.Bond` 和 slave master 属性设置 active-backup 的 mode/miimon/primary，
      以及 LACP 的 mode 4、封闭 hash policy、slow rate。LACP 计划中缺失的 `Miimon` 在
      平台内部规范化为 `DefaultBondMiimon`；不扩展 `BondPlan` 或暗中设置未建模的
      AD_SYS_PRIORITY 等属性。bond 名固定 `bond0`，碰到非本系统创建的同名接口失败关闭。
- [ ] 从结构化 netlink 属性回读 `BondTopology`、active slave、LACP aggregator/actor/
      partner 状态、速度和双工；禁止解析 `/proc/net/bonding`。
- [ ] `Probe` 对必要权限、bond 模块和实际可用属性做保守探测；只有完整的
      active-backup 或 LACP 契约才分别将对应模式加入 `SupportedModes`。探测不确定时仅关闭
      对应 bond 模式；基础 Probe 已成功时必须保留独立的 `gateway` capability。
- [ ] LACP 对端未协商是可观察状态而不是 Apply 失败；内核拒绝 mode/属性保留 LACP
      sentinel，以便 service 映射 1114。

验证：

```bash
cd app
go test ./internal/pkg/netconfig/... ./internal/service/...
make vet
```

Linux 特权验证：mode=1 bond 创建/两 veth slave 绑定/拆除归还；mode=4 的属性映射、
Probe fail-closed、`HostPlan.Mode` 与 `BondPlan` 不匹配拒绝、LACP 缺失 miimon 时的固定
默认值，以及 Read DTO。交换机协商与断链切换留给 M6 台架项。

**回滚点**：任何 capability 或属性读取不完整时关闭对应模式，不以 fake 行为掩盖。

### M4. Linux DHCP、漂移订阅与启动恢复

目标文件：

- 新增 `linux_dhcp.go`、`linux_drift.go`
- `manager_linux.go`、`app/internal/service/network.go` 及测试

工作项：

- [ ] 为每个受管 DHCP 接口实现可取消 worker：INIT-REBOOT、DORA、T1 renew、T2 rebind、
      NAK/到期清理和有界退避；安装租约前验证地址、网关、DNS、classless route 与
      server/client/XID 关联。
- [ ] DHCP 地址/路由/DNS 经 M2 的同一精确操作原语安装；非主出口不安装默认路由或全局 DNS。
      连续失败以结构化 zap 日志和实际 `IPStatusUnavailable` 可见。
- [ ] 订阅 address/route/link 变化，排除本进程登记的变更；外来变化将受影响接口标为
      `OwnershipConflict`，overview 为 `StateOwnershipConflict`，所有写路径返回 1110。
- [ ] `Close` 取消 DHCP 和订阅 goroutine 并等待退出。启动严格执行：Init → Probe → Read →
      factory → pending Restore → last-valid reconcile → subscriptions → ready。
- [ ] pending 或 last-valid 恢复失败时保留状态文件并阻止写入；若仍能读取则提供
      `StateRecoveryFailed` 诊断，若不可读取则启动失败。

验证：

```bash
cd app
go test ./internal/pkg/netconfig/... ./internal/service/...
make test-netconfig-integration
make vet
make test
```

新增用例：DORA/T1/NAK/到期、非法租约、carrier/context 取消、外部地址/路由漂移、
boot ID 变化、pending 优先回滚、confirmed static/DHCP 重放和 worker 关闭。

**回滚点**：netns 套件故障或租约状态不确定时，停止受管 DHCP worker、删除本系统登记的
租约元组并保留其它网络状态；不尝试全局网络 reset。

### M5. macOS SystemConfiguration 真实路径

目标文件：

- `app/internal/pkg/netconfig/bridge_darwin.h`
- `app/internal/pkg/netconfig/bridge_darwin.c`
- `app/internal/pkg/netconfig/manager_darwin.go`
- 新增 Darwin 纯函数/错误映射测试和专用手动验收 checklist

工作项：

- [ ] 扩展窄 C bridge：service 枚举、完整 IPv4/DNS 字典读取与写回、preferences signature、
      service order、Dynamic Store 实际状态、lock/commit/apply。Go 不接收未托管 CF 对象，
      C 不接受命令文本、HTTP 输入或长期存活的 Go 指针。
- [ ] 只操作已有 Ethernet/IEEE80211 service；ServiceID、service/interface 指纹或
      preferences signature 不匹配时停止写入。不能按名称猜测、创建 service 或调用 shell。
- [ ] Apply 遵循锁定 → 复制完整旧字典/签名 → set → commit → apply → Dynamic Store 验证；
      失败/取消/超时/启动 pending 以旧字典收敛，回滚前验证外部没有改动 preferences。
- [ ] DHCP 仅切换 macOS ConfigMethod 并回读系统 lease；不复用 Linux DHCP worker。
- [ ] `darwin && !cgo` 构建继续明确 unsupported；Darwin capability 固定只声明
      `multi-address`。

验证：

```bash
cd app
# 仅在原生 macOS 且 Xcode/Command Line Tools 可用时执行；不能作为 cgo 跨平台测试。
CGO_ENABLED=1 go test ./internal/pkg/netconfig/...
CGO_ENABLED=1 go build -o /tmp/aivision-darwin-api ./cmd/api
make vet
make test
```

专用 root macOS 手动验收：Ethernet/Wi-Fi DHCP 与 Manual、DNS、主出口 service order、
动态状态验证、超时回滚、重启一致性、外部 service 删除重建冲突。没有该机器时只能标记
AC12/AC13 未验证。

**回滚点**：每次真实 macOS Apply 已保存完整原字典；测试或验证失败先用 bridge Restore，
再终止服务，绝不以 preferences 成功代替实际状态成功。

### M6. 特权集成、部署物与全量验收

目标文件：

- Linux `netconfig_integration` build-tag 集成测试
- `app/Makefile` 的 `test-netconfig-integration` target
- 仅在用户确认 runner 可用时新增项目 CI workflow
- `deploy/` 下 systemd unit、Profile 样例与 macOS launchd 操作说明
- 任务验收记录

工作项：

- [ ] 建立 `netconfig_integration` Linux test tag 与 `make test-netconfig-integration`：只有
      root 且 `NETCONFIG_NETNS_TEST=1` 时创建 netns/veth/DHCP server/bond；其他环境明确
      `Skip` 原因，普通 `make test` 保持无特权。
- [ ] 覆盖 AC1–AC7：静态往返、补偿、DNS、DHCP、漂移、重放和 bond。用户确认有合格
      self-hosted runner 时，本任务新增 CI job 调用该 target 并作为自动门禁；否则写明外部
      pipeline 接入方式，AC1–AC7 保持未勾选。
- [ ] 交付 host-root systemd unit（`CAP_NET_ADMIN`、`CAP_NET_RAW`）与严格 Profile 样例；
      默认容器继续 fail closed，不能新增 NET_ADMIN、host network 或 resolver mount 来伪装支持。
- [ ] 记录 Linux 目标机 AC9/AC10、bond active-backup AC11、LACP child 台架项和 macOS
      AC12/AC13 的实际结果；无设备的项目保持未勾选。
- [ ] 针对本任务新增/改动 Go 文件运行 `gofmt`，确认 `gofmt -l .` 无输出，再执行全量
      `make vet`、`make test`。Linux 产物按 Dockerfile 同款分别验证 `api`、`migrate`、
      `bootstrap` 三个 `CGO_ENABLED=0 GOOS=linux` command build。

**回滚点**：部署物不改变默认容器；若目标机探测失败，服务不监听或将平台能力关闭，仍可用
`fake_platform=true` 执行业务层测试，不能把它作为生产降级路径。

## 3. 最终质量门禁

- [ ] `cd app && make vet && make test` 全绿（宿主机 cgo/SQLite 测试环境）。
- [ ] Linux 特权 runner：`cd app && make test-netconfig-integration` 全绿；若无已确认的
      runner，该 target 与外部 pipeline 接入说明存在，但 AC1–AC7 保持未勾选。
- [ ] Linux 容器产物：按 Dockerfile 同款分别完成
      `CGO_ENABLED=0 GOOS=linux go build ./cmd/api`、`./cmd/migrate`、`./cmd/bootstrap`。
- [ ] macOS：仅原生 macOS 上 `CGO_ENABLED=1 go build ./cmd/api` 与无副作用
      bridge/manager 单测全绿；手动项目有验收记录或明确未验证。
- [ ] `go.mod`/`go.sum` 只含真实使用的直接依赖；没有新增 `os/exec`、shell 调用、sysfs 字符串写入、HTTP 端点、权限码或 errno。
- [ ] fake 平台与既有 active-backup/LACP/gateway service/API 测试未因真实化而改变语义。
- [ ] PRD AC1–AC14 按实际 CI、目标机、台架或 macOS 结果更新，不把无法运行的特权/硬件检查标为通过。

## 4. 实施期复核点

- 每次碰到 `Platform`、`HostPlan`、`HostSnapshot` 或 `InterfaceInfo` 时，对照
  `.trellis/tasks/08-23-lacp-aggregation/design.md` 的平台接线契约，确保没有破坏已交付
  的 fake/前端 DTO。
- 每次增加路径、常量、重试或状态时先搜索现有定义；超时、退避阈值、错误映射和状态标记
  都必须有唯一所有者。
- 发现平台真实化需要新增公开 HTTP 契约、权限、错误码或支持默认容器时，停止实施并回到
  Phase 1 更新 PRD/design，重新取得确认。
