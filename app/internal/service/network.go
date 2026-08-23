package service

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/netconfig"
)

// NetworkService 网络配置业务编排接口。
type NetworkService interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	GetOverview(ctx context.Context) (*netconfig.NetworkOverview, error)
	GetTransaction(ctx context.Context, txnID string) (*netconfig.PendingTransaction, error)
	ApplyInterface(ctx context.Context, ifaceID string, input ApplyInterfaceInput) (*netconfig.TransactionResult, error)
	ConfirmTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error)
	CancelTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error)
	FactoryReset(ctx context.Context, ifaceID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error)
}

// ApplyInterfaceInput 接口配置输入。
type ApplyInterfaceInput struct {
	Mode          netconfig.IPMode `json:"mode"`
	Primary       bool             `json:"primary"`
	Address       *string          `json:"address"`
	Prefix        *int             `json:"prefix"`
	Gateway       *string          `json:"gateway"`
	DNSServers    []string         `json:"dnsServers"`
	ActorID       uint64           `json:"-"`
	ActorUsername string           `json:"-"`
	ClientIP      string           `json:"-"`
}

type networkService struct {
	cfg          *config.Config
	platform     netconfig.Platform
	store        netconfig.StateStore
	oplogService OperationLogService
	log          *zap.Logger
	mu           sync.Mutex
	timer        *time.Timer
	ready        bool
}

// NewNetworkService 创建网络服务实例（纯依赖装配，不执行启动副作用）。
func NewNetworkService(
	cfg *config.Config,
	oplogService OperationLogService,
	log *zap.Logger,
) (NetworkService, error) {
	if log == nil {
		log = zap.NewNop()
	}

	platform, err := netconfig.NewPlatform(cfg.Network.ProfilePath, cfg.Network.FakePlatform)
	if err != nil {
		return nil, fmt.Errorf("create network platform: %w", err)
	}

	store := netconfig.NewFileStateStore(cfg.Network.StateDir, platform.Type())

	return &networkService{
		cfg:          cfg,
		platform:     platform,
		store:        store,
		oplogService: oplogService,
		log:          log,
	}, nil
}

func (s *networkService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 初始化状态存储
	if err := s.store.Init(s.platform.Type()); err != nil {
		return fmt.Errorf("init network store: %w", err)
	}

	// 2. 平台能力探测
	if err := s.platform.Probe(ctx); err != nil {
		s.log.Warn("network platform probe returned warning", zap.Error(err))
	}

	// 3. 读取当前平台实际快照
	currentSnapshot, err := s.platform.Read(ctx)
	if err != nil {
		s.log.Error("read current network snapshot failed", zap.Error(err))
	}

	// 4. 初始化首次接管不可变出厂基线
	if _, err := s.store.GetFactory(); err != nil {
		factoryPlan := netconfig.HostPlan{
			Interfaces:         make(map[string]netconfig.InterfacePlan),
			PrimaryInterfaceID: currentSnapshot.PrimaryInterfaceID,
		}
		for id, info := range currentSnapshot.Interfaces {
			factoryPlan.Interfaces[id] = netconfig.InterfacePlan{
				Mode:       info.IPv4.Mode,
				Primary:    info.IsPrimary,
				Address:    info.IPv4.Address,
				Prefix:     info.IPv4.Prefix,
				Gateway:    info.IPv4.Gateway,
				DNSServers: info.IPv4.DNSServers,
			}
		}
		_ = s.store.SetFactory(&netconfig.FactoryData{
			Plan:     factoryPlan,
			Snapshot: currentSnapshot,
		})
	}

	// 5. 检查是否存在未完成的候选事务，若存在则优先自动回滚
	if pending, err := s.store.GetPending(); err == nil && pending != nil {
		s.log.Info("found pending network transaction on startup, rolling back", zap.String("txnId", pending.Transaction.ID))
		if _, err := s.platform.Restore(ctx, pending.Before); err != nil {
			s.log.Error("startup rollback failed", zap.Error(err))
		}
		_ = s.store.ClearPending()
		s.recordSystemLog(ctx, "system.log.actionNetworkStartupRecovery", "Startup recovery rolled back dangling transaction")
	}

	s.ready = true
	s.log.Info("network service started successfully")
	return nil
}

func (s *networkService) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.ready = false
	return s.platform.Close(ctx)
}

