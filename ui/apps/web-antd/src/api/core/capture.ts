import { requestClient } from '#/api/request';

export namespace CaptureApi {
  export type TargetType =
    | 'all'
    | 'face'
    | 'generic'
    | 'non_motor'
    | 'person'
    | 'vehicle';

  export interface CaptureItem {
    id: number;
    eventId: string;
    instanceId: string;
    targetType: Exclude<TargetType, 'all'>;
    cameraId: string;
    cameraName: string;
    taskId: number;
    algorithmId: string;
    algorithmVersion: string;
    trackId: number;
    confidence: number;
    qualityScore: number;
    bbox?: [number, number, number, number];
    subBbox?: [number, number, number, number];
    imageId: string;
    imageRelPath: string;
    cropImageId: string;
    cropImageRelPath: string;
    subCropImageId: string;
    subCropImageRelPath: string;
    imageUrl?: string;
    cropImageUrl?: string;
    subCropImageUrl?: string;
    isRecognized: boolean;
    attributes: Record<string, unknown>;
    capturedAt: string;
    createdAt: string;
    updatedAt: string;
  }

  export interface CaptureListQuery {
    page?: number;
    pageSize?: number;
    startTime?: string;
    endTime?: string;
    targetType?: TargetType;
    cameraId?: string;
    trackId?: number;
    keyword?: string;
    isRecognized?: boolean;
    minQuality?: number;
    maxQuality?: number;
    minConfidence?: number;
    maxConfidence?: number;
  }

  export interface CapturePageResult {
    items: CaptureItem[];
    total: number;
  }
}

/** 分页获取通用抓拍事件记录。 */
export async function getCaptureListApi(params?: CaptureApi.CaptureListQuery) {
  return requestClient.get<CaptureApi.CapturePageResult>('/record/captures', {
    params,
  });
}

/** 获取单条通用抓拍事件详情。 */
export async function getCaptureDetailApi(id: number) {
  return requestClient.get<CaptureApi.CaptureItem>(`/record/captures/${id}`);
}

export namespace FaceCaptureApi {
  export interface FaceCandidate {
    faceId: string;
    personId: string;
    personName: string;
    similarity: number;
  }

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
    candidates?: FaceCandidate[];
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
    bestCandidates?: FaceCandidate[];
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
    '/v1/record/captures',
    { params: queryParams },
  );
}

/**
 * 获取单条人脸抓拍时序演进记录详情
 */
export async function getFaceCaptureDetailApi(id: number) {
  return requestClient.get<FaceCaptureApi.FaceCaptureItem>(
    `/v1/record/captures/${id}`,
  );
}
