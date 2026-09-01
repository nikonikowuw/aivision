# 链路聚合网络模式实施计划（LACP 802.3ad）

状态：`in_progress`
对应设计：`design.md`
前置：`08-23-active-backup-bonding` 已实现并归档；真实 Linux 验收依赖 `08-23-linux-platform-realization`

## 1. 实施前门禁 M0

- [x] 用户确认本任务最新规划摘要与 `api.md` 增量契约。
- [x] 确认本任务只扩展 LACP 业务层、fake 语义和平台接线契约，不自动配置交换机，不把真实硬件台架项伪装成 CI。
- [x] `task.py start .trellis/tasks/08-23-lacp-aggregation` 成功，任务状态变为 `in_progress`。

验证：`python3 ./.trellis/scripts/task.py current --source` 指向本任务；`task.py validate` 通过；`api.md` 标记为 `confirmed`。

## 2. 模型与契约 M1

目标文件：

- `app/internal/pkg/netconfig/types.go`
- `app/internal/pkg/netconfig/platform.go`
- `ui/apps/web-antd/src/api/core/network.ts`
- 本任务 `api.md`

工作项：

- [x] 新增 `NetworkModeLACP`、hash policy 封闭枚举、slow LACP rate 常量与校验函数。
- [x] 扩展 `BondPlan` / `BondTopology`，增加 LACP 状态 DTO、speed/duplex 字段和 warning DTO；所有新增持久化字段可选。
- [x] `AllNetworkModes`、前端 `NETWORK_MODES`、Transaction 类型保持前后端逐字一致。
- [x] 确认 `CurrentSchemaVersion`、状态 envelope 读取逻辑与旧 active-backup JSON 不变。

验证：`cd app && go test ./internal/pkg/netconfig/...`；新增旧格式兼容、枚举边界和 JSON round-trip 测试。

## 3. Fake 平台与能力探测 M2

目标文件：

- `app/internal/pkg/netconfig/fake.go`
- `app/internal/pkg/netconfig/manager_linux.go`
- `app/internal/pkg/netconfig/manager_linux_test.go`
- `app/internal/pkg/netconfig/manager_darwin_test.go`
- `app/internal/pkg/netconfig/netconfig_test.go`

工作项：

- [x] fake capability 增加 LACP；fake bond apply 支持 2 到当前可用物理 slave 数，保存 hash policy 和 slow rate。
- [x] fake 支持已协商、未协商、部分进组三种可控状态，Read 返回 aggregator/actor/partner DTO。
- [x] fake 支持速度/双工注入，验证 mismatch 只产生 warning 不阻断。
- [x] fake Restore 完整恢复旧 mode、bond、slave 归属和 IPv4。
- [x] Linux capability 只有 Probe 成功后才暴露 LACP；Probe 未完成/失败 fail closed。Darwin 继续只暴露 multi-address。
- [x] 为内核/平台拒绝 LACP 增加可测试的内部 sentinel 注入，不把 partner 未协商模拟为 error。

验证：

```bash
cd app
make vet
go test ./internal/pkg/netconfig/...
```

重点断言：macOS 请求 LACP 返回 1106；fake 三种协商状态可回读；内核拒绝映射路径和未协商成功路径可区分。

## 4. Service 模式切换 M3

目标文件：

- `app/internal/service/network.go`
- `app/internal/service/network_test.go`
- `app/internal/pkg/errno/errno.go`
- `app/internal/middleware/error_handler.go`

工作项：

- [x] 将 `SwitchMode` 的 bond 分支泛化为 active-backup/LACP；LACP 默认 hash policy、固定 slow rate 和 `mode=4` 计划字段由服务端填充。
- [x] 校验 LACP slave 数量、存在性、物理性、可写性/当前 bond 重用、指纹、重复、占用关系；非法 hash policy 在 Apply 前拒绝且状态零修改。
- [x] 比较 speed/duplex 并把 warning 写入响应与 pending；不因 mismatch 自动回滚。
- [x] 支持 active-backup 与 LACP 在相同 slave 集合上的直接重建；不满足条件时返回既有 mode conflict。
- [x] LACP partner 未协商时保持 pending/成功状态，overview 带诊断；平台真正拒绝 mode/属性时恢复 before 并返回 1114。
- [x] 确保模式切换响应 overview 填充完整 LACP 状态，confirm/cancel/timeout/startup recovery 复用既有回滚。
- [x] 新增 `CodeNetworkLacpNegotiationFailed=1114` 三语文案，并映射 HTTP 503；不改变 1106/1107/1113 既有语义。

