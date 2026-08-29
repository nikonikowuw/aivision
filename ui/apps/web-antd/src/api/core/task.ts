import { requestClient } from '#/api/request';

export namespace TaskApi {
  /** 任务运行状态码（数值对齐 proto TaskStatusCode） */
  export type TaskStatusCode = 0 | 1 | 2 | 3 | 4 | 5 | 6;

  export const TaskStatus = {
    UNSPECIFIED: 0,
    STARTING: 1,
    RUNNING: 2,
    DEGRADED: 3,
    RECONNECTING: 4,
    STOPPED: 5,
    ERROR: 6,
  } as const;

  /** 算法实例运行状态码（数值对齐 proto InstanceStatusCode） */
  export type InstanceStatusCode = 0 | 1 | 2 | 3 | 4 | 5;

  export const InstanceStatus = {
    UNSPECIFIED: 0,
    STARTING: 1,
    RUNNING: 2,
    DEGRADED: 3,
    STOPPED: 4,
    ERROR: 5,
  } as const;

  export interface DetectionPoint {
    x: number;
    y: number;
  }

  export interface DetectionRule {
    role: number;
    lineDirection: number;
    points: DetectionPoint[];
  }

  /** 任务列表页轻量级实例摘要（供 1:N 管道流水线与控制台展示） */
  export interface TaskInstanceBrief {
    instanceId: string;
    algorithmId: string;
    analysisFps: number;
    currentFps: null | number;
    enabled: boolean;
    actualStatus: number;
    statusMessage: string;
    rulesCount: number;
  }

  /** 分析任务项（库中配置 + 内存实时字段合并） */
  export interface TaskItem {
    cameraId: string;
    name: string;
    desiredEnabled: boolean;
    actualStatus: TaskStatusCode;
    statusMessage: string;
    lastFrameAt: null | string;
    reportedAt: null | string;
    instanceCount?: number;
    instances?: TaskInstanceBrief[];
  }

  export interface TaskListQuery {
    page?: number;
    pageSize?: number;
    cameraId?: string;
    name?: string;
    configured?: boolean;
  }

  export interface TaskPageResult {
    items: TaskItem[];
    total: number;
  }

  /** 任务管理概览统计（供页面顶部统计条展示） */
  export interface TaskStats {
    totalTasks: number;
    runningTasks: number;
    totalInstances: number;
    enabledInstances: number;
    usedUnits: number;
    /** 0 表示 Engine 未上报算力（旧版/离线），负载应展示为不可用 */
    totalUnits: number;
    reservedUnits: number;
    availableUnits: number;
  }

  export interface CreateTaskInput {
    cameraId: string;
    name: string;
  }

  export interface UpdateTaskInput {
    name: string;
  }

  export interface BatchDeleteTaskInput {
    cameraIds: string[];
  }

  /** 未分配任务的摄像头轻量信息（供下拉选择） */
  export interface AvailableCameraItem {
    cameraId: string;
    name: string;
    protocol: string;
  }

  /** 算法实例项 */
  export interface InstanceItem {
    instanceId: string;
    cameraId: string;
    algorithmId: string;
    analysisFps: number;
    paramsJson: Record<string, unknown> | string;
    rules: DetectionRule[];
    enabled: boolean;
    actualStatus: InstanceStatusCode;
    statusMessage: string;
    currentFps: null | number;
    reportedAt: null | string;
  }

  export interface CreateInstanceInput {
    cameraId: string;
    algorithmId: string;
    analysisFps?: number;
    paramsJson?: Record<string, unknown> | string;
    rules?: DetectionRule[];
    enabled?: boolean;
  }

  export interface UpdateInstanceInput {
    analysisFps: number;
    paramsJson: Record<string, unknown> | string;
    rules: DetectionRule[];
  }
}

/**
 * 分页获取分析任务列表
 */
export async function getTaskListApi(params?: TaskApi.TaskListQuery) {
  return requestClient.get<TaskApi.TaskPageResult>('/task/list', { params });
}

/**
 * 获取任务管理概览统计（在线任务/已调度实例/计算单元负载）
 */
export async function getTaskStatsApi() {
  return requestClient.get<TaskApi.TaskStats>('/task/stats');
}

/**
 * 创建分析任务
 */
export async function createTaskApi(data: TaskApi.CreateTaskInput) {
  return requestClient.post<TaskApi.TaskItem>('/task', data);
}

/**
 * 更新分析任务名称
 */
export async function updateTaskApi(
  cameraId: string,
  data: TaskApi.UpdateTaskInput,
) {
  return requestClient.put<null>(`/task/${cameraId}`, data);
}

/**
 * 启停分析任务
 */
export async function setTaskEnabledApi(cameraId: string, enabled: boolean) {
  return requestClient.put<null>(`/task/${cameraId}/enabled`, { enabled });
}

/**
 * 删除分析任务
 */
export async function deleteTaskApi(cameraId: string) {
  return requestClient.delete<null>(`/task/${cameraId}`);
}

/**
 * 批量删除分析任务
 */
export async function batchDeleteTasksApi(data: TaskApi.BatchDeleteTaskInput) {
  return requestClient.delete<null>('/task/batch', { data });
}

/**
 * 获取未建任务的可用摄像头列表
 */
export async function getAvailableCamerasApi() {
  return requestClient.get<TaskApi.AvailableCameraItem[]>(
    '/task/available-cameras',
  );
}

/**
 * 获取指定摄像头的算法实例列表
 */
export async function getInstanceListApi(cameraId: string) {
  return requestClient.get<TaskApi.InstanceItem[]>('/task/instance/list', {
    params: { cameraId },
  });
}

/**
 * 创建算法实例
 */
export async function createInstanceApi(data: TaskApi.CreateInstanceInput) {
  return requestClient.post<TaskApi.InstanceItem>('/task/instance', data);
}

/**
 * 更新算法实例配置（整份提交）
 */
export async function updateInstanceApi(
  instanceId: string,
  data: TaskApi.UpdateInstanceInput,
) {
  return requestClient.put<null>(`/task/instance/${instanceId}`, data);
}

/**
 * 启停算法实例
 */
export async function setInstanceEnabledApi(
  instanceId: string,
  enabled: boolean,
) {
  return requestClient.put<null>(`/task/instance/${instanceId}/enabled`, {
    enabled,
  });
}

/**
 * 删除算法实例
 */
export async function deleteInstanceApi(instanceId: string) {
  return requestClient.delete<null>(`/task/instance/${instanceId}`);
}
