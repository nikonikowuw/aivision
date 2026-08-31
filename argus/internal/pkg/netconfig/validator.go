package netconfig

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrInvalidConfig          = errors.New("invalid network configuration")
	ErrGatewayPoolInvalid     = errors.New("invalid gateway address pool configuration")
	ErrDhcpServerConflict     = errors.New("dhcp server conflict detected on interface")
	ErrTransactionPending     = errors.New("network transaction already pending")
	ErrTransactionNotFound    = errors.New("network transaction not found")
	ErrTransactionExpired     = errors.New("network transaction expired")
	ErrInterfaceNotManaged    = errors.New("network interface not managed or writable")
	ErrOwnershipConflict      = errors.New("network ownership conflict")
	ErrUnsupported            = errors.New("network platform unsupported")
	ErrApplyFailed            = errors.New("network apply failed")
	ErrRecoveryFailed         = errors.New("network recovery failed")
	ErrStateCorrupt           = errors.New("network state file corrupt")
	ErrExternalDrift          = errors.New("network external drift detected")
	ErrNotReady               = errors.New("network service not ready")
	ErrLacpNegotiationFailed  = errors.New("network lacp negotiation failed")
	ErrInsufficientPrivileges = errors.New("network insufficient privileges")
	ErrResolverConflict       = errors.New("network resolver conflict")
	ErrProfileInvalid         = errors.New("network profile invalid")
)

// NormalizeAndValidateIPv4 校验并规范化 IPv4 与 Prefix，计算子网掩码。
func NormalizeAndValidateIPv4(ipStr string, prefix int) (string, string, error) {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil || ip.To4() == nil {
		return "", "", fmt.Errorf("%w: invalid IPv4 address %q", ErrInvalidConfig, ipStr)
	}
	ipv4 := ip.To4()

	if prefix < 0 || prefix > 32 {
		return "", "", fmt.Errorf("%w: invalid prefix %d (must be 0..32)", ErrInvalidConfig, prefix)
	}

	// 排除 0.0.0.0, 255.255.255.255, 环回, 组播
	if ipv4.IsUnspecified() || ipv4.IsLoopback() || ipv4.IsMulticast() || ipv4.Equal(net.IPv4bcast) {
		return "", "", fmt.Errorf("%w: address %q is not a valid unicast host address", ErrInvalidConfig, ipStr)
	}

	mask := net.CIDRMask(prefix, 32)
	maskStr := fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])

	// 检查网络地址和广播地址（仅针对 /1 ~ /30）
	if prefix > 0 && prefix < 31 {
		networkIP := ipv4.Mask(mask)
		broadcastIP := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			broadcastIP[i] = networkIP[i] | ^mask[i]
		}
		if ipv4.Equal(networkIP) {
			return "", "", fmt.Errorf("%w: address %q cannot be the network address for /%d", ErrInvalidConfig, ipStr, prefix)
		}
		if ipv4.Equal(broadcastIP) {
			return "", "", fmt.Errorf("%w: address %q cannot be the broadcast address for /%d", ErrInvalidConfig, ipStr, prefix)
		}
	}

	return ipv4.String(), maskStr, nil
}

// ValidateGatewayInSubnet 检查 Gateway 是否在 IP 与 Prefix 对应的同子网中。
func ValidateGatewayInSubnet(ipStr string, prefix int, gwStr string) (string, error) {
	gw := net.ParseIP(strings.TrimSpace(gwStr))
	if gw == nil || gw.To4() == nil {
		return "", fmt.Errorf("%w: invalid gateway IPv4 address %q", ErrInvalidConfig, gwStr)
	}
	gw4 := gw.To4()

	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("%w: invalid IPv4 address %q", ErrInvalidConfig, ipStr)
	}
	ip4 := ip.To4()

	mask := net.CIDRMask(prefix, 32)
	if !ip4.Mask(mask).Equal(gw4.Mask(mask)) {
		return "", fmt.Errorf("%w: gateway %q is not reachable in subnet of %q/%d", ErrInvalidConfig, gwStr, ipStr, prefix)
	}

	if ip4.Equal(gw4) {
		return "", fmt.Errorf("%w: gateway %q cannot be the same as interface IP %q", ErrInvalidConfig, gwStr, ipStr)
	}

	return gw4.String(), nil
}

// ValidateDNSServers 校验 DNS 列表：合法 IPv4、非空去重、最多 3 个。
func ValidateDNSServers(servers []string) ([]string, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("%w: DNS servers list cannot be empty for static primary interface", ErrInvalidConfig)
	}
	if len(servers) > 3 {
		return nil, fmt.Errorf("%w: at most 3 DNS servers allowed, got %d", ErrInvalidConfig, len(servers))
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, len(servers))
	for _, s := range servers {
		trimmed := strings.TrimSpace(s)
		ip := net.ParseIP(trimmed)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("%w: invalid DNS server IPv4 address %q", ErrInvalidConfig, trimmed)
		}
		ip4 := ip.To4()
		if ip4.IsUnspecified() || ip4.IsMulticast() || ip4.Equal(net.IPv4bcast) {
			return nil, fmt.Errorf("%w: invalid DNS server address %q", ErrInvalidConfig, trimmed)
		}
		stdStr := ip4.String()
		if _, exists := seen[stdStr]; !exists {
			seen[stdStr] = struct{}{}
			result = append(result, stdStr)
		}
	}
	return result, nil
}

