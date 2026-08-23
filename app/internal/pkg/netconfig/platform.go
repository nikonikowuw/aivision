package netconfig

import (
	"context"
)

// Platform 平台网络硬件/内核适配层接口。
type Platform interface {
	Type() PlatformType
	Probe(ctx context.Context) error
	Discover(ctx context.Context) ([]InterfaceInfo, error)
	Read(ctx context.Context) (HostSnapshot, error)
	Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error)
	Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error)
	Close(ctx context.Context) error
}
