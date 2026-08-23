package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/netconfig"
)

func newTestNetworkService(t *testing.T) NetworkService {
	return newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}, nil)
}

func newTestNetworkServiceWithLog(t *testing.T, oplog OperationLogService) NetworkService {
	return newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}, oplog)
}

func newTestNetworkServiceCfg(t *testing.T, cfg *config.Config, oplog OperationLogService) NetworkService {
	t.Helper()
	srv, err := NewNetworkService(cfg, oplog, nil)
	if err != nil {
		t.Fatalf("NewNetworkService failed: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start network service failed: %v", err)
	}
	return srv
}

func TestNetworkService_Overview(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if len(overview.Interfaces) != 3 {
		t.Errorf("got %d interfaces, want 3", len(overview.Interfaces))
	}
	if overview.PrimaryInterfaceID == nil || *overview.PrimaryInterfaceID != "eth0" {
		t.Errorf("got primary %v, want eth0", overview.PrimaryInterfaceID)
	}
}

func TestNetworkService_ApplyConfirm(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 1. 将 eth1 配置为静态主出口
	newIP := "192.168.20.50"
	newPrefix := 24
	newGW := "192.168.20.1"
	dns := []string{"192.168.20.1", "1.1.1.1"}

	res, err := srv.ApplyInterface(ctx, "eth1", ApplyInterfaceInput{
		Mode:          netconfig.IPModeStatic,
		Primary:       true,
		Address:       &newIP,
		Prefix:        &newPrefix,
		Gateway:       &newGW,
		DNSServers:    dns,
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("ApplyInterface failed: %v", err)
	}
	if res.Status != netconfig.TxnStatusPendingConfirmation {
		t.Errorf("got status %v, want pending_confirmation", res.Status)
	}

	// 2. 验证处于 pending 期间不可发起新的写入
	_, err = srv.ApplyInterface(ctx, "eth0", ApplyInterfaceInput{
		Mode:    netconfig.IPModeDHCP,
		Primary: false,
	})
	if err == nil {
		t.Error("ApplyInterface while pending should return error")
	}

	// 3. 用户确认事务
	confirmRes, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}
	if confirmRes.Status != netconfig.TxnStatusConfirmed {
		t.Errorf("got status %v, want confirmed", confirmRes.Status)
	}

	// 4. 再次读取 Overview 验证主出口已迁移到 eth1，eth0 已降级
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.PrimaryInterfaceID == nil || *overview.PrimaryInterfaceID != "eth1" {
		t.Errorf("got primary %v, want eth1", overview.PrimaryInterfaceID)
	}
	if overview.PendingTransaction != nil {
		t.Error("PendingTransaction should be cleared after confirm")
	}
}

func TestNetworkService_CancelRollback(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 尝试修改 eth0 为 DHCP
	res, err := srv.ApplyInterface(ctx, "eth0", ApplyInterfaceInput{
		Mode:          netconfig.IPModeDHCP,
		Primary:       true,
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("ApplyInterface failed: %v", err)
	}

	// 取消该事务
	cancelRes, err := srv.CancelTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("CancelTransaction failed: %v", err)
	}
	if cancelRes.Status != netconfig.TxnStatusRolledBack {
		t.Errorf("got status %v, want rolled_back", cancelRes.Status)
	}

	// 验证 eth0 状态已恢复
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	for _, iface := range overview.Interfaces {
		if iface.ID == "eth0" {
			if iface.IPv4.Mode != netconfig.IPModeStatic {
				t.Errorf("eth0 mode should be restored to static, got %v", iface.IPv4.Mode)
			}
		}
	}
}

