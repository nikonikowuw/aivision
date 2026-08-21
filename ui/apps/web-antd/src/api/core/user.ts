import type { UserInfo } from '@vben/types';

import type { SystemStatus } from '#/constants/system';

import { requestClient } from '#/api/request';

export namespace UserApi {
  export interface UserItem {
    id: number;
    username: string;
    nickname?: string;
    email?: string;
    phone?: string;
    avatar?: string;
    deptId?: number;
    deptName?: string;
    status: SystemStatus;
    remark?: string;
    createdAt?: string;
    updatedAt?: string;
  }

  export interface UserPageQuery {
    page?: number;
    pageSize?: number;
    username?: string;
    nickname?: string;
    status?: SystemStatus;
    deptId?: number;
  }

  export interface UserPageResult {
    items: UserItem[];
    total: number;
  }

  export interface SaveUserInput {
    username: string;
    password?: string;
    nickname?: string;
    email?: string;
    phone?: string;
    status?: SystemStatus;
    remark?: string;
    deptId?: number;
  }

  export interface CurrentProfile {
    username: string;
    nickname: string;
    email: string;
    phone: string;
    avatar: string;
    remark: string;
  }

  export interface UpdateCurrentProfileInput {
    nickname?: string;
    email?: string;
    phone?: string;
    avatar?: string;
    remark?: string;
  }

  export interface ChangeCurrentPasswordInput {
    oldPassword: string;
    newPassword: string;
  }
}

/**
 * 获取当前登录用户的个人资料
 */
export async function getCurrentProfileApi() {
  return requestClient.get<UserApi.CurrentProfile>('/user/profile');
}

/**
 * 更新当前登录用户的个人资料
 */
export async function updateCurrentProfileApi(
  data: UserApi.UpdateCurrentProfileInput,
) {
  return requestClient.put<UserApi.CurrentProfile>('/user/profile', data);
}

/**
 * 修改当前登录用户密码
 */
export async function changeCurrentPasswordApi(
  data: UserApi.ChangeCurrentPasswordInput,
) {
  return requestClient.put<null>('/user/profile/password', data);
}

/**
 * 获取当前登录用户信息
 */
export async function getUserInfoApi() {
  return requestClient.get<UserInfo>('/user/info');
}

/**
 * 获取用户分页列表
 */
export async function getUserPageApi(params?: UserApi.UserPageQuery) {
  return requestClient.get<UserApi.UserPageResult>('/user/page', { params });
}

/**
 * 创建用户
 */
export async function createUserApi(data: UserApi.SaveUserInput) {
  return requestClient.post<UserApi.UserItem>('/user', data);
}

/**
 * 更新用户
 */
export async function updateUserApi(id: number, data: UserApi.SaveUserInput) {
  return requestClient.put<UserApi.UserItem>(`/user/${id}`, data);
}

/**
 * 删除用户
 */
export async function deleteUserApi(id: number) {
  return requestClient.delete<null>(`/user/${id}`);
}

/**
 * 重置用户密码
 */
export async function resetUserPasswordApi(id: number) {
  return requestClient.put<null>(`/user/${id}/reset-password`);
}

/**
 * 获取用户关联的角色 ID 列表
 */
export async function getUserRolesApi(id: number) {
  return requestClient.get<number[]>(`/user/${id}/roles`);
}

/**
 * 分配用户角色
 */
export async function assignUserRolesApi(id: number, roleIds: number[]) {
  return requestClient.put<null>(`/user/${id}/roles`, { roleIds });
}

/**
 * 更新用户状态 (启用/停用)
 */
export async function updateUserStatusApi(id: number, status: SystemStatus) {
  return requestClient.put<null>(`/user/${id}/status`, { status });
}

/**
 * 批量删除用户
 */
export async function batchDeleteUserApi(ids: number[]) {
  return requestClient.delete<null>('/user/batch', { data: { ids } });
}

/**
 * 批量更新用户状态 (启用/停用)
 */
export async function batchUpdateUserStatusApi(
  ids: number[],
  status: SystemStatus,
) {
  return requestClient.put<null>('/user/batch-status', { ids, status });
}
