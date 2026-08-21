import { requestClient } from '#/api/request';

export namespace FileApi {
  export interface UploadResult {
    key: string;
    name: string;
    size: number;
    contentType: string;
    url: string;
  }
}

/**
 * 上传单个文件
 */
export async function uploadFileApi(file: Blob | File) {
  return requestClient.upload<FileApi.UploadResult>('/file/upload', { file });
}
