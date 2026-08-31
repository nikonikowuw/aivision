//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"os"

	"github.com/vishvananda/netlink"
)

// mapXmitHashPolicyToNetlink 将封闭枚举转换为 netlink BondXmitHashPolicy。
func mapXmitHashPolicyToNetlink(p *BondXmitHashPolicy) netlink.BondXmitHashPolicy {
	if p == nil {
		return netlink.BOND_XMIT_HASH_POLICY_LAYER2_3
	}
	switch *p {
	case BondXmitHashPolicyLayer2:
		return netlink.BOND_XMIT_HASH_POLICY_LAYER2
	case BondXmitHashPolicyLayer23:
		return netlink.BOND_XMIT_HASH_POLICY_LAYER2_3
	case BondXmitHashPolicyLayer34:
		return netlink.BOND_XMIT_HASH_POLICY_LAYER3_4
	default:
		return netlink.BOND_XMIT_HASH_POLICY_LAYER2_3
	}
}

// reconcileLinuxBond 根据 HostPlan 协调 bond0 接口的生命周期与 slave 绑定。
func reconcileLinuxBond(
	ctx context.Context,
	plan HostPlan,
	currentSnapshot HostSnapshot,
) error {
	const bondName = "bond0"
	existingBond, _ := netlink.LinkByName(bondName)

	if plan.Mode == NetworkModeMultiAddress || plan.Mode == NetworkModeGateway {
		// 退出 bond 模式：拆除 bond0 与解绑 slave
		if existingBond != nil {
			// 先解绑现有 slaves
			links, err := netlink.LinkList()
			if err == nil {
				for _, l := range links {
					if l.Attrs().MasterIndex == existingBond.Attrs().Index {
						_ = netlink.LinkSetNoMaster(l)
					}
				}
			}
			if err := netlink.LinkDel(existingBond); err != nil {
				return fmt.Errorf("delete bond interface %s: %w", bondName, err)
			}
		}
		return nil
	}

	if !plan.Mode.IsBond() || plan.Bond == nil {
		return nil
	}

	// 准备创建或更新 bond0
	miimon := plan.Bond.Miimon
	if miimon <= 0 {
		miimon = DefaultBondMiimon
	}

	bondMode := netlink.BOND_MODE_ACTIVE_BACKUP
	if plan.Mode == NetworkModeLACP {
		bondMode = netlink.BOND_MODE_802_3AD
	}

	if existingBond == nil {
		// 创建 bond 接口
		bondLink := netlink.NewLinkBond(netlink.LinkAttrs{
			Name: bondName,
		})
		bondLink.Mode = bondMode
		bondLink.Miimon = miimon
		if plan.Mode == NetworkModeActiveBackup && plan.Bond.PrimarySlaveID != "" {
			if pLink, err := netlink.LinkByName(plan.Bond.PrimarySlaveID); err == nil {
				bondLink.Primary = pLink.Attrs().Index
			}
		}
		if plan.Mode == NetworkModeLACP {
			bondLink.XmitHashPolicy = mapXmitHashPolicyToNetlink(plan.Bond.XmitHashPolicy)
			bondLink.LacpRate = netlink.BOND_LACP_RATE_SLOW
		}

		if err := netlink.LinkAdd(bondLink); err != nil {
			if plan.Mode == NetworkModeLACP {
				return fmt.Errorf("%w: failed to create lacp bond link: %v", ErrLacpNegotiationFailed, err)
			}
			return fmt.Errorf("%w: failed to create bond link: %v", ErrApplyFailed, err)
		}

		// 重新获取创建成功的 bond 句柄
		var err error
		existingBond, err = netlink.LinkByName(bondName)
		if err != nil {
			return fmt.Errorf("retrieve created bond %s: %w", bondName, err)
		}
		// 启动 bond 接口
		_ = netlink.LinkSetUp(existingBond)
	}

	// 绑定 Slave 接口
	for _, slaveID := range plan.Bond.SlaveIDs {
		slaveLink, err := netlink.LinkByName(slaveID)
		if err != nil {
			return fmt.Errorf("%w: slave interface %s not found: %v", ErrApplyFailed, slaveID, err)
		}
		// 设置 slave admin down -> master -> admin up
		_ = netlink.LinkSetDown(slaveLink)
		if err := netlink.LinkSetMaster(slaveLink, existingBond); err != nil {
			return fmt.Errorf("%w: attach slave %s to bond %s: %v", ErrApplyFailed, slaveID, bondName, err)
		}
		_ = netlink.LinkSetUp(slaveLink)
	}

	return nil
}

// rollbackLinuxBond 将 bond 拓扑回滚恢复至指定 snapshot 状态。
func rollbackLinuxBond(target HostSnapshot) error {
	const bondName = "bond0"
	existingBond, _ := netlink.LinkByName(bondName)

	if !target.Mode.IsBond() {
		// 目标状态为非 bond 模式：清理 bond0 并解绑 slave
		if existingBond != nil {
			links, err := netlink.LinkList()
			if err == nil {
				for _, l := range links {
					if l.Attrs().MasterIndex == existingBond.Attrs().Index {
						_ = netlink.LinkSetNoMaster(l)
					}
				}
			}
			_ = netlink.LinkDel(existingBond)
		}
		return nil
	}

	// 目标状态原本为 bond 模式：恢复 slave 拓扑
	if existingBond != nil {
		for ifID, ifInfo := range target.Interfaces {
			if ifInfo.MasterID != nil && *ifInfo.MasterID == bondName {
				if slaveLink, err := netlink.LinkByName(ifID); err == nil {
					_ = netlink.LinkSetMaster(slaveLink, existingBond)
				}
			}
		}
	}
	return nil
}

// probeBondSupport 探测内核对 bonding 模块及特定模式的支持。
func probeBondSupport() (supportsActiveBackup bool, supportsLACP bool) {
	// 检查内核是否加载了 bonding 模块
	if _, err := os.Stat("/sys/class/net/bonding_masters"); err == nil {
		return true, true
	}

	// 探测 Active-Backup 支持
	abLink := netlink.NewLinkBond(netlink.LinkAttrs{Name: "argus_probe_ab"})
	abLink.Mode = netlink.BOND_MODE_ACTIVE_BACKUP
	if err := netlink.LinkAdd(abLink); err == nil {
		_ = netlink.LinkDel(abLink)
		supportsActiveBackup = true
	}

	// 探测 802.3ad (LACP) 支持
	lacpLink := netlink.NewLinkBond(netlink.LinkAttrs{Name: "argus_probe_lacp"})
	lacpLink.Mode = netlink.BOND_MODE_802_3AD
	if err := netlink.LinkAdd(lacpLink); err == nil {
		_ = netlink.LinkDel(lacpLink)
		supportsLACP = true
	}

	return supportsActiveBackup, supportsLACP
}
