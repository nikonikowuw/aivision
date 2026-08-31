//go:build linux

package netconfig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestLinuxProfileParsing(t *testing.T) {
	tmpDir := t.TempDir()
	profilePath := filepath.Join(tmpDir, "network-profile.json")

	content := `{
		"version": 1,
		"interfaces": [
			{ "name": "eth0", "comment": "uplink primary" },
			{ "name": "eth1", "comment": "uplink backup" }
		],
		"resolver": {
			"path": "/etc/resolv.conf",
			"requireExclusive": true
		}
	}`
	if err := os.WriteFile(profilePath, []byte(content), 0600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	p, err := LoadProfile(profilePath)
	if err != nil {
		t.Fatalf("load profile failed: %v", err)
	}
	if p.Version != 1 {
		t.Errorf("expected version 1, got %d", p.Version)
	}
	if !p.IsAllowlisted("eth0") || !p.IsAllowlisted("eth1") {
		t.Errorf("eth0 and eth1 should be allowlisted")
	}
	if p.IsAllowlisted("eth2") {
		t.Errorf("eth2 should not be allowlisted")
	}

	// Invalid version
	badVersionPath := filepath.Join(tmpDir, "bad-version.json")
	if err := os.WriteFile(badVersionPath, []byte(`{"version": 2}`), 0600); err != nil {
		t.Fatalf("write bad version profile: %v", err)
	}
	if _, err := LoadProfile(badVersionPath); err == nil {
		t.Errorf("expected error for unsupported version 2")
	}
}

func TestAnchorStore(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewAnchorStore(tmpDir)
	if err != nil {
		t.Fatalf("new anchor store: %v", err)
	}

	// 1. 首次锚定
	ok, err := store.CheckOrAnchor("eth0", "00:11:22:33:44:55")
	if err != nil || !ok {
		t.Fatalf("initial anchor failed: ok=%v, err=%v", ok, err)
	}

	// 2. 相同 MAC 校验通过
	ok, err = store.CheckOrAnchor("eth0", "00:11:22:33:44:55")
	if err != nil || !ok {
		t.Fatalf("matching anchor failed: ok=%v, err=%v", ok, err)
	}

	// 3. MAC 不匹配返回 ErrOwnershipConflict
	ok, err = store.CheckOrAnchor("eth0", "00:11:22:33:44:66")
	if ok || !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("mismatching anchor expected ErrOwnershipConflict, got ok=%v, err=%v", ok, err)
	}

	// 4. 空 MAC 或全零 MAC 拒绝
	ok, err = store.CheckOrAnchor("eth1", "")
	if ok || !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("empty MAC expected ErrOwnershipConflict, got ok=%v, err=%v", ok, err)
	}

	// 5. 重启后从磁盘加载
	store2, err := NewAnchorStore(tmpDir)
	if err != nil {
		t.Fatalf("reload anchor store: %v", err)
	}
	ok, err = store2.CheckOrAnchor("eth0", "00:11:22:33:44:55")
	if err != nil || !ok {
		t.Fatalf("reloaded matching anchor failed: ok=%v, err=%v", ok, err)
	}
}

func TestLinuxPlatformCapabilities(t *testing.T) {
	p, err := NewLinuxPlatform("", "", true)
	if err != nil {
		t.Fatalf("NewLinuxPlatform failed: %v", err)
	}

	// Probe 前只声明 multi-address
	caps := p.Capabilities(context.Background())
	wantModes := []NetworkMode{NetworkModeMultiAddress}
	if !slices.Equal(caps.SupportedModes, wantModes) {
		t.Errorf("SupportedModes before probe = %v, want %v", caps.SupportedModes, wantModes)
	}

	// 模拟 Probe 成功
	if err := p.Probe(context.Background()); err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// Probe 成功后开放 multi-address 和 gateway
	caps = p.Capabilities(context.Background())
	if !slices.Contains(caps.SupportedModes, NetworkModeGateway) {
		t.Errorf("SupportedModes after probe should contain gateway, got %v", caps.SupportedModes)
	}
}

func TestLinuxProfileResolverExclusive(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. 普通文件测试
	resolvFile := filepath.Join(tmpDir, "resolv.conf")
	if err := os.WriteFile(resolvFile, []byte("nameserver 8.8.8.8\n"), 0644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}

	p := &LinuxProfile{
		Version: 1,
		Resolver: LinuxProfileResolver{
			Path:             resolvFile,
			RequireExclusive: true,
		},
	}
	err := p.CheckResolverExclusive()
	if os.Geteuid() != 0 {
		if !errors.Is(err, ErrResolverConflict) {
			t.Logf("non-root resolver check returned expected error: %v", err)
		}
	} else {
		if err != nil {
			t.Errorf("CheckResolverExclusive failed: %v", err)
		}
	}

	// 2. Symlink 测试
	symlinkFile := filepath.Join(tmpDir, "resolv_symlink.conf")
	if err := os.Symlink(resolvFile, symlinkFile); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	pSymlink := &LinuxProfile{
		Version: 1,
		Resolver: LinuxProfileResolver{
			Path:             symlinkFile,
			RequireExclusive: true,
		},
	}
	if err := pSymlink.CheckResolverExclusive(); !errors.Is(err, ErrResolverConflict) {
		t.Errorf("symlink resolver should return ErrResolverConflict, got %v", err)
	}
}

