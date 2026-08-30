package netconfig

import (
	"slices"
	"time"
)

// PlatformType 运行平台类型。
type PlatformType string

const (
	PlatformLinux  PlatformType = "linux"
	PlatformDarwin PlatformType = "darwin"
	PlatformFake   PlatformType = "fake"
)

// NetworkMode 整机网络工作模式。空值等价于 NetworkModeMultiAddress。
type NetworkMode string

const (
	NetworkModeMultiAddress NetworkMode = "multi-address"
	NetworkModeActiveBackup NetworkMode = "active-backup"
	NetworkModeLACP         NetworkMode = "lacp-aggregation"
	NetworkModeGateway      NetworkMode = "gateway"
)

// AllNetworkModes 枚举的单一事实来源，供校验/迭代/默认值复用。
// 新增模式只需改这里 + 常量定义 + 行为分支，避免校验点散落。
func AllNetworkModes() []NetworkMode {
	return []NetworkMode{NetworkModeMultiAddress, NetworkModeActiveBackup, NetworkModeLACP, NetworkModeGateway}
}

// Valid 边界校验用，替代各调用点手写的 switch / slices.Contains。
func (m NetworkMode) Valid() bool {
	return slices.Contains(AllNetworkModes(), m)
}

// Normalize 空值归一化为 multi-address，只在读侧调用。
func (m NetworkMode) Normalize() NetworkMode {
	if m == "" {
		return NetworkModeMultiAddress
	}
	return m
}

// IsBond 是否属于绑定（bonding）模式，即占用 bond0 与 slave 集合的拓扑。
func (m NetworkMode) IsBond() bool {
	return m == NetworkModeActiveBackup || m == NetworkModeLACP
}

// DefaultBondMiimon 默认链路监测周期（毫秒）。
const DefaultBondMiimon = 100

// DefaultConfirmTimeout 默认候选事务确认超时时间。
const DefaultConfirmTimeout = 120 * time.Second

// BondXmitHashPolicy LACP 出向传输哈希策略封闭枚举。
type BondXmitHashPolicy string

const (
	BondXmitHashPolicyLayer2  BondXmitHashPolicy = "layer2"
	BondXmitHashPolicyLayer23 BondXmitHashPolicy = "layer2+3"
	BondXmitHashPolicyLayer34 BondXmitHashPolicy = "layer3+4"
	DefaultBondXmitHashPolicy                    = BondXmitHashPolicyLayer23
)

// AllBondXmitHashPolicies 支持的传输哈希策略列表。
func AllBondXmitHashPolicies() []BondXmitHashPolicy {
	return []BondXmitHashPolicy{
		BondXmitHashPolicyLayer2,
		BondXmitHashPolicyLayer23,
		BondXmitHashPolicyLayer34,
	}
}

// Valid 校验传输哈希策略是否属于封闭枚举。
func (p BondXmitHashPolicy) Valid() bool {
	return slices.Contains(AllBondXmitHashPolicies(), p)
}

// BondLACPRate LACP 协商报文发送速率内部枚举。
type BondLACPRate string

const (
	BondLACPRateSlow BondLACPRate = "slow"
)

// LACPPortState 单个 LACP 端口的 actor/partner 状态标志。
type LACPPortState struct {
	Active       bool `json:"active"`
	ShortTimeout bool `json:"shortTimeout"`
	Aggregation  bool `json:"aggregation"`
	Synchronized bool `json:"synchronized"`
	Collecting   bool `json:"collecting"`
	Distributing bool `json:"distributing"`
	Defaulted    bool `json:"defaulted"`
	Expired      bool `json:"expired"`
}

// LACPPortStatus 单个 slave 接口的 LACP 协商与聚合状态。
type LACPPortStatus struct {
	InterfaceID  string        `json:"interfaceId"`
	AggregatorID *uint16       `json:"aggregatorId,omitempty"`
	InAggregator bool          `json:"inAggregator"`
	ActorState   LACPPortState `json:"actorState"`
	PartnerState LACPPortState `json:"partnerState"`
}

// LACPStatus 整机 bond0 接口的 LACP 协商与拓扑可观测状态。
type LACPStatus struct {
	AggregatorID   *uint16          `json:"aggregatorId,omitempty"`
	Negotiated     bool             `json:"negotiated"`
	Slaves         []LACPPortStatus `json:"slaves"`
	DiagnosticCode string           `json:"diagnosticCode,omitempty"`
}

