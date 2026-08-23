package netconfig

import (
	"context"
	"net"

	"go.uber.org/zap"
)

// DHCPServerConfig 启动 DHCP Server 的配置参数。
type DHCPServerConfig struct {
	InterfaceName string
	ServerIP      net.IP
	SubnetMask    net.IPMask
	PoolStart     net.IP
	PoolEnd       net.IP
	LeaseDuration int64 // 秒
}

// DHCPServer DHCP 服务端抽象接口。
type DHCPServer interface {
	Serve() error
	Close() error
}

// GatewayBackend 平台层与底层 DHCP / 系统转发交互的抽象。
type GatewayBackend interface {
	ReadIPForward(ctx context.Context) (bool, error)
	WriteIPForward(ctx context.Context, enabled bool) error
	ProbeDHCP(ctx context.Context, interfaceName string, serverIP net.IP, mask net.IPMask) (bool, error)
	StartDHCP(ctx context.Context, cfg DHCPServerConfig, store StateStore) (DHCPServer, error)
}

// GatewayRuntime 边缘网关业务运行时接口。
type GatewayRuntime interface {
	Snapshot(ctx context.Context) (GatewayState, error)
	Probe(ctx context.Context, plan GatewayPlan, iface *InterfaceInfo) (responded bool, err error)
	Apply(ctx context.Context, plan GatewayPlan, before GatewayState, iface *InterfaceInfo) (GatewayState, error)
	Restore(ctx context.Context, before GatewayState, iface *InterfaceInfo) (GatewayState, error)
	Leases(ctx context.Context) ([]GatewayLease, error)
	Close(ctx context.Context) error
}

// NewGatewayBackend 根据平台类型创建对应的网关后端。
func NewGatewayBackend(platformType PlatformType, fake bool, log *zap.Logger) GatewayBackend {
	if fake || platformType == PlatformFake {
		return NewFakeGatewayBackend()
	}
	if platformType == PlatformLinux {
		return newLinuxGatewayBackend(log)
	}
	return NewFakeGatewayBackend()
}

