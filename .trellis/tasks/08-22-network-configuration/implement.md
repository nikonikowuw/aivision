# 网络配置服务实施计划

状态：`planning`；`task.py start` 前置条件：用户确认最新 `api.md` 与本规划摘要。

## 1. 执行顺序

### M0. 进入实现前的门禁

- [ ] 用户确认 `api.md` 的 endpoint、DTO、状态枚举、权限码和错误码。
- [ ] 记录确认后的 API 版本；若契约发生实质变化，重新运行 PRD 收敛并更新 design/implement。
- [ ] `python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-22-network-configuration`
  通过，且 `implement.jsonl`/`check.jsonl` 均存在真实条目。
- [ ] 仅在上述确认完成后执行
  `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-22-network-configuration`。
- [ ] 进入实现前读取 `.trellis/spec/backend/index.md`、`.trellis/spec/frontend/index.md`
  及 manifests 中的全部研究文件；确认当前工作区其他任务目录不被修改。

### M1. 配置、root 门禁和运行时生命周期

- [ ] 在 `app/internal/pkg/config` 增加网络状态目录、Linux Profile 位置等部署配置，
  同步 defaults、validate 和 `app/configs/config.yaml` 示例；不暴露 resolver/state 路径为
  HTTP 字段。明确原生 host-root 部署要求；现有 `app/Dockerfile` 的 `CGO_ENABLED=0` 和
  `deploy/docker-compose.yml` 的普通容器网络不纳入网络写入支持矩阵。
- [ ] 在 `main.go` 中把 root 检查放到 `config.Load` 后、`InitializeApp` 前；增加可单测的
  `requireRoot(euid)`，非 root 不监听端口。
- [ ] 扩展 `App`/Wire 产物，使 `NetworkService.Start` 在 schema ready 后、HTTP listener
  前调用，`Close` 在 HTTP shutdown 后调用；执行 `make wire`，不手改 `wire_gen.go`。
- [ ] 验证：root/non-root 启动单测或启动 smoke test；检查失败时没有监听端口；
  `go test ./internal/pkg/config ./cmd/api`（按实际包调整）。

回滚点：如果生命周期改动影响现有 API，先保留 NetworkService provider 但不注册网络写路由，
恢复 `App` 的显式关闭顺序后再继续。

### M2. 纯 Go 模型、校验器和状态存储

- [ ] 新建 `app/internal/pkg/netconfig` 的公共结构化类型、platform interface、fake、
  clock 和错误哨兵；实现完整 `HostPlan` 合并与主出口不变量。
- [ ] 实现 IPv4/prefix/gateway/DNS 校验、规范化和脱敏摘要；测试验证任何校验失败都不会
  调平台 Apply。
- [ ] 实现 root-only `factory`/`last-valid`/`pending` envelope、schemaVersion、SHA-256、
  generation、原子写入、fsync、锁和损坏检测；测试模拟写入中断、checksum 错误、未知版本、
  symlink、权限不安全和重复恢复。
- [ ] 实现 NetworkService 的 Idle → Prepared → Applying → PendingConfirmation →
  Committing/RollingBack/RecoveryFailed 状态机；用 fake platform 和临时时钟覆盖 120 秒、
  cancel、apply failure、进程重启恢复、全局单候选与主出口切换。
- [ ] 将 actor、source IP、目标接口、模式和脱敏摘要写入 pending 元数据，确保后台审计不
  依赖 HTTP context。
- [ ] 验证：`go test ./internal/pkg/netconfig ./internal/service`；运行 race 测试；
  检查测试没有调用真实 netlink、SystemConfiguration 或 shell。

回滚点：任何状态文件协议变化必须先增加 schemaVersion 和迁移/拒绝逻辑；不能删除或覆盖
已有 factory/last-valid 文件。

### M3. Linux 平台实现

- [ ] 引入并锁定 `github.com/vishvananda/netlink` 与 `github.com/insomniacslk/dhcp`，
  在 Linux build tag 下实现 Profile allowlist、永久硬件指纹、link/address/route 读取。
