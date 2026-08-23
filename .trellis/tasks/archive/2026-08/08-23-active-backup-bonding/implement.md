# 主备容错网络模式实施计划

状态：`planning`
对应设计：`design.md`

## 1. 执行顺序

每个里程碑独立可验证；未通过验证点不得进入下一步。

### M0. 进入实现前的门禁

- [ ] `api.md` 的模式切换端点契约经用户确认（PRD R5.5）。
- [ ] 确认本轮范围：业务层 + fake 平台，不做真实 rtnetlink（PRD D6）。
- [ ] `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-23-active-backup-bonding`
      将状态置为 `in_progress`。

**验证点**：`task.py current` 指向本 task；`api.md` 状态为 `confirmed`。

### M1. 模型扩展与状态文件兼容

- [ ] `netconfig/types.go` 新增 `NetworkMode`、`BondPlan`、`BondTopology`；
      `TransactionAction` 增 `mode_switch`。
- [ ] `HostPlan` / `HostSnapshot` / `InterfaceInfo` / `Capabilities` /
      `NetworkOverview` / `PendingTransaction` 按 design 2.2 增可选字段，全部 `omitempty`。
- [ ] 新增 `normalizeMode()`，空值归一化为 `multi-address`；只在读侧调用。
- [ ] **不改** `state.go` 的 `CurrentSchemaVersion` 与 `readEnvelope`。

**验证点**：`cd app && make vet && go test ./internal/pkg/netconfig/...`；
新增用例——用 08-22 格式（无 mode/bond 字段）的 envelope 写入临时目录，
`GetFactory()` / `GetLastValid()` 读取成功且 `Mode` 归一化为 `multi-address`（AC5）。

### M2. Platform 接口与能力声明

- [ ] `platform.go` 的 `Platform` 接口新增 `Capabilities(ctx) Capabilities`。
- [ ] 四个实现补齐该方法，`SupportedModes` 按 design 3.2 表格取值；
      其余四个布尔字段照搬 `service/network.go:199-204` 的现有取值，不改行为。
- [ ] `service/network.go` 的 `GetOverview` 删除硬编码 `Capabilities` 字面量，
      改为 `s.platform.Capabilities(ctx)`。

**验证点**：`go test ./internal/pkg/netconfig/... ./internal/service/...`；
新增用例断言四个实现的 `SupportedModes` 与四个布尔字段取值；
既有 overview 用例（DHCP/StaticIPv4/FactoryReset/WifiAssociation）不变。

### M3. Fake 平台的 bond 语义

- [ ] `FakePlatform` 增 `mode` / `bond` 字段。
- [ ] `Apply` 按 design 4.1 实现进入 `active-backup`：创建 `bond0`、标记 slave 归属、
      清空 slave IPv4、填充 `HostSnapshot.Bond`。
- [ ] `Apply` 按 design 4.2 实现退回 `multi-address`：删除 bond、归还 slave、恢复其 IPv4。
- [ ] `Restore` 补两行同步 `f.mode` / `f.bond`，其余不动。

**验证点**：`go test ./internal/pkg/netconfig/...`；
新增用例——`Apply(active-backup)` 后 `Read()` 含 bond0、两 slave `MasterID` 指向 bond0
且 `Writable=false`；`Restore(before)` 后 bond0 消失、slave 归还且 IPv4 与 before 逐字段相等（AC6）。

### M4. 服务层 SwitchMode

- [ ] `NetworkService` 接口新增 `SwitchMode`；`SwitchModeInput` 按 design 5.1 定义。
- [ ] 按 design 5.2 的 11 步实现，校验全部前置于 `platform.Apply`。
- [ ] `ConfirmTransaction` / `CancelTransaction` / `handleTimeout` 中按
      `Action == mode_switch` 补写审计，**不改其控制流**。
- [ ] `GetOverview` 增加 `Mode` / `Bond` 输出。

