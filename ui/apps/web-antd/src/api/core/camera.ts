import { requestClient } from '#/api/request';

export namespace CameraApi {
  /** 最近一次测活状态 */
  export type ProbeStatus = 'failed' | 'never' | 'success';

  /** 摄像头列表/详情项 */
  export interface CameraItem {
    id: number;
    cameraId: string;
    protocol: string;
    name: string;
    rtspUrl: string;
    subRtspUrl: string;
    remark: string;
    transportPolicy: string;
    lastProbeStatus: ProbeStatus;
    lastProbeAt: null | string;
    lastProbeErrorCode: string;
    lastSuccessAt: null | string;
    lastSuccessTransport: string;
    lastCodec: string;
    lastWidth: number;
    lastHeight: number;
    lastFps: number;
    createdAt: string;
    updatedAt: string;
  }

  export interface CameraPageQuery {
    page?: number;
    pageSize?: number;
    name?: string;
  }

  export interface CameraPageResult {
    items: CameraItem[];
    total: number;
  }

  export interface SaveCameraInput {
    name: string;
    rtspUrl: string;
    subRtspUrl?: string;
    remark?: string;
  }

  export interface LiveStreamResult {
    streamPath: string;
    httpPort: number;
    wsPort: number;
    httpUrl: string;
    wsUrl: string;
    streamType: string;
  }

  /** 单次传输方式尝试结果 */
  export interface ProbeAttempt {
    transport: 'tcp' | 'udp';
    elapsedMs: number;
    failureCode: string;
  }

  /** 测活结构化结果（无论成功失败均 code=0） */
  export interface ProbeResult {
    status: 'failed' | 'success';
    failureCode: string;
    failureMessage: string;
    attempts: ProbeAttempt[];
    selectedTransport: string;
    codec: string;
    width: number;
    height: number;
    fps: number;
    elapsedMs: number;
    persisted: boolean;
    stale: boolean;
  }

  export interface ProbeCameraInput {
    id?: number;
    protocol?: string;
    rtspUrl: string;
  }
}

/**
 * 获取摄像头分页列表
 */
export async function getCameraPageApi(params?: CameraApi.CameraPageQuery) {
  return requestClient.get<CameraApi.CameraPageResult>('/camera/page', {
    params,
  });
}

/**
 * 创建摄像头
 */
export async function createCameraApi(data: CameraApi.SaveCameraInput) {
  return requestClient.post<CameraApi.CameraItem>('/camera', data);
}

/**
 * 更新摄像头
 */
export async function updateCameraApi(
  id: number,
  data: CameraApi.SaveCameraInput,
) {
  return requestClient.put<CameraApi.CameraItem>(`/camera/${id}`, data);
}

/**
 * 删除摄像头
 */
export async function deleteCameraApi(id: number) {
  return requestClient.delete<null>(`/camera/${id}`);
}

/**
 * 批量删除摄像头
 */
export async function batchDeleteCameraApi(ids: number[]) {
  return requestClient.delete<null>('/camera/batch', { data: { ids } });
}

/**
 * 摄像头测活（TCP 优先，失败回退 UDP；测活失败也返回 code=0，结果在 data.status）
 * 后端整体超时约为 12s，此处前端设置 15s 超时，避免网络层在后端诊断完成前过早抛出 timeout
 */
export async function probeCameraApi(data: CameraApi.ProbeCameraInput) {
  return requestClient.post<CameraApi.ProbeResult>('/camera/probe', data, {
    timeout: 15_000,
  });
}

/**
 * 开启实时预览取流
 */
export async function startLivePreviewApi(
  id: number,
  streamType: 'main' | 'sub' = 'main',
) {
  return requestClient.post<CameraApi.LiveStreamResult>(
    `/camera/${id}/preview/start`,
    undefined,
    {
      params: { streamType },
    },
  );
}

/**
 * 停止实时预览取流
 */
export async function stopLivePreviewApi(
  id: number,
  streamType: 'main' | 'sub' = 'main',
) {
  return requestClient.post<null>(`/camera/${id}/preview/stop`, undefined, {
    params: { streamType },
  });
}
