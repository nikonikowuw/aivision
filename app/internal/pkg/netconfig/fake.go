package netconfig

import (
	"context"
	"fmt"
	"sync"
)

// FakeLACPScenario fake 平台模拟的 LACP 协商场景。
type FakeLACPScenario string

const (
	FakeLACPScenarioNegotiated FakeLACPScenario = "negotiated"
	FakeLACPScenarioNone       FakeLACPScenario = "none"
	FakeLACPScenarioPartial    FakeLACPScenario = "partial"
)

// FakePlatform 纯内存测试替身实现。
type FakePlatform struct {
	mu           sync.Mutex
	platformType PlatformType
	interfaces   map[string]InterfaceInfo
	primaryID    *string
	systemDNS    []string
	mode         NetworkMode
	bond         *BondTopology
	lacpScenario FakeLACPScenario
	failApply    bool
	failRestore  bool
	failLACP     bool
}

// fakeBondInterfaceID fake 平台固定使用的 bond 逻辑口 ID（D1：系统生成，不接受用户命名）。
const fakeBondInterfaceID = "bond0"

// NewFakePlatform 创建 Fake 测试替身。
func NewFakePlatform(platformType PlatformType) *FakePlatform {
	eth0ID := "eth0"
	eth0Name := "eth0"
	eth0MAC := "02:42:ac:11:00:02"
	eth0Addr := "192.168.1.100"
	eth0Prefix := 24
	eth0Mask := "255.255.255.0"
	eth0GW := "192.168.1.1"

	eth1ID := "eth1"
	eth1Name := "eth1"
	eth1MAC := "02:42:ac:11:00:03"
	eth1Addr := "192.168.2.100"
	eth1Prefix := 24
	eth1Mask := "255.255.255.0"

	wlan0ID := "wlan0"
	wlan0Name := "wlan0"
	wlan0MAC := "02:42:ac:11:00:04"
	wlan0Addr := "192.168.31.50"
	wlan0Prefix := 24
	wlan0Mask := "255.255.255.0"

	return &FakePlatform{
		platformType: platformType,
		primaryID:    &eth0ID,
		systemDNS:    []string{"192.168.1.1", "1.1.1.1"},
		interfaces: map[string]InterfaceInfo{
			eth0ID: {
				ID:          eth0ID,
				Name:        eth0Name,
				DisplayName: "LAN 1",
				Type:        InterfaceEthernet,
				MAC:         &eth0MAC,
				LinkStatus:  LinkUp,
				Ownership:   OwnershipManaged,
				Writable:    true,
				IsPrimary:   true,
				IPv4: IPv4State{
					Mode:       IPModeStatic,
					Address:    &eth0Addr,
					Prefix:     &eth0Prefix,
					SubnetMask: &eth0Mask,
					Gateway:    &eth0GW,
					DNSServers: []string{"192.168.1.1", "1.1.1.1"},
					Status:     IPStatusEffective,
				},
				Fingerprint: "fp-eth0",
			},
			eth1ID: {
				ID:          eth1ID,
				Name:        eth1Name,
				DisplayName: "LAN 2",
				Type:        InterfaceEthernet,
				MAC:         &eth1MAC,
				LinkStatus:  LinkUp,
				Ownership:   OwnershipManaged,
				Writable:    true,
				IsPrimary:   false,
				IPv4: IPv4State{
					Mode:       IPModeDHCP,
					Address:    &eth1Addr,
					Prefix:     &eth1Prefix,
					SubnetMask: &eth1Mask,
					Gateway:    nil,
					DNSServers: nil,
					Status:     IPStatusEffective,
				},
				Fingerprint: "fp-eth1",
			},
			wlan0ID: {
				ID:          wlan0ID,
				Name:        wlan0Name,
				DisplayName: "WLAN",
				Type:        InterfaceWifi,
				MAC:         &wlan0MAC,
				LinkStatus:  LinkDown,
				Ownership:   OwnershipManaged,
				Writable:    true,
				IsPrimary:   false,
				IPv4: IPv4State{
					Mode:       IPModeDHCP,
					Address:    &wlan0Addr,
					Prefix:     &wlan0Prefix,
					SubnetMask: &wlan0Mask,
					Gateway:    nil,
					DNSServers: nil,
					Status:     IPStatusEffective,
				},
				Fingerprint: "fp-wlan0",
			},
		},
	}
}

