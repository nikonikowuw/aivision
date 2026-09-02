import { requestClient } from '#/api/request';

export namespace StorageApi {
  export interface StorageStatus {
    totalBytes: number;
    usedBytes: number;
    freeBytes: number;
    usagePercent: number;
    alarmRecordCount: number;
    plateObservationCount: number;
    faceObservationCount: number;
    faceCaptureCount: number;
    operationLogCount: number;
    status: 'cleaning' | 'degraded' | 'normal';
    circuitBreakerActive: boolean;
    lastCleanupAt?: null | string;
    lastFreedBytes: number;
  }

  export interface StorageRetentionConfig {
    retentionDays: number;
    highWatermarkPercent: number;
    lowWatermarkPercent: number;
    checkIntervalSeconds: number;
    autoCleanupEnabled: boolean;
  }

  export interface UpdateStorageConfigInput {
    retentionDays: number;
    highWatermarkPercent: number;
    lowWatermarkPercent: number;
    checkIntervalSeconds: number;
    autoCleanupEnabled: boolean;
  }
}

/**
 * 获取物理磁盘容量、各业务表记录数及清理状态 (GET /api/storage/status)
 */
export async function getStorageStatusApi() {
  return requestClient.get<StorageApi.StorageStatus>('/storage/status');
}

/**
 * 获取存储保留天数与高低水位配置 (GET /api/storage/config)
 */
export async function getStorageConfigApi() {
  return requestClient.get<StorageApi.StorageRetentionConfig>(
    '/storage/config',
  );
}

/**
 * 更新存储保留策略配置 (PUT /api/storage/config)
 */
export async function updateStorageConfigApi(
  data: StorageApi.UpdateStorageConfigInput,
) {
  return requestClient.put<StorageApi.StorageRetentionConfig>(
    '/storage/config',
    data,
  );
}

/**
 * 手动触发一次存储巡检与清理 (POST /api/storage/cleanup)
 */
export async function triggerStorageCleanupApi() {
  return requestClient.post<null>('/storage/cleanup');
}
