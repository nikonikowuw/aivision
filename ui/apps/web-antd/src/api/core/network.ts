import { requestClient } from '#/api/request';

// 网络工作模式（与后端 netconfig.NetworkMode 常量逐字一致）
export const NETWORK_MODES = {
  MultiAddress: 'multi-address',
  ActiveBackup: 'active-backup',
  LACPAggregation: 'lacp-aggregation',
  Gateway: 'gateway',
} as const;

export namespace NetworkApi {
  export type PlatformType = 'darwin' | 'fake' | 'linux';
  export type StateStatus =
    | 'degraded'
    | 'ownership_conflict'
    | 'ready'
    | 'recovery_failed'
    | 'unsupported';
  export type InterfaceType = 'ethernet' | 'other' | 'wifi';
  export type LinkStatus = 'down' | 'unknown' | 'up';
  export type OwnershipStatus =
    | 'conflict'
    | 'managed'
    | 'unproven'
    | 'unsupported';
  export type IPMode = 'dhcp' | 'static' | 'unknown' | 'unsupported';
  export type IPStatus =
    | 'conflict'
    | 'effective'
    | 'unavailable'
    | 'unsupported';
  export type TransactionStatus =
    | 'applying'
    | 'committing'
    | 'confirmed'
    | 'pending_confirmation'
    | 'recovery_failed'
    | 'rolled_back'
    | 'rolling_back';
  export type TransactionAction = 'apply' | 'factory_reset' | 'mode_switch';
  export type NetworkMode = (typeof NETWORK_MODES)[keyof typeof NETWORK_MODES];

  export type BondXmitHashPolicy = 'layer2' | 'layer2+3' | 'layer3+4';
  export type InterfaceDuplex = 'full' | 'half' | 'unknown';

  export interface LACPPortState {
    active: boolean;
    shortTimeout: boolean;
    aggregation: boolean;
    synchronized: boolean;
    collecting: boolean;
    distributing: boolean;
    defaulted: boolean;
    expired: boolean;
  }

  export interface LACPPortStatus {
    interfaceId: string;
    aggregatorId?: number;
    inAggregator: boolean;
    actorState: LACPPortState;
    partnerState: LACPPortState;
  }

  export interface LACPStatus {
    aggregatorId?: number;
    negotiated: boolean;
    slaves: LACPPortStatus[];
    diagnosticCode?: string;
  }

  export interface BondTopology {
    bondInterfaceId: string;
    slaveIds: string[];
    primarySlaveId?: string;
    activeSlaveId?: null | string;
    miimon?: number;
    xmitHashPolicy?: BondXmitHashPolicy;
    lacp?: LACPStatus;
  }

  export interface IPv4State {
    mode: IPMode;
    address: null | string;
    prefix: null | number;
    subnetMask: null | string;
    gateway: null | string;
    dnsServers: string[];
    status: IPStatus;
  }

  export interface InterfaceInfo {
    id: string;
    name: string;
    displayName: string;
    type: InterfaceType;
    mac: null | string;
    linkStatus: LinkStatus;
    ownership: OwnershipStatus;
    writable: boolean;
    isPrimary: boolean;
    isBond: boolean;
    masterId: null | string;
    speedMbps?: number;
    duplex?: InterfaceDuplex;
    ipv4: IPv4State;
  }

  export interface NetworkWarning {
    code: 'bond_slave_link_mismatch';
    interfaceIds: string[];
  }

  export interface ReconnectAddress {
    interfaceId: string;
    address: string;
    prefix: number;
  }

  export interface CandidateSummary {
    mode: IPMode;
    address?: null | string;
    prefix?: null | number;
    subnetMask?: null | string;
    gateway?: null | string;
    dnsServers?: string[];
  }

  export interface PendingTransaction {
    id: string;
    status: TransactionStatus;
    action: TransactionAction;
    createdAt: string;
    expiresAt: string;
    remainingSeconds: number;
    targetInterfaceId: string;
    previousPrimaryInterfaceId: null | string;
    candidatePrimaryInterfaceId: null | string;
    reconnectAddresses: ReconnectAddress[];
    requiresReconnect: boolean;
    candidate: CandidateSummary;
    targetMode?: NetworkMode;
    previousMode?: NetworkMode;
    warnings?: NetworkWarning[];
  }