func (f *FakePlatform) Type() PlatformType {
	return f.platformType
}

// Capabilities 声明支持的模式。FakePlatform 同时支持 multi-address、active-backup 与 lacp-aggregation。
func (f *FakePlatform) Capabilities(ctx context.Context) Capabilities {
	return Capabilities{
		DHCP:            true,
		StaticIPv4:      true,
		FactoryReset:    true,
		WifiAssociation: false,
		SupportedModes:  []NetworkMode{NetworkModeMultiAddress, NetworkModeActiveBackup, NetworkModeLACP},
	}
}

func (f *FakePlatform) SetLACPScenario(scenario FakeLACPScenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lacpScenario = scenario
	if f.mode == NetworkModeLACP && f.bond != nil {
		f.bond.LACP = f.buildLACPStatus(f.bond.SlaveIDs)
	}
}

func (f *FakePlatform) SetFailLACP(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failLACP = fail
}

func (f *FakePlatform) SetInterfaceLinkProperties(ifaceID string, speedMbps *int, duplex InterfaceDuplex) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info, ok := f.interfaces[ifaceID]; ok {
		info.SpeedMbps = speedMbps
		info.Duplex = duplex
		f.interfaces[ifaceID] = info
	}
}

func (f *FakePlatform) SetFailApply(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failApply = fail
}

func (f *FakePlatform) SetFailRestore(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failRestore = fail
}

func (f *FakePlatform) Probe(ctx context.Context) error {
	return nil
}

func (f *FakePlatform) Discover(ctx context.Context) ([]InterfaceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := make([]InterfaceInfo, 0, len(f.interfaces))
	// 按稳定顺序返回：eth0 -> eth1 -> wlan0
	keys := []string{"eth0", "eth1", "wlan0"}
	for _, k := range keys {
		if iface, ok := f.interfaces[k]; ok {
			list = append(list, iface)
		}
	}
	for k, iface := range f.interfaces {
		if k != "eth0" && k != "eth1" && k != "wlan0" {
			list = append(list, iface)
		}
	}
	return list, nil
}

func (f *FakePlatform) Read(ctx context.Context) (HostSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ifacesCopy := make(map[string]InterfaceInfo, len(f.interfaces))
	for k, v := range f.interfaces {
		ifacesCopy[k] = v
	}

	return HostSnapshot{
		Interfaces:              ifacesCopy,
		PrimaryInterfaceID:      f.primaryID,
		DefaultRouteInterfaceID: f.primaryID,
		SystemDNSServers:        f.systemDNS,
		Fingerprint:             "fake-host-fingerprint",
		Native: NativeSnapshot{
			Version: 1,
			Data:    []byte(`{"fake": true}`),
		},
		Mode: f.mode,
		Bond: f.bond,
	}, nil
}

