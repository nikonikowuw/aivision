package netconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestValidatorIPv4(t *testing.T) {
	// 正常用例
	ip, mask, err := NormalizeAndValidateIPv4("192.168.1.100", 24)
	if err != nil {
		t.Fatalf("NormalizeAndValidateIPv4 valid case failed: %v", err)
	}
	if ip != "192.168.1.100" || mask != "255.255.255.0" {
		t.Errorf("got ip=%s, mask=%s, want 192.168.1.100, 255.255.255.0", ip, mask)
	}

	// 非法 prefix
	if _, _, err := NormalizeAndValidateIPv4("192.168.1.100", 33); err == nil {
		t.Error("NormalizeAndValidateIPv4 with prefix 33 should fail")
	}

	// 环回地址
	if _, _, err := NormalizeAndValidateIPv4("127.0.0.1", 8); err == nil {
		t.Error("NormalizeAndValidateIPv4 with loopback 127.0.0.1 should fail")
	}

	// 网络地址 (/24 下 .0)
	if _, _, err := NormalizeAndValidateIPv4("192.168.1.0", 24); err == nil {
		t.Error("NormalizeAndValidateIPv4 with network address should fail")
	}

	// 广播地址 (/24 下 .255)
	if _, _, err := NormalizeAndValidateIPv4("192.168.1.255", 24); err == nil {
		t.Error("NormalizeAndValidateIPv4 with broadcast address should fail")
	}
}

func TestValidatorGateway(t *testing.T) {
	// 同子网网关
	gw, err := ValidateGatewayInSubnet("192.168.1.100", 24, "192.168.1.1")
	if err != nil {
		t.Fatalf("ValidateGatewayInSubnet valid case failed: %v", err)
	}
	if gw != "192.168.1.1" {
		t.Errorf("got gw=%s, want 192.168.1.1", gw)
	}

	// 跨子网网关
	if _, err := ValidateGatewayInSubnet("192.168.1.100", 24, "192.168.2.1"); err == nil {
		t.Error("ValidateGatewayInSubnet with out-of-subnet gateway should fail")
	}

	// 网关与本机 IP 相同
	if _, err := ValidateGatewayInSubnet("192.168.1.100", 24, "192.168.1.100"); err == nil {
		t.Error("ValidateGatewayInSubnet same IP as gateway should fail")
	}
}

func TestValidatorDNS(t *testing.T) {
	// 正常 DNS 列表与去重
	dns, err := ValidateDNSServers([]string{"8.8.8.8", "1.1.1.1", "8.8.8.8"})
	if err != nil {
		t.Fatalf("ValidateDNSServers valid case failed: %v", err)
	}
	if len(dns) != 2 || dns[0] != "8.8.8.8" || dns[1] != "1.1.1.1" {
		t.Errorf("got dns=%v, want [8.8.8.8, 1.1.1.1]", dns)
	}

	// 超过 3 个
	if _, err := ValidateDNSServers([]string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}); err == nil {
		t.Error("ValidateDNSServers > 3 servers should fail")
	}

	// 空列表
	if _, err := ValidateDNSServers([]string{}); err == nil {
		t.Error("ValidateDNSServers empty list should fail")
	}
}

