package netconfig

import (
	"errors"
	"os"
	"path/filepath"
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
