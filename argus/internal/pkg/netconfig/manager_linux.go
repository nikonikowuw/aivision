//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/vishvananda/netlink"
)

// LinuxPlatform 基于 rtnetlink / Profile 声明的 Linux 平台实现。
type LinuxPlatform struct {
	mu             sync.Mutex
	profilePath    string
	stateDir       string
	fakePlatform   bool
	profile        *LinuxProfile
	anchors        *AnchorStore
	probed         bool
	supportedModes []NetworkMode
	lastSnapshot   *HostSnapshot
	dhcpClients    map[string]*LinuxDHCPClient
	driftDetector  *LinuxDriftDetector
}

func NewLinuxPlatform(profilePath string, stateDir string, fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformLinux), nil
	}

	var profile *LinuxProfile
	if profilePath != "" {
		var err error
		profile, err = LoadProfile(profilePath)
		if err != nil {
			return nil, fmt.Errorf("load linux network profile: %w", err)
		}
	}

	var anchors *AnchorStore
	if stateDir != "" {
		var err error
		anchors, err = NewAnchorStore(stateDir)
		if err != nil {
			return nil, fmt.Errorf("init linux anchor store: %w", err)
		}
	}

	return &LinuxPlatform{
		profilePath:   profilePath,
		stateDir:      stateDir,
		fakePlatform:  fakePlatform,
		profile:       profile,
		anchors:       anchors,
		dhcpClients:   make(map[string]*LinuxDHCPClient),
		driftDetector: NewLinuxDriftDetector(),
	}, nil
}

func (p *LinuxPlatform) Type() PlatformType {
	return PlatformLinux
}

// Capabilities 声明支持的模式。Probe 成功后声明 multi-address 与 gateway。
func (p *LinuxPlatform) Capabilities(ctx context.Context) Capabilities {
	p.mu.Lock()
	defer p.mu.Unlock()

	modes := []NetworkMode{NetworkModeMultiAddress}
	if p.probed && len(p.supportedModes) > 0 {
		modes = p.supportedModes
	}

	return Capabilities{
		DHCP:            true,
		StaticIPv4:      true,
		FactoryReset:    true,
		WifiAssociation: false,
		SupportedModes:  modes,
	}
}

func (p *LinuxPlatform) Probe(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. 验证特权 (root/CAP_NET_ADMIN)
	if os.Geteuid() != 0 && !p.fakePlatform {
		return fmt.Errorf("%w: root (uid 0) required for linux netconfig platform", ErrInsufficientPrivileges)
	}

	// 2. 验证 profile 与 resolver 独占性
	if p.profile != nil {
		if err := p.profile.CheckResolverExclusive(); err != nil {
			return err
		}
	}

	// 3. 验证 netlink 可通信
	if _, err := netlink.LinkList(); err != nil {
		return fmt.Errorf("netlink communication failed: %w", err)
	}

	p.probed = true
	modes := []NetworkMode{
		NetworkModeMultiAddress,
		NetworkModeGateway,
	}

	ab, lacp := probeBondSupport()
	if ab {
		modes = append(modes, NetworkModeActiveBackup)
	}
	if lacp {
		modes = append(modes, NetworkModeLACP)
	}

	p.supportedModes = modes

	// 启动漂移检测
	if p.driftDetector != nil {
		p.driftDetector.Start(ctx)
	}

	return nil
}

func (p *LinuxPlatform) Discover(ctx context.Context) ([]InterfaceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, infos, err := readLinuxSnapshot(ctx, p.profile, p.anchors, p.lastSnapshot)
	if err != nil {
		return nil, err
	}
	return infos, nil
}

func (p *LinuxPlatform) Read(ctx context.Context) (HostSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap, _, err := readLinuxSnapshot(ctx, p.profile, p.anchors, p.lastSnapshot)
	if err != nil {
		return HostSnapshot{}, err
	}
	p.lastSnapshot = &snap

	// 同步漂移检测基线
	if p.driftDetector != nil {
		p.driftDetector.SyncState(snap)
	}

	return snap, nil
}