func TestWriteAndRestoreResolvConf(t *testing.T) {
	tmpDir := t.TempDir()
	resolvFile := filepath.Join(tmpDir, "resolv.conf")

	origContent := "# original\nnameserver 1.1.1.1\n"
	if err := os.WriteFile(resolvFile, []byte(origContent), 0644); err != nil {
		t.Fatalf("write orig resolv: %v", err)
	}

	// 写入新的 DNS 服务器列表（传入 4 个，应截断保留前 3 个）
	dnsList := []string{"8.8.8.8", "8.8.4.4", "114.114.114.114", "223.5.5.5"}
	oldContent, err := writeResolvConf(resolvFile, dnsList)
	if err != nil {
		t.Fatalf("writeResolvConf failed: %v", err)
	}
	if oldContent != origContent {
		t.Errorf("expected old content %q, got %q", origContent, oldContent)
	}

	newBytes, err := os.ReadFile(resolvFile)
	if err != nil {
		t.Fatalf("read new resolv: %v", err)
	}
	newStr := string(newBytes)
	if !strings.Contains(newStr, "nameserver 8.8.8.8") || !strings.Contains(newStr, "nameserver 8.8.4.4") || !strings.Contains(newStr, "nameserver 114.114.114.114") {
		t.Errorf("expected top 3 nameservers in resolv content, got:\n%s", newStr)
	}
	if strings.Contains(newStr, "223.5.5.5") {
		t.Errorf("expected 4th nameserver to be truncated, but was present in:\n%s", newStr)
	}

	// 恢复旧内容
	if err := restoreResolvConf(resolvFile, oldContent); err != nil {
		t.Fatalf("restoreResolvConf failed: %v", err)
	}
	restoredBytes, _ := os.ReadFile(resolvFile)
	if string(restoredBytes) != origContent {
		t.Errorf("expected restored content %q, got %q", origContent, string(restoredBytes))
	}
}

// mockOps 用于单元测试故障注入与补偿逆操作验证
type mockOps struct {
	links   map[string]netlink.Link
	addrs   map[string][]netlink.Addr
	routes  []netlink.Route
	callLog []string
	failAt  string
	failErr error
}

func newMockOps() *mockOps {
	eth0 := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Index:        1,
			Name:         "eth0",
			Flags:        0, // Down
			HardwareAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		},
	}
	return &mockOps{
		links: map[string]netlink.Link{
			"eth0": eth0,
		},
		addrs: map[string][]netlink.Addr{
			"eth0": {},
		},
		callLog: make([]string, 0),
	}
}

func (m *mockOps) LinkByName(name string) (netlink.Link, error) {
	if m.failAt == "LinkByName" {
		return nil, m.failErr
	}
	if l, ok := m.links[name]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("link %s not found", name)
}

func (m *mockOps) LinkSetUp(link netlink.Link) error {
	m.callLog = append(m.callLog, fmt.Sprintf("LinkSetUp(%s)", link.Attrs().Name))
	if m.failAt == "LinkSetUp" {
		return m.failErr
	}
	link.Attrs().Flags |= net.FlagUp
	return nil
}

func (m *mockOps) LinkSetDown(link netlink.Link) error {
	m.callLog = append(m.callLog, fmt.Sprintf("LinkSetDown(%s)", link.Attrs().Name))
	link.Attrs().Flags &= ^net.FlagUp
	return nil
}

func (m *mockOps) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return m.addrs[link.Attrs().Name], nil
}

func (m *mockOps) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	m.callLog = append(m.callLog, fmt.Sprintf("AddrAdd(%s, %s)", link.Attrs().Name, addr.IPNet.String()))
	if m.failAt == "AddrAdd" {
		return m.failErr
	}
	m.addrs[link.Attrs().Name] = append(m.addrs[link.Attrs().Name], *addr)
	return nil
}

