# Linux/macOS 平台层真实化：实施前基线与契约证据

记录日期：2026-08-23
用途：为本任务的设计修正、实施分批与 sub-agent 上下文提供可复现证据。

## 1. 已继承的研究结论

08-22 的原始研究已随任务归档，以下文件仍是本任务的技术依据：

- `.trellis/tasks/archive/2026-08/08-22-network-configuration/research/linux-network-stack.md`
  规定 Linux 使用 rtnetlink、`insomniacslk/dhcp` 的 `nclient4`、独占 resolver 文件、
  Profile allowlist 与 netns 特权集成测试。
- `.trellis/tasks/archive/2026-08/08-22-network-configuration/research/macos-systemconfiguration.md`
  规定 macOS 使用 SystemConfiguration，区分 preferences 与 Dynamic Store，且 cgo
  bridge 不能泄漏 CF 对象。
- `.trellis/tasks/archive/2026-08/08-22-network-configuration/research/repository-integration.md`
  规定平台实现保留在 `internal/pkg/netconfig`，网络服务拥有候选事务生命周期；默认
  容器不支持 host 网络写入。

这些是事实来源，不复制到本文件以避免两份规范漂移。

## 2. 当前代码基线（`dev` @ `353be48`）

- `app/internal/pkg/netconfig/manager_linux.go` 的 `Read`、`Apply`、`Restore` 仍委托
  `FakePlatform`；`Discover` 使用 `net.Interfaces()`，且真实路径会在接口枚举失败时
  回落 fake。因此 `fake_platform=false` 不是安全的真实平台模式。
- `app/internal/pkg/netconfig/manager_darwin.go` 只经 bridge 枚举 service，实际状态读取
  使用 `net.Interface`；其 `Read`、`Apply`、`Restore` 同样委托 fake。现有
  `bridge_darwin.{c,h}` 只有 `sc_get_services`/`sc_free_services`。
- `app/internal/pkg/netconfig/factory.go` 的构造函数只接收 `profilePath` 与 fake 开关，
  但需求要求永久 MAC 锚点写入 `state_dir`。实现必须把 `cfg.Network.StateDir` 显式注入
  平台构造函数，不能新增全局路径或把锚点误放到 Profile 旁。
- `app/internal/service/network.go` 当前记录并忽略 `Probe` 和初始 `Read` 的错误；真实化后
  Profile、权限或初始读失败必须在监听前停止启动。pending/last-valid 恢复失败则保留证据、
  以可诊断只读状态 fail closed，而非清空 pending 后伪造 ready。
- `HostPlan.Bond`、`BondTopology`、LACP DTO 与 `Capabilities.SupportedModes` 已在
  `types.go`；active-backup/LACP 业务层把 `Platform.Apply` 视为唯一的目标最终状态入口。
  平台层不得引入第二个模式切换通道。
- `ErrOwnershipConflict`、`ErrExternalDrift`、`ErrUnsupported`、`ErrApplyFailed` 已在
  `validator.go`，对应业务码 1105、1110、1106、1107/1114 已存在。真实平台必须保留
  原因链，由 service 的单一私有映射点转换，不能一律报 1107。
- `app/go.mod` 已因网关 child 间接包含 `github.com/insomniacslk/dhcp`；本任务实际直接
  使用后应由 Go modules 提升为直接依赖。`vishvananda/netlink` 与仅测试的 `netns` 尚未
  声明。

## 3. 上游 API 复核

- `vishvananda/netlink` 的 `LinkAttrs` 公开 `PermHWAddr`，可满足永久 MAC 锚点；当它为空
  时不能退化为可变的 `HardwareAddr`。
- 同库提供 `AddrAdd`/`AddrDel`、`AddrSubscribe`，并接受完成通知的 done channel。实现应
  把订阅和 DHCP worker 绑定到平台 `Close` 的统一 context，避免 namespace 测试和进程退出
  时遗留 goroutine 或 netlink socket。
- `netlink.Bond` 已有 `Mode`、`Miimon`、`Primary`、`XmitHashPolicy`、`LacpRate`、
  `AdInfo` 等结构化字段；输入仍必须先由现有封闭 `BondPlan` 枚举映射，不能接受任意
  kernel/sysfs 字符串。

