import apiClient from "./apiClient";
import { GetHistoryResponse } from "../types/api";
import { Message } from "../types/models";
import { messageDao, MessageRow } from "./db_sqlite";
import { useAuthStore } from "../stores/authStore";
import { localFileService } from "./LocalFileService";

export const messageService = {
    // Save message to local DB and handle file downloads
    saveMessage: async (msg: Message) => {
        const rawMsg = msg as Message & Record<string, any>;
        const payloadSource = rawMsg.payload_json ?? rawMsg.payload;
        const payload = (() => {
            if (!payloadSource) return {};
            if (typeof payloadSource === 'string') {
                try {
                    return JSON.parse(payloadSource);
                } catch {
                    return {};
                }
            }
            if (typeof payloadSource === 'object') return payloadSource;
            return {};
        })();

        // 兼容 WS 与 history 接口不同字段
        const normalizedAttachmentId = rawMsg.attachment_id || payload.attachment_id || null;
        const normalizedThumbAttachmentId = rawMsg.thumb_attachment_id || payload.thumb_attachment_id || null;
        const normalizedContentType = rawMsg.content_type || payload.content_type || (rawMsg.is_image ? 'image' : (normalizedAttachmentId ? 'file' : 'text'));
        const isImageMessage = !!rawMsg.is_image || normalizedContentType === 'image' || !!normalizedThumbAttachmentId;
        let normalizedCachePath = typeof rawMsg.cache_path === 'string' && rawMsg.cache_path.trim() ? rawMsg.cache_path : null;
        const currentUserCode = useAuthStore.getState().user?.user_code;
        const shouldCacheAttachmentLocally = normalizedContentType === 'image' || normalizedContentType === 'file' || !!normalizedAttachmentId;
        const timestampSeconds = Number(rawMsg.timestamp ?? rawMsg.created_at ?? Date.now() / 1000);
        const createdAtMs = timestampSeconds > 1e12 ? timestampSeconds : timestampSeconds * 1000;

        console.info('[MessageService] saveMessage entry', {
            id: rawMsg.id,
            room_id: rawMsg.room_id,
            is_image: rawMsg.is_image,
            content_type: rawMsg.content_type,
            attachment_id: normalizedAttachmentId,
            thumb_attachment_id: normalizedThumbAttachmentId
        });

        // 回填标准字段，避免后续逻辑依赖历史接口原始字段
        if (!rawMsg.content && rawMsg.content_text) {
            rawMsg.content = rawMsg.content_text;
        }
        if (!rawMsg.from_user_id && rawMsg.sender_id) {
            rawMsg.from_user_id = rawMsg.sender_id;
        }
        if (!rawMsg.timestamp && rawMsg.created_at) {
            rawMsg.timestamp = rawMsg.created_at;
        }
        if (!rawMsg.attachment_id && normalizedAttachmentId) {
            rawMsg.attachment_id = normalizedAttachmentId;
        }
        if (!rawMsg.thumb_attachment_id && normalizedThumbAttachmentId) {
            rawMsg.thumb_attachment_id = normalizedThumbAttachmentId;
        }
        if (!rawMsg.is_image && isImageMessage) {
            rawMsg.is_image = true;
        }
        if (!rawMsg.payload_json && Object.keys(payload).length > 0) {
            rawMsg.payload_json = payload;
        }

        // 1. Prepare message row
        const messageRow: MessageRow = {
            id: rawMsg.id || `msg-${Date.now()}`,
            room_id: rawMsg.room_id || '',
            sequence_id: rawMsg.sequence_id || 0,
            sender_id: rawMsg.from_user_id || rawMsg.sender_id || '',
            content_type: isImageMessage ? 'image' : (normalizedContentType || 'text'),
            content_text: rawMsg.content || rawMsg.content_text || '',
            cache_path: normalizedCachePath,
            attachment_id: normalizedAttachmentId,
            payload_json: null,
            created_at: createdAtMs,
            status: rawMsg.status || 'sent'
        };

        // 2. Handle attachment cache download (image/file):
        // 一律使用 attachment_id 获取 URL 并下载原文件到本地
        if (!normalizedCachePath && shouldCacheAttachmentLocally && normalizedAttachmentId && currentUserCode && window.electronAPI) {
            try {
                normalizedCachePath = await localFileService.downloadAndSave(
                    normalizedAttachmentId,
                    normalizedAttachmentId,
                    String(currentUserCode)
                );
                messageRow.cache_path = normalizedCachePath;
            } catch (error) {
                console.warn('[MessageService] failed to cache attachment locally', {
                    id: rawMsg.id,
                    attachment_id: normalizedAttachmentId,
                    error
                });
            }
        }

        if (messageRow.cache_path) {
            rawMsg.cache_path = messageRow.cache_path;
        }
        messageRow.payload_json = JSON.stringify(rawMsg);

        // 3. Save to SQLite
        await messageDao.upsert(messageRow);
        
        return messageRow;
    },

    // 获取指定 Sequence 之后的历史记录 (增量同步)
    getHistoryAfter: async (roomId: string, afterSequenceId: number = 0): Promise<GetHistoryResponse> => {
        return await apiClient.get('/messages/history/after', {
            params: {
                room_id: roomId,
                after_sequence_id: afterSequenceId
            }
        });
    },

    // 获取指定消息之前的历史记录 (分页加载)
    getHistoryBefore: async (roomId: string, messageId: string): Promise<GetHistoryResponse> => {
        return await apiClient.get('/messages/history/before', {
            params: {
                room_id: roomId,
                message_id: messageId
            }
        });
    },

    // 获取最新消息 (初次加载)
    getLatestByFriend: async (friendId: string): Promise<GetHistoryResponse> => {
        return await apiClient.get('/messages/history/latest', {
            params: {
                friend_id: friendId
            }
        });
    },

    getLatestByGroup: async (groupId: string): Promise<GetHistoryResponse> => {
        return await apiClient.get('/messages/history/group', {
            params: {
                group_id: groupId
            }
        });
    }
};