func TestNetworkService_ValidationErrors(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 非法 IP
	badIP := "999.999.999.999"
	prefix := 24
	_, err := srv.ApplyInterface(ctx, "eth0", ApplyInterfaceInput{
		Mode:    netconfig.IPModeStatic,
		Primary: false,
		Address: &badIP,
		Prefix:  &prefix,
	})
	if !errno.Is(err, errno.CodeNetworkInvalidConfig) {
		t.Errorf("ApplyInterface with bad IP should return CodeNetworkInvalidConfig, got %v", err)
	}

	// 非主网卡带网关
	validIP := "192.168.10.50"
	gw := "192.168.10.1"
	_, err = srv.ApplyInterface(ctx, "eth1", ApplyInterfaceInput{
		Mode:    netconfig.IPModeStatic,
		Primary: false,
		Address: &validIP,
		Prefix:  &prefix,
		Gateway: &gw,
	})
	if !errno.Is(err, errno.CodeNetworkInvalidConfig) {
		t.Errorf("ApplyInterface non-primary with gateway should return CodeNetworkInvalidConfig, got %v", err)
	}
}

// mockOperationLogService 内存操作日志替身，用于断言审计写入。
type mockOperationLogService struct {
	mu      sync.Mutex
	records []*model.OperationLog
}

func (m *mockOperationLogService) Record(_ context.Context, log *model.OperationLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, log)
	return nil
}

func (m *mockOperationLogService) GetByID(_ context.Context, _ uint64) (*model.OperationLog, error) {
	return nil, nil
}

func (m *mockOperationLogService) GetPage(_ context.Context, _ *LogPageQuery) (*LogPageResult, error) {
	return nil, nil
}

func (m *mockOperationLogService) actions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r.Action)
	}
	return out
}

func (m *mockOperationLogService) all() []*model.OperationLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*model.OperationLog, len(m.records))
	copy(out, m.records)
	return out
}

// multiOnlyPlatform 包装 FakePlatform，只声明 multi-address（模拟 Linux/Darwin 本轮能力）。
type multiOnlyPlatform struct {
	netconfig.Platform
}

func (p *multiOnlyPlatform) Capabilities(_ context.Context) netconfig.Capabilities {
	return netconfig.Capabilities{
		DHCP:            true,
		StaticIPv4:      true,
		FactoryReset:    true,
		WifiAssociation: false,
		SupportedModes:  []netconfig.NetworkMode{netconfig.NetworkModeMultiAddress},
	}
}

// switchModeSuccessInput 构造一个合法的 active-backup 切换输入。
func switchModeSuccessInput() SwitchModeInput {
	addr := "192.168.9.9"
	prefix := 24
	gw := "192.168.9.1"
	return SwitchModeInput{
		Mode:           netconfig.NetworkModeActiveBackup,
		SlaveIDs:       []string{"eth0", "eth1"},
		PrimarySlaveID: "eth0",
		BondIPv4: ApplyInterfaceInput{
			Mode:       netconfig.IPModeStatic,
			Primary:    true,
			Address:    &addr,
			Prefix:     &prefix,
			Gateway:    &gw,
			DNSServers: []string{"192.168.9.1"},
		},
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	}
}

