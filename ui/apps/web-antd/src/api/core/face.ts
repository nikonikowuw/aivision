import { requestClient } from '#/api/request';

export namespace FaceObservationApi {
  export interface FaceCandidate {
    faceId: string;
    personId: string;
    personName: string;
    similarity: number;
  }

  export interface FaceObservationItem {
    id: number;
    eventId: string;
    instanceId: string;
    cameraId: string;
    cameraName: string;
    algorithmId: string;
    algorithmVersion: string;
    timeSynced: boolean;
    trackId: number;
    faceId: string;
    personId: string;
    personName: string;
    similarity: number;
    candidates?: FaceCandidate[];
    bbox: [number, number, number, number];
    panoramaImageUrl: string;
    faceImageUrl: string;
    imageId: string;
    imageRelPath: string;
    faceImageId: string;
    faceImageRelPath: string;
    observedAt: string;
    createdAt: string;
  }

  export interface FaceObservationListQuery {
    page?: number;
    pageSize?: number;
    startTime?: string;
    endTime?: string;
    cameraId?: string;
    personId?: string;
    personName?: string;
    minSimilarity?: number;
    maxSimilarity?: number;
  }

  export interface FaceObservationPageResult {
    items: FaceObservationItem[];
    total: number;
  }
}

/**
 * 分页获取人脸识别记录
 */
export async function getFaceObservationListApi(
  params?: FaceObservationApi.FaceObservationListQuery,
) {
  return requestClient.get<FaceObservationApi.FaceObservationPageResult>(
    '/record/faces',
    { params },
  );
}

/**
 * 获取单条人脸识别记录详情
 */
export async function getFaceObservationDetailApi(id: number) {
  return requestClient.get<FaceObservationApi.FaceObservationItem>(
    `/record/faces/${id}`,
  );
}
