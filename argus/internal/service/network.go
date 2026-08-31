package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"argus/app/internal/model"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/netconfig"
)

// NetworkService 网络配置业务编排接口。
type NetworkService interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	GetOverview(ctx context.Context) (*netconfig.NetworkOverview, error)
	GetTransaction(ctx context.Context, txnID string) (*netconfig.PendingTransaction, error)
	ApplyInterface(ctx context.Context, ifaceID string, input ApplyInterfaceInput) (*netconfig.TransactionResult, error)
	SwitchMode(ctx context.Context, input SwitchModeInput) (*netconfig.TransactionResult, error)
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

// GatewayInput 网关模式输入。
type GatewayInput struct {
	DownstreamInterfaceID string `json:"downstreamInterfaceId"`
	PoolStart             string `json:"poolStart"`
	PoolEnd               string `json:"poolEnd"`
	Prefix                int    `json:"prefix"`
	LeaseDurationSeconds  int64  `json:"leaseDurationSeconds"`
	IPForward             bool   `json:"ipForward"`
}

// SwitchModeInput 模式切换输入。
type SwitchModeInput struct {
	Mode           netconfig.NetworkMode         `json:"mode"`
	SlaveIDs       []string                      `json:"slaveIds"`
	PrimarySlaveID string                        `json:"primarySlaveId"`
	XmitHashPolicy *netconfig.BondXmitHashPolicy `json:"xmitHashPolicy"`
	BondIPv4       ApplyInterfaceInput           `json:"ipv4"`
	Gateway        *GatewayInput                 `json:"gateway,omitempty"`
	ActorID        uint64                        `json:"-"`
	ActorUsername  string                        `json:"-"`
	ClientIP       string                        `json:"-"`
}

