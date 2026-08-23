package netconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
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
	wantModes := []NetworkMode{NetworkModeMultiAddress, NetworkModeActiveBackup}
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

// TestFakeBondRestore 验证 Restore(before) 完整回滚 bond 拓扑与模式（M3 / R4.1 / AC6）。
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

func intPtr(i int) *int {
	return &i
}