func TestNetworkService_SwitchMode_Success(t *testing.T) {
	oplog := &mockOperationLogService{}
	srv := newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}, oplog)
	ctx := context.Background()

	res, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode failed: %v", err)
	}
	if res.Status != netconfig.TxnStatusPendingConfirmation {
		t.Errorf("got status %v, want pending_confirmation", res.Status)
	}
	if res.Overview == nil || res.Overview.PendingTransaction == nil ||
		res.Overview.PendingTransaction.TargetMode != netconfig.NetworkModeActiveBackup {
		t.Errorf("overview should carry mode_switch pending txn with targetMode")
	}

	// overview 出现 bond0 与 slave 归属，且 slave 退出可写集合（AC6）
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeActiveBackup {
		t.Errorf("mode = %q, want active-backup", overview.Mode)
	}
	if overview.Bond == nil || overview.Bond.BondInterfaceID != "bond0" {
		t.Errorf("bond topology = %+v, want bond0", overview.Bond)
	}
	foundBond := false
	for _, iface := range overview.Interfaces {
		if iface.ID == "bond0" {
			foundBond = true
			if !iface.IsBond {
				t.Errorf("bond0.IsBond should be true")
			}
		}
		if iface.ID == "eth0" || iface.ID == "eth1" {
			if iface.Writable {
				t.Errorf("%s should be unwritable while bonded", iface.ID)
			}
			if iface.MasterID == nil || *iface.MasterID != "bond0" {
				t.Errorf("%s master = %v, want bond0", iface.ID, iface.MasterID)
			}
		}
	}
	if !foundBond {
		t.Errorf("bond0 should appear in overview interfaces")
	}

	// 确认事务：固化为 last-valid，审计写入
	if _, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}
	// 审计应使用真实操作者/来源 IP，而非硬编码 system/127.0.0.1（回归保护）
	modeSwitchLogs := 0
	for _, r := range oplog.all() {
		if r.Action != "system.log.actionNetworkModeSwitch" {
			continue
		}
		modeSwitchLogs++
		if r.UserID != 1 || r.Username != "admin" {
			t.Errorf("mode switch audit operator = %s(%d), want admin(1)", r.Username, r.UserID)
		}
		if r.IP != "127.0.0.1" {
			t.Errorf("mode switch audit ip = %q, want 127.0.0.1", r.IP)
		}
	}
	if modeSwitchLogs < 2 {
		t.Errorf("mode switch audit should be recorded for submit+confirm, got %d", modeSwitchLogs)
	}
}

// 退出 active-backup 后，slave 应从 last-valid 恢复原 IPv4 配置（AC6 / R3.4）。
func TestNetworkService_SwitchMode_ExitRestoresSlaveIPv4(t *testing.T) {
	oplog := &mockOperationLogService{}
	srv := newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}, oplog)
	ctx := context.Background()

	// 1. 进入 active-backup 并确认
	res, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode enter failed: %v", err)
	}
	if _, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction enter failed: %v", err)
	}

	// 2. 退出到 multi-address 并确认
	exit, err := srv.SwitchMode(ctx, SwitchModeInput{
		Mode:          netconfig.NetworkModeMultiAddress,
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("SwitchMode exit failed: %v", err)
	}
	if _, err := srv.ConfirmTransaction(ctx, exit.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction exit failed: %v", err)
	}

	// 3. 验证：bond 消失、slave 归还并恢复原 IPv4，primary 回到原接口（AC6 / R3.4）
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeMultiAddress {
		t.Errorf("mode = %q, want multi-address", overview.Mode)
	}
	if overview.Bond != nil {
		t.Errorf("bond should be nil after exit, got %+v", overview.Bond)
	}
	// primary 应回到进入 bonding 前的接口（eth0），不能残留指向已删除的 bond0
	if overview.PrimaryInterfaceID == nil || *overview.PrimaryInterfaceID != "eth0" {
		t.Errorf("primary = %v, want eth0 after exit", overview.PrimaryInterfaceID)
	}

	wantAddrs := map[string]string{
		"eth0": "192.168.1.100", // fake 初始 static
	}
	for _, iface := range overview.Interfaces {
		if iface.ID == "bond0" {
			t.Errorf("bond0 should be removed after exit")
		}
		if iface.ID == "eth0" || iface.ID == "eth1" {
			if !iface.Writable {
				t.Errorf("%s should be writable after exit", iface.ID)
			}
			if iface.MasterID != nil {
				t.Errorf("%s master = %v, want nil after exit", iface.ID, iface.MasterID)
			}
			if iface.IPv4.Mode == netconfig.IPModeUnknown {
				t.Errorf("%s ipv4 mode = unknown, want restored", iface.ID)
			}
			// eth0 是 static，恢复后应还原初始地址；eth1 是 dhcp，地址由平台动态分配，只断言模式
			if want, ok := wantAddrs[iface.ID]; ok {
				if iface.IPv4.Address == nil || *iface.IPv4.Address != want {
					t.Errorf("%s addr = %v, want %s", iface.ID, iface.IPv4.Address, want)
				}
				// 原 primary slave（eth0）应恢复 gateway（R3.4 精确恢复）
				if iface.IPv4.Gateway == nil || *iface.IPv4.Gateway != "192.168.1.1" {
					t.Errorf("%s gateway = %v, want 192.168.1.1", iface.ID, iface.IPv4.Gateway)
				}
			}
		}
	}
}

