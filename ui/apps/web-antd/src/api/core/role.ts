import type { SystemStatus } from '#/constants/system';

import { requestClient } from '#/api/request';

export namespace RoleApi {
  export interface RoleItem {
    id: number;
    name: string;
    code: string;
    status: SystemStatus;
    sort: number;
    remark?: string;
    createdAt?: string;
    updatedAt?: string;
  }

  export interface RolePageQuery {
    page?: number;
    pageSize?: number;
    name?: string;
    code?: string;
    status?: SystemStatus;
  }

  export interface RolePageResult {
    items: RoleItem[];
    total: number;
  }

  export interface SaveRoleInput {
    name: string;
    code: string;
    status?: SystemStatus;
    sort?: number;
    remark?: string;
  }
}

/**
 * 获取角色分页列表
 */
export async function getRolePageApi(params?: RoleApi.RolePageQuery) {
  return requestClient.get<RoleApi.RolePageResult>('/role/page', { params });
}

/**
 * 创建角色
 */
export async function createRoleApi(data: RoleApi.SaveRoleInput) {
  return requestClient.post<RoleApi.RoleItem>('/role', data);
}

/**
 * 更新角色
 */
export async function updateRoleApi(id: number, data: RoleApi.SaveRoleInput) {
  return requestClient.put<RoleApi.RoleItem>(`/role/${id}`, data);
}

/**
 * 删除角色
 */
export async function deleteRoleApi(id: number) {
  return requestClient.delete<null>(`/role/${id}`);
}

/**
 * 批量删除角色
 */
export async function batchDeleteRoleApi(ids: number[]) {
  return requestClient.delete<null>('/role/batch', { data: { ids } });
}

/**
 * 获取角色关联的菜单 ID 列表
 */
export async function getRoleMenuIdsApi(id: number) {
  return requestClient.get<number[]>(`/role/${id}/menu-ids`);
}

/**
 * 分配角色菜单权限
 */
export async function assignRoleMenusApi(id: number, menuIds: number[]) {
  return requestClient.put<null>(`/role/${id}/menus`, { menuIds });
}
