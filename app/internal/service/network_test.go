package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/netconfig"
)

func newTestNetworkService(t *testing.T) NetworkService {
	t.Helper()
	tmpDir := filepath.Join(t.TempDir(), "net-state")
	cfg := &config.Config{
		Network: config.Network{
			StateDir:       tmpDir,
			ProfilePath:    "/tmp/dummy-profile.json",
			ConfirmTimeout: 2 * time.Second,
			FakePlatform:   true,
		},
	}
	srv, err := NewNetworkService(cfg, nil, nil)
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
