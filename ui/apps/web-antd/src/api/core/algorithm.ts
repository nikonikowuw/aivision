import { requestClient } from '#/api/request';

export namespace AlgorithmApi {
  export interface FPSTier {
    fps: number;
    units: number;
  }

  export interface AlgorithmVersionItem {
    id: number;
    algorithmId: string;
    version: string;
    platformId: string;
    minAdapterVersion: string;
    packageRoot: string;
    fpsTiers: FPSTier[];
    configSchema: Record<string, unknown>;
    manifestRaw: Record<string, unknown>;
    packageSizeBytes: number;
    isActive: boolean;
    isBuiltin?: boolean;
    createdAt: string;
    updatedAt: string;
  }

  export interface AlgorithmItem {
    id: number;
    algorithmId: string;
    name: string;
    algorithmType: string;
    alarmTypeId: string;
    activeVersion: string;
    description: string;
    isBuiltin?: boolean;
    createdAt: string;
    updatedAt: string;
    versions?: AlgorithmVersionItem[];
  }

  export interface AlgorithmPageQuery {
    page?: number;
    pageSize?: number;
    algorithmType?: string;
    keyword?: string;
    isBuiltin?: boolean;
  }

  export interface AlgorithmPageResult {
    items: AlgorithmItem[];
    total: number;
  }
}

/**
 * 分页获取算法列表
 */
export async function getAlgorithmList(
  params?: AlgorithmApi.AlgorithmPageQuery,
) {
  return requestClient.get<AlgorithmApi.AlgorithmPageResult>('/algorithm', {
    params,
  });
}

/**
 * 获取单个算法详情
 */
export async function getAlgorithmDetail(algorithmId: string) {
  return requestClient.get<AlgorithmApi.AlgorithmItem>(
    `/algorithm/${algorithmId}`,
  );
}

/**
 * 获取某算法的所有版本列表
 */
export async function getAlgorithmVersions(algorithmId: string) {
  return requestClient.get<AlgorithmApi.AlgorithmVersionItem[]>(
    `/algorithm/${algorithmId}/versions`,
  );
}

/**
 * 上传并安装算法包
 */
export async function uploadAlgorithmPackage(file: File) {
  const formData = new FormData();
  formData.append('file', file);
  return requestClient.post<AlgorithmApi.AlgorithmVersionItem>(
    '/algorithm/upload',
    formData,
    {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    },
  );
}

/**
 * 激活/切换算法版本
 */
export async function activateAlgorithmVersion(
  algorithmId: string,
  version: string,
) {
  return requestClient.put<null>(
    `/algorithm/${algorithmId}/versions/${version}/activate`,
  );
}

/**
 * 卸载算法版本
 */
export async function uninstallAlgorithmVersion(
  algorithmId: string,
  version: string,
) {
  return requestClient.delete<null>(
    `/algorithm/${algorithmId}/versions/${version}`,
  );
}