// PrefixToSubnetMask 将 CIDR 前缀转换为点分十进制子网掩码。
func PrefixToSubnetMask(prefix int) string {
	if prefix < 0 || prefix > 32 {
		return ""
	}
	mask := net.CIDRMask(prefix, 32)
	return fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
}

// NormalizedIPv4Config 经过校验与规范化的 IPv4 参数。
type NormalizedIPv4Config struct {
	Address    *string
	SubnetMask *string
	Prefix     *int
	Gateway    *string
	DNSServers []string
}

// ValidateAndNormalizeIPv4 统一校验并规范化 IPv4 配置（适用于单接口与 bond 接口）。
func ValidateAndNormalizeIPv4(mode IPMode, primary bool, address *string, prefix *int, gateway *string, dnsServers []string) (*NormalizedIPv4Config, error) {
	if mode == IPModeStatic {
		if address == nil || prefix == nil {
			return nil, ErrInvalidConfig
		}
		addr, mask, err := NormalizeAndValidateIPv4(*address, *prefix)
		if err != nil {
			return nil, err
		}
		norm := &NormalizedIPv4Config{
			Address:    &addr,
			SubnetMask: &mask,
			Prefix:     prefix,
		}

		if primary {
			if gateway == nil || len(dnsServers) == 0 {
				return nil, fmt.Errorf("%w: gateway and dns required for static primary interface", ErrInvalidConfig)
			}
			gw, err := ValidateGatewayInSubnet(addr, *prefix, *gateway)
			if err != nil {
				return nil, err
			}
			norm.Gateway = &gw

			dns, err := ValidateDNSServers(dnsServers)
			if err != nil {
				return nil, err
			}
			norm.DNSServers = dns
		} else {
			if gateway != nil || len(dnsServers) > 0 {
				return nil, fmt.Errorf("%w: gateway/dns not allowed on non-primary static interface", ErrInvalidConfig)
			}
		}
		return norm, nil
	} else if mode == IPModeDHCP {
		if address != nil || prefix != nil || gateway != nil || len(dnsServers) > 0 {
			return nil, fmt.Errorf("%w: address/prefix/gateway/dns not allowed in dhcp mode", ErrInvalidConfig)
		}
		return &NormalizedIPv4Config{}, nil
	}

	return nil, fmt.Errorf("%w: unsupported ip mode %q", ErrInvalidConfig, mode)
}

