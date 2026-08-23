package netconfig

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"go.uber.org/zap"
)

// LinuxGatewayBackend Linux 平台下的网关后端实现。
type LinuxGatewayBackend struct {
	logger *zap.Logger
}

// NewLinuxGatewayBackend 创建 Linux 网关后端。
func NewLinuxGatewayBackend(logger *zap.Logger) *LinuxGatewayBackend {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LinuxGatewayBackend{logger: logger}
}

const procIPForwardPath = "/proc/sys/net/ipv4/ip_forward"

func (b *LinuxGatewayBackend) ReadIPForward(ctx context.Context) (bool, error) {
	data, err := os.ReadFile(procIPForwardPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", procIPForwardPath, err)
	}
	val := strings.TrimSpace(string(data))
	return val == "1", nil
}

func (b *LinuxGatewayBackend) WriteIPForward(ctx context.Context, enabled bool) error {
	val := "0\n"
	if enabled {
		val = "1\n"
	}
	if err := os.WriteFile(procIPForwardPath, []byte(val), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", procIPForwardPath, err)
	}
	return nil
}

func (b *LinuxGatewayBackend) ProbeDHCP(ctx context.Context, interfaceName string, serverIP net.IP, mask net.IPMask) (bool, error) {
	// 在指定接口上发送 DHCPDISCOVER 探测是否存在其他 DHCP Server
	client, err := nclient4.New(interfaceName, nclient4.WithTimeout(1500*time.Millisecond), nclient4.WithRetry(1))
	if err != nil {
		b.logger.Warn("create dhcp probe client failed", zap.String("interface", interfaceName), zap.Error(err))
		return false, nil
	}
	defer client.Close()

	lease, err := client.Discover(ctx)
	if err != nil {
		// 未收到任何响应，说明链路无其他 DHCP 服务
		return false, nil
	}
	if lease != nil && lease.Offer != nil {
		offeredServerIP := lease.Offer.ServerIPAddr()
		// 如果 Offer 来自本机配置的 ServerIP，说明是自身运行中的 DHCP Server，不计为外部冲突
		if serverIP != nil && offeredServerIP != nil && offeredServerIP.Equal(serverIP) {
			b.logger.Debug("dhcp probe detected own server response, ignoring",
				zap.String("interface", interfaceName),
				zap.String("server_ip", offeredServerIP.String()),
			)
			return false, nil
		}

		b.logger.Warn("detected existing dhcp server on link",
			zap.String("interface", interfaceName),
			zap.String("server_ip", offeredServerIP.String()),
			zap.String("offered_ip", lease.Offer.YourIPAddr.String()),
		)
		return true, nil
	}
	return false, nil
}

// insomniacDHCPServer 包装 server4.Server 并管理租约分配。
type insomniacDHCPServer struct {
	cfg        DHCPServerConfig
	store      StateStore
	server     *server4.Server
	logger     *zap.Logger
	mu         sync.Mutex
	leases     map[string]*GatewayLease // key: MAC
	allocated  map[string]string        // IP -> MAC
	closed     bool
}

func (b *LinuxGatewayBackend) StartDHCP(ctx context.Context, cfg DHCPServerConfig, store StateStore) (DHCPServer, error) {
	laddr := net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: 67,
	}

	srv := &insomniacDHCPServer{
		cfg:       cfg,
		store:     store,
		logger:    b.logger,
		leases:    make(map[string]*GatewayLease),
		allocated: make(map[string]string),
	}

	// 从 store 恢复已有租约
	if store != nil {
		persisted, err := store.GetGatewayLeases()
		if err == nil {
			now := time.Now().UTC()
			for _, l := range persisted {
				// 未过期的租约恢复到内存
				if l.ExpiresAt.After(now) {
					leaseCopy := l
					srv.leases[l.MAC] = &leaseCopy
					srv.allocated[l.IP] = l.MAC
				}
			}
		}
	}

	s, err := server4.NewServer(cfg.InterfaceName, &laddr, srv.handleDHCP)
	if err != nil {
		return nil, fmt.Errorf("create dhcp server on %s: %w", cfg.InterfaceName, err)
	}
	srv.server = s

	return srv, nil
}

func (s *insomniacDHCPServer) Serve() error {
	return s.server.Serve()
}