// 边缘网关与 DHCP 默认参数
const (
	DefaultGatewayLeaseDurationSeconds int64 = 3600
	MinGatewayLeaseDurationSeconds     int64 = 60
	MaxGatewayLeaseDurationSeconds     int64 = 604800
	GatewayLeasesFilename                    = "gateway-leases.json"
)

// GatewayPlan 边缘网关模式的目标配置。
type GatewayPlan struct {
	DownstreamInterfaceID string `json:"downstreamInterfaceId"`
	PoolStart             string `json:"poolStart"`
	PoolEnd               string `json:"poolEnd"`
	Prefix                int    `json:"prefix"`
	LeaseDurationSeconds  int64  `json:"leaseDurationSeconds"`
	IPForward             bool   `json:"ipForward"`
}

// GatewayState 运行时 Gateway 内部状态。
type GatewayState struct {
	Plan              *GatewayPlan `json:"plan,omitempty"`
	Running           bool         `json:"running"`
	IPForward         bool         `json:"ipForward"`
	PreviousIPForward *bool        `json:"previousIpForward,omitempty"`
	ConflictDetected  bool         `json:"conflictDetected"`
}

// GatewayLease DHCP 租约记录。
type GatewayLease struct {
	MAC           string    `json:"mac"`
	IP            string    `json:"ip"`
	StartsAt      time.Time `json:"startsAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	LastRenewedAt time.Time `json:"lastRenewedAt"`
	Hostname      string    `json:"hostname,omitempty"`
}

// GatewayOverview 网络概览中对外暴露的 Gateway 视图。
type GatewayOverview struct {
	DownstreamInterfaceID string         `json:"downstreamInterfaceId"`
	PoolStart             string         `json:"poolStart"`
	PoolEnd               string         `json:"poolEnd"`
	Prefix                int            `json:"prefix"`
	LeaseDurationSeconds  int64          `json:"leaseDurationSeconds"`
	IPForward             bool           `json:"ipForward"`
	Running               bool           `json:"running"`
	ConflictDetected      bool           `json:"conflictDetected"`
	Leases                []GatewayLease `json:"leases"`
}

// BondPlan 绑定拓扑的目标配置。
type BondPlan struct {
	SlaveIDs       []string            `json:"slaveIds"`                 // active-backup 恰好 2 个，lacp >= 2
	PrimarySlaveID string              `json:"primarySlaveId,omitempty"` // active-backup 必须 ∈ SlaveIDs；lacp 不使用
	Miimon         int                 `json:"miimon,omitempty"`         // 固定 100ms，由服务端填充
	XmitHashPolicy *BondXmitHashPolicy `json:"xmitHashPolicy,omitempty"` // lacp 可配，默认 layer2+3
	LACPRate       *BondLACPRate       `json:"lacpRate,omitempty"`       // lacp 内部固定 slow
}

// BondTopology 平台回读的实际绑定拓扑。
type BondTopology struct {
	BondInterfaceID string              `json:"bondInterfaceId"`
	SlaveIDs        []string            `json:"slaveIds"`
	PrimarySlaveID  string              `json:"primarySlaveId,omitempty"`
	ActiveSlaveID   *string             `json:"activeSlaveId,omitempty"` // active-backup 当前实际承载流量的 slave
	Miimon          int                 `json:"miimon,omitempty"`
	XmitHashPolicy  *BondXmitHashPolicy `json:"xmitHashPolicy,omitempty"`
	LACP            *LACPStatus         `json:"lacp,omitempty"`
}

// StateStatus 网络整体运行状态。
type StateStatus string

const (
	StateReady             StateStatus = "ready"
	StateDegraded          StateStatus = "degraded"
	StateOwnershipConflict StateStatus = "ownership_conflict"
	StateRecoveryFailed    StateStatus = "recovery_failed"
	StateUnsupported       StateStatus = "unsupported"
)

// InterfaceType 网卡类型。
type InterfaceType string

const (
	InterfaceEthernet InterfaceType = "ethernet"
	InterfaceWifi     InterfaceType = "wifi"
	InterfaceOther    InterfaceType = "other"
)

// LinkStatus 物理/逻辑连接状态。
type LinkStatus string

const (
	LinkUp      LinkStatus = "up"
	LinkDown    LinkStatus = "down"
	LinkUnknown LinkStatus = "unknown"
)

// OwnershipStatus 网卡所有权状态。
type OwnershipStatus string

const (
	OwnershipManaged     OwnershipStatus = "managed"
	OwnershipUnproven    OwnershipStatus = "unproven"
	OwnershipConflict    OwnershipStatus = "conflict"
	OwnershipUnsupported OwnershipStatus = "unsupported"
)

// IPMode IPv4 获取模式。
type IPMode string

const (
	IPModeDHCP        IPMode = "dhcp"
	IPModeStatic      IPMode = "static"
	IPModeUnknown     IPMode = "unknown"
	IPModeUnsupported IPMode = "unsupported"
)

// IPStatus IPv4 实际生效状态。
type IPStatus string

const (
	IPStatusEffective   IPStatus = "effective"
	IPStatusUnavailable IPStatus = "unavailable"
	IPStatusConflict    IPStatus = "conflict"
	IPStatusUnsupported IPStatus = "unsupported"
)

// TransactionStatus 候选事务对客户端/内部可见的状态。
type TransactionStatus string

const (
	TxnStatusApplying            TransactionStatus = "applying"
	TxnStatusPendingConfirmation TransactionStatus = "pending_confirmation"
	TxnStatusCommitting          TransactionStatus = "committing"
	TxnStatusRollingBack         TransactionStatus = "rolling_back"
	TxnStatusConfirmed           TransactionStatus = "confirmed"
	TxnStatusRolledBack          TransactionStatus = "rolled_back"
	TxnStatusRecoveryFailed      TransactionStatus = "recovery_failed"
)

// TransactionAction 事务类型。
type TransactionAction string

const (
	TxnActionApply        TransactionAction = "apply"
	TxnActionFactoryReset TransactionAction = "factory_reset"
	TxnActionModeSwitch   TransactionAction = "mode_switch"
)

// Capabilities 平台能力集。
type Capabilities struct {
	DHCP            bool          `json:"dhcp"`
	StaticIPv4      bool          `json:"staticIpv4"`
	FactoryReset    bool          `json:"factoryReset"`
	WifiAssociation bool          `json:"wifiAssociation"`
	SupportedModes  []NetworkMode `json:"supportedModes"`
}

// IPv4State 单个接口的实际 IPv4 状态。
type IPv4State struct {
	Mode       IPMode   `json:"mode"`
	Address    *string  `json:"address"`
	Prefix     *int     `json:"prefix"`
	SubnetMask *string  `json:"subnetMask"`
	Gateway    *string  `json:"gateway"`
	DNSServers []string `json:"dnsServers"`
	Status     IPStatus `json:"status"`
}

// InterfaceDuplex 网卡双工模式。
type InterfaceDuplex string

const (
	DuplexUnknown InterfaceDuplex = "unknown"
	DuplexHalf    InterfaceDuplex = "half"
	DuplexFull    InterfaceDuplex = "full"
)

// InterfaceInfo 接口信息。
type InterfaceInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName"`
	Type        InterfaceType   `json:"type"`
	MAC         *string         `json:"mac"`
	LinkStatus  LinkStatus      `json:"linkStatus"`
	Ownership   OwnershipStatus `json:"ownership"`
	Writable    bool            `json:"writable"`
	IsPrimary   bool            `json:"isPrimary"`
	IsBond      bool            `json:"isBond"`   // 该接口是 bond 逻辑口
	MasterID    *string         `json:"masterId"` // 该接口是某 bond 的 slave
	SpeedMbps   *int            `json:"speedMbps,omitempty"`
	Duplex      InterfaceDuplex `json:"duplex,omitempty"`
	IPv4        IPv4State       `json:"ipv4"`
	Fingerprint string          `json:"-"`
}

