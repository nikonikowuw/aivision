package netconfig

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// FakeGatewayBackend 用于测试和 Fake 平台的网关后端。
type FakeGatewayBackend struct {
	mu               sync.Mutex
	ipForward        bool
	probeResponse    bool
	probeErr         error
	runningServer    *FakeDHCPServer
	startErr         error
	simulatedLeases  []GatewayLease
	conflictDetected bool
}

// NewFakeGatewayBackend 创建 Fake 网关后端。
func NewFakeGatewayBackend() *FakeGatewayBackend {
	return &FakeGatewayBackend{}
}

func (b *FakeGatewayBackend) ReadIPForward(ctx context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ipForward, nil
}

func (b *FakeGatewayBackend) WriteIPForward(ctx context.Context, enabled bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ipForward = enabled
	return nil
}

func (b *FakeGatewayBackend) ProbeDHCP(ctx context.Context, interfaceName string, serverIP net.IP, mask net.IPMask) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probeErr != nil {
		return false, b.probeErr
	}
	return b.probeResponse, nil
}

// SetProbeResponse 注入冲突探测返回值。
func (b *FakeGatewayBackend) SetProbeResponse(hasResponse bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probeResponse = hasResponse
	b.probeErr = err
}

func (b *FakeGatewayBackend) StartDHCP(ctx context.Context, cfg DHCPServerConfig, store StateStore) (DHCPServer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.startErr != nil {
		return nil, b.startErr
	}

	srv := &FakeDHCPServer{
		cfg:     cfg,
		store:   store,
		backend: b,
		leases:  make(map[string]GatewayLease),
	}

	// 恢复 store 中的 leases
	if store != nil {
		persisted, err := store.GetGatewayLeases()
		if err == nil {
			for _, l := range persisted {
				srv.leases[l.MAC] = l
			}
		}
	}
	b.runningServer = srv
	return srv, nil
}

// FakeDHCPServer 模拟 DHCP 服务。
type FakeDHCPServer struct {
	cfg     DHCPServerConfig
	store   StateStore
	backend *FakeGatewayBackend
	mu      sync.Mutex
	leases  map[string]GatewayLease
	closed  bool
}

func (s *FakeDHCPServer) Serve() error {
	return nil
}

func (s *FakeDHCPServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// AllocateLease 测试时模拟客户端获取/续租 IP。
func (s *FakeDHCPServer) AllocateLease(mac string, ip string, hostname string) GatewayLease {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	lease := GatewayLease{
		MAC:           mac,
		IP:            ip,
		StartsAt:      now,
		ExpiresAt:     now.Add(time.Duration(s.cfg.LeaseDuration) * time.Second),
		LastRenewedAt: now,
		Hostname:      hostname,
	}
	s.leases[mac] = lease
	if s.store != nil {
		var list []GatewayLease
		for _, l := range s.leases {
			list = append(list, l)
		}
		_ = s.store.SetGatewayLeases(list)
	}
	return lease
}

// ReleaseLease 测试时模拟客户端释放 IP。
func (s *FakeDHCPServer) ReleaseLease(mac string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, mac)
	if s.store != nil {
		var list []GatewayLease
		for _, l := range s.leases {
			list = append(list, l)
		}
		_ = s.store.SetGatewayLeases(list)
	}
}

// DefaultGatewayRuntime 标准网关运行时实现。
type DefaultGatewayRuntime struct {
	backend    GatewayBackend
	store      StateStore
	mu         sync.Mutex
	state      GatewayState
	server     DHCPServer
	monitorCtx context.Context
	cancelMon  context.CancelFunc
	closed     bool
}

// NewDefaultGatewayRuntime 创建标准网关运行时。
func NewDefaultGatewayRuntime(backend GatewayBackend, store StateStore) *DefaultGatewayRuntime {
	return &DefaultGatewayRuntime{
		backend: backend,
		store:   store,
	}
}

func (r *DefaultGatewayRuntime) Snapshot(ctx context.Context) (GatewayState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	curForward, _ := r.backend.ReadIPForward(ctx)
	st := r.state
	st.IPForward = curForward
	return st, nil
}

func (r *DefaultGatewayRuntime) Probe(ctx context.Context, plan GatewayPlan, iface *InterfaceInfo) (bool, error) {
	if iface == nil || iface.IPv4.Address == nil || iface.IPv4.Prefix == nil {
		return false, fmt.Errorf("interface %s does not have static IPv4", plan.DownstreamInterfaceID)
	}
	serverIP := net.ParseIP(*iface.IPv4.Address)
	mask := net.CIDRMask(*iface.IPv4.Prefix, 32)
	return r.backend.ProbeDHCP(ctx, iface.Name, serverIP, mask)
}