上游源依据：

- `https://github.com/vishvananda/netlink/blob/main/link.go`
- `https://github.com/vishvananda/netlink/blob/main/addr_linux.go`

## 4. 规划修正结论

1. `design.md` 的基准已改为 `353be48`，不再把已提交的 bond/LACP 类型称为未提交工作区。
2. `NewPlatform` 会获得 `stateDir` 注入；相关 Linux/non-Linux fallback、测试构造点一并
   更新。
3. 启动顺序固定为 pending 回滚先于 last-valid reconcile；`Probe`/初始 `Read` 为启动硬
   失败，恢复失败保留诊断并禁止写入。
4. 漂移通过既有 `InterfaceInfo.Ownership` 和 `StateOwnershipConflict` 可见，写路径复用
   1105/1110，不新增 HTTP 端点、错误码、权限或前端 schema。
5. 已归档 gateway child 的真实 Linux 能力依赖本任务：基础 Probe 成功后必须声明
   `NetworkModeGateway`，而 bond 模式仍由各自的 netlink 属性/状态探测独立开放。
6. bond 模式以 `HostPlan.Mode` 为唯一事实来源；LACP 历史计划缺失 `Miimon` 时由平台
   规范化为既有 `DefaultBondMiimon`，未出现在 `BondPlan` 的高级属性保持内核默认值。

## 5. 已验证的 M0 阻塞与构建矩阵

- `app/internal/pkg/netconfig/netconfig_test.go:622` 有已提交的裸 `=======`；因此
  `cd app && go test ./internal/pkg/netconfig` 当前在编译前即报
  `expected declaration, found '=='`。这是 `722c694` 合并后留下的单一冲突标记，不是本次
  调研产生的工作区修改。
- Linux 选择 `gateway_linux.go` 时已实际导入 `dhcpv4`/`nclient4`。执行 Dockerfile 同款
  `CGO_ENABLED=0 GOOS=linux go build ./cmd/api` 当前失败，因为 `go.sum` 缺少
  `github.com/u-root/uio/uio`、`github.com/u-root/uio/rand`、
  `github.com/mdlayher/packet` 的 checksum。M0 必须先执行 `go mod tidy` 并重新验证三个
  command build；它会将已使用的 DHCP 提升为直接依赖。
- `app/Dockerfile` 的生产矩阵是三个独立 command build：`api`、`migrate`、`bootstrap`，
  都是 `CGO_ENABLED=0 GOOS=linux`。常规测试不复用这一环境：多个测试包导入
  `gorm.io/driver/sqlite`，其依赖 `mattn/go-sqlite3`，因此 `make test` 必须在具备本机
  cgo/SQLite 的环境运行。
- `go list` 已验证 Linux `CGO_ENABLED=0` 忽略 `manager_darwin.go` 与
  `bridge_darwin.c/.h`；Darwin 真实路径仅通过 `darwin && cgo` 的 Go 文件带入 C bridge。
  macOS bridge 的编译/无副作用测试必须在原生 macOS（SDK + C compiler）执行，不能把
  `GOOS=darwin` 的非原生交叉测试视为完成。
- 仓库当前没有项目自有 CI workflow，`app/Makefile` 只有常规 `build`/`test`/`vet`。
  本任务必须提供 `make test-netconfig-integration`；只有用户确认存在具备 root、
  `CAP_NET_ADMIN`、`CAP_NET_RAW` 的 self-hosted Linux runner 后，才新增并启用对应 CI job。

## 6. 未在本机宣称完成的验证

- macOS SystemConfiguration 的真实 commit/apply/rollback 需要专用 root macOS 主机。
- netns/veth/DHCP/bond 测试需要具备 `CAP_NET_ADMIN` 与 `CAP_NET_RAW` 的 Linux runner。
- 真实物理 NIC、NetworkManager 冲突和交换机 LACP 协商需要目标机/台架；正常 `make test`
  只能覆盖 fake 与无副作用单测。