type networkService struct {
	cfg            *config.Config
	platform       netconfig.Platform
	store          netconfig.StateStore
	gatewayRuntime netconfig.GatewayRuntime
	oplogService   OperationLogService
	log            *zap.Logger
	mu             sync.Mutex
	timer          *time.Timer
	ready          bool
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

	platform, err := netconfig.NewPlatform(cfg.Network.ProfilePath, cfg.Network.StateDir, cfg.Network.FakePlatform)
	if err != nil {
		return nil, fmt.Errorf("create network platform: %w", err)
	}

	store := netconfig.NewFileStateStore(cfg.Network.StateDir, platform.Type())
	gwBackend := netconfig.NewGatewayBackend(platform.Type(), cfg.Network.FakePlatform, log)
	gwRuntime := netconfig.NewDefaultGatewayRuntime(gwBackend, store)

	return &networkService{
		cfg:            cfg,
		platform:       platform,
		store:          store,
		gatewayRuntime: gwRuntime,
		oplogService:   oplogService,
		log:            log,
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
		s.restoreGatewayState(ctx, pending.Before.Gateway, currentSnapshot.Interfaces)
		if _, err := s.platform.Restore(ctx, pending.Before); err != nil {
			s.log.Error("startup rollback failed", zap.Error(err))
		}
		_ = s.store.ClearPending()
		// 自动事件：操作者与来源 IP 取自 pending 中保存的原操作者；无关联操作者时回退到 system（spec 5.2）。
		s.recordSystemLog(ctx, "system.log.actionNetworkStartupRecovery", "Startup recovery rolled back dangling transaction",
			pending.Transaction.ActorID, pending.Transaction.ActorUsername, pending.Transaction.ClientIP, 0)
	} else {
		// 无 pending 时，若 last-valid 为 gateway 模式，恢复 gateway 运行时
		if lv, err := s.store.GetLastValid(); err == nil && lv != nil && lv.Plan.Mode == netconfig.NetworkModeGateway && lv.Plan.Gateway != nil {
			if iface, ok := currentSnapshot.Interfaces[lv.Plan.Gateway.DownstreamInterfaceID]; ok {
				st := netconfig.GatewayState{
					Plan:      lv.Plan.Gateway,
					IPForward: lv.Plan.Gateway.IPForward,
				}
				if _, err := s.gatewayRuntime.Apply(ctx, *lv.Plan.Gateway, st, &iface); err != nil {
					s.log.Error("failed to restore confirmed gateway runtime on startup", zap.Error(err))
				}
			}
		}

		// 6. 确认配置重放 reconcile（针对 Linux/macOS 重启后未持久化到内核的静态地址/网关/DNS/主接口）
		if lv, err := s.store.GetLastValid(); err == nil && lv != nil && planNeedsReconcile(lv.Plan, currentSnapshot) {
			s.log.Info("reconciling network configuration with last-valid plan on startup")
			if _, err := s.platform.Apply(ctx, lv.Plan); err != nil {
				s.log.Error("failed to reconcile last-valid network plan on startup", zap.Error(err))
			}
		}
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
	_ = s.gatewayRuntime.Close(ctx)
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

	var gwOverview *netconfig.GatewayOverview
	if snapshot.Mode.Normalize() == netconfig.NetworkModeGateway {
		gwState, _ := s.gatewayRuntime.Snapshot(ctx)
		leases, _ := s.gatewayRuntime.Leases(ctx)
		if gwState.Plan != nil {
			gwOverview = &netconfig.GatewayOverview{
				DownstreamInterfaceID: gwState.Plan.DownstreamInterfaceID,
				PoolStart:             gwState.Plan.PoolStart,
				PoolEnd:               gwState.Plan.PoolEnd,
				Prefix:                gwState.Plan.Prefix,
				LeaseDurationSeconds:  gwState.Plan.LeaseDurationSeconds,
				IPForward:             gwState.IPForward,
				Running:               gwState.Running,
				ConflictDetected:      gwState.ConflictDetected,
				Leases:                leases,
			}
		}
	}

	overview := &netconfig.NetworkOverview{
		Platform:                s.platform.Type(),
		State:                   netconfig.StateReady,
		PrimaryInterfaceID:      snapshot.PrimaryInterfaceID,
		DefaultRouteInterfaceID: snapshot.DefaultRouteInterfaceID,
		SystemDNSServers:        snapshot.SystemDNSServers,
		Interfaces:              interfacesList,
		PendingTransaction:      pendingTxn,
		Capabilities:            s.platform.Capabilities(ctx),
		Mode:                    snapshot.Mode.Normalize(),
		Bond:                    snapshot.Bond,
		Gateway:                 gwOverview,
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
	norm, err := netconfig.ValidateAndNormalizeIPv4(input.Mode, input.Primary, input.Address, input.Prefix, input.Gateway, input.DNSServers)
	if err != nil {
		return nil, errno.New(errno.CodeNetworkInvalidConfig)
	}
	normAddr := norm.Address
	normMask := norm.SubnetMask
	normPrefix := norm.Prefix
	normGW := norm.Gateway
	normDNS := norm.DNSServers

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
		timeout = netconfig.DefaultConfirmTimeout
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
		return nil, mapPlatformError(err)
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

// SwitchMode 切换整机网络工作模式。复用既有候选事务协议（120s 确认窗口、超时回滚、启动恢复）。
// 校验全部前置完成，不触碰平台则状态零修改（AC3）。
func (s *networkService) SwitchMode(ctx context.Context, input SwitchModeInput) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
	if !s.ready {
		return nil, errno.New(errno.CodeNetworkNotReady)
	}

	// 1. 校验目标模式是合法枚举值
	if !input.Mode.Valid() {
		return nil, errno.New(errno.CodeNetworkInvalidConfig)
	}

	// 2. 复用整机候选事务槽位：已有待确认事务则拒绝（R1.5）
	if existing, err := s.store.GetPending(); err == nil && existing != nil {
		return nil, errno.New(errno.CodeNetworkTransactionPending)
	}

	// 3. capability 协商：平台不支持则拒绝，不得静默降级（R2.2）
	if !slices.Contains(s.platform.Capabilities(ctx).SupportedModes, input.Mode) {
		return nil, errno.New(errno.CodeNetworkUnsupported)
	}

	// 4. 读取当前快照作为 before 与拓扑基线
	currentSnapshot, err := s.platform.Read(ctx)
	if err != nil {
		return nil, errno.New(errno.CodeNetworkUnsupported)
	}

	// 5. 模式冲突校验（R5.2 1113）
	if currentSnapshot.Mode.Normalize() == input.Mode {
		return nil, errno.New(errno.CodeNetworkBondModeConflict)
	}

	// 5.1 direct switch between active-backup and lacp-aggregation validation (design 2.2)
	isCurrentBond := currentSnapshot.Mode.Normalize().IsBond()
	isTargetBond := input.Mode.IsBond()
	if isCurrentBond && isTargetBond {
		if currentSnapshot.Bond == nil {
			return nil, errno.New(errno.CodeNetworkBondModeConflict)
		}
		// active-backup <-> lacp 之间直接切换：必须复用当前 bond 的完全相同 slave 集合
		currSlaves := slices.Clone(currentSnapshot.Bond.SlaveIDs)
		slices.Sort(currSlaves)
		reqSlaves := slices.Clone(input.SlaveIDs)
		slices.Sort(reqSlaves)
		if !slices.Equal(currSlaves, reqSlaves) {
			return nil, errno.New(errno.CodeNetworkBondModeConflict)
		}
	}

	// 6. 边缘网关 (gateway)、主备容错 (active-backup) 与链路聚合 (lacp) 参数专属校验
	var bondPlan *netconfig.BondPlan
	var gwPlan *netconfig.GatewayPlan
	var targetIface *netconfig.InterfaceInfo
	var warnings []netconfig.NetworkWarning

	if input.Mode == netconfig.NetworkModeGateway {
		if input.Gateway == nil {
			return nil, errno.New(errno.CodeNetworkInvalidConfig)
		}
		ifcInfo, exists := currentSnapshot.Interfaces[input.Gateway.DownstreamInterfaceID]
		if !exists {
			return nil, errno.New(errno.CodeNetworkGatewayPoolInvalid)
		}
		targetIface = &ifcInfo

		unvalidatedPlan := &netconfig.GatewayPlan{
			DownstreamInterfaceID: input.Gateway.DownstreamInterfaceID,
			PoolStart:             input.Gateway.PoolStart,
			PoolEnd:               input.Gateway.PoolEnd,
			Prefix:                input.Gateway.Prefix,
			LeaseDurationSeconds:  input.Gateway.LeaseDurationSeconds,
			IPForward:             input.Gateway.IPForward,
		}
		validPlan, err := netconfig.ValidateGatewayPlan(unvalidatedPlan, targetIface, currentSnapshot.PrimaryInterfaceID)
		if err != nil {
			if errors.Is(err, netconfig.ErrDhcpServerConflict) {
				return nil, errno.New(errno.CodeNetworkDhcpServerConflict)
			}
			return nil, errno.New(errno.CodeNetworkGatewayPoolInvalid)
		}
		gwPlan = validPlan

		// 启用前探测同链路是否存在既有 DHCP Server
		hasConflict, err := s.gatewayRuntime.Probe(ctx, *gwPlan, targetIface)
		if err != nil {
			s.log.Error("gateway dhcp probe failed", zap.Error(err))
			return nil, errno.New(errno.CodeNetworkApplyFailed)
		}
		if hasConflict {
			return nil, errno.New(errno.CodeNetworkDhcpServerConflict)
		}
	} else if input.Mode == netconfig.NetworkModeActiveBackup {
		if len(input.SlaveIDs) != 2 {
			return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
		}
		if input.SlaveIDs[0] == input.SlaveIDs[1] {
			return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
		}
		if !slices.Contains(input.SlaveIDs, input.PrimarySlaveID) {
			return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
		}
		for _, sid := range input.SlaveIDs {
			if !slaveUsableForBond(currentSnapshot, sid, isCurrentBond) {
				return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
			}
		}
		bondPlan = &netconfig.BondPlan{
			SlaveIDs:       input.SlaveIDs,
			PrimarySlaveID: input.PrimarySlaveID,
			Miimon:         netconfig.DefaultBondMiimon, // D2：固定 100ms，不接受客户端输入
		}
	} else if input.Mode == netconfig.NetworkModeLACP {
		if len(input.SlaveIDs) < 2 {
			return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
		}
		// 校验 slave 唯一性
		seen := make(map[string]struct{}, len(input.SlaveIDs))
		for _, sid := range input.SlaveIDs {
			if _, exists := seen[sid]; exists {
				return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
			}
			seen[sid] = struct{}{}
		}

		for _, sid := range input.SlaveIDs {
			if !slaveUsableForBond(currentSnapshot, sid, isCurrentBond) {
				return nil, errno.New(errno.CodeNetworkBondSlaveInvalid)
			}
		}

		// 检查速率和双工一致性以产生 non-blocking warning
		if slaveLinkMismatch(currentSnapshot, input.SlaveIDs) {
			warnings = append(warnings, netconfig.NetworkWarning{
				Code:         netconfig.WarningBondSlaveLinkMismatch,
				InterfaceIDs: input.SlaveIDs,
			})
		}

		hashPolicy := netconfig.DefaultBondXmitHashPolicy
		if input.XmitHashPolicy != nil {
			if !input.XmitHashPolicy.Valid() {
				return nil, errno.New(errno.CodeNetworkInvalidConfig)
			}
			hashPolicy = *input.XmitHashPolicy
		}

		lacpRate := netconfig.BondLACPRateSlow
		bondPlan = &netconfig.BondPlan{
			SlaveIDs:       input.SlaveIDs,
			XmitHashPolicy: &hashPolicy,
			LACPRate:       &lacpRate,
		}
	}

	// 7. bond 的 IPv4 参数校验（复用统一 IPv4 校验规则）
	var normAddr *string
	var normMask *string
	var normPrefix *int
	var normGW *string
	var normDNS []string
	if input.Mode.IsBond() {
		norm, err := netconfig.ValidateAndNormalizeIPv4(
			input.BondIPv4.Mode,
			input.BondIPv4.Primary,
			input.BondIPv4.Address,
			input.BondIPv4.Prefix,
			input.BondIPv4.Gateway,
			input.BondIPv4.DNSServers,
		)
		if err != nil {
			return nil, errno.New(errno.CodeNetworkInvalidConfig)
		}
		normAddr = norm.Address
		normMask = norm.SubnetMask
		normPrefix = norm.Prefix
		normGW = norm.Gateway
		normDNS = norm.DNSServers
	}

	// 8. 构建完整 HostPlan：以 before 接口配置为基线
	beforeGWState, _ := s.gatewayRuntime.Snapshot(ctx)
	currentSnapshot.Gateway = &beforeGWState

	plan := netconfig.HostPlan{
		Interfaces:         make(map[string]netconfig.InterfacePlan),
		PrimaryInterfaceID: currentSnapshot.PrimaryInterfaceID,
		Mode:               input.Mode,
		Bond:               bondPlan,
		Gateway:            gwPlan,
	}
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

	if input.Mode == netconfig.NetworkModeGateway {
		// 网关模式下下行接口保持静态配置
	} else if input.Mode.IsBond() {
		// 进入 bonding：slave 条目保留完整原值（含 primary/gateway/dns），随 last-valid 持久化，
		// 供退出模式时恢复（R3.4 / design 5.2 步骤 8）。
		// 追加 bond0 计划
		bondID := "bond0"
		plan.Interfaces[bondID] = netconfig.InterfacePlan{
			Mode:       input.BondIPv4.Mode,
			Primary:    input.BondIPv4.Primary,
			Address:    normAddr,
			Prefix:     normPrefix,
			Gateway:    normGW,
			DNSServers: normDNS,
		}
		if input.BondIPv4.Primary {
			newPrimary := bondID
			plan.PrimaryInterfaceID = &newPrimary
		}
	} else if currentSnapshot.Bond != nil {
		// 退出 bonding：移除 bond0 条目，从 last-valid 恢复 slave 原 IPv4 配置与 primary 指向（R3.4 / design 5.2 步骤 8）。
		delete(plan.Interfaces, "bond0")
		// 进入 bonding 时 slave 完整原值已随候选 plan 持久化到 last-valid；
		// 若 last-valid 缺失/损坏则保持清空（降级，可接受）。
		if lv, err := s.store.GetLastValid(); err == nil && lv != nil && lv.Plan.Interfaces != nil {
			for _, sid := range currentSnapshot.Bond.SlaveIDs {
				if orig, ok := lv.Plan.Interfaces[sid]; ok {
					plan.Interfaces[sid] = orig
				}
			}
			// 恢复 primary：last-valid 中 primary=true 的非 bond 接口
			for id, ip := range lv.Plan.Interfaces {
				if id != "bond0" && ip.Primary {
					pid := id
					plan.PrimaryInterfaceID = &pid
					break
				}
			}
		}
	}

	// 9. 生成事务
	now := time.Now().UTC()
	timeout := s.cfg.Network.ConfirmTimeout
	if timeout <= 0 {
		timeout = netconfig.DefaultConfirmTimeout
	}
	expiresAt := now.Add(timeout)
	txnID := fmt.Sprintf("txn-%d", now.UnixNano())

	targetIfaceID := ""
	if input.Mode.IsBond() {
		targetIfaceID = "bond0"
	} else if input.Mode == netconfig.NetworkModeGateway {
		targetIfaceID = gwPlan.DownstreamInterfaceID
	}

	reconnectAddrs := make([]netconfig.ReconnectAddress, 0)
	if input.Mode.IsBond() && normAddr != nil && normPrefix != nil {
		reconnectAddrs = append(reconnectAddrs, netconfig.ReconnectAddress{
			InterfaceID: "bond0",
			Address:     *normAddr,
			Prefix:      *normPrefix,
		})
	} else if input.Mode == netconfig.NetworkModeGateway && targetIface != nil && targetIface.IPv4.Address != nil && targetIface.IPv4.Prefix != nil {
		reconnectAddrs = append(reconnectAddrs, netconfig.ReconnectAddress{
			InterfaceID: targetIface.ID,
			Address:     *targetIface.IPv4.Address,
			Prefix:      *targetIface.IPv4.Prefix,
		})
	}

	summary := fmt.Sprintf("mode switch: %s -> %s slaves=%v primary=%s",
		currentSnapshot.Mode.Normalize(), input.Mode, input.SlaveIDs, input.PrimarySlaveID)
	if input.Mode == netconfig.NetworkModeGateway {
		summary = fmt.Sprintf("mode switch: %s -> gateway iface=%s pool=[%s, %s] forward=%v",
			currentSnapshot.Mode.Normalize(), gwPlan.DownstreamInterfaceID, gwPlan.PoolStart, gwPlan.PoolEnd, gwPlan.IPForward)
	}

	pending := &netconfig.PendingData{
		Transaction: netconfig.PendingTransaction{
			ID:                          txnID,
			Status:                      netconfig.TxnStatusPendingConfirmation,
			Action:                      netconfig.TxnActionModeSwitch,
			CreatedAt:                   now,
			ExpiresAt:                   expiresAt,
			RemainingSeconds:            int(timeout.Seconds()),
			TargetInterfaceID:           targetIfaceID,
			PreviousPrimaryInterfaceID:  currentSnapshot.PrimaryInterfaceID,
			CandidatePrimaryInterfaceID: plan.PrimaryInterfaceID,
			ReconnectAddresses:          reconnectAddrs,
			RequiresReconnect:           true,
			TargetMode:                  input.Mode,
			PreviousMode:                currentSnapshot.Mode.Normalize(),
			Warnings:                    warnings,
			Candidate: netconfig.CandidateSummary{
				Mode:       input.BondIPv4.Mode,
				Address:    normAddr,
				Prefix:     normPrefix,
				SubnetMask: normMask,
				Gateway:    normGW,
				DNSServers: normDNS,
			},
			ActorID:       input.ActorID,
			ActorUsername: input.ActorUsername,
			ClientIP:      input.ClientIP,
			ActionSummary: summary,
		},
		Before:    currentSnapshot,
		Candidate: plan,
	}

	// 10. 持久化 pending 后平台应用与 runtime 应用，失败补偿
	if err := s.store.SetPending(pending); err != nil {
		return nil, errno.New(errno.CodeNetworkStateCorrupt)
	}

	// 平台层应用
	candidateSnapshot, err := s.platform.Apply(ctx, plan)
	if err != nil {
		s.log.Error("platform apply failed on mode switch, restoring before snapshot", zap.Error(err))
		_, _ = s.platform.Restore(ctx, currentSnapshot)
		_ = s.store.ClearPending()
		if input.Mode == netconfig.NetworkModeLACP {
			return nil, errno.New(errno.CodeNetworkLacpNegotiationFailed)
		}
		return nil, errno.New(errno.CodeNetworkApplyFailed)
	}

	// Gateway Runtime 应用
	if input.Mode == netconfig.NetworkModeGateway {
		if _, err := s.gatewayRuntime.Apply(ctx, *gwPlan, beforeGWState, targetIface); err != nil {
			s.log.Error("gateway runtime apply failed, restoring platform and runtime", zap.Error(err))
			_, _ = s.gatewayRuntime.Restore(ctx, beforeGWState, targetIface)
			_, _ = s.platform.Restore(ctx, currentSnapshot)
			_ = s.store.ClearPending()
			return nil, errno.New(errno.CodeNetworkApplyFailed)
		}
	} else if currentSnapshot.Mode.Normalize() == netconfig.NetworkModeGateway {
		// 退出网关模式
		s.restoreGatewayState(ctx, currentSnapshot.Gateway, currentSnapshot.Interfaces)
	}

	// 11. 启动超时自动回滚定时器 + 审计
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(timeout, func() {
		s.handleTimeout(context.Background(), txnID)
	})

	// 补写模式切换业务摘要审计：操作者/来源 IP/耗时取自本次 HTTP 请求者（R5.3）。
	actionKey := "system.log.actionNetworkModeSwitch"
	if input.Mode == netconfig.NetworkModeGateway {
		actionKey = "system.log.actionNetworkGatewaySwitch"
	}
	s.recordSystemLog(ctx, actionKey, summary,
		input.ActorID, input.ActorUsername, input.ClientIP, time.Since(start).Milliseconds())

	overview := &netconfig.NetworkOverview{
		Platform:                s.platform.Type(),
		State:                   netconfig.StateReady,
		PrimaryInterfaceID:      candidateSnapshot.PrimaryInterfaceID,
		DefaultRouteInterfaceID: candidateSnapshot.DefaultRouteInterfaceID,
		SystemDNSServers:        candidateSnapshot.SystemDNSServers,
		PendingTransaction:      &pending.Transaction,
		Mode:                    candidateSnapshot.Mode.Normalize(),
		Bond:                    candidateSnapshot.Bond,
	}

	return &netconfig.TransactionResult{
		TransactionID:      txnID,
		Status:             netconfig.TxnStatusPendingConfirmation,
		ExpiresAt:          &expiresAt,
		Overview:           overview,
		ReconnectAddresses: reconnectAddrs,
		Warnings:           warnings,
	}, nil
}

func (s *networkService) ConfirmTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
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

	// 模式切换事务补写审计（R5.3），不改既有控制流
	if pending.Transaction.Action == netconfig.TxnActionModeSwitch {
		actionKey := "system.log.actionNetworkModeSwitch"
		if pending.Transaction.TargetMode == netconfig.NetworkModeGateway {
			actionKey = "system.log.actionNetworkGatewaySwitch"
		}
		s.recordSystemLog(ctx, actionKey, s.modeSwitchSummary("confirmed", pending),
			actorID, actorUsername, clientIP, time.Since(start).Milliseconds())
	}

	return &netconfig.TransactionResult{
		TransactionID: txnID,
		Status:        netconfig.TxnStatusConfirmed,
	}, nil
}

func (s *networkService) CancelTransaction(ctx context.Context, txnID string, actorID uint64, actorUsername string, clientIP string) (*netconfig.TransactionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
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
	s.restoreGatewayState(ctx, pending.Before.Gateway, pending.Before.Interfaces)

	if _, err := s.platform.Restore(ctx, pending.Before); err != nil {
		s.log.Error("platform restore failed on cancel", zap.Error(err))
		return nil, errno.New(errno.CodeNetworkRecoveryFailed)
	}

	_ = s.store.ClearPending()
	s.recordSystemLog(ctx, "system.log.actionNetworkRollback", fmt.Sprintf("Transaction %s cancelled by user", txnID),
		actorID, actorUsername, clientIP, time.Since(start).Milliseconds())
	// 模式切换事务补写模式细节审计（R5.3），不改既有控制流
	if pending.Transaction.Action == netconfig.TxnActionModeSwitch {
		actionKey := "system.log.actionNetworkModeSwitch"
		if pending.Transaction.TargetMode == netconfig.NetworkModeGateway {
			actionKey = "system.log.actionNetworkGatewaySwitch"
		}
		s.recordSystemLog(ctx, actionKey, s.modeSwitchSummary("cancelled", pending),
			actorID, actorUsername, clientIP, time.Since(start).Milliseconds())
	}

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

	beforeGW, _ := s.gatewayRuntime.Snapshot(ctx)
	currentSnapshot.Gateway = &beforeGW

	now := time.Now().UTC()
	timeout := s.cfg.Network.ConfirmTimeout
	if timeout <= 0 {
		timeout = netconfig.DefaultConfirmTimeout
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

	// 若当前在 gateway模式，先清理 gateway runtime
	if currentSnapshot.Mode.Normalize() == netconfig.NetworkModeGateway {
		s.restoreGatewayState(ctx, &beforeGW, currentSnapshot.Interfaces)
	}

	if _, err := s.platform.Apply(ctx, factory.Plan); err != nil {
		if currentSnapshot.Gateway != nil {
			s.restoreGatewayState(ctx, currentSnapshot.Gateway, currentSnapshot.Interfaces)
		}
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
	s.restoreGatewayState(ctx, pending.Before.Gateway, pending.Before.Interfaces)
	_, _ = s.platform.Restore(ctx, pending.Before)
	_ = s.store.ClearPending()
	// 自动事件：操作者与来源 IP 取自 pending 中保存的原操作者；无关联操作者时回退到 system（spec 5.2）。
	s.recordSystemLog(ctx, "system.log.actionNetworkRollback", fmt.Sprintf("Transaction %s expired (120s timeout) and was rolled back automatically", txnID),
		pending.Transaction.ActorID, pending.Transaction.ActorUsername, pending.Transaction.ClientIP, 0)
	// 模式切换事务补写模式细节审计（R5.3），不改既有控制流
	if pending.Transaction.Action == netconfig.TxnActionModeSwitch {
		actionKey := "system.log.actionNetworkModeSwitch"
		if pending.Transaction.TargetMode == netconfig.NetworkModeGateway {
			actionKey = "system.log.actionNetworkGatewaySwitch"
		}
		s.recordSystemLog(ctx, actionKey, s.modeSwitchSummary("rolled back on timeout", pending),
			pending.Transaction.ActorID, pending.Transaction.ActorUsername, pending.Transaction.ClientIP, 0)
	}
}

// modeSwitchSummary 构造模式切换审计摘要。切回 multi-address 时 Candidate.Bond/Gateway 为 nil，需空指针保护。
func (s *networkService) modeSwitchSummary(event string, pending *netconfig.PendingData) string {
	slaves := "-"
	primary := "-"
	if pending.Candidate.Bond != nil {
		slaves = fmt.Sprintf("%v", pending.Candidate.Bond.SlaveIDs)
		primary = pending.Candidate.Bond.PrimarySlaveID
	}
	if pending.Transaction.TargetMode == netconfig.NetworkModeGateway && pending.Candidate.Gateway != nil {
		gw := pending.Candidate.Gateway
		return fmt.Sprintf("mode switch %s: %s -> gateway iface=%s pool=[%s, %s] forward=%v",
			event, pending.Transaction.PreviousMode, gw.DownstreamInterfaceID, gw.PoolStart, gw.PoolEnd, gw.IPForward)
	}
	return fmt.Sprintf("mode switch %s: %s -> %s slaves=%s primary=%s",
		event, pending.Transaction.PreviousMode, pending.Transaction.TargetMode, slaves, primary)
}

// slaveUsableForBond 判断 sid 是否可加入 bond：当前已处于 bond 拓扑且 sid 是现有成员时允许复用；
// 否则要求该接口存在、可写、未被其他 master 占用且本身不是 bond 逻辑口。
func slaveUsableForBond(current netconfig.HostSnapshot, sid string, isCurrentBond bool) bool {
	slave, exists := current.Interfaces[sid]
	if !exists {
		return false
	}
	if isCurrentBond && current.Bond != nil && slices.Contains(current.Bond.SlaveIDs, sid) {
		return true
	}
	return slave.Writable && slave.MasterID == nil && !slave.IsBond
}

// slaveLinkMismatch 检查各 slave 的速率/双工是否一致（仅对已知值比较），不一致返回 true。
// 首个未知双工的接口作为参照，与既有链路监测语义保持一致。
func slaveLinkMismatch(current netconfig.HostSnapshot, slaveIDs []string) bool {
	var firstSpeed *int
	var firstDuplex netconfig.InterfaceDuplex
	for _, sid := range slaveIDs {
		slave := current.Interfaces[sid]
		if firstDuplex == "" {
			firstSpeed = slave.SpeedMbps
			firstDuplex = slave.Duplex
			continue
		}
		if (firstSpeed != nil && slave.SpeedMbps != nil && *firstSpeed != *slave.SpeedMbps) ||
			(firstDuplex != "" && slave.Duplex != "" && firstDuplex != slave.Duplex) {
			return true
		}
	}
	return false
}

// recordSystemLog 写一条 ops 模块操作日志（R5.3 / spec 5.2）。
// actorID/actorUsername/clientIP 为空时回退到系统守护进程身份（自动事件无关联操作者时使用固定 system actor）。
func (s *networkService) recordSystemLog(ctx context.Context, action, summary string, actorID uint64, actorUsername, clientIP string, durationMs int64) {
	if s.oplogService == nil {
		return
	}
	if actorUsername == "" {
		actorUsername = "system"
	}
	if clientIP == "" {
		clientIP = "127.0.0.1"
	}
	_ = s.oplogService.Record(ctx, &model.OperationLog{
		CreatedAt:  time.Now(),
		UserID:     actorID,
		Username:   actorUsername,
		Module:     "ops",
		Action:     action,
		Method:     "INTERNAL",
		Path:       "/api/network",
		Body:       summary,
		StatusCode: 200,
		DurationMs: durationMs,
		IP:         clientIP,
		UserAgent:  "system-daemon",
	})
}

// restoreGatewayState 辅助函数：根据 GatewayState 与网卡列表恢复 gateway runtime。
func (s *networkService) restoreGatewayState(ctx context.Context, gwState *netconfig.GatewayState, ifaces map[string]netconfig.InterfaceInfo) {
	if gwState != nil {
		var iface *netconfig.InterfaceInfo
		if gwState.Plan != nil && ifaces != nil {
			if ifcInfo, ok := ifaces[gwState.Plan.DownstreamInterfaceID]; ok {
				iface = &ifcInfo
			}
		}
		_, _ = s.gatewayRuntime.Restore(ctx, *gwState, iface)
	} else {
		_, _ = s.gatewayRuntime.Restore(ctx, netconfig.GatewayState{}, nil)
	}
}

// planNeedsReconcile 检查系统当前快照是否偏离已持久化的确认配置（如重启后静态配置未自动生效）。
func planNeedsReconcile(plan netconfig.HostPlan, snap netconfig.HostSnapshot) bool {
	if plan.Mode != "" && plan.Mode != snap.Mode {
		return true
	}
	if plan.PrimaryInterfaceID != nil {
		if snap.PrimaryInterfaceID == nil || *plan.PrimaryInterfaceID != *snap.PrimaryInterfaceID {
			return true
		}
	}
	for id, pIf := range plan.Interfaces {
		cIf, ok := snap.Interfaces[id]
		if !ok || cIf.IPv4.Mode != pIf.Mode {
			return true
		}
		if pIf.Mode == netconfig.IPModeStatic {
			if (pIf.Address != nil && (cIf.IPv4.Address == nil || *cIf.IPv4.Address != *pIf.Address)) ||
				(pIf.Prefix != nil && (cIf.IPv4.Prefix == nil || *cIf.IPv4.Prefix != *pIf.Prefix)) ||
				(pIf.Gateway != nil && (cIf.IPv4.Gateway == nil || *cIf.IPv4.Gateway != *pIf.Gateway)) {
				return true
			}
			if len(pIf.DNSServers) > 0 && !slices.Equal(pIf.DNSServers, snap.SystemDNSServers) {
				return true
			}
		}
	}
	return false
}

// mapPlatformError 将底层平台错误映射为业务 errno。
func mapPlatformError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, netconfig.ErrOwnershipConflict) {
		return errno.New(errno.CodeNetworkOwnershipConflict)
	}
	if errors.Is(err, netconfig.ErrExternalDrift) {
		return errno.New(errno.CodeNetworkExternalDrift)
	}
	if errors.Is(err, netconfig.ErrUnsupported) {
		return errno.New(errno.CodeNetworkUnsupported)
	}
	if errors.Is(err, netconfig.ErrLacpNegotiationFailed) {
		return errno.New(errno.CodeNetworkLacpNegotiationFailed)
	}
	return errno.New(errno.CodeNetworkApplyFailed)
}
