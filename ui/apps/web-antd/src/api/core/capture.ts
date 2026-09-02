import { requestClient } from '#/api/request';

export namespace FaceCaptureApi {
  export interface FaceCaptureSnapshotItem {
    snapshotIndex: number;
    wallTimeNs: number;
    timeSynced: boolean;
    bbox?: [number, number, number, number];
    faceBbox?: [number, number, number, number];
    qualityScore: number;
    similarity: number;
    faceId?: string;
    personId?: string;
    personName?: string;
    panoramaImageUrl: string;
    faceImageUrl: string;
    imageId: string;
    imageRelPath: string;
    faceImageId: string;
    faceImageRelPath: string;
    observedAt?: string;
    capturedAt: string;
  }

  export interface FaceCaptureItem {
    id: number;
    eventId: string;
    instanceId: string;
    cameraId: string;
    cameraName: string;
    algorithmId: string;
    algorithmVersion: string;
    trackId: number;
    snapshotCount: number;
    snapshots?: FaceCaptureSnapshotItem[];
    bestQualityScore: number;
    bestSimilarity: number;
    bestPersonId?: string;
    bestPersonName?: string;
    bestBbox?: [number, number, number, number];
    bestFaceCropUrl: string;
    bestPanoramaUrl: string;
    panoramaImageUrl?: string;
    faceImageUrl?: string;
    bestImageId: string;
    bestImageRelPath: string;
    bestFaceImageId: string;
    bestFaceImageRelPath: string;
    isStranger: boolean;
    firstObservedAt: string;
    lastObservedAt: string;
    createdAt: string;
    updatedAt?: string;
  }

  export interface FaceCaptureListQuery {
    page?: number;
    pageSize?: number;
    startTime?: string;
    endTime?: string;
    cameraId?: string;
    trackId?: number;
    personName?: string;
    personId?: string;
    status?: string; // "all", "recognized", "stranger"
    isStranger?: boolean;
    minSimilarity?: number;
    maxSimilarity?: number;
  }

  export interface FaceCapturePageResult {
    items: FaceCaptureItem[];
    total: number;
  }
}

/**
 * 分页获取人脸抓拍记录（时序演进聚合记录）
 */
export async function getFaceCaptureListApi(
  params?: FaceCaptureApi.FaceCaptureListQuery,
) {
  const queryParams = { ...params };
  if (queryParams.status === undefined) {
    if (queryParams.isStranger === true) {
      queryParams.status = 'stranger';
    } else if (queryParams.isStranger === false) {
      queryParams.status = 'recognized';
    }
  }
  return requestClient.get<FaceCaptureApi.FaceCapturePageResult>(
    '/record/captures',
    { params: queryParams },
  );
}

/**
 * 获取单条人脸抓拍时序演进记录详情
 */
export async function getFaceCaptureDetailApi(id: number) {
  return requestClient.get<FaceCaptureApi.FaceCaptureItem>(
    `/record/captures/${id}`,
  );
}