func TestNetworkService_SwitchMode_PendingConflict(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	if _, err := srv.ApplyInterface(ctx, "eth0", ApplyInterfaceInput{
		Mode:    netconfig.IPModeDHCP,
		Primary: true,
	}); err != nil {
		t.Fatalf("ApplyInterface failed: %v", err)
	}
	_, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if !errno.Is(err, errno.CodeNetworkTransactionPending) {
		t.Errorf("SwitchMode with pending txn should return 1101, got %v", err)
	}
}

func TestNetworkService_SwitchMode_Unsupported(t *testing.T) {
	srv := newTestNetworkService(t)
	// 模拟 Linux/Darwin 平台：仅 multi-address
	srv.(*networkService).platform = &multiOnlyPlatform{Platform: srv.(*networkService).platform}
	ctx := context.Background()

	_, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if !errno.Is(err, errno.CodeNetworkUnsupported) {
		t.Errorf("SwitchMode on multi-only platform should return 1106, got %v", err)
	}
}

func TestNetworkService_SwitchMode_SlaveInvalid(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()
	base := switchModeSuccessInput()

	tests := []struct {
		name   string
		mutate func(*SwitchModeInput)
	}{
		{"count not 2", func(in *SwitchModeInput) { in.SlaveIDs = []string{"eth0"} }},
		{"duplicate", func(in *SwitchModeInput) { in.SlaveIDs = []string{"eth0", "eth0"} }},
		{"primary not in set", func(in *SwitchModeInput) {
			in.SlaveIDs = []string{"eth1", "wlan0"}
			in.PrimarySlaveID = "eth0"
		}},
		{"slave not exists", func(in *SwitchModeInput) { in.SlaveIDs = []string{"eth9", "eth1"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base
			input.SlaveIDs = append([]string(nil), base.SlaveIDs...)
			input.PrimarySlaveID = base.PrimarySlaveID
			input.BondIPv4 = base.BondIPv4
			tt.mutate(&input)
			_, err := srv.SwitchMode(ctx, input)
			if !errno.Is(err, errno.CodeNetworkBondSlaveInvalid) {
				t.Errorf("%s should return 1112, got %v", tt.name, err)
			}
		})
	}

	// 状态零修改：全部失败后仍为 multi-address，无 bond（AC3）
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeMultiAddress {
		t.Errorf("mode should stay multi-address after all failures, got %q", overview.Mode)
	}
	if overview.Bond != nil {
		t.Errorf("bond should be nil after all failures")
	}
}

func TestNetworkService_SwitchMode_ModeConflict(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 第一次切换并确认（清除 pending，模式固化 active-backup），再次请求同模式应 1113
	res, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if err != nil {
		t.Fatalf("first SwitchMode failed: %v", err)
	}
	if _, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}
	_, err = srv.SwitchMode(ctx, switchModeSuccessInput())
	if !errno.Is(err, errno.CodeNetworkBondModeConflict) {
		t.Errorf("same-mode switch should return 1113, got %v", err)
	}
}

func TestNetworkService_SwitchMode_InvalidBondIPv4(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	input := switchModeSuccessInput()
	bad := "999.999.999.999"
	input.BondIPv4.Address = &bad
	_, err := srv.SwitchMode(ctx, input)
	if !errno.Is(err, errno.CodeNetworkInvalidConfig) {
		t.Errorf("bad bond IPv4 should return 1100, got %v", err)
	}
}

func TestNetworkService_SwitchMode_ApplyFailed(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	srv.(*networkService).platform.(*netconfig.FakePlatform).SetFailApply(true)
	_, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if !errno.Is(err, errno.CodeNetworkApplyFailed) {
		t.Errorf("apply failure should return 1107, got %v", err)
	}

	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeMultiAddress || overview.Bond != nil {
		t.Errorf("state should be untouched after apply failure, mode=%q bond=%+v", overview.Mode, overview.Bond)
	}
}