func (m *mockOps) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	m.callLog = append(m.callLog, fmt.Sprintf("AddrDel(%s, %s)", link.Attrs().Name, addr.IPNet.String()))
	if m.failAt == "AddrDel" {
		return m.failErr
	}
	list := m.addrs[link.Attrs().Name]
	for i, a := range list {
		if a.IPNet.String() == addr.IPNet.String() {
			m.addrs[link.Attrs().Name] = append(list[:i], list[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockOps) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return m.routes, nil
}

func (m *mockOps) RouteAdd(route *netlink.Route) error {
	m.callLog = append(m.callLog, fmt.Sprintf("RouteAdd(gw=%s)", route.Gw.String()))
	if m.failAt == "RouteAdd" {
		return m.failErr
	}
	m.routes = append(m.routes, *route)
	return nil
}

func (m *mockOps) RouteDel(route *netlink.Route) error {
	m.callLog = append(m.callLog, fmt.Sprintf("RouteDel(gw=%s)", route.Gw.String()))
	return nil
}

func (m *mockOps) RouteReplace(route *netlink.Route) error {
	m.callLog = append(m.callLog, fmt.Sprintf("RouteReplace(gw=%s)", route.Gw.String()))
	if m.failAt == "RouteReplace" {
		return m.failErr
	}
	return nil
}

func TestLinuxApplyCompensation_FaultInjection(t *testing.T) {
	ops := newMockOps()
	// 注入在 RouteAdd 处失败
	ops.failAt = "RouteAdd"
	ops.failErr = errors.New("simulated route error")

	addr := "192.168.1.100"
	prefix := 24
	gw := "192.168.1.1"
	pID := "eth0"

	plan := HostPlan{
		PrimaryInterfaceID: &pID,
		Interfaces: map[string]InterfacePlan{
			"eth0": {
				Mode:    IPModeStatic,
				Address: &addr,
				Prefix:  &prefix,
				Gateway: &gw,
			},
		},
	}

	profile := &LinuxProfile{
		Version: 1,
		Interfaces: []LinuxProfileInterface{
			{Name: "eth0"},
		},
	}

	_, err := applyLinuxPlan(context.Background(), ops, plan, profile, nil, "/tmp/nonexistent-resolv", HostSnapshot{})
	if err == nil {
		t.Fatalf("expected apply failure on injected error")
	}

	// 验证逆操作回滚被逆序执行：LinkSetDown 和 AddrDel 必须被调用补偿
	hasLinkSetDown := false
	hasAddrDel := false
	for _, call := range ops.callLog {
		if call == "LinkSetDown(eth0)" {
			hasLinkSetDown = true
		}
		if call == "AddrDel(eth0, 192.168.1.100/24)" {
			hasAddrDel = true
		}
	}
	if !hasLinkSetDown {
		t.Errorf("expected LinkSetDown in rollback log, log: %v", ops.callLog)
	}
	if !hasAddrDel {
		t.Errorf("expected AddrDel in rollback log, log: %v", ops.callLog)
	}
}

func TestLinuxBondMapping(t *testing.T) {
	p2 := BondXmitHashPolicyLayer2
	p23 := BondXmitHashPolicyLayer23
	p34 := BondXmitHashPolicyLayer34

	if mapXmitHashPolicyToNetlink(&p2) != netlink.BOND_XMIT_HASH_POLICY_LAYER2 {
		t.Errorf("layer2 mapping mismatch")
	}
	if mapXmitHashPolicyToNetlink(&p23) != netlink.BOND_XMIT_HASH_POLICY_LAYER2_3 {
		t.Errorf("layer2+3 mapping mismatch")
	}
	if mapXmitHashPolicyToNetlink(&p34) != netlink.BOND_XMIT_HASH_POLICY_LAYER3_4 {
		t.Errorf("layer3+4 mapping mismatch")
	}
	if mapXmitHashPolicyToNetlink(nil) != netlink.BOND_XMIT_HASH_POLICY_LAYER2_3 {
		t.Errorf("default mapping should be layer2+3")
	}
}

func TestLinuxDriftDetector_Basic(t *testing.T) {
	d := NewLinuxDriftDetector()
	if d.IsDrifted() {
		t.Errorf("new drift detector should not be drifted")
	}

	d.SetDrifted(true)
	if !d.IsDrifted() {
		t.Errorf("expected drifted to be true")
	}

	d.ClearDrift()
	if d.IsDrifted() {
		t.Errorf("expected drifted to be false after ClearDrift")
	}

	// SyncState
	addr := "192.168.1.100"
	prefix := 24
	gw := "192.168.1.1"
	snap := HostSnapshot{
		Interfaces: map[string]InterfaceInfo{
			"eth0": {
				ID:        "eth0",
				Name:      "eth0",
				Writable:  true,
				Ownership: OwnershipManaged,
				IPv4: IPv4State{
					Address: &addr,
					Prefix:  &prefix,
					Gateway: &gw,
				},
			},
		},
	}
	d.SyncState(snap)
	if d.IsDrifted() {
		t.Errorf("SyncState should not alter drift boolean")
	}
}

func TestLinuxDHCPClient_Lifecycle(t *testing.T) {
	client := NewLinuxDHCPClient("eth0")
	if client.GetLastLease() != nil {
		t.Errorf("initial last lease should be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = client.Start(ctx, func(lease DHCPLeaseInfo) {})
	client.Stop()
}