**验证点**：`go test ./internal/service/...`；用例覆盖——
成功切换、pending 冲突（1101）、平台不支持（1106）、slave 六类非法组合（1112）、
模式冲突（1113）、bond IPv4 非法（1100）、apply 失败补偿、超时回滚、启动恢复、审计写入。
校验失败用例必须断言 `platform.Read()` 结果与调用前逐字段相等（AC3）。

### M5. API、路由、错误码与迁移

- [ ] `api/network.go` 新增 `SwitchMode` handler 与请求 DTO。
- [ ] `router.go:219-233` 追加 `PUT /network/mode` 路由与 `ops:network:mode` 权限注册。
- [ ] `errno.go` 追加 1112 / 1113 及三语文案。
- [ ] 新增 `migrations/000010_add_network_mode_permission.{up,down}.sql`，
      沿用 000009 的幂等写法。

**验证点**：`go test ./internal/api/...`；用例覆盖端点绑定、缺权限 403、错误码 HTTP 映射。
`make migrate-up` 后查询 `menus` 表存在 `ops:network:mode` 且已绑定 super 角色；
`migrate down` 一步可回退。

### M6. 前端

- [ ] `api/core/network.ts` 按 design 8.1 扩展类型并新增 `switchNetworkModeApi`。
- [ ] `views/ops/network/index.vue` 按 design 8.2 插入工作模式卡片、拓扑展示与切换表单；
      倒计时与确认/取消复用 `index.vue:113-226` 既有函数。
- [ ] `ops.json` 三语补齐模式名称、拓扑标签、警示文案与 action key。

**验证点**：`cd ui && pnpm check && pnpm test:unit`；
用例覆盖——`supportedModes` 不含 `active-backup` 时该选项禁用且不发请求；
拓扑区正确渲染 bond 与 slave 从属关系；确认弹窗文案存在。

### M7. 集成验证与交付前检查

- [ ] `fake_platform: true` 下手动走通：切到 `active-backup` → 页面显示拓扑 →
      不确认等待 120 秒 → 自动回滚 → 页面恢复多址。
- [ ] 重复上述流程但中途重启服务，验证启动恢复回滚。
- [ ] 确认 `LinuxPlatform` / `DarwinPlatform` 下模式选项被禁用。

**验证点**：`cd app && make vet && make test`；`cd ui && pnpm check && pnpm test:unit`；
PRD 的 AC1–AC7 全部勾选；AC8 保持 `[阻塞]` 未勾。

## 2. 质量门禁

- 后端：`make vet`、`make test` 全绿。
- 前端：`pnpm check`（circular + dep + typecheck + cspell）、`pnpm test:unit` 全绿。
- 迁移：`make migrate-up` 与一步 `down` 均成功。
- 不得出现新的 `go vet` 警告或 TypeScript `any` 逃逸。
- 三语 locale key 数量一致，无缺失。

## 3. 变更后的复核清单

- [ ] `Platform` 接口的四个实现全部补齐 `Capabilities`，无编译遗漏。
- [ ] 状态文件 `CurrentSchemaVersion` 仍为 1，`readEnvelope` 未改。
- [ ] 既有五条写路径（apply / confirm / cancel / factory-reset / GET）的控制流未改，
      只在 confirm/cancel/timeout 内新增了 `Action == mode_switch` 的审计分支。
- [ ] 08-22 遗留的 `ApplyInterface` 审计缺口未被顺带修改（属 Out of Scope）。
- [ ] 新增的 errno 号段与 parent `prd.md` 的登记表一致（1112 / 1113）。
- [ ] 前端未新建 store，pending 状态仍走页面局部状态。
- [ ] `miimon` 未出现在任何 HTTP 请求体或前端表单中（D2）。
- [ ] PRD 的 AC8 仍标注 `[阻塞]`，未被误勾。

## 4. 回滚点

每个里程碑一次提交，M1–M6 均可独立 revert：

- M1/M2 只增字段与方法，revert 无副作用。
- M5 的 migration 需先 `migrate down` 再 revert 代码。
- M6 前端独立于后端，可单独 revert 而不影响 API。