// ValidateGatewayPlan 校验网关模式目标计划。
// 规则：
// 1. 目标接口必须存在、可写、非 bond、非 slave，且不能是当前整机主出口 (PrimaryInterfaceID)。
// 2. 目标接口必须是静态 IPv4 模式 (IPModeStatic)，且有有效 IP 与 Prefix。若为 DHCP client 则返回 ErrDhcpServerConflict。
// 3. 请求 prefix 必须有效 (LAN prefix /1 ~ /30，拒绝 /31 与 /32) 并与目标接口 prefix 严格一致。
// 4. poolStart / poolEnd 必须为有效 IPv4 单播地址，且处于该接口子网内，且 poolStart <= poolEnd。
// 5. 地址池不得包含网络地址、广播地址，且不得包含接口自身 IP。
// 6. 租约时长必须在 [60, 604800] 之间，若为 0 则默认为 3600。
func ValidateGatewayPlan(plan *GatewayPlan, iface *InterfaceInfo, primaryInterfaceID *string) (*GatewayPlan, error) {
	if plan == nil {
		return nil, fmt.Errorf("%w: gateway plan is required", ErrInvalidConfig)
	}
	if iface == nil {
		return nil, fmt.Errorf("%w: interface does not exist", ErrGatewayPoolInvalid)
	}
	if !iface.Writable || iface.IsBond || iface.MasterID != nil {
		return nil, fmt.Errorf("%w: interface %q is not a writable physical interface", ErrGatewayPoolInvalid, iface.ID)
	}
	if primaryInterfaceID != nil && *primaryInterfaceID == iface.ID {
		return nil, fmt.Errorf("%w: gateway interface %q cannot be the primary interface", ErrGatewayPoolInvalid, iface.ID)
	}

	// 接口必须处于静态模式且有 IP
	if iface.IPv4.Mode == IPModeDHCP {
		return nil, fmt.Errorf("%w: interface %q is in DHCP client mode", ErrDhcpServerConflict, iface.ID)
	}
	if iface.IPv4.Mode != IPModeStatic || iface.IPv4.Address == nil || iface.IPv4.Prefix == nil {
		return nil, fmt.Errorf("%w: interface %q must have a static IPv4 address", ErrGatewayPoolInvalid, iface.ID)
	}

	ifaceIPStr := *iface.IPv4.Address
	ifacePrefix := *iface.IPv4.Prefix

	ifaceIP := net.ParseIP(ifaceIPStr)
	if ifaceIP == nil || ifaceIP.To4() == nil {
		return nil, fmt.Errorf("%w: invalid interface IPv4 address %q", ErrGatewayPoolInvalid, ifaceIPStr)
	}
	ifaceIPv4 := ifaceIP.To4()

	// prefix 校验：必须与接口 prefix 一致，且仅允许 /1 ~ /30 (DHCP LAN 子网不接受 /31, /32)
	if plan.Prefix != ifacePrefix {
		return nil, fmt.Errorf("%w: plan prefix /%d does not match interface prefix /%d", ErrGatewayPoolInvalid, plan.Prefix, ifacePrefix)
	}
	if plan.Prefix < 1 || plan.Prefix > 30 {
		return nil, fmt.Errorf("%w: prefix /%d is not supported for gateway dhcp (must be /1 to /30)", ErrGatewayPoolInvalid, plan.Prefix)
	}

	// 租约时长校验
	leaseDuration := plan.LeaseDurationSeconds
	if leaseDuration == 0 {
		leaseDuration = DefaultGatewayLeaseDurationSeconds
	}
	if leaseDuration < MinGatewayLeaseDurationSeconds || leaseDuration > MaxGatewayLeaseDurationSeconds {
		return nil, fmt.Errorf("%w: lease duration %d seconds out of range [%d, %d]", ErrGatewayPoolInvalid, leaseDuration, MinGatewayLeaseDurationSeconds, MaxGatewayLeaseDurationSeconds)
	}

	// 起止地址校验
	startIP := net.ParseIP(strings.TrimSpace(plan.PoolStart))
	if startIP == nil || startIP.To4() == nil {
		return nil, fmt.Errorf("%w: invalid poolStart %q", ErrGatewayPoolInvalid, plan.PoolStart)
	}
	startIPv4 := startIP.To4()

	endIP := net.ParseIP(strings.TrimSpace(plan.PoolEnd))
	if endIP == nil || endIP.To4() == nil {
		return nil, fmt.Errorf("%w: invalid poolEnd %q", ErrGatewayPoolInvalid, plan.PoolEnd)
	}
	endIPv4 := endIP.To4()

	mask := net.CIDRMask(plan.Prefix, 32)
	networkIP := ifaceIPv4.Mask(mask)
	broadcastIP := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcastIP[i] = networkIP[i] | ^mask[i]
	}

	// 检查 start / end 是否在接口子网内
	if !startIPv4.Mask(mask).Equal(networkIP) {
		return nil, fmt.Errorf("%w: poolStart %q is not in interface subnet %q/%d", ErrGatewayPoolInvalid, plan.PoolStart, ifaceIPStr, plan.Prefix)
	}
	if !endIPv4.Mask(mask).Equal(networkIP) {
		return nil, fmt.Errorf("%w: poolEnd %q is not in interface subnet %q/%d", ErrGatewayPoolInvalid, plan.PoolEnd, ifaceIPStr, plan.Prefix)
	}

	// 检查 poolStart <= poolEnd
	startVal := uint32(startIPv4[0])<<24 | uint32(startIPv4[1])<<16 | uint32(startIPv4[2])<<8 | uint32(startIPv4[3])
	endVal := uint32(endIPv4[0])<<24 | uint32(endIPv4[1])<<16 | uint32(endIPv4[2])<<8 | uint32(endIPv4[3])
	if startVal > endVal {
		return nil, fmt.Errorf("%w: poolStart %q is greater than poolEnd %q", ErrGatewayPoolInvalid, plan.PoolStart, plan.PoolEnd)
	}

	ifaceVal := uint32(ifaceIPv4[0])<<24 | uint32(ifaceIPv4[1])<<16 | uint32(ifaceIPv4[2])<<8 | uint32(ifaceIPv4[3])
	netVal := uint32(networkIP[0])<<24 | uint32(networkIP[1])<<16 | uint32(networkIP[2])<<8 | uint32(networkIP[3])
	bcastVal := uint32(broadcastIP[0])<<24 | uint32(broadcastIP[1])<<16 | uint32(broadcastIP[2])<<8 | uint32(broadcastIP[3])

	// 检查不包含网络地址和广播地址
	if startVal <= netVal && netVal <= endVal {
		return nil, fmt.Errorf("%w: address pool contains network address %q", ErrGatewayPoolInvalid, networkIP.String())
	}
	if startVal <= bcastVal && bcastVal <= endVal {
		return nil, fmt.Errorf("%w: address pool contains broadcast address %q", ErrGatewayPoolInvalid, broadcastIP.String())
	}

	// 检查不包含接口自身 IP
	if startVal <= ifaceVal && ifaceVal <= endVal {
		return nil, fmt.Errorf("%w: address pool contains interface IP %q", ErrGatewayPoolInvalid, ifaceIPStr)
	}

	normalized := &GatewayPlan{
		DownstreamInterfaceID: plan.DownstreamInterfaceID,
		PoolStart:             startIPv4.String(),
		PoolEnd:               endIPv4.String(),
		Prefix:                plan.Prefix,
		LeaseDurationSeconds:  leaseDuration,
		IPForward:             plan.IPForward,
	}

	return normalized, nil
}
