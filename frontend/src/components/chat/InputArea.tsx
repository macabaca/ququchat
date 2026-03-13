import React, { useState } from 'react';
import { Input, Button, Upload, message, Progress, Space } from 'antd';
import { SendOutlined, PaperClipOutlined, PictureOutlined } from '@ant-design/icons';
import { fileService } from '../../api/FileService';
import { localFileService } from '../../api/LocalFileService';
import { useAuthStore } from '../../stores/authStore';
import { useAIChatStore } from '../../stores/aiChatStore';
import { llmService } from '../../api/LLMService';
import { buildReplySuggestionsPrompt, getRecentRoomMessages, parseReplySuggestions } from '../../stores/aiReplyUtils';

interface InputAreaProps {
    onSend: (
        content: string,
        type: 'text' | 'image' | 'file',
        attachmentId?: string,
        thumbId?: string,
        cachePath?: string
    ) => void;
    roomId: string;
    roomName?: string;
}

const { TextArea } = Input;

const InputArea: React.FC<InputAreaProps> = ({ onSend, roomId, roomName }) => {
    const [value, setValue] = useState('');
    const [uploading, setUploading] = useState(false);
    const [uploadProgress, setUploadProgress] = useState(0);
    const [suggestions, setSuggestions] = useState<string[]>([]);
    const [isSuggesting, setIsSuggesting] = useState(false);
    const [aiError, setAiError] = useState<string>('');
    const currentUser = useAuthStore((state) => state.user);
    const aiConfig = useAIChatStore((state) => state.config);

    const handleSend = () => {
        if (!value.trim()) return;
        onSend(value, 'text');
        setValue('');
        setSuggestions([]);
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSend();
        }
    };

    const normalizeErrorMessage = (error: any) => {
        if (typeof error === 'string') return error;
        if (typeof error?.message === 'string') return error.message;
        if (typeof error?.error === 'string') return error.error;
        if (typeof error?.error?.message === 'string') return error.error.message;
        try {
            return JSON.stringify(error);
        } catch {
            return '生成回复失败';
        }
    };

    const extractJsonErrorMessage = (text: string) => {
        const trimmed = text.trim();
        if (!trimmed.startsWith('{')) return text;
        try {
            const parsed = JSON.parse(trimmed);
            const msg = parsed?.error?.message || parsed?.message;
            return typeof msg === 'string' && msg.trim() ? msg : text;
        } catch {
            return text;
        }
    };

    const handleSuggest = async () => {
        if (isSuggesting) return;
        if (aiError) {
            setAiError('');
            return;
        }
        if (!roomId) return;
        if (!aiConfig.apiKey) {
            setAiError('AI 配置有误：请先配置有效的 API Key');
            return;
        }
        setIsSuggesting(true);
        setAiError('');
        try {
            const list = await getRecentRoomMessages(roomId, 100);
            const prompt = buildReplySuggestionsPrompt({
                messages: list,
                currentUserId: currentUser?.id,
                roomName
            });
            let raw = '';
            const fullText = await llmService.sendMessageStream({
                config: {
                    baseUrl: aiConfig.baseUrl,
                    apiKey: aiConfig.apiKey,
                    model: aiConfig.model,
                    temperature: aiConfig.temperature
                },
                messages: [{ role: 'user', content: prompt }],
                onDelta: (delta) => {
                    raw += delta;
                }
            });
            if (!raw) {
                raw = fullText || '';
            }
            if (!raw.trim()) {
                throw new Error('未获取到模型响应，请检查模型配置');
            }
            const parsed = parseReplySuggestions(raw);
            if (parsed.replies.length === 0) {
                throw new Error('未生成回复建议，请检查模型配置');
            }
            setSuggestions(parsed.replies);
        } catch (error: any) {
            let msg = normalizeErrorMessage(error);
            msg = extractJsonErrorMessage(msg);
            setAiError(msg);
        } finally {
            setIsSuggesting(false);
        }
    };

    const handleUpload = async (file: File, type: 'image' | 'file') => {
        setUploading(true);
        setUploadProgress(0);
        try {
            const response = await fileService.uploadFile(file, (percent) => {
                setUploadProgress(percent);
            });

            const attachmentId = response.attachment?.id;
            if (!attachmentId) {
                throw new Error('Upload succeeded but attachment_id is missing');
            }

            // 上传成功后立即复制到用户私有目录（图片/文件都需要）
            const userCode = currentUser?.user_code ? String(currentUser.user_code) : '';
            if (!userCode) {
                throw new Error('当前用户缺少 user_code，无法写入本地缓存');
            }
            const cachePath = await localFileService.saveUploadedFileToUserDir(file, userCode, attachmentId);

            let contentForSend = file.name || 'File';
            if (type === 'image') {
                const urlRes = await fileService.getFileUrl(attachmentId);
                contentForSend = urlRes.url;
            }

            // Pass attachment_id / thumb_attachment_id / cache_path for local-first rendering and SQLite persistence
            onSend(contentForSend, type, attachmentId, response.attachment?.thumb_attachment_id, cachePath);
            message.success('Upload successful');
        } catch (error) {
            message.error('Upload failed');
            console.error(error);
        } finally {
            setUploading(false);
            setUploadProgress(0);
        }
        return false; // Prevent auto upload by antd
    };

    return (
        <div style={{ borderTop: '1px solid #f0f0f0', background: '#fff', padding: '10px' }}>
            <div style={{ marginBottom: '8px', display: 'flex', gap: '8px', alignItems: 'center' }}>
                <Upload
                    showUploadList={false}
                    beforeUpload={(file) => handleUpload(file, 'image')}
                    accept="image/*"
                    disabled={uploading}
                >
                    <Button type="text" icon={<PictureOutlined />} loading={uploading} />
                </Upload>
                <Upload
                    showUploadList={false}
                    beforeUpload={(file) => handleUpload(file, 'file')}
                    disabled={uploading}
                >
                    <Button type="text" icon={<PaperClipOutlined />} loading={uploading} />
                </Upload>
                {uploading && (
                    <div style={{ width: 100, marginLeft: 8 }}>
                        <Progress percent={uploadProgress} size="small" status="active" />
                    </div>
                )}
            </div>
            {suggestions.length > 0 && (
                <div style={{ marginBottom: 8 }}>
                    <Space wrap>
                        {suggestions.map((item, index) => (
                            <Button key={`${item}-${index}`} size="small" onClick={() => setValue(item)}>
                                {item}
                            </Button>
                        ))}
                    </Space>
                </div>
            )}
            <TextArea
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Type a message..."
                autoSize={{ minRows: 2, maxRows: 6 }}
                style={{ border: 'none', resize: 'none', boxShadow: 'none' }}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '8px' }}>
                <Space>
                    <Button onClick={handleSuggest} loading={isSuggesting} danger={!!aiError}>
                        {aiError ? 'AI 回复失败' : 'AI 回复'}
                    </Button>
                    <Button type="primary" onClick={handleSend} icon={<SendOutlined />}>
                        Send
                    </Button>
                </Space>
            </div>
        </div>
    );
};

export default InputArea;
