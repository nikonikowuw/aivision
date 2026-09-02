import { requestClient } from '#/api/request';

export namespace AlarmRecordApi {
  export interface DetectionPoint {
    x: number;
    y: number;
  }

  export interface DetectionRule {
    role: number;
    lineDirection: number;
    points: DetectionPoint[];
  }

  export interface AlarmRecordItem {
    id: number;
    eventId: string;
    instanceId: string;
    cameraId: string;
    cameraName: string;
    algorithmId: string;
    algorithmName: string;
    algorithmVersion: string;
    alarmTypeId: string;
    occurredAt: string;
    timeSynced: boolean;
    targetLabel: string;
    confidence: number;
    trackId: number;
    bbox: [number, number, number, number]; // [x1, y1, x2, y2]
    imageId: string;
    imageUrl: string;
    createdAt: string;
  }

  export interface AlarmRecordDetail extends AlarmRecordItem {
    rules: DetectionRule[];
  }

  export interface AlarmRecordListQuery {
    page?: number;
    pageSize?: number;
    startTime?: string;
    endTime?: string;
    cameraId?: string;
    algorithmId?: string;
    alarmTypeId?: string;
    targetLabel?: string;
    minConfidence?: number;
    maxConfidence?: number;
  }

  export interface AlarmRecordPageResult {
    items: AlarmRecordItem[];
    total: number;
  }
}

/**
 * 分页获取告警记录列表
 */
export async function getAlarmRecordListApi(
  params?: AlarmRecordApi.AlarmRecordListQuery,
) {
  return requestClient.get<AlarmRecordApi.AlarmRecordPageResult>(
    '/record/alarms',
    { params },
  );
}

/**
 * 获取指定告警记录详情
 */
export async function getAlarmRecordDetailApi(id: number | string) {
  return requestClient.get<AlarmRecordApi.AlarmRecordDetail>(
    `/record/alarms/${id}`,
  );
}

// 单例内存缓存（防同图重复下载）
const imageBlobCache = new Map<string, Promise<Blob | null>>();

/**
 * 获取受控告警图片 Blob（带 Authorization header、单例内存缓存）
 * @param imageId 图片唯一 ID
 * @param isThumbnail 是否请求硬件低带宽缩略图 (默认 true 供列表秒开)
 */
export async function getAlarmImageBlobApi(
  imageId: string,
  isThumbnail: boolean = true,
): Promise<Blob | null> {
  if (!imageId) return null;
  const cacheKey = isThumbnail ? `${imageId}:thumb` : `${imageId}:full`;
  if (imageBlobCache.has(cacheKey)) {
    return (await imageBlobCache.get(cacheKey)) || null;
  }
  const url = isThumbnail
    ? `/record/images/${imageId}?type=thumb`
    : `/record/images/${imageId}`;
  const promise = requestClient.download<Blob>(url).catch((err) => {
    imageBlobCache.delete(cacheKey);
    throw err;
  });
  imageBlobCache.set(cacheKey, promise);
  return await promise;
}
