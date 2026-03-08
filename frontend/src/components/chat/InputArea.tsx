import React, { useState } from 'react';
import { Input, Button, Upload, message, Progress } from 'antd';
import { SendOutlined, PaperClipOutlined, PictureOutlined } from '@ant-design/icons';
import { fileService } from '../../api/FileService';
import { localFileService } from '../../api/LocalFileService';
import { useAuthStore } from '../../stores/authStore';

interface InputAreaProps {
    onSend: (
        content: string,
        type: 'text' | 'image' | 'file',
        attachmentId?: string,
        thumbId?: string,
        cachePath?: string
    ) => void;
}

const { TextArea } = Input;

const InputArea: React.FC<InputAreaProps> = ({ onSend }) => {
    const [value, setValue] = useState('');
    const [uploading, setUploading] = useState(false);
    const [uploadProgress, setUploadProgress] = useState(0);
    const currentUser = useAuthStore((state) => state.user);

    const handleSend = () => {
        if (!value.trim()) return;
        onSend(value, 'text');
        setValue('');
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSend();
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
            <TextArea
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Type a message..."
                autoSize={{ minRows: 2, maxRows: 6 }}
                style={{ border: 'none', resize: 'none', boxShadow: 'none' }}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '8px' }}>
                <Button type="primary" onClick={handleSend} icon={<SendOutlined />}>
                    Send
                </Button>
            </div>
        </div>
    );
};

export default InputArea;