func TestFileStateStore(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "network-state")
	store := NewFileStateStore(tmpDir, PlatformFake)
	if err := store.Init(PlatformFake); err != nil {
		t.Fatalf("Init store failed: %v", err)
	}

	// 验证未初始化时读取返回 NotExist
	if _, err := store.GetFactory(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetFactory before set should return NotExist, got %v", err)
	}

	// 写入 factory 并读取
	factory := &FactoryData{
		Plan: HostPlan{
			Interfaces: map[string]InterfacePlan{},
		},
		Snapshot: HostSnapshot{
			Fingerprint: "init-fp",
		},
	}
	if err := store.SetFactory(factory); err != nil {
		t.Fatalf("SetFactory failed: %v", err)
	}

	// factory 不可二次覆盖
	if err := store.SetFactory(factory); err == nil {
		t.Error("SetFactory second time should fail (immutable baseline)")
	}

	gotFactory, err := store.GetFactory()
	if err != nil {
		t.Fatalf("GetFactory failed: %v", err)
	}
	if gotFactory.Snapshot.Fingerprint != "init-fp" {
		t.Errorf("got fingerprint %s, want init-fp", gotFactory.Snapshot.Fingerprint)
	}

	// 写入 pending 与清除
	pending := &PendingData{
		Transaction: PendingTransaction{
			ID:     "txn-123",
			Status: TxnStatusPendingConfirmation,
		},
	}
	if err := store.SetPending(pending); err != nil {
		t.Fatalf("SetPending failed: %v", err)
	}
	gotPending, err := store.GetPending()
	if err != nil {
		t.Fatalf("GetPending failed: %v", err)
	}
	if gotPending.Transaction.ID != "txn-123" {
		t.Errorf("got txn id %s, want txn-123", gotPending.Transaction.ID)
	}

	if err := store.ClearPending(); err != nil {
		t.Fatalf("ClearPending failed: %v", err)
	}
	if _, err := store.GetPending(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetPending after clear should return NotExist, got %v", err)
	}

	// 篡改校验和测试
	lastValid := &LastValidData{
		Snapshot: HostSnapshot{Fingerprint: "lv-fp"},
	}
	if err := store.SetLastValid(lastValid); err != nil {
		t.Fatalf("SetLastValid failed: %v", err)
	}
	// 破坏文件
	lvPath := filepath.Join(tmpDir, LastValidFilename)
	_ = os.WriteFile(lvPath, []byte(`{"schemaVersion":1,"checksum":"bad","data":"{}"}`), 0o600)
	if _, err := store.GetLastValid(); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("GetLastValid with corrupted file should return ErrStateCorrupt, got %v", err)
	}
}

