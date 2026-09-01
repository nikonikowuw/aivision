import { requestClient } from '#/api/request';

export namespace PersonApi {
  /** 人员公开传输对象（严禁包含内部数据库 id） */
  export interface PersonItem {
    personId: string;
    name: string;
    primaryFaceId?: string;
    faceCount?: number;
    createdAt: string;
    updatedAt: string;
  }

  export interface FaceBoundingBox {
    x: number;
    y: number;
    width: number;
    height: number;
  }

  export interface PersonFaceItem {
    faceId: string;
    algorithmId: string;
    algorithmVersion: string;
    qualityScore: number;
    detectionScore: number;
    boundingBox?: FaceBoundingBox;
    rawImageSize: number;
    rawImageMime: string;
    alignedFaceSize: number;
    alignedFaceMime: string;
    isPrimary?: boolean;
    createdAt: string;
  }

  export interface PersonPageQuery {
    page?: number;
    pageSize?: number;
    personId?: string;
    name?: string;
  }

  export interface PersonPageResult {
    items: PersonItem[];
    total: number;
  }

  export interface CreatePersonInput {
    personId?: string;
    name: string;
  }

  export interface UpdatePersonInput {
    name: string;
  }

  export interface BatchDeletePersonInput {
    personIds: string[];
  }
}

/**
 * 获取人员分页列表
 */
export async function getPersonPageApi(params?: PersonApi.PersonPageQuery) {
  return requestClient.get<PersonApi.PersonPageResult>('/person/page', {
    params,
  });
}

/**
 * 创建人员
 */
export async function createPersonApi(data: PersonApi.CreatePersonInput) {
  return requestClient.post<PersonApi.PersonItem>('/person', data);
}

/**
 * 更新人员姓名
 */
export async function updatePersonApi(
  personId: string,
  data: PersonApi.UpdatePersonInput,
) {
  return requestClient.put<PersonApi.PersonItem>(`/person/${personId}`, data);
}

/**
 * 删除人员
 */
export async function deletePersonApi(personId: string) {
  return requestClient.delete<null>(`/person/${personId}`);
}

/**
 * 批量删除人员
 */
export async function batchDeletePersonApi(
  data: PersonApi.BatchDeletePersonInput,
) {
  return requestClient.delete<null>('/person/batch', { data });
}

/**
 * 查询人员的人脸样本列表
 */
export async function listPersonFacesApi(personId: string) {
  return requestClient.get<PersonApi.PersonFaceItem[]>(
    `/person/${personId}/faces`,
  );
}

/**
 * 上传并注册单张人脸样本
 */
export async function registerPersonFaceApi(personId: string, file: File) {
  return requestClient.upload<PersonApi.PersonFaceItem>(
    `/person/${personId}/faces`,
    { file },
  );
}

/**
 * 删除单个人脸样本
 */
export async function deletePersonFaceApi(personId: string, faceId: string) {
  return requestClient.delete<null>(`/person/${personId}/faces/${faceId}`);
}

/**
 * 设置人员主图样本
 */
export async function setPrimaryFaceApi(personId: string, faceId: string) {
  return requestClient.put<null>(`/person/${personId}/primary-face`, {
    faceId,
  });
}

/**
 * 获取人脸样本图片 URL 路径
 */
export function getPersonFaceImageUrl(
  personId: string,
  faceId: string,
  type: 'aligned' | 'raw' = 'aligned',
): string {
  const endpoint = type === 'raw' ? 'image' : 'aligned-image';
  return `/api/person/${personId}/faces/${faceId}/${endpoint}`;
}