func (s *networkService) GetOverview(ctx context.Context) (*netconfig.NetworkOverview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	snapshot, err := s.platform.Read(ctx)
	if err != nil {
		return nil, errno.New(errno.CodeNetworkUnsupported)
	}

	interfacesList, err := s.platform.Discover(ctx)
	if err != nil {
		interfacesList = make([]netconfig.InterfaceInfo, 0)
		for _, v := range snapshot.Interfaces {
			interfacesList = append(interfacesList, v)
		}
	}
	// 保持网卡列表输出顺序稳定（按名称升序）
	slices.SortFunc(interfacesList, func(a, b netconfig.InterfaceInfo) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	var pendingTxn *netconfig.PendingTransaction
	if pData, err := s.store.GetPending(); err == nil && pData != nil {
		remaining := int(time.Until(pData.Transaction.ExpiresAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		txnCopy := pData.Transaction
		txnCopy.RemainingSeconds = remaining
		pendingTxn = &txnCopy
	}

	overview := &netconfig.NetworkOverview{
		Platform:                s.platform.Type(),
		State:                   netconfig.StateReady,
		PrimaryInterfaceID:      snapshot.PrimaryInterfaceID,
		DefaultRouteInterfaceID: snapshot.DefaultRouteInterfaceID,
		SystemDNSServers:        snapshot.SystemDNSServers,
		Interfaces:              interfacesList,
		PendingTransaction:      pendingTxn,
		Capabilities: netconfig.Capabilities{
			DHCP:            true,
			StaticIPv4:      true,
			FactoryReset:    true,
			WifiAssociation: false,
		},
	}

	return overview, nil
}

func (s *networkService) GetTransaction(ctx context.Context, txnID string) (*netconfig.PendingTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	pData, err := s.store.GetPending()
	if err != nil || pData == nil || pData.Transaction.ID != txnID {
		return nil, errno.New(errno.CodeNetworkTransactionNotFound)
	}

	remaining := int(time.Until(pData.Transaction.ExpiresAt).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	txnCopy := pData.Transaction
	txnCopy.RemainingSeconds = remaining
	return &txnCopy, nil
}

func (s *networkService) ApplyInterface(ctx context.Context, ifaceID string, input ApplyInterfaceInput) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	// 1. 检查是否存在待确认事务
	if existing, err := s.store.GetPending(); err == nil && existing != nil {
		return nil, errno.New(errno.CodeNetworkTransactionPending)
	}

	// 2. 读取当前状态并校验接口存在且可写
	currentSnapshot, err := s.platform.Read(ctx)
	if err != nil {
		return nil, errno.New(errno.CodeNetworkUnsupported)
	}

	targetIface, exists := currentSnapshot.Interfaces[ifaceID]
	if !exists || !targetIface.Writable {
		return nil, errno.New(errno.CodeNetworkInterfaceNotManaged)
	}

	// 3. 校验并规范化参数
	var normAddr *string
	var normMask *string
	var normPrefix *int
	var normGW *string
	var normDNS []string

	if input.Mode == netconfig.IPModeStatic {
		if input.Address == nil || input.Prefix == nil {
			return nil, errno.New(errno.CodeNetworkInvalidConfig)
		}
		addr, mask, err := netconfig.NormalizeAndValidateIPv4(*input.Address, *input.Prefix)
		if err != nil {
			return nil, errno.New(errno.CodeNetworkInvalidConfig)
		}
		normAddr = &addr
		normMask = &mask
		normPrefix = input.Prefix

		if input.Primary {
			if input.Gateway == nil || len(input.DNSServers) == 0 {
				return nil, errno.New(errno.CodeNetworkInvalidConfig)
			}
			gw, err := netconfig.ValidateGatewayInSubnet(addr, *input.Prefix, *input.Gateway)
			if err != nil {
				return nil, errno.New(errno.CodeNetworkInvalidConfig)
			}
			normGW = &gw

			dns, err := netconfig.ValidateDNSServers(input.DNSServers)
			if err != nil {
				return nil, errno.New(errno.CodeNetworkInvalidConfig)
			}
			normDNS = dns
		} else {
			if input.Gateway != nil || len(input.DNSServers) > 0 {
				return nil, errno.New(errno.CodeNetworkInvalidConfig)
			}
		}
	} else if input.Mode == netconfig.IPModeDHCP {
		if input.Address != nil || input.Prefix != nil || input.Gateway != nil || len(input.DNSServers) > 0 {
			return nil, errno.New(errno.CodeNetworkInvalidConfig)
		}
	} else {
		return nil, errno.New(errno.CodeNetworkInvalidConfig)
	}

	// 4. 构建完整的 HostPlan
	plan := netconfig.HostPlan{
		Interfaces:         make(map[string]netconfig.InterfacePlan),
		PrimaryInterfaceID: currentSnapshot.PrimaryInterfaceID,
	}

	// 复制现有接口配置
	for id, info := range currentSnapshot.Interfaces {
		plan.Interfaces[id] = netconfig.InterfacePlan{
			Mode:       info.IPv4.Mode,
			Primary:    info.IsPrimary,
			Address:    info.IPv4.Address,
			Prefix:     info.IPv4.Prefix,
			Gateway:    info.IPv4.Gateway,
			DNSServers: info.IPv4.DNSServers,
		}
	}

	// 合并目标接口修改
	if input.Primary {
		newPrimary := ifaceID
		plan.PrimaryInterfaceID = &newPrimary
		// 将旧主出口降级
		for id, ifacePlan := range plan.Interfaces {
			if id != ifaceID && ifacePlan.Primary {
				ifacePlan.Primary = false
				ifacePlan.Gateway = nil
				ifacePlan.DNSServers = nil
				plan.Interfaces[id] = ifacePlan
			}
		}
	} else if plan.PrimaryInterfaceID != nil && *plan.PrimaryInterfaceID == ifaceID {
		plan.PrimaryInterfaceID = nil
	}

	plan.Interfaces[ifaceID] = netconfig.InterfacePlan{
		Mode:       input.Mode,
		Primary:    input.Primary,
		Address:    normAddr,
		Prefix:     normPrefix,
		Gateway:    normGW,
		DNSServers: normDNS,
	}

	// 5. 生成事务与过期时间
	now := time.Now().UTC()
	timeout := s.cfg.Network.ConfirmTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	expiresAt := now.Add(timeout)
	txnID := fmt.Sprintf("txn-%d", now.UnixNano())

	reconnectAddrs := make([]netconfig.ReconnectAddress, 0)
	if normAddr != nil && normPrefix != nil {
		reconnectAddrs = append(reconnectAddrs, netconfig.ReconnectAddress{
			InterfaceID: ifaceID,
			Address:     *normAddr,
			Prefix:      *normPrefix,
		})
	}

	pending := &netconfig.PendingData{
		Transaction: netconfig.PendingTransaction{
			ID:                          txnID,
			Status:                      netconfig.TxnStatusPendingConfirmation,
			Action:                      netconfig.TxnActionApply,
			CreatedAt:                   now,
			ExpiresAt:                   expiresAt,
			RemainingSeconds:            int(timeout.Seconds()),
			TargetInterfaceID:           ifaceID,
			PreviousPrimaryInterfaceID:  currentSnapshot.PrimaryInterfaceID,
			CandidatePrimaryInterfaceID: plan.PrimaryInterfaceID,
			ReconnectAddresses:          reconnectAddrs,
			RequiresReconnect:           true,
			Candidate: netconfig.CandidateSummary{
				Mode:       input.Mode,
				Address:    normAddr,
				Prefix:     normPrefix,
				SubnetMask: normMask,
				Gateway:    normGW,
				DNSServers: normDNS,
			},
			ActorID:       input.ActorID,
			ActorUsername: input.ActorUsername,
			ClientIP:      input.ClientIP,
		},
		Before:    currentSnapshot,
		Candidate: plan,
	}

	// 6. 持久化 pending
	if err := s.store.SetPending(pending); err != nil {
		return nil, errno.New(errno.CodeNetworkStateCorrupt)
	}

	// 7. 平台补偿性应用
	candidateSnapshot, err := s.platform.Apply(ctx, plan)
	if err != nil {
		s.log.Error("platform apply failed, restoring before snapshot", zap.Error(err))
		_, _ = s.platform.Restore(ctx, currentSnapshot)
		_ = s.store.ClearPending()
		return nil, errno.New(errno.CodeNetworkApplyFailed)
	}

	// 8. 启动超时自动回滚定时器
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(timeout, func() {
		s.handleTimeout(context.Background(), txnID)
	})

	overview := &netconfig.NetworkOverview{
		Platform:                s.platform.Type(),
		State:                   netconfig.StateReady,
		PrimaryInterfaceID:      candidateSnapshot.PrimaryInterfaceID,
		DefaultRouteInterfaceID: candidateSnapshot.DefaultRouteInterfaceID,
		SystemDNSServers:        candidateSnapshot.SystemDNSServers,
		PendingTransaction:      &pending.Transaction,
	}

	return &netconfig.TransactionResult{
		TransactionID:      txnID,
		Status:             netconfig.TxnStatusPendingConfirmation,
		ExpiresAt:          &expiresAt,
		Overview:           overview,
		ReconnectAddresses: reconnectAddrs,
	}, nil
}

func (s *networkService) ConfirmTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	pending, err := s.store.GetPending()
	if err != nil || pending == nil || pending.Transaction.ID != txnID {
		return nil, errno.New(errno.CodeNetworkTransactionNotFound)
	}

	if time.Now().UTC().After(pending.Transaction.ExpiresAt) {
		return nil, errno.New(errno.CodeNetworkTransactionExpired)
	}

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	// 固化为 last-valid
	currentSnapshot, _ := s.platform.Read(ctx)
	lastValid := &netconfig.LastValidData{
		Plan:     pending.Candidate,
		Snapshot: currentSnapshot,
	}
	_ = s.store.SetLastValid(lastValid)
	_ = s.store.ClearPending()

	return &netconfig.TransactionResult{
		TransactionID: txnID,
		Status:        netconfig.TxnStatusConfirmed,
	}, nil
}

func (s *networkService) CancelTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	pending, err := s.store.GetPending()
	if err != nil || pending == nil || pending.Transaction.ID != txnID {
		return nil, errno.New(errno.CodeNetworkTransactionNotFound)
	}

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	// 恢复 before
	if _, err := s.platform.Restore(ctx, pending.Before); err != nil {
		s.log.Error("platform restore failed on cancel", zap.Error(err))
		return nil, errno.New(errno.CodeNetworkRecoveryFailed)
	}

	_ = s.store.ClearPending()
	s.recordSystemLog(ctx, "system.log.actionNetworkRollback", fmt.Sprintf("Transaction %s cancelled by user", txnID))

	return &netconfig.TransactionResult{
		TransactionID: txnID,
		Status:        netconfig.TxnStatusRolledBack,
	}, nil
}

func (s *networkService) FactoryReset(ctx context.Context, ifaceID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	// 检查是否存在待确认事务
	if existing, err := s.store.GetPending(); err == nil && existing != nil {
		return nil, errno.New(errno.CodeNetworkTransactionPending)
	}

	factory, err := s.store.GetFactory()
	if err != nil || factory == nil {
		return nil, errno.New(errno.CodeNetworkRecoveryFailed)
	}

	currentSnapshot, err := s.platform.Read(ctx)
	if err != nil {
		return nil, errno.New(errno.CodeNetworkUnsupported)
	}

	now := time.Now().UTC()
	timeout := s.cfg.Network.ConfirmTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	expiresAt := now.Add(timeout)
	txnID := fmt.Sprintf("txn-%d", now.UnixNano())

	pending := &netconfig.PendingData{
		Transaction: netconfig.PendingTransaction{
			ID:                          txnID,
			Status:                      netconfig.TxnStatusPendingConfirmation,
			Action:                      netconfig.TxnActionFactoryReset,
			CreatedAt:                   now,
			ExpiresAt:                   expiresAt,
			RemainingSeconds:            int(timeout.Seconds()),
			TargetInterfaceID:           ifaceID,
			PreviousPrimaryInterfaceID:  currentSnapshot.PrimaryInterfaceID,
			CandidatePrimaryInterfaceID: factory.Plan.PrimaryInterfaceID,
			RequiresReconnect:           true,
			ActorID:                     actorID,
			ActorUsername:               actorUsername,
			ClientIP:                    clientIP,
		},
		Before:    currentSnapshot,
		Candidate: factory.Plan,
	}

	_ = s.store.SetPending(pending)
	if _, err := s.platform.Apply(ctx, factory.Plan); err != nil {
		_, _ = s.platform.Restore(ctx, currentSnapshot)
		_ = s.store.ClearPending()
		return nil, errno.New(errno.CodeNetworkApplyFailed)
	}

	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(timeout, func() {
		s.handleTimeout(context.Background(), txnID)
	})

	return &netconfig.TransactionResult{
		TransactionID: txnID,
		Status:        netconfig.TxnStatusPendingConfirmation,
		ExpiresAt:     &expiresAt,
	}, nil
}

func (s *networkService) handleTimeout(ctx context.Context, txnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending, err := s.store.GetPending()
	if err != nil || pending == nil || pending.Transaction.ID != txnID {
		return
	}

	s.log.Info("network transaction expired, auto rolling back", zap.String("txnId", txnID))
	_, _ = s.platform.Restore(ctx, pending.Before)
	_ = s.store.ClearPending()
	s.recordSystemLog(ctx, "system.log.actionNetworkRollback", fmt.Sprintf("Transaction %s expired (120s timeout) and was rolled back automatically", txnID))
}

func (s *networkService) recordSystemLog(ctx context.Context, action string, summary string) {
	if s.oplogService == nil {
		return
	}
	_ = s.oplogService.Record(ctx, &model.OperationLog{
		CreatedAt:  time.Now(),
		UserID:     0,
		Username:   "system",
		Module:     "ops",
		Action:     action,
		Method:     "INTERNAL",
		Path:       "/api/network",
		Body:       summary,
		StatusCode: 200,
		IP:         "127.0.0.1",
		UserAgent:  "system-daemon",
	})
}