func (r *DefaultGatewayRuntime) Apply(ctx context.Context, plan GatewayPlan, before GatewayState, iface *InterfaceInfo) (GatewayState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. 读取进入网关模式前的 ip_forward 值（若 before 中未保存）
	prevForward := before.IPForward
	if before.PreviousIPForward != nil {
		prevForward = *before.PreviousIPForward
	} else {
		cur, err := r.backend.ReadIPForward(ctx)
		if err == nil {
			prevForward = cur
		}
	}

	// 2. 写入目标 ip_forward
	if err := r.backend.WriteIPForward(ctx, plan.IPForward); err != nil {
		return GatewayState{}, fmt.Errorf("write ip_forward: %w", err)
	}

	// 3. 启动 DHCP Server
	ifaceIP := net.ParseIP(*iface.IPv4.Address)
	mask := net.CIDRMask(plan.Prefix, 32)
	poolStart := net.ParseIP(plan.PoolStart)
	poolEnd := net.ParseIP(plan.PoolEnd)

	cfg := DHCPServerConfig{
		InterfaceName: iface.Name,
		ServerIP:      ifaceIP,
		SubnetMask:    mask,
		PoolStart:     poolStart,
		PoolEnd:       poolEnd,
		LeaseDuration: plan.LeaseDurationSeconds,
	}

	// 停止先前的 server（如有）
	if r.server != nil {
		_ = r.server.Close()
		r.server = nil
	}
	if r.cancelMon != nil {
		r.cancelMon()
		r.cancelMon = nil
	}

	srv, err := r.backend.StartDHCP(ctx, cfg, r.store)
	if err != nil {
		// 回滚 ip_forward
		_ = r.backend.WriteIPForward(ctx, prevForward)
		return GatewayState{}, fmt.Errorf("start dhcp server: %w", err)
	}
	r.server = srv

	go func() {
		_ = srv.Serve()
	}()

	// 启动后台冲突监测 goroutine
	monCtx, cancel := context.WithCancel(context.Background())
	r.monitorCtx = monCtx
	r.cancelMon = cancel
	go r.runConflictMonitor(monCtx, iface.Name, ifaceIP, mask)

	planCopy := plan
	r.state = GatewayState{
		Plan:              &planCopy,
		Running:           true,
		IPForward:         plan.IPForward,
		PreviousIPForward: &prevForward,
		ConflictDetected:  false,
	}

	return r.state, nil
}

func (r *DefaultGatewayRuntime) runConflictMonitor(ctx context.Context, ifaceName string, serverIP net.IP, mask net.IPMask) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hasConflict, err := r.backend.ProbeDHCP(ctx, ifaceName, serverIP, mask)
			if err == nil {
				r.mu.Lock()
				r.state.ConflictDetected = hasConflict
				r.mu.Unlock()
			}
		}
	}
}

func (r *DefaultGatewayRuntime) Restore(ctx context.Context, before GatewayState, iface *InterfaceInfo) (GatewayState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. 停止 DHCP 服务与后台监测
	if r.cancelMon != nil {
		r.cancelMon()
		r.cancelMon = nil
	}
	if r.server != nil {
		_ = r.server.Close()
		r.server = nil
	}

	// 2. 清除租约表
	if r.store != nil {
		_ = r.store.ClearGatewayLeases()
	}

	// 3. 恢复 ip_forward
	targetForward := false
	if before.PreviousIPForward != nil {
		targetForward = *before.PreviousIPForward
	}
	_ = r.backend.WriteIPForward(ctx, targetForward)

	r.state = GatewayState{
		Plan:              nil,
		Running:           false,
		IPForward:         targetForward,
		PreviousIPForward: nil,
		ConflictDetected:  false,
	}

	return r.state, nil
}

func (r *DefaultGatewayRuntime) Leases(ctx context.Context) ([]GatewayLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return []GatewayLease{}, nil
	}
	return r.store.GetGatewayLeases()
}

func (r *DefaultGatewayRuntime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.cancelMon != nil {
		r.cancelMon()
		r.cancelMon = nil
	}
	if r.server != nil {
		_ = r.server.Close()
		r.server = nil
	}
	return nil
}
