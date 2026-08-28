import { requestClient } from '#/api/request';

export namespace PersonApi {
  /** 人员公开传输对象（严禁包含内部数据库 id） */
  export interface PersonItem {
    personId: string;
    name: string;
    createdAt: string;
    updatedAt: string;
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
