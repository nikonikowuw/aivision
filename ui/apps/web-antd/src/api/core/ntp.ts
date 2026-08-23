import { requestClient } from '#/api/request';

export namespace NTPApi {
  export interface NTPConfig {
    id: number;
    mode: 'manual' | 'ntp';
    servers: string[];
    createdAt: string;
    updatedAt: string;
  }

  export interface UpdateNTPConfigInput {
    mode: 'manual' | 'ntp';
    servers: string[];
  }

  export interface SyncStatus {
    synced: boolean;
    source: string;
    offset: string;
    lastSyncTime: null | string;
  }

  export interface SetTimeInput {
    time: string; // RFC3339 格式时间字符串
  }

  export interface SyncedResult {
    synced: boolean;
  }
}

/**
 * 获取当前对时配置
 */
export async function getNTPConfigApi() {
  return requestClient.get<NTPApi.NTPConfig>('/ntp/config');
}

/**
 * 更新对时配置
 */
export async function updateNTPConfigApi(data: NTPApi.UpdateNTPConfigInput) {
  return requestClient.put<null>('/ntp/config', data);
}

/**
 * 实时获取时钟同步状态
 */
export async function getNTPStatusApi() {
  return requestClient.get<NTPApi.SyncStatus>('/ntp/status');
}

/**
 * 触发立即同步（NTP 模式）
 */
export async function syncNTPNowApi() {
  return requestClient.post<null>('/ntp/sync');
}

/**
 * 手动设置系统时间（手动模式）
 */
export async function setNTPTimeApi(data: NTPApi.SetTimeInput) {
  return requestClient.post<null>('/ntp/set-time', data);
}

/**
 * 内部时钟同步状态查询
 */
export async function isTimeSyncedApi() {
  return requestClient.get<NTPApi.SyncedResult>('/ntp/synced');
}