func (s *insomniacDHCPServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *insomniacDHCPServer) handleDHCP(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
	if m == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	mac := m.ClientHWAddr.String()
	hostname := m.HostName()
	now := time.Now().UTC()

	switch m.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		ip := s.allocateOrReuseIP(mac, nil)
		if ip == nil {
			s.logger.Warn("no available IP in gateway pool", zap.String("mac", mac))
			return
		}

		reply, err := dhcpv4.NewOfferFromDHCPv4(m,
			dhcpv4.WithYourIP(ip),
			dhcpv4.WithServerIP(s.cfg.ServerIP),
			dhcpv4.WithNetmask(s.cfg.SubnetMask),
			dhcpv4.WithRouter(s.cfg.ServerIP),
			dhcpv4.WithLeaseTime(uint32(s.cfg.LeaseDuration)),
		)
		if err != nil {
			s.logger.Warn("create dhcp offer failed", zap.Error(err))
			return
		}

		// 发送 Offer
		_, _ = conn.WriteTo(reply.ToBytes(), peer)

	case dhcpv4.MessageTypeRequest:
		reqIP := m.RequestedIPAddress()
		if reqIP == nil || reqIP.IsUnspecified() {
			reqIP = m.ClientIPAddr
		}

		var preferredIP net.IP
		if reqIP != nil && !reqIP.IsUnspecified() {
			preferredIP = reqIP.To4()
		}

		// 校验/分配 IP（优先满足客户端请求的合法地址）
		ip := s.allocateOrReuseIP(mac, preferredIP)
		if ip == nil {
			nak, err := dhcpv4.NewNakFromDHCPv4(m, dhcpv4.WithServerIP(s.cfg.ServerIP))
			if err == nil {
				_, _ = conn.WriteTo(nak.ToBytes(), peer)
			}
			return
		}

		lease := &GatewayLease{
			MAC:           mac,
			IP:            ip.String(),
			StartsAt:      now,
			ExpiresAt:     now.Add(time.Duration(s.cfg.LeaseDuration) * time.Second),
			LastRenewedAt: now,
			Hostname:      hostname,
		}
		s.leases[mac] = lease
		s.allocated[ip.String()] = mac
		s.persistLeases()

		ack, err := dhcpv4.NewAckFromDHCPv4(m,
			dhcpv4.WithYourIP(ip),
			dhcpv4.WithServerIP(s.cfg.ServerIP),
			dhcpv4.WithNetmask(s.cfg.SubnetMask),
			dhcpv4.WithRouter(s.cfg.ServerIP),
			dhcpv4.WithLeaseTime(uint32(s.cfg.LeaseDuration)),
		)
		if err != nil {
			s.logger.Warn("create dhcp ack failed", zap.Error(err))
			return
		}

		_, _ = conn.WriteTo(ack.ToBytes(), peer)

	case dhcpv4.MessageTypeRelease:
		if l, ok := s.leases[mac]; ok {
			delete(s.allocated, l.IP)
			delete(s.leases, mac)
			s.persistLeases()
		}
	}
}

func (s *insomniacDHCPServer) allocateOrReuseIP(mac string, requestedIP net.IP) net.IP {
	now := time.Now().UTC()

	// 1. 清理过期租约
	for m, l := range s.leases {
		if l.ExpiresAt.Before(now) {
			delete(s.allocated, l.IP)
			delete(s.leases, m)
		}
	}

	start := ipToUint32(s.cfg.PoolStart)
	end := ipToUint32(s.cfg.PoolEnd)

	// 2. 如果客户端请求了特定 IP，优先校验并分配
	if requestedIP != nil {
		req4 := requestedIP.To4()
		if req4 != nil {
			reqVal := ipToUint32(req4)
			if reqVal >= start && reqVal <= end {
				reqStr := req4.String()
				allocatedMAC, exists := s.allocated[reqStr]
				if !exists || allocatedMAC == mac {
					return req4
				}
				// 请求的 IP 在池内但被其他人占用
				return nil
			}
		}
	}

	// 3. 如果该 MAC 已有租约且未过期，复用原 IP
	if l, ok := s.leases[mac]; ok {
		if l.ExpiresAt.After(now) {
			return net.ParseIP(l.IP).To4()
		}
	}

	// 4. 顺序查找未占用的池内 IP
	for cur := start; cur <= end; cur++ {
		ip := uint32ToIP(cur)
		ipStr := ip.String()
		if _, taken := s.allocated[ipStr]; !taken {
			return ip
		}
	}

	return nil
}

func (s *insomniacDHCPServer) persistLeases() {
	if s.store == nil {
		return
	}
	leasesList := make([]GatewayLease, 0, len(s.leases))
	for _, l := range s.leases {
		leasesList = append(leasesList, *l)
	}
	_ = s.store.SetGatewayLeases(leasesList)
}

func ipToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}
