# macOS SystemConfiguration 实现研究

## 结论

macOS 侧使用官方 SystemConfiguration C API 管理已有 Network Service，不以
`networksetup`、`ifconfig` 或 plist 文本解析作为主实现。配置偏好与实际运行状态是两个
不同数据源：`SCPreferences`/`SCNetworkConfiguration` 负责配置，`SCDynamicStore` 负责
生效状态。

## 服务发现与稳定标识

- 从当前 `SCNetworkSet`（network location）遍历已有 `SCNetworkService`。
- 持久标识使用 `SCNetworkServiceGetServiceID`。
- 通过 `SCNetworkServiceGetInterface` 校验类型，只接收
  `kSCNetworkInterfaceTypeEthernet` 与 `kSCNetworkInterfaceTypeIEEE80211`。
- service 名称是显示字段，`en0` 等 BSD name 只用于诊断/交叉校验，不能作为唯一持久 ID。
- service 被删除重建后 ID 可能变化。此时应标记原配置不可匹配并停止写入，不能按名称猜测。

参考：

- [SCNetworkConfiguration](https://developer.apple.com/documentation/systemconfiguration/scnetworkconfiguration)
- [SCNetworkServiceGetServiceID](https://developer.apple.com/documentation/systemconfiguration/scnetworkservicegetserviceid(_:))
- [SCNetworkInterfaceGetBSDName](https://developer.apple.com/documentation/systemconfiguration/scnetworkinterfacegetbsdname(_:))

## IPv4 与 DNS 配置

对每个 service 读取已有的 `kSCNetworkProtocolTypeIPv4` 和 `kSCNetworkProtocolTypeDNS`
protocol，只修改协议配置，不触碰 CoreWLAN、SSID、密码或关联状态。

IPv4 字典：

- DHCP：`ConfigMethod = DHCP`。
- Static：`ConfigMethod = Manual`，并设置 `Addresses`、`SubnetMasks`、`Router`。

DNS 字典使用 `ServerAddresses`。DHCP 模式清除本系统设置的静态 DNS，使运行态 DNS 跟随
租约。修改前必须复制完整原字典，避免无意删除 DHCP Client ID 或其他平台字段。目标
protocol 不存在时 MVP 失败关闭，不隐式创建 Network Service。

参考：

- [IPv4 Entity Keys](https://developer.apple.com/documentation/systemconfiguration/ipv4-entity-keys)
- [DNS Entity Keys](https://developer.apple.com/documentation/systemconfiguration/dns-entity-keys)
- [SCNetworkProtocolSetConfiguration](https://developer.apple.com/documentation/systemconfiguration/scnetworkprotocolsetconfiguration(_:_:))

## Commit、Apply 与回滚

`SCNetworkProtocolSetConfiguration` 只修改 preferences session；候选配置必须先
`SCPreferencesCommitChanges` 写入持久存储，再用 `SCPreferencesApplyChanges` 请求应用
到运行系统。Apple API 没有临时的 120 秒事务。

建议流程：

1. 获取 `SCPreferences` 排他锁，并保存原始 IPv4/DNS 字典、preferences signature、
   service/interface 指纹和当前 set ID。
2. 在本系统 root-only 事务日志中原子写入旧快照、候选值、generation 和 deadline。
3. `SetConfiguration` 候选值，commit，然后 apply。
4. 从 Dynamic Store 等待并验证实际地址、router、DNS 或 DHCP lease。
5. 验证成功后进入待用户确认状态；确认后写 last-valid 并清除 pending。
6. apply/验证失败、取消、超时或启动发现 pending 时，将原字典再次 set、commit、apply。
7. 回滚前确认当前 preferences 仍对应本事务候选；若已被外部修改则报告冲突，不能覆盖。

必须显式处理 `SCPreferencesLock` 忙和每次 `SCError()`。root launchd daemon 通常可以用
`SCPreferencesCreate` 修改系统配置；既然产品选择 root 主进程，就不依赖交互式
Authorization Services。

参考：

- [SCPreferencesCommitChanges](https://developer.apple.com/documentation/systemconfiguration/scpreferencescommitchanges(_:))
- [SCPreferencesApplyChanges](https://developer.apple.com/documentation/systemconfiguration/scpreferencesapplychanges(_:))
- [SCPreferencesLock](https://developer.apple.com/documentation/systemconfiguration/scpreferenceslock(_:_:))

## 快照与实际状态

factory baseline 在首次接管时保存完整 IPv4/DNS 字典、service/interface 指纹和 set ID，
之后不能被普通确认覆盖。last-valid 保存最近一次确认且实际验证成功的配置。

实际状态使用：

- `SCDynamicStoreKeyCreateNetworkServiceEntity` 创建 service IPv4/DNS state key。
- `SCDynamicStoreCopyValue` 读取实际地址、router 与 DNS。
- `SCDynamicStoreCopyDHCPInfo` 读取 DHCP 细节。

不能把 preferences 字典直接当成实际生效状态，也不能读取 plist 或命令输出代替 Dynamic
Store。

参考：

- [SCDynamicStore](https://developer.apple.com/documentation/systemconfiguration/scdynamicstore-gb2)
- [SCDynamicStoreKeyCreateNetworkServiceEntity](https://developer.apple.com/documentation/systemconfiguration/scdynamicstorekeycreatenetworkserviceentity(_:_:_:_:))

## Go/cgo 边界

最小实现使用 build-tag 隔离的平台包：

- `bridge_darwin.c/.h`：封装 CoreFoundation/SystemConfiguration 对象、锁、配置字典、
  Dynamic Store 查询和内存释放；纯 C 即可，不要求 Objective-C。
- `manager_darwin.go`：`//go:build darwin && cgo`，链接
  `SystemConfiguration` 与 `CoreFoundation` framework，将 C 结果规范化为共享 Go 类型。
- `manager_darwin_nocgo.go`：`//go:build darwin && !cgo`，在启动能力检查时返回明确不支持。
- Linux 文件不编译 cgo bridge；不要让 CF 引用或 Go 指针跨调用边界长期存活。

C bridge 应暴露窄的结构化函数，不向 Go 返回未托管 CF 对象，也不接受命令、文件路径或
脚本文本。macOS API 二进制必须以 `CGO_ENABLED=1` 构建。

## 测试边界

- Go 契约测试用 fake platform 覆盖 DHCP/Manual 规范化、状态机、快照、确认、超时、
  重启恢复和外部配置冲突。
- C bridge 可把配置字典转换/错误映射拆成无副作用测试，但真实 commit/apply 不能进入普通
  单元测试。
- macOS SDK 头文件还确认 network preference 层提供 `kSCPropNetServiceOrder`（`CFArray`）
  和 `kSCPropNetOverridePrimary`（`CFNumber`），`SCNetworkSetGetServiceOrder` 返回当前
  set 的用户指定 service ID 顺序，Dynamic Store 提供 `PrimaryService` 与
  `PrimaryInterface`。因此主出口设计应把 service order/OverridePrimary 纳入候选快照，
  并以 Dynamic Store 的实际 PrimaryService 作为回读事实；不能仅凭 preferences 成功
  就声称默认路由或 DNS 已迁移。当前机器 SDK 证据位置为
  `/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/System/Library/Frameworks/SystemConfiguration.framework/Headers/SCSchemaDefinitions.h:80-90,413-414,699-710,2359-2363`
  与 `SCNetworkConfiguration.h:1312-1322,1376-1383`。Service order 的系统版本差异和
  DHCP 作用域 resolver 行为仍必须在 macOS 特权集成测试中验证；对外契约只承诺一个
  非作用域主出口，不承诺删除系统内部可能存在的 scoped route/resolver。
