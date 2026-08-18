import { requestClient } from '#/api/request';

export namespace LogApi {
  export interface LogItem {
    id: number;
    userId?: number;
    username: string;
    module: string;
    action: string;
    method: string;
    path: string;
    query?: string;
    body?: string;
    statusCode: number;
    durationMs: number;
    ip: string;
    userAgent: string;
    createdAt: string;
  }

  export interface LogPageQuery {
    page?: number;
    pageSize?: number;
    username?: string;
    module?: string;
    statusCode?: number;
    startTime?: string;
    endTime?: string;
  }

  export interface LogPageResult {
    items: LogItem[];
    total: number;
  }
}

/**
 * 获取操作日志分页列表
 */
export async function getLogPageApi(params?: LogApi.LogPageQuery) {
  return requestClient.get<LogApi.LogPageResult>('/oplog/page', { params });
}

/**
 * 获取操作日志详情
 */
export async function getLogDetailApi(id: number) {
  return requestClient.get<LogApi.LogItem>(`/oplog/${id}`);
}

/**
 * 删除单条操作日志
 */
export async function deleteLogApi(id: number) {
  return requestClient.delete<null>(`/oplog/${id}`);
}

/**
 * 批量删除操作日志
 */
export async function batchDeleteLogApi(ids: number[]) {
  return requestClient.delete<null>('/oplog/batch', { data: { ids } });
}