  export interface Capabilities {
    dhcp: boolean;
    staticIpv4: boolean;
    factoryReset: boolean;
    wifiAssociation: boolean;
    supportedModes: NetworkMode[];
  }

  export interface GatewayLease {
    mac: string;
    ip: string;
    startsAt: string;
    expiresAt: string;
    lastRenewedAt: string;
    hostname?: string;
  }

  export interface GatewayOverview {
    downstreamInterfaceId: string;
    poolStart: string;
    poolEnd: string;
    prefix: number;
    leaseDurationSeconds: number;
    ipForward: boolean;
    running: boolean;
    conflictDetected: boolean;
    leases: GatewayLease[];
  }

  export interface NetworkOverview {
    platform: PlatformType;
    state: StateStatus;
    primaryInterfaceId: null | string;
    defaultRouteInterfaceId: null | string;
    systemDnsServers: string[];
    interfaces: InterfaceInfo[];
    pendingTransaction: null | PendingTransaction;
    capabilities: Capabilities;
    mode: NetworkMode;
    bond: BondTopology | null;
    gateway: GatewayOverview | null;
  }

  export interface TransactionResult {
    transactionId: string;
    status: TransactionStatus;
    expiresAt?: string;
    overview?: NetworkOverview;
    reconnectAddresses?: ReconnectAddress[];
    warnings?: NetworkWarning[];
    reason?: null | string;
  }

  export interface ApplyInterfaceParams {
    mode: IPMode;
    primary: boolean;
    address?: string;
    prefix?: number;
    gateway?: string;
    dnsServers?: string[];
  }

  export interface BondParams {
    slaveIds: string[];
    primarySlaveId?: string;
    xmitHashPolicy?: BondXmitHashPolicy;
    ipv4: ApplyInterfaceParams;
  }

  export interface GatewayParams {
    downstreamInterfaceId: string;
    poolStart: string;
    poolEnd: string;
    prefix: number;
    leaseDurationSeconds: number;
    ipForward: boolean;
  }

  export interface SwitchModeParams {
    mode: NetworkMode;
    bond?: BondParams;
    gateway?: GatewayParams;
  }
}

/**
 * 获取网络概览及所有接口状态
 */
export async function getNetworkOverviewApi() {
  return requestClient.get<NetworkApi.NetworkOverview>('/network');
}

/**
 * 查询当前候选事务
 */
export async function getNetworkTransactionApi(transactionId: string) {
  return requestClient.get<NetworkApi.PendingTransaction>(
    `/network/transactions/${transactionId}`,
  );
}

/**
 * 应用接口候选配置
 */
export async function applyInterfaceApi(
  interfaceId: string,
  data: NetworkApi.ApplyInterfaceParams,
) {
  return requestClient.put<NetworkApi.TransactionResult>(
    `/network/interfaces/${interfaceId}`,
    data,
  );
}

/**
 * 确认候选事务
 */
export async function confirmNetworkTransactionApi(transactionId: string) {
  return requestClient.post<NetworkApi.TransactionResult>(
    `/network/transactions/${transactionId}/confirm`,
  );
}

/**
 * 取消候选事务
 */
export async function cancelNetworkTransactionApi(transactionId: string) {
  return requestClient.post<NetworkApi.TransactionResult>(
    `/network/transactions/${transactionId}/cancel`,
  );
}

/**
 * 恢复出厂基线
 */
export async function factoryResetInterfaceApi(interfaceId: string) {
  return requestClient.post<NetworkApi.TransactionResult>(
    `/network/interfaces/${interfaceId}/factory-reset`,
  );
}

/**
 * 切换整机网络工作模式
 */
export async function switchNetworkModeApi(data: NetworkApi.SwitchModeParams) {
  return requestClient.put<NetworkApi.TransactionResult>('/network/mode', data);
}
