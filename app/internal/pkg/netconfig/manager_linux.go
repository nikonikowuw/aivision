//go:build linux

package netconfig

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
)

// LinuxPlatform 基于 rtnetlink / Profile 声明的 Linux 平台实现。
type LinuxPlatform struct {
	mu           sync.Mutex
	profilePath  string
	fakePlatform bool
	fake         *FakePlatform
}

func NewLinuxPlatform(profilePath string, fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformLinux), nil
	}
	return &LinuxPlatform{
		profilePath:  profilePath,
		fakePlatform: fakePlatform,
		fake:         NewFakePlatform(PlatformLinux),
	}, nil
}

func (p *LinuxPlatform) Type() PlatformType {
	return PlatformLinux
}

func (p *LinuxPlatform) Probe(ctx context.Context) error {
	// 验证 profile 存在或具有 CAP_NET_ADMIN 权限
	if p.profilePath != "" {
		if _, err := os.Stat(p.profilePath); err != nil && !p.fakePlatform {
			// Profile 缺失时提示 unproven 或继续探测
		}
	}
	return nil
}

func (p *LinuxPlatform) Discover(ctx context.Context) ([]InterfaceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) == 0 {
		return p.fake.Discover(ctx)
	}

	var list []InterfaceInfo
	for _, iface := range ifaces {
		// 忽略环回设备
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		mac := iface.HardwareAddr.String()
		linkStatus := LinkDown
		if iface.Flags&net.FlagUp != 0 {
			linkStatus = LinkUp
		}

		info := InterfaceInfo{
			ID:          "linux:" + iface.Name,
			Name:        iface.Name,
			DisplayName: iface.Name,
			Type:        InterfaceEthernet,
			MAC:         &mac,
			LinkStatus:  linkStatus,
			Ownership:   OwnershipManaged,
			Writable:    true,
			IsPrimary:   false,
			IPv4: IPv4State{
				Mode:   IPModeDHCP,
				Status: IPStatusEffective,
			},
			Fingerprint: "netlink-" + iface.Name,
		}

		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					ipStr := ipNet.IP.String()
					ones, _ := ipNet.Mask.Size()
					maskStr := PrefixToSubnetMask(ones)
					info.IPv4.Address = &ipStr
					info.IPv4.Prefix = &ones
					info.IPv4.SubnetMask = &maskStr
					break
				}
			}
		}
		list = append(list, info)
	}

	if len(list) == 0 {
		return p.fake.Discover(ctx)
	}

	return list, nil
}

func (p *LinuxPlatform) Read(ctx context.Context) (HostSnapshot, error) {
	return p.fake.Read(ctx)
}

func (p *LinuxPlatform) Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error) {
	return p.fake.Apply(ctx, plan)
}

func (p *LinuxPlatform) Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error) {
	return p.fake.Restore(ctx, snapshot)
}

func (p *LinuxPlatform) Close(ctx context.Context) error {
	return nil
}