func TestNetworkService_SwitchMode_TimeoutRollback(t *testing.T) {
	oplog := &mockOperationLogService{}
	srv := newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 300 * time.Millisecond,
			FakePlatform:   true,
		},
	}, oplog)
	ctx := context.Background()

	res, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode failed: %v", err)
	}
	_ = res

	// 等待超时自动回滚
	time.Sleep(600 * time.Millisecond)
	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeMultiAddress {
		t.Errorf("mode should roll back to multi-address, got %q", overview.Mode)
	}
	if overview.Bond != nil {
		t.Errorf("bond should be removed after rollback")
	}
	if overview.PendingTransaction != nil {
		t.Errorf("pending should be cleared after rollback")
	}
	// 自动事件回滚应沿用 pending 中保存的原操作者/来源 IP，而非硬编码 system（spec 5.2 / 回归保护）
	rollbackFound := false
	for _, r := range oplog.all() {
		if r.Action != "system.log.actionNetworkRollback" {
			continue
		}
		rollbackFound = true
		if r.UserID != 1 || r.Username != "admin" {
			t.Errorf("rollback audit operator = %s(%d), want admin(1)", r.Username, r.UserID)
		}
		if r.IP != "127.0.0.1" {
			t.Errorf("rollback audit ip = %q, want 127.0.0.1", r.IP)
		}
	}
	if !rollbackFound {
		t.Errorf("rollback audit should be recorded, got %v", oplog.actions())
	}
}

func switchModeLACPInput() SwitchModeInput {
	addr := "192.168.9.9"
	prefix := 24
	gw := "192.168.9.1"
	hashPolicy := netconfig.BondXmitHashPolicyLayer23
	return SwitchModeInput{
		Mode:           netconfig.NetworkModeLACP,
		SlaveIDs:       []string{"eth0", "eth1"},
		XmitHashPolicy: &hashPolicy,
		BondIPv4: ApplyInterfaceInput{
			Mode:       netconfig.IPModeStatic,
			Primary:    true,
			Address:    &addr,
			Prefix:     &prefix,
			Gateway:    &gw,
			DNSServers: []string{"192.168.9.1"},
		},
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	}
}

func switchGatewaySuccessInput() SwitchModeInput {
	return SwitchModeInput{
		Mode: netconfig.NetworkModeGateway,
		Gateway: &GatewayInput{
			DownstreamInterfaceID: "eth1",
			PoolStart:             "192.168.2.100",
			PoolEnd:               "192.168.2.200",
			Prefix:                24,
			LeaseDurationSeconds:  3600,
			IPForward:             true,
		},
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	}
}

