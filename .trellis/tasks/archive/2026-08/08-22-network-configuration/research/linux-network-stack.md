# Linux 网络配置实现研究

## 结论

Linux 侧可以用统一的内核接口实现链路、IPv4 地址和路由，但 DHCP 与 DNS 不属于
rtnetlink。受控设备镜像必须明确三个所有权边界：

1. 本系统独占 Profile 声明的物理有线接口。
2. 本系统拥有这些接口的 DHCPv4 生命周期。
3. 本系统拥有系统 resolver 文件；若 `/etc/resolv.conf` 由其他组件托管则拒绝写入。

“所有 Linux”应解释为使用共同的 Linux 内核 ABI，并由部署 Profile 建立上述运行条件，
不能解释为在任意现存发行版上与未知网络管理器无条件共存。

## 库选型

### rtnetlink

首选 [`github.com/vishvananda/netlink`](https://github.com/vishvananda/netlink)
（Apache-2.0）。该库直接使用 `NETLINK_ROUTE`，高层 API 覆盖 link、地址、路由和
事件监听，不需要执行 `ip` 命令。实现时只操作受管 ifindex 和本系统记录的精确地址/
路由元组，禁止 flush 全局网络状态。

备选是 [`github.com/jsimonetti/rtnetlink`](https://github.com/jsimonetti/rtnetlink)
或基于 [`github.com/mdlayher/netlink`](https://github.com/mdlayher/netlink) 自行编码。
二者更接近协议层，但会增加消息编解码和兼容维护工作；MVP 没有证据要求承担该复杂度。

官方语义依据：[`rtnetlink(7)`](https://man7.org/linux/man-pages/man7/rtnetlink.7.html)。

### DHCPv4

首选 [`github.com/insomniacslk/dhcp`](https://github.com/insomniacslk/dhcp)
（BSD-3-Clause）的 `dhcpv4/nclient4`。它支持 DORA、Renew、Release 和 DHCP option
解析，但不会替应用管理地址、路由、DNS 或完整租约生命周期。

无 IPv4 地址的接口不能依赖普通 UDP 路由发送 Discover。`nclient4` 在 Unix 使用
`AF_PACKET`/`SOCK_DGRAM` 并绑定 ifindex；这需要 `CAP_NET_RAW`，root 主进程满足。
接收包仍需按 XID、客户端 MAC、消息类型和 Server Identifier 校验。

应用层必须按 [RFC 2131](https://www.rfc-editor.org/rfc/rfc2131.html) 编排 BOUND、
T1 Renew、T2 Rebind、租约到期清理和重新 DORA；链路变化和进程 context 取消也必须
终止或重启对应接口的租约协程。DHCP 返回的地址、网关、DNS 和 classless route 在安装前
都要校验。

## DNS 边界

Linux 没有 rtnetlink DNS API。glibc 通常读取
[`resolv.conf(5)`](https://man7.org/linux/man-pages/man5/resolv.conf.5.html)，但
`/etc/resolv.conf` 可能是 systemd-resolved、resolvconf 或发行版脚本维护的 symlink。

MVP 的最小可移植契约：

- 部署 Profile 保证 `/etc/resolv.conf` 是由本系统独占的普通文件。
- 写入使用同目录临时文件、`fsync` 和原子替换，最多输出 3 个 `nameserver`。
- 遇到 symlink、只读文件或已知托管标记时拒绝写入，不跟随链接覆盖其他组件状态。
- DNS 是整机全局资源，因此同一时刻只能有一个网络候选事务；多接口需要明确唯一主出口，
  只有它提供默认路由和系统 DNS。

## 接口识别与所有权

接口名模式 `eth*`/`en*` 不可靠，`ARPHRD_ETHER` 也可能代表 bridge、VLAN、veth 或
Wi-Fi。候选接口可综合检查：Ethernet link type、非 loopback、
`/sys/class/net/<name>/device` 存在、`iflink == ifindex`、MAC、driver 和 carrier；但
最终事实来源必须是部署 Profile 的 allowlist（首次按名称定位后持久化永久 MAC/硬件
标识）。参考 [sysfs 网络 ABI](https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net)。

不存在可以证明“所有其他网络管理器均未管理此接口”的统一 Linux API。NetworkManager
可查 D-Bus `Managed/State`，其他管理器、DHCP 客户端和厂商脚本没有共同协议。设计应：

- 由 Profile 负责取消托管并写入 root-only 所有权声明。
- 对已知管理器做 best-effort 检查。
- 订阅 rtnetlink 事件检测实际配置漂移；检测到外部覆盖后阻止后续写入并告警。
- 不把进程名、配置文件或 `/run` 标记不存在表述为绝对无冲突证明。

NetworkManager 参考：
[`org.freedesktop.NetworkManager.Device`](https://networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.Device.html)。

## 候选事务与 DHCP 恢复

rtnetlink 的多条消息不是跨对象原子事务。应用候选前必须持久记录旧快照、候选值、
generation、截止时间和 boot ID，然后按可补偿顺序修改地址、直连路由、默认路由和 DNS；
任一步失败都幂等恢复已变更部分。

静态旧配置可以精确恢复。旧模式是 DHCP 时，只能恢复 DHCP 模式并重新获取/尝试续租，
DHCP 服务器不保证重新分配相同地址；验收不能要求 DHCP 回滚后 IP 字面值必然不变。
启动时，未完成候选事务立即回滚；确认的静态配置直接重放，确认的 DHCP 配置尝试
INIT-REBOOT 或重新 DORA。

## 测试建议

- 纯单元/契约测试使用 fake platform 和临时时钟，不触碰宿主机网络。
- Linux 特权集成测试使用独立 network namespace、veth 和真实 DHCP test server。
- 覆盖丢包、重复 Offer、NAK、T1/T2、租约到期、链路断开、context 取消。
- 覆盖多网卡默认路由、DNS 普通文件/symlink/只读、外部 rtnetlink 漂移。
- 对每个 rtnetlink 应用步骤注入失败，验证补偿与重复回滚幂等。
- 覆盖进程崩溃、重启、boot ID 变化和墙上时间跳变。
- 至少在目标 ARM Linux Profile 和常规 Linux CI 内核上运行集成套件。