// NetworkWarningCode 非阻断告警码。
type NetworkWarningCode string

const (
	WarningBondSlaveLinkMismatch NetworkWarningCode = "bond_slave_link_mismatch"
)

// NetworkWarning 非阻断告警对象。
type NetworkWarning struct {
	Code         NetworkWarningCode `json:"code"`
	InterfaceIDs []string           `json:"interfaceIds"`
}

// ReconnectAddress 重连地址提示。
type ReconnectAddress struct {
	InterfaceID string `json:"interfaceId"`
	Address     string `json:"address"`
	Prefix      int    `json:"prefix"`
}

// CandidateSummary 候选配置摘要。
type CandidateSummary struct {
	Mode       IPMode   `json:"mode"`
	Address    *string  `json:"address,omitempty"`
	Prefix     *int     `json:"prefix,omitempty"`
	SubnetMask *string  `json:"subnetMask,omitempty"`
	Gateway    *string  `json:"gateway,omitempty"`
	DNSServers []string `json:"dnsServers,omitempty"`
}

// PendingTransaction 待确认事务。
type PendingTransaction struct {
	ID                          string             `json:"id"`
	Status                      TransactionStatus  `json:"status"`
	Action                      TransactionAction  `json:"action"`
	CreatedAt                   time.Time          `json:"createdAt"`
	ExpiresAt                   time.Time          `json:"expiresAt"`
	RemainingSeconds            int                `json:"remainingSeconds"`
	TargetInterfaceID           string             `json:"targetInterfaceId"`
	PreviousPrimaryInterfaceID  *string            `json:"previousPrimaryInterfaceId"`
	CandidatePrimaryInterfaceID *string            `json:"candidatePrimaryInterfaceId"`
	ReconnectAddresses          []ReconnectAddress `json:"reconnectAddresses"`
	RequiresReconnect           bool               `json:"requiresReconnect"`
	Candidate                   CandidateSummary   `json:"candidate"`
	TargetMode                  NetworkMode        `json:"targetMode,omitempty"`   // 仅 mode_switch 事务
	PreviousMode                NetworkMode        `json:"previousMode,omitempty"` // 仅 mode_switch 事务
	Warnings                    []NetworkWarning   `json:"warnings,omitempty"`     // 非阻断告警

	// 审计所需元数据（不输出到公共简短 JSON，但在落盘与审计中必须持久化）
	ActorID       uint64 `json:"actorId,omitempty"`
	ActorUsername string `json:"actorUsername,omitempty"`
	ClientIP      string `json:"clientIp,omitempty"`
	ActionSummary string `json:"actionSummary,omitempty"`
}

