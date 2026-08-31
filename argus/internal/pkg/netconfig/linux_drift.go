//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/vishvananda/netlink"
)

// LinuxDriftDetector 监听 rtnetlink 路由/地址变化，检测未受管的外部变更。
type LinuxDriftDetector struct {
	mu           sync.RWMutex
	drifted      atomic.Bool
	cancelFunc   context.CancelFunc
	managedLinks map[int]string  // linkIndex -> name
	knownAddrs   map[string]bool // "<linkIndex>:<cidr>" -> true
	knownRoutes  map[string]bool // "<linkIndex>:<dst>:<gw>" -> true
}

func NewLinuxDriftDetector() *LinuxDriftDetector {
	return &LinuxDriftDetector{
		managedLinks: make(map[int]string),
		knownAddrs:   make(map[string]bool),
		knownRoutes:  make(map[string]bool),
	}
}

// IsDrifted 查询当前是否存在外部配置漂移。
func (d *LinuxDriftDetector) IsDrifted() bool {
	return d.drifted.Load()
}

// SetDrifted 显式设置漂移状态。
func (d *LinuxDriftDetector) SetDrifted(drifted bool) {
	d.drifted.Store(drifted)
}

// ClearDrift 清除漂移标记（用于主动接管或收敛恢复后）。
func (d *LinuxDriftDetector) ClearDrift() {
	d.drifted.Store(false)
}

// SyncState 同步当前系统基线中的受管接口、已知地址与已知路由。
func (d *LinuxDriftDetector) SyncState(snapshot HostSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.managedLinks = make(map[int]string)
	d.knownAddrs = make(map[string]bool)
	d.knownRoutes = make(map[string]bool)

	// 解析快照中的受管接口
	for _, iface := range snapshot.Interfaces {
		if iface.Writable || iface.Ownership == OwnershipManaged {
			if link, err := netlink.LinkByName(iface.Name); err == nil {
				idx := link.Attrs().Index
				d.managedLinks[idx] = iface.Name

				if iface.IPv4.Address != nil && iface.IPv4.Prefix != nil {
					cidr := fmt.Sprintf("%s/%d", *iface.IPv4.Address, *iface.IPv4.Prefix)
					d.knownAddrs[fmt.Sprintf("%d:%s", idx, cidr)] = true
				}
				if iface.IPv4.Gateway != nil {
					d.knownRoutes[fmt.Sprintf("%d:default:%s", idx, *iface.IPv4.Gateway)] = true
				}
			}
		}
	}
}

// Start 启动 rtnetlink 订阅协程。
func (d *LinuxDriftDetector) Start(ctx context.Context) {
	d.mu.Lock()
	if d.cancelFunc != nil {
		d.mu.Unlock()
		return
	}

	cCtx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel
	d.mu.Unlock()

	addrCh := make(chan netlink.AddrUpdate, 16)
	routeCh := make(chan netlink.RouteUpdate, 16)
	done := cCtx.Done()

	if err := netlink.AddrSubscribe(addrCh, done); err != nil {
		return
	}
	if err := netlink.RouteSubscribe(routeCh, done); err != nil {
		return
	}

	go func() {
		for {
			select {
			case <-done:
				return
			case update, ok := <-addrCh:
				if !ok {
					return
				}
				// 忽略 IPv6 或链路本地地址
				if update.LinkAddress.IP.To4() == nil || update.LinkAddress.IP.IsLinkLocalUnicast() {
					continue
				}

				d.mu.RLock()
				// 仅检查受管接口
				if _, isManaged := d.managedLinks[update.LinkIndex]; isManaged {
					addrKey := fmt.Sprintf("%d:%s", update.LinkIndex, update.LinkAddress.String())
					if update.NewAddr {
						// 外部新增地址
						if !d.knownAddrs[addrKey] {
							d.drifted.Store(true)
						}
					} else {
						// 外部删除受管地址
						if d.knownAddrs[addrKey] {
							d.drifted.Store(true)
						}
					}
				}
				d.mu.RUnlock()

			case update, ok := <-routeCh:
				if !ok {
					return
				}
				d.mu.RLock()
				if _, isManaged := d.managedLinks[update.Route.LinkIndex]; isManaged {
					// 仅关注默认路由与受管主路由变更
					if update.Route.Dst == nil || update.Route.Dst.IP.Equal(net.IPv4zero) {
						if update.Route.Gw != nil && !update.Route.Gw.IsUnspecified() {
							routeKey := fmt.Sprintf("%d:default:%s", update.Route.LinkIndex, update.Route.Gw.String())
							if !d.knownRoutes[routeKey] {
								d.drifted.Store(true)
							}
						}
					}
				}
				d.mu.RUnlock()
			}
		}
	}()
}

// Stop 停止漂移检测。
func (d *LinuxDriftDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancelFunc != nil {
		d.cancelFunc()
		d.cancelFunc = nil
	}
}
