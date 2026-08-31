//go:build linux

package netconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
)

// Linux 内核网络设备标志常量
const (
	// IFF_RUNNING 对应 Linux 内核 netdevice 标志位 (1<<6 = 0x40)，表示 RFC2863 operstate UP 或 carrier 就绪
	IFF_RUNNING = 0x40
)

// LinuxNativeData 保存平台原生网络状态，用于补偿与指纹校验。
type LinuxNativeData struct {
	Interfaces []NativeInterfaceTuple `json:"interfaces"`
	Routes     []NativeRouteTuple     `json:"routes"`
	DNSHash    string                 `json:"dnsHash"`
}

type NativeInterfaceTuple struct {
	Name      string   `json:"name"`
	IfIndex   int      `json:"ifIndex"`
	Addresses []string `json:"addresses"` // CIDRs, e.g. "192.168.1.100/24"
	AdminUp   bool     `json:"adminUp"`
	PermMAC   string   `json:"permMAC,omitempty"`
}

type NativeRouteTuple struct {
	Dst     string `json:"dst,omitempty"`
	Gw      string `json:"gw,omitempty"`
	Link    string `json:"link"`
	Metric  int    `json:"metric"`
	Default bool   `json:"default"`
}

// readSysfsSpeedAndDuplex 读取 sysfs 下的网卡速率和双工信息。
func readSysfsSpeedAndDuplex(name string) (*int, InterfaceDuplex) {
	duplex := DuplexUnknown
	if dBytes, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/duplex", name)); err == nil {
		dStr := strings.TrimSpace(string(dBytes))
		switch dStr {
		case "full":
			duplex = DuplexFull
		case "half":
			duplex = DuplexHalf
		}
	}

	var speedPtr *int
	if sBytes, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", name)); err == nil {
		sStr := strings.TrimSpace(string(sBytes))
		var speedVal int
		if _, err := fmt.Sscanf(sStr, "%d", &speedVal); err == nil && speedVal > 0 {
			speedPtr = &speedVal
		}
	}
	return speedPtr, duplex
}

