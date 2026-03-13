import apiClient from "./apiClient";
import { UploadFileResponse, GetFileUrlResponse } from "../types/api";

const CHUNK_SIZE = 5 * 1024 * 1024; // 5MB per chunk

interface InitiateMultipartResponse {
    upload_id: string;
    id: string; // Backend returns "id" instead of "attachment_id"
    attachment_id?: string;
    storage_key: string;
    file_name: string;
    mime_type: string;
}

interface PartResponse {
    part_number: number;
    etag: string;
    size: number;
}

export const fileService = {
    // 原始单文件上传（保留以备不时之需，或用于小文件）
    // 文档对应: 1. 上传文件（普通上传）
    uploadFileSimple: async (file: File): Promise<UploadFileResponse> => {
        const formData = new FormData();
        formData.append('file', file);
        return await apiClient.post('/files/upload', formData, {
            headers: { 'Content-Type': 'multipart/form-data' }
        });
    },

    // 分片上传主逻辑
    // 文档对应: 4, 5, 7 接口
    uploadFile: async (file: File, onProgress?: (percent: number) => void): Promise<UploadFileResponse> => {
        // 1. 初始化分片上传 (4. 初始化分片上传)
        // URL: /api/files/multipart/start
        const initRes = await apiClient.post<InitiateMultipartResponse>('/files/multipart/start', {
            file_name: file.name,
            mime_type: file.type
        }) as unknown as InitiateMultipartResponse;
        
        const { upload_id, storage_key, id, attachment_id } = initRes;
        const finalAttachmentId = id || attachment_id;

        if (!finalAttachmentId) {
            throw new Error('Failed to get attachment ID from initialization response');
        }

        // 2. 分片上传 (5. 上传分片)
        // URL: /api/files/multipart/part
        const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
        
        for (let partNumber = 1; partNumber <= totalChunks; partNumber++) {
            const start = (partNumber - 1) * CHUNK_SIZE;
            const end = Math.min(start + CHUNK_SIZE, file.size);
            const chunk = file.slice(start, end);

            const formData = new FormData();
            formData.append('upload_id', upload_id);
            formData.append('storage_key', storage_key);
            formData.append('part_number', partNumber.toString());
            formData.append('file', chunk);

            await apiClient.post<PartResponse>('/files/multipart/part', formData, {
                headers: { 'Content-Type': 'multipart/form-data' }
            });

            // 计算并回调进度
            if (onProgress) {
                const percent = Math.round((partNumber / totalChunks) * 100);
                onProgress(percent);
            }
        }

        // 3. 完成分片上传 (7. 完成分片上传)
        // URL: /api/files/multipart/complete
        const completeRes = await apiClient.post<UploadFileResponse>('/files/multipart/complete', {
            upload_id: upload_id,
            storage_key: storage_key,
            attachment_id: finalAttachmentId,
            file_name: file.name,
            mime_type: file.type
            // expected_sha256: optional
        }) as unknown as UploadFileResponse;

        return completeRes;
    },

    // 取消分片上传
    // 文档对应: 8. 取消分片上传
    abortUpload: async (uploadId: string, storageKey: string): Promise<{ message: string }> => {
        return await apiClient.post('/files/multipart/abort', {
            upload_id: uploadId,
            storage_key: storageKey
        });
    },

    // 获取文件下载链接
    // 文档对应: 2. 获取文件下载链接（原文件）
    getFileUrl: async (attachmentId: string): Promise<GetFileUrlResponse> => {
        return await apiClient.get(`/files/${attachmentId}/url`);
    },
};