- [ ] 实现 DHCPv4 lease worker：DORA、T1/T2、renew、rebind、release、XID/MAC/server 校验、
  context 取消和链路变化；非主接口不安装默认 route/DNS。
- [ ] 实现精确地址/直连路由/默认路由补偿顺序；不 flush 全局对象；逐步失败都能幂等 Restore。
- [ ] 实现独占普通 `/etc/resolv.conf` 的 owner/symlink/readonly/托管检查、临时文件加
  fsync/rename；未知 resolver 管理器时 fail closed。
- [ ] 实现已知管理器 best-effort 检查和 rtnetlink 漂移事件；ownership conflict 使 API 只读。
- [ ] 验证：普通 Linux 编译/单测；在隔离 network namespace + veth + DHCP test server
  中运行特权契约测试，覆盖两接口主出口迁移、非主 DHCP、DNS、外部漂移、每个 Apply 步骤
  的失败注入和整体回滚。不得在开发机默认 namespace 测试。

回滚点：任何真实设备上的平台失败先关闭网络写权限和 DHCP worker，保留只读状态及 pending
恢复信息；禁止用 `ip`/`nmcli` 等命令作为临时 fallback。

### M4. macOS 平台实现

- [ ] 增加 `bridge_darwin.c/.h` 和 `manager_darwin.go` 的窄 C bridge；链接
  SystemConfiguration/CoreFoundation；增加 `manager_darwin_nocgo.go` 的显式 unsupported。
- [ ] 实现当前 `SCNetworkSet` 中 Ethernet/Wi-Fi Service 发现，记录 service ID、set ID、
  interface/BSD/MAC 指纹；service 删除重建和 set 漂移拒绝写入。
- [ ] 复制并修改 IPv4/DNS protocol 字典，执行 `SCPreferencesLock`、commit、apply；保存
  service order/primary override；使用 Dynamic Store 回读 IPv4、DNS、DHCP、PrimaryService。
- [ ] 实现 macOS 主出口切换与回滚，处理 lock busy、commit/apply 错误、外部 preference 修改、
  scoped resolver/route 不可控等情况；不调用 `networksetup`、`ifconfig` 或 shell。
- [ ] 验证：`CGO_ENABLED=1` 的 macOS 构建与 fake 契约测试；真实测试必须显式 build tag/环境
  变量、root、隔离 Network Service，并在退出/失败/中断后恢复原始快照。

回滚点：macOS C bridge 或 SDK 兼容性未达到门禁时，平台能力返回 unsupported，不能让业务层
退回文本命令或错误地宣称 commit 即生效。

### M5. API、权限、错误、审计和数据迁移

- [ ] 新建 `app/internal/service/network.go` 的输入/输出 DTO 转换和 `app/internal/api/network.go`；
  handler 不接触平台类型。
- [ ] 在 `router.go` 注册 overview、interface apply、transaction query/confirm/cancel、
  factory reset；GET 显式注册 `ops:network`，写路由注册 `ops:network:edit/confirm/cancel/reset`。
- [ ] 在 `errno.go` 增加网络错误码和 `zh-CN`/`en-US`/`zh-TW` 文案；在 `error_handler.go`
  映射参数错误、事务冲突、平台不可用和恢复失败的 HTTP 状态，保持 `{code,data,message}`。
- [ ] 扩展 Oplog `actionI18nMap` 和三语 action 文案；网络写路径不能沿用现有异步失败只
  warn 的 best-effort 记录，改为同步 `Record` 或等价可恢复 outbox，并验证最终状态、脱敏
  摘要和 actor 已落库后才返回。后台 rollback/startup recovery/lease 事件由 NetworkService
  显式写 OperationLogService。
- [ ] 按 `app/migrations/` 最新编号创建网络菜单 migration 的 up/down，并同步 `model/seed.go`；
  新库和既有升级库都得到相同菜单/角色绑定，不修改既有菜单用户的自定义字段。
