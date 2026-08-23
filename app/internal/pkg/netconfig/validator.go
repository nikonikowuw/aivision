package netconfig

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrInvalidConfig        = errors.New("invalid network configuration")
	ErrTransactionPending   = errors.New("network transaction already pending")
	ErrTransactionNotFound  = errors.New("network transaction not found")
	ErrTransactionExpired   = errors.New("network transaction expired")
	ErrInterfaceNotManaged  = errors.New("network interface not managed or writable")
	ErrOwnershipConflict    = errors.New("network ownership conflict")
	ErrUnsupported          = errors.New("network platform unsupported")
	ErrApplyFailed          = errors.New("network apply failed")
	ErrRecoveryFailed       = errors.New("network recovery failed")
	ErrStateCorrupt         = errors.New("network state file corrupt")
	ErrExternalDrift        = errors.New("network external drift detected")
	ErrNotReady             = errors.New("network service not ready")
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

// SubnetMaskToPrefix 将点分十进制子网掩码转换为 CIDR 前缀。
func SubnetMaskToPrefix(maskStr string) (int, error) {
	ip := net.ParseIP(strings.TrimSpace(maskStr))
	if ip == nil || ip.To4() == nil {
		return 0, fmt.Errorf("%w: invalid subnet mask %q", ErrInvalidConfig, maskStr)
	}
	mask := net.IPMask(ip.To4())
	ones, bits := mask.Size()
	if bits != 32 || ones == 0 && maskStr != "0.0.0.0" {
		return 0, fmt.Errorf("%w: non-contiguous subnet mask %q", ErrInvalidConfig, maskStr)
	}
	return ones, nil
}
