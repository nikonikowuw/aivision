//go:build linux

package netconfig

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

// DHCPLeaseInfo 保存已获取的 DHCPv4 租约信息。
type DHCPLeaseInfo struct {
	IPNet      net.IPNet
	Gateway    net.IP
	DNS        []net.IP
	LeaseTime  time.Duration
	AcquiredAt time.Time
}

// LinuxDHCPClient 管理单个网络接口的 DHCPv4 客户端生命周期（DORA/T1/T2/释放）。
type LinuxDHCPClient struct {
	mu         sync.Mutex
	ifName     string
	cancelFunc context.CancelFunc
	running    bool
	lastLease  *DHCPLeaseInfo
}

func NewLinuxDHCPClient(ifName string) *LinuxDHCPClient {
	return &LinuxDHCPClient{
		ifName: ifName,
	}
}

// GetLastLease 获取最近一次成功的租约信息。
func (c *LinuxDHCPClient) GetLastLease() *DHCPLeaseInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastLease
}

// Start 启动 DHCPv4 客户端租约协程。
func (c *LinuxDHCPClient) Start(ctx context.Context, onLeaseAcquired func(lease DHCPLeaseInfo)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	cCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	c.running = true

	go c.runLoop(cCtx, onLeaseAcquired)
	return nil
}

func (c *LinuxDHCPClient) runLoop(ctx context.Context, onLeaseAcquired func(lease DHCPLeaseInfo)) {
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	client, err := nclient4.New(c.ifName, nclient4.WithTimeout(5*time.Second))
	if err != nil {
		return
	}
	defer client.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 1. 发起 DORA 获取租约
		lease, err := client.Request(ctx)
		if err != nil {
			// DORA 失败后等待 5 秒重试
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		ack := lease.ACK
		if ack == nil {
			continue
		}

		ip := ack.YourIPAddr
		if ip == nil || ip.To4() == nil || ip.IsUnspecified() {
			continue
		}

		mask := net.IPMask(ack.SubnetMask())
		if mask == nil || len(mask) != 4 {
			mask = net.CIDRMask(24, 32)
		}

		var gw net.IP
		routers := ack.Router()
		if len(routers) > 0 && routers[0].To4() != nil && !routers[0].IsUnspecified() {
			gw = routers[0]
		}

		dnsList := ack.DNS()
		leaseDuration := ack.IPAddressLeaseTime(3600 * time.Second)
		if leaseDuration < 10*time.Second {
			leaseDuration = 10 * time.Second
		}

		leaseInfo := DHCPLeaseInfo{
			IPNet: net.IPNet{
				IP:   ip.To4(),
				Mask: mask,
			},
			Gateway:    gw,
			DNS:        dnsList,
			LeaseTime:  leaseDuration,
			AcquiredAt: time.Now(),
		}

		c.mu.Lock()
		c.lastLease = &leaseInfo
		c.mu.Unlock()

		if onLeaseAcquired != nil {
			onLeaseAcquired(leaseInfo)
		}

		// 2. T1 续租（在 0.5 * LeaseTime 处尝试）
		t1Duration := leaseDuration / 2
		select {
		case <-ctx.Done():
			_ = client.Release(lease)
			return
		case <-time.After(t1Duration):
		}

		// 执行 Renew
		renewedLease, err := client.Renew(ctx, lease)
		if err == nil && renewedLease != nil {
			lease = renewedLease
			continue
		}

		// 3. T1 失败，等待至 T2（0.875 * LeaseTime）
		t2Duration := (leaseDuration * 3) / 8
		select {
		case <-ctx.Done():
			_ = client.Release(lease)
			return
		case <-time.After(t2Duration):
		}

		// T2 仍未续约则重新执行完整 DORA
	}
}

// Stop 停止 DHCP 客户端并释放租约。
func (c *LinuxDHCPClient) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}
	c.running = false
}