func (p *LinuxPlatform) Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var currentSnap HostSnapshot
	if p.lastSnapshot != nil {
		currentSnap = *p.lastSnapshot
	} else {
		var err error
		currentSnap, _, err = readLinuxSnapshot(ctx, p.profile, p.anchors, nil)
		if err != nil {
			return HostSnapshot{}, fmt.Errorf("read current snapshot before apply: %w", err)
		}
	}

	dnsPath := "/etc/resolv.conf"
	if p.profile != nil && p.profile.Resolver.Path != "" {
		dnsPath = p.profile.Resolver.Path
	}

	snap, err := applyLinuxPlan(ctx, realNetlinkOps{}, plan, p.profile, p.anchors, dnsPath, currentSnap)
	if err != nil {
		return HostSnapshot{}, err
	}
	p.lastSnapshot = &snap

	// 编排 DHCP 客户端生命周期
	dhcpIfaces := make(map[string]bool, len(plan.Interfaces))
	for ifID, ifPlan := range plan.Interfaces {
		dhcpIfaces[ifID] = (ifPlan.Mode == IPModeDHCP)
	}
	var primaryIfID string
	if plan.PrimaryInterfaceID != nil {
		primaryIfID = *plan.PrimaryInterfaceID
	}
	p.syncDHCPClients(ctx, dhcpIfaces, primaryIfID)

	// 重新同步漂移基线
	if p.driftDetector != nil {
		p.driftDetector.SyncState(snap)
	}

	return snap, nil
}

func (p *LinuxPlatform) syncDHCPClients(ctx context.Context, dhcpIfaces map[string]bool, primaryIfID string) {
	for ifID, isDHCP := range dhcpIfaces {
		if isDHCP {
			client, exists := p.dhcpClients[ifID]
			if !exists {
				client = NewLinuxDHCPClient(ifID)
				p.dhcpClients[ifID] = client
			}
			isPrimary := (ifID == primaryIfID)
			_ = client.Start(ctx, func(lease DHCPLeaseInfo) {
				p.handleDHCPLeaseAcquired(ifID, lease, isPrimary)
			})
		} else {
			if client, exists := p.dhcpClients[ifID]; exists {
				client.Stop()
				delete(p.dhcpClients, ifID)
			}
		}
	}
}

func (p *LinuxPlatform) handleDHCPLeaseAcquired(ifName string, lease DHCPLeaseInfo, isPrimary bool) {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return
	}

	// 1. 设置 DHCP 分配的 IP 地址
	addr := &netlink.Addr{
		IPNet: &lease.IPNet,
	}
	_ = netlink.AddrReplace(link, addr)

	// 2. 如果为主接口，更新默认网关与 DNS
	if isPrimary && lease.Gateway != nil {
		defRoute := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Gw:        lease.Gateway,
			Priority:  100,
		}
		_ = netlink.RouteReplace(defRoute)

		if len(lease.DNS) > 0 {
			dnsPath := "/etc/resolv.conf"
			if p.profile != nil && p.profile.Resolver.Path != "" {
				dnsPath = p.profile.Resolver.Path
			}
			var dnsStrings []string
			for _, dnsIP := range lease.DNS {
				dnsStrings = append(dnsStrings, dnsIP.String())
			}
			_, _ = writeResolvConf(dnsPath, dnsStrings)
		}
	}
}

func (p *LinuxPlatform) Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	dnsPath := "/etc/resolv.conf"
	if p.profile != nil && p.profile.Resolver.Path != "" {
		dnsPath = p.profile.Resolver.Path
	}

	snap, err := restoreLinuxSnapshot(ctx, realNetlinkOps{}, snapshot, p.profile, p.anchors, dnsPath)
	if err != nil {
		return HostSnapshot{}, err
	}
	p.lastSnapshot = &snap

	// 同步 DHCP 客户端
	dhcpIfaces := make(map[string]bool, len(snapshot.Interfaces))
	for ifID, ifTarget := range snapshot.Interfaces {
		dhcpIfaces[ifID] = (ifTarget.IPv4.Mode == IPModeDHCP)
	}
	var primaryIfID string
	if snapshot.PrimaryInterfaceID != nil {
		primaryIfID = *snapshot.PrimaryInterfaceID
	}
	p.syncDHCPClients(ctx, dhcpIfaces, primaryIfID)

	// 恢复后清除漂移状态并同步新基线
	if p.driftDetector != nil {
		p.driftDetector.ClearDrift()
		p.driftDetector.SyncState(snap)
	}

	return snap, nil
}

func (p *LinuxPlatform) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, client := range p.dhcpClients {
		client.Stop()
	}
	p.dhcpClients = make(map[string]*LinuxDHCPClient)

	if p.driftDetector != nil {
		p.driftDetector.Stop()
	}
	return nil
}