// NetworkOverview 网络整体概览。
type NetworkOverview struct {
	Platform                PlatformType        `json:"platform"`
	State                   StateStatus         `json:"state"`
	PrimaryInterfaceID      *string             `json:"primaryInterfaceId"`
	DefaultRouteInterfaceID *string             `json:"defaultRouteInterfaceId"`
	SystemDNSServers        []string            `json:"systemDnsServers"`
	Interfaces              []InterfaceInfo     `json:"interfaces"`
	PendingTransaction      *PendingTransaction `json:"pendingTransaction"`
	Capabilities            Capabilities        `json:"capabilities"`
	Mode                    NetworkMode         `json:"mode"` // 始终有值，空则归一化为 multi-address
	Bond                    *BondTopology       `json:"bond"`
	Gateway                 *GatewayOverview    `json:"gateway"`
}

// TransactionResult 事务操作执行结果。
type TransactionResult struct {
	TransactionID      string             `json:"transactionId"`
	Status             TransactionStatus  `json:"status"`
	ExpiresAt          *time.Time         `json:"expiresAt,omitempty"`
	Overview           *NetworkOverview   `json:"overview,omitempty"`
	ReconnectAddresses []ReconnectAddress `json:"reconnectAddresses"`
	Warnings           []NetworkWarning   `json:"warnings,omitempty"`
	Reason             *string            `json:"reason"`
}

// InterfacePlan 单个接口的目标配置。
type InterfacePlan struct {
	Mode       IPMode   `json:"mode"`
	Primary    bool     `json:"primary"`
	Address    *string  `json:"address,omitempty"`
	Prefix     *int     `json:"prefix,omitempty"`
	Gateway    *string  `json:"gateway,omitempty"`
	DNSServers []string `json:"dnsServers,omitempty"`
}

// HostPlan 整机完整配置计划。
type HostPlan struct {
	Interfaces         map[string]InterfacePlan `json:"interfaces"`
	PrimaryInterfaceID *string                  `json:"primaryInterfaceId"`
	Mode               NetworkMode              `json:"mode,omitempty"`
	Bond               *BondPlan                `json:"bond,omitempty"`
	Gateway            *GatewayPlan             `json:"gateway,omitempty"`
}

// NativeSnapshot 平台专有快照用于恢复。
type NativeSnapshot struct {
	Version int    `json:"version"`
	Data    []byte `json:"data"`
}

// HostSnapshot 平台回读的整机快照。
type HostSnapshot struct {
	Interfaces              map[string]InterfaceInfo `json:"interfaces"`
	PrimaryInterfaceID      *string                  `json:"primaryInterfaceId"`
	DefaultRouteInterfaceID *string                  `json:"defaultRouteInterfaceId"`
	SystemDNSServers        []string                 `json:"systemDnsServers"`
	Fingerprint             string                   `json:"fingerprint"`
	Native                  NativeSnapshot           `json:"native"`
	Mode                    NetworkMode              `json:"mode,omitempty"`
	Bond                    *BondTopology            `json:"bond,omitempty"`
	Gateway                 *GatewayState            `json:"gateway,omitempty"`
}
