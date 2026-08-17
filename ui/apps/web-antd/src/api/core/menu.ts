import type { RouteRecordStringComponent } from '@vben/types';

import type { SystemStatus } from '#/constants/system';

import { requestClient } from '#/api/request';

export namespace MenuApi {
  export interface MenuItem {
    id: number;
    parentId: number;
    type: 'button' | 'catalog' | 'menu';
    name: string;
    title: string;
    path?: string;
    component?: string;
    icon?: string;
    permission?: string;
    sort: number;
    status: SystemStatus;
    affix?: boolean;
    keepAlive?: boolean;
    homePath?: string;
    children?: MenuItem[];
    createdAt?: string;
    updatedAt?: string;
  }

  export interface SaveMenuInput {
    parentId: number;
    type: 'button' | 'catalog' | 'menu';
    name: string;
    title: string;
    path?: string;
    component?: string;
    icon?: string;
    permission?: string;
    sort?: number;
    status?: SystemStatus;
    affix?: boolean;
    keepAlive?: boolean;
    homePath?: string;
  }
}

/**
 * 获取当前用户有权访问的动态路由树
 */
export async function getAllMenusApi() {
  return requestClient.get<RouteRecordStringComponent[]>('/menu/all');
}

/**
 * 获取全量菜单树（用于菜单管理、权限分配）
 */
export async function getMenuTreeApi() {
  return requestClient.get<MenuApi.MenuItem[]>('/menu/tree');
}

/**
 * 创建菜单
 */
export async function createMenuApi(data: MenuApi.SaveMenuInput) {
  return requestClient.post<MenuApi.MenuItem>('/menu', data);
}

/**
 * 更新菜单
 */
export async function updateMenuApi(id: number, data: MenuApi.SaveMenuInput) {
  return requestClient.put<MenuApi.MenuItem>(`/menu/${id}`, data);
}

/**
 * 删除菜单
 */
export async function deleteMenuApi(id: number) {
  return requestClient.delete<null>(`/menu/${id}`);
}
