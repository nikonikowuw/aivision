import { requestClient } from '#/api/request';

export namespace PlateObservationApi {
  export interface PlateObservationItem {
    id: number;
    eventId: string;
    taskId: string;
    instanceId: string;
    cameraId: string;
    cameraName: string;
    algorithmId: string;
    algorithmVersion: string;
    timeSynced: boolean;
    plateText: string;
    normalizedText: string;
    plateColor: string;
    plateType: string;
    confidence: number;
    ocrConfidence: number;
    trackId: number;
    bbox: [number, number, number, number]; // [x_min, y_min, x_max, y_max]
    vehicleBbox: [number, number, number, number]; // [x_min, y_min, x_max, y_max]
    panoramaImageUrl: string;
    plateImageUrl: string;
    imageId: string;
    imageRelPath: string;
    plateImageId: string;
    plateImageRelPath: string;
    observedAt: string;
    createdAt: string;
  }

  export interface PlateObservationListQuery {
    page?: number;
    pageSize?: number;
    startTime?: string;
    endTime?: string;
    cameraId?: string;
    plateText?: string;
    plateColor?: string;
    plateType?: string;
    minConfidence?: number;
    maxConfidence?: number;
    minOcrConfidence?: number;
  }

  export interface PlateObservationPageResult {
    items: PlateObservationItem[];
    total: number;
  }
}

/**
 * 分页获取车牌抓拍过车记录列表
 */
export async function getPlateObservationListApi(
  params?: PlateObservationApi.PlateObservationListQuery,
) {
  return requestClient.get<PlateObservationApi.PlateObservationPageResult>(
    '/record/plates',
    { params },
  );
}

/**
 * 获取单条车牌过车详情
 */
export async function getPlateObservationDetailApi(id: number) {
  return requestClient.get<PlateObservationApi.PlateObservationItem>(
    `/record/plates/${id}`,
  );
}