验证：

```bash
cd app
go test ./internal/pkg/errno/... ./internal/service/...
```

覆盖：非法 policy、2 个 slave、3 个以上 slave、当前 bond 重用、speed/duplex warning、三种协商状态、pending 冲突、apply 拒绝补偿、取消、超时、启动恢复和审计。

## 5. API 绑定 M4

目标文件：

- `app/internal/api/network.go`
- `app/internal/api/network_test.go`
- `app/internal/middleware/error_handler.go`（如 M3 未完成）

工作项：

- [x] 在既有 `SwitchModeRequest`/`BondRequest` 中增加 LACP 的 `xmitHashPolicy` 可选字段。
- [x] active-backup 旧请求、LACP 请求、multi-address 退出请求分别绑定；LACP 不接受 primary/lacpRate 等未定义输入。
- [x] 保持既有 `/network/mode` 路由和 `ops:network:mode` 权限，不新增 migration。
- [x] 固定错误 envelope、1114/503、1106/409 等 API 契约。

验证：`cd app && go test ./internal/api/... ./internal/middleware/...`。

## 6. 前端 M5

目标文件：

- `ui/apps/web-antd/src/api/core/network.ts`
- `ui/apps/web-antd/src/views/ops/network/index.vue`
- `ui/apps/web-antd/src/locales/langs/{zh-CN,en-US,zh-TW}/ops.json`
- 相关页面测试（如仓库已有测试目录）

工作项：

- [x] 增加 LACP 类型、hash policy 选项和状态 DTO，禁止裸字符串散落在模板中。
- [x] capability 含 LACP 时显示选项；缺失时隐藏/禁用且不发起请求。
- [x] LACP 表单选择 2 到当前可写物理网卡上限，提供 hash policy 下拉，默认 `layer2+3` 并标注需与交换机对齐。
- [x] 提交前展示交换机动态 LAG、单条连接不跨链路、入向分流由交换机决定的提示。
- [x] 拓扑区展示 aggregator、actor/partner 状态和进组标识；未进组以警示样式显示诊断文案。
- [x] 展示 speed/duplex mismatch warning，继续复用 pending 倒计时、确认和取消交互。
- [x] 三语 key 同步，文案不硬编码；保持 active-backup 页面行为不回归。

验证：

```bash
cd ui
pnpm check
pnpm test:unit
```

## 7. 平台接线与台架 M6

目标边界：`08-23-linux-platform-realization` 提供真实 netlink 原语；LACP child 负责联调与契约测试。

- [x] 确认 Probe 能在属性不可验证时 fail closed，并在目标 Linux 上声明 LACP。
- [x] 确认 mode 4、hash 数值映射、slow rate、slave 绑定和 bond 拆除均经 rtnetlink。
- [x] 确认 Read 不解析 `/proc/net/bonding/bond0`，能读 aggregator/actor/partner 状态。
- [ ] 目标机成功创建 LACP bond，取消/超时可拆除并恢复 slave 原配置。
- [ ] 交换机配置 LAG 后全部 slave 同一 aggregator；未配置 LAG 时 overview 显示 warning 且设备不因协商结果自动回滚。
- [ ] 多路不同源 IP RTSP 流总吞吐与单流上限按 PRD AC7 验收，不能把单流不跨链路当缺陷。

## 8. 质量检查 M7

- [x] `cd app && make vet && make test`。
- [x] `cd ui && pnpm check && pnpm test:unit`。
- [x] 检查 `gofmt -l .`、前端格式/类型检查、三语 key 数量和 warning/status 字段一致。
- [x] 检查 active-backup 与 multi-address 既有测试全部通过，状态 schema 仍为 1，无 shell、sysfs 文本写入或 `/proc/net/bonding` 解析。
- [x] PRD AC1–AC3、AC8 CI 项完成；AC4–AC7 按目标机/台架标注真实结果，不把缺少硬件写成通过。

## 9. 回滚点

- M1 可独立回退，不改变状态 schema。
- M2/M3 回退前端可仍显示 active-backup；回退时保留 active-backup 测试。
- M4/M5 为同一 API 增量，回退不删除既有 `/network/mode`。
- M6 真实平台联调失败时保持 capability fail closed，禁止用 fake 结果冒充目标机成功。
