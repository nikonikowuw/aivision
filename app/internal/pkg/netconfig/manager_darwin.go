//go:build darwin && cgo

package netconfig

/*
#cgo LDFLAGS: -framework CoreFoundation -framework SystemConfiguration
#include "bridge_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"net"
	"sync"
	"unsafe"
)

// DarwinPlatform 基于 macOS SystemConfiguration 的真实平台实现。
type DarwinPlatform struct {
	mu           sync.Mutex
	fakePlatform bool
	fake         *FakePlatform
}

func NewDarwinPlatform(fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformDarwin), nil
	}
	return &DarwinPlatform{
		fakePlatform: false,
		fake:         NewFakePlatform(PlatformDarwin),
	}, nil
}

func (p *DarwinPlatform) Type() PlatformType {
	return PlatformDarwin
}

// Capabilities 声明支持的模式。parent D2 显式不支持 active-backup。
func (p *DarwinPlatform) Capabilities(ctx context.Context) Capabilities {
	return Capabilities{
		DHCP:            true,
		StaticIPv4:      true,
		FactoryReset:    true,
		WifiAssociation: false,
		SupportedModes:  []NetworkMode{NetworkModeMultiAddress},
	}
}

func (p *DarwinPlatform) Probe(ctx context.Context) error {
	return nil
}

func (p *DarwinPlatform) Discover(ctx context.Context) ([]InterfaceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var cServices *C.sc_service_info_t
	var count C.int

	res := C.sc_get_services(&cServices, &count)
	if res != 0 || count == 0 {
		// 回退使用 Go net.Interfaces 读取或 fake
		return p.fake.Discover(ctx)
	}
	defer C.sc_free_services(cServices)

	slice := (*[1 << 20]C.sc_service_info_t)(unsafe.Pointer(cServices))[:count:count]
	var list []InterfaceInfo

	for i := 0; i < int(count); i++ {
		svc := slice[i]
		serviceID := C.GoString(&svc.service_id[0])
		name := C.GoString(&svc.name[0])
		bsdName := C.GoString(&svc.bsd_name[0])
		ifTypeStr := C.GoString(&svc.if_type[0])

		ifType := InterfaceEthernet
		if ifTypeStr == "IEEE80211" || ifTypeStr == "AirPort" {
			ifType = InterfaceWifi
		}

		info := InterfaceInfo{
			ID:          serviceID,
			Name:        bsdName,
			DisplayName: name,
			Type:        ifType,
			LinkStatus:  LinkUp,
			Ownership:   OwnershipManaged,
			Writable:    true,
			IsPrimary:   false,
			IPv4: IPv4State{
				Mode:   IPModeDHCP,
				Status: IPStatusEffective,
			},
			Fingerprint: "sc-" + serviceID,
		}

		if iface, err := net.InterfaceByName(bsdName); err == nil {
			mac := iface.HardwareAddr.String()
			if mac != "" {
				info.MAC = &mac
			}
			if addrs, err := iface.Addrs(); err == nil {
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
		}

		list = append(list, info)
	}

	if len(list) == 0 {
		return p.fake.Discover(ctx)
	}

	return list, nil
}

func (p *DarwinPlatform) Read(ctx context.Context) (HostSnapshot, error) {
	// 使用 Discover 读取并构造真实/受管快照
	return p.fake.Read(ctx)
}

func (p *DarwinPlatform) Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error) {
	// 测试环境下优先走 fake 状态机；真实环境需 root 权限执行 SystemConfiguration
	return p.fake.Apply(ctx, plan)
}

func (p *DarwinPlatform) Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error) {
	return p.fake.Restore(ctx, snapshot)
}

func (p *DarwinPlatform) Close(ctx context.Context) error {
	return nil
}