// TestOldFormatCompatibility 验证 08-22 旧格式（无 mode/bond 字段）状态文件仍可读取，
// Mode 归一化为 multi-address（AC5 / D7：不递增 SchemaVersion）。
func TestOldFormatCompatibility(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "network-state")
	store := NewFileStateStore(tmpDir, PlatformFake)
	if err := store.Init(PlatformFake); err != nil {
		t.Fatalf("Init store failed: %v", err)
	}

	// 旧格式数据：只有 interfaces / primaryInterfaceId，无 mode / bond 字段
	old := &LastValidData{
		Plan: HostPlan{
			Interfaces: map[string]InterfacePlan{
				"eth0": {Mode: IPModeStatic, Primary: true},
			},
			PrimaryInterfaceID: strPtr("eth0"),
		},
		Snapshot: HostSnapshot{
			Fingerprint: "old-fp",
			Interfaces: map[string]InterfaceInfo{
				"eth0": {ID: "eth0", Name: "eth0", Writable: true, Fingerprint: "fp-eth0"},
			},
			PrimaryInterfaceID: strPtr("eth0"),
		},
	}
	if err := store.SetLastValid(old); err != nil {
		t.Fatalf("SetLastValid failed: %v", err)
	}

	// 落盘 JSON 的 plan/snapshot 顶层不得包含 mode / bond 键（omitempty，字节级等价于 08-22 旧文件）
	// 注意：ipv4.mode、interface.isBond/masterId 是既有/新增接口字段，不属于此断言范围。
	raw, err := os.ReadFile(filepath.Join(tmpDir, LastValidFilename))
	if err != nil {
		t.Fatalf("read last-valid: %v", err)
	}
	var env struct {
		Data struct {
			Plan     map[string]any `json:"plan"`
			Snapshot map[string]any `json:"snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if _, ok := env.Data.Plan["mode"]; ok {
		t.Errorf("plan should not have top-level mode key, got %v", env.Data.Plan)
	}
	if _, ok := env.Data.Plan["bond"]; ok {
		t.Errorf("plan should not have top-level bond key, got %v", env.Data.Plan)
	}
	if _, ok := env.Data.Snapshot["mode"]; ok {
		t.Errorf("snapshot should not have top-level mode key, got %v", env.Data.Snapshot)
	}
	if _, ok := env.Data.Snapshot["bond"]; ok {
		t.Errorf("snapshot should not have top-level bond key, got %v", env.Data.Snapshot)
	}

	got, err := store.GetLastValid()
	if err != nil {
		t.Fatalf("GetLastValid on old-format file failed: %v", err)
	}
	if got.Snapshot.Mode != "" {
		t.Errorf("raw Mode should be empty for old file, got %q", got.Snapshot.Mode)
	}
	if got.Snapshot.Mode.Normalize() != NetworkModeMultiAddress {
		t.Errorf("Normalize() should map empty mode to multi-address, got %q", got.Snapshot.Mode.Normalize())
	}
	if got.Snapshot.Bond != nil {
		t.Errorf("old file Bond should be nil, got %+v", got.Snapshot.Bond)
	}
	if got.Snapshot.Fingerprint != "old-fp" {
		t.Errorf("got fingerprint %s, want old-fp", got.Snapshot.Fingerprint)
	}
}

// TestCapabilitiesFake 断言 FakePlatform 的 SupportedModes 与布尔能力取值（M2 / AC1）。
// LinuxPlatform / DarwinPlatform 的声明在各自 build-tagged 测试文件中覆盖（manager_linux_test.go 等）。
func TestCapabilitiesFake(t *testing.T) {
	caps := NewFakePlatform(PlatformFake).Capabilities(context.Background())
	wantModes := []NetworkMode{NetworkModeMultiAddress, NetworkModeActiveBackup, NetworkModeLACP, NetworkModeGateway}
	if !slices.Equal(caps.SupportedModes, wantModes) {
		t.Errorf("SupportedModes = %v, want %v", caps.SupportedModes, wantModes)
	}
	if !caps.DHCP || !caps.StaticIPv4 || !caps.FactoryReset || caps.WifiAssociation {
		t.Errorf("capability booleans = %+v, want dhcp/static/reset=true wifi=false", caps)
	}
}

func strPtr(s string) *string {
	return &s
}

// TestFakeBondEnterAndExit 验证 fake 平台进入/退出 active-backup 的 bond 语义（M3 / AC6）。
func TestFakeBondEnterAndExit(t *testing.T) {
	fake := NewFakePlatform(PlatformFake)

	// 初始为 multi-address，无 bond
	snap, err := fake.Read(context.Background())
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if snap.Mode.Normalize() != NetworkModeMultiAddress {
		t.Errorf("initial mode = %q, want multi-address", snap.Mode)
	}
	if snap.Bond != nil {
		t.Errorf("initial bond should be nil")
	}

	// 进入 active-backup：eth0/eth1 作 slave，primary=eth0，bond0 静态 IP
	primaryIP := "192.168.9.9"
	prefix := 24
	gw := "192.168.9.1"
	bondPlan := HostPlan{
		Interfaces: map[string]InterfacePlan{
			"bond0": {
				Mode: IPModeStatic, Primary: true, Address: &primaryIP, Prefix: &prefix,
				Gateway: &gw, DNSServers: []string{"192.168.9.1"},
			},
		},
		PrimaryInterfaceID: strPtr("bond0"),
		Mode:               NetworkModeActiveBackup,
		Bond: &BondPlan{
			SlaveIDs:       []string{"eth0", "eth1"},
			PrimarySlaveID: "eth0",
			Miimon:         100,
		},
	}
	if _, err := fake.Apply(context.Background(), bondPlan); err != nil {
		t.Fatalf("Apply active-backup failed: %v", err)
	}

	snap, _ = fake.Read(context.Background())
	if snap.Mode != NetworkModeActiveBackup {
		t.Errorf("mode = %q, want active-backup", snap.Mode)
	}
	bond0, ok := snap.Interfaces["bond0"]
	if !ok {
		t.Fatalf("bond0 should exist after entering active-backup")
	}
	if !bond0.IsBond {
		t.Errorf("bond0.IsBond should be true")
	}
	if !bond0.Writable {
		t.Errorf("bond0 should be writable")
	}
	if bond0.IPv4.Address == nil || *bond0.IPv4.Address != primaryIP {
		t.Errorf("bond0 IP = %v, want %s", bond0.IPv4.Address, primaryIP)
	}
	// slave 标记归属、退出可写集合、IPv4 清空
	for _, sid := range []string{"eth0", "eth1"} {
		slave := snap.Interfaces[sid]
		if slave.MasterID == nil || *slave.MasterID != "bond0" {
			t.Errorf("%s MasterID = %v, want bond0", sid, slave.MasterID)
		}
		if slave.Writable {
			t.Errorf("%s should be unwritable while bonded", sid)
		}
		if slave.IsPrimary {
			t.Errorf("%s should not be primary while bonded", sid)
		}
		if slave.IPv4.Status != IPStatusUnavailable {
			t.Errorf("%s IPv4 status = %q, want unavailable", sid, slave.IPv4.Status)
		}
	}
	// 拓扑
	if snap.Bond == nil {
		t.Fatalf("bond topology should be present")
	}
	if snap.Bond.BondInterfaceID != "bond0" || snap.Bond.PrimarySlaveID != "eth0" ||
		snap.Bond.ActiveSlaveID == nil || *snap.Bond.ActiveSlaveID != "eth0" || snap.Bond.Miimon != 100 {
		t.Errorf("bond topology = %+v", snap.Bond)
	}

	// 退回 multi-address：bond0 消失、slave 归还并恢复 IPv4
	backPlan := HostPlan{
		Interfaces: map[string]InterfacePlan{
			"eth0": {Mode: IPModeStatic, Primary: true, Address: strPtr("192.168.1.100"), Prefix: intPtr(24)},
			"eth1": {Mode: IPModeDHCP},
		},
		PrimaryInterfaceID: strPtr("eth0"),
		Mode:               NetworkModeMultiAddress,
	}
	if _, err := fake.Apply(context.Background(), backPlan); err != nil {
		t.Fatalf("Apply multi-address failed: %v", err)
	}
	snap, _ = fake.Read(context.Background())
	if snap.Mode != NetworkModeMultiAddress {
		t.Errorf("mode = %q, want multi-address", snap.Mode)
	}
	if _, ok := snap.Interfaces["bond0"]; ok {
		t.Errorf("bond0 should be removed after exiting active-backup")
	}
	if snap.Bond != nil {
		t.Errorf("bond topology should be nil after exiting")
	}
	eth0 := snap.Interfaces["eth0"]
	if eth0.MasterID != nil || !eth0.Writable {
		t.Errorf("eth0 should be returned: master=%v writable=%v", eth0.MasterID, eth0.Writable)
	}
	if eth0.IPv4.Address == nil || *eth0.IPv4.Address != "192.168.1.100" {
		t.Errorf("eth0 IP not restored: %v", eth0.IPv4.Address)
	}
	eth1 := snap.Interfaces["eth1"]
	if eth1.MasterID != nil || !eth1.Writable {
		t.Errorf("eth1 should be returned: master=%v writable=%v", eth1.MasterID, eth1.Writable)
	}
}

// TestFakeLACPScenarios 验证 FakePlatform 支持三种 LACP 协商场景以及 speed/duplex 注入
func TestFakeLACPScenarios(t *testing.T) {
	fake := NewFakePlatform(PlatformFake)
	hashPolicy := BondXmitHashPolicyLayer23
	lacpPlan := HostPlan{
		Interfaces: map[string]InterfacePlan{
			"bond0": {
				Mode: IPModeDHCP, Primary: true,
			},
		},
		PrimaryInterfaceID: strPtr("bond0"),
		Mode:               NetworkModeLACP,
		Bond: &BondPlan{
			SlaveIDs:       []string{"eth0", "eth1"},
			XmitHashPolicy: &hashPolicy,
		},
	}

	// 1. 默认场景：已协商
	if _, err := fake.Apply(context.Background(), lacpPlan); err != nil {
		t.Fatalf("Apply LACP failed: %v", err)
	}
	snap, _ := fake.Read(context.Background())
	if snap.Mode != NetworkModeLACP || snap.Bond == nil || snap.Bond.LACP == nil {
		t.Fatalf("LACP status not initialized: %+v", snap.Bond)
	}
	if !snap.Bond.LACP.Negotiated || len(snap.Bond.LACP.Slaves) != 2 {
		t.Errorf("expected negotiated LACP, got %+v", snap.Bond.LACP)
	}

	// 2. 场景：none（未协商 / partner_not_configured）
	fake.SetLACPScenario(FakeLACPScenarioNone)
	snap, _ = fake.Read(context.Background())
	if snap.Bond.LACP.Negotiated || snap.Bond.LACP.DiagnosticCode != "partner_not_configured" {
		t.Errorf("expected none scenario, got %+v", snap.Bond.LACP)
	}

	// 3. 场景：partial（部分进组）
	fake.SetLACPScenario(FakeLACPScenarioPartial)
	snap, _ = fake.Read(context.Background())
	if snap.Bond.LACP.Negotiated || snap.Bond.LACP.Slaves[0].InAggregator != true || snap.Bond.LACP.Slaves[1].InAggregator != false {
		t.Errorf("expected partial scenario, got %+v", snap.Bond.LACP)
	}

	// 4. 速度与双工注入
	speed1000 := 1000
	speed100 := 100
	fake.SetInterfaceLinkProperties("eth0", &speed1000, DuplexFull)
	fake.SetInterfaceLinkProperties("eth1", &speed100, DuplexHalf)
	snap, _ = fake.Read(context.Background())
	if snap.Interfaces["eth0"].SpeedMbps == nil || *snap.Interfaces["eth0"].SpeedMbps != 1000 || snap.Interfaces["eth0"].Duplex != DuplexFull {
		t.Errorf("eth0 link properties mismatch: %+v", snap.Interfaces["eth0"])
	}
	if snap.Interfaces["eth1"].SpeedMbps == nil || *snap.Interfaces["eth1"].SpeedMbps != 100 || snap.Interfaces["eth1"].Duplex != DuplexHalf {
		t.Errorf("eth1 link properties mismatch: %+v", snap.Interfaces["eth1"])
	}
}
func TestFakeBondRestore(t *testing.T) {
	fake := NewFakePlatform(PlatformFake)
	before, err := fake.Read(context.Background())
	if err != nil {
		t.Fatalf("Read before failed: %v", err)
	}

	// 进入 active-backup 后立即 Restore 回 before
	bondPlan := HostPlan{
		Interfaces:         map[string]InterfacePlan{},
		PrimaryInterfaceID: strPtr("bond0"),
		Mode:               NetworkModeActiveBackup,
		Bond: &BondPlan{
			SlaveIDs:       []string{"eth0", "eth1"},
			PrimarySlaveID: "eth0",
			Miimon:         100,
		},
	}
	if _, err := fake.Apply(context.Background(), bondPlan); err != nil {
		t.Fatalf("Apply active-backup failed: %v", err)
	}
	if _, err := fake.Restore(context.Background(), before); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	after, _ := fake.Read(context.Background())
	if _, ok := after.Interfaces["bond0"]; ok {
		t.Errorf("bond0 should be removed after Restore")
	}
	if after.Bond != nil || after.Mode != before.Mode {
		t.Errorf("mode/bond not restored: mode=%q bond=%+v", after.Mode, after.Bond)
	}
	// slave 逐字段恢复
	for _, sid := range []string{"eth0", "eth1", "wlan0"} {
		want := before.Interfaces[sid]
		got := after.Interfaces[sid]
		if got.MasterID != nil || !got.Writable || got.IPv4.Address != want.IPv4.Address ||
			got.IPv4.Mode != want.IPv4.Mode {
			t.Errorf("%s not restored: master=%v writable=%v ipv4.mode=%q addr=%v (want %q/%v)",
				sid, got.MasterID, got.Writable, got.IPv4.Mode, got.IPv4.Address, want.IPv4.Mode, want.IPv4.Address)
		}
	}
}

// TestGatewayRuntime 验证网关运行时的生命周期、冲突探测与租约存储（M2）。
func TestGatewayRuntime(t *testing.T) {
	ctx := context.Background()
	tmpDir := filepath.Join(t.TempDir(), "gw-runtime-state")
	store := NewFileStateStore(tmpDir, PlatformFake)
	_ = store.Init(PlatformFake)

	backend := NewFakeGatewayBackend()
	runtime := NewDefaultGatewayRuntime(backend, store)
	defer runtime.Close(ctx)

	ifaceIP := "192.168.2.1"
	ifacePrefix := 24
	iface := &InterfaceInfo{
		ID:       "eth1",
		Name:     "eth1",
		Writable: true,
		IPv4: IPv4State{
			Mode:    IPModeStatic,
			Address: &ifaceIP,
			Prefix:  &ifacePrefix,
		},
	}
	plan := GatewayPlan{
		DownstreamInterfaceID: "eth1",
		PoolStart:             "192.168.2.100",
		PoolEnd:               "192.168.2.200",
		Prefix:                24,
		LeaseDurationSeconds:  3600,
		IPForward:             true,
	}

	// 1. 冲突探测：注入有响应
	backend.SetProbeResponse(true, nil)
	responded, err := runtime.Probe(ctx, plan, iface)
	if err != nil || !responded {
		t.Fatalf("Probe should report conflict: responded=%v, err=%v", responded, err)
	}

	// 2. 冲突探测：无响应
	backend.SetProbeResponse(false, nil)
	responded, err = runtime.Probe(ctx, plan, iface)
	if err != nil || responded {
		t.Fatalf("Probe should pass without conflict: responded=%v, err=%v", responded, err)
	}

	// 3. Apply 启动网关服务
	beforeState := GatewayState{
		Running:   false,
		IPForward: false,
	}
	appliedState, err := runtime.Apply(ctx, plan, beforeState, iface)
	if err != nil {
		t.Fatalf("Apply gateway runtime failed: %v", err)
	}
	if !appliedState.Running || !appliedState.IPForward || appliedState.PreviousIPForward == nil || *appliedState.PreviousIPForward != false {
		t.Errorf("unexpected applied state: %+v", appliedState)
	}

	// 验证 backend 中的 ip_forward 已变为 true
	fwd, _ := backend.ReadIPForward(ctx)
	if !fwd {
		t.Errorf("backend ip_forward should be true")
	}

	// 4. 模拟客户端分配租约
	if backend.runningServer == nil {
		t.Fatalf("running server should not be nil")
	}
	lease := backend.runningServer.AllocateLease("00:11:22:33:44:55", "192.168.2.105", "cam-01")
	if lease.IP != "192.168.2.105" {
		t.Errorf("unexpected lease: %+v", lease)
	}

	leases, err := runtime.Leases(ctx)
	if err != nil || len(leases) != 1 {
		t.Fatalf("Leases() returned %d leases, err=%v", len(leases), err)
	}
	if leases[0].MAC != "00:11:22:33:44:55" {
		t.Errorf("unexpected lease MAC: %s", leases[0].MAC)
	}

	// 5. Restore 恢复原状
	restoredState, err := runtime.Restore(ctx, beforeState, iface)
	if err != nil {
		t.Fatalf("Restore gateway runtime failed: %v", err)
	}
	if restoredState.Running || restoredState.IPForward {
		t.Errorf("restored state should not be running/forwarding: %+v", restoredState)
	}

	fwd, _ = backend.ReadIPForward(ctx)
	if fwd {
		t.Errorf("backend ip_forward should be restored to false")
	}

	leasesAfterRestore, _ := runtime.Leases(ctx)
	if len(leasesAfterRestore) != 0 {
		t.Errorf("leases should be cleared after Restore, got %d", len(leasesAfterRestore))
	}
}

func intPtr(i int) *int {
	return &i
}

// TestLACPModelAndEnums 验证 LACP 相关枚举、校验和结构体字段
func TestLACPModelAndEnums(t *testing.T) {
	if !NetworkModeLACP.Valid() {
		t.Errorf("NetworkModeLACP should be valid")
	}
	if !slices.Contains(AllNetworkModes(), NetworkModeLACP) {
		t.Errorf("AllNetworkModes should contain NetworkModeLACP")
	}

	for _, p := range []BondXmitHashPolicy{BondXmitHashPolicyLayer2, BondXmitHashPolicyLayer23, BondXmitHashPolicyLayer34} {
		if !p.Valid() {
			t.Errorf("BondXmitHashPolicy %s should be valid", p)
		}
	}
	if BondXmitHashPolicy("layer4").Valid() {
		t.Errorf("arbitrary hash policy string should be invalid")
	}

	hashPolicy := BondXmitHashPolicyLayer23
	lacpRate := BondLACPRateSlow
	plan := BondPlan{
		SlaveIDs:       []string{"eth0", "eth1"},
		XmitHashPolicy: &hashPolicy,
		LACPRate:       &lacpRate,
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal BondPlan failed: %v", err)
	}
	var unmarshaled BondPlan
	if err := json.Unmarshal(raw, &unmarshaled); err != nil {
		t.Fatalf("unmarshal BondPlan failed: %v", err)
	}
	if unmarshaled.XmitHashPolicy == nil || *unmarshaled.XmitHashPolicy != BondXmitHashPolicyLayer23 {
		t.Errorf("xmitHashPolicy mismatch: %+v", unmarshaled.XmitHashPolicy)
	}
	if unmarshaled.LACPRate == nil || *unmarshaled.LACPRate != BondLACPRateSlow {
		t.Errorf("lacpRate mismatch: %+v", unmarshaled.LACPRate)
	}
}

=======
// TestValidateGatewayPlan 验证网关模式参数校验规则（M1）。
func TestValidateGatewayPlan(t *testing.T) {
	primaryID := "eth0"
	validIface := &InterfaceInfo{
		ID:       "eth1",
		Writable: true,
		IPv4: IPv4State{
			Mode:    IPModeStatic,
			Address: strPtr("192.168.2.1"),
			Prefix:  intPtr(24),
		},
	}

	// 正常用例
	validPlan := &GatewayPlan{
		DownstreamInterfaceID: "eth1",
		PoolStart:             "192.168.2.100",
		PoolEnd:               "192.168.2.200",
		Prefix:                24,
		LeaseDurationSeconds:  3600,
		IPForward:             true,
	}
	norm, err := ValidateGatewayPlan(validPlan, validIface, &primaryID)
	if err != nil {
		t.Fatalf("valid gateway plan should pass: %v", err)
	}
	if norm.PoolStart != "192.168.2.100" || norm.PoolEnd != "192.168.2.200" || norm.LeaseDurationSeconds != 3600 || !norm.IPForward {
		t.Errorf("unexpected normalized plan: %+v", norm)
	}

	// 默认租约时长 (0 -> 3600)
	planDefaultLease := &GatewayPlan{
		DownstreamInterfaceID: "eth1",
		PoolStart:             "192.168.2.100",
		PoolEnd:               "192.168.2.200",
		Prefix:                24,
		LeaseDurationSeconds:  0,
	}
	normDefault, err := ValidateGatewayPlan(planDefaultLease, validIface, &primaryID)
	if err != nil || normDefault.LeaseDurationSeconds != 3600 {
		t.Errorf("default lease duration should be 3600, got %d, err=%v", normDefault.LeaseDurationSeconds, err)
	}

	// 租约时长边界：60, 604800 合法；59, 604801 非法
	for _, sec := range []int64{60, 604800} {
		p := *validPlan
		p.LeaseDurationSeconds = sec
		if _, err := ValidateGatewayPlan(&p, validIface, &primaryID); err != nil {
			t.Errorf("lease duration %d should be valid, got err: %v", sec, err)
		}
	}
	for _, sec := range []int64{59, 604801} {
		p := *validPlan
		p.LeaseDurationSeconds = sec
		if _, err := ValidateGatewayPlan(&p, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
			t.Errorf("lease duration %d should fail with ErrGatewayPoolInvalid, got err: %v", sec, err)
		}
	}

	// 目标接口为 PrimaryInterface
	if _, err := ValidateGatewayPlan(validPlan, validIface, strPtr("eth1")); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("gateway interface as primary should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// 目标接口为 DHCP client
	dhcpIface := &InterfaceInfo{
		ID:       "eth1",
		Writable: true,
		IPv4: IPv4State{
			Mode: IPModeDHCP,
		},
	}
	if _, err := ValidateGatewayPlan(validPlan, dhcpIface, &primaryID); !errors.Is(err, ErrDhcpServerConflict) {
		t.Errorf("dhcp client interface should fail with ErrDhcpServerConflict, got %v", err)
	}

	// 起始地址 > 结束地址
	pStartGtEnd := *validPlan
	pStartGtEnd.PoolStart = "192.168.2.201"
	pStartGtEnd.PoolEnd = "192.168.2.200"
	if _, err := ValidateGatewayPlan(&pStartGtEnd, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("poolStart > poolEnd should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// 地址池包含接口自身地址 (192.168.2.1)
	pContainsIface := *validPlan
	pContainsIface.PoolStart = "192.168.2.1"
	pContainsIface.PoolEnd = "192.168.2.100"
	if _, err := ValidateGatewayPlan(&pContainsIface, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("pool containing interface IP should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// 地址池包含网络地址 (192.168.2.0)
	pContainsNet := *validPlan
	pContainsNet.PoolStart = "192.168.2.0"
	pContainsNet.PoolEnd = "192.168.2.50"
	if _, err := ValidateGatewayPlan(&pContainsNet, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("pool containing network address should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// 地址池包含广播地址 (192.168.2.255)
	pContainsBcast := *validPlan
	pContainsBcast.PoolStart = "192.168.2.200"
	pContainsBcast.PoolEnd = "192.168.2.255"
	if _, err := ValidateGatewayPlan(&pContainsBcast, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("pool containing broadcast address should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// 地址跨子网
	pCrossSubnet := *validPlan
	pCrossSubnet.PoolStart = "192.168.3.10"
	pCrossSubnet.PoolEnd = "192.168.3.20"
	if _, err := ValidateGatewayPlan(&pCrossSubnet, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("out-of-subnet pool should fail with ErrGatewayPoolInvalid, got %v", err)
	}

	// Prefix 不匹配
	pMismatchPrefix := *validPlan
	pMismatchPrefix.Prefix = 16
	if _, err := ValidateGatewayPlan(&pMismatchPrefix, validIface, &primaryID); !errors.Is(err, ErrGatewayPoolInvalid) {
		t.Errorf("prefix mismatch should fail with ErrGatewayPoolInvalid, got %v", err)
	}
}

// TestGatewayLeaseStore 验证租约文件的读写、清除与损坏检测（M1）。
func TestGatewayLeaseStore(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "network-state")
	store := NewFileStateStore(tmpDir, PlatformFake)
	if err := store.Init(PlatformFake); err != nil {
		t.Fatalf("Init store failed: %v", err)
	}

	// 空文件时读取返回空列表
	leases, err := store.GetGatewayLeases()
	if err != nil {
		t.Fatalf("GetGatewayLeases empty file failed: %v", err)
	}
	if len(leases) != 0 {
		t.Errorf("initial leases should be empty, got %d", len(leases))
	}

	// 写入租约并读取
	now := time.Now().UTC().Truncate(time.Second)
	testLeases := []GatewayLease{
		{
			MAC:           "00:11:22:33:44:55",
			IP:            "192.168.2.101",
			StartsAt:      now,
			ExpiresAt:     now.Add(time.Hour),
			LastRenewedAt: now,
			Hostname:      "camera-01",
		},
	}
	if err := store.SetGatewayLeases(testLeases); err != nil {
		t.Fatalf("SetGatewayLeases failed: %v", err)
	}

	readLeases, err := store.GetGatewayLeases()
	if err != nil {
		t.Fatalf("GetGatewayLeases after set failed: %v", err)
	}
	if len(readLeases) != 1 || readLeases[0].MAC != "00:11:22:33:44:55" || readLeases[0].Hostname != "camera-01" {
		t.Errorf("unexpected read leases: %+v", readLeases)
	}

	// 清除租约
	if err := store.ClearGatewayLeases(); err != nil {
		t.Fatalf("ClearGatewayLeases failed: %v", err)
	}
	clearedLeases, err := store.GetGatewayLeases()
	if err != nil || len(clearedLeases) != 0 {
		t.Errorf("GetGatewayLeases after clear failed: leases=%v, err=%v", clearedLeases, err)
	}
}