- [ ] 更新 Wire provider、router deps 和 API 文档注释；执行 `make wire`。
- [ ] 验证：migration up/down 在临时数据库运行；路由权限测试覆盖读/写/confirm/cancel/reset
  默认拒绝；API 测试验证统一 response、非法接口 ID、pending 冲突和失败审计。

回滚点：若迁移或权限注册不完整，先撤销网络菜单/写权限并保留服务只读；不能通过绕过
PermMiddleware 让接口暂时可用。

### M6. 前端网络配置页面

- [ ] 新建 `ui/apps/web-antd/src/api/core/network.ts` 并从 `core/index.ts` 导出，类型与
  `api.md` 完全一致。
- [ ] 新建 `views/ops/network/index.vue`：接口列表/状态、DHCP/静态表单、主出口选择、
  变更摘要、断连警示、pending 倒计时、确认/取消、恢复失败和 ownership conflict 状态。
- [ ] 使用 `useVbenForm` + Zod；非主接口禁用网关/DNS；主出口切换显示旧接口降级和共享默认
  route/DNS 迁移；所有写按钮使用 `v-access:code`。
- [ ] 更新三语 `routes.json`、`system.json` 及网络错误/action 文案；不增加静态 route，依赖
  后端动态菜单 component。
- [ ] 验证：Vitest 覆盖 DHCP/static 字段联动、主出口切换摘要、倒计时、断线后 pending
  查询和错误状态；执行相关 `pnpm test:unit`、`pnpm check`。

回滚点：页面无法动态加载或翻译缺失时，先隐藏菜单并保留后端只读 API；不提交带硬编码文案
或绕过权限指令的 UI。

### M7. 集成验证与交付前检查

- [ ] Linux namespace 套件和 macOS 隔离 Service 套件通过；普通 CI 明确使用 fake。
- [ ] 运行 `cd app && make vet && make test`，再运行 `cd ui && pnpm check && pnpm test:unit`。
- [ ] 执行完整跨层回归：登录/RBAC → network overview → apply → reconnect/GET pending →
  confirm/cancel/timeout → audit log → restart recovery。
- [ ] 检查状态目录权限、静态文件暴露、日志脱敏、root 启动失败和 shell/路径输入注入；
  额外检查默认 Docker/Compose 不能误报 Linux/macOS 网络写入能力，目标部署必须提供 cgo
  （macOS）、宿主网络权限、Profile、resolver 所有权和 root-only state。
- [ ] 运行 `git diff --check`、Trellis validate 和最终 code/spec review；记录真实平台测试
  的内核、macOS 版本、测试 Network Service 与恢复结果。

## 2. 质量门禁

每个里程碑都必须同时满足：

- 修改只落在本任务和明确的产品代码路径，不触碰 `08-22-cpp-engine-skeleton-macos` 或
  `08-22-ntp-sync` 任务目录；
- 新增行为有 fake/纯单元测试，平台写入有隔离特权测试；
- 失败路径不返回 shell、路径、CoreFoundation 错误堆栈或完整内部快照；
- 主出口唯一、整机单候选、DHCP 回滚不承诺原 IP、factory 不可变等不变量有直接测试；
- 任何失败保持 fail closed：读可用时继续读，写入被拒绝并产生可查询审计/诊断状态。

## 3. 变更后的复核清单

- [ ] `design.md` 的接口和状态机与实际实现一致。
- [ ] `api.md` 的 DTO、权限、错误码与 Go handler、router、frontend API 一致。
- [ ] seed、migration、dynamic menu、三语文案和按钮权限没有漏项。
- [ ] Wire 重新生成且生成文件没有手工编辑。
- [ ] 所有真实网络测试有隔离环境和退出恢复；默认 `go test`/Vitest 不触碰宿主网络。
- [ ] 只有在用户确认最终结果后才进入 Trellis finish/commit 流程。
