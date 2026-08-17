import type { SystemStatus } from '#/constants/system';

import { requestClient } from '#/api/request';

export namespace DeptApi {
  export interface DeptItem {
    id: number;
    parentId: number;
    name: string;
    sort: number;
    leader?: string;
    phone?: string;
    status: SystemStatus;
    children?: DeptItem[];
    createdAt?: string;
    updatedAt?: string;
  }

  export interface SaveDeptInput {
    parentId: number;
    name: string;
    sort?: number;
    leader?: string;
    phone?: string;
    status?: SystemStatus;
  }
}

/**
 * 获取全量部门树
 */
export async function getDeptTreeApi() {
  return requestClient.get<DeptApi.DeptItem[]>('/dept/tree');
}

/**
 * 创建部门
 */
export async function createDeptApi(data: DeptApi.SaveDeptInput) {
  return requestClient.post<DeptApi.DeptItem>('/dept', data);
}

/**
 * 更新部门
 */
export async function updateDeptApi(id: number, data: DeptApi.SaveDeptInput) {
  return requestClient.put<DeptApi.DeptItem>(`/dept/${id}`, data);
}

/**
 * 删除部门
 */
export async function deleteDeptApi(id: number) {
  return requestClient.delete<null>(`/dept/${id}`);
}