func (f *FakePlatform) Apply(ctx context.Context, plan HostPlan) (HostSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failApply {
		return HostSnapshot{}, fmt.Errorf("%w: fake platform injected apply failure", ErrApplyFailed)
	}
	if f.failLACP && plan.Mode == NetworkModeLACP {
		return HostSnapshot{}, fmt.Errorf("%w: fake platform injected lacp kernel rejection", ErrApplyFailed)
	}

	f.primaryID = plan.PrimaryInterfaceID

	// 模式切换：进入 active-backup 创建 bond0 并标记 slave；退回 multi-address 拆除并归还
	f.applyMode(plan)

	for ifaceID, ifacePlan := range plan.Interfaces {
		current, ok := f.interfaces[ifaceID]
		if !ok {
			continue
		}
		// slave 绑定期间由 applyMode 统一管理，不单独应用 IPv4（D3）
		if current.MasterID != nil {
			continue
		}
		current.IsPrimary = (plan.PrimaryInterfaceID != nil && *plan.PrimaryInterfaceID == ifaceID)
		current.IPv4.Mode = ifacePlan.Mode

		if ifacePlan.Mode == IPModeDHCP {
			dhcpAddr := "192.168.10.150"
			dhcpPrefix := 24
			dhcpMask := "255.255.255.0"
			current.IPv4.Address = &dhcpAddr
			current.IPv4.Prefix = &dhcpPrefix
			current.IPv4.SubnetMask = &dhcpMask
			if current.IsPrimary {
				dhcpGW := "192.168.10.1"
				current.IPv4.Gateway = &dhcpGW
				current.IPv4.DNSServers = []string{"192.168.10.1"}
				f.systemDNS = []string{"192.168.10.1"}
			} else {
				current.IPv4.Gateway = nil
				current.IPv4.DNSServers = nil
			}
		} else {
			current.IPv4.Address = ifacePlan.Address
			current.IPv4.Prefix = ifacePlan.Prefix
			if ifacePlan.Prefix != nil {
				mask := PrefixToSubnetMask(*ifacePlan.Prefix)
				current.IPv4.SubnetMask = &mask
			}
			if current.IsPrimary {
				current.IPv4.Gateway = ifacePlan.Gateway
				current.IPv4.DNSServers = ifacePlan.DNSServers
				f.systemDNS = ifacePlan.DNSServers
			} else {
				current.IPv4.Gateway = nil
				current.IPv4.DNSServers = nil
			}
		}
		current.IPv4.Status = IPStatusEffective
		f.interfaces[ifaceID] = current
	}

	ifacesCopy := make(map[string]InterfaceInfo, len(f.interfaces))
	for k, v := range f.interfaces {
		ifacesCopy[k] = v
	}

	return HostSnapshot{
		Interfaces:              ifacesCopy,
		PrimaryInterfaceID:      f.primaryID,
		DefaultRouteInterfaceID: f.primaryID,
		SystemDNSServers:        f.systemDNS,
		Fingerprint:             "fake-host-fingerprint",
		Native: NativeSnapshot{
			Version: 1,
			Data:    []byte(`{"fake": true}`),
		},
		Mode: f.mode,
		Bond: f.bond,
	}, nil
}

func (f *FakePlatform) Restore(ctx context.Context, snapshot HostSnapshot) (HostSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failRestore {
		return HostSnapshot{}, fmt.Errorf("%w: fake platform injected restore failure", ErrRecoveryFailed)
	}

	f.interfaces = make(map[string]InterfaceInfo, len(snapshot.Interfaces))
	for k, v := range snapshot.Interfaces {
		f.interfaces[k] = v
	}
	f.primaryID = snapshot.PrimaryInterfaceID
	f.systemDNS = snapshot.SystemDNSServers
	// 同步模式与拓扑，使回滚完整恢复 mode/bond（R4.1）
	f.mode = snapshot.Mode
	f.bond = snapshot.Bond

	return snapshot, nil
}

