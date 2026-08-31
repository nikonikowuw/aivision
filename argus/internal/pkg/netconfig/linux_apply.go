//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"net"
	"slices"

	"github.com/vishvananda/netlink"
)

type netlinkOps interface {
	LinkByName(name string) (netlink.Link, error)
	LinkSetUp(link netlink.Link) error
	LinkSetDown(link netlink.Link) error
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	AddrDel(link netlink.Link, addr *netlink.Addr) error
	RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	RouteAdd(route *netlink.Route) error
	RouteDel(route *netlink.Route) error
	RouteReplace(route *netlink.Route) error
}

type realNetlinkOps struct{}

func (realNetlinkOps) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (realNetlinkOps) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (realNetlinkOps) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (realNetlinkOps) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (realNetlinkOps) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (realNetlinkOps) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (realNetlinkOps) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}

func (realNetlinkOps) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (realNetlinkOps) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (realNetlinkOps) RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}

// applyLinuxPlan 按照补偿顺序应用 HostPlan。
func applyLinuxPlan(
	ctx context.Context,
	ops netlinkOps,
	plan HostPlan,
	profile *LinuxProfile,
	anchors *AnchorStore,
	dnsPath string,
	currentSnapshot HostSnapshot,
) (HostSnapshot, error) {
	for ifID := range plan.Interfaces {
		// bond0 虚拟接口在 Bond 模式下自动受管
		if plan.Mode.IsBond() && ifID == "bond0" {
			continue
		}
		if profile != nil && !profile.IsAllowlisted(ifID) {
			return HostSnapshot{}, fmt.Errorf("%w: interface %s not in profile allowlist", ErrInterfaceNotManaged, ifID)
		}
	}

	undoStack := make([]func() error, 0)
	rollback := func() {
		for i := len(undoStack) - 1; i >= 0; i-- {
			_ = undoStack[i]()
		}
	}

	// 步骤 0: 协调 Bond/LACP 拓扑
	if plan.Mode != "" && (plan.Mode != currentSnapshot.Mode || plan.Bond != nil) {
		if err := reconcileLinuxBond(ctx, plan, currentSnapshot); err != nil {
			return HostSnapshot{}, fmt.Errorf("%w: reconcile bond topology: %v", ErrApplyFailed, err)
		}
		undoStack = append(undoStack, func() error {
			return rollbackLinuxBond(currentSnapshot)
		})
	}

	// 步骤 1: 链路 UP
	for ifID := range plan.Interfaces {
		link, err := ops.LinkByName(ifID)
		if err != nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: link by name %s: %v", ErrApplyFailed, ifID, err)
		}
		wasUp := link.Attrs().Flags&net.FlagUp != 0
		if !wasUp {
			if err := ops.LinkSetUp(link); err != nil {
				rollback()
				return HostSnapshot{}, fmt.Errorf("%w: link set up %s: %v", ErrApplyFailed, ifID, err)
			}
			undoStack = append(undoStack, func() error {
				return ops.LinkSetDown(link)
			})
		}
	}

	// 步骤 2: 地址精确替换 (AddrDel old, AddrAdd new)
	for ifID, ifPlan := range plan.Interfaces {
		link, err := ops.LinkByName(ifID)
		if err != nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: link by name %s: %v", ErrApplyFailed, ifID, err)
		}
		currentAddrs, err := ops.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: list addrs for %s: %v", ErrApplyFailed, ifID, err)
		}

		var desiredAddrs []netlink.Addr
		if ifPlan.Mode == IPModeStatic && ifPlan.Address != nil && ifPlan.Prefix != nil {
			cidr := fmt.Sprintf("%s/%d", *ifPlan.Address, *ifPlan.Prefix)
			addr, err := netlink.ParseAddr(cidr)
			if err != nil {
				rollback()
				return HostSnapshot{}, fmt.Errorf("%w: parse addr %s: %v", ErrInvalidConfig, cidr, err)
			}
			desiredAddrs = append(desiredAddrs, *addr)
		}

		toAdd, toDel := diffAddresses(currentAddrs, desiredAddrs)

		// 删除多余地址并入栈逆操作
		for _, oldAddr := range toDel {
			oldAddrCopy := oldAddr
			if err := ops.AddrDel(link, &oldAddrCopy); err != nil {
				rollback()
				return HostSnapshot{}, fmt.Errorf("%w: del addr %s on %s: %v", ErrApplyFailed, oldAddrCopy.String(), ifID, err)
			}
			undoStack = append(undoStack, func() error {
				return ops.AddrAdd(link, &oldAddrCopy)
			})
		}

		// 增加新地址并入栈逆操作
		for _, newAddr := range toAdd {
			newAddrCopy := newAddr
			if err := ops.AddrAdd(link, &newAddrCopy); err != nil {
				rollback()
				return HostSnapshot{}, fmt.Errorf("%w: add addr %s on %s: %v", ErrApplyFailed, newAddrCopy.String(), ifID, err)
			}
			undoStack = append(undoStack, func() error {
				return ops.AddrDel(link, &newAddrCopy)
			})
		}
	}

	// 步骤 3: 直连路由（内核自动维护）

	// 步骤 4 & 5: 默认路由迁移（先加新，后删旧）
	var primaryIfID string
	if plan.PrimaryInterfaceID != nil {
		primaryIfID = *plan.PrimaryInterfaceID
	}

	var primaryGW string
	if primaryIfID != "" {
		if ifPlan, ok := plan.Interfaces[primaryIfID]; ok && ifPlan.Gateway != nil {
			primaryGW = *ifPlan.Gateway
		}
	}

	var newDefaultRoute *netlink.Route
	if primaryIfID != "" && primaryGW != "" {
		pLink, err := ops.LinkByName(primaryIfID)
		if err != nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: link by name for primary %s: %v", ErrApplyFailed, primaryIfID, err)
		}
		gwIP := net.ParseIP(primaryGW)
		if gwIP == nil || gwIP.To4() == nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: invalid gateway IP %s", ErrInvalidConfig, primaryGW)
		}
		newDefaultRoute = &netlink.Route{
			LinkIndex: pLink.Attrs().Index,
			Gw:        gwIP,
			Priority:  100,
		}
		if err := ops.RouteAdd(newDefaultRoute); err != nil {
			if errReplace := ops.RouteReplace(newDefaultRoute); errReplace != nil {
				rollback()
				return HostSnapshot{}, fmt.Errorf("%w: add default route via %s on %s: %v", ErrApplyFailed, primaryGW, primaryIfID, errReplace)
			}
		}
		undoStack = append(undoStack, func() error {
			return ops.RouteDel(newDefaultRoute)
		})
	}

	// 清理非 primary 接口上的旧默认路由
	allRoutes, err := ops.RouteList(nil, netlink.FAMILY_V4)
	if err == nil {
		for _, r := range allRoutes {
			if r.Dst == nil || r.Dst.IP.Equal(net.IPv4zero) {
				if newDefaultRoute == nil || r.LinkIndex != newDefaultRoute.LinkIndex || !r.Gw.Equal(newDefaultRoute.Gw) {
					isManagedOld := false
					for ifID := range plan.Interfaces {
						l, _ := ops.LinkByName(ifID)
						if l != nil && l.Attrs().Index == r.LinkIndex {
							isManagedOld = true
							break
						}
					}
					if isManagedOld {
						rCopy := r
						if err := ops.RouteDel(&rCopy); err == nil {
							undoStack = append(undoStack, func() error {
								return ops.RouteAdd(&rCopy)
							})
						}
					}
				}
			}
		}
	}

	// 步骤 6: DNS 原子写入
	var primaryDNS []string
	if primaryIfID != "" {
		if ifPlan, ok := plan.Interfaces[primaryIfID]; ok {
			primaryDNS = ifPlan.DNSServers
		}
	}
	if len(primaryDNS) > 0 {
		oldDNSContent, err := writeResolvConf(dnsPath, primaryDNS)
		if err != nil {
			rollback()
			return HostSnapshot{}, fmt.Errorf("%w: write resolv.conf: %v", ErrApplyFailed, err)
		}
		undoStack = append(undoStack, func() error {
			return restoreResolvConf(dnsPath, oldDNSContent)
		})
	}

	newSnap, _, err := readLinuxSnapshot(ctx, profile, anchors, &currentSnapshot)
	if err != nil {
		rollback()
		return HostSnapshot{}, fmt.Errorf("%w: read snapshot after apply: %v", ErrApplyFailed, err)
	}
	return newSnap, nil
}