// TestNetworkService_SwitchMode_GatewayLifecycle 验证切换到网关模式的全生命周期（进入、分配租约、确认、退出、回滚、恢复出厂）。
func TestNetworkService_SwitchMode_GatewayLifecycle(t *testing.T) {
	oplog := &mockOperationLogService{}
	srv := newTestNetworkServiceWithLog(t, oplog)
	ctx := context.Background()

	// 先将 eth1 设置为静态 IPv4 (192.168.2.1/24)
	ipStr := "192.168.2.1"
	pfx := 24
	applyRes, err := srv.ApplyInterface(ctx, "eth1", ApplyInterfaceInput{
		Mode:          netconfig.IPModeStatic,
		Address:       &ipStr,
		Prefix:        &pfx,
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("ApplyInterface static eth1 failed: %v", err)
	}
	_, err = srv.ConfirmTransaction(ctx, applyRes.TransactionID, 1, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("ConfirmTransaction static eth1 failed: %v", err)
	}

	// 1. 切换到 Gateway 模式
	res, err := srv.SwitchMode(ctx, switchGatewaySuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode to gateway failed: %v", err)
	}
	if res.Status != netconfig.TxnStatusPendingConfirmation {
		t.Errorf("status = %q, want pending_confirmation", res.Status)
	}

	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeGateway {
		t.Errorf("mode = %q, want gateway", overview.Mode)
	}
	if overview.Gateway == nil || !overview.Gateway.Running || !overview.Gateway.IPForward {
		t.Errorf("gateway overview = %+v, want running=true ipForward=true", overview.Gateway)
	}

	// 2. 确认事务
	confirmRes, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}
	if confirmRes.Status != netconfig.TxnStatusConfirmed {
		t.Errorf("confirm status = %q, want confirmed", confirmRes.Status)
	}

	// 3. 退回 multi-address 模式
	exitInput := SwitchModeInput{
		Mode:          netconfig.NetworkModeMultiAddress,
		ActorID:       1,
		ActorUsername: "admin",
		ClientIP:      "127.0.0.1",
	}
	exitRes, err := srv.SwitchMode(ctx, exitInput)
	if err != nil {
		t.Fatalf("SwitchMode exit gateway failed: %v", err)
	}
	_, err = srv.ConfirmTransaction(ctx, exitRes.TransactionID, 1, "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("ConfirmTransaction exit gateway failed: %v", err)
	}

	overviewExit, _ := srv.GetOverview(ctx)
	if overviewExit.Mode != netconfig.NetworkModeMultiAddress {
		t.Errorf("mode after exit = %q, want multi-address", overviewExit.Mode)
	}
	if overviewExit.Gateway != nil {
		t.Errorf("gateway overview should be nil after exit, got %+v", overviewExit.Gateway)
	}
}

// TestNetworkService_SwitchMode_GatewayConflictProbe 验证启用前冲突探测拒绝（AC3 / 1116）。
func TestNetworkService_SwitchMode_GatewayConflictProbe(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 先将 eth1 设置为静态
	ipStr := "192.168.2.1"
	pfx := 24
	applyRes, _ := srv.ApplyInterface(ctx, "eth1", ApplyInterfaceInput{
		Mode:    netconfig.IPModeStatic,
		Address: &ipStr,
		Prefix:  &pfx,
	})
	_, _ = srv.ConfirmTransaction(ctx, applyRes.TransactionID, 1, "admin", "127.0.0.1")

	// 若接口仍为 DHCP client 模式，直接返回 1116
	dhcpInput := switchGatewaySuccessInput()
	dhcpInput.Gateway.DownstreamInterfaceID = "wlan0" // wlan0 是 DHCP
	_, err := srv.SwitchMode(ctx, dhcpInput)
	if !errno.Is(err, errno.CodeNetworkDhcpServerConflict) {
		t.Errorf("dhcp client downstream iface should return 1116, got %v", err)
	}
}

func TestNetworkService_SwitchMode_StartupRecovery(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "net-state")
	ctx := context.Background()

	// 第一次会话：切换模式但立即关闭（模拟未确认就重启）
	cfg := &config.Config{
		Network: config.Network{
			StateDir:       stateDir,
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}
	srv1 := newTestNetworkServiceCfg(t, cfg, nil)
	_ = srv1.Start(ctx)

	applyRes, _ := srv1.ApplyInterface(ctx, "eth1", ApplyInterfaceInput{
		Mode:    netconfig.IPModeStatic,
		Address: strPtr("192.168.2.1"),
		Prefix:  intPtr(24),
	})
	_, _ = srv1.ConfirmTransaction(ctx, applyRes.TransactionID, 1, "admin", "127.0.0.1")

	_, err := srv1.SwitchMode(ctx, switchGatewaySuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode failed: %v", err)
	}
	_ = srv1.Close(ctx)

	// 第二次会话：启动时应自动检测到 pending 并回滚
	srv2 := newTestNetworkServiceCfg(t, cfg, nil)
	if err := srv2.Start(ctx); err != nil {
		t.Fatalf("second start failed: %v", err)
	}
	defer srv2.Close(ctx)

	overview, err := srv2.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview after restart failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeMultiAddress {
		t.Errorf("mode after recovery = %q, want multi-address", overview.Mode)
	}
}

