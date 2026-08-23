package netconfig

import (
	"time"
)

// PlatformType 运行平台类型。
type PlatformType string

const (
	PlatformLinux  PlatformType = "linux"
	PlatformDarwin PlatformType = "darwin"
	PlatformFake   PlatformType = "fake"
)

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
	OwnershipConflict   OwnershipStatus = "conflict"
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
)

// Capabilities 平台能力集。
type Capabilities struct {
	DHCP            bool `json:"dhcp"`
	StaticIPv4      bool `json:"staticIpv4"`
	FactoryReset    bool `json:"factoryReset"`
	WifiAssociation bool `json:"wifiAssociation"`
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
	IPv4        IPv4State       `json:"ipv4"`
	Fingerprint string          `json:"-"`
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
}

// TransactionResult 事务操作执行结果。
type TransactionResult struct {
	TransactionID      string              `json:"transactionId"`
	Status             TransactionStatus   `json:"status"`
	ExpiresAt          *time.Time          `json:"expiresAt,omitempty"`
	Overview           *NetworkOverview    `json:"overview,omitempty"`
	ReconnectAddresses []ReconnectAddress  `json:"reconnectAddresses"`
	Reason             *string             `json:"reason"`
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
}
