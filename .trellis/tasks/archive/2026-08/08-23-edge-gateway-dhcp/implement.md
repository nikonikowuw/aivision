# 边缘网关模式实施计划

状态：`draft`（等待 API 契约和规划摘要确认）  
对应设计：`design.md`

## 1. 进入实现前门禁

- [ ] `08-22-network-configuration` 已完成并归档，且其 `Platform`/状态存储契约不再移动。
- [ ] `08-23-active-backup-bonding` 的实现与质量检查已通过；模式框架、capability、pending 事务和前端确认交互可复用。
- [ ] `github.com/insomniacslk/dhcp` 的版本已验证同时提供 `dhcpv4` 与 `server4`，并锁入 `app/go.mod` / `go.sum`。
- [ ] 本 child 的 `api.md` 状态改为 `confirmed`，用户确认 gateway 请求体与 overview 租约投影。
- [ ] 任务状态由 planning 切为 in_progress；未执行 `task.py start` 前不得修改 `app/` 或 `ui/` 产品代码。

验证点：`python3 ./.trellis/scripts/task.py current` 指向本 child；`task.py validate` 通过；前置 child 状态与代码提交可追溯。

## 2. 执行顺序

### M1. 模型、状态与纯校验

- [ ] `netconfig/types.go` 增加 `NetworkModeGateway`、`GatewayPlan`、`GatewayState`、`GatewayLease`、`GatewayOverview`，并把 gateway 字段接入 `HostPlan`、`HostSnapshot`、`NetworkOverview`。
- [ ] 增加 `GatewayLeaseStore` 和 `FileGatewayLeaseStore`，写入 `gateway-leases.json`；复用现有 envelope、checksum、权限、fsync、rename，不修改 `CurrentSchemaVersion`。
- [ ] 在 validator 中实现 `ValidateGatewayPlan`：目标接口、静态 IPv4、主出口、pool 起止/子网/网络地址/广播地址/自身地址、prefix 与租约时长边界。
- [ ] 增加 `gateway` 模式的旧状态兼容用例与地址池表驱动测试；非法输入必须断言状态文件和 fake host snapshot 未改变。

验证点：`cd app && go test ./internal/pkg/netconfig/...`；覆盖 60 秒、604800 秒边界，越界返回可映射到 1115 的内部错误。

回滚点：M1 只增可选 JSON 字段和独立 lease 文件，不改既有 envelope schema。

### M2. Runtime、fake 与 Linux backend

- [ ] 在 `netconfig` 增加 `GatewayRuntime`/`GatewayBackend` 窄接口，runtime 构造只组装依赖。
- [ ] fake backend 支持：冲突探测注入、ip_forward 读写、server running、lease 分配/续租/释放、worker 退出和错误注入。
- [ ] 引入已验证版本的 `github.com/insomniacslk/dhcp`；封装 `server4`，绑定具体接口，不把 library API 泄漏到 service。
- [ ] Linux backend 通过 Go 文件 API 读写 `/proc/sys/net/ipv4/ip_forward`，实现接口绑定、有限 DHCP options、关闭等待和运行期冲突探测；不调用 shell、不写 NAT/防火墙。
- [ ] `Capabilities`：fake Linux 增加 `gateway`；真实 Linux 只有在平台层真实化能力和权限探测完成后开放；Darwin/其他平台不开放。

验证点：fake runtime 单测全绿；Linux 编译不依赖 macOS/cgo；无 socket/goroutine 泄漏；未开启 capability 时服务不触碰 backend。

回滚点：关闭真实 Linux gateway capability 可保留业务代码和 fake 测试，不得留下已启动的 server 或修改后的 sysctl。

### M3. NetworkService 事务与生命周期

- [ ] `networkService` 装配 gateway runtime，增加合并 platform/runtime 快照的内部 helper。
- [ ] 扩展 `SwitchMode` 的 gateway 分支：所有校验和启用前 probe 在 pending/平台写入前完成；候选 plan 保存 Gateway 配置与 `PreviousIPForward`。
- [ ] 实现进入、退出、cancel、timeout、factory-reset 的补偿序列：退出/恢复时 runtime 在前，platform 在后；进入时 platform 在前，runtime 在后。
- [ ] `ConfirmTransaction` 将合并后的 gateway 状态写入 last-valid；lease 分配不写 OperationLog；模式、forwarding、回滚写受控审计。
- [ ] `Start` 恢复 dangling pending；若 last-valid 是 gateway，恢复租约并启动 runtime；`Close` 停止 server/monitor/worker 但保留已确认状态。
- [ ] 恢复失败不得清除 pending 或返回假成功；overview 显示非 ready/recovery 状态并记录原始错误到 zap。