// readLinuxSnapshot 执行单次 netlink dump 并组装 HostSnapshot 与 InterfaceInfo 列表。
func readLinuxSnapshot(
	ctx context.Context,
	profile *LinuxProfile,
	anchors *AnchorStore,
	lastValid *HostSnapshot,
) (HostSnapshot, []InterfaceInfo, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return HostSnapshot{}, nil, fmt.Errorf("netlink link list: %w", err)
	}

	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return HostSnapshot{}, nil, fmt.Errorf("netlink route list: %w", err)
	}

	// 建立 link ifindex -> name 索引
	linkNameByIndex := make(map[int]string, len(links))
	for _, link := range links {
		linkNameByIndex[link.Attrs().Index] = link.Attrs().Name
	}

	// 识别 primary 接口与默认路由
	var primaryIfID string
	var defaultRouteIfID string
	bestMetric := math.MaxInt

	for _, route := range routes {
		if route.Dst == nil || route.Dst.IP.Equal(net.IPv4zero) {
			if route.Gw != nil && !route.Gw.IsUnspecified() {
				if ifID, ok := linkNameByIndex[route.LinkIndex]; ok {
					if route.Priority < bestMetric {
						bestMetric = route.Priority
						primaryIfID = ifID
						defaultRouteIfID = ifID
					}
				}
			}
		}
	}

	var infos []InterfaceInfo
	ifaceMap := make(map[string]InterfaceInfo)
	var nativeInterfaces []NativeInterfaceTuple
	var nativeRoutes []NativeRouteTuple

	for _, link := range links {
		attrs := link.Attrs()
		if attrs.Flags&net.FlagLoopback != 0 {
			continue
		}

		name := attrs.Name
		ifID := name

		macStr := attrs.HardwareAddr.String()
		var macPtr *string
		if macStr != "" {
			macPtr = &macStr
		}

		permMAC := attrs.PermHWAddr.String()
		if permMAC == "" {
			permMAC = macStr
		}

		// LinkStatus: carrier/admin
		linkStatus := LinkDown
		if attrs.Flags&net.FlagUp != 0 {
			if attrs.OperState == netlink.OperUp || attrs.RawFlags&IFF_RUNNING != 0 || attrs.OperState == netlink.OperUnknown {
				linkStatus = LinkUp
			}
		}

		// Speed & Duplex
		speedPtr, duplex := readSysfsSpeedAndDuplex(name)

		// 接口类型推断
		ifType := InterfaceEthernet
		linkType := link.Type()
		if strings.Contains(strings.ToLower(linkType), "wireless") || strings.Contains(strings.ToLower(linkType), "wifi") {
			ifType = InterfaceWifi
		}

		isBond := (linkType == "bond")
		var masterID *string
		if attrs.MasterIndex > 0 {
			for _, mLink := range links {
				if mLink.Attrs().Index == attrs.MasterIndex {
					mName := mLink.Attrs().Name
					masterID = &mName
					break
				}
			}
		}

		// 所有权与可写性判断
		ownership := OwnershipManaged
		writable := true

		if isBond && name == "bond0" {
			// bond0 为 Argus 模式管理的虚拟聚合接口，始终受管且可写
			ownership = OwnershipManaged
			writable = true
		} else if profile != nil {
			if profile.IsAllowlisted(name) {
				if anchors != nil && permMAC != "" {
					ok, err := anchors.CheckOrAnchor(name, permMAC)
					if !ok || err != nil {
						ownership = OwnershipConflict
						writable = false
					}
				}
			} else {
				ownership = OwnershipUnsupported
				writable = false
			}
		} else {
			if anchors != nil && permMAC != "" {
				_, _ = anchors.CheckOrAnchor(name, permMAC)
			}
		}

		// 获取接口 IPv4 地址
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			return HostSnapshot{}, nil, fmt.Errorf("netlink addr list for %s: %w", name, err)
		}

		var addrStrings []string
		var ipv4State IPv4State
		ipv4State.Mode = IPModeUnknown
		ipv4State.Status = IPStatusEffective

		if lastValid != nil {
			if lastIf, ok := lastValid.Interfaces[ifID]; ok {
				ipv4State.Mode = lastIf.IPv4.Mode
			}
		}
		if ipv4State.Mode == IPModeUnknown {
			ipv4State.Mode = IPModeStatic
		}

		for _, addr := range addrs {
			ip := addr.IPNet.IP
			if ip.To4() != nil {
				ipStr := ip.String()
				ones, _ := addr.IPNet.Mask.Size()
				maskStr := PrefixToSubnetMask(ones)
				cidrStr := fmt.Sprintf("%s/%d", ipStr, ones)
				addrStrings = append(addrStrings, cidrStr)

				if ipv4State.Address == nil {
					ipv4State.Address = &ipStr
					ipv4State.Prefix = &ones
					ipv4State.SubnetMask = &maskStr
				}
			}
		}

		// 检查该接口是否有默认网关
		for _, route := range routes {
			if route.LinkIndex == attrs.Index && (route.Dst == nil || route.Dst.IP.Equal(net.IPv4zero)) {
				if route.Gw != nil && !route.Gw.IsUnspecified() {
					gwStr := route.Gw.String()
					ipv4State.Gateway = &gwStr
				}
			}
		}

		isPrimary := (ifID == primaryIfID)

		info := InterfaceInfo{
			ID:          ifID,
			Name:        name,
			DisplayName: name,
			Type:        ifType,
			MAC:         macPtr,
			LinkStatus:  linkStatus,
			Ownership:   ownership,
			Writable:    writable,
			IsPrimary:   isPrimary,
			IsBond:      isBond,
			MasterID:    masterID,
			SpeedMbps:   speedPtr,
			Duplex:      duplex,
			IPv4:        ipv4State,
			Fingerprint: fmt.Sprintf("netlink-%s-%s", name, strings.Join(addrStrings, ",")),
		}
		infos = append(infos, info)
		ifaceMap[ifID] = info

		nativeInterfaces = append(nativeInterfaces, NativeInterfaceTuple{
			Name:      name,
			IfIndex:   attrs.Index,
			Addresses: addrStrings,
			AdminUp:   attrs.Flags&net.FlagUp != 0,
			PermMAC:   permMAC,
		})
	}

	// 记录路由元组
	for _, route := range routes {
		linkName := linkNameByIndex[route.LinkIndex]
		var dstStr string
		isDef := (route.Dst == nil || route.Dst.IP.Equal(net.IPv4zero))
		if !isDef {
			dstStr = route.Dst.String()
		}
		var gwStr string
		if route.Gw != nil {
			gwStr = route.Gw.String()
		}
		nativeRoutes = append(nativeRoutes, NativeRouteTuple{
			Dst:     dstStr,
			Gw:      gwStr,
			Link:    linkName,
			Metric:  route.Priority,
			Default: isDef,
		})
	}

	// 读取 DNS
	dnsPath := "/etc/resolv.conf"
	if profile != nil && profile.Resolver.Path != "" {
		dnsPath = profile.Resolver.Path
	}
	dnsHash := ""
	var systemDNS []string
	if resolvBytes, err := os.ReadFile(dnsPath); err == nil {
		h := sha256.Sum256(resolvBytes)
		dnsHash = hex.EncodeToString(h[:])
		lines := strings.Split(string(resolvBytes), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "nameserver") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					systemDNS = append(systemDNS, fields[1])
				}
			}
		}
	}

	nativeData := LinuxNativeData{
		Interfaces: nativeInterfaces,
		Routes:     nativeRoutes,
		DNSHash:    dnsHash,
	}
	nativeJSON, _ := json.Marshal(nativeData)

	fpHash := sha256.New()
	fpHash.Write(nativeJSON)
	globalFingerprint := hex.EncodeToString(fpHash.Sum(nil))

	var primaryPtr *string
	if primaryIfID != "" {
		primaryPtr = &primaryIfID
	}
	var defaultRoutePtr *string
	if defaultRouteIfID != "" {
		defaultRoutePtr = &defaultRouteIfID
	}

	snapshot := HostSnapshot{
		PrimaryInterfaceID:      primaryPtr,
		DefaultRouteInterfaceID: defaultRoutePtr,
		SystemDNSServers:        systemDNS,
		Interfaces:              ifaceMap,
		Native: NativeSnapshot{
			Version: 1,
			Data:    nativeJSON,
		},
		Fingerprint: globalFingerprint,
	}

	return snapshot, infos, nil
}
