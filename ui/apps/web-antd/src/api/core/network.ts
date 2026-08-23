import { requestClient } from '#/api/request';

export namespace NetworkApi {
  export type PlatformType = 'linux' | 'darwin' | 'fake';
  export type StateStatus =
    | 'ready'
    | 'degraded'
    | 'ownership_conflict'
    | 'recovery_failed'
    | 'unsupported';
  export type InterfaceType = 'ethernet' | 'wifi' | 'other';
  export type LinkStatus = 'up' | 'down' | 'unknown';
  export type OwnershipStatus =
    | 'managed'
    | 'unproven'
    | 'conflict'
    | 'unsupported';
  export type IPMode = 'dhcp' | 'static' | 'unknown' | 'unsupported';
  export type IPStatus =
    | 'effective'
    | 'unavailable'
    | 'conflict'
    | 'unsupported';
  export type TransactionStatus =
    | 'applying'
    | 'pending_confirmation'
    | 'committing'
    | 'rolling_back'
    | 'confirmed'
    | 'rolled_back'
    | 'recovery_failed';
  export type TransactionAction = 'apply' | 'factory_reset';

  export interface IPv4State {
    mode: IPMode;
    address: string | null;
    prefix: number | null;
    subnetMask: string | null;
    gateway: string | null;
    dnsServers: string[];
    status: IPStatus;
  }

  export interface InterfaceInfo {
    id: string;
    name: string;
    displayName: string;
    type: InterfaceType;
    mac: string | null;
    linkStatus: LinkStatus;
    ownership: OwnershipStatus;
    writable: boolean;
    isPrimary: boolean;
    ipv4: IPv4State;
  }

  export interface ReconnectAddress {
    interfaceId: string;
    address: string;
    prefix: number;
  }

  export interface CandidateSummary {
    mode: IPMode;
    address?: string | null;
    prefix?: number | null;
    subnetMask?: string | null;
    gateway?: string | null;
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
    previousPrimaryInterfaceId: string | null;
    candidatePrimaryInterfaceId: string | null;
    reconnectAddresses: ReconnectAddress[];
    requiresReconnect: boolean;
    candidate: CandidateSummary;
  }

  export interface Capabilities {
    dhcp: boolean;
    staticIpv4: boolean;
    factoryReset: boolean;
    wifiAssociation: boolean;
  }

  export interface NetworkOverview {
    platform: PlatformType;
    state: StateStatus;
    primaryInterfaceId: string | null;
    defaultRouteInterfaceId: string | null;
    systemDnsServers: string[];
    interfaces: InterfaceInfo[];
    pendingTransaction: PendingTransaction | null;
    capabilities: Capabilities;
  }

  export interface TransactionResult {
    transactionId: string;
    status: TransactionStatus;
    expiresAt?: string;
    overview?: NetworkOverview;
    reconnectAddresses?: ReconnectAddress[];
    reason?: string | null;
  }

  export interface ApplyInterfaceParams {
    mode: IPMode;
    primary: boolean;
    address?: string;
    prefix?: number;
    gateway?: string;
    dnsServers?: string[];
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