// restoreLinuxSnapshot 收敛式恢复目标快照。
func restoreLinuxSnapshot(
	ctx context.Context,
	ops netlinkOps,
	target HostSnapshot,
	profile *LinuxProfile,
	anchors *AnchorStore,
	dnsPath string,
) (HostSnapshot, error) {
	// 恢复 Bond 拓扑
	_ = rollbackLinuxBond(target)

	for ifID, ifTarget := range target.Interfaces {
		link, err := ops.LinkByName(ifID)
		if err != nil {
			continue
		}
		if ifTarget.LinkStatus == LinkUp {
			_ = ops.LinkSetUp(link)
		}

		currentAddrs, _ := ops.AddrList(link, netlink.FAMILY_V4)

		var desiredAddrs []netlink.Addr
		if ifTarget.IPv4.Address != nil && ifTarget.IPv4.Prefix != nil {
			cidr := fmt.Sprintf("%s/%d", *ifTarget.IPv4.Address, *ifTarget.IPv4.Prefix)
			if addr, err := netlink.ParseAddr(cidr); err == nil {
				desiredAddrs = append(desiredAddrs, *addr)
			}
		}

		toAdd, toDel := diffAddresses(currentAddrs, desiredAddrs)
		for _, oldAddr := range toDel {
			oldAddrCopy := oldAddr
			_ = ops.AddrDel(link, &oldAddrCopy)
		}
		for _, newAddr := range toAdd {
			newAddrCopy := newAddr
			_ = ops.AddrAdd(link, &newAddrCopy)
		}
	}

	if target.PrimaryInterfaceID != nil {
		pID := *target.PrimaryInterfaceID
		if pLink, err := ops.LinkByName(pID); err == nil {
			if pIf, ok := target.Interfaces[pID]; ok && pIf.IPv4.Gateway != nil {
				gwIP := net.ParseIP(*pIf.IPv4.Gateway)
				if gwIP != nil && gwIP.To4() != nil {
					defRoute := &netlink.Route{
						LinkIndex: pLink.Attrs().Index,
						Gw:        gwIP,
						Priority:  100,
					}
					_ = ops.RouteReplace(defRoute)
				}
			}
		}
	}

	if len(target.SystemDNSServers) > 0 {
		_, _ = writeResolvConf(dnsPath, target.SystemDNSServers)
	}

	snap, _, err := readLinuxSnapshot(ctx, profile, anchors, &target)
	if err != nil {
		return target, nil
	}
	return snap, nil
}

// diffAddresses 计算当前地址与期望地址的增删差异（toAdd 为需新增地址，toDel 为需删除地址）。
func diffAddresses(current, desired []netlink.Addr) (toAdd, toDel []netlink.Addr) {
	for _, oldAddr := range current {
		found := slices.ContainsFunc(desired, func(d netlink.Addr) bool {
			return oldAddr.IPNet.String() == d.IPNet.String()
		})
		if !found {
			toDel = append(toDel, oldAddr)
		}
	}
	for _, newAddr := range desired {
		found := slices.ContainsFunc(current, func(c netlink.Addr) bool {
			return newAddr.IPNet.String() == c.IPNet.String()
		})
		if !found {
			toAdd = append(toAdd, newAddr)
		}
	}
	return toAdd, toDel
}