// applyMode 处理工作模式切换：
// - 进入 active-backup：创建 bond0、标记 slave 归属并清空其 IPv4，填充 BondTopology；
// - 进入 lacp-aggregation：创建 bond0、标记 slave 归属并清空其 IPv4，填充 BondTopology 与 LACPStatus；
// - 退回 multi-address（含空值归一化）：拆除 bond0、归还 slave（IPv4 由主循环按 plan.Interfaces 恢复）。
func (f *FakePlatform) applyMode(plan HostPlan) {
	if (plan.Mode == NetworkModeActiveBackup || plan.Mode == NetworkModeLACP) && plan.Bond != nil {
		bondID := fakeBondInterfaceID
		// 汇总 slave 链路状态：任一 up 即 up；MAC 取首个或 primary slave 的 MAC
		anyUp := false
		var primaryMAC *string
		for _, sid := range plan.Bond.SlaveIDs {
			slave, ok := f.interfaces[sid]
			if !ok {
				continue
			}
			if slave.LinkStatus == LinkUp {
				anyUp = true
			}
			if plan.Mode == NetworkModeActiveBackup && sid == plan.Bond.PrimarySlaveID {
				primaryMAC = slave.MAC
			} else if primaryMAC == nil {
				primaryMAC = slave.MAC
			}
		}
		linkStatus := LinkDown
		if anyUp {
			linkStatus = LinkUp
		}

		f.interfaces[bondID] = InterfaceInfo{
			ID:          bondID,
			Name:        bondID,
			DisplayName: "Bond",
			Type:        InterfaceEthernet,
			MAC:         primaryMAC,
			LinkStatus:  linkStatus,
			Ownership:   OwnershipManaged,
			Writable:    true,
			IsPrimary:   plan.PrimaryInterfaceID != nil && *plan.PrimaryInterfaceID == bondID,
			IsBond:      true,
			IPv4: IPv4State{
				Mode:   IPModeUnknown,
				Status: IPStatusUnavailable,
			},
			Fingerprint: "fp-" + bondID,
		}

		// 标记 slave：归属 bond、不可写、清空 IPv4（D3）
		for _, sid := range plan.Bond.SlaveIDs {
			slave, ok := f.interfaces[sid]
			if !ok {
				continue
			}
			slave.MasterID = &bondID
			slave.Writable = false
			slave.IsPrimary = false
			slave.IPv4 = IPv4State{Mode: IPModeUnknown, Status: IPStatusUnavailable}
			f.interfaces[sid] = slave
		}

		if plan.Mode == NetworkModeActiveBackup {
			activeID := plan.Bond.PrimarySlaveID
			f.mode = NetworkModeActiveBackup
			f.bond = &BondTopology{
				BondInterfaceID: bondID,
				SlaveIDs:        plan.Bond.SlaveIDs,
				PrimarySlaveID:  plan.Bond.PrimarySlaveID,
				ActiveSlaveID:   &activeID,
				Miimon:          plan.Bond.Miimon,
			}
		} else {
			f.mode = NetworkModeLACP
			hashPolicy := BondXmitHashPolicyLayer23
			if plan.Bond.XmitHashPolicy != nil {
				hashPolicy = *plan.Bond.XmitHashPolicy
			}
			f.bond = &BondTopology{
				BondInterfaceID: bondID,
				SlaveIDs:        plan.Bond.SlaveIDs,
				XmitHashPolicy:  &hashPolicy,
				LACP:            f.buildLACPStatus(plan.Bond.SlaveIDs),
			}
		}
		return
	}

	// 退回 multi-address（空值等价 multi-address）
	f.mode = plan.Mode.Normalize()
	f.bond = nil
	delete(f.interfaces, fakeBondInterfaceID)
	for id, iface := range f.interfaces {
		if iface.MasterID != nil {
			iface.MasterID = nil
			iface.Writable = true
			f.interfaces[id] = iface
		}
	}
}

// lacpPortState 生成 LACP 端口状态位：inAgg=true 表示已加入聚合组并完成协商，
// 否则处于 defaulted 待协商状态。
func lacpPortState(inAgg bool) LACPPortState {
	return LACPPortState{
		Active:       true,
		ShortTimeout: false,
		Aggregation:  true,
		Synchronized: inAgg,
		Collecting:   inAgg,
		Distributing: inAgg,
		Defaulted:    !inAgg,
		Expired:      false,
	}
}

// lacpAggregatorID 返回聚合组 ID 指针：inAgg 时指向 1，否则为 nil。
func lacpAggregatorID(inAgg bool) *uint16 {
	if !inAgg {
		return nil
	}
	v := uint16(1)
	return &v
}

func (f *FakePlatform) buildLACPStatus(slaveIDs []string) *LACPStatus {
	scenario := f.lacpScenario
	if scenario == "" {
		scenario = FakeLACPScenarioNegotiated
	}
	negotiated := scenario == FakeLACPScenarioNegotiated

	slaves := make([]LACPPortStatus, 0, len(slaveIDs))
	for i, sid := range slaveIDs {
		inAgg := negotiated || (scenario == FakeLACPScenarioPartial && i == 0)
		partner := LACPPortState{Defaulted: true}
		if inAgg {
			partner = lacpPortState(true)
		}
		slaves = append(slaves, LACPPortStatus{
			InterfaceID:  sid,
			AggregatorID: lacpAggregatorID(inAgg),
			InAggregator: inAgg,
			ActorState:   lacpPortState(inAgg),
			PartnerState: partner,
		})
	}

	status := &LACPStatus{
		Negotiated: negotiated,
		Slaves:     slaves,
	}
	if negotiated || scenario == FakeLACPScenarioPartial {
		status.AggregatorID = lacpAggregatorID(true)
	}
	if scenario == FakeLACPScenarioNone {
		status.DiagnosticCode = "partner_not_configured"
	}
	return status
}

func (f *FakePlatform) Close(ctx context.Context) error {
	return nil
}