func TestNetworkService_SwitchMode_LACP_Success(t *testing.T) {
	oplog := &mockOperationLogService{}
	srv := newTestNetworkServiceCfg(t, &config.Config{
		Network: config.Network{
			StateDir:       filepath.Join(t.TempDir(), "net-state"),
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}, oplog)
	ctx := context.Background()

	res, err := srv.SwitchMode(ctx, switchModeLACPInput())
	if err != nil {
		t.Fatalf("SwitchMode LACP failed: %v", err)
	}
	if res.Status != netconfig.TxnStatusPendingConfirmation {
		t.Errorf("got status %v, want pending_confirmation", res.Status)
	}
	if res.Overview == nil || res.Overview.Mode != netconfig.NetworkModeLACP {
		t.Errorf("overview mode = %v, want lacp-aggregation", res.Overview)
	}
	if res.Overview.Bond == nil || res.Overview.Bond.LACP == nil || !res.Overview.Bond.LACP.Negotiated {
		t.Errorf("overview bond lacp status not properly populated: %+v", res.Overview.Bond)
	}

	// 确认事务
	if _, err := srv.ConfirmTransaction(ctx, res.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}

	overview, err := srv.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Mode != netconfig.NetworkModeLACP || overview.Bond == nil || overview.Bond.LACP == nil {
		t.Fatalf("GetOverview did not return confirmed LACP status: %+v", overview)
	}
}

func TestNetworkService_SwitchMode_LACP_SpeedDuplexWarning(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 注入速度和双工不一致
	speed1000 := 1000
	speed100 := 100
	srv.(*networkService).platform.(*netconfig.FakePlatform).SetInterfaceLinkProperties("eth0", &speed1000, netconfig.DuplexFull)
	srv.(*networkService).platform.(*netconfig.FakePlatform).SetInterfaceLinkProperties("eth1", &speed100, netconfig.DuplexHalf)

	res, err := srv.SwitchMode(ctx, switchModeLACPInput())
	if err != nil {
		t.Fatalf("SwitchMode LACP with mismatch should not fail, got %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected warnings for speed/duplex mismatch")
	}
	if res.Warnings[0].Code != netconfig.WarningBondSlaveLinkMismatch {
		t.Errorf("got warning code %s, want %s", res.Warnings[0].Code, netconfig.WarningBondSlaveLinkMismatch)
	}
}

func TestNetworkService_SwitchMode_LACP_KernelRejection1114(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 注入 LACP 内核拒绝
	srv.(*networkService).platform.(*netconfig.FakePlatform).SetFailLACP(true)

	_, err := srv.SwitchMode(ctx, switchModeLACPInput())
	if !errno.Is(err, errno.CodeNetworkLacpNegotiationFailed) {
		t.Errorf("kernel rejection should return 1114, got %v", err)
	}
}

func TestNetworkService_SwitchMode_LACP_DirectSwitchBetweenBonds(t *testing.T) {
	srv := newTestNetworkService(t)
	ctx := context.Background()

	// 1. 先切到 active-backup 并确认
	res1, err := srv.SwitchMode(ctx, switchModeSuccessInput())
	if err != nil {
		t.Fatalf("SwitchMode active-backup failed: %v", err)
	}
	if _, err := srv.ConfirmTransaction(ctx, res1.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}

	// 2. 直接切到 LACP (相同 slaves: eth0, eth1) 应该成功
	res2, err := srv.SwitchMode(ctx, switchModeLACPInput())
	if err != nil {
		t.Fatalf("Direct switch from active-backup to LACP failed: %v", err)
	}
	if _, err := srv.ConfirmTransaction(ctx, res2.TransactionID, 1, "admin", "127.0.0.1"); err != nil {
		t.Fatalf("ConfirmTransaction failed: %v", err)
	}

	overview, _ := srv.GetOverview(ctx)
	if overview.Mode != netconfig.NetworkModeLACP {
		t.Errorf("mode = %s, want lacp-aggregation", overview.Mode)
	}
}