验证点：service 测试覆盖 AC1–AC7；每条错误路径断言 platform、runtime、lease store、ip_forward 的最终状态；已有 apply/confirm/cancel/factory-reset 测试不回归。

回滚点：M3 失败时先调用 runtime restore/close，再回退代码；生产环境先保持 gateway capability 关闭。

### M4. API、错误、权限与审计

- [ ] `api/network.go` 扩展 `SwitchModeRequest.Gateway`，校验 gateway 与 mode 的必选/互斥形状，新增 gateway DTO；不把平台路径/命令传给 service。
- [ ] `errno` 增加 1115/1116 三语文案；`error_handler.go` 将 1115 映射 400、1116 映射 409。
- [ ] 复用已有 `PUT /network/mode` 和 `ops:network:mode`；`GET /network` 的 gateway overview/leases 复用 `ops:network`，不新增路由。
- [ ] `middleware/oplog.go` 注册稳定 action key；内部审计摘要只含脱敏配置摘要，不保存 DHCP 原文和状态路径。
- [ ] 不新增数据库迁移；确认已有 000010 的 `ops:network:mode` 权限和三语前端 action key 能覆盖本 child。

验证点：`cd app && go test ./internal/api/... ./internal/middleware/... ./internal/router/...`；API 测试覆盖 1115/1116、403、旧模式请求回归和 JSON envelope。

### M5. 前端网关面板

- [ ] `api/core/network.ts` 增加 gateway mode、GatewayPlan、GatewayOverview、GatewayLease 类型；扩展 `SwitchModeParams`。
- [ ] `views/ops/network/index.vue` 复用现有模式选择、pending 倒计时、确认/取消和重连地址，增加下行接口、pool、prefix、lease duration、ip_forward 控件。
- [ ] 下行接口选项仅显示静态、可写、非 primary、非 bond/slave；unsupported capability 不渲染可提交选项。
- [ ] 提交前始终显示 DHCP 广播域风险和不做 NAT 的后果；运行中显示 conflict warning 与租约列表。
- [ ] 三份 `ops.json` 同步新增模式、字段、风险、NAT 后果、租约表头与审计文案；时间列使用已有全局格式化。

验证点：`cd ui && pnpm check && pnpm test:unit`；覆盖 capability gating、字段校验、警示可见、lease rendering 和三语 key 对齐。

### M6. 集成与交付检查

- [ ] fake Linux：切入 gateway → overview 显示运行与空租约 → fake client 分配/续租 → lease 列表可查。
- [ ] fake Linux：冲突 probe、DHCP client 下行口、非法 pool、ip_forward 三路径均无部分写入。
- [ ] fake Linux：取消/超时/恢复出厂停止 server、释放 socket、清理 lease、恢复原 `ip_forward` 和接口快照。
- [ ] fake Linux：重启服务恢复有效 lease 和已确认 gateway；dangling pending 自动回滚。
- [ ] macOS/真实 Linux 未开放 capability 时返回 1106，页面不允许提交 gateway。
- [ ] Linux 特权集成 build tag 在独立 network namespace 中验证 socket 绑定、DORA/renew/release、冲突探测和 sysctl 恢复。
- [ ] 勾选 PRD 中 `[CI]` 项；目标机/台架项按环境记录，不把 fake 结果标作真实硬件通过。

最终验证：`cd app && make vet && make test`；`cd ui && pnpm check && pnpm test:unit`；必要时运行显式 Linux 特权集成命令。

## 3. 风险清单

- DHCP 依赖版本 API 与 raw socket 权限不兼容：先锁版本和构造 smoke test，再实现 runtime。
- runtime 与 platform 补偿顺序错误：用 fake failure injection 固定进入/退出/超时/恢复出厂的资源状态。
- lease 文件损坏导致重复分配：checksum/version 失败必须阻止启动，不得当空池处理。
- `Close` 错误地恢复 `ip_forward` 或清理 confirmed lease：用重启测试区分进程关闭与模式退出。
- capability 提前开放：真实 Linux 在平台层未完成前保持 `multi-address`，CI 仅用 fake Linux。

## 4. 交付前复核

- [ ] `CurrentSchemaVersion` 未改变，旧状态文件可读。
- [ ] gateway 请求不接受接口名以外的平台私有字段、命令或路径。
- [ ] 1115/1116 在 errno、HTTP 映射、前端错误处理和三语文案中一致。
- [ ] 租约分配没有 OperationLog 写入；模式、forwarding、确认、回滚有审计。
- [ ] 没有 NAT、防火墙或外部 DHCP 进程。
- [ ] 所有后台 goroutine 都由 Start/Close 管理并在测试中等待退出。
- [ ] API 契约和本计划确认后才运行 `task.py start`。
